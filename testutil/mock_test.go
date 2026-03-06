package testutil

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/happyhackingspace/sindoq/internal/provider"
	"github.com/happyhackingspace/sindoq/pkg/executor"
)

func TestMockProvider_Create(t *testing.T) {
	p := NewMockProvider("test")

	ctx := context.Background()
	instance, err := p.Create(ctx, nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if instance == nil {
		t.Fatal("Create() returned nil instance")
	}

	if instance.ID() == "" {
		t.Error("Instance ID should not be empty")
	}

	// Verify instance is tracked
	instances := p.Instances()
	if len(instances) != 1 {
		t.Errorf("Expected 1 instance, got %d", len(instances))
	}
}

func TestMockProvider_Name(t *testing.T) {
	p := NewMockProvider("my-provider")
	if p.Name() != "my-provider" {
		t.Errorf("Name() = %q, want %q", p.Name(), "my-provider")
	}
}

func TestMockProvider_Capabilities(t *testing.T) {
	p := NewMockProvider("test")
	caps := p.Capabilities()

	if !caps.SupportsStreaming {
		t.Error("Default capabilities should support streaming")
	}
	if len(caps.SupportedLanguages) == 0 {
		t.Error("Default capabilities should have supported languages")
	}
}

func TestMockProvider_SetCapabilities(t *testing.T) {
	p := NewMockProvider("test")
	newCaps := provider.Capabilities{
		SupportsStreaming:  false,
		MaxMemoryMB:        1024,
		SupportedLanguages: []string{"CustomLang"},
	}
	p.SetCapabilities(newCaps)

	caps := p.Capabilities()
	if caps.SupportsStreaming {
		t.Error("Capabilities should be updated")
	}
	if caps.MaxMemoryMB != 1024 {
		t.Error("MaxMemoryMB should be 1024")
	}
}

func TestMockProvider_Hooks(t *testing.T) {
	p := NewMockProvider("test")

	// Test OnCreate hook
	expectedErr := errors.New("create error")
	p.OnCreate = func(ctx context.Context, opts *provider.CreateOptions) (provider.Instance, error) {
		return nil, expectedErr
	}

	_, err := p.Create(context.Background(), nil)
	if err != expectedErr {
		t.Errorf("Expected error %v, got %v", expectedErr, err)
	}

	// Test OnValidate hook
	p.OnValidate = func(ctx context.Context) error {
		return expectedErr
	}
	if err := p.Validate(context.Background()); err != expectedErr {
		t.Errorf("Expected error %v, got %v", expectedErr, err)
	}

	// Test OnClose hook
	p.OnClose = func() error {
		return expectedErr
	}
	if err := p.Close(); err != expectedErr {
		t.Errorf("Expected error %v, got %v", expectedErr, err)
	}
}

func TestMockInstance_Execute(t *testing.T) {
	p := NewMockProvider("test")
	inst, _ := p.Create(context.Background(), nil)
	mockInst := inst.(*MockInstance)

	// Default execution
	result, err := inst.Execute(context.Background(), "print('hello')", executor.DefaultExecutionOptions())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("Default execution should have exit code 0, got %d", result.ExitCode)
	}

	// Check execution was recorded
	if len(mockInst.Executions) != 1 {
		t.Errorf("Expected 1 execution record, got %d", len(mockInst.Executions))
	}
	if mockInst.Executions[0].Code != "print('hello')" {
		t.Errorf("Execution record code mismatch")
	}
}

