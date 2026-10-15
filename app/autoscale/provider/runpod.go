package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func init() {
	Register("runpod", newRunPodProvider)
}

const runpodDefaultBaseURL = "https://rest.runpod.io/v1"

// runpodConfig is the decrypted provider config (stored in autoscale_providers.config).
type runpodConfig struct {
	APIKey    string `json:"api_key"`
	GPUTypeID string `json:"gpu_type_id"` // exact RunPod id, e.g. "NVIDIA GeForce RTX 4090"
	// GPUTypeIDs lists acceptable GPU types in order of preference; RunPod rents the
	// first one available. Takes precedence over gpu_type_id. Cheap types are often
	// out of stock, so a list makes rentals far more likely to succeed.
	GPUTypeIDs      []string `json:"gpu_type_ids,omitempty"`
	CloudType       string   `json:"cloud_type"`        // "SECURE" (default) or "COMMUNITY"
	ContainerDiskGB int      `json:"container_disk_gb"` // fallback when InstanceSpec.DiskGB is 0
	VolumeGB        int      `json:"volume_in_gb"`      // optional persistent volume
	VolumeMountPath string   `json:"volume_mount_path"` // optional, e.g. "/workspace"
	BaseURL         string   `json:"base_url"`          // optional override (tests)
	// MinCUDAVersion restricts hosts to drivers supporting at least this CUDA version
	// (default "12.8", required by the inference image). "none" disables the filter.
	MinCUDAVersion string `json:"min_cuda_version,omitempty"`
}

// runpodCUDAVersions is RunPod's allowedCudaVersions enum, oldest first.
var runpodCUDAVersions = []string{"11.8", "12.0", "12.1", "12.2", "12.3", "12.4", "12.5", "12.6", "12.7", "12.8", "12.9", "13.0"}

// allowedCUDAVersions returns the enum values >= min ("" -> default 12.8, "none" -> nil).
func allowedCUDAVersions(min string) []string {
	switch min {
	case "none":
		return nil
	case "":
		min = defaultMinCUDAVersion
	}
	for i, v := range runpodCUDAVersions {
		if v == min {
			return runpodCUDAVersions[i:]
		}
	}
	return nil
}

// runPodProvider talks to the RunPod REST v1 API.
type runPodProvider struct {
	cfg    runpodConfig
	client *http.Client
}

