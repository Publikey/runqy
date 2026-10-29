package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestVastAIFactoryValidation(t *testing.T) {
	if _, err := Build("vastai", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error when api_key missing")
	}
	p, err := Build("vastai", json.RawMessage(`{"api_key":"vast_test"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "vastai" {
		t.Fatalf("name = %q want vastai", p.Name())
	}
}

// fakeVast mimics the documented Vast.ai API (docs.vast.ai/api-reference), with the
// response shapes observed on the real service:
//   - GET /instances/{id}/ -> {"instances": {...}} or {"instances": null} (HTTP 200) when gone
//   - instances not created by runqy have arbitrary labels (e.g. production pods)
type fakeVast struct {
	mu        sync.Mutex
	search    map[string]interface{}
	create    map[string]interface{}
	createURL string
	deleted   []string
	instances map[string]string // id -> label
	status    map[string]string // id -> actual_status
}

func (f *fakeVast) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer vast_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/bundles/":
			b, _ := io.ReadAll(r.Body)
			f.search = nil // keep only this request's filters
			_ = json.Unmarshal(b, &f.search)
			w.Write([]byte(`{"offers":[{"id":98765,"machine_id":54116,"dph_total":0.31,"gpu_name":"RTX 4090"},{"id":11111,"machine_id":777,"dph_total":0.45,"gpu_name":"RTX 4090"}]}`))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/asks/"):
			f.createURL = r.URL.Path
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &f.create)
			label, _ := f.create["label"].(string)
			f.instances["55555"] = label
			f.status["55555"] = "loading"
			w.Write([]byte(`{"success":true,"new_contract":55555}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/instances/"):
			id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/instances/"), "/")
			label, ok := f.instances[id]
			if !ok {
				w.Write([]byte(`{"instances": null}`))
				return
			}
			b, _ := json.Marshal(map[string]interface{}{"instances": map[string]interface{}{
				"id": json.Number(id), "label": label, "actual_status": f.status[id], "cur_state": "running",
			}})
			w.Write(b)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/instances/"):
			id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/instances/"), "/")
			f.deleted = append(f.deleted, id)
			delete(f.instances, id)
			w.Write([]byte(`{"success":true}`))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func newFakeVast(t *testing.T) (*fakeVast, Provider) {
	t.Helper()
	f := &fakeVast{instances: map[string]string{}, status: map[string]string{}}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	cfg, _ := json.Marshal(vastConfig{APIKey: "vast_test", BaseURL: srv.URL})
	p, err := Build("vastai", cfg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return f, p
}

func TestVastAILifecycle(t *testing.T) {
	f, p := newFakeVast(t)
	ctx := context.Background()

	id, err := p.Provision(ctx, InstanceSpec{
		Queue: "inference", Image: "ghcr.io/publikey/runqy-worker:inference", DiskGB: 50, GPU: "RTX 4090", MaxPricePerHour: 0.5,
		Env: map[string]string{"RUNQY_SERVER_URL": "https://x.example.com", "RUNQY_INSTANCE_ID": "as-0123456789abcdef"},
	})
	if err != nil || id != "55555" {
		t.Fatalf("provision id=%q err=%v", id, err)
	}
	// Documented search: flat filters, on-demand.
	if _, wrapped := f.search["q"]; wrapped {
		t.Errorf("search body must not be wrapped in \"q\": %v", f.search)
	}
	// Real GPU name with spaces: the live API returns no offer for "RTX_4090".
	if gn, _ := f.search["gpu_name"].(map[string]interface{}); gn == nil || gn["eq"] != "RTX 4090" {
		t.Errorf("gpu_name filter wrong: %v", f.search["gpu_name"])
	}
	if f.search["type"] != "ondemand" {
		t.Errorf("type = %v, want ondemand", f.search["type"])
	}
	// Cheapest offer rented with PUT /asks/{id}/.
	if f.createURL != "/asks/98765/" {
		t.Errorf("created from %s, want /asks/98765/", f.createURL)
	}
	if f.create["env"] != "-e RUNQY_INSTANCE_ID=as-0123456789abcdef -e RUNQY_SERVER_URL=https://x.example.com" {
		t.Errorf("env = %v, want the docker-flag string", f.create["env"])
	}
	if f.create["runtype"] != "args" || !strings.HasPrefix(f.create["label"].(string), runqyLabelPrefix) {
		t.Errorf("runtype/label wrong: %v / %v", f.create["runtype"], f.create["label"])
	}

	if st, err := p.Status(ctx, "55555"); err != nil || st.Status != StatusProvisioning {
		t.Fatalf("loading instance: %+v %v", st, err)
	}
	f.status["55555"] = "running"
	if st, _ := p.Status(ctx, "55555"); st.Status != StatusRunning {
		t.Fatalf("running instance: %+v", st)
	}
	// Gone: Vast answers 200 {"instances": null}.
	if st, err := p.Status(ctx, "404040"); err != nil || st.Status != StatusTerminated {
		t.Fatalf("missing instance: %+v %v", st, err)
	}

	if err := p.Destroy(ctx, "55555"); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if len(f.deleted) != 1 || f.deleted[0] != "55555" {
		t.Fatalf("deleted = %v", f.deleted)
	}
	// Idempotent: already gone is success, without another DELETE.
	if err := p.Destroy(ctx, "55555"); err != nil || len(f.deleted) != 1 {
		t.Fatalf("second destroy: err=%v deleted=%v", err, f.deleted)
	}
}

// A production pod on the same account (label set by hand) must never be destroyed,
// even if runqy were handed its id by mistake.
func TestVastAIDestroyRefusesForeignInstances(t *testing.T) {
	f, p := newFakeVast(t)
	f.instances["54984369"] = "anima" // like the production pods seen on the account
	f.status["54984369"] = "running"
	f.instances["777"] = "" // no label at all

	for _, id := range []string{"54984369", "777"} {
		err := p.Destroy(context.Background(), id)
		if err == nil || !strings.Contains(err.Error(), "refusing to destroy") {
			t.Errorf("destroy %s: err = %v, want a refusal", id, err)
		}
	}
	if len(f.deleted) != 0 {
		t.Fatalf("DELETE was sent for a foreign instance: %v", f.deleted)
	}
}

func TestVastAINoOffers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"offers":[]}`))
	}))
	defer srv.Close()
	cfg, _ := json.Marshal(vastConfig{APIKey: "vast_test", BaseURL: srv.URL})
	p, _ := Build("vastai", cfg)
	if _, err := p.Provision(context.Background(), InstanceSpec{GPU: "H100", Image: "img"}); err == nil {
		t.Fatal("expected error when no offers match")
	}
}

func TestVastEnvRejectsUnsafeValues(t *testing.T) {
	if _, err := vastEnv(map[string]string{"A": "has space"}); err == nil {
		t.Error("value with a space accepted")
	}
	if s, err := vastEnv(map[string]string{"B": "2", "A": "1"}); err != nil || s != "-e A=1 -e B=2" {
		t.Errorf("vastEnv = %q, %v", s, err)
	}
}

func TestMapVastStatus(t *testing.T) {
	cases := []struct{ actual, cur, want string }{
		{"running", "", StatusRunning},
		{"exited", "", StatusTerminated},
		{"offline", "", StatusTerminated},
		{"unknown", "", StatusTerminated},
		{"", "running", StatusRunning},
		{"", "stopped", StatusTerminated},
		{"loading", "", StatusProvisioning},
		{"loading", "running", StatusProvisioning}, // contract active, image still downloading
		{"created", "stopped", StatusTerminated},
		{"", "", StatusProvisioning},
	}
	for _, c := range cases {
		if got := mapVastStatus(c.actual, c.cur); got != c.want {
			t.Errorf("mapVastStatus(%q,%q)=%q want %q", c.actual, c.cur, got, c.want)
		}
	}
}

func TestVastAIGPUNameUnderscoresAccepted(t *testing.T) {
	f, p := newFakeVast(t)
	p.(*vastAIProvider).cfg.GPUName = "RTX_3060" // as written in Vast's docs and CLI
	if _, err := p.Provision(context.Background(), InstanceSpec{Queue: "q", Image: "img"}); err != nil {
		t.Fatal(err)
	}
	if gn, _ := f.search["gpu_name"].(map[string]interface{}); gn == nil || gn["eq"] != "RTX 3060" {
		t.Errorf("gpu_name sent as %v, want the real name with a space", f.search["gpu_name"])
	}
}

func TestVastAIOfferFilters(t *testing.T) {
	cases := []struct {
		name   string
		cfg    func(c *vastConfig)
		geo    map[string]interface{}
		minMbs float64
	}{
		{"defaults", func(c *vastConfig) {}, map[string]interface{}{"notin": []interface{}{"CN"}}, 100},
		{"allow-list wins", func(c *vastConfig) { c.Countries = []string{"FR", "DE"} }, map[string]interface{}{"in": []interface{}{"FR", "DE"}}, 100},
		{"explicit empty exclusion allows all", func(c *vastConfig) { c.ExcludeCountries = []string{} }, nil, 100},
		{"custom bandwidth", func(c *vastConfig) { c.MinDownloadMbps = 500 }, map[string]interface{}{"notin": []interface{}{"CN"}}, 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, p := newFakeVast(t)
			tc.cfg(&p.(*vastAIProvider).cfg)
			if _, err := p.Provision(context.Background(), InstanceSpec{Queue: "q", Image: "img"}); err != nil {
				t.Fatal(err)
			}
			geo, _ := f.search["geolocation"].(map[string]interface{})
			if fmt.Sprint(geo) != fmt.Sprint(tc.geo) {
				t.Errorf("geolocation = %v, want %v", f.search["geolocation"], tc.geo)
			}
			if in, _ := f.search["inet_down"].(map[string]interface{}); in == nil || in["gte"] != tc.minMbs {
				t.Errorf("inet_down = %v, want gte %v", f.search["inet_down"], tc.minMbs)
			}
		})
	}
}

func TestVastAIAvoidsMachinesThatNeverStarted(t *testing.T) {
	f, p := newFakeVast(t)
	ctx := context.Background()
	spec := InstanceSpec{Queue: "q", Image: "img", Env: map[string]string{"RUNQY_INSTANCE_ID": "as-1"}}

	// 1st rental lands on machine 54116, whose Docker daemon fails: the container never
	// starts and Vast reports the instance exited (seen on staging: containerd write error).
	id, err := p.Provision(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, excluded := f.search["machine_id"]; excluded {
		t.Fatalf("no machine should be excluded yet: %v", f.search["machine_id"])
	}
	f.status[id] = "exited"
	if st, _ := p.Status(ctx, id); st.Status != StatusTerminated {
		t.Fatalf("status = %q", st.Status)
	}
	if err := p.Destroy(ctx, id); err != nil {
		t.Fatal(err)
	}

	// The next search excludes that host.
	if _, err := p.Provision(ctx, spec); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(f.search["machine_id"]) != fmt.Sprint(map[string]interface{}{"notin": []interface{}{"54116"}}) {
		t.Fatalf("machine_id filter = %v, want notin [54116]", f.search["machine_id"])
	}
}

func TestVastAIDoesNotBlameHostWhenContainerStarted(t *testing.T) {
	f, p := newFakeVast(t)
	ctx := context.Background()
	spec := InstanceSpec{Queue: "q", Image: "img", Env: map[string]string{"RUNQY_INSTANCE_ID": "as-1"}}

	id, _ := p.Provision(ctx, spec)
	f.status[id] = "running" // container up; the worker never became ready (image/code issue)
	p.Status(ctx, id)
	if err := p.Destroy(ctx, id); err != nil { // boot_timeout teardown
		t.Fatal(err)
	}
	p.Provision(ctx, spec)
	if _, excluded := f.search["machine_id"]; excluded {
		t.Fatalf("host excluded although the container started: %v", f.search["machine_id"])
	}
}

func TestVastAIVerifiedOnlyByDefault(t *testing.T) {
	f, p := newFakeVast(t)
	p.Provision(context.Background(), InstanceSpec{Queue: "q", Image: "img"})
	if v, _ := f.search["verified"].(map[string]interface{}); v == nil || v["eq"] != true {
		t.Errorf("verified filter = %v, want eq true by default", f.search["verified"])
	}
	off := false
	p.(*vastAIProvider).cfg.VerifiedOnly = &off
	p.Provision(context.Background(), InstanceSpec{Queue: "q", Image: "img"})
	if _, set := f.search["verified"]; set {
		t.Errorf("verified filter still sent with verified_only=false")
	}
}

func TestVastAIMinCUDAFilter(t *testing.T) {
	f, p := newFakeVast(t)
	p.Provision(context.Background(), InstanceSpec{Queue: "q", Image: "img"})
	if c, _ := f.search["cuda_max_good"].(map[string]interface{}); c == nil || c["gte"] != 12.8 {
		t.Errorf("cuda_max_good = %v, want gte 12.8 by default", f.search["cuda_max_good"])
	}
	p.(*vastAIProvider).cfg.MinCUDAVersion = "none"
	p.Provision(context.Background(), InstanceSpec{Queue: "q", Image: "img"})
	if _, set := f.search["cuda_max_good"]; set {
		t.Error("cuda filter sent with min_cuda_version=none")
	}
}

// memState is an in-memory StateStore.
type memState struct{ data []byte }

func (m *memState) Load(_ context.Context, v any) (bool, error) {
	if m.data == nil {
		return false, nil
	}
	return true, json.Unmarshal(m.data, v)
}

func (m *memState) Save(_ context.Context, v any) error {
	var err error
	m.data, err = json.Marshal(v)
	return err
}

func TestVastAIStateSurvivesRestart(t *testing.T) {
	f, p := newFakeVast(t)
	ctx := context.Background()
	state := &memState{}
	p.(Stateful).SetStateStore(ctx, state)
	spec := InstanceSpec{Queue: "q", Image: "img", Env: map[string]string{"RUNQY_INSTANCE_ID": "as-1"}}

	// Rented before the restart; the container never starts.
	id, err := p.Provision(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}

	// After a restart, a fresh provider (same config, same saved state) still knows the
	// instance was placed on machine 54116 and never started: it is a host fault...
	cfg, _ := json.Marshal(p.(*vastAIProvider).cfg)
	p2, err := Build("vastai", cfg)
	if err != nil {
		t.Fatal(err)
	}
	p2.(Stateful).SetStateStore(ctx, state)
	f.status[id] = "exited"
	if st, _ := p2.Status(ctx, id); !st.HostFault {
		t.Fatalf("status = %+v, want a host fault", st)
	}

	// ...and a third provider (another restart) avoids that machine.
	p3, _ := Build("vastai", cfg)
	p3.(Stateful).SetStateStore(ctx, state)
	if _, err := p3.Provision(ctx, spec); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(f.search["machine_id"]) != fmt.Sprint(map[string]interface{}{"notin": []interface{}{"54116"}}) {
		t.Fatalf("machine_id filter = %v, want notin [54116]", f.search["machine_id"])
	}
}

func TestVastAIMinRAMAndVCPUFilters(t *testing.T) {
	f, p := newFakeVast(t)
	if _, err := p.Provision(context.Background(), InstanceSpec{Queue: "q", Image: "img", MinRAMGB: 32, MinVCPU: 8}); err != nil {
		t.Fatal(err)
	}
	if ram, _ := f.search["cpu_ram"].(map[string]interface{}); ram == nil || ram["gte"] != float64(32000) {
		t.Errorf("cpu_ram = %v, want gte 32000 (MB)", f.search["cpu_ram"])
	}
	if cpu, _ := f.search["cpu_cores_effective"].(map[string]interface{}); cpu == nil || cpu["gte"] != float64(8) {
		t.Errorf("cpu_cores_effective = %v, want gte 8", f.search["cpu_cores_effective"])
	}
	f2, p2 := newFakeVast(t)
	if _, err := p2.Provision(context.Background(), InstanceSpec{Queue: "q", Image: "img"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := f2.search["cpu_ram"]; ok {
		t.Error("cpu_ram filter sent when unset")
	}
	if _, ok := f2.search["cpu_cores_effective"]; ok {
		t.Error("cpu_cores_effective filter sent when unset")
	}
}
