package factory

import (
	"context"
	"errors"
	"testing"

	"github.com/happyhackingspace/sindoq/internal/provider"
	"github.com/happyhackingspace/sindoq/pkg/executor"
	"github.com/happyhackingspace/sindoq/pkg/fs"
)

type factoryTestProvider struct {
	name         string
	validateErr  error
	capabilities provider.Capabilities
}

func (p *factoryTestProvider) Name() string { return p.name }
func (p *factoryTestProvider) Create(ctx context.Context, opts *provider.CreateOptions) (provider.Instance, error) {
	return &factoryTestInstance{id: "factory-test-123"}, nil
}
func (p *factoryTestProvider) Capabilities() provider.Capabilities {
	return p.capabilities
}
func (p *factoryTestProvider) Validate(ctx context.Context) error {
	return p.validateErr
}
func (p *factoryTestProvider) Close() error { return nil }

type factoryTestInstance struct {
	id string
}

func (i *factoryTestInstance) ID() string       { return i.id }
func (i *factoryTestInstance) Provider() string { return "factory-test" }
func (i *factoryTestInstance) Status(ctx context.Context) (provider.InstanceStatus, error) {
	return provider.StatusRunning, nil
}
func (i *factoryTestInstance) Execute(ctx context.Context, code string, opts *executor.ExecutionOptions) (*executor.ExecutionResult, error) {
	return &executor.ExecutionResult{ExitCode: 0}, nil
}
func (i *factoryTestInstance) ExecuteStream(ctx context.Context, code string, opts *executor.ExecutionOptions, handler executor.StreamHandler) error {
	return nil
}
func (i *factoryTestInstance) RunCommand(ctx context.Context, cmd string, args []string) (*executor.CommandResult, error) {
	return &executor.CommandResult{ExitCode: 0}, nil
}
func (i *factoryTestInstance) FileSystem() fs.FileSystem      { return nil }
func (i *factoryTestInstance) Network() provider.Network      { return nil }
func (i *factoryTestInstance) Stop(ctx context.Context) error { return nil }

func TestNewFactory(t *testing.T) {
	r := NewRegistry()
	f := NewFactory(r)

	if f == nil {
		t.Fatal("NewFactory() returned nil")
	}
	if f.registry != r {
		t.Error("factory should use provided registry")
	}
}

func TestNewDefaultFactory(t *testing.T) {
	f := NewDefaultFactory()

	if f == nil {
		t.Fatal("NewDefaultFactory() returned nil")
	}
	if f.registry != DefaultRegistry {
		t.Error("factory should use DefaultRegistry")
	}
}

func TestFactoryCreateSandbox(t *testing.T) {
	r := NewRegistry()
	r.Register("test", func(config any) (provider.Provider, error) {
		return &factoryTestProvider{
			name:         "test",
			capabilities: provider.Capabilities{SupportedLanguages: []string{"Python"}},
		}, nil
	})

	f := NewFactory(r)
	ctx := context.Background()

	instance, err := f.CreateSandbox(ctx, "test", nil, nil)
	if err != nil {
		t.Fatalf("CreateSandbox() error = %v", err)
	}

	if instance == nil {
		t.Fatal("instance should not be nil")
	}
	if instance.ID() != "factory-test-123" {
		t.Errorf("ID() = %q, want %q", instance.ID(), "factory-test-123")
	}
}

func TestFactoryCreateSandboxWithOptions(t *testing.T) {
	r := NewRegistry()
	r.Register("test", func(config any) (provider.Provider, error) {
		return &factoryTestProvider{name: "test"}, nil
	})

	f := NewFactory(r)
	ctx := context.Background()

	opts := &provider.CreateOptions{
		Runtime: "Python",
		WorkDir: "/app",
	}

	instance, err := f.CreateSandbox(ctx, "test", nil, opts)
	if err != nil {
		t.Fatalf("CreateSandbox() error = %v", err)
	}
	if instance == nil {
		t.Fatal("instance should not be nil")
	}
}

func TestFactoryCreateSandboxProviderNotFound(t *testing.T) {
	r := NewRegistry()
	f := NewFactory(r)
	ctx := context.Background()

	_, err := f.CreateSandbox(ctx, "nonexistent", nil, nil)
	if err == nil {
		t.Error("CreateSandbox() should fail for unregistered provider")
	}
}

