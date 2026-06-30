// Package provider defines the pluggable cloud-GPU backend interface used by the
// autoscaler, plus the built-in providers (currently a mock/dry-run provider) and the
// encrypted store that persists provider configuration independently of the vault.
package provider

import "context"

// Instance status values reported by a provider. These mirror (a subset of) the
// lifecycle states tracked in the autoscale_instances table.
const (
	StatusProvisioning = "provisioning"
	StatusRunning      = "running"
	StatusTerminated   = "terminated"
	StatusFailed       = "failed"
	StatusUnknown      = "unknown"
)

// InstanceSpec is the request the autoscaler hands a provider to create one GPU worker.
type InstanceSpec struct {
	Queue           string            // queue this instance serves (informational)
	GPU             string            // e.g. "RTX 4090"
	Image           string            // container image to boot
	DiskGB          int               // disk size
	MaxPricePerHour float64           // bid ceiling
	Env             map[string]string // env injected into the booting worker (RUNQY_*)
}

// InstanceStatus is a provider's view of a single instance.
type InstanceStatus struct {
	InstanceID string `json:"instance_id"`
	Status     string `json:"status"` // one of the Status* constants
	Message    string `json:"message,omitempty"`
}

// Provider is a pluggable cloud GPU backend. Implementations must be safe for concurrent
// use by the autoscaler goroutine.
type Provider interface {
	// Name returns the provider type identifier (e.g. "mock", "vastai").
	Name() string
	// Provision creates one instance and returns its provider-assigned id.
	Provision(ctx context.Context, spec InstanceSpec) (instanceID string, err error)
	// Destroy tears an instance down. It must be idempotent (no error if already gone).
	Destroy(ctx context.Context, instanceID string) error
	// Status reports the current state of an instance.
	Status(ctx context.Context, instanceID string) (InstanceStatus, error)
}
