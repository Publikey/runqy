package autoscale

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/Publikey/runqy/autoscale/provider"
	"github.com/Publikey/runqy/config"
	"github.com/Publikey/runqy/control"
	"github.com/Publikey/runqy/health"
	"github.com/Publikey/runqy/leader"
	queueworker "github.com/Publikey/runqy/queues"
	"github.com/Publikey/runqy/third_party/asynq"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// baseTick is how often the control loop wakes; each queue is only evaluated once its own
// poll_interval has elapsed since its last evaluation.
const baseTick = 10 * time.Second

// queueEvalTimeout bounds one queue evaluation (provider HTTP calls use 30s timeouts each).
const queueEvalTimeout = 2 * time.Minute

// workerStaleThreshold matches the server/worker heartbeat staleness window.
const workerStaleThreshold = 30 // seconds

// workerLostTimeout is how long a correlated worker may stop heartbeating before its
// instance is considered dead and torn down (the machine would otherwise bill forever).
const workerLostTimeout = 5 * time.Minute

// leaderTTL outlives the longest queue evaluation, so the lock (renewed before each
// queue) cannot lapse mid-evaluation and let a second server act on the same queue.
// It is also how long a crashed leader takes to be replaced.
const leaderTTL = queueEvalTimeout + time.Minute

// maxProvisionBackoff caps the wait between failed provisioning attempts.
const maxProvisionBackoff = 10 * time.Minute

// maxBootBackoff caps the wait after instances that never became ready. Each such
// instance was billed for a whole boot_timeout, so retries slow down much more.
const maxBootBackoff = time.Hour

// workerStatusBootstrapping is the heartbeat status a worker reports from registration
// until its supervised process signals ready (see runqy-worker heartbeat).
const workerStatusBootstrapping = "bootstrapping"

// Statuses of a worker drained through the control channel.
const (
	workerStatusDraining = "draining" // not taking tasks, some still in flight
	workerStatusDrained  = "drained"  // not taking tasks, nothing in flight
)

