package adapters

import (
	"fmt"
	"sort"
	"sync"
)

// Factory creates an adapter instance from a base URL and upstream auth config.
// Each adapter type registers a factory function at init time.
type Factory func(baseURL string, auth UpstreamAuth) (Adapter, error)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Factory)
)

// Register adds an adapter factory to the global registry. Adapter packages
// call this in their init() function so that serve.go can look up adapters
// by name from the gateway config without hardcoded imports.
//
// Panics if a factory is already registered for the given name, which
// catches duplicate registrations at startup rather than silently
// overwriting.
func Register(name string, factory Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("adapter %q already registered", name))
	}
	registry[name] = factory
}

// Lookup returns the factory for the named adapter, or an error if no
// such adapter is registered.
func Lookup(name string) (Factory, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown adapter %q; registered adapters: %v", name, RegisteredNames())
	}
	return f, nil
}

// RegisteredNames returns the sorted list of registered adapter names.
// Useful for error messages and CLI help text.
func RegisteredNames() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ResetRegistry clears all registered factories. Only used in tests.
func ResetRegistry() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = make(map[string]Factory)
}
