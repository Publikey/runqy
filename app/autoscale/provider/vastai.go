package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func init() {
	Register("vastai", newVastAIProvider)
}

const vastDefaultBaseURL = "https://console.vast.ai/api/v0"

// vastConfig is the decrypted provider config (stored in autoscale_providers.config).
type vastConfig struct {
	APIKey   string  `json:"api_key"`
	GPUName  string  `json:"gpu_name"`           // e.g. "RTX 4090" (or "RTX_4090"); overrides spec.GPU
	MaxPrice float64 `json:"max_price_per_hour"` // overrides spec.MaxPricePerHour when > 0
	DiskGB   int     `json:"disk_gb"`            // fallback when spec.DiskGB is 0
	OnStart  string  `json:"onstart"`            // optional onstart script contents
	BaseURL  string  `json:"base_url"`           // optional override (tests)

	// Offer filters. Machines must download the worker image (several GB) from ghcr.io
	// and clone the task code from GitHub: both are slow or blocked from some regions.
	Countries        []string `json:"countries,omitempty"`         // allow-list of ISO country codes (empty = any)
	ExcludeCountries []string `json:"exclude_countries,omitempty"` // default ["CN"]; set [] to allow every country
	MinDownloadMbps  float64  `json:"min_download_mbps,omitempty"` // default 100
	// VerifiedOnly rents only machines verified by Vast (default true). Unverified
	// hosts were unreliable in testing: blocked registry, Docker errors, GPU not attachable.
	VerifiedOnly *bool `json:"verified_only,omitempty"`
	// MinCUDAVersion keeps hosts whose driver supports at least this CUDA version
	// (default "12.8", required by the inference image). "none" disables the filter.
	MinCUDAVersion string `json:"min_cuda_version,omitempty"`
}

// Defaults for the offer filters when the config leaves them unset.
var (
	vastDefaultExcludeCountries = []string{"CN"}
	vastDefaultMinDownloadMbps  = 100.0
)

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

	// Hosts that failed to start our container are avoided for a while: the cheapest
	// offer is often the same broken machine (full disk, unreachable registry...).
	mu          sync.Mutex
	placed      map[string]string    // instance id -> machine id
	started     map[string]bool      // instance id -> its container reached "running"
	badMachines map[string]time.Time // machine id -> excluded until
	state       StateStore           // optional: persists the three maps above
}

// vastState is what the provider persists through its StateStore.
type vastState struct {
	Placed      map[string]string    `json:"placed,omitempty"`
	Started     map[string]bool      `json:"started,omitempty"`
	BadMachines map[string]time.Time `json:"bad_machines,omitempty"`
}