// Autoscaler is the background control loop that scales GPU workers per queue.
type Autoscaler struct {
	instances *Store
	providers *provider.Store
	queues    *queueworker.Store
	inspector *asynq.Inspector
	rdb       *redis.Client
	cfg       *config.Config
	leader    *leader.Lock

	provCacheMu sync.Mutex
	provCache   map[string]cachedProvider // by provider name

	// Loop-goroutine state (not shared).
	lastEval   map[string]time.Time         // parent queue -> last evaluation time
	backoff    map[string]*provisionBackoff // parent queue -> provisioning failure backoff
	envWarned  map[string]string            // parent queue -> last missing-prerequisite error logged
	bootFails  map[string]*provisionBackoff // parent queue -> instances that never became ready
	hostFails  map[string]*provisionBackoff // parent queue -> instances whose host could not start the container
	backoffs   backoffStore                 // persists the three backoff maps (nil: memory only)
	staleSince map[string]time.Time         // instance id -> first evaluation its worker looked dead (reset when alive)
	bootSince  map[string]bootObservation   // instance id -> since when its current worker has been bootstrapping
	drainSent  map[string]string            // instance id -> worker id asked to drain before a scale-down
	wasLeader  bool

	events EventSink // optional: failures reported to alerting

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// EventSink receives autoscaling failures (provisioning errors, instances torn down
// because they never became ready or lost their worker). alerts.EventRecorder implements it.
type EventSink interface {
	Record(ctx context.Context, queue, rule, message string) error
}

// SetEventSink registers where failures are reported. Call before Start.
func (a *Autoscaler) SetEventSink(s EventSink) { a.events = s }

// reportFailure forwards a failure to the event sink, if any. It never blocks the loop
// for long and never fails it.
func (a *Autoscaler) reportFailure(queue, message string) {
	if a.events == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.events.Record(ctx, queue, queueworker.AlertAutoscaleFailure, message); err != nil {
		log.Printf("[AUTOSCALE] cannot report failure to alerting: %v", err)
	}
}

type cachedProvider struct {
	prov      provider.Provider
	updatedAt time.Time
}

// bootObservation tracks a worker seen bootstrapping, so a worker restarting on an old
// machine gets its own boot_timeout instead of being judged on the machine's age.
type bootObservation struct {
	workerID string
	since    time.Time
}

// provisionBackoff spaces out retries after provisioning failures (no capacity, bad
// credentials, price too low...) so a broken config does not hammer the provider API.
type provisionBackoff struct {
	failures  int
	until     time.Time
	announced bool // the pause was logged (log it once, not every poll)
}

// New constructs an Autoscaler. All dependencies are required.
func New(instances *Store, providers *provider.Store, queues *queueworker.Store, inspector *asynq.Inspector, rdb *redis.Client, cfg *config.Config) *Autoscaler {
	return &Autoscaler{
		instances:  instances,
		providers:  providers,
		queues:     queues,
		inspector:  inspector,
		rdb:        rdb,
		cfg:        cfg,
		leader:     leader.New(rdb, "runqy:autoscale:leader", leaderTTL),
		provCache:  make(map[string]cachedProvider),
		lastEval:   make(map[string]time.Time),
		backoff:    make(map[string]*provisionBackoff),
		envWarned:  make(map[string]string),
		bootFails:  make(map[string]*provisionBackoff),
		hostFails:  make(map[string]*provisionBackoff),
		backoffs:   redisBackoffStore{rdb: rdb},
		staleSince: make(map[string]time.Time),
		bootSince:  make(map[string]bootObservation),
		drainSent:  make(map[string]string),
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
}

// Start launches the control loop in a goroutine.
func (a *Autoscaler) Start() {
	go a.run()
}

// Stop signals the loop to stop and waits for it to finish. Safe to call more than once.
func (a *Autoscaler) Stop() {
	a.stopOnce.Do(func() { close(a.stopCh) })
	<-a.doneCh
}

func (a *Autoscaler) run() {
	defer close(a.doneCh)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		a.leader.Release(ctx)
	}()
	ticker := time.NewTicker(baseTick)
	defer ticker.Stop()
	for {
		select {
		case <-a.stopCh:
			return
		case <-ticker.C:
			a.tick()
		}
	}
}

// tick evaluates every autoscale-enabled parent queue whose poll interval has elapsed.
// Only the server holding the leader lock acts, so several servers sharing one Redis
// never provision twice.
func (a *Autoscaler) tick() {
	if !a.holdLeadership() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), baseTick)
	parents, err := a.queues.ListParentQueues(ctx)
	if err != nil {
		cancel()
		log.Printf("[AUTOSCALE] failed to list queues: %v", err)
		return
	}
	type due struct {
		parent          string
		queue           *queueworker.Queue
		window, elapsed time.Duration
	}
	var todo []due
	now := time.Now()
	for _, parent := range parents {
		queue, err := a.queues.GetQueue(ctx, parent)
		if err != nil || queue == nil || queue.Deployment == nil || queue.Deployment.Autoscale == nil {
			continue
		}
		asc := queue.Deployment.Autoscale
		if !asc.Enabled {
			continue
		}
		interval := asc.PollIntervalOrDefault()
		elapsed := interval
		if last, ok := a.lastEval[parent]; ok {
			if now.Sub(last) < interval {
				continue
			}
			elapsed = now.Sub(last)
		}
		a.lastEval[parent] = now
		todo = append(todo, due{parent, queue, interval, elapsed})
	}
	cancel()

	for _, d := range todo {
		select {
		case <-a.stopCh:
			return
		default:
		}
		// Renew before each queue: an evaluation (< queueEvalTimeout) never outlives the lock.
		if !a.holdLeadership() {
			return
		}
		qctx, qcancel := context.WithTimeout(context.Background(), queueEvalTimeout)
		a.evaluateQueue(qctx, d.parent, d.queue, d.queue.Deployment.Autoscale, d.window, d.elapsed)
		qcancel()
	}
}

// holdLeadership takes or renews the leader lock. Observations made during an earlier
// term (another server may have acted since) are discarded when leadership is regained.
func (a *Autoscaler) holdLeadership() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	isLeader := a.leader.Acquire(ctx)
	if isLeader && !a.wasLeader {
		a.staleSince = make(map[string]time.Time)
		a.bootSince = make(map[string]bootObservation)
		a.drainSent = make(map[string]string)
		a.loadBackoffs(ctx, time.Now())
	}
	a.wasLeader = isLeader
	return isLeader
}

