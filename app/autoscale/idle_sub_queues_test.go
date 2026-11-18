package autoscale

import (
	"testing"

	"github.com/Publikey/runqy/health"
	queueworker "github.com/Publikey/runqy/queues"
)

// An idle trigger listing sub_queues only counts their tasks: an instance working on
// another sub-queue (typically an idle-only, priority 0 one) is not busy, so it can be
// scaled down. Workers that do not report per sub-queue count every task.
func TestIdleTriggerSubQueues(t *testing.T) {
	asc := &queueworker.AutoscaleConfig{ScaleDown: []queueworker.ScaleTrigger{
		{Trigger: queueworker.TriggerIdle, Timeout: "5m", SubQueues: []string{"paid"}},
	}}
	watch := idleWatchedQueues("images", asc)
	if len(watch) != 1 || watch[0] != "images.paid" {
		t.Fatalf("idleWatchedQueues = %v", watch)
	}
	all := &queueworker.AutoscaleConfig{ScaleDown: []queueworker.ScaleTrigger{{Trigger: queueworker.TriggerIdle, Timeout: "5m"}}}
	if got := idleWatchedQueues("images", all); got != nil {
		t.Fatalf("idle without sub_queues watches %v, want everything (nil)", got)
	}

	// busy on images.free only, last paid task at 1000
	hb := &workerHeartbeat{
		active: 1, lastTaskAt: 5000,
		byQueue: map[string]health.QueueActivity{
			"images.free": {Active: 1, LastTaskAt: 5000},
			"images.paid": {LastTaskAt: 1000},
		},
	}
	if active, last := hb.watchedActivity(watch); active != 0 || last != 1000 {
		t.Errorf("watching paid: active=%d last=%d, want 0 and 1000", active, last)
	}
	if active, last := hb.watchedActivity(nil); active != 1 || last != 5000 {
		t.Errorf("watching everything: active=%d last=%d, want 1 and 5000", active, last)
	}

	// a paid task in flight keeps the instance busy
	hb.byQueue["images.paid"] = health.QueueActivity{Active: 1, LastTaskAt: 6000}
	if active, last := hb.watchedActivity(watch); active != 1 || last != 6000 {
		t.Errorf("paid task in flight: active=%d last=%d, want 1 and 6000", active, last)
	}

	// older worker: no per sub-queue activity, every task counts
	hb.byQueue = nil
	if active, last := hb.watchedActivity(watch); active != 1 || last != 5000 {
		t.Errorf("worker without per sub-queue activity: active=%d last=%d, want 1 and 5000", active, last)
	}
}
