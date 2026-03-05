// Package sindoq provides a unified SDK for code execution across multiple isolated environments.
//
// Sindoq supports multiple providers including Docker, Vercel Sandbox, E2B,
// Kubernetes, Podman, and Firecracker. It features automatic language detection, streaming
// output, and async execution support.
//
// Basic usage:
//
//	// Execute code with auto-detected language
//	result, err := sindoq.Execute(ctx, `print("Hello, World!")`)
//
//	// Create a sandbox for multiple executions
//	sb, err := sindoq.Create(ctx, sindoq.WithProvider("docker"))
//	defer sb.Stop(ctx)
//
//	result, err = sb.Execute(ctx, code)
package sindoq

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/happyhackingspace/sindoq/internal/factory"
	"github.com/happyhackingspace/sindoq/internal/provider"
	"github.com/happyhackingspace/sindoq/pkg/event"
	"github.com/happyhackingspace/sindoq/pkg/executor"
	"github.com/happyhackingspace/sindoq/pkg/fs"
	"github.com/happyhackingspace/sindoq/pkg/langdetect"

	// Register cross-platform providers so they are available when using sindoq as a library.
	_ "github.com/happyhackingspace/sindoq/internal/provider/docker"
	_ "github.com/happyhackingspace/sindoq/internal/provider/e2b"
	_ "github.com/happyhackingspace/sindoq/internal/provider/kubernetes"
	_ "github.com/happyhackingspace/sindoq/internal/provider/podman"
	_ "github.com/happyhackingspace/sindoq/internal/provider/vercel"
	_ "github.com/happyhackingspace/sindoq/internal/provider/wasmer"
)

// Sandbox represents an isolated code execution environment.
// This is the primary interface users interact with.
type Sandbox interface {
	// ID returns the unique identifier for this sandbox instance.
	ID() string

	// Provider returns the name of the provider.
	Provider() string

	// Execute runs code and returns the result.
	// Blocks until execution completes.
	Execute(ctx context.Context, code string, opts ...ExecuteOption) (*executor.ExecutionResult, error)

	// ExecuteAsync runs code asynchronously and returns immediately.
	// Results are delivered via the returned channel.
	ExecuteAsync(ctx context.Context, code string, opts ...ExecuteOption) (<-chan *executor.ExecutionResult, error)

	// ExecuteStream runs code with streaming output.
	// The handler receives output events as they occur.
	ExecuteStream(ctx context.Context, code string, handler executor.StreamHandler, opts ...ExecuteOption) error

	// RunCommand executes a shell command in the sandbox.
	RunCommand(ctx context.Context, cmd string, args ...string) (*executor.CommandResult, error)

	// Files returns the file system interface for this sandbox.
	Files() fs.FileSystem

	// Network returns the network interface for this sandbox (may be nil).
	Network() provider.Network

	// Subscribe registers an event callback.
	Subscribe(eventType event.EventType, handler event.EventHandler) (unsubscribe func())

	// Stop terminates the sandbox and releases resources.
	Stop(ctx context.Context) error

	// Status returns the current sandbox status.
	Status(ctx context.Context) (provider.InstanceStatus, error)
}

// sandbox is the concrete implementation of the Sandbox interface.
type sandbox struct {
	instance     provider.Instance
	config       *Config
	detector     *langdetect.Detector
	eventBus     *event.Bus
	mu           sync.RWMutex
	stopped      bool
	providerName string
}

// Create creates a new sandbox with the given options.
// This is the primary entry point for the SDK.
func Create(ctx context.Context, opts ...Option) (Sandbox, error) {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	return createSandbox(ctx, cfg)
}

// MustCreate creates a sandbox or panics on error.
// Use for initialization where errors are fatal.
func MustCreate(ctx context.Context, opts ...Option) Sandbox {
	sb, err := Create(ctx, opts...)
	if err != nil {
		panic(fmt.Sprintf("sindoq: failed to create sandbox: %v", err))
	}
	return sb
}

func createSandbox(ctx context.Context, cfg *Config) (Sandbox, error) {
	// Create provider options
	createOpts := &provider.CreateOptions{
		Image:          cfg.Image,
		Runtime:        cfg.Runtime,
		Resources:      cfg.Resources.ToProviderConfig(),
		Environment:    make(map[string]string),
		Timeout:        cfg.DefaultTimeout,
		WorkDir:        "/workspace",
		InternetAccess: cfg.InternetAccess,
	}

	// Create instance via factory
	instance, err := factory.CreateSandbox(ctx, cfg.Provider, cfg.ProviderConfig, createOpts)
	if err != nil {
		return nil, NewError("create", cfg.Provider, "", err)
	}

	sb := &sandbox{
		instance:     instance,
		config:       cfg,
		detector:     langdetect.New(),
		eventBus:     event.NewBus(),
		providerName: cfg.Provider,
	}

	// Register global event handler if provided
	if cfg.EventHandler != nil {
		sb.eventBus.SubscribeAll(cfg.EventHandler)
	}

	// Emit creation event
	sb.eventBus.Emit(event.NewEvent(event.EventSandboxCreated, instance.ID(), nil))

	return sb, nil
}

