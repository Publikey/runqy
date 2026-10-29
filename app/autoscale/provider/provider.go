// Package provider defines the pluggable cloud-GPU backend interface used by the
// autoscaler, plus the built-in providers (currently a mock/dry-run provider) and the
// encrypted store that persists provider configuration independently of the vault.
package provider

import "context"

// defaultMinCUDAVersion is the lowest host CUDA version accepted by default: the
// runqy-worker inference image is built on CUDA 12.8.
const defaultMinCUDAVersion = "12.8"

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
	MinRAMGB        int               // minimum system RAM (0 = provider default)
	MinVCPU         int               // minimum vCPUs (0 = provider default)
	Env             map[string]string // env injected into the booting worker (RUNQY_*)
}

// InstanceStatus is a provider's view of a single instance.
type InstanceStatus struct {
	InstanceID string `json:"instance_id"`
	Status     string `json:"status"` // one of the Status* constants
	Message    string `json:"message,omitempty"`
	// HostFault marks a failed/terminated instance whose container never started:
	// the machine is at fault (GPU not attachable, Docker error...), not the image.
	// The autoscaler then retries soon on another host instead of backing off as for
	// a broken image.
	HostFault bool `json:"host_fault,omitempty"`
}

// StateStore persists a provider's small runtime state (e.g. hosts to avoid) so it
// survives a server restart, a change of autoscaler leader and a provider config update.
type StateStore interface {
	// Load decodes the saved state into v; ok is false when nothing was saved.
	Load(ctx context.Context, v any) (ok bool, err error)
	// Save replaces the saved state with v.
	Save(ctx context.Context, v any) error
}

// Stateful is implemented by providers whose runtime state is worth persisting. The
// autoscaler calls SetStateStore once, right after building the provider.
type Stateful interface {
	SetStateStore(ctx context.Context, s StateStore)
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
