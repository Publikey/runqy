package queueworker

import (
	"os"
	"path/filepath"
	"testing"
)

// strict_priority is authored at the queue level and folded into the deployment JSON, which
// is what /worker/register reads.
func TestStrictPriorityFoldedIntoDeployment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.yaml")
	yml := `queues:
  video:
    strict_priority: true
    sub_queues:
      - name: priority
        priority: 6
      - name: low
        priority: 1
    deployment:
      git_url: "https://example.com/repo.git"
      branch: main
      startup_cmd: "python main.py"
  images:
    priority: 3
    deployment:
      git_url: "https://example.com/repo.git"
      branch: main
      startup_cmd: "python main.py"
`
	if err := os.WriteFile(path, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	video := cfg.Queues["video"]
	q, _ := video.ToQueueAndSubQueues("video")
	if !q.Deployment.StrictPriority {
		t.Fatal("video: strict_priority not folded into the deployment (ToQueueAndSubQueues)")
	}
	for _, c := range video.ToQueueConfigs("video") {
		if !c.Deployment.StrictPriority {
			t.Fatalf("%s: strict_priority not folded into the deployment (ToQueueConfigs)", c.Name)
		}
	}

	images := cfg.Queues["images"]
	if q, _ := images.ToQueueAndSubQueues("images"); q.Deployment.StrictPriority {
		t.Fatal("images: strict_priority must default to false (weighted)")
	}
}