// ID returns the unique identifier for this sandbox instance.
func (s *sandbox) ID() string {
	return s.instance.ID()
}

// Provider returns the name of the provider.
func (s *sandbox) Provider() string {
	return s.providerName
}

// Execute runs code and returns the result.
func (s *sandbox) Execute(ctx context.Context, code string, opts ...ExecuteOption) (*executor.ExecutionResult, error) {
	s.mu.RLock()
	if s.stopped {
		s.mu.RUnlock()
		return nil, NewError("execute", s.providerName, s.instance.ID(), ErrSandboxStopped)
	}
	s.mu.RUnlock()

	// Build execution config
	execCfg := DefaultExecuteConfig()
	for _, opt := range opts {
		opt(execCfg)
	}

	// Detect language if not specified
	language := execCfg.Language
	if language == "" && s.config.AutoDetectLanguage {
		result := s.detector.Detect(code, &langdetect.DetectOptions{
			Filename:      execCfg.Filename,
			UseContent:    true,
			UseShebang:    true,
			UseHeuristics: true,
		})
		if result.Language != "" {
			language = result.Language
		} else if s.config.DefaultLanguage != "" {
			language = s.config.DefaultLanguage
		} else {
			return nil, NewError("execute", s.providerName, s.instance.ID(), ErrLanguageDetectionFailed)
		}
	}

	// Build execution options
	execOpts := &executor.ExecutionOptions{
		Language:      language,
		Filename:      execCfg.Filename,
		Timeout:       execCfg.Timeout,
		Env:           execCfg.Env,
		WorkDir:       execCfg.WorkDir,
		Stdin:         execCfg.Stdin,
		Files:         execCfg.Files,
		KeepArtifacts: execCfg.KeepArtifacts,
	}

	// Emit start event
	s.eventBus.Emit(event.NewEvent(event.EventExecutionStarted, s.instance.ID(), &event.ExecutionStartedData{
		Language: language,
		CodeSize: len(code),
	}))

	start := time.Now()

	// Execute
	result, err := s.instance.Execute(ctx, code, execOpts)
	if err != nil {
		s.eventBus.Emit(event.NewErrorEvent(event.EventExecutionError, s.instance.ID(), err))
		return nil, NewError("execute", s.providerName, s.instance.ID(), err)
	}

	// Set duration if not set by provider
	if result.Duration == 0 {
		result.Duration = time.Since(start)
	}

	// Set language
	result.Language = language

	// Emit completion event
	s.eventBus.Emit(event.NewEvent(event.EventExecutionComplete, s.instance.ID(), &event.ExecutionCompleteData{
		ExitCode: result.ExitCode,
		Duration: result.Duration,
		Language: language,
	}))

	return result, nil
}

// ExecuteAsync runs code asynchronously and returns immediately.
func (s *sandbox) ExecuteAsync(ctx context.Context, code string, opts ...ExecuteOption) (<-chan *executor.ExecutionResult, error) {
	s.mu.RLock()
	if s.stopped {
		s.mu.RUnlock()
		return nil, NewError("executeAsync", s.providerName, s.instance.ID(), ErrSandboxStopped)
	}
	s.mu.RUnlock()

	results := make(chan *executor.ExecutionResult, 1)

	go func() {
		defer close(results)

		result, err := s.Execute(ctx, code, opts...)
		if err != nil {
			results <- &executor.ExecutionResult{
				Error: err,
			}
			return
		}
		results <- result
	}()

	return results, nil
}

