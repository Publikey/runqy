package autoscale

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Publikey/runqy/autoscale/provider"
	"github.com/Publikey/runqy/config"
	"github.com/Publikey/runqy/models"
	queueworker "github.com/Publikey/runqy/queues"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// fakeProvider records calls and returns scripted statuses.
type fakeProvider struct {
	mu           sync.Mutex
	name         string
	status       map[string]string // provider id -> status (default running)
	hostFault    map[string]bool   // provider id -> report a host fault with the status
	provisionErr error
	destroyErr   error
	provisions   int
	destroyed    []string
}

func (f *fakeProvider) Name() string {
	if f.name == "" {
		return "fake"
	}
	return f.name
}

func (f *fakeProvider) Provision(_ context.Context, _ provider.InstanceSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.provisions++
	if f.provisionErr != nil {
		return "", f.provisionErr
	}
	return fmt.Sprintf("p-%d", f.provisions), nil
}

func (f *fakeProvider) Destroy(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.destroyErr != nil {
		return f.destroyErr
	}
	f.destroyed = append(f.destroyed, id)
	return nil
}

func (f *fakeProvider) Status(_ context.Context, id string) (provider.InstanceStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := provider.StatusRunning
	if s, ok := f.status[id]; ok {
		st = s
	}
	return provider.InstanceStatus{InstanceID: id, Status: st, HostFault: f.hostFault[id]}, nil
}