// evaluateQueue runs one scaling decision for a single parent queue.
func (a *Autoscaler) evaluateQueue(ctx context.Context, parent string, queue *queueworker.Queue, asc *queueworker.AutoscaleConfig, window, elapsed time.Duration) {
	// Resolve the provider config (must exist and be enabled).
	rec, err := a.providers.Get(ctx, asc.Provider)
	if err != nil {
		log.Printf("[AUTOSCALE] queue %q: provider lookup failed: %v", parent, err)
		return
	}
	if rec == nil {
		log.Printf("[AUTOSCALE] queue %q: provider %q not found — skipping", parent, asc.Provider)
		return
	}
	if !rec.Enabled {
		log.Printf("[AUTOSCALE] queue %q: provider %q is disabled — skipping", parent, asc.Provider)
		return
	}
	prov, err := a.getProvider(rec)
	if err != nil {
		log.Printf("[AUTOSCALE] queue %q: cannot build provider %q: %v", parent, asc.Provider, err)
		return
	}

	// Read worker heartbeats once per evaluation: used for correlation, live counting,
	// boot-phase detection and per-instance activity.
	workers, err := a.listWorkers(ctx)
	if err != nil {
		// Without heartbeats every instance would look dead and the queue unserved: do nothing.
		log.Printf("[AUTOSCALE] queue %q: cannot read worker heartbeats, skipping evaluation: %v", parent, err)
		return
	}
	a.reconcileWorkers(ctx, workers)
	byInstance := make(map[string]*workerHeartbeat)
	for i := range workers {
		if id := workers[i].instanceID; id != "" {
			byInstance[id] = &workers[i]
		}
	}

	managed, err := a.instances.ListByQueue(ctx, parent)
	if err != nil {
		log.Printf("[AUTOSCALE] queue %q: list instances failed: %v", parent, err)
		return
	}
	now := time.Now()
	live := a.reconcileInstances(ctx, prov, parent, managed, byInstance, asc.BootTimeoutOrDefault(), now)

	stats := a.queueStats(ctx, parent)
	a.accrueCost(ctx, live, elapsed)

	snap := queueSnapshot{
		now:          now,
		window:       window,
		pending:      stats.pending,
		active:       stats.active,
		latency:      stats.latency,
		totalWorkers: countLiveWorkers(workers, parent),
	}
	for _, inst := range live {
		snap.instances = append(snap.instances, a.viewInstance(ctx, inst, byInstance[inst.InstanceID], stats.active, now))
	}

	plan := decide(asc, snap)

	if plan.provision > 0 {
		scaleLog := fmt.Sprintf("scale-up %d -> %d (pending=%d active=%d latency=%s workers=%d)",
			len(live), plan.desired, stats.pending, stats.active, stats.latency.Round(time.Second), snap.totalWorkers)
		a.provisionN(ctx, parent, asc, prov, rec.Name, plan.provision, window, now, scaleLog)
		return
	}
	a.scaleDown(ctx, prov, parent, live, plan.destroy, byInstance)
}

// scaleDown destroys the instances the plan removes. A worker that listens on the
// control channel is drained first (it stops taking tasks) and destroyed once its
// heartbeat says "drained", so no task is picked up just before the machine goes away.
// Older workers are destroyed right away, as before. Instances the plan no longer
// removes (load came back) are resumed.
func (a *Autoscaler) scaleDown(ctx context.Context, prov provider.Provider, parent string, live, destroy []*Instance, byInstance map[string]*workerHeartbeat) {
	removing := make(map[string]bool, len(destroy))
	for _, inst := range destroy {
		removing[inst.InstanceID] = true
		hb := byInstance[inst.InstanceID]
		if hb == nil || !hb.control.Supports(control.ActionDrain) || hb.status == workerStatusDrained {
			delete(a.drainSent, inst.InstanceID)
			log.Printf("[AUTOSCALE] queue %q: scale-down destroying %s", parent, inst.InstanceID)
			a.teardown(ctx, prov, inst, StatusTerminated, "scale-down")
			continue
		}
		if hb.status == workerStatusDraining && a.drainSent[inst.InstanceID] == hb.workerID {
			continue // waiting for in-flight tasks
		}
		if err := a.sendControl(ctx, hb.workerID, control.ActionDrain); err != nil {
			log.Printf("[AUTOSCALE] queue %q: cannot drain %s, destroying it: %v", parent, inst.InstanceID, err)
			a.teardown(ctx, prov, inst, StatusTerminated, "scale-down")
			continue
		}
		if a.drainSent[inst.InstanceID] != hb.workerID {
			log.Printf("[AUTOSCALE] queue %q: scale-down draining %s (worker %s) before teardown", parent, inst.InstanceID, hb.workerID)
		}
		a.drainSent[inst.InstanceID] = hb.workerID
	}

	// Load came back: resume the instances still draining that the plan now keeps.
	for _, inst := range live {
		workerID, sent := a.drainSent[inst.InstanceID]
		if !sent || removing[inst.InstanceID] {
			continue
		}
		delete(a.drainSent, inst.InstanceID)
		if err := a.sendControl(ctx, workerID, control.ActionResume); err != nil {
			log.Printf("[AUTOSCALE] queue %q: cannot resume %s: %v", parent, inst.InstanceID, err)
			continue
		}
		log.Printf("[AUTOSCALE] queue %q: scale-down of %s cancelled, worker resumed", parent, inst.InstanceID)
	}
}

