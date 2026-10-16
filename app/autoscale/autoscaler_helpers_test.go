package autoscale

import (
	"context"
	"testing"
	"time"

	"github.com/Publikey/runqy/autoscale/provider"
	"github.com/Publikey/runqy/third_party/asynq"
)

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

func TestDisplayStatus(t *testing.T) {
	cases := map[[2]string]string{
		{StatusRunning, ""}:       "booting",
		{StatusRunning, "host:1"}: StatusRunning,
		{StatusProvisioning, ""}:  StatusProvisioning,
		{StatusTerminated, ""}:    StatusTerminated,
		{StatusFailed, "host:1"}:  StatusFailed,
	}
	for in, want := range cases {
		if got := DisplayStatus(in[0], in[1]); got != want {
			t.Errorf("DisplayStatus(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestAggregateQueueStatsIgnoresPausedPending(t *testing.T) {
	got := aggregateQueueStats([]*asynq.QueueInfo{
		{Queue: "inference.high", Pending: 3, Active: 1, Latency: time.Minute},
		{Queue: "inference.low", Pending: 50, Active: 2, Latency: time.Hour, Paused: true},
	})
	if got.pending != 3 || got.latency != time.Minute {
		t.Errorf("paused sub-queue counted as load: %+v", got)
	}
	if got.active != 3 {
		t.Errorf("active = %d, want 3 (machines busy on a paused sub-queue still count)", got.active)
	}
	if all := aggregateQueueStats([]*asynq.QueueInfo{{Pending: 25, Paused: true}}); all.pending != 0 {
		t.Errorf("fully paused queue: pending = %d, want 0 (no scale-up)", all.pending)
	}
}