func TestFactoryGetProvider(t *testing.T) {
	r := NewRegistry()
	tp := &factoryTestProvider{name: "test"}
	r.Register("test", func(config any) (provider.Provider, error) {
		return tp, nil
	})

	f := NewFactory(r)

	p, err := f.GetProvider("test", nil)
	if err != nil {
		t.Fatalf("GetProvider() error = %v", err)
	}
	if p != tp {
		t.Error("should return same provider")
	}
}

func TestFactoryListProviders(t *testing.T) {
	r := NewRegistry()
	r.Register("one", func(config any) (provider.Provider, error) {
		return &factoryTestProvider{name: "one"}, nil
	})
	r.Register("two", func(config any) (provider.Provider, error) {
		return &factoryTestProvider{name: "two"}, nil
	})

	f := NewFactory(r)
	list := f.ListProviders()

	if len(list) != 2 {
		t.Errorf("ListProviders() = %d providers, want 2", len(list))
	}
}

func TestFactoryGetCapabilities(t *testing.T) {
	r := NewRegistry()
	r.Register("test", func(config any) (provider.Provider, error) {
		return &factoryTestProvider{
			name: "test",
			capabilities: provider.Capabilities{
				SupportsStreaming:  true,
				SupportedLanguages: []string{"Python", "Go"},
				MaxMemoryMB:        1024,
			},
		}, nil
	})

	f := NewFactory(r)

	caps, err := f.GetCapabilities("test", nil)
	if err != nil {
		t.Fatalf("GetCapabilities() error = %v", err)
	}

	if !caps.SupportsStreaming {
		t.Error("SupportsStreaming should be true")
	}
	if len(caps.SupportedLanguages) != 2 {
		t.Errorf("SupportedLanguages = %d, want 2", len(caps.SupportedLanguages))
	}
	if caps.MaxMemoryMB != 1024 {
		t.Errorf("MaxMemoryMB = %d, want 1024", caps.MaxMemoryMB)
	}
}

func TestFactoryGetCapabilitiesNotFound(t *testing.T) {
	r := NewRegistry()
	f := NewFactory(r)

	_, err := f.GetCapabilities("nonexistent", nil)
	if err == nil {
		t.Error("GetCapabilities() should fail for unregistered provider")
	}
}

func TestFactoryValidateProvider(t *testing.T) {
	r := NewRegistry()
	r.Register("valid", func(config any) (provider.Provider, error) {
		return &factoryTestProvider{name: "valid"}, nil
	})
	r.Register("invalid", func(config any) (provider.Provider, error) {
		return &factoryTestProvider{
			name:        "invalid",
			validateErr: errors.New("validation failed"),
		}, nil
	})

	f := NewFactory(r)
	ctx := context.Background()

	err := f.ValidateProvider(ctx, "valid", nil)
	if err != nil {
		t.Errorf("ValidateProvider(valid) error = %v", err)
	}

	err = f.ValidateProvider(ctx, "invalid", nil)
	if err == nil {
		t.Error("ValidateProvider(invalid) should fail")
	}

	err = f.ValidateProvider(ctx, "nonexistent", nil)
	if err == nil {
		t.Error("ValidateProvider(nonexistent) should fail")
	}
}

func TestFactoryClose(t *testing.T) {
	r := NewRegistry()
	closed := false
	r.Register("test", func(config any) (provider.Provider, error) {
		return &closeTrackingProvider{
			factoryTestProvider: &factoryTestProvider{name: "test"},
			closed:              &closed,
		}, nil
	})

	f := NewFactory(r)

	// Create to cache
	_, _ = f.GetProvider("test", nil)

	err := f.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if !closed {
		t.Error("provider should be closed")
	}
}

type closeTrackingProvider struct {
	*factoryTestProvider
	closed *bool
}

func (p *closeTrackingProvider) Close() error {
	*p.closed = true
	return nil
}

func TestGlobalFactory(t *testing.T) {
	// Save original state
	orig := globalFactory
	defer func() {
		globalFactory = orig
	}()

	r := NewRegistry()
	r.Register("global-test", func(config any) (provider.Provider, error) {
		return &factoryTestProvider{name: "global-test"}, nil
	})

	SetGlobalFactory(NewFactory(r))

	f := GetGlobalFactory()
	if f == nil {
		t.Fatal("GetGlobalFactory() returned nil")
	}

	ctx := context.Background()
	instance, err := CreateSandbox(ctx, "global-test", nil, nil)
	if err != nil {
		t.Fatalf("CreateSandbox() error = %v", err)
	}
	if instance == nil {
		t.Fatal("instance should not be nil")
	}
}
