package queueworker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeYAML(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "q.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const strictDeployment = `
    deployment:
      git_url: "https://example.com/repo.git"
      branch: main
      startup_cmd: "python main.py"`

// A misplaced or misspelled key must fail the load instead of being silently ignored.
func TestLoadFileRejectsUnknownKeys(t *testing.T) {
	for name, body := range map[string]string{
		"queue level":      "queues:\n  video:\n    priority: 1\n    max_retries: 3" + strictDeployment + "\n",
		"deployment level": "queues:\n  video:\n    priority: 1" + strictDeployment + "\n      active_timeout: 30m\n",
	} {
		_, err := LoadFile(writeYAML(t, body))
		if err == nil {
			t.Errorf("%s: unknown key accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("%s: error does not name the unknown field: %v", name, err)
		}
	}
}

// `limits` next to sub_queues is where people expect it: accept it as deployment.limits.
func TestLoadFileQueueLevelLimitsAlias(t *testing.T) {
	cfg, err := LoadFile(writeYAML(t, "queues:\n  video:\n    priority: 1\n    limits:\n      active_timeout: 30m"+strictDeployment+"\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	video := cfg.Queues["video"]
	q, _ := video.ToQueueAndSubQueues("video")
	if q.Deployment.Limits == nil || q.Deployment.Limits.ActiveTimeout != "30m" {
		t.Fatalf("queue-level limits not folded: %+v", q.Deployment.Limits)
	}

	_, err = LoadFile(writeYAML(t, "queues:\n  video:\n    priority: 1\n    limits:\n      active_timeout: 30m"+strictDeployment+"\n      limits:\n        max_retry: 1\n"))
	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatalf("limits at both levels: err = %v", err)
	}
}

func TestLoadFileCommentOnly(t *testing.T) {
	cfg, err := LoadFile(writeYAML(t, "# nothing here\n"))
	if err != nil || cfg != nil {
		t.Fatalf("got %v, %v", cfg, err)
	}
}

// The shipped examples must stay valid under strict decoding.
func TestExamplesLoadStrictly(t *testing.T) {
	var files []string
	for _, pattern := range []string{"../../examples/*.yaml", "../../examples/*/*.yaml", "../../examples/*/*.yml"} {
		m, _ := filepath.Glob(pattern)
		files = append(files, m...)
	}
	if len(files) == 0 {
		t.Fatal("no example files found")
	}
	for _, f := range files {
		if _, err := LoadFile(f); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}
