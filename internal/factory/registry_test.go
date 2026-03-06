package factory

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/happyhackingspace/sindoq/internal/provider"
	"github.com/happyhackingspace/sindoq/pkg/executor"
	"github.com/happyhackingspace/sindoq/pkg/fs"
)

type testProvider struct {
	name   string
	closed bool
}

func (p *testProvider) Name() string { return p.name }
func (p *testProvider) Create(ctx context.Context, opts *provider.CreateOptions) (provider.Instance, error) {
	return &testInstance{id: "test-123"}, nil
}
func (p *testProvider) Capabilities() provider.Capabilities {
	return provider.Capabilities{SupportedLanguages: []string{"Python"}}
}
func (p *testProvider) Validate(ctx context.Context) error { return nil }
func (p *testProvider) Close() error {
	p.closed = true
	return nil
}

type testInstance struct {
	id string
}

func (i *testInstance) ID() string       { return i.id }
func (i *testInstance) Provider() string { return "test" }
func (i *testInstance) Status(ctx context.Context) (provider.InstanceStatus, error) {
	return provider.StatusRunning, nil
}
func (i *testInstance) Execute(ctx context.Context, code string, opts *executor.ExecutionOptions) (*executor.ExecutionResult, error) {
	return &executor.ExecutionResult{ExitCode: 0}, nil
}
func (i *testInstance) ExecuteStream(ctx context.Context, code string, opts *executor.ExecutionOptions, handler executor.StreamHandler) error {
	return nil
}
func (i *testInstance) RunCommand(ctx context.Context, cmd string, args []string) (*executor.CommandResult, error) {
	return &executor.CommandResult{ExitCode: 0}, nil
}
func (i *testInstance) FileSystem() fs.FileSystem      { return nil }
func (i *testInstance) Network() provider.Network      { return nil }
func (i *testInstance) Stop(ctx context.Context) error { return nil }

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry() returned nil")
	}
	if r.constructors == nil {
		t.Error("constructors map should be initialized")
	}
	if r.providers == nil {
		t.Error("providers map should be initialized")
	}
}

func TestRegistryRegister(t *testing.T) {
	r := NewRegistry()

	r.Register("test", func(config any) (provider.Provider, error) {
		return &testProvider{name: "test"}, nil
	})

	if !r.IsRegistered("test") {
		t.Error("provider should be registered")
	}
}

func TestRegistryUnregister(t *testing.T) {
	r := NewRegistry()

	tp := &testProvider{name: "test"}
	r.Register("test", func(config any) (provider.Provider, error) {
		return tp, nil
	})

	// Create provider to cache it
	_, err := r.Get("test", nil)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	r.Unregister("test")

	if r.IsRegistered("test") {
		t.Error("provider should not be registered after Unregister")
	}
	if !tp.closed {
		t.Error("cached provider should be closed")
	}
}

func TestRegistryGet(t *testing.T) {
	r := NewRegistry()

	callCount := 0
	r.Register("test", func(config any) (provider.Provider, error) {
		callCount++
		return &testProvider{name: "test"}, nil
	})

	// First call creates provider
	p1, err := r.Get("test", nil)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if callCount != 1 {
		t.Errorf("constructor called %d times, want 1", callCount)
	}

	// Second call returns cached provider
	p2, err := r.Get("test", nil)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if callCount != 1 {
		t.Errorf("constructor called %d times after cache, want 1", callCount)
	}

	if p1 != p2 {
		t.Error("should return same cached provider")
	}
}

func TestRegistryGetNotRegistered(t *testing.T) {
	r := NewRegistry()

	_, err := r.Get("nonexistent", nil)
	if err == nil {
		t.Error("Get() should fail for unregistered provider")
	}
}

func TestRegistryGetConstructorError(t *testing.T) {
	r := NewRegistry()

	r.Register("failing", func(config any) (provider.Provider, error) {
		return nil, errors.New("construction failed")
	})

	_, err := r.Get("failing", nil)
	if err == nil {
		t.Error("Get() should fail when constructor fails")
	}
}

func TestRegistryGetConstructor(t *testing.T) {
	r := NewRegistry()

	constructor := func(config any) (provider.Provider, error) {
		return &testProvider{name: "test"}, nil
	}
	r.Register("test", constructor)

	got, ok := r.GetConstructor("test")
	if !ok {
		t.Error("GetConstructor() should succeed")
	}
	if got == nil {
		t.Error("constructor should not be nil")
	}

	_, ok = r.GetConstructor("nonexistent")
	if ok {
		t.Error("GetConstructor() should fail for unregistered provider")
	}
}

func TestRegistryAvailable(t *testing.T) {
	r := NewRegistry()

	r.Register("one", func(config any) (provider.Provider, error) {
		return &testProvider{name: "one"}, nil
	})
	r.Register("two", func(config any) (provider.Provider, error) {
		return &testProvider{name: "two"}, nil
	})

	available := r.Available()
	if len(available) != 2 {
		t.Errorf("Available() = %d providers, want 2", len(available))
	}

	names := make(map[string]bool)
	for _, n := range available {
		names[n] = true
	}
	if !names["one"] || !names["two"] {
		t.Error("Available() should contain all registered providers")
	}
}

func TestRegistryIsRegistered(t *testing.T) {
	r := NewRegistry()

	r.Register("test", func(config any) (provider.Provider, error) {
		return &testProvider{name: "test"}, nil
	})

	if !r.IsRegistered("test") {
		t.Error("IsRegistered() should return true for registered provider")
	}
	if r.IsRegistered("nonexistent") {
		t.Error("IsRegistered() should return false for unregistered provider")
	}
}

func TestRegistryClose(t *testing.T) {
	r := NewRegistry()

	tp1 := &testProvider{name: "one"}
	tp2 := &testProvider{name: "two"}

	r.Register("one", func(config any) (provider.Provider, error) {
		return tp1, nil
	})
	r.Register("two", func(config any) (provider.Provider, error) {
		return tp2, nil
	})

	// Create providers to cache them
	_, _ = r.Get("one", nil)
	_, _ = r.Get("two", nil)

	err := r.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if !tp1.closed || !tp2.closed {
		t.Error("all cached providers should be closed")
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	r := NewRegistry()

	r.Register("test", func(config any) (provider.Provider, error) {
		return &testProvider{name: "test"}, nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.Get("test", nil)
			if err != nil {
				t.Errorf("Get() error = %v", err)
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent access timed out")
	}
}

func TestDefaultRegistryFunctions(t *testing.T) {
	// Save original state
	origRegistry := DefaultRegistry
	defer func() {
		DefaultRegistry = origRegistry
	}()

	// Create fresh registry
	DefaultRegistry = NewRegistry()

	Register("test", func(config any) (provider.Provider, error) {
		return &testProvider{name: "test"}, nil
	})

	if !IsRegistered("test") {
		t.Error("IsRegistered() should return true")
	}

	p, err := Get("test", nil)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if p.Name() != "test" {
		t.Errorf("Name() = %q, want %q", p.Name(), "test")
	}

	available := Available()
	if len(available) != 1 {
		t.Errorf("Available() = %d, want 1", len(available))
	}

	Unregister("test")
	if IsRegistered("test") {
		t.Error("IsRegistered() should return false after Unregister")
	}
}
