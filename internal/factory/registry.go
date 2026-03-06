// Package factory provides the factory pattern for creating sandboxes.
package factory

import (
	"fmt"
	"sync"

	"github.com/happyhackingspace/sindoq/internal/provider"
)

// ProviderConstructor creates a provider from configuration.
type ProviderConstructor func(config any) (provider.Provider, error)

// Registry maintains available providers.
type Registry struct {
	mu           sync.RWMutex
	constructors map[string]ProviderConstructor
	providers    map[string]provider.Provider
}

// NewRegistry creates a new provider registry.
func NewRegistry() *Registry {
	return &Registry{
		constructors: make(map[string]ProviderConstructor),
		providers:    make(map[string]provider.Provider),
	}
}

// Register adds a provider constructor.
func (r *Registry) Register(name string, constructor ProviderConstructor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.constructors[name] = constructor
}

// Unregister removes a provider constructor.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.constructors, name)

	// Also close and remove any cached provider
	if p, ok := r.providers[name]; ok {
		_ = p.Close()
		delete(r.providers, name)
	}
}

// Get retrieves or creates a provider.
func (r *Registry) Get(name string, config any) (provider.Provider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if provider already exists
	if p, ok := r.providers[name]; ok {
		return p, nil
	}

	// Get constructor
	constructor, ok := r.constructors[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not registered", name)
	}

	// Create provider
	p, err := constructor(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider %q: %w", name, err)
	}

	// Cache provider
	r.providers[name] = p

	return p, nil
}

// GetConstructor retrieves a provider constructor.
func (r *Registry) GetConstructor(name string) (ProviderConstructor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	constructor, ok := r.constructors[name]
	return constructor, ok
}

// Available returns all registered provider names.
func (r *Registry) Available() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.constructors))
	for name := range r.constructors {
		names = append(names, name)
	}
	return names
}

// IsRegistered checks if a provider is registered.
func (r *Registry) IsRegistered(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.constructors[name]
	return ok
}

// Close closes all cached providers.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var lastErr error
	for name, p := range r.providers {
		if err := p.Close(); err != nil {
			lastErr = fmt.Errorf("failed to close provider %q: %w", name, err)
		}
		delete(r.providers, name)
	}

	return lastErr
}

// DefaultRegistry is the global provider registry.
var DefaultRegistry = NewRegistry()

// Register adds a provider constructor to the default registry.
func Register(name string, constructor ProviderConstructor) {
	DefaultRegistry.Register(name, constructor)
}

// Unregister removes a provider from the default registry.
func Unregister(name string) {
	DefaultRegistry.Unregister(name)
}

// Get retrieves a provider from the default registry.
func Get(name string, config any) (provider.Provider, error) {
	return DefaultRegistry.Get(name, config)
}

// Available returns all available providers from the default registry.
func Available() []string {
	return DefaultRegistry.Available()
}

// IsRegistered checks if a provider is registered in the default registry.
func IsRegistered(name string) bool {
	return DefaultRegistry.IsRegistered(name)
}