// sendControl publishes a command on a worker's control channel (no-op without Redis,
// as in unit tests).
func (a *Autoscaler) sendControl(ctx context.Context, workerID, action string) error {
	if a.rdb == nil {
		return nil
	}
	_, err := control.Publish(ctx, a.rdb, control.WorkerChannel(workerID), control.Message{Action: action})
	return err
}

// viewInstance builds the decision view of a live instance from its row and its worker's
// heartbeat, and persists the activity clock.
func (a *Autoscaler) viewInstance(ctx context.Context, inst *Instance, hb *workerHeartbeat, queueActive int, now time.Time) instanceView {
	fresh := hb != nil && now.Unix()-hb.lastBeat <= workerStaleThreshold
	ready := fresh && hb.status != workerStatusBootstrapping
	iv := instanceView{
		inst:         inst,
		booting:      !ready && (inst.Status == StatusProvisioning || inst.WorkerID == "" || (fresh && hb.status == workerStatusBootstrapping)),
		lastActivity: activityTime(inst),
	}
	switch {
	case fresh && hb.active >= 0:
		// Worker reports its own activity: idleness is measured per instance.
		iv.busy = hb.active > 0
		if hb.lastTaskAt > 0 {
			if t := time.Unix(hb.lastTaskAt, 0); t.After(iv.lastActivity) {
				iv.lastActivity = t
				_ = a.instances.MarkJobActivity(ctx, inst.InstanceID, t)
			}
		}
	case ready:
		// Older worker without activity fields: fall back to queue-level activity.
		iv.busy = queueActive > 0
	}
	if iv.busy {
		iv.lastActivity = now
		_ = a.instances.MarkJobActivity(ctx, inst.InstanceID, now)
	}
	return iv
}

// reconcileInstances syncs instance rows with the provider and with worker heartbeats,
// tearing down instances that are dead or stuck so they stop billing, and returns the
// instances still live.
//   - Teardown left unfinished (provider error, crash): retry the destroy.
//   - Provider reports failed/terminated: destroy (idempotent; frees e.g. Vast storage).
//   - No worker registered within boot_timeout of creation, or a worker bootstrapping for
//     longer than boot_timeout: destroy (bad image, crash loop...).
//   - Correlated worker dead for workerLostTimeout across evaluations: destroy.
func (a *Autoscaler) reconcileInstances(ctx context.Context, prov provider.Provider, parent string, managed []*Instance, byInstance map[string]*workerHeartbeat, bootTimeout time.Duration, now time.Time) []*Instance {
	live := make([]*Instance, 0, len(managed))
	for _, inst := range managed {
		if inst.Status == StatusDraining || inst.Status == StatusDestroying {
			a.finishTeardown(ctx, prov, inst)
			continue
		}
		if !inst.IsLive() {
			continue
		}

		if st, err := prov.Status(ctx, providerInstanceID(inst)); err == nil {
			switch st.Status {
			case provider.StatusRunning:
				if inst.Status == StatusProvisioning {
					_ = a.instances.UpdateStatus(ctx, inst.InstanceID, StatusRunning)
					inst.Status = StatusRunning
				}
			case provider.StatusFailed, provider.StatusTerminated:
				final := StatusTerminated
				reason := "provider reported " + st.Status + messageSuffix(st.Message)
				if inst.Status == StatusProvisioning {
					final = StatusFailed
					if st.HostFault {
						// The machine failed, not the image: the provider avoids that host,
						// retry soon on another one.
						reason += a.recordFailure(backoffHost, parent, hostFaultRetryBase, maxProvisionBackoff, now, "host fault")
					} else {
						reason += a.recordBootFailure(parent, bootTimeout, now)
					}
				}
				log.Printf("[AUTOSCALE] queue %q: instance %s reported %s by provider — tearing down", parent, inst.InstanceID, st.Status)
				a.teardown(ctx, prov, inst, final, reason)
				continue
			}
		}

		hb := byInstance[inst.InstanceID]
		fresh := hb != nil && now.Unix()-hb.lastBeat <= workerStaleThreshold
		// alive tolerates a beat or two of lag: only a worker silent for well over the
		// staleness window counts towards workerLostTimeout.
		alive := hb != nil && now.Unix()-hb.lastBeat <= 2*workerStaleThreshold

		// Boot clock: from instance creation until a worker registers, then from when the
		// current worker id was first seen bootstrapping (a restarted worker gets a fresh one).
		bootRef := inst.CreatedAt
		stillBooting := inst.WorkerID == ""
		if !stillBooting && fresh && hb.status == workerStatusBootstrapping {
			obs, ok := a.bootSince[inst.InstanceID]
			if !ok || obs.workerID != hb.workerID {
				obs = bootObservation{workerID: hb.workerID, since: now}
				a.bootSince[inst.InstanceID] = obs
			}
			bootRef = obs.since
			stillBooting = true
		} else if fresh {
			delete(a.bootSince, inst.InstanceID)
		}
		if !stillBooting && fresh {
			// A worker is ready on this machine: image, code and host work, stop slowing down.
			a.clearBackoff(backoffBoot, parent)
			a.clearBackoff(backoffHost, parent)
		}
		if stillBooting && now.Sub(bootRef) > bootTimeout {
			log.Printf("[AUTOSCALE] queue %q: instance %s has no ready worker after %s — tearing down", parent, inst.InstanceID, bootTimeout)
			reason := fmt.Sprintf("no ready worker within boot_timeout (%s)", bootTimeout) + a.recordBootFailure(parent, bootTimeout, now)
			a.teardown(ctx, prov, inst, StatusFailed, reason)
			continue
		}

		if alive || inst.WorkerID == "" {
			delete(a.staleSince, inst.InstanceID)
		} else if since, ok := a.staleSince[inst.InstanceID]; !ok {
			// First dead-looking observation: start the clock, decide on a later evaluation.
			a.staleSince[inst.InstanceID] = now
		} else if now.Sub(since) > workerLostTimeout {
			if inst.Protected {
				log.Printf("[AUTOSCALE] queue %q: worker of protected instance %s silent for %s — NOT destroying (protected)", parent, inst.InstanceID, now.Sub(since).Round(time.Second))
			} else {
				log.Printf("[AUTOSCALE] queue %q: worker of instance %s silent for %s — tearing down", parent, inst.InstanceID, now.Sub(since).Round(time.Second))
				a.teardown(ctx, prov, inst, StatusFailed, fmt.Sprintf("worker heartbeat lost for more than %s", workerLostTimeout))
				continue
			}
		}

		live = append(live, inst)
	}
	return live
}

