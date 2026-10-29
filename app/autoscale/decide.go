package autoscale

import (
	"sort"
	"time"

	queueworker "github.com/Publikey/runqy/queues"
)

// queueSnapshot is everything the scaling decision needs about one parent queue, gathered
// once per evaluation. Keeping decide() free of I/O makes the policy unit-testable.
type queueSnapshot struct {
	now          time.Time
	window       time.Duration           // poll window: a cron that fired within it counts as "now"
	pending      int                     // pending jobs across sub-queues
	active       int                     // active jobs across sub-queues
	latency      time.Duration           // age of the oldest pending job across sub-queues
	subQueues    map[string]subQueueLoad // per sub-queue load, by short name
	totalWorkers int                     // live workers (managed or manual) serving the queue
	instances    []instanceView
}

// instanceView is a live managed instance plus what its worker heartbeat says.
type instanceView struct {
	inst         *Instance
	booting      bool      // provisioning at the provider, or no ready worker yet
	busy         bool      // its worker is processing a task (or, for workers that don't report it, the queue is)
	lastActivity time.Time // last time it started/finished a task (or became ready)
}

// busyGrace keeps an instance that started or finished a task this recently out of any
// scale-down: its last heartbeat may predate a task it just picked up.
const busyGrace = 30 * time.Second

// scalePlan is the outcome of one decision.
type scalePlan struct {
	provision int         // instances to create
	destroy   []*Instance // instances to scale down
	desired   int         // target live count (for logging)
}

// load returns the pending count and oldest-pending age a trigger looks at: the whole
// queue, or only the sub-queues the trigger lists. An unknown sub-queue counts as empty.
func (s queueSnapshot) load(subQueues []string) (pending int, latency time.Duration) {
	if len(subQueues) == 0 {
		return s.pending, s.latency
	}
	for _, name := range subQueues {
		l := s.subQueues[name]
		pending += l.pending
		if l.latency > latency {
			latency = l.latency
		}
	}
	return pending, latency
}

// cronFiredWithin reports whether the cron expression fired within the trailing window.
func cronFiredWithin(expr string, window time.Duration, now time.Time) bool {
	sched, err := queueworker.ParseCron(expr)
	if err != nil {
		return false
	}
	next := sched.Next(now.Add(-window))
	return !next.After(now)
}

// decide applies the queue's autoscale policy to a snapshot.
//
// Scale-up never stacks: while an instance is still booting, depth/latency triggers hold
// the current count instead of adding one per poll. Scale-down only considers instances
// that are ready, unprotected and not busy, and never goes below min_workers or an active
// schedule hold. Scale-up and scale-down never happen in the same decision.
func decide(asc *queueworker.AutoscaleConfig, snap queueSnapshot) scalePlan {
	live := len(snap.instances)
	booting := 0
	for _, iv := range snap.instances {
		if iv.booting {
			booting++
		}
	}

	// growth is the count that "one more instance" triggers aim for: never stack a new
	// instance on top of one that is still coming up.
	growth := live + 1
	if booting > 0 {
		growth = live
	}

	desired := asc.MinWorkers
	floor := asc.MinWorkers // scale-down never goes below this
	for i := range asc.ScaleUp {
		t := &asc.ScaleUp[i]
		switch t.Trigger {
		case queueworker.TriggerNoWorkers:
			if pending, _ := snap.load(t.SubQueues); pending > 0 && snap.totalWorkers == 0 {
				desired = maxInt(desired, 1)
			}
		case queueworker.TriggerQueueDepth:
			if pending, _ := snap.load(t.SubQueues); pending > t.Threshold {
				desired = maxInt(desired, growth)
			}
		case queueworker.TriggerLatency:
			pending, latency := snap.load(t.SubQueues)
			if d, err := time.ParseDuration(t.MaxLatency); err == nil && pending > 0 && latency > d {
				desired = maxInt(desired, growth)
			}
		case queueworker.TriggerSchedule:
			// A schedule holds its target for `duration` after firing (default: one poll window).
			hold := snap.window
			if d, err := time.ParseDuration(t.Duration); err == nil && d > hold {
				hold = d
			}
			if cronFiredWithin(t.Cron, hold, snap.now) {
				desired = maxInt(desired, t.Workers)
				floor = maxInt(floor, t.Workers)
			}
		}
	}
	if desired > asc.MaxWorkers {
		desired = asc.MaxWorkers
	}
	if floor > asc.MaxWorkers {
		floor = asc.MaxWorkers
	}

	if desired > live {
		return scalePlan{provision: desired - live, desired: desired}
	}

	// ---- Scale-down ----
	forceToFloor := false
	var idleTimeout time.Duration
	hasIdle := false
	for i := range asc.ScaleDown {
		t := &asc.ScaleDown[i]
		switch t.Trigger {
		case queueworker.TriggerSchedule:
			if cronFiredWithin(t.Cron, snap.window, snap.now) {
				forceToFloor = true
			}
		case queueworker.TriggerIdle:
			if d, err := time.ParseDuration(t.Timeout); err == nil {
				idleTimeout = d
				hasIdle = true
			}
		}
	}
	if forceToFloor {
		// A forced scale-down returns to min_workers, ignoring scale-up schedule holds.
		floor = asc.MinWorkers
	}

	maxRemovable := live - floor
	if maxRemovable <= 0 || (!forceToFloor && !hasIdle) {
		return scalePlan{desired: live}
	}

	// Removable: ready, unprotected and not processing a task. Busy instances are left
	// for a later poll rather than killed mid-task.
	removable := make([]instanceView, 0, live)
	for _, iv := range snap.instances {
		if iv.inst.Protected || iv.booting || iv.busy || snap.now.Sub(iv.lastActivity) < busyGrace {
			continue
		}
		if !forceToFloor && snap.now.Sub(iv.lastActivity) < idleTimeout {
			continue
		}
		removable = append(removable, iv)
	}
	sort.Slice(removable, func(i, j int) bool {
		return removable[i].lastActivity.Before(removable[j].lastActivity)
	})
	if len(removable) > maxRemovable {
		removable = removable[:maxRemovable]
	}

	plan := scalePlan{desired: live - len(removable)}
	for _, iv := range removable {
		plan.destroy = append(plan.destroy, iv.inst)
	}
	return plan
}
