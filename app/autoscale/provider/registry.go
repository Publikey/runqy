package provider

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// Factory builds a Provider from its decrypted JSON config blob. The blob is
// provider-type specific (e.g. credentials, endpoints); the mock provider ignores it.
type Factory func(config json.RawMessage) (Provider, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register adds a provider factory under a provider_type key. Call from init().
func Register(providerType string, f Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[providerType] = f
}

// Build constructs a provider of the given type from its decrypted config.
func Build(providerType string, config json.RawMessage) (Provider, error) {
	registryMu.RLock()
	f, ok := registry[providerType]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown autoscale provider type %q (known: %v)", providerType, Types())
	}
	return f(config)
}

// IsRegistered reports whether a provider_type has a registered factory.
func IsRegistered(providerType string) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	_, ok := registry[providerType]
	return ok
}

// Types returns the sorted list of registered provider type names.
func Types() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
