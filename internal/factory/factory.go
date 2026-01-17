package factory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/happyhackingspace/sindoq/internal/provider"
)

// SandboxFactory creates sandboxes from different providers.
type SandboxFactory struct {
	registry *Registry
}

// NewFactory creates a factory with a custom registry.
func NewFactory(registry *Registry) *SandboxFactory {
	return &SandboxFactory{
		registry: registry,
	}
}

// NewDefaultFactory creates a factory with the default registry.
func NewDefaultFactory() *SandboxFactory {
	return &SandboxFactory{
		registry: DefaultRegistry,
	}
}

// CreateSandbox creates a sandbox using the specified provider.
func (f *SandboxFactory) CreateSandbox(ctx context.Context, providerName string, providerConfig any, opts *provider.CreateOptions) (provider.Instance, error) {
	if !f.registry.IsRegistered(providerName) {
		available := f.registry.Available()
		var buf strings.Builder
		fmt.Fprintf(&buf, "provider %q not found\n\nAvailable providers:\n", providerName)
		for _, p := range available {
			if p == "docker" {
				fmt.Fprintf(&buf, "  - %s (default)\n", p)
			} else {
				fmt.Fprintf(&buf, "  - %s\n", p)
			}
		}
		return nil, errors.New(buf.String())
	}

	p, err := f.registry.Get(providerName, providerConfig)
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}

	if opts == nil {
		opts = provider.DefaultCreateOptions()
	}

	instance, err := p.Create(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("create sandbox: %w", err)
	}

	return instance, nil
}

// GetProvider returns a specific provider.
func (f *SandboxFactory) GetProvider(name string, config any) (provider.Provider, error) {
	return f.registry.Get(name, config)
}

// ListProviders returns all available provider names.
func (f *SandboxFactory) ListProviders() []string {
	return f.registry.Available()
}

// GetCapabilities returns the capabilities of a provider.
func (f *SandboxFactory) GetCapabilities(providerName string, config any) (*provider.Capabilities, error) {
	p, err := f.registry.Get(providerName, config)
	if err != nil {
		return nil, err
	}

	caps := p.Capabilities()
	return &caps, nil
}

// ValidateProvider checks if a provider is properly configured.
func (f *SandboxFactory) ValidateProvider(ctx context.Context, providerName string, config any) error {
	p, err := f.registry.Get(providerName, config)
	if err != nil {
		return err
	}

	return p.Validate(ctx)
}

// Close closes all providers in the factory.
func (f *SandboxFactory) Close() error {
	return f.registry.Close()
}

// Global factory instance
var globalFactory = NewDefaultFactory()

// CreateSandbox creates a sandbox using the global factory.
func CreateSandbox(ctx context.Context, providerName string, providerConfig any, opts *provider.CreateOptions) (provider.Instance, error) {
	return globalFactory.CreateSandbox(ctx, providerName, providerConfig, opts)
}

// GetGlobalFactory returns the global factory instance.
func GetGlobalFactory() *SandboxFactory {
	return globalFactory
}

// SetGlobalFactory sets the global factory instance.
func SetGlobalFactory(factory *SandboxFactory) {
	globalFactory = factory
}