// hostFaultRetryBase is the first retry delay after a host fault (the provider already
// avoids the faulty machine).
const hostFaultRetryBase = 30 * time.Second

// recordFailure counts a failure in the given backoff map (provider errors, or hosts
// that could not start the container) and pauses provisioning for base × 2^(failures-1),
// capped at max. It returns a suffix for logs, events and teardown reasons.
func (a *Autoscaler) recordFailure(kind, parent string, base, max time.Duration, now time.Time, cause string) string {
	m := a.backoffMap(kind)
	b := m[parent]
	if b == nil {
		b = &provisionBackoff{}
		m[parent] = b
	}
	b.failures++
	delay := base << uint(minInt(b.failures-1, 10))
	if delay > max || delay <= 0 {
		delay = max
	}
	b.until = now.Add(delay)
	b.announced = false
	a.persistBackoff(kind, parent)
	log.Printf("[AUTOSCALE] queue %q: provision failed (%d in a row), next attempt in %s: %s", parent, b.failures, delay, cause)
	return fmt.Sprintf(" (%d in a row, next attempt in %s): %s", b.failures, delay, cause)
}

// recordBootFailure counts an instance that never became ready and pauses provisioning
// for boot_timeout × 2^(failures-1), capped at maxBootBackoff: a broken image or a
// crashing task would otherwise rent (and pay for) a new machine every boot_timeout,
// forever. It returns a suffix for the teardown reason.
func (a *Autoscaler) recordBootFailure(parent string, bootTimeout time.Duration, now time.Time) string {
	b := a.bootFails[parent]
	if b == nil {
		b = &provisionBackoff{}
		a.bootFails[parent] = b
	}
	b.failures++
	delay := bootTimeout << uint(minInt(b.failures-1, 10))
	if delay > maxBootBackoff || delay <= 0 {
		delay = maxBootBackoff
	}
	b.until = now.Add(delay)
	b.announced = false
	a.persistBackoff(backoffBoot, parent)
	log.Printf("[AUTOSCALE] queue %q: %d instance(s) in a row never became ready, provisioning paused for %s", parent, b.failures, delay)
	return fmt.Sprintf("; %d in a row, next instance in %s", b.failures, delay)
}

