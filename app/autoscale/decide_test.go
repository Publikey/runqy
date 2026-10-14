package autoscale

import (
	"testing"
	"time"

	queueworker "github.com/Publikey/runqy/queues"
)

var t0 = time.Date(2026, 10, 14, 12, 0, 10, 0, time.UTC)

func ready(id string, idleFor time.Duration) instanceView {
	return instanceView{inst: &Instance{InstanceID: id, Status: StatusRunning}, lastActivity: t0.Add(-idleFor)}
}

func booting(id string) instanceView {
	return instanceView{inst: &Instance{InstanceID: id, Status: StatusProvisioning}, booting: true, lastActivity: t0}
}

func cfg(min, max int, up, down []queueworker.ScaleTrigger) *queueworker.AutoscaleConfig {
	return &queueworker.AutoscaleConfig{Enabled: true, Provider: "p", MinWorkers: min, MaxWorkers: max, ScaleUp: up, ScaleDown: down}
}

var (
	noWorkers = queueworker.ScaleTrigger{Trigger: queueworker.TriggerNoWorkers}
	depth10   = queueworker.ScaleTrigger{Trigger: queueworker.TriggerQueueDepth, Threshold: 10}
	latency1m = queueworker.ScaleTrigger{Trigger: queueworker.TriggerLatency, MaxLatency: "1m"}
	idle5m    = queueworker.ScaleTrigger{Trigger: queueworker.TriggerIdle, Timeout: "5m"}
)

func snapshot(pending, workers int, insts ...instanceView) queueSnapshot {
	return queueSnapshot{now: t0, window: 30 * time.Second, pending: pending, totalWorkers: workers, instances: insts}
}

func ids(insts []*Instance) []string {
	var out []string
	for _, i := range insts {
		out = append(out, i.InstanceID)
	}
	return out
}

func TestDecideScaleUp(t *testing.T) {
	tests := []struct {
		name string
		asc  *queueworker.AutoscaleConfig
		snap queueSnapshot
		want int
	}{
		{"no workers + pending -> 1", cfg(0, 3, []queueworker.ScaleTrigger{noWorkers}, nil), snapshot(1, 0), 1},
		{"no workers but nothing pending", cfg(0, 3, []queueworker.ScaleTrigger{noWorkers}, nil), snapshot(0, 0), 0},
		{"no workers, one already booting", cfg(0, 3, []queueworker.ScaleTrigger{noWorkers}, nil), snapshot(5, 0, booting("a")), 0},
		{"manual worker serves queue", cfg(0, 3, []queueworker.ScaleTrigger{noWorkers}, nil), snapshot(5, 1), 0},
		{"depth over threshold adds one", cfg(0, 3, []queueworker.ScaleTrigger{depth10}, nil), snapshot(11, 1, ready("a", 0)), 1},
		{"depth holds while one is booting", cfg(0, 3, []queueworker.ScaleTrigger{depth10}, nil), snapshot(50, 1, ready("a", 0), booting("b")), 0},
		{"depth capped at max", cfg(0, 2, []queueworker.ScaleTrigger{depth10}, nil), snapshot(50, 2, ready("a", 0), ready("b", 0)), 0},
		{"min workers enforced", cfg(2, 3, nil, nil), snapshot(0, 0), 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decide(tt.asc, tt.snap).provision; got != tt.want {
				t.Errorf("provision = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestDecideLatency(t *testing.T) {
	asc := cfg(0, 3, []queueworker.ScaleTrigger{latency1m}, nil)
	snap := snapshot(3, 1, ready("a", 0))
	snap.latency = 30 * time.Second
	if got := decide(asc, snap).provision; got != 0 {
		t.Errorf("latency under threshold: provision = %d, want 0", got)
	}
	snap.latency = 2 * time.Minute
	if got := decide(asc, snap).provision; got != 1 {
		t.Errorf("latency over threshold: provision = %d, want 1", got)
	}
}

func TestDecideScheduleHold(t *testing.T) {
	// Fires daily at 12:00; t0 is 12:00:10.
	prewarm := queueworker.ScaleTrigger{Trigger: queueworker.TriggerSchedule, Cron: "0 12 * * *", Workers: 2, Duration: "2h"}
	asc := cfg(0, 4, []queueworker.ScaleTrigger{prewarm}, []queueworker.ScaleTrigger{idle5m})

	if got := decide(asc, snapshot(0, 0)).provision; got != 2 {
		t.Fatalf("pre-warm: provision = %d, want 2", got)
	}

	// 90 minutes later, still inside the 2h hold: idle instances must not be removed.
	snap := snapshot(0, 2, ready("a", time.Hour), ready("b", time.Hour))
	snap.now = t0.Add(90 * time.Minute)
	if got := decide(asc, snap).destroy; len(got) != 0 {
		t.Errorf("inside hold: destroyed %v, want none", ids(got))
	}

	// After the hold, the idle trigger applies again.
	snap.now = t0.Add(3 * time.Hour)
	if got := decide(asc, snap).destroy; len(got) != 2 {
		t.Errorf("after hold: destroyed %v, want both", ids(got))
	}
}

func TestDecideScaleDown(t *testing.T) {
	asc := cfg(1, 5, nil, []queueworker.ScaleTrigger{idle5m})

	busy := ready("busy", time.Hour)
	busy.busy = true
	protected := ready("protected", time.Hour)
	protected.inst.Protected = true

	plan := decide(asc, snapshot(0, 6,
		ready("fresh", time.Minute), // recently active
		ready("old", 2*time.Hour),   // idle the longest
		ready("idle", 10*time.Minute),
		busy, protected, booting("boot"),
	))
	// 6 live, floor 1 -> up to 5 removable, but only "old" and "idle" qualify.
	got := ids(plan.destroy)
	if len(got) != 2 || got[0] != "old" || got[1] != "idle" {
		t.Errorf("destroy = %v, want [old idle] (oldest first)", got)
	}
}

func TestDecideScaleDownRespectsFloor(t *testing.T) {
	asc := cfg(2, 5, nil, []queueworker.ScaleTrigger{idle5m})
	plan := decide(asc, snapshot(0, 3, ready("a", time.Hour), ready("b", 2*time.Hour), ready("c", 3*time.Hour)))
	if got := ids(plan.destroy); len(got) != 1 || got[0] != "c" {
		t.Errorf("destroy = %v, want [c]", got)
	}
}

func TestDecideForcedScheduleScaleDown(t *testing.T) {
	night := queueworker.ScaleTrigger{Trigger: queueworker.TriggerSchedule, Cron: "0 12 * * *"}
	asc := cfg(0, 5, nil, []queueworker.ScaleTrigger{night})
	busy := ready("busy", 0)
	busy.busy = true
	plan := decide(asc, snapshot(0, 2, ready("a", 0), busy))
	// Forced scale-down ignores idleness but still never kills a busy worker.
	if got := ids(plan.destroy); len(got) != 1 || got[0] != "a" {
		t.Errorf("destroy = %v, want [a]", got)
	}
}

func TestDecideNoScaleDownWithoutTrigger(t *testing.T) {
	asc := cfg(0, 5, []queueworker.ScaleTrigger{noWorkers}, nil)
	if plan := decide(asc, snapshot(0, 1, ready("a", 24*time.Hour))); len(plan.destroy) != 0 {
		t.Errorf("destroy = %v, want none (no scale_down trigger)", ids(plan.destroy))
	}
}
