package autoscale

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Publikey/runqy/autoscale/provider"
	"github.com/Publikey/runqy/config"
	queueworker "github.com/Publikey/runqy/queues"
	"github.com/Publikey/runqy/third_party/asynq"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// baseTick is how often the control loop wakes; each queue is only evaluated once its own
// poll_interval has elapsed since its last evaluation.
const baseTick = 10 * time.Second

// workerStaleThreshold matches the server/worker heartbeat staleness window.
const workerStaleThreshold = 30 // seconds

// Autoscaler is the background control loop that scales GPU workers per queue.
type Autoscaler struct {
	instances *Store
	providers *provider.Store
	queues    *queueworker.Store
	inspector *asynq.Inspector
	rdb       *redis.Client
	cfg       *config.Config

	provCacheMu sync.Mutex
	provCache   map[string]cachedProvider // by provider name

	lastEval map[string]time.Time // parent queue -> last evaluation time

	stopCh chan struct{}
	doneCh chan struct{}
}

type cachedProvider struct {
	prov      provider.Provider
	updatedAt time.Time
}

// New constructs an Autoscaler. All dependencies are required.
func New(instances *Store, providers *provider.Store, queues *queueworker.Store, inspector *asynq.Inspector, rdb *redis.Client, cfg *config.Config) *Autoscaler {
	return &Autoscaler{
		instances: instances,
		providers: providers,
		queues:    queues,
		inspector: inspector,
		rdb:       rdb,
		cfg:       cfg,
		provCache: make(map[string]cachedProvider),
		lastEval:  make(map[string]time.Time),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

// Start launches the control loop in a goroutine.
func (a *Autoscaler) Start() {
	go a.run()
}

// Stop signals the loop to stop and waits for it to finish.
func (a *Autoscaler) Stop() {
	close(a.stopCh)
	<-a.doneCh
}

func (a *Autoscaler) run() {
	defer close(a.doneCh)
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
func (a *Autoscaler) tick() {
	ctx, cancel := context.WithTimeout(context.Background(), baseTick)
	defer cancel()

	parents, err := a.queues.ListParentQueues(ctx)
	if err != nil {
		log.Printf("[AUTOSCALE] failed to list queues: %v", err)
		return
	}
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
		if last, ok := a.lastEval[parent]; ok && now.Sub(last) < interval {
			continue
		}
		elapsed := interval
		if last, ok := a.lastEval[parent]; ok {
			elapsed = now.Sub(last)
		}
		a.lastEval[parent] = now
		a.evaluateQueue(ctx, parent, queue, asc, interval, elapsed)
	}
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

	// Reconcile worker correlation + protection from heartbeats, then load managed state.
	a.reconcileWorkers(ctx, parent)

	managed, err := a.instances.ListByQueue(ctx, parent)
	if err != nil {
		log.Printf("[AUTOSCALE] queue %q: list instances failed: %v", parent, err)
		return
	}
	a.reconcileInstanceStates(ctx, prov, managed)

	live := make([]*Instance, 0, len(managed))
	for _, inst := range managed {
		if inst.IsLive() {
			live = append(live, inst)
		}
	}
	liveCount := len(live)

	pending, active := a.queueDepth(ctx, parent)
	totalWorkers := a.countLiveWorkers(ctx, parent)

	// Cost accumulation + idle activity tracking.
	a.accrueCost(ctx, live, elapsed)
	if active > 0 {
		now := time.Now()
		for _, inst := range live {
			_ = a.instances.MarkJobActivity(ctx, inst.InstanceID, now)
		}
	}

	// ---- Scale-up evaluation ----
	desired := asc.MinWorkers
	for i := range asc.ScaleUp {
		t := &asc.ScaleUp[i]
		switch t.Trigger {
		case queueworker.TriggerNoWorkers:
			if pending > 0 && totalWorkers == 0 {
				desired = maxInt(desired, 1)
			}
		case queueworker.TriggerQueueDepth:
			if pending > t.Threshold {
				desired = maxInt(desired, liveCount+1)
			}
		case queueworker.TriggerSchedule:
			if a.cronFired(t.Cron, window) {
				desired = maxInt(desired, t.Workers)
			}
		}
	}
	if desired > asc.MaxWorkers {
		desired = asc.MaxWorkers
	}

	if desired > liveCount {
		toAdd := desired - liveCount
		log.Printf("[AUTOSCALE] queue %q: scale-up %d -> %d (pending=%d active=%d workers=%d)", parent, liveCount, desired, pending, active, totalWorkers)
		for i := 0; i < toAdd; i++ {
			a.provision(ctx, parent, queue, asc, prov, rec.Name)
		}
		return // don't scale down in the same tick
	}

	// ---- Scale-down evaluation ----
	floor := asc.MinWorkers
	forceToFloor := false
	var idleTimeout time.Duration
	hasIdle := false
	for i := range asc.ScaleDown {
		t := &asc.ScaleDown[i]
		switch t.Trigger {
		case queueworker.TriggerSchedule:
			if a.cronFired(t.Cron, window) {
				forceToFloor = true
			}
		case queueworker.TriggerIdle:
			if d, err := time.ParseDuration(t.Timeout); err == nil {
				idleTimeout = d
				hasIdle = true
			}
		}
	}

	// Removable = live, unprotected, oldest activity first.
	removable := make([]*Instance, 0, len(live))
	for _, inst := range live {
		if !inst.Protected {
			removable = append(removable, inst)
		}
	}
	sort.Slice(removable, func(i, j int) bool {
		return activityTime(removable[i]).Before(activityTime(removable[j]))
	})

	maxRemovable := liveCount - floor
	if maxRemovable < 0 {
		maxRemovable = 0
	}

	var toRemove []*Instance
	switch {
	case forceToFloor:
		if maxRemovable > len(removable) {
			maxRemovable = len(removable)
		}
		toRemove = removable[:maxRemovable]
	case hasIdle:
		now := time.Now()
		for _, inst := range removable {
			if len(toRemove) >= maxRemovable {
				break
			}
			if now.Sub(activityTime(inst)) >= idleTimeout {
				toRemove = append(toRemove, inst)
			}
		}
	}

	for _, inst := range toRemove {
		log.Printf("[AUTOSCALE] queue %q: scale-down destroying %s (protected=%v)", parent, inst.InstanceID, inst.Protected)
		a.destroy(ctx, prov, inst)
	}
}

// provision creates one instance: generate a correlation id, inject env, call the provider,
// and record the instance row.
func (a *Autoscaler) provision(ctx context.Context, parent string, queue *queueworker.Queue, asc *queueworker.AutoscaleConfig, prov provider.Provider, providerName string) {
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
		log.Printf("[AUTOSCALE] queue %q: provision failed: %v", parent, err)
		// Record the failure so it is visible in status.
		_ = a.instances.Create(ctx, &Instance{
			InstanceID: correlationID, Provider: providerName, Queue: parent, Status: StatusFailed,
			Metadata: mustJSON(map[string]string{"error": err.Error()}),
		})
		return
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
		log.Printf("[AUTOSCALE] queue %q: failed to record instance %s: %v", parent, correlationID, err)
	}
}

// destroy drains then destroys an instance.
func (a *Autoscaler) destroy(ctx context.Context, prov provider.Provider, inst *Instance) {
	_ = a.instances.UpdateStatus(ctx, inst.InstanceID, StatusDraining)
	providerID := providerInstanceID(inst)
	if err := prov.Destroy(ctx, providerID); err != nil {
		log.Printf("[AUTOSCALE] destroy %s (provider id %s) failed: %v", inst.InstanceID, providerID, err)
		_ = a.instances.UpdateStatus(ctx, inst.InstanceID, StatusRunning) // revert; retry next tick
		return
	}
	_ = a.instances.UpdateStatus(ctx, inst.InstanceID, StatusTerminated)
}

// reconcileInstanceStates polls the provider for provisioning instances and promotes them
// to running (real providers boot asynchronously; mock is immediate).
func (a *Autoscaler) reconcileInstanceStates(ctx context.Context, prov provider.Provider, managed []*Instance) {
	for _, inst := range managed {
		if inst.Status != StatusProvisioning {
			continue
		}
		st, err := prov.Status(ctx, providerInstanceID(inst))
		if err != nil {
			continue
		}
		switch st.Status {
		case provider.StatusRunning:
			_ = a.instances.UpdateStatus(ctx, inst.InstanceID, StatusRunning)
			inst.Status = StatusRunning
		case provider.StatusFailed, provider.StatusTerminated:
			_ = a.instances.UpdateStatus(ctx, inst.InstanceID, StatusFailed)
			inst.Status = StatusFailed
		}
	}
}

// reconcileWorkers matches registered workers (by RUNQY_INSTANCE_ID echoed in their
// heartbeat) to managed instance rows, and propagates self-declared protection.
func (a *Autoscaler) reconcileWorkers(ctx context.Context, parent string) {
	for _, w := range a.listWorkers(ctx) {
		if w.instanceID == "" {
			continue
		}
		inst, err := a.instances.GetByInstanceID(ctx, w.instanceID)
		if err != nil || inst == nil {
			continue
		}
		if inst.WorkerID != w.workerID {
			_ = a.instances.SetWorkerID(ctx, inst.InstanceID, w.workerID)
		}
		if w.protected && !inst.Protected {
			_ = a.instances.SetProtected(ctx, inst.InstanceID, true)
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

// queueDepth sums pending and active across all sub-queues of a parent queue.
func (a *Autoscaler) queueDepth(ctx context.Context, parent string) (pending, active int) {
	q, err := a.queues.GetQueueWithSubQueues(ctx, parent)
	if err != nil || q == nil {
		return 0, 0
	}
	subs := q.SubQueues
	if len(subs) == 0 {
		// Fall back to the default sub-queue name.
		if info, err := a.inspector.GetQueueInfo(queueworker.NormalizeQueueName(parent)); err == nil {
			return info.Pending, info.Active
		}
		return 0, 0
	}
	for _, sq := range subs {
		full := queueworker.BuildFullQueueName(parent, sq.Name)
		if info, err := a.inspector.GetQueueInfo(full); err == nil {
			pending += info.Pending
			active += info.Active
		}
	}
	return pending, active
}

// workerHeartbeat is the subset of worker hash fields the autoscaler needs.
type workerHeartbeat struct {
	workerID   string
	queues     string
	instanceID string
	protected  bool
	lastBeat   int64
}

// listWorkers reads all worker heartbeats from Redis.
func (a *Autoscaler) listWorkers(ctx context.Context) []workerHeartbeat {
	var out []workerHeartbeat
	iter := a.rdb.Scan(ctx, 0, "asynq:workers:*", 100).Iterator()
	var keys []string
	for iter.Next(ctx) {
		k := iter.Val()
		if strings.Count(k, ":") >= 2 {
			keys = append(keys, k)
		}
	}
	if iter.Err() != nil {
		return out
	}
	for _, key := range keys {
		if t, err := a.rdb.Type(ctx, key).Result(); err != nil || t != "hash" {
			continue
		}
		data, err := a.rdb.HGetAll(ctx, key).Result()
		if err != nil || len(data) == 0 {
			continue
		}
		hb := workerHeartbeat{
			workerID:   strings.TrimPrefix(key, "asynq:workers:"),
			queues:     data["queues"],
			instanceID: data["instance_id"],
			protected:  strings.EqualFold(data["protected"], "true"),
		}
		if lb, err := strconv.ParseInt(data["last_beat"], 10, 64); err == nil {
			hb.lastBeat = lb
		}
		out = append(out, hb)
	}
	return out
}

// countLiveWorkers counts non-stale workers (managed or manual) serving the parent queue.
func (a *Autoscaler) countLiveWorkers(ctx context.Context, parent string) int {
	now := time.Now().Unix()
	count := 0
	for _, w := range a.listWorkers(ctx) {
		if now-w.lastBeat > workerStaleThreshold {
			continue
		}
		if workerServesParent(w.queues, parent) {
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
	a.provCache[rec.Name] = cachedProvider{prov: p, updatedAt: rec.UpdatedAt}
	return p, nil
}

// cronFired reports whether the cron expression activated within the trailing window.
func (a *Autoscaler) cronFired(expr string, window time.Duration) bool {
	sched, err := queueworker.ParseCron(expr)
	if err != nil {
		return false
	}
	now := time.Now()
	next := sched.Next(now.Add(-window))
	return !next.After(now)
}

// workerEnv builds the env injected into a booting worker so it auto-registers and correlates.
func (a *Autoscaler) workerEnv(parent, correlationID string) map[string]string {
	serverURL := os.Getenv("RUNQY_PUBLIC_URL")
	if serverURL == "" {
		serverURL = "http://localhost:" + a.cfg.HTTPPort
	}
	apiKey := a.cfg.WorkerAPIKey
	if apiKey == "" {
		apiKey = a.cfg.APIKey
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
	var m map[string]string
	if err := json.Unmarshal(meta, &m); err != nil {
		return ""
	}
	return m["provider_instance_id"]
}

func workerServesParent(queuesField, parent string) bool {
	if queuesField == "" {
		return false
	}
	for _, q := range strings.Split(queuesField, ",") {
		q = strings.TrimSpace(q)
		if q == parent {
			return true
		}
		if p, _, _ := queueworker.ParseQueueName(q); p == parent {
			return true
		}
	}
	return false
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