// provisionN creates up to n instances, stopping at the first failure and backing off
// exponentially (poll interval × 2^failures, capped) before the next attempt.
//
// scaleLog describes the decision; it is logged only when provisioning is actually
// attempted, and pauses are logged once per pause instead of every poll.
func (a *Autoscaler) provisionN(ctx context.Context, parent string, asc *queueworker.AutoscaleConfig, prov provider.Provider, providerName string, n int, pollInterval time.Duration, now time.Time, scaleLog string) {
	if b := a.backoff[parent]; b != nil && now.Before(b.until) {
		if !b.announced {
			log.Printf("[AUTOSCALE] queue %q: provisioning paused until %s after %d failure(s)", parent, b.until.Format(time.RFC3339), b.failures)
			b.announced = true
		}
		return
	}
	if b := a.hostFails[parent]; b != nil && now.Before(b.until) {
		if !b.announced {
			log.Printf("[AUTOSCALE] queue %q: provisioning paused until %s after %d host fault(s)", parent, b.until.Format(time.RFC3339), b.failures)
			b.announced = true
		}
		return
	}
	if b := a.bootFails[parent]; b != nil && now.Before(b.until) {
		if !b.announced {
			log.Printf("[AUTOSCALE] queue %q: provisioning paused until %s after %d instance(s) that never became ready", parent, b.until.Format(time.RFC3339), b.failures)
			b.announced = true
		}
		return
	}
	if err := a.checkWorkerEnv(prov.Name()); err != nil {
		if msg := err.Error(); a.envWarned[parent] != msg {
			log.Printf("[AUTOSCALE] queue %q: cannot provision: %v", parent, err)
			a.envWarned[parent] = msg
		}
		a.reportFailure(parent, "cannot provision: "+err.Error()) // throttled by the alert evaluator
		return
	}
	delete(a.envWarned, parent)
	if scaleLog != "" {
		log.Printf("[AUTOSCALE] queue %q: %s", parent, scaleLog)
	}
	for i := 0; i < n; i++ {
		if err := a.provision(ctx, parent, asc, prov, providerName); err != nil {
			suffix := a.recordFailure(backoffProvision, parent, pollInterval, maxProvisionBackoff, now, err.Error())
			a.reportFailure(parent, "provisioning failed"+suffix)
			return
		}
		a.clearBackoff(backoffProvision, parent)
	}
}

// provision creates one instance: generate a correlation id, inject env, call the provider,
// and record the instance row.
func (a *Autoscaler) provision(ctx context.Context, parent string, asc *queueworker.AutoscaleConfig, prov provider.Provider, providerName string) error {
	correlationID := "as-" + uuid.NewString()

	spec := provider.InstanceSpec{Queue: parent, Env: a.workerEnv(parent, correlationID)}
	if asc.Instance != nil {
		spec.GPU = asc.Instance.GPU
		spec.Image = asc.Instance.Image
		spec.DiskGB = asc.Instance.DiskGB
		spec.MaxPricePerHour = asc.Instance.MaxPricePerHour
	}

	providerID, err := prov.Provision(ctx, spec)
	if err != nil {
		// Record the failure so it is visible in status.
		_ = a.instances.Create(ctx, &Instance{
			InstanceID: correlationID, Provider: providerName, Queue: parent, Status: StatusFailed,
			Metadata: mustJSON(map[string]string{"error": err.Error()}),
		})
		return err
	}

	var price float64
	if asc.Instance != nil {
		price = asc.Instance.MaxPricePerHour
	}
	meta := mustJSON(map[string]string{"provider_instance_id": providerID})
	inst := &Instance{
		InstanceID:   correlationID,
		Provider:     providerName,
		Queue:        parent,
		Status:       StatusProvisioning,
		PricePerHour: price,
		Metadata:     meta,
	}
	if err := a.instances.Create(ctx, inst); err != nil {
		// The machine exists but we can't track it: destroy it rather than leak it.
		log.Printf("[AUTOSCALE] queue %q: failed to record instance %s, destroying it: %v", parent, correlationID, err)
		if derr := prov.Destroy(ctx, providerID); derr != nil {
			log.Printf("[AUTOSCALE] queue %q: destroy of untracked instance %s (provider id %s) failed: %v", parent, correlationID, providerID, derr)
		}
		return err
	}
	return nil
}

// teardown destroys an instance at the provider and records its final status and reason.
// The intent is persisted first (status draining + final_status/reason in metadata), so a
// failed Destroy or a crash in between is retried by finishTeardown on later evaluations
// instead of leaving a machine that nothing tracks.
func (a *Autoscaler) teardown(ctx context.Context, prov provider.Provider, inst *Instance, finalStatus, reason string) {
	if finalStatus == StatusFailed {
		a.reportFailure(inst.Queue, fmt.Sprintf("instance %s destroyed: %s", inst.InstanceID, reason))
	}
	wctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.instances.UpdateStatusWithMeta(wctx, inst, StatusDraining, map[string]string{"reason": reason, "final_status": finalStatus}); err != nil {
		log.Printf("[AUTOSCALE] teardown %s: cannot record intent, not destroying yet: %v", inst.InstanceID, err)
		return
	}
	a.finishTeardown(ctx, prov, inst)
}