func TestMockInstance_SetExecuteResult(t *testing.T) {
	p := NewMockProvider("test")
	inst, _ := p.Create(context.Background(), nil)
	mockInst := inst.(*MockInstance)

	// Configure custom result
	mockInst.SetExecuteResult("custom stdout", "custom stderr", 42)

	result, err := inst.Execute(context.Background(), "code", nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Stdout != "custom stdout" {
		t.Errorf("Stdout = %q, want %q", result.Stdout, "custom stdout")
	}
	if result.Stderr != "custom stderr" {
		t.Errorf("Stderr = %q, want %q", result.Stderr, "custom stderr")
	}
	if result.ExitCode != 42 {
		t.Errorf("ExitCode = %d, want %d", result.ExitCode, 42)
	}
}

func TestMockInstance_SetExecuteError(t *testing.T) {
	p := NewMockProvider("test")
	inst, _ := p.Create(context.Background(), nil)
	mockInst := inst.(*MockInstance)

	expectedErr := errors.New("execution failed")
	mockInst.SetExecuteError(expectedErr)

	_, err := inst.Execute(context.Background(), "code", nil)
	if err != expectedErr {
		t.Errorf("Expected error %v, got %v", expectedErr, err)
	}
}

func TestMockInstance_RunCommand(t *testing.T) {
	p := NewMockProvider("test")
	inst, _ := p.Create(context.Background(), nil)
	mockInst := inst.(*MockInstance)

	result, err := inst.RunCommand(context.Background(), "ls", []string{"-la"})
	if err != nil {
		t.Fatalf("RunCommand() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("Default command should have exit code 0")
	}

	// Check command was recorded
	if len(mockInst.Commands) != 1 {
		t.Errorf("Expected 1 command record, got %d", len(mockInst.Commands))
	}
	if mockInst.Commands[0].Command != "ls" {
		t.Errorf("Command record mismatch")
	}
}

func TestMockInstance_Stop(t *testing.T) {
	p := NewMockProvider("test")
	inst, _ := p.Create(context.Background(), nil)

	// Initial status should be running
	status, _ := inst.Status(context.Background())
	if status != provider.StatusRunning {
		t.Errorf("Initial status = %v, want %v", status, provider.StatusRunning)
	}

	// Stop the instance
	if err := inst.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Status should now be stopped
	status, _ = inst.Status(context.Background())
	if status != provider.StatusStopped {
		t.Errorf("After stop status = %v, want %v", status, provider.StatusStopped)
	}
}

func TestMockFileSystem(t *testing.T) {
	fs := NewMockFileSystem()
	ctx := context.Background()

	// Write file
	if err := fs.Write(ctx, "/test.txt", []byte("hello")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Check exists
	exists, err := fs.Exists(ctx, "/test.txt")
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if !exists {
		t.Error("File should exist after Write")
	}

	// Read file
	data, err := fs.Read(ctx, "/test.txt")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("Read() = %q, want %q", string(data), "hello")
	}

	// Delete file
	if err := fs.Delete(ctx, "/test.txt"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Should not exist after delete
	exists, _ = fs.Exists(ctx, "/test.txt")
	if exists {
		t.Error("File should not exist after Delete")
	}

	// Read non-existent file should error
	_, err = fs.Read(ctx, "/nonexistent.txt")
	if err == nil {
		t.Error("Read() of non-existent file should error")
	}
}

func TestMockFileSystem_SetFile(t *testing.T) {
	fs := NewMockFileSystem()

	// Use SetFile helper
	fs.SetFile("/preset.txt", []byte("preset content"))

	data, ok := fs.GetFile("/preset.txt")
	if !ok {
		t.Error("GetFile() should find preset file")
	}
	if string(data) != "preset content" {
		t.Errorf("GetFile() = %q, want %q", string(data), "preset content")
	}
}

func TestMockFileSystem_Copy(t *testing.T) {
	fs := NewMockFileSystem()
	ctx := context.Background()

	fs.SetFile("/src.txt", []byte("source content"))

	if err := fs.Copy(ctx, "/src.txt", "/dst.txt"); err != nil {
		t.Fatalf("Copy() error = %v", err)
	}

	data, _ := fs.Read(ctx, "/dst.txt")
	if string(data) != "source content" {
		t.Errorf("Copied content = %q, want %q", string(data), "source content")
	}
}

func TestMockFileSystem_Move(t *testing.T) {
	fs := NewMockFileSystem()
	ctx := context.Background()

	fs.SetFile("/src.txt", []byte("source content"))

	if err := fs.Move(ctx, "/src.txt", "/dst.txt"); err != nil {
		t.Fatalf("Move() error = %v", err)
	}

	// Dst should exist
	exists, _ := fs.Exists(ctx, "/dst.txt")
	if !exists {
		t.Error("Destination should exist after Move")
	}

	// Src should not exist
	exists, _ = fs.Exists(ctx, "/src.txt")
	if exists {
		t.Error("Source should not exist after Move")
	}
}

func TestMockNetwork(t *testing.T) {
	n := NewMockNetwork()
	ctx := context.Background()

	// Publish port
	published, err := n.PublishPort(ctx, 8080)
	if err != nil {
		t.Fatalf("PublishPort() error = %v", err)
	}
	if published.LocalPort != 8080 {
		t.Errorf("PublishedPort.LocalPort = %d, want %d", published.LocalPort, 8080)
	}

	// Get public URL
	url, err := n.GetPublicURL(8080)
	if err != nil {
		t.Fatalf("GetPublicURL() error = %v", err)
	}
	if url == "" {
		t.Error("GetPublicURL() should return non-empty URL")
	}

	// List ports
	ports, err := n.ListPorts(ctx)
	if err != nil {
		t.Fatalf("ListPorts() error = %v", err)
	}
	if len(ports) != 1 {
		t.Errorf("Expected 1 port, got %d", len(ports))
	}

	// Unpublish port
	if err := n.UnpublishPort(ctx, 8080); err != nil {
		t.Fatalf("UnpublishPort() error = %v", err)
	}

	ports, _ = n.ListPorts(ctx)
	if len(ports) != 0 {
		t.Error("Port should be removed after UnpublishPort")
	}
}

func TestMockNetwork_SetPort(t *testing.T) {
	n := NewMockNetwork()

	n.SetPort(3000, "http://custom.url:3000")

	url, err := n.GetPublicURL(3000)
	if err != nil {
		t.Fatalf("GetPublicURL() error = %v", err)
	}
	if url != "http://custom.url:3000" {
		t.Errorf("GetPublicURL() = %q, want %q", url, "http://custom.url:3000")
	}
}

func TestMockInstance_ExecuteStream(t *testing.T) {
	p := NewMockProvider("test")
	inst, _ := p.Create(context.Background(), nil)
	mockInst := inst.(*MockInstance)

	// Configure result for streaming
	mockInst.SetExecuteResult("stream output", "", 0)

	var events []*executor.StreamEvent
	handler := func(e *executor.StreamEvent) error {
		events = append(events, e)
		return nil
	}

	err := inst.ExecuteStream(context.Background(), "code", nil, handler)
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	// Should have stdout and complete events
	if len(events) < 2 {
		t.Errorf("Expected at least 2 events, got %d", len(events))
	}

	// Last event should be complete
	lastEvent := events[len(events)-1]
	if lastEvent.Type != executor.StreamComplete {
		t.Errorf("Last event type = %v, want %v", lastEvent.Type, executor.StreamComplete)
	}
}

func TestMockInstance_FileSystem(t *testing.T) {
	p := NewMockProvider("test")
	inst, _ := p.Create(context.Background(), nil)

	fs := inst.FileSystem()
	if fs == nil {
		t.Error("FileSystem() should not return nil")
	}

	// Test that it works
	ctx := context.Background()
	if err := fs.Write(ctx, "/test.txt", []byte("test")); err != nil {
		t.Errorf("Write() error = %v", err)
	}
}

func TestMockInstance_Network(t *testing.T) {
	p := NewMockProvider("test")
	inst, _ := p.Create(context.Background(), nil)

	net := inst.Network()
	if net == nil {
		t.Error("Network() should not return nil")
	}

	// Test that it works
	ctx := context.Background()
	_, err := net.PublishPort(ctx, 8080)
	if err != nil {
		t.Errorf("PublishPort() error = %v", err)
	}
}

func TestMockProvider_Validate(t *testing.T) {
	p := NewMockProvider("test")

	// Default validate should succeed
	if err := p.Validate(context.Background()); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestMockProvider_Close(t *testing.T) {
	p := NewMockProvider("test")

	// Default close should succeed
	if err := p.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestMockInstance_Status(t *testing.T) {
	p := NewMockProvider("test")
	inst, _ := p.Create(context.Background(), nil)
	mockInst := inst.(*MockInstance)

	// Set custom status
	mockInst.SetStatus(provider.StatusError)

	status, err := inst.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status != provider.StatusError {
		t.Errorf("Status() = %v, want %v", status, provider.StatusError)
	}
}

func TestMockFileSystem_Clear(t *testing.T) {
	fs := NewMockFileSystem()
	ctx := context.Background()

	fs.SetFile("/a.txt", []byte("a"))
	fs.SetFile("/b.txt", []byte("b"))

	fs.Clear()

	exists, _ := fs.Exists(ctx, "/a.txt")
	if exists {
		t.Error("Files should be cleared")
	}
}

func TestMockNetwork_Clear(t *testing.T) {
	n := NewMockNetwork()
	ctx := context.Background()

	n.SetPort(8080, "http://localhost:8080")
	n.Clear()

	ports, _ := n.ListPorts(ctx)
	if len(ports) != 0 {
		t.Error("Ports should be cleared")
	}
}

func TestMockInstance_Concurrent(t *testing.T) {
	p := NewMockProvider("test")
	inst, _ := p.Create(context.Background(), nil)

	// Run concurrent executions
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = inst.Execute(context.Background(), "code", nil)
			done <- true
		}()
	}

	// Wait for all
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Concurrent execution timeout")
		}
	}
}
