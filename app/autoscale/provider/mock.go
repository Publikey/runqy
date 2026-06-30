package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
)

func init() {
	Register("mock", newMockProvider)
}

// mockProvider is a dry-run provider: it simulates instances in memory and logs every
// decision, never touching a real cloud or spending money. It is the default first
// provider and is ideal for testing the scaling logic end-to-end.
type mockProvider struct {
	mu        sync.Mutex
	counter   int
	instances map[string]string // instanceID -> status
}

func newMockProvider(_ json.RawMessage) (Provider, error) {
	return &mockProvider{instances: make(map[string]string)}, nil
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) Provision(_ context.Context, spec InstanceSpec) (string, error) {
	m.mu.Lock()
	m.counter++
	id := fmt.Sprintf("mock-%d", m.counter)
	m.instances[id] = StatusRunning
	m.mu.Unlock()
	log.Printf("[AUTOSCALE][mock] PROVISION queue=%q gpu=%q image=%q disk=%dGB maxPrice=%.2f env=%d -> %s",
		spec.Queue, spec.GPU, spec.Image, spec.DiskGB, spec.MaxPricePerHour, len(spec.Env), id)
	return id, nil
}

func (m *mockProvider) Destroy(_ context.Context, instanceID string) error {
	m.mu.Lock()
	_, existed := m.instances[instanceID]
	m.instances[instanceID] = StatusTerminated
	m.mu.Unlock()
	log.Printf("[AUTOSCALE][mock] DESTROY %s (tracked=%v)", instanceID, existed)
	return nil
}

func (m *mockProvider) Status(_ context.Context, instanceID string) (InstanceStatus, error) {
	m.mu.Lock()
	st, ok := m.instances[instanceID]
	m.mu.Unlock()
	if !ok {
		// Provider was rebuilt (state lost) or instance is externally tracked: the DB is the
		// source of truth, so report running rather than spuriously terminated.
		return InstanceStatus{InstanceID: instanceID, Status: StatusRunning}, nil
	}
	return InstanceStatus{InstanceID: instanceID, Status: st}, nil
}
