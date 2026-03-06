package testutil

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/happyhackingspace/sindoq/internal/provider"
	"github.com/happyhackingspace/sindoq/pkg/executor"
	"github.com/happyhackingspace/sindoq/pkg/fs"
)

var instanceCounter uint64

// MockInstance is a configurable mock implementation of provider.Instance.
type MockInstance struct {
	id         string
	provider   *MockProvider
	status     provider.InstanceStatus
	filesystem *MockFileSystem
	network    *MockNetwork
	mu         sync.RWMutex

	// Execution history for assertions
	Executions []ExecutionRecord
	Commands   []CommandRecord

	// Hooks for testing
	OnExecute       func(ctx context.Context, code string, opts *executor.ExecutionOptions) (*executor.ExecutionResult, error)
	OnExecuteStream func(ctx context.Context, code string, opts *executor.ExecutionOptions, handler executor.StreamHandler) error
	OnRunCommand    func(ctx context.Context, cmd string, args []string) (*executor.CommandResult, error)
	OnStop          func(ctx context.Context) error
}

// ExecutionRecord records an execution for testing assertions.
type ExecutionRecord struct {
	Code     string
	Options  *executor.ExecutionOptions
	Result   *executor.ExecutionResult
	Error    error
	Duration time.Duration
}

// CommandRecord records a command execution for testing assertions.
type CommandRecord struct {
	Command  string
	Args     []string
	Result   *executor.CommandResult
	Error    error
	Duration time.Duration
}

// NewMockInstance creates a new mock instance.
func NewMockInstance(p *MockProvider) *MockInstance {
	id := atomic.AddUint64(&instanceCounter, 1)
	return &MockInstance{
		id:         generateMockID(id),
		provider:   p,
		status:     provider.StatusRunning,
		filesystem: NewMockFileSystem(),
		network:    NewMockNetwork(),
		Executions: make([]ExecutionRecord, 0),
		Commands:   make([]CommandRecord, 0),
	}
}

func generateMockID(n uint64) string {
	return "mock-" + string(rune('a'+n%26)) + string(rune('0'+n%10))
}

// ID returns the instance ID.
func (i *MockInstance) ID() string {
	return i.id
}

// Execute runs code in the mock instance.
func (i *MockInstance) Execute(ctx context.Context, code string, opts *executor.ExecutionOptions) (*executor.ExecutionResult, error) {
	start := time.Now()

	if i.OnExecute != nil {
		result, err := i.OnExecute(ctx, code, opts)
		i.recordExecution(code, opts, result, err, time.Since(start))
		return result, err
	}

	// Default behavior: return successful empty result
	var lang string
	if opts != nil {
		lang = opts.Language
	}
	result := &executor.ExecutionResult{
		ExitCode: 0,
		Stdout:   "",
		Stderr:   "",
		Duration: time.Since(start),
		Language: lang,
	}

	i.recordExecution(code, opts, result, nil, time.Since(start))
	return result, nil
}

func (i *MockInstance) recordExecution(code string, opts *executor.ExecutionOptions, result *executor.ExecutionResult, err error, duration time.Duration) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.Executions = append(i.Executions, ExecutionRecord{
		Code:     code,
		Options:  opts,
		Result:   result,
		Error:    err,
		Duration: duration,
	})
}

// ExecuteStream runs code with streaming output.
func (i *MockInstance) ExecuteStream(ctx context.Context, code string, opts *executor.ExecutionOptions, handler executor.StreamHandler) error {
	if i.OnExecuteStream != nil {
		return i.OnExecuteStream(ctx, code, opts, handler)
	}

	// Default behavior: execute and emit events
	result, err := i.Execute(ctx, code, opts)
	if err != nil {
		_ = handler(&executor.StreamEvent{
			Type:      executor.StreamError,
			Error:     err,
			Timestamp: time.Now(),
		})
		return err
	}

	if result.Stdout != "" {
		_ = handler(&executor.StreamEvent{
			Type:      executor.StreamStdout,
			Data:      result.Stdout,
			Timestamp: time.Now(),
		})
	}

	if result.Stderr != "" {
		_ = handler(&executor.StreamEvent{
			Type:      executor.StreamStderr,
			Data:      result.Stderr,
			Timestamp: time.Now(),
		})
	}

	_ = handler(&executor.StreamEvent{
		Type:      executor.StreamComplete,
		ExitCode:  result.ExitCode,
		Timestamp: time.Now(),
	})

	return nil
}

// RunCommand executes a shell command.
func (i *MockInstance) RunCommand(ctx context.Context, cmd string, args []string) (*executor.CommandResult, error) {
	start := time.Now()

	if i.OnRunCommand != nil {
		result, err := i.OnRunCommand(ctx, cmd, args)
		i.recordCommand(cmd, args, result, err, time.Since(start))
		return result, err
	}

	// Default behavior: return successful result
	result := &executor.CommandResult{
		ExitCode: 0,
		Stdout:   "",
		Stderr:   "",
		Duration: time.Since(start),
	}

	i.recordCommand(cmd, args, result, nil, time.Since(start))
	return result, nil
}

func (i *MockInstance) recordCommand(cmd string, args []string, result *executor.CommandResult, err error, duration time.Duration) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.Commands = append(i.Commands, CommandRecord{
		Command:  cmd,
		Args:     args,
		Result:   result,
		Error:    err,
		Duration: duration,
	})
}

// FileSystem returns the mock filesystem.
func (i *MockInstance) FileSystem() fs.FileSystem {
	return i.filesystem
}

// Network returns the mock network.
func (i *MockInstance) Network() provider.Network {
	return i.network
}

// Stop terminates the mock instance.
func (i *MockInstance) Stop(ctx context.Context) error {
	if i.OnStop != nil {
		return i.OnStop(ctx)
	}

	i.mu.Lock()
	defer i.mu.Unlock()
	i.status = provider.StatusStopped
	return nil
}

// Status returns the current status.
func (i *MockInstance) Status(ctx context.Context) (provider.InstanceStatus, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.status, nil
}

// SetStatus allows tests to set the instance status.
func (i *MockInstance) SetStatus(status provider.InstanceStatus) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.status = status
}

// SetExecuteResult configures the mock to return specific results.
func (i *MockInstance) SetExecuteResult(stdout, stderr string, exitCode int) {
	i.OnExecute = func(ctx context.Context, code string, opts *executor.ExecutionOptions) (*executor.ExecutionResult, error) {
		var lang string
		if opts != nil {
			lang = opts.Language
		}
		return &executor.ExecutionResult{
			ExitCode: exitCode,
			Stdout:   stdout,
			Stderr:   stderr,
			Duration: time.Millisecond * 10,
			Language: lang,
		}, nil
	}
}

// SetExecuteError configures the mock to return an error.
func (i *MockInstance) SetExecuteError(err error) {
	i.OnExecute = func(ctx context.Context, code string, opts *executor.ExecutionOptions) (*executor.ExecutionResult, error) {
		return nil, err
	}
}

var _ provider.Instance = (*MockInstance)(nil)