// ExecuteStream runs code with streaming output.
func (s *sandbox) ExecuteStream(ctx context.Context, code string, handler executor.StreamHandler, opts ...ExecuteOption) error {
	s.mu.RLock()
	if s.stopped {
		s.mu.RUnlock()
		return NewError("executeStream", s.providerName, s.instance.ID(), ErrSandboxStopped)
	}
	s.mu.RUnlock()

	// Build execution config
	execCfg := DefaultExecuteConfig()
	for _, opt := range opts {
		opt(execCfg)
	}

	// Detect language
	language := execCfg.Language
	if language == "" && s.config.AutoDetectLanguage {
		result := s.detector.Detect(code, &langdetect.DetectOptions{
			Filename:      execCfg.Filename,
			UseContent:    true,
			UseShebang:    true,
			UseHeuristics: true,
		})
		if result.Language != "" {
			language = result.Language
		} else if s.config.DefaultLanguage != "" {
			language = s.config.DefaultLanguage
		} else {
			return NewError("executeStream", s.providerName, s.instance.ID(), ErrLanguageDetectionFailed)
		}
	}

	execOpts := &executor.ExecutionOptions{
		Language:      language,
		Filename:      execCfg.Filename,
		Timeout:       execCfg.Timeout,
		Env:           execCfg.Env,
		WorkDir:       execCfg.WorkDir,
		Stdin:         execCfg.Stdin,
		Files:         execCfg.Files,
		KeepArtifacts: execCfg.KeepArtifacts,
	}

	// Emit start event
	handler(&executor.StreamEvent{
		Type:      executor.StreamStart,
		Timestamp: time.Now(),
	})

	s.eventBus.Emit(event.NewEvent(event.EventExecutionStarted, s.instance.ID(), &event.ExecutionStartedData{
		Language: language,
		CodeSize: len(code),
	}))

	// Execute with streaming
	err := s.instance.ExecuteStream(ctx, code, execOpts, handler)
	if err != nil {
		s.eventBus.Emit(event.NewErrorEvent(event.EventExecutionError, s.instance.ID(), err))
		return NewError("executeStream", s.providerName, s.instance.ID(), err)
	}

	return nil
}

// RunCommand executes a shell command in the sandbox.
func (s *sandbox) RunCommand(ctx context.Context, cmd string, args ...string) (*executor.CommandResult, error) {
	s.mu.RLock()
	if s.stopped {
		s.mu.RUnlock()
		return nil, NewError("runCommand", s.providerName, s.instance.ID(), ErrSandboxStopped)
	}
	s.mu.RUnlock()

	return s.instance.RunCommand(ctx, cmd, args)
}

// Files returns the file system interface for this sandbox.
func (s *sandbox) Files() fs.FileSystem {
	return s.instance.FileSystem()
}

// Network returns the network interface for this sandbox.
func (s *sandbox) Network() provider.Network {
	return s.instance.Network()
}

// Subscribe registers an event callback.
func (s *sandbox) Subscribe(eventType event.EventType, handler event.EventHandler) func() {
	return s.eventBus.Subscribe(eventType, handler)
}

// Stop terminates the sandbox and releases resources.
func (s *sandbox) Stop(ctx context.Context) error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.stopped = true
	s.mu.Unlock()

	err := s.instance.Stop(ctx)
	if err != nil {
		s.eventBus.Emit(event.NewErrorEvent(event.EventSandboxError, s.instance.ID(), err))
		return NewError("stop", s.providerName, s.instance.ID(), err)
	}

	s.eventBus.Emit(event.NewEvent(event.EventSandboxStopped, s.instance.ID(), nil))
	return nil
}

// Status returns the current sandbox status.
func (s *sandbox) Status(ctx context.Context) (provider.InstanceStatus, error) {
	s.mu.RLock()
	if s.stopped {
		s.mu.RUnlock()
		return provider.StatusStopped, nil
	}
	s.mu.RUnlock()

	return s.instance.Status(ctx)
}

// Execute is a convenience function for one-shot execution.
// Creates a sandbox, runs code, and cleans up.
func Execute(ctx context.Context, code string, opts ...Option) (*executor.ExecutionResult, error) {
	sb, err := Create(ctx, opts...)
	if err != nil {
		return nil, err
	}
	defer sb.Stop(context.Background())

	return sb.Execute(ctx, code)
}

// ExecuteStream is a convenience function for streaming execution.
func ExecuteStream(ctx context.Context, code string, handler executor.StreamHandler, opts ...Option) error {
	sb, err := Create(ctx, opts...)
	if err != nil {
		return err
	}
	defer sb.Stop(context.Background())

	return sb.ExecuteStream(ctx, code, handler)
}

// ListProviders returns available provider names.
func ListProviders() []string {
	return factory.Available()
}

// ProviderCapabilities returns what a provider supports.
func ProviderCapabilities(providerName string) (*provider.Capabilities, error) {
	fac := factory.GetGlobalFactory()
	return fac.GetCapabilities(providerName, nil)
}

// DetectLanguage detects the programming language of code.
func DetectLanguage(code string, filename string) *langdetect.DetectResult {
	return langdetect.Full(code, filename)
}

// SupportedLanguages returns all languages with runtime support.
func SupportedLanguages() []string {
	return langdetect.SupportedLanguages()
}

// GetRuntimeInfo returns runtime information for a language.
func GetRuntimeInfo(language string) (*langdetect.RuntimeInfo, bool) {
	return langdetect.GetRuntimeInfo(language)
}
