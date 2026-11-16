package autoscale

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/Publikey/runqy/autoscale/provider"
	queueworker "github.com/Publikey/runqy/queues"
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

func TestFillDerivedExposesTeardownReason(t *testing.T) {
	torn := &Instance{Metadata: mustJSON(map[string]string{
		"provider_instance_id": "123456", "gpu": "RTX 4090",
		"reason": "no ready worker within boot_timeout (30m0s)", "final_status": "failed",
	})}
	torn.fillDerived()
	if torn.ProviderInstanceID != "123456" || torn.GPU != "RTX 4090" || torn.Reason != "no ready worker within boot_timeout (30m0s)" || torn.Error != "" {
		t.Errorf("fillDerived = %+v", torn)
	}

	notRented := &Instance{Metadata: mustJSON(map[string]string{"error": "no offer matches"})}
	notRented.fillDerived()
	if notRented.Error != "no offer matches" || notRented.ProviderInstanceID != "" {
		t.Errorf("fillDerived (provision error) = %+v", notRented)
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
	rented, err := p.Provision(ctx, provider.InstanceSpec{Queue: "inference"})
	id := rented.ID
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
	if got.bySub["high"].pending != 3 || got.bySub["low"].pending != 0 {
		t.Errorf("per sub-queue load = %+v, want high=3 and paused low=0", got.bySub)
	}
	if got.active != 3 {
		t.Errorf("active = %d, want 3 (machines busy on a paused sub-queue still count)", got.active)
	}
	if all := aggregateQueueStats([]*asynq.QueueInfo{{Pending: 25, Paused: true}}); all.pending != 0 {
		t.Errorf("fully paused queue: pending = %d, want 0 (no scale-up)", all.pending)
	}
}

func TestUnknownSubQueues(t *testing.T) {
	asc := &queueworker.AutoscaleConfig{ScaleUp: []queueworker.ScaleTrigger{
		{Trigger: queueworker.TriggerQueueDepth, Threshold: 1, SubQueues: []string{"premium", "vip"}},
		{Trigger: queueworker.TriggerLatency, MaxLatency: "1m", SubQueues: []string{"vip"}},
	}}
	bySub := map[string]subQueueLoad{"premium": {}, "standard": {}}
	if got := unknownSubQueues(asc, bySub); fmt.Sprint(got) != "[vip]" {
		t.Errorf("unknownSubQueues = %v, want [vip]", got)
	}
	if got := unknownSubQueues(asc, nil); got != nil {
		t.Errorf("no statistics must not report anything, got %v", got)
	}
}

// An idle-only sub-queue (priority 0) is not read for the scaling decision, and rented
// instances do not listen on it.
func TestIdleOnlySubQueuesAreInvisibleToAutoscaling(t *testing.T) {
	subs := []queueworker.SubQueue{
		{Name: "priority", Priority: 7, Enabled: true},
		{Name: "normal", Priority: 2, Enabled: true},
		{Name: "free", Priority: 0, Enabled: true},
	}
	names, idleOnly := statQueueNames("images", subs)
	if !reflect.DeepEqual(names, []string{"images.priority", "images.normal"}) || !reflect.DeepEqual(idleOnly, []string{"free"}) {
		t.Errorf("statQueueNames = %v, %v", names, idleOnly)
	}
	if got := managedQueues("images", subs); got != "images.normal,images.priority" {
		t.Errorf("managedQueues = %q, want the two paid sub-queues", got)
	}

	// without an idle-only sub-queue nothing changes: the instance listens on the parent
	if got := managedQueues("images", subs[:2]); got != "images" {
		t.Errorf("managedQueues without idle-only = %q, want the parent", got)
	}
	if names, idleOnly := statQueueNames("images", nil); !reflect.DeepEqual(names, []string{"images.default"}) || idleOnly != nil {
		t.Errorf("statQueueNames without sub-queues = %v, %v", names, idleOnly)
	}
	// a disabled paid sub-queue is not listened on
	subs[1].Enabled = false
	if got := managedQueues("images", subs); got != "images.priority" {
		t.Errorf("managedQueues with a disabled sub-queue = %q", got)
	}
}

// A full idle-only sub-queue asks for no machine: with its load left out, the decision sees
// an empty queue.
func TestDecideIgnoresIdleOnlyBacklog(t *testing.T) {
	asc := &queueworker.AutoscaleConfig{
		Enabled: true, Provider: "mock", MinWorkers: 0, MaxWorkers: 5,
		ScaleUp: []queueworker.ScaleTrigger{
			{Trigger: queueworker.TriggerNoWorkers},
			{Trigger: queueworker.TriggerQueueDepth, Threshold: 0},
			{Trigger: queueworker.TriggerLatency, MaxLatency: "1s"},
		},
	}
	// what queueStats builds when only the idle-only sub-queue is backlogged
	stats := aggregateQueueStats([]*asynq.QueueInfo{
		{Queue: "images.priority"}, {Queue: "images.normal"},
	})
	stats.bySub["free"] = subQueueLoad{}
	snap := queueSnapshot{now: time.Now(), window: 30 * time.Second, pending: stats.pending,
		active: stats.active, latency: stats.latency, subQueues: stats.bySub}
	if plan := decide(asc, snap); plan.provision != 0 {
		t.Errorf("provision = %d, want 0", plan.provision)
	}
	asc.ScaleUp = []queueworker.ScaleTrigger{{Trigger: queueworker.TriggerQueueDepth, Threshold: 0, SubQueues: []string{"free"}}}
	if plan := decide(asc, snap); plan.provision != 0 {
		t.Errorf("trigger naming the idle-only sub-queue: provision = %d, want 0", plan.provision)
	}
}
