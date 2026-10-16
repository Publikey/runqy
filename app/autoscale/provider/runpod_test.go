package provider

import (
	"context"
	"encoding/json"
	"fmt"
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
	var deleted []string

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
			w.Write([]byte(`{"id":"pod-123","name":"runqy-inference-abcdef123456","desiredStatus":"RUNNING"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/pods/prod-pod":
			w.Write([]byte(`{"id":"prod-pod","name":"minimax","desiredStatus":"RUNNING"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/pods/gone":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"not found"}`))
		case r.Method == http.MethodDelete:
			deleted = append(deleted, r.URL.Path)
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
	// No billed persistent volume unless configured (RunPod defaults to 20 GB).
	if v, ok := createBody["volumeInGb"]; !ok || v != float64(0) {
		t.Errorf("volumeInGb = %v (sent=%v), want an explicit 0", v, ok)
	}
	// No CUDA filter by default: RunPod's exact-version enum stops at 13.0 and would
	// exclude every recent host.
	if _, set := createBody["allowedCudaVersions"]; set {
		t.Errorf("allowedCudaVersions sent by default: %v", createBody["allowedCudaVersions"])
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

	// A pod runqy did not create is never destroyed, even with its id.
	if err := p.Destroy(ctx, "prod-pod"); err == nil || !strings.Contains(err.Error(), "refusing to destroy") {
		t.Fatalf("destroy of a foreign pod: err = %v, want a refusal", err)
	}
	if len(deleted) != 1 || deleted[0] != "/pods/pod-123" {
		t.Fatalf("DELETE calls = %v, want only /pods/pod-123", deleted)
	}
	// Already gone is success.
	if err := p.Destroy(ctx, "gone"); err != nil {
		t.Fatalf("destroy of a gone pod: %v", err)
	}
}

func TestAllowedCUDAVersions(t *testing.T) {
	if got := fmt.Sprint(allowedCUDAVersions("12.6")); got != "[12.6 12.7 12.8 12.9 13.0]" {
		t.Errorf("12.6 -> %s", got)
	}
	if allowedCUDAVersions("none") != nil || allowedCUDAVersions("") != nil {
		t.Error("unset or none should disable the filter")
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

func TestRunPodGPUTypePreferenceList(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"pod-1"}`))
	}))
	defer srv.Close()
	cfg, _ := json.Marshal(runpodConfig{APIKey: "k", BaseURL: srv.URL, GPUTypeID: "ignored",
		GPUTypeIDs: []string{"NVIDIA GeForce RTX 3070", "NVIDIA RTX A4000"}})
	p, _ := Build("runpod", cfg)
	if _, err := p.Provision(context.Background(), InstanceSpec{Queue: "q", Image: "img"}); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(body["gpuTypeIds"]); got != "[NVIDIA GeForce RTX 3070 NVIDIA RTX A4000]" {
		t.Errorf("gpuTypeIds = %s, want the preference list in order", got)
	}
}
