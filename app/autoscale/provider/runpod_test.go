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

func TestRunPodFactoryValidation(t *testing.T) {
	if _, err := Build("runpod", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error when api_key missing")
	}
	p, err := Build("runpod", json.RawMessage(`{"api_key":"rpa_test"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "runpod" {
		t.Fatalf("name = %q want runpod", p.Name())
	}
}

func TestRunPodLifecycle(t *testing.T) {
	var gotAuth, gotMethod, gotPath string
	var createBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotMethod, gotPath = r.Method, r.URL.Path
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/pods":
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &createBody)
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":"pod-123","desiredStatus":"RUNNING"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/pods/pod-123":
			w.Write([]byte(`{"id":"pod-123","desiredStatus":"RUNNING"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/pods/gone":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"not found"}`))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	cfg, _ := json.Marshal(runpodConfig{APIKey: "rpa_test", GPUTypeID: "NVIDIA GeForce RTX 4090", BaseURL: srv.URL})
	p, err := Build("runpod", cfg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	ctx := context.Background()

	// Provision
	id, err := p.Provision(ctx, InstanceSpec{
		Queue: "inference", Image: "ghcr.io/publikey/runqy-worker:inference", DiskGB: 50,
		Env: map[string]string{"RUNQY_SERVER_URL": "http://x", "RUNQY_INSTANCE_ID": "as-abcdef123456"},
	})
	if err != nil || id != "pod-123" {
		t.Fatalf("provision id=%q err=%v", id, err)
	}
	if gotAuth != "Bearer rpa_test" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if createBody["imageName"] != "ghcr.io/publikey/runqy-worker:inference" {
		t.Fatalf("imageName not sent: %v", createBody["imageName"])
	}
	if env, ok := createBody["env"].(map[string]interface{}); !ok || env["RUNQY_SERVER_URL"] != "http://x" {
		t.Fatalf("env not sent as object map: %v", createBody["env"])
	}
	if gpus, ok := createBody["gpuTypeIds"].([]interface{}); !ok || len(gpus) != 1 || gpus[0] != "NVIDIA GeForce RTX 4090" {
		t.Fatalf("gpuTypeIds not sent: %v", createBody["gpuTypeIds"])
	}

	// Status (running)
	st, err := p.Status(ctx, "pod-123")
	if err != nil || st.Status != StatusRunning {
		t.Fatalf("status=%+v err=%v", st, err)
	}

	// Status of missing pod -> terminated (not error)
	st, err = p.Status(ctx, "gone")
	if err != nil || st.Status != StatusTerminated {
		t.Fatalf("missing pod status=%+v err=%v", st, err)
	}

	// Destroy
	if err := p.Destroy(ctx, "pod-123"); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if gotMethod != http.MethodDelete || !strings.HasPrefix(gotPath, "/pods/") {
		t.Fatalf("destroy hit %s %s", gotMethod, gotPath)
	}
}

func TestMapRunPodStatus(t *testing.T) {
	cases := map[string]string{"RUNNING": StatusRunning, "EXITED": StatusTerminated, "TERMINATED": StatusTerminated, "": StatusProvisioning, "CREATED": StatusProvisioning}
	for in, want := range cases {
		if got := mapRunPodStatus(in); got != want {
			t.Errorf("mapRunPodStatus(%q)=%q want %q", in, got, want)
		}
	}
}