// SetStateStore restores the saved placements and excluded machines, then keeps them
// saved: without it a restart would forget broken hosts and rent the same one again.
func (p *vastAIProvider) SetStateStore(ctx context.Context, s StateStore) {
	var st vastState
	ok, err := s.Load(ctx, &st)
	if err != nil {
		log.Printf("[AUTOSCALE] vastai: cannot load saved state: %v", err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state = s
	if !ok {
		return
	}
	for id, m := range st.Placed {
		if _, known := p.placed[id]; !known {
			p.placed[id] = m
		}
	}
	for id, v := range st.Started {
		if v {
			p.started[id] = true
		}
	}
	now := time.Now()
	for m, until := range st.BadMachines {
		if until.After(now) && until.After(p.badMachines[m]) {
			p.badMachines[m] = until
		}
	}
}

// saveState persists the current maps (best effort: a failure only costs the memory
// of bad hosts across a restart).
func (p *vastAIProvider) saveState(ctx context.Context) {
	p.mu.Lock()
	s := p.state
	st := vastState{Placed: map[string]string{}, Started: map[string]bool{}, BadMachines: map[string]time.Time{}}
	for k, v := range p.placed {
		st.Placed[k] = v
	}
	for k, v := range p.started {
		st.Started[k] = v
	}
	now := time.Now()
	for k, v := range p.badMachines {
		if v.After(now) {
			st.BadMachines[k] = v
		}
	}
	p.mu.Unlock()
	if s == nil {
		return
	}
	if err := s.Save(ctx, st); err != nil {
		log.Printf("[AUTOSCALE] vastai: cannot save state: %v", err)
	}
}

// badMachineTTL is how long a machine whose container never started is excluded.
const badMachineTTL = 24 * time.Hour

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
	return &vastAIProvider{
		cfg:         cfg,
		client:      &http.Client{Timeout: 30 * time.Second},
		placed:      map[string]string{},
		started:     map[string]bool{},
		badMachines: map[string]time.Time{},
	}, nil
}

func (p *vastAIProvider) Name() string { return "vastai" }

// Provision searches the marketplace for the cheapest matching offer and rents it.
func (p *vastAIProvider) Provision(ctx context.Context, spec InstanceSpec) (string, error) {
	gpuName := p.cfg.GPUName
	if gpuName == "" {
		gpuName = spec.GPU
	}
	// The API matches Vast's real GPU names, with spaces ("RTX 3060"). Vast's CLI and
	// docs write underscores ("RTX_3060") and convert them client-side; accept both.
	gpuName = strings.TrimSpace(strings.ReplaceAll(gpuName, "_", " "))
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

	offerID, machineID, err := p.cheapestOffer(ctx, gpuName, maxPrice, disk, spec.MinRAMGB, spec.MinVCPU)
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
	p.mu.Lock()
	p.placed[resp.NewContract.String()] = machineID
	p.mu.Unlock()
	p.saveState(ctx)
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
func (p *vastAIProvider) cheapestOffer(ctx context.Context, gpuName string, maxPrice float64, disk, minRAMGB, minVCPU int) (offerID, machineID string, err error) {
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
	// The API takes cpu_ram in MB (Vast's CLI multiplies GB by 1000) and counts the
	// vCPUs the rental gets as cpu_cores_effective.
	if minRAMGB > 0 {
		q["cpu_ram"] = map[string]interface{}{"gte": minRAMGB * 1000}
	}
	if minVCPU > 0 {
		q["cpu_cores_effective"] = map[string]interface{}{"gte": minVCPU}
	}
	if len(p.cfg.Countries) > 0 {
		q["geolocation"] = map[string]interface{}{"in": p.cfg.Countries}
	} else if exclude := p.excludeCountries(); len(exclude) > 0 {
		q["geolocation"] = map[string]interface{}{"notin": exclude}
	}
	minDown := p.cfg.MinDownloadMbps
	if minDown <= 0 {
		minDown = vastDefaultMinDownloadMbps
	}
	q["inet_down"] = map[string]interface{}{"gte": minDown}
	if minCUDA := p.cfg.MinCUDAVersion; minCUDA != "none" {
		if minCUDA == "" {
			minCUDA = defaultMinCUDAVersion
		}
		if v, err := strconv.ParseFloat(minCUDA, 64); err == nil {
			q["cuda_max_good"] = map[string]interface{}{"gte": v}
		}
	}
	if p.cfg.VerifiedOnly == nil || *p.cfg.VerifiedOnly {
		q["verified"] = map[string]interface{}{"eq": true}
	}
	if bad := p.excludedMachines(time.Now()); len(bad) > 0 {
		q["machine_id"] = map[string]interface{}{"notin": bad}
	}

	var resp struct {
		Offers []struct {
			ID        json.Number `json:"id"`
			MachineID json.Number `json:"machine_id"`
			DPHTotal  float64     `json:"dph_total"`
		} `json:"offers"`
	}
	if err := p.do(ctx, http.MethodPost, "/bundles/", q, &resp); err != nil {
		return "", "", err
	}
	if len(resp.Offers) == 0 {
		return "", "", fmt.Errorf("vastai: no offers match (gpu=%q max_price=%.2f disk=%d min_ram_gb=%d min_vcpu=%d)", gpuName, maxPrice, disk, minRAMGB, minVCPU)
	}
	return resp.Offers[0].ID.String(), resp.Offers[0].MachineID.String(), nil
}

// excludedMachines lists machines still within their exclusion window (and forgets
// expired ones).
func (p *vastAIProvider) excludedMachines(now time.Time) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for id, until := range p.badMachines {
		if now.After(until) {
			delete(p.badMachines, id)
			continue
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// observe records what an instance did: whether its container ever started, and, when
// it ends without ever starting, that its machine is bad (host fault: full disk,
// registry unreachable...). A container that started but whose worker never became
// ready points at the image or the task code, not the host, so it is not penalised.
// It reports whether the instance ended as a host fault.
func (p *vastAIProvider) observe(ctx context.Context, instanceID string, running, ended bool) (hostFault bool) {
	changed := false
	defer func() {
		if changed {
			p.saveState(ctx)
		}
	}()
	p.mu.Lock()
	defer p.mu.Unlock()
	if running && !p.started[instanceID] {
		if _, known := p.placed[instanceID]; known {
			p.started[instanceID] = true
			changed = true
		}
	}
	if !ended {
		return false
	}
	machine, known := p.placed[instanceID]
	if known && !p.started[instanceID] {
		hostFault = true
		if machine != "" {
			p.badMachines[machine] = time.Now().Add(badMachineTTL)
		}
	}
	if _, tracked := p.started[instanceID]; known || tracked {
		changed = true
	}
	delete(p.placed, instanceID)
	delete(p.started, instanceID)
	return hostFault
}

// excludeCountries returns the configured exclusion list; nil (unset) means the default,
// while an explicit empty list allows every country.
func (p *vastAIProvider) excludeCountries() []string {
	if p.cfg.ExcludeCountries == nil {
		return vastDefaultExcludeCountries
	}
	return p.cfg.ExcludeCountries
}

// vastInstance is the subset of GET /instances/{id}/ we use.
type vastInstance struct {
	ID           json.Number `json:"id"`
	Label        *string     `json:"label"`
	ActualStatus string      `json:"actual_status"`
	CurState     string      `json:"cur_state"`
	StatusMsg    *string     `json:"status_msg"`
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
		p.observe(ctx, instanceID, false, true)
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
	if err != nil && !isNotFound(err) {
		return err
	}
	p.observe(ctx, instanceID, strings.EqualFold(inst.ActualStatus, "running"), true)
	return nil
}

func (p *vastAIProvider) Status(ctx context.Context, instanceID string) (InstanceStatus, error) {
	inst, err := p.instance(ctx, instanceID)
	if err != nil {
		return InstanceStatus{}, err
	}
	if inst == nil {
		p.observe(ctx, instanceID, false, true)
		return InstanceStatus{InstanceID: instanceID, Status: StatusTerminated}, nil
	}
	status := mapVastStatus(inst.ActualStatus, inst.CurState)
	hostFault := p.observe(ctx, instanceID, strings.EqualFold(inst.ActualStatus, "running"), status == StatusTerminated)
	msg := ""
	if inst.StatusMsg != nil {
		msg = *inst.StatusMsg
	}
	return InstanceStatus{InstanceID: instanceID, Status: status, Message: msg, HostFault: hostFault}, nil
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
