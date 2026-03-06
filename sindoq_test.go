package sindoq

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/happyhackingspace/sindoq/internal/factory"
	"github.com/happyhackingspace/sindoq/internal/provider"
	"github.com/happyhackingspace/sindoq/pkg/executor"
	"github.com/happyhackingspace/sindoq/pkg/fs"
)

// mockProvider implements provider.Provider for testing
type mockProvider struct {
	name      string
	createErr error
	instance  *mockInstance
}

func (p *mockProvider) Name() string { return p.name }

func (p *mockProvider) Create(ctx context.Context, opts *provider.CreateOptions) (provider.Instance, error) {
	if p.createErr != nil {
		return nil, p.createErr
	}
	if p.instance == nil {
		p.instance = &mockInstance{
			id:     "test-instance-123",
			status: provider.StatusRunning,
		}
	}
	return p.instance, nil
}

func (p *mockProvider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		SupportsStreaming:  true,
		SupportedLanguages: []string{"Python", "JavaScript", "Go"},
	}
}

func (p *mockProvider) Validate(ctx context.Context) error { return nil }
func (p *mockProvider) Close() error                       { return nil }

// mockInstance implements provider.Instance for testing
type mockInstance struct {
	id         string
	status     provider.InstanceStatus
	execResult *executor.ExecutionResult
	execErr    error
	stopErr    error
	stopped    bool
}

func (i *mockInstance) ID() string       { return i.id }
func (i *mockInstance) Provider() string { return "mock" }
func (i *mockInstance) Status(ctx context.Context) (provider.InstanceStatus, error) {
	return i.status, nil
}

func (i *mockInstance) Execute(ctx context.Context, code string, opts *executor.ExecutionOptions) (*executor.ExecutionResult, error) {
	if i.execErr != nil {
		return nil, i.execErr
	}
	if i.execResult != nil {
		return i.execResult, nil
	}
	return &executor.ExecutionResult{
		ExitCode: 0,
		Stdout:   "Hello, World!\n",
		Language: opts.Language,
		Duration: 100 * time.Millisecond,
	}, nil
}

func (i *mockInstance) ExecuteStream(ctx context.Context, code string, opts *executor.ExecutionOptions, handler executor.StreamHandler) error {
	_ = handler(&executor.StreamEvent{Type: executor.StreamStdout, Data: "Hello"})
	_ = handler(&executor.StreamEvent{Type: executor.StreamComplete, ExitCode: 0})
	return nil
}

func (i *mockInstance) RunCommand(ctx context.Context, cmd string, args []string) (*executor.CommandResult, error) {
	return &executor.CommandResult{ExitCode: 0, Stdout: "ok"}, nil
}

func (i *mockInstance) FileSystem() fs.FileSystem { return nil }
func (i *mockInstance) Network() provider.Network { return nil }

func (i *mockInstance) Stop(ctx context.Context) error {
	if i.stopErr != nil {
		return i.stopErr
	}
	i.stopped = true
	i.status = provider.StatusStopped
	return nil
}

func setupMockProvider(t *testing.T) func() {
	t.Helper()

	mp := &mockProvider{name: "mock"}
	factory.Register("mock", func(config any) (provider.Provider, error) {
		return mp, nil
	})

	return func() {
		factory.Unregister("mock")
	}
}

