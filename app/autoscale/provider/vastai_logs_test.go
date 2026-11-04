package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// vastLogsServer serves request_logs and the result files. containerLogs is what the
// container produced (empty = it never started); the result file is "not ready" (404)
// on the first poll, as on the real storage host.
func vastLogsServer(t *testing.T, containerLogs, daemonLogs string) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	var authOnResult atomic.Bool
	var polls atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/instances/request_logs/42/":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["tail"] != "200" {
				t.Errorf("tail = %q, want 200", body["tail"])
			}
			file := "container"
			if body["daemon_logs"] == "true" {
				file = "daemon"
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"result_url": srv.URL + "/results/" + file})
		case strings.HasPrefix(r.URL.Path, "/results/"):
			if r.Header.Get("Authorization") != "" {
				authOnResult.Store(true)
			}
			if polls.Add(1) == 1 {
				http.NotFound(w, r)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/daemon") {
				w.Write([]byte(daemonLogs))
			} else {
				w.Write([]byte(containerLogs))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	return srv, &authOnResult
}

func vastForTest(t *testing.T, url string) LogFetcher {
	t.Helper()
	cfg, _ := json.Marshal(vastConfig{APIKey: "vast_test", BaseURL: url})
	p, err := Build("vastai", cfg)
	if err != nil {
		t.Fatal(err)
	}
	lf, ok := p.(LogFetcher)
	if !ok {
		t.Fatal("vastai provider does not implement LogFetcher")
	}
	return lf
}

func TestVastAILogsReturnsContainerOutput(t *testing.T) {
	srv, authOnResult := vastLogsServer(t, "pip install ...\n\n\nTraceback: CUDA out of memory\n", "daemon stuff")
	defer srv.Close()

	got, err := vastForTest(t, srv.URL).Logs(context.Background(), "42", 200)
	if err != nil {
		t.Fatal(err)
	}
	if got != "pip install ...\nTraceback: CUDA out of memory\n" {
		t.Errorf("logs = %q", got)
	}
	if authOnResult.Load() {
		t.Error("the API key was sent to the pre-signed result URL")
	}
}

func TestVastAILogsFallsBackToDaemonLogs(t *testing.T) {
	srv, _ := vastLogsServer(t, "", "Error response from daemon: pull access denied for ghcr.io/x/worker")
	defer srv.Close()

	got, err := vastForTest(t, srv.URL).Logs(context.Background(), "42", 200)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "host daemon logs") || !strings.Contains(got, "pull access denied") {
		t.Errorf("logs = %q", got)
	}
}
