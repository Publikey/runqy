package autoscale

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Publikey/runqy/autoscale/provider"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// loggingProvider is a fakeProvider that also returns logs.
type loggingProvider struct {
	fakeProvider
	logs    string
	logsErr error
	asked   []string
}

func (p *loggingProvider) Logs(_ context.Context, id string, _ int) (string, error) {
	p.asked = append(p.asked, id)
	return p.logs, p.logsErr
}

var _ provider.LogFetcher = (*loggingProvider)(nil)

func TestBootTimeoutTeardownSavesMachineLogs(t *testing.T) {
	a := newTestAutoscaler(t)
	addInstance(t, a, "stuck", StatusProvisioning, "", time.Hour) // past the 20m boot timeout
	prov := &loggingProvider{logs: "Error response from daemon: pull access denied"}

	reconcile(t, a, prov, nil, time.Now())

	if len(prov.asked) != 1 || prov.asked[0] != "p-stuck" {
		t.Fatalf("logs fetched for %v, want the provider id p-stuck", prov.asked)
	}
	inst, _ := a.instances.GetByInstanceID(context.Background(), "stuck")
	md := inst.MetadataMap()
	if inst.Status != StatusFailed || md["logs"] != "Error response from daemon: pull access denied" || md["logs_at"] == nil {
		t.Errorf("status %s, metadata %v", inst.Status, md)
	}
	if !strings.Contains(inst.Reason, "boot_timeout") {
		t.Errorf("reason = %q", inst.Reason)
	}
}

func TestTeardownKeepsGoingWhenLogsFail(t *testing.T) {
	a := newTestAutoscaler(t)
	addInstance(t, a, "stuck", StatusProvisioning, "", time.Hour)
	prov := &loggingProvider{logsErr: errors.New("vastai: logs not ready")}

	reconcile(t, a, prov, nil, time.Now())

	inst, _ := a.instances.GetByInstanceID(context.Background(), "stuck")
	if inst.Status != StatusFailed || len(prov.destroyed) != 1 {
		t.Fatalf("status %s, destroyed %v: a log failure must not block the teardown", inst.Status, prov.destroyed)
	}
	if inst.MetadataMap()["logs_error"] != "vastai: logs not ready" {
		t.Errorf("metadata %v", inst.MetadataMap())
	}
}

func TestTailBytesKeepsTheEnd(t *testing.T) {
	s := strings.Repeat("old line\n", 100) + "the error\n"
	got := tailBytes(s, 30)
	if !strings.HasSuffix(got, "the error\n") || !strings.HasPrefix(got, "[…]\n") || len(got) > 40 {
		t.Errorf("tailBytes = %q", got)
	}
	if tailBytes("short", 30) != "short" {
		t.Error("short input must be unchanged")
	}
}

func TestListPausesReturnsOnlyActivePauses(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	store := redisBackoffStore{rdb: rdb}
	ctx := context.Background()
	now := time.Now()
	_ = store.Save(ctx, backoffField(backoffBoot, "sdxl"), persistedBackoff{Failures: 4, Until: now.Add(time.Hour)})
	_ = store.Save(ctx, backoffField(backoffProvision, "anima"), persistedBackoff{Failures: 1, Until: now.Add(time.Minute)})
	_ = store.Save(ctx, backoffField(backoffHost, "old"), persistedBackoff{Failures: 2, Until: now.Add(-time.Minute)}) // over

	pauses, err := ListPauses(ctx, rdb, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(pauses) != 2 || pauses[0].Queue != "anima" || pauses[1].Queue != "sdxl" {
		t.Fatalf("pauses = %+v", pauses)
	}
	if pauses[1].Kind != backoffBoot || pauses[1].Failures != 4 || pauses[1].Cause == "" {
		t.Errorf("sdxl pause = %+v", pauses[1])
	}
}

// A machine the provider stopped while its worker was serving keeps its last output, like
// a failed boot: it is the only trace of why it went away.
func TestProviderTerminatedRunningInstanceSavesLogs(t *testing.T) {
	a := newTestAutoscaler(t)
	addInstance(t, a, "lost", StatusRunning, "w1", time.Hour)
	addInstance(t, a, "fine", StatusRunning, "w2", time.Hour)
	prov := &loggingProvider{logs: "Killed", fakeProvider: fakeProvider{status: map[string]string{"p-lost": provider.StatusTerminated}}}
	now := time.Now()
	hbs := map[string]*workerHeartbeat{"fine": {workerID: "w2", status: "running", lastBeat: now.Unix()}}

	reconcile(t, a, prov, hbs, now)

	inst, _ := a.instances.GetByInstanceID(context.Background(), "lost")
	if inst.Status != StatusTerminated || inst.MetadataMap()["logs"] != "Killed" {
		t.Errorf("status %s, metadata %v, want terminated with the saved logs", inst.Status, inst.MetadataMap())
	}
	if len(prov.asked) != 1 {
		t.Errorf("logs fetched for %v, want only the lost instance", prov.asked)
	}
}

func TestResumeQueueLiftsEveryPause(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	ctx := context.Background()
	now := time.Now()
	store := redisBackoffStore{rdb: rdb}
	_ = store.Save(ctx, backoffField(backoffBoot, "q"), persistedBackoff{Failures: 5, Until: now.Add(time.Hour), Suspended: true})
	_ = store.Save(ctx, backoffField(backoffHost, "q"), persistedBackoff{Failures: 1, Until: now.Add(time.Minute)})
	_ = store.Save(ctx, backoffField(backoffBoot, "other"), persistedBackoff{Failures: 1, Until: now.Add(time.Hour)})

	pauses, _ := ListPauses(ctx, rdb, now)
	if len(pauses) != 3 || !pauses[1].Suspended || !strings.Contains(pauses[1].Cause, "suspended") {
		t.Fatalf("pauses = %+v, want q's boot pause flagged as suspended", pauses)
	}
	n, err := ResumeQueue(ctx, rdb, "q")
	if err != nil || n != 2 {
		t.Fatalf("ResumeQueue = %d, %v, want 2 pauses lifted", n, err)
	}
	if ok, _ := store.Has(ctx, backoffField(backoffBoot, "q")); ok {
		t.Error("boot pause still stored")
	}
	if pauses, _ = ListPauses(ctx, rdb, now); len(pauses) != 1 || pauses[0].Queue != "other" {
		t.Errorf("pauses after resume = %+v, want only the other queue", pauses)
	}
}