func TestCreate(t *testing.T) {
	cleanup := setupMockProvider(t)
	defer cleanup()

	ctx := context.Background()
	sb, err := Create(ctx, WithProvider("mock"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer func() { _ = sb.Stop(ctx) }()

	if sb.ID() == "" {
		t.Error("ID() should not be empty")
	}
	if sb.Provider() != "mock" {
		t.Errorf("Provider() = %q, want %q", sb.Provider(), "mock")
	}
}

func TestCreateWithOptions(t *testing.T) {
	cleanup := setupMockProvider(t)
	defer cleanup()

	ctx := context.Background()
	sb, err := Create(ctx,
		WithProvider("mock"),
		WithTimeout(5*time.Minute),
		WithRuntime("Python"),
	)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer func() { _ = sb.Stop(ctx) }()

	if sb.ID() == "" {
		t.Error("ID() should not be empty")
	}
}

func TestCreateError(t *testing.T) {
	mp := &mockProvider{name: "failing", createErr: errors.New("connection failed")}
	factory.Register("failing", func(config any) (provider.Provider, error) {
		return mp, nil
	})
	defer factory.Unregister("failing")

	ctx := context.Background()
	_, err := Create(ctx, WithProvider("failing"))
	if err == nil {
		t.Fatal("Create() should fail")
	}
}

func TestCreateUnregisteredProvider(t *testing.T) {
	ctx := context.Background()
	_, err := Create(ctx, WithProvider("nonexistent"))
	if err == nil {
		t.Fatal("Create() should fail for unregistered provider")
	}
}

func TestMustCreate(t *testing.T) {
	cleanup := setupMockProvider(t)
	defer cleanup()

	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("MustCreate() panicked unexpectedly: %v", r)
		}
	}()

	sb := MustCreate(ctx, WithProvider("mock"))
	defer func() { _ = sb.Stop(ctx) }()

	if sb == nil {
		t.Error("MustCreate() returned nil")
	}
}

func TestMustCreatePanic(t *testing.T) {
	ctx := context.Background()

	defer func() {
		if r := recover(); r == nil {
			t.Error("MustCreate() should panic for unregistered provider")
		}
	}()

	MustCreate(ctx, WithProvider("definitely-not-registered"))
}