// finishTeardown destroys a draining instance at the provider and, on success, moves it
// to the final status recorded by teardown (terminated if none).
func (a *Autoscaler) finishTeardown(ctx context.Context, prov provider.Provider, inst *Instance) {
	providerID := providerInstanceID(inst)
	if err := prov.Destroy(ctx, providerID); err != nil {
		log.Printf("[AUTOSCALE] destroy %s (provider id %s) failed, will retry: %v", inst.InstanceID, providerID, err)
		return
	}
	final := StatusTerminated
	if s, ok := inst.MetadataMap()["final_status"].(string); ok && s != "" {
		final = s
	}
	wctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = a.instances.UpdateStatus(wctx, inst.InstanceID, final)
	inst.Status = final
	delete(a.staleSince, inst.InstanceID)
	delete(a.bootSince, inst.InstanceID)
}

// reconcileWorkers matches registered workers (by RUNQY_INSTANCE_ID echoed in their
// heartbeat) to managed instance rows, propagates self-declared protection, and keeps
// the idle clock from counting boot time as idleness.
func (a *Autoscaler) reconcileWorkers(ctx context.Context, workers []workerHeartbeat) {
	now := time.Now()
	staleCutoff := now.Unix() - workerStaleThreshold
	for _, w := range workers {
		if w.instanceID == "" {
			continue
		}
		inst, err := a.instances.GetByInstanceID(ctx, w.instanceID)
		if err != nil || inst == nil {
			continue
		}
		if inst.WorkerID != w.workerID {
			_ = a.instances.SetWorkerID(ctx, inst.InstanceID, w.workerID)
			// First correlation: start the idle clock now rather than at instance
			// creation, so time spent booting is not counted as idleness.
			if inst.LastJobAt == nil {
				_ = a.instances.MarkJobActivity(ctx, inst.InstanceID, now)
			}
		}
		if w.protected && !inst.Protected {
			_ = a.instances.SetProtected(ctx, inst.InstanceID, true)
		}
		// While the worker is still bootstrapping (fresh beat), keep sliding the idle
		// clock forward: idleness only starts counting once the worker is ready.
		if w.status == workerStatusBootstrapping && w.lastBeat >= staleCutoff {
			_ = a.instances.MarkJobActivity(ctx, inst.InstanceID, now)
		}
	}
}

// accrueCost adds price*elapsed to each live instance's accumulated cost.
func (a *Autoscaler) accrueCost(ctx context.Context, live []*Instance, elapsed time.Duration) {
	hours := elapsed.Hours()
	if hours <= 0 {
		return
	}
	for _, inst := range live {
		if inst.PricePerHour > 0 {
			_ = a.instances.AddCost(ctx, inst.InstanceID, inst.PricePerHour*hours)
		}
	}
}

// queueStatsResult aggregates a parent queue's sub-queues.
type queueStatsResult struct {
	pending, active int
	latency         time.Duration // oldest pending job across sub-queues
}

// queueStats sums pending/active and takes the max latency across all sub-queues of a
// parent queue. Pending jobs of a paused sub-queue are left out: no worker will take them,
// so they must not trigger a scale-up (renting machines for a paused queue wastes money).
// Their active jobs still count, as machines working on them are busy.
func (a *Autoscaler) queueStats(ctx context.Context, parent string) queueStatsResult {
	q, err := a.queues.GetQueueWithSubQueues(ctx, parent)
	if err != nil || q == nil {
		return queueStatsResult{}
	}
	names := []string{queueworker.NormalizeQueueName(parent)}
	if len(q.SubQueues) > 0 {
		names = names[:0]
		for _, sq := range q.SubQueues {
			names = append(names, queueworker.BuildFullQueueName(parent, sq.Name))
		}
	}
	var infos []*asynq.QueueInfo
	for _, name := range names {
		if info, err := a.inspector.GetQueueInfo(name); err == nil {
			infos = append(infos, info)
		}
	}
	return aggregateQueueStats(infos)
}

// aggregateQueueStats combines sub-queue statistics (see queueStats).
func aggregateQueueStats(infos []*asynq.QueueInfo) queueStatsResult {
	var out queueStatsResult
	for _, info := range infos {
		out.active += info.Active
		if info.Paused {
			continue
		}
		out.pending += info.Pending
		if info.Latency > out.latency {
			out.latency = info.Latency
		}
	}
	return out
}