func newRunPodProvider(raw json.RawMessage) (Provider, error) {
	var cfg runpodConfig
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("invalid runpod config: %w", err)
		}
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("runpod config requires api_key")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = runpodDefaultBaseURL
	}
	if cfg.CloudType == "" {
		cfg.CloudType = "SECURE"
	}
	return &runPodProvider{cfg: cfg, client: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (p *runPodProvider) Name() string { return "runpod" }

// createPodResponse is the subset of the POST /pods response we use.
type createPodResponse struct {
	ID            string `json:"id"`
	DesiredStatus string `json:"desiredStatus"`
}

func (p *runPodProvider) Provision(ctx context.Context, spec InstanceSpec) (string, error) {
	gpuTypes := p.cfg.GPUTypeIDs
	if len(gpuTypes) == 0 {
		gpuType := p.cfg.GPUTypeID
		if gpuType == "" {
			gpuType = spec.GPU
		}
		if gpuType != "" {
			gpuTypes = []string{gpuType}
		}
	}
	disk := spec.DiskGB
	if disk <= 0 {
		disk = p.cfg.ContainerDiskGB
	}

	body := map[string]interface{}{
		"name":              podName(spec),
		"imageName":         spec.Image,
		"gpuCount":          1,
		"cloudType":         p.cfg.CloudType,
		"containerDiskInGb": disk,
		"env":               spec.Env, // RunPod REST takes env as a {KEY:VALUE} object
	}
	if len(gpuTypes) > 0 {
		body["gpuTypeIds"] = gpuTypes
	}
	// RunPod creates (and bills) a 20 GB persistent volume when volumeInGb is omitted:
	// workers do not need one unless configured.
	body["volumeInGb"] = p.cfg.VolumeGB
	if cuda := allowedCUDAVersions(p.cfg.MinCUDAVersion); len(cuda) > 0 {
		body["allowedCudaVersions"] = cuda
	}
	if p.cfg.VolumeMountPath != "" {
		body["volumeMountPath"] = p.cfg.VolumeMountPath
	}

	var resp createPodResponse
	if err := p.do(ctx, http.MethodPost, "/pods", body, &resp); err != nil {
		return "", err
	}
	if resp.ID == "" {
		return "", fmt.Errorf("runpod: create pod returned no id")
	}
	return resp.ID, nil
}

// Destroy terminates a pod. It is idempotent (gone is success) and refuses any pod whose
// name lacks runqy's prefix, so a wrong id can never destroy a pod runqy did not create
// (the account may run hand-made production pods).
func (p *runPodProvider) Destroy(ctx context.Context, instanceID string) error {
	var pod struct {
		Name string `json:"name"`
	}
	if err := p.do(ctx, http.MethodGet, "/pods/"+instanceID, nil, &pod); err != nil {
		if isNotFound(err) {
			return nil // already gone
		}
		return err
	}
	if !strings.HasPrefix(pod.Name, runqyLabelPrefix) {
		return fmt.Errorf("runpod: refusing to destroy pod %s: name %q was not set by runqy (expected prefix %q)", instanceID, pod.Name, runqyLabelPrefix)
	}
	err := p.do(ctx, http.MethodDelete, "/pods/"+instanceID, nil, nil)
	if err != nil && isNotFound(err) {
		return nil // already gone — idempotent
	}
	return err
}

func (p *runPodProvider) Status(ctx context.Context, instanceID string) (InstanceStatus, error) {
	var pod struct {
		ID            string `json:"id"`
		DesiredStatus string `json:"desiredStatus"`
	}
	err := p.do(ctx, http.MethodGet, "/pods/"+instanceID, nil, &pod)
	if err != nil {
		if isNotFound(err) {
			return InstanceStatus{InstanceID: instanceID, Status: StatusTerminated}, nil
		}
		return InstanceStatus{}, err
	}
	return InstanceStatus{InstanceID: instanceID, Status: mapRunPodStatus(pod.DesiredStatus)}, nil
}

// mapRunPodStatus maps RunPod desiredStatus to our provider status constants.
func mapRunPodStatus(s string) string {
	switch strings.ToUpper(s) {
	case "RUNNING":
		return StatusRunning
	case "EXITED", "TERMINATED", "DEAD":
		return StatusTerminated
	case "":
		return StatusProvisioning
	default:
		return StatusProvisioning
	}
}

// runqyLabelPrefix starts the name/label of every machine runqy creates. Providers use
// it to recognise their own instances (Vast.ai's Destroy refuses anything else).
const runqyLabelPrefix = "runqy-"

// podName derives a stable, human-readable pod name from the spec.
func podName(spec InstanceSpec) string {
	suffix := spec.Env["RUNQY_INSTANCE_ID"]
	if len(suffix) > 12 {
		suffix = suffix[len(suffix)-12:]
	}
	name := runqyLabelPrefix + spec.Queue
	if suffix != "" {
		name += "-" + suffix
	}
	return name
}

// httpError carries the status code so callers can detect 404s.
type httpError struct {
	code int
	body string
}

func (e *httpError) Error() string { return fmt.Sprintf("provider api error (%d): %s", e.code, e.body) }

func isNotFound(err error) bool {
	he, ok := err.(*httpError)
	return ok && he.code == http.StatusNotFound
}

// do performs an authenticated request, decoding a 2xx JSON body into out (if non-nil).
func (p *runPodProvider) do(ctx context.Context, method, path string, body, out interface{}) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.cfg.BaseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("runpod request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return &httpError{code: resp.StatusCode, body: string(respBody)}
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