func TestSandboxExecute(t *testing.T) {
	cleanup := setupMockProvider(t)
	defer cleanup()

	ctx := context.Background()
	sb, err := Create(ctx, WithProvider("mock"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer func() { _ = sb.Stop(ctx) }()

	result, err := sb.Execute(ctx, `print("Hello")`, WithLanguage("Python"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if result.Stdout == "" {
		t.Error("Stdout should not be empty")
	}
	if result.Language != "Python" {
		t.Errorf("Language = %q, want %q", result.Language, "Python")
	}
}

func TestSandboxExecuteAutoDetect(t *testing.T) {
	cleanup := setupMockProvider(t)
	defer cleanup()

	ctx := context.Background()
	sb, err := Create(ctx, WithProvider("mock"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer func() { _ = sb.Stop(ctx) }()

	// Python code should auto-detect
	code := `import json
def main():
    print(json.dumps({"hello": "world"}))
main()`

	result, err := sb.Execute(ctx, code)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if result.Language == "" {
		t.Error("Language should be auto-detected")
	}
}

func TestSandboxExecuteAfterStop(t *testing.T) {
	cleanup := setupMockProvider(t)
	defer cleanup()

	ctx := context.Background()
	sb, err := Create(ctx, WithProvider("mock"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_ = sb.Stop(ctx)

	_, err = sb.Execute(ctx, `print("Hello")`, WithLanguage("Python"))
	if err == nil {
		t.Error("Execute() should fail after Stop()")
	}
	if !errors.Is(err, ErrSandboxStopped) {
		t.Errorf("error should be ErrSandboxStopped, got %v", err)
	}
}

func TestSandboxExecuteAsync(t *testing.T) {
	cleanup := setupMockProvider(t)
	defer cleanup()

	ctx := context.Background()
	sb, err := Create(ctx, WithProvider("mock"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer func() { _ = sb.Stop(ctx) }()

	results, err := sb.ExecuteAsync(ctx, `print("Hello")`, WithLanguage("Python"))
	if err != nil {
		t.Fatalf("ExecuteAsync() error = %v", err)
	}

	result := <-results
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestSandboxExecuteStream(t *testing.T) {
	cleanup := setupMockProvider(t)
	defer cleanup()

	ctx := context.Background()
	sb, err := Create(ctx, WithProvider("mock"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer func() { _ = sb.Stop(ctx) }()

	var events []*executor.StreamEvent
	err = sb.ExecuteStream(ctx, `print("Hello")`, func(e *executor.StreamEvent) error {
		events = append(events, e)
		return nil
	}, WithLanguage("Python"))
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	if len(events) == 0 {
		t.Error("should receive stream events")
	}
}

func TestSandboxRunCommand(t *testing.T) {
	cleanup := setupMockProvider(t)
	defer cleanup()

	ctx := context.Background()
	sb, err := Create(ctx, WithProvider("mock"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer func() { _ = sb.Stop(ctx) }()

	result, err := sb.RunCommand(ctx, "ls", "-la")
	if err != nil {
		t.Fatalf("RunCommand() error = %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestSandboxStatus(t *testing.T) {
	cleanup := setupMockProvider(t)
	defer cleanup()

	ctx := context.Background()
	sb, err := Create(ctx, WithProvider("mock"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	status, err := sb.Status(ctx)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status != provider.StatusRunning {
		t.Errorf("Status() = %v, want %v", status, provider.StatusRunning)
	}

	_ = sb.Stop(ctx)

	status, err = sb.Status(ctx)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status != provider.StatusStopped {
		t.Errorf("Status() after stop = %v, want %v", status, provider.StatusStopped)
	}
}

func TestSandboxStopIdempotent(t *testing.T) {
	cleanup := setupMockProvider(t)
	defer cleanup()

	ctx := context.Background()
	sb, err := Create(ctx, WithProvider("mock"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// First stop
	err = sb.Stop(ctx)
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Second stop should be idempotent
	err = sb.Stop(ctx)
	if err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
}

func TestExecuteConvenience(t *testing.T) {
	cleanup := setupMockProvider(t)
	defer cleanup()

	ctx := context.Background()
	// Use recognizable Python code for auto-detection
	code := `import json
def main():
    print(json.dumps({"hello": "world"}))
main()`
	result, err := Execute(ctx, code, WithProvider("mock"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestExecuteStreamConvenience(t *testing.T) {
	cleanup := setupMockProvider(t)
	defer cleanup()

	ctx := context.Background()
	// Use recognizable Python code for auto-detection
	code := `import json
def main():
    print(json.dumps({"hello": "world"}))
main()`
	var output string
	err := ExecuteStream(ctx, code, func(e *executor.StreamEvent) error {
		if e.Type == executor.StreamStdout {
			output += e.Data
		}
		return nil
	}, WithProvider("mock"))
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	if output == "" {
		t.Error("should receive output")
	}
}

func TestListProviders(t *testing.T) {
	cleanup := setupMockProvider(t)
	defer cleanup()

	providers := ListProviders()
	found := false
	for _, p := range providers {
		if p == "mock" {
			found = true
			break
		}
	}
	if !found {
		t.Error("ListProviders() should include mock provider")
	}
}

func TestProviderCapabilities(t *testing.T) {
	cleanup := setupMockProvider(t)
	defer cleanup()

	caps, err := ProviderCapabilities("mock")
	if err != nil {
		t.Fatalf("ProviderCapabilities() error = %v", err)
	}

	if !caps.SupportsStreaming {
		t.Error("SupportsStreaming should be true")
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		code     string
		filename string
		want     string
	}{
		{`print("Hello")`, "main.py", "Python"},
		{`console.log("Hello")`, "app.js", "JavaScript"},
		{`package main`, "main.go", "Go"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			result := DetectLanguage(tt.code, tt.filename)
			if result.Language != tt.want {
				t.Errorf("DetectLanguage() = %q, want %q", result.Language, tt.want)
			}
		})
	}
}

func TestSupportedLanguages(t *testing.T) {
	langs := SupportedLanguages()
	if len(langs) == 0 {
		t.Error("SupportedLanguages() should not be empty")
	}

	foundPython := false
	for _, lang := range langs {
		if lang == "Python" {
			foundPython = true
			break
		}
	}
	if !foundPython {
		t.Error("SupportedLanguages() should include Python")
	}
}

func TestGetRuntimeInfo(t *testing.T) {
	info, ok := GetRuntimeInfo("Python")
	if !ok {
		t.Fatal("GetRuntimeInfo(Python) should succeed")
	}

	if info.Runtime == "" {
		t.Error("Runtime should not be empty")
	}
	if info.DockerImage == "" {
		t.Error("DockerImage should not be empty")
	}
}
