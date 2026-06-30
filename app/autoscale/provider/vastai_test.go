package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestVastAILifecycle(t *testing.T) {
	var askBody, createBody map[string]interface{}
	var createdOfferPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer vast_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/asks/":
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &askBody)
			w.Write([]byte(`{"offers":[{"id":98765,"dph_total":0.31},{"id":11111,"dph_total":0.45}]}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/instances/"):
			createdOfferPath = r.URL.Path
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &createBody)
			w.Write([]byte(`{"success":true,"new_contract":55555}`))
		case r.Method == http.MethodGet && r.URL.Path == "/instances/":
			w.Write([]byte(`{"instances":[{"id":55555,"actual_status":"running","cur_state":"running"}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/instances/55555/":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	cfg, _ := json.Marshal(vastConfig{APIKey: "vast_test", BaseURL: srv.URL})
	p, err := Build("vastai", cfg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	ctx := context.Background()

	id, err := p.Provision(ctx, InstanceSpec{
		Queue: "inference", Image: "ghcr.io/publikey/runqy-worker:inference", DiskGB: 50, GPU: "RTX 4090", MaxPricePerHour: 0.5,
		Env: map[string]string{"RUNQY_SERVER_URL": "http://x", "RUNQY_INSTANCE_ID": "as-zzz"},
	})
	if err != nil || id != "55555" {
		t.Fatalf("provision id=%q err=%v", id, err)
	}
	// Cheapest offer (98765) selected.
	if createdOfferPath != "/instances/98765/" {
		t.Fatalf("created from wrong offer path: %s", createdOfferPath)
	}
	// GPU name normalized to underscores in the search query (nested under "q").
	q, _ := askBody["q"].(map[string]interface{})
	if q == nil {
		t.Fatalf("search body missing q wrapper: %v", askBody)
	}
	if gn, _ := q["gpu_name"].(map[string]interface{}); gn == nil || gn["eq"] != "RTX_4090" {
		t.Fatalf("gpu_name filter wrong: %v", q["gpu_name"])
	}
	// Env translated to docker -e flags.
	if env, ok := createBody["env"].(map[string]interface{}); !ok || env["-e RUNQY_SERVER_URL"] != "http://x" {
		t.Fatalf("env not translated to -e flags: %v", createBody["env"])
	}

	st, err := p.Status(ctx, "55555")
	if err != nil || st.Status != StatusRunning {
		t.Fatalf("status=%+v err=%v", st, err)
	}
	// Unknown instance id -> terminated.
	st, _ = p.Status(ctx, "404040")
	if st.Status != StatusTerminated {
		t.Fatalf("unknown instance status = %q want terminated", st.Status)
	}

	if err := p.Destroy(ctx, "55555"); err != nil {
		t.Fatalf("destroy: %v", err)
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

func TestMapVastStatus(t *testing.T) {
	cases := []struct{ actual, cur, want string }{
		{"running", "", StatusRunning},
		{"exited", "", StatusTerminated},
		{"offline", "", StatusTerminated},
		{"", "running", StatusRunning},
		{"", "stopped", StatusTerminated},
		{"loading", "", StatusProvisioning},
		{"", "", StatusProvisioning},
	}
	for _, c := range cases {
		if got := mapVastStatus(c.actual, c.cur); got != c.want {
			t.Errorf("mapVastStatus(%q,%q)=%q want %q", c.actual, c.cur, got, c.want)
		}
	}
}