func newTestAutoscaler(t *testing.T) *Autoscaler {
	t.Helper()
	db, err := sqlx.Connect("sqlite", filepath.Join(t.TempDir(), "autoscale.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := models.EnsureSchema(db, false); err != nil {
		t.Fatal(err)
	}
	return &Autoscaler{
		instances:  NewStore(db),
		cfg:        &config.Config{HTTPPort: "3000", APIKey: "admin"},
		backoff:    make(map[string]*provisionBackoff),
		bootFails:  make(map[string]*provisionBackoff),
		hostFails:  make(map[string]*provisionBackoff),
		envWarned:  make(map[string]string),
		staleSince: make(map[string]time.Time),
		bootSince:  make(map[string]bootObservation),
	}
}

func addInstance(t *testing.T, a *Autoscaler, id, status, workerID string, age time.Duration) *Instance {
	t.Helper()
	inst := &Instance{
		InstanceID: id, Provider: "fake", Queue: "inference", Status: status, WorkerID: workerID,
		CreatedAt: time.Now().Add(-age), Metadata: mustJSON(map[string]string{"provider_instance_id": "p-" + id}),
	}
	if err := a.instances.Create(context.Background(), inst); err != nil {
		t.Fatal(err)
	}
	return inst
}

func statusOf(t *testing.T, a *Autoscaler, id string) string {
	t.Helper()
	inst, err := a.instances.GetByInstanceID(context.Background(), id)
	if err != nil || inst == nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return inst.Status
}

func reconcile(t *testing.T, a *Autoscaler, prov provider.Provider, hbs map[string]*workerHeartbeat, now time.Time) []*Instance {
	t.Helper()
	managed, err := a.instances.ListByQueue(context.Background(), "inference")
	if err != nil {
		t.Fatal(err)
	}
	if hbs == nil {
		hbs = map[string]*workerHeartbeat{}
	}
	return a.reconcileInstances(context.Background(), prov, "inference", managed, hbs, 20*time.Minute, now)
}

func TestReconcileTearsDownInstancesDeadAtProvider(t *testing.T) {
	a := newTestAutoscaler(t)
	addInstance(t, a, "gone", StatusRunning, "w1", time.Hour)
	addInstance(t, a, "boot-failed", StatusProvisioning, "", time.Minute)
	addInstance(t, a, "ok", StatusRunning, "w3", time.Hour)
	prov := &fakeProvider{status: map[string]string{
		"p-gone":        provider.StatusTerminated,
		"p-boot-failed": provider.StatusFailed,
	}}
	now := time.Now()
	hbs := map[string]*workerHeartbeat{"ok": {workerID: "w3", status: "running", lastBeat: now.Unix()}}

	live := reconcile(t, a, prov, hbs, now)

	if len(live) != 1 || live[0].InstanceID != "ok" {
		t.Fatalf("live = %v, want [ok]", ids(live))
	}
	if len(prov.destroyed) != 2 {
		t.Errorf("destroyed = %v, want both dead instances destroyed at the provider", prov.destroyed)
	}
	if s := statusOf(t, a, "gone"); s != StatusTerminated {
		t.Errorf("gone: status %q, want terminated", s)
	}
	if s := statusOf(t, a, "boot-failed"); s != StatusFailed {
		t.Errorf("boot-failed: status %q, want failed", s)
	}
}

func TestReconcileBootTimeout(t *testing.T) {
	a := newTestAutoscaler(t)
	addInstance(t, a, "stuck", StatusRunning, "", 30*time.Minute)   // never registered
	addInstance(t, a, "young", StatusProvisioning, "", time.Minute) // still within boot_timeout
	prov := &fakeProvider{}

	live := reconcile(t, a, prov, nil, time.Now())

	if len(live) != 1 || live[0].InstanceID != "young" {
		t.Fatalf("live = %v, want [young]", ids(live))
	}
	if s := statusOf(t, a, "stuck"); s != StatusFailed {
		t.Errorf("stuck: status %q, want failed", s)
	}
}

func TestReconcileLostWorker(t *testing.T) {
	a := newTestAutoscaler(t)
	addInstance(t, a, "lost", StatusRunning, "w1", time.Hour)
	p := addInstance(t, a, "lost-protected", StatusRunning, "w2", time.Hour)
	if err := a.instances.SetProtected(context.Background(), p.InstanceID, true); err != nil {
		t.Fatal(err)
	}
	prov := &fakeProvider{}
	now := time.Now()

	// First look (e.g. right after a server restart): the clock starts, nothing is killed.
	if live := reconcile(t, a, prov, nil, now); len(live) != 2 {
		t.Fatalf("first pass live = %v, want both", ids(live))
	}
	// Still silent past workerLostTimeout: the unprotected one is torn down.
	live := reconcile(t, a, prov, nil, now.Add(workerLostTimeout+time.Minute))
	if len(live) != 1 || live[0].InstanceID != "lost-protected" {
		t.Fatalf("live = %v, want [lost-protected]", ids(live))
	}
	if s := statusOf(t, a, "lost"); s != StatusFailed {
		t.Errorf("lost: status %q, want failed", s)
	}
}

func TestProvisionBackoff(t *testing.T) {
	a := newTestAutoscaler(t)
	prov := &fakeProvider{name: "mock", provisionErr: errors.New("no offers")}
	asc := &queueworker.AutoscaleConfig{Enabled: true, Provider: "p", MaxWorkers: 3}
	ctx := context.Background()
	now := time.Now()

	a.provisionN(ctx, "inference", asc, prov, "p", 3, 30*time.Second, now, "")
	if prov.provisions != 1 {
		t.Fatalf("provisions = %d, want 1 (stop at first failure)", prov.provisions)
	}
	a.provisionN(ctx, "inference", asc, prov, "p", 3, 30*time.Second, now.Add(10*time.Second), "")
	if prov.provisions != 1 {
		t.Fatalf("provisions = %d, want 1 (still backing off)", prov.provisions)
	}
	later := now.Add(31 * time.Second)
	a.provisionN(ctx, "inference", asc, prov, "p", 1, 30*time.Second, later, "")
	if prov.provisions != 2 || a.backoff["inference"].failures != 2 {
		t.Fatalf("provisions = %d failures = %d, want 2/2", prov.provisions, a.backoff["inference"].failures)
	}
	if wait := a.backoff["inference"].until.Sub(later); wait != time.Minute {
		t.Errorf("second backoff = %s, want 1m (doubled)", wait)
	}

	prov.provisionErr = nil
	a.provisionN(ctx, "inference", asc, prov, "p", 2, 30*time.Second, now.Add(2*time.Minute), "")
	if prov.provisions != 4 || a.backoff["inference"] != nil {
		t.Errorf("after recovery: provisions = %d backoff = %v, want 4 and reset", prov.provisions, a.backoff["inference"])
	}
}

func TestRealProviderNeedsPublicURLAndWorkerKey(t *testing.T) {
	a := newTestAutoscaler(t)
	t.Setenv("RUNQY_PUBLIC_URL", "")
	prov := &fakeProvider{name: "runpod"}
	asc := &queueworker.AutoscaleConfig{Enabled: true, Provider: "p", MaxWorkers: 1}

	a.provisionN(context.Background(), "inference", asc, prov, "p", 1, 30*time.Second, time.Now(), "")
	if prov.provisions != 0 {
		t.Fatal("provisioned without RUNQY_PUBLIC_URL")
	}

	t.Setenv("RUNQY_PUBLIC_URL", "https://runqy.example.com")
	a.provisionN(context.Background(), "inference", asc, prov, "p", 1, 30*time.Second, time.Now(), "")
	if prov.provisions != 0 {
		t.Fatal("provisioned with the admin API key (no RUNQY_WORKER_API_KEY)")
	}

	a.cfg.WorkerAPIKey = "worker-key"
	a.provisionN(context.Background(), "inference", asc, prov, "p", 1, 30*time.Second, time.Now(), "")
	if prov.provisions != 1 {
		t.Fatalf("provisions = %d, want 1 once configured", prov.provisions)
	}
	if env := a.workerEnv("inference", "as-1"); env["RUNQY_API_KEY"] != "worker-key" {
		t.Errorf("RUNQY_API_KEY = %q, want the worker key", env["RUNQY_API_KEY"])
	}
}

func TestTeardownRetriedAfterDestroyFailure(t *testing.T) {
	a := newTestAutoscaler(t)
	addInstance(t, a, "doomed", StatusRunning, "", 30*time.Minute) // past boot_timeout
	prov := &fakeProvider{destroyErr: errors.New("provider API down")}

	if live := reconcile(t, a, prov, nil, time.Now()); len(live) != 0 {
		t.Fatalf("live = %v, want none", ids(live))
	}
	if s := statusOf(t, a, "doomed"); s != StatusDraining {
		t.Fatalf("after failed destroy: status %q, want draining (kept for retry)", s)
	}

	prov.destroyErr = nil
	reconcile(t, a, prov, nil, time.Now())
	if len(prov.destroyed) != 1 {
		t.Fatalf("destroy not retried: %v", prov.destroyed)
	}
	if s := statusOf(t, a, "doomed"); s != StatusFailed {
		t.Errorf("after retry: status %q, want failed (the recorded final status)", s)
	}
}

func TestReconcileSlightlyLateHeartbeatIsNotLost(t *testing.T) {
	a := newTestAutoscaler(t)
	addInstance(t, a, "busy", StatusRunning, "w1", time.Hour)
	prov := &fakeProvider{}
	now := time.Now()
	hb := func(at time.Time, lag int64) map[string]*workerHeartbeat {
		return map[string]*workerHeartbeat{"busy": {workerID: "w1", status: "running", lastBeat: at.Unix() - lag, active: 1}}
	}

	reconcile(t, a, prov, hb(now, 0), now)
	// One evaluation much later (long poll interval) where the beat is just past the
	// staleness window: the worker is alive, nothing must be destroyed.
	later := now.Add(6 * time.Minute)
	if live := reconcile(t, a, prov, hb(later, workerStaleThreshold+1), later); len(live) != 1 {
		t.Fatalf("live = %v, want [busy]", ids(live))
	}
	if len(prov.destroyed) != 0 {
		t.Errorf("destroyed %v, want none", prov.destroyed)
	}
}

func TestBootTimeoutRestartsClockForRestartedWorker(t *testing.T) {
	a := newTestAutoscaler(t)
	addInstance(t, a, "old", StatusRunning, "w2", 3*time.Hour) // machine is old, worker just restarted
	prov := &fakeProvider{}
	now := time.Now()
	booting := func(at time.Time) map[string]*workerHeartbeat {
		return map[string]*workerHeartbeat{"old": {workerID: "w2", status: workerStatusBootstrapping, lastBeat: at.Unix()}}
	}

	if live := reconcile(t, a, prov, booting(now), now); len(live) != 1 {
		t.Fatalf("restarted worker torn down on machine age: live = %v", ids(live))
	}
	later := now.Add(21 * time.Minute)
	if live := reconcile(t, a, prov, booting(later), later); len(live) != 0 {
		t.Fatalf("worker bootstrapping for longer than boot_timeout kept: live = %v", ids(live))
	}
}

type fakeSink struct{ messages []string }

func (f *fakeSink) Record(_ context.Context, queue, rule, message string) error {
	f.messages = append(f.messages, queue+"|"+rule+"|"+message)
	return nil
}

func TestFailuresReportedToEventSink(t *testing.T) {
	a := newTestAutoscaler(t)
	sink := &fakeSink{}
	a.SetEventSink(sink)

	prov := &fakeProvider{name: "mock", provisionErr: errors.New("no offers")}
	a.provisionN(context.Background(), "inference", &queueworker.AutoscaleConfig{MaxWorkers: 1}, prov, "p", 1, 30*time.Second, time.Now(), "")
	addInstance(t, a, "stuck", StatusRunning, "", time.Hour) // past boot_timeout
	reconcile(t, a, &fakeProvider{}, nil, time.Now())

	if len(sink.messages) != 2 ||
		!strings.Contains(sink.messages[0], "inference|autoscale_failure|provisioning failed") ||
		!strings.Contains(sink.messages[1], "instance stuck destroyed: no ready worker") {
		t.Fatalf("events = %v", sink.messages)
	}
}

func TestBootFailuresBackOffProvisioning(t *testing.T) {
	a := newTestAutoscaler(t)
	prov := &fakeProvider{name: "mock"}
	asc := &queueworker.AutoscaleConfig{MaxWorkers: 2}
	ctx := context.Background()
	now := time.Now()

	// An instance that never got a worker is destroyed after boot_timeout (20m in reconcile)...
	addInstance(t, a, "stuck-1", StatusRunning, "", 30*time.Minute)
	reconcile(t, a, prov, nil, now)
	if b := a.bootFails["inference"]; b == nil || b.failures != 1 || b.until.Sub(now) != 20*time.Minute {
		t.Fatalf("after 1st boot failure: %+v, want 1 failure and a 20m pause", b)
	}
	// ...and the next instance is not rented right away (the staging loop rented one every boot_timeout).
	a.provisionN(ctx, "inference", asc, prov, "p", 1, 30*time.Second, now.Add(time.Minute), "")
	if prov.provisions != 0 {
		t.Fatal("provisioned during the boot-failure pause")
	}

	// A second failure doubles the pause.
	later := now.Add(21 * time.Minute)
	addInstance(t, a, "stuck-2", StatusRunning, "", 30*time.Minute)
	reconcile(t, a, prov, nil, later)
	if b := a.bootFails["inference"]; b.failures != 2 || b.until.Sub(later) != 40*time.Minute {
		t.Fatalf("after 2nd boot failure: %+v, want 2 failures and a 40m pause", b)
	}
	inst, _ := a.instances.GetByInstanceID(ctx, "stuck-2")
	if !strings.Contains(string(inst.Metadata), "2 in a row, next instance in 40m0s") {
		t.Errorf("teardown reason should say when the next instance comes: %s", inst.Metadata)
	}

	// A ready worker on any instance resets the counter: the image works.
	addInstance(t, a, "good", StatusRunning, "w-good", time.Minute)
	hbs := map[string]*workerHeartbeat{"good": {workerID: "w-good", status: "running", lastBeat: later.Unix()}}
	reconcile(t, a, prov, hbs, later)
	if a.bootFails["inference"] != nil {
		t.Fatalf("boot-failure pause not reset by a ready worker: %+v", a.bootFails["inference"])
	}
	a.provisionN(ctx, "inference", asc, prov, "p", 1, 30*time.Second, later, "")
	if prov.provisions != 1 {
		t.Fatalf("provisions = %d, want 1 after the reset", prov.provisions)
	}
}

func TestLostWorkerIsNotABootFailure(t *testing.T) {
	a := newTestAutoscaler(t)
	addInstance(t, a, "was-fine", StatusRunning, "w1", time.Hour)
	now := time.Now()
	reconcile(t, a, &fakeProvider{}, nil, now)
	reconcile(t, a, &fakeProvider{}, nil, now.Add(workerLostTimeout+time.Minute))
	if s := statusOf(t, a, "was-fine"); s != StatusFailed {
		t.Fatalf("lost worker: status %q, want failed", s)
	}
	if a.bootFails["inference"] != nil {
		t.Error("a machine that worked then died (e.g. preempted) must not slow down provisioning")
	}
}

func TestProvisionPauseLoggedOnce(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	a := newTestAutoscaler(t)
	prov := &fakeProvider{name: "mock", provisionErr: errors.New("no offers")}
	asc := &queueworker.AutoscaleConfig{MaxWorkers: 1}
	now := time.Now()
	a.provisionN(context.Background(), "inference", asc, prov, "p", 1, time.Minute, now, "scale-up 0 -> 1")
	for i := 1; i <= 5; i++ { // five more polls inside the 1m pause
		a.provisionN(context.Background(), "inference", asc, prov, "p", 1, time.Minute, now.Add(time.Duration(i)*5*time.Second), "scale-up 0 -> 1")
	}
	out := buf.String()
	if n := strings.Count(out, "provisioning paused"); n != 1 {
		t.Errorf("pause logged %d times, want once:\n%s", n, out)
	}
	if n := strings.Count(out, "scale-up 0 -> 1"); n != 1 {
		t.Errorf("scale-up logged %d times, want once (only when actually attempted):\n%s", n, out)
	}
}

func TestHostFaultsRetrySoonOnAnotherHost(t *testing.T) {
	a := newTestAutoscaler(t)
	ctx := context.Background()
	now := time.Now()
	prov := &fakeProvider{name: "mock",
		status:    map[string]string{"p-h1": provider.StatusTerminated, "p-h2": provider.StatusTerminated},
		hostFault: map[string]bool{"p-h1": true, "p-h2": true}}

	// A host that could not start the container: short pause, no boot-failure penalty.
	addInstance(t, a, "h1", StatusProvisioning, "", time.Minute)
	reconcile(t, a, prov, nil, now)
	if b := a.hostFails["inference"]; b == nil || b.until.Sub(now) != hostFaultRetryBase {
		t.Fatalf("after 1st host fault: %+v, want a %s pause", b, hostFaultRetryBase)
	}
	if a.bootFails["inference"] != nil {
		t.Error("a host fault must not count as a boot failure (25m+ pauses)")
	}
	inst, _ := a.instances.GetByInstanceID(ctx, "h1")
	if !strings.Contains(string(inst.Metadata), "host fault") {
		t.Errorf("reason should say host fault: %s", inst.Metadata)
	}

	// The next rental succeeds but that host fails too: the pause still grows, because a
	// successful rental is not a success here, only a ready worker is.
	a.provisionN(ctx, "inference", &queueworker.AutoscaleConfig{MaxWorkers: 1}, &fakeProvider{name: "mock"}, "p", 1, 30*time.Second, now.Add(time.Minute), "")
	later := now.Add(2 * time.Minute)
	addInstance(t, a, "h2", StatusProvisioning, "", time.Minute)
	reconcile(t, a, prov, nil, later)
	if b := a.hostFails["inference"]; b == nil || b.failures != 2 || b.until.Sub(later) != 2*hostFaultRetryBase {
		t.Fatalf("after 2nd host fault: %+v, want 2 failures and a %s pause", b, 2*hostFaultRetryBase)
	}

	// A ready worker resets it.
	addInstance(t, a, "good", StatusRunning, "w", time.Minute)
	reconcile(t, a, &fakeProvider{}, map[string]*workerHeartbeat{"good": {workerID: "w", status: "running", lastBeat: later.Unix()}}, later)
	if a.hostFails["inference"] != nil {
		t.Errorf("host-fault pause not reset by a ready worker")
	}
}
