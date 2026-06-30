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
	Register("vastai", newVastAIProvider)
}

const vastDefaultBaseURL = "https://console.vast.ai/api/v0"

// vastConfig is the decrypted provider config (stored in autoscale_providers.config).
type vastConfig struct {
	APIKey   string  `json:"api_key"`
	GPUName  string  `json:"gpu_name"`            // Vast format with underscores, e.g. "RTX_4090"; overrides spec.GPU
	MaxPrice float64 `json:"max_price_per_hour"`  // overrides spec.MaxPricePerHour when > 0
	DiskGB   int     `json:"disk_gb"`             // fallback when spec.DiskGB is 0
	OnStart  string  `json:"onstart"`             // optional onstart script contents
	BaseURL  string  `json:"base_url"`            // optional override (tests)
}

// vastAIProvider talks to the Vast.ai REST API (console.vast.ai/api/v0).
type vastAIProvider struct {
	cfg    vastConfig
	client *http.Client
}

func newVastAIProvider(raw json.RawMessage) (Provider, error) {
	var cfg vastConfig
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("invalid vastai config: %w", err)
		}
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("vastai config requires api_key")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = vastDefaultBaseURL
	}
	return &vastAIProvider{cfg: cfg, client: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (p *vastAIProvider) Name() string { return "vastai" }

// Provision searches the marketplace for the cheapest matching offer and rents it.
func (p *vastAIProvider) Provision(ctx context.Context, spec InstanceSpec) (string, error) {
	gpuName := p.cfg.GPUName
	if gpuName == "" {
		gpuName = strings.ReplaceAll(strings.TrimSpace(spec.GPU), " ", "_")
	}
	maxPrice := spec.MaxPricePerHour
	if p.cfg.MaxPrice > 0 {
		maxPrice = p.cfg.MaxPrice
	}
	disk := spec.DiskGB
	if disk <= 0 {
		disk = p.cfg.DiskGB
	}

	offerID, err := p.cheapestOffer(ctx, gpuName, maxPrice, disk)
	if err != nil {
		return "", err
	}

	// Vast env is a dict of docker-style flags: {"-e VAR": "value"}.
	denv := make(map[string]string, len(spec.Env))
	for k, v := range spec.Env {
		denv["-e "+k] = v
	}

	body := map[string]interface{}{
		"client_id": "me",
		"image":     spec.Image,
		"disk":      disk,
		"label":     podName(spec),
		"runtype":   "args", // run the image's own entrypoint (worker auto-starts from env)
		"env":       denv,
		"onstart":   p.cfg.OnStart,
		"target_state": "running",
	}

	var resp struct {
		Success     bool        `json:"success"`
		NewContract json.Number `json:"new_contract"`
		Error       string      `json:"error"`
		Msg         string      `json:"msg"`
	}
	if err := p.do(ctx, http.MethodPost, "/instances/"+offerID+"/", body, &resp); err != nil {
		return "", err
	}
	if resp.NewContract.String() == "" || resp.NewContract.String() == "0" {
		if resp.Error != "" || resp.Msg != "" {
			return "", fmt.Errorf("vastai create failed: %s %s", resp.Error, resp.Msg)
		}
		return "", fmt.Errorf("vastai create returned no contract id")
	}
	return resp.NewContract.String(), nil
}

// cheapestOffer searches /asks/ and returns the lowest-price rentable offer id.
func (p *vastAIProvider) cheapestOffer(ctx context.Context, gpuName string, maxPrice float64, disk int) (string, error) {
	q := map[string]interface{}{
		"rentable": map[string]interface{}{"eq": true},
		"rented":   map[string]interface{}{"eq": false},
		"num_gpus": map[string]interface{}{"gte": 1},
		"order":    [][]interface{}{{"dph_total", "asc"}},
		"limit":    20,
	}
	if gpuName != "" {
		q["gpu_name"] = map[string]interface{}{"eq": gpuName}
	}
	if maxPrice > 0 {
		q["dph_total"] = map[string]interface{}{"lte": maxPrice}
	}
	if disk > 0 {
		q["disk_space"] = map[string]interface{}{"gte": disk}
	}

	var resp struct {
		Offers []struct {
			ID       json.Number `json:"id"`
			DPHTotal float64     `json:"dph_total"`
		} `json:"offers"`
	}
	if err := p.do(ctx, http.MethodPost, "/asks/", map[string]interface{}{"q": q}, &resp); err != nil {
		return "", err
	}
	if len(resp.Offers) == 0 {
		return "", fmt.Errorf("vastai: no offers match (gpu=%q max_price=%.2f disk=%d)", gpuName, maxPrice, disk)
	}
	return resp.Offers[0].ID.String(), nil
}

func (p *vastAIProvider) Destroy(ctx context.Context, instanceID string) error {
	err := p.do(ctx, http.MethodDelete, "/instances/"+instanceID+"/", nil, nil)
	if err != nil && isNotFound(err) {
		return nil
	}
	return err
}

func (p *vastAIProvider) Status(ctx context.Context, instanceID string) (InstanceStatus, error) {
	var resp struct {
		Instances []struct {
			ID           json.Number `json:"id"`
			ActualStatus string      `json:"actual_status"`
			CurState     string      `json:"cur_state"`
		} `json:"instances"`
	}
	if err := p.do(ctx, http.MethodGet, "/instances/", nil, &resp); err != nil {
		return InstanceStatus{}, err
	}
	for _, inst := range resp.Instances {
		if inst.ID.String() == instanceID {
			return InstanceStatus{InstanceID: instanceID, Status: mapVastStatus(inst.ActualStatus, inst.CurState)}, nil
		}
	}
	// Not in the list anymore -> gone.
	return InstanceStatus{InstanceID: instanceID, Status: StatusTerminated}, nil
}

// mapVastStatus maps Vast's actual_status/cur_state to internal provider states.
func mapVastStatus(actual, cur string) string {
	switch strings.ToLower(actual) {
	case "running":
		return StatusRunning
	case "exited", "offline":
		return StatusTerminated
	case "created", "loading", "":
		// Fall back to cur_state when actual_status is not yet populated.
		if strings.EqualFold(cur, "running") {
			return StatusRunning
		}
		if strings.EqualFold(cur, "stopped") {
			return StatusTerminated
		}
		return StatusProvisioning
	default:
		return StatusProvisioning
	}
}

// do performs an authenticated request, decoding a 2xx JSON body into out (if non-nil).
func (p *vastAIProvider) do(ctx context.Context, method, path string, body, out interface{}) error {
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
		return fmt.Errorf("vastai request failed: %w", err)
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