// workerHeartbeat is the subset of worker hash fields the autoscaler needs.
type workerHeartbeat struct {
	workerID   string
	queues     string
	instanceID string
	status     string
	protected  bool
	lastBeat   int64
	active     int   // in-flight tasks; -1 when the worker does not report it (older workers)
	lastTaskAt int64 // unix seconds of last task start/finish; 0 if none or not reported
	control    control.WorkerFields
}

// listWorkers reads all worker heartbeats from Redis.
func (a *Autoscaler) listWorkers(ctx context.Context) ([]workerHeartbeat, error) {
	workers, err := health.ReadWorkers(ctx, a.rdb)
	if err != nil {
		return nil, err
	}
	out := make([]workerHeartbeat, len(workers))
	for i, w := range workers {
		out[i] = workerHeartbeat{
			workerID:   w.ID,
			queues:     w.Queues,
			instanceID: w.InstanceID,
			status:     w.Status,
			protected:  w.Protected,
			lastBeat:   w.LastBeat,
			active:     w.Active,
			lastTaskAt: w.LastTaskAt,
			control:    w.Control,
		}
	}
	return out, nil
}

// countLiveWorkers counts non-stale workers (managed or manual) serving the parent queue.
func countLiveWorkers(workers []workerHeartbeat, parent string) int {
	now := time.Now().Unix()
	count := 0
	for _, w := range workers {
		if now-w.lastBeat > workerStaleThreshold {
			continue
		}
		if queueworker.WorkerServesParent(w.queues, parent) {
			count++
		}
	}
	return count
}

// getProvider returns a cached provider for a config record, rebuilding when the record changed.
func (a *Autoscaler) getProvider(rec *provider.Record) (provider.Provider, error) {
	a.provCacheMu.Lock()
	defer a.provCacheMu.Unlock()
	if c, ok := a.provCache[rec.Name]; ok && c.updatedAt.Equal(rec.UpdatedAt) {
		return c.prov, nil
	}
	p, err := provider.Build(rec.ProviderType, rec.Config)
	if err != nil {
		return nil, err
	}
	// Restore the provider's runtime state (e.g. Vast.ai hosts to avoid): it must
	// outlive a restart, a new leader and this rebuild after a config update.
	if sp, ok := p.(provider.Stateful); ok && a.rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		sp.SetStateStore(ctx, newProviderState(a.rdb, rec.Name))
		cancel()
	}
	a.provCache[rec.Name] = cachedProvider{prov: p, updatedAt: rec.UpdatedAt}
	return p, nil
}

// checkWorkerEnv verifies a remote machine could actually join this server. Real
// providers boot on third-party hosts, so they need a reachable public URL and a
// worker-scoped API key: the admin key must never be shipped to a rented machine.
// The mock provider boots nothing and is exempt.
func (a *Autoscaler) checkWorkerEnv(providerType string) error {
	if providerType == "mock" {
		return nil
	}
	if os.Getenv("RUNQY_PUBLIC_URL") == "" {
		return fmt.Errorf("RUNQY_PUBLIC_URL is not set: remote workers could not reach this server")
	}
	if a.cfg.WorkerAPIKey == "" {
		return fmt.Errorf("RUNQY_WORKER_API_KEY is not set: refusing to send the admin API key to a remote machine")
	}
	return nil
}

// workerEnv builds the env injected into a booting worker so it auto-registers and correlates.
func (a *Autoscaler) workerEnv(parent, correlationID string) map[string]string {
	serverURL := os.Getenv("RUNQY_PUBLIC_URL")
	if serverURL == "" {
		serverURL = "http://localhost:" + a.cfg.HTTPPort
	}
	apiKey := a.cfg.WorkerAPIKey
	if apiKey == "" {
		apiKey = a.cfg.APIKey // mock provider only (see checkWorkerEnv)
	}
	return map[string]string{
		"RUNQY_SERVER_URL":  serverURL,
		"RUNQY_API_KEY":     apiKey,
		"RUNQY_QUEUES":      parent,
		"RUNQY_INSTANCE_ID": correlationID,
	}
}

// --- helpers ---

func activityTime(inst *Instance) time.Time {
	if inst.LastJobAt != nil {
		return *inst.LastJobAt
	}
	return inst.CreatedAt
}

func messageSuffix(msg string) string {
	if msg == "" {
		return ""
	}
	return ": " + msg
}

func providerInstanceID(inst *Instance) string {
	if id := providerIDFromMeta(inst.Metadata); id != "" {
		return id
	}
	return inst.InstanceID
}

func providerIDFromMeta(meta []byte) string {
	if len(meta) == 0 {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal(meta, &m); err != nil {
		return ""
	}
	id, _ := m["provider_instance_id"].(string)
	return id
}

func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
