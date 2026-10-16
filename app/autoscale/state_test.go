package autoscale

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	queueworker "github.com/Publikey/runqy/queues"
)

// memBackoffStore is an in-memory backoffStore shared by "servers" in a test.
type memBackoffStore struct {
	mu sync.Mutex
	m  map[string]persistedBackoff
}

func newMemBackoffStore() *memBackoffStore { return &memBackoffStore{m: map[string]persistedBackoff{}} }

func (s *memBackoffStore) LoadAll(context.Context) (map[string]persistedBackoff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]persistedBackoff, len(s.m))
	for k, v := range s.m {
		out[k] = v
	}
	return out, nil
}

func (s *memBackoffStore) Save(_ context.Context, field string, b persistedBackoff) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[field] = b
	return nil
}

func (s *memBackoffStore) Delete(_ context.Context, field string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, field)
	return nil
}

func TestBackoffSurvivesRestart(t *testing.T) {
	store := newMemBackoffStore()
	ctx := context.Background()
	now := time.Now()
	asc := &queueworker.AutoscaleConfig{Enabled: true, Provider: "p", MaxWorkers: 1}

	// Server 1: a provisioning failure and an instance that never became ready.
	a := newTestAutoscaler(t)
	a.backoffs = store
	prov := &fakeProvider{name: "mock", provisionErr: errors.New("no offers")}
	a.provisionN(ctx, "inference", asc, prov, "p", 1, 30*time.Second, now, "")
	addInstance(t, a, "stuck", StatusRunning, "", 30*time.Minute)
	reconcile(t, a, prov, nil, now)
	if len(store.m) != 2 {
		t.Fatalf("persisted = %v, want provision and boot entries", store.m)
	}

	// Server 2 (restart or new leader) picks the pauses up and does not provision.
	b := newTestAutoscaler(t)
	b.backoffs = store
	b.loadBackoffs(ctx, now.Add(time.Minute))
	if p := b.bootFails["inference"]; p == nil || p.failures != 1 || !p.until.Equal(a.bootFails["inference"].until) {
		t.Fatalf("boot backoff not restored: %+v", p)
	}
	if p := b.backoff["inference"]; p == nil || p.failures != 1 {
		t.Fatalf("provision backoff not restored: %+v", p)
	}
	prov2 := &fakeProvider{name: "mock"}
	b.provisionN(ctx, "inference", asc, prov2, "p", 1, 30*time.Second, now.Add(time.Minute), "")
	if prov2.provisions != 0 {
		t.Fatal("restarted server provisioned during a persisted pause")
	}

	// A ready worker clears the boot pause in the store too.
	addInstance(t, b, "good", StatusRunning, "w-good", time.Minute)
	hbs := map[string]*workerHeartbeat{"good": {workerID: "w-good", status: "running", lastBeat: now.Unix()}}
	reconcile(t, b, prov2, hbs, now)
	if _, ok := store.m[backoffField(backoffBoot, "inference")]; ok {
		t.Error("boot backoff still persisted after a ready worker")
	}
}

func TestLoadBackoffsDropsOldEntries(t *testing.T) {
	store := newMemBackoffStore()
	now := time.Now()
	store.m[backoffField(backoffHost, "gone")] = persistedBackoff{Failures: 3, Until: now.Add(-backoffRetention - time.Hour)}
	store.m[backoffField(backoffHost, "recent")] = persistedBackoff{Failures: 2, Until: now.Add(-time.Hour)}
	store.m["junk"] = persistedBackoff{Failures: 1, Until: now}

	a := newTestAutoscaler(t)
	a.backoffs = store
	a.loadBackoffs(context.Background(), now)
	if a.hostFails["gone"] != nil {
		t.Error("expired entry loaded")
	}
	if _, ok := store.m[backoffField(backoffHost, "gone")]; ok {
		t.Error("expired entry not deleted from the store")
	}
	// An ended pause keeps its count so the next failure still doubles the delay.
	if p := a.hostFails["recent"]; p == nil || p.failures != 2 {
		t.Errorf("recent entry = %+v, want 2 failures kept", p)
	}
}
