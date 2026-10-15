package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
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
	GPUName  string  `json:"gpu_name"`           // Vast format with underscores, e.g. "RTX_4090"; overrides spec.GPU
	MaxPrice float64 `json:"max_price_per_hour"` // overrides spec.MaxPricePerHour when > 0
	DiskGB   int     `json:"disk_gb"`            // fallback when spec.DiskGB is 0
	OnStart  string  `json:"onstart"`            // optional onstart script contents
	BaseURL  string  `json:"base_url"`           // optional override (tests)
}

// vastAIProvider talks to the Vast.ai REST API (console.vast.ai/api/v0), following the
// documented endpoints (docs.vast.ai/api-reference):
//
//	search offers    POST   /bundles/          flat filters, no "q" wrapper
//	create instance  PUT    /asks/{offer_id}/  returns {"new_contract": <instance id>}
//	show instance    GET    /instances/{id}/   {"instances": {...}}, or null when gone
//	destroy          DELETE /instances/{id}/
//
// The account may run instances that runqy does not manage (e.g. hand-made production
// pods): runqy only ever reads or destroys the ids it created, and Destroy refuses any
// instance whose label does not carry runqy's prefix.
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

	env, err := vastEnv(spec.Env)
	if err != nil {
		return "", err
	}

	offerID, err := p.cheapestOffer(ctx, gpuName, maxPrice, disk)
	if err != nil {
		return "", err
	}

	body := map[string]interface{}{
		"image":        spec.Image,
		"disk":         disk,
		"label":        podName(spec), // runqy-<queue>-<id>: Destroy only accepts this prefix
		"runtype":      "args",        // run the image's own entrypoint (the worker starts from env)
		"env":          env,
		"onstart":      p.cfg.OnStart,
		"target_state": "running",
	}

	var resp struct {
		Success     bool        `json:"success"`
		NewContract json.Number `json:"new_contract"`
		Error       string      `json:"error"`
		Msg         string      `json:"msg"`
	}
	if err := p.do(ctx, http.MethodPut, "/asks/"+offerID+"/", body, &resp); err != nil {
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

// vastEnv renders env vars in Vast's docker-flag string form: "-e K=V -e K2=V2".
// Values are runqy-controlled (URLs, keys, queue and instance ids); one containing
// whitespace or quotes could not be expressed safely, so it is rejected.
func vastEnv(env map[string]string) (string, error) {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := env[k]
		if strings.ContainsAny(k+v, " \t\r\n\"'") {
			return "", fmt.Errorf("vastai: env var %s contains whitespace or quotes", k)
		}
		parts = append(parts, "-e "+k+"="+v)
	}
	return strings.Join(parts, " "), nil
}

// cheapestOffer searches on-demand offers and returns the lowest-price rentable one.
func (p *vastAIProvider) cheapestOffer(ctx context.Context, gpuName string, maxPrice float64, disk int) (string, error) {
	q := map[string]interface{}{
		"rentable": map[string]interface{}{"eq": true},
		"rented":   map[string]interface{}{"eq": false},
		"num_gpus": map[string]interface{}{"gte": 1},
		"type":     "ondemand",
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
	if err := p.do(ctx, http.MethodPost, "/bundles/", q, &resp); err != nil {
		return "", err
	}
	if len(resp.Offers) == 0 {
		return "", fmt.Errorf("vastai: no offers match (gpu=%q max_price=%.2f disk=%d)", gpuName, maxPrice, disk)
	}
	return resp.Offers[0].ID.String(), nil
}

// vastInstance is the subset of GET /instances/{id}/ we use.
type vastInstance struct {
	ID           json.Number `json:"id"`
	Label        *string     `json:"label"`
	ActualStatus string      `json:"actual_status"`
	CurState     string      `json:"cur_state"`
}

// instance fetches one instance; nil means it does not exist (any more).
func (p *vastAIProvider) instance(ctx context.Context, instanceID string) (*vastInstance, error) {
	var resp struct {
		Instances *vastInstance `json:"instances"`
	}
	err := p.do(ctx, http.MethodGet, "/instances/"+instanceID+"/", nil, &resp)
	if err != nil && isNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return resp.Instances, nil
}

// Destroy tears an instance down. It is idempotent (gone is success) and refuses any
// instance whose label lacks runqy's prefix, so a wrong id can never destroy a machine
// runqy did not create.
func (p *vastAIProvider) Destroy(ctx context.Context, instanceID string) error {
	inst, err := p.instance(ctx, instanceID)
	if err != nil {
		return err
	}
	if inst == nil {
		return nil
	}
	label := ""
	if inst.Label != nil {
		label = *inst.Label
	}
	if !strings.HasPrefix(label, runqyLabelPrefix) {
		return fmt.Errorf("vastai: refusing to destroy instance %s: label %q was not set by runqy (expected prefix %q)", instanceID, label, runqyLabelPrefix)
	}
	err = p.do(ctx, http.MethodDelete, "/instances/"+instanceID+"/", nil, nil)
	if err != nil && isNotFound(err) {
		return nil
	}
	return err
}

func (p *vastAIProvider) Status(ctx context.Context, instanceID string) (InstanceStatus, error) {
	inst, err := p.instance(ctx, instanceID)
	if err != nil {
		return InstanceStatus{}, err
	}
	if inst == nil {
		return InstanceStatus{InstanceID: instanceID, Status: StatusTerminated}, nil
	}
	return InstanceStatus{InstanceID: instanceID, Status: mapVastStatus(inst.ActualStatus, inst.CurState)}, nil
}

// mapVastStatus maps Vast's actual_status/cur_state to internal provider states.
// Per Vast's docs, exited/unknown/offline never reach running again.
func mapVastStatus(actual, cur string) string {
	switch strings.ToLower(actual) {
	case "running":
		return StatusRunning
	case "exited", "offline", "unknown":
		return StatusTerminated
	case "created", "loading":
		// The contract (cur_state) is already "running" while the image downloads.
		if strings.EqualFold(cur, "stopped") {
			return StatusTerminated
		}
		return StatusProvisioning
	case "":
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
