package provider

import (
	"context"
	"encoding/json"
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
			_ = json.Unmarshal(b, &f.search)
			w.Write([]byte(`{"offers":[{"id":98765,"dph_total":0.31,"gpu_name":"RTX_4090"},{"id":11111,"dph_total":0.45,"gpu_name":"RTX_4090"}]}`))
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
