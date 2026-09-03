package autoscale

import (
	"context"
	"testing"
	"time"

	"github.com/Publikey/runqy/autoscale/provider"
)

func TestWorkerServesParent(t *testing.T) {
	cases := []struct {
		queues string
		parent string
		want   bool
	}{
		{"inference.high,inference.low", "inference", true},
		{"inference", "inference", true},
		{"other.default", "inference", false},
		{"", "inference", false},
		{" inference.high ", "inference", true},
		{"map[inference.default:5]", "inference", true},
		{"map[inference.high:10 inference.low:1]", "inference", true},
		{"map[other.default:5]", "inference", false},
	}
	for _, c := range cases {
		if got := workerServesParent(c.queues, c.parent); got != c.want {
			t.Errorf("workerServesParent(%q,%q)=%v want %v", c.queues, c.parent, got, c.want)
		}
	}
}

func TestCountLiveWorkers(t *testing.T) {
	now := time.Now().Unix()
	workers := []workerHeartbeat{
		{workerID: "w1", queues: "inference.high", lastBeat: now},
		{workerID: "w2", queues: "inference.low", lastBeat: now - workerStaleThreshold - 10}, // stale
		{workerID: "w3", queues: "other.default", lastBeat: now},
	}
	if got := countLiveWorkers(workers, "inference"); got != 1 {
		t.Errorf("countLiveWorkers = %d want 1", got)
	}
}

func TestBootstrappingInstances(t *testing.T) {
	now := time.Now().Unix()
	workers := []workerHeartbeat{
		{workerID: "w1", instanceID: "as-1", status: workerStatusBootstrapping, lastBeat: now},
		{workerID: "w2", instanceID: "as-2", status: "running", lastBeat: now},
		{workerID: "w3", instanceID: "as-3", status: workerStatusBootstrapping, lastBeat: now - workerStaleThreshold - 10}, // stale
		{workerID: "w4", instanceID: "", status: workerStatusBootstrapping, lastBeat: now},                                 // uncorrelated
	}
	got := bootstrappingInstances(workers)
	if !got["as-1"] {
		t.Errorf("as-1 (fresh bootstrapping) should be protected from idle-kill")
	}
	if got["as-2"] {
		t.Errorf("as-2 (running) should not be in the bootstrapping set")
	}
	if got["as-3"] {
		t.Errorf("as-3 (stale heartbeat) should not be in the bootstrapping set")
	}
	if len(got) != 1 {
		t.Errorf("bootstrappingInstances size = %d want 1", len(got))
	}
}

func TestProviderInstanceID(t *testing.T) {
	inst := &Instance{InstanceID: "as-123", Metadata: mustJSON(map[string]string{"provider_instance_id": "mock-7"})}
	if got := providerInstanceID(inst); got != "mock-7" {
		t.Errorf("providerInstanceID = %q want mock-7", got)
	}
	bare := &Instance{InstanceID: "as-456"}
	if got := providerInstanceID(bare); got != "as-456" {
		t.Errorf("providerInstanceID fallback = %q want as-456", got)
	}
}

func TestMockProviderLifecycle(t *testing.T) {
	p, err := provider.Build("mock", nil)
	if err != nil {
		t.Fatalf("build mock: %v", err)
	}
	ctx := context.Background()
	id, err := p.Provision(ctx, provider.InstanceSpec{Queue: "inference"})
	if err != nil || id == "" {
		t.Fatalf("provision: id=%q err=%v", id, err)
	}
	st, err := p.Status(ctx, id)
	if err != nil || st.Status != provider.StatusRunning {
		t.Fatalf("status: %+v err=%v", st, err)
	}
	if err := p.Destroy(ctx, id); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	st, _ = p.Status(ctx, id)
	if st.Status != provider.StatusTerminated {
		t.Fatalf("post-destroy status = %q want terminated", st.Status)
	}
}
