// Package wasmer provides a WebAssembly sandbox provider for sindoq.
// Wasmer runs code in a secure WASM sandbox with WASI support.
// It's cross-platform (Linux, macOS, Windows) and provides strong isolation.
package wasmer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/happyhackingspace/sindoq/internal/factory"
	"github.com/happyhackingspace/sindoq/internal/provider"
	"github.com/happyhackingspace/sindoq/pkg/executor"
	"github.com/happyhackingspace/sindoq/pkg/fs"
)

func init() {
	factory.Register("wasmer", func(config any) (provider.Provider, error) {
		cfg, ok := config.(*Config)
		if !ok && config != nil {
			return nil, fmt.Errorf("invalid config type for wasmer provider")
		}
		return New(cfg)
	})
}

// WasmRuntime defines a WASM runtime for a programming language.
type WasmRuntime struct {
	// Package is the wasmer package name (e.g., "python")
	Package string

	// EntryPoint is the binary entrypoint in the package
	EntryPoint string

	// FileExt is the source file extension
	FileExt string

	// Args are additional arguments for the runtime
	Args []string

	// RunWithFile if true, passes the filename as argument; if false, uses -c with code
	RunWithFile bool
}

// supportedRuntimes maps languages to their WASM runtimes.
// These use wasmer's registry packages.
var supportedRuntimes = map[string]WasmRuntime{
	"Python": {
		Package:     "python",
		EntryPoint:  "python",
		FileExt:     ".py",
		Args:        []string{},
		RunWithFile: true,
	},
	"JavaScript": {
		Package:     "quickjs",
		EntryPoint:  "qjs",
		FileExt:     ".js",
		Args:        []string{"--std"},
		RunWithFile: true,
	},
	"Lua": {
		Package:     "lualua",
		EntryPoint:  "lua",
		FileExt:     ".lua",
		Args:        []string{},
		RunWithFile: true,
	},
	"Ruby": {
		Package:     "ruby",
		EntryPoint:  "ruby",
		FileExt:     ".rb",
		Args:        []string{},
		RunWithFile: true,
	},
	"PHP": {
		Package:     "php",
		EntryPoint:  "php",
		FileExt:     ".php",
		Args:        []string{},
		RunWithFile: true,
	},
	"Shell": {
		Package:     "sharrattj/bash",
		EntryPoint:  "bash",
		FileExt:     ".sh",
		Args:        []string{},
		RunWithFile: true,
	},
}

// Config holds Wasmer provider configuration.
type Config struct {
	// WasmerPath is the path to wasmer binary.
	WasmerPath string

	// CacheDir is the directory for WASM module cache.
	CacheDir string

	// TimeLimit is the maximum execution time in seconds.
	TimeLimit uint32

	// MaxMemoryMB is the memory limit (Wasmer supports memory limits via WASI).
	MaxMemoryMB uint32

	// EnableNetwork allows network access (via WASI).
	EnableNetwork bool

	// CustomRuntimes allows adding custom WASM runtimes.
	CustomRuntimes map[string]WasmRuntime

	// PreinstalledPackages lists packages to install during provider creation.
	PreinstalledPackages []string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	cacheDir := os.Getenv("WASMER_CACHE_DIR")
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "wasmer-cache")
	}

	return &Config{
		WasmerPath:    "wasmer",
		CacheDir:      cacheDir,
		TimeLimit:     30,
		MaxMemoryMB:   256,
		EnableNetwork: false,
	}
}

// Provider implements the Wasmer sandbox provider.
type Provider struct {
	config    *Config
	instances map[string]*Instance
	mu        sync.RWMutex
	runtimes  map[string]WasmRuntime
}

// New creates a new Wasmer provider.
func New(cfg *Config) (*Provider, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Merge default and custom runtimes
	runtimes := make(map[string]WasmRuntime)
	for lang, rt := range supportedRuntimes {
		runtimes[lang] = rt
	}
	for lang, rt := range cfg.CustomRuntimes {
		runtimes[lang] = rt
	}

	// Ensure cache directory exists
	if err := os.MkdirAll(cfg.CacheDir, 0755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}

	return &Provider{
		config:    cfg,
		instances: make(map[string]*Instance),
		runtimes:  runtimes,
	}, nil
}

// Name returns the provider identifier.
func (p *Provider) Name() string {
	return "wasmer"
}

// Create initializes a new Wasmer sandbox instance.
func (p *Provider) Create(ctx context.Context, opts *provider.CreateOptions) (provider.Instance, error) {
	// Check if wasmer is available
	if err := p.Validate(ctx); err != nil {
		return nil, err
	}

	if opts == nil {
		opts = provider.DefaultCreateOptions()
	}

	id := fmt.Sprintf("wasmer-%d", time.Now().UnixNano())

	// Create sandbox directory for this instance
	sandboxDir, err := os.MkdirTemp("", id)
	if err != nil {
		return nil, fmt.Errorf("create sandbox dir: %w", err)
	}

	// Create workspace inside sandbox
	workDir := filepath.Join(sandboxDir, "workspace")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		_ = os.RemoveAll(sandboxDir)
		return nil, fmt.Errorf("create workspace: %w", err)
	}

	instance := &Instance{
		id:         id,
		provider:   p,
		sandboxDir: sandboxDir,
		workDir:    workDir,
		config:     p.config,
		timeout:    opts.Timeout,
		env:        opts.Environment,
	}

	p.mu.Lock()
	p.instances[id] = instance
	p.mu.Unlock()

	return instance, nil
}

// Capabilities returns Wasmer provider capabilities.
func (p *Provider) Capabilities() provider.Capabilities {
	languages := make([]string, 0, len(p.runtimes))
	for lang := range p.runtimes {
		languages = append(languages, lang)
	}

	return provider.Capabilities{
		SupportsStreaming:  true,
		SupportsAsync:      true,
		SupportsFileSystem: true,
		SupportsNetwork:    p.config.EnableNetwork,
		SupportedLanguages: languages,
		MaxExecutionTime:   time.Duration(p.config.TimeLimit) * time.Second,
		MaxMemoryMB:        int(p.config.MaxMemoryMB),
		MaxCPUs:            1, // WASM is single-threaded
	}
}

// Validate checks if Wasmer is available.
func (p *Provider) Validate(ctx context.Context) error {
	if _, err := exec.LookPath(p.config.WasmerPath); err != nil {
		return fmt.Errorf("wasmer not found: %w\n\nInstall Wasmer:\n  curl https://get.wasmer.io -sSfL | sh\n\nOr visit: https://wasmer.io/", err)
	}

	// Test wasmer works
	cmd := exec.CommandContext(ctx, p.config.WasmerPath, "--version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("wasmer not working: %w", err)
	}

	return nil
}

// Close releases provider resources.
func (p *Provider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, instance := range p.instances {
		_ = instance.Stop(context.Background())
	}

	return nil
}

// InstallRuntime installs a WASM runtime package.
func (p *Provider) InstallRuntime(ctx context.Context, packageName string) error {
	cmd := exec.CommandContext(ctx, p.config.WasmerPath, "run", packageName, "--", "--help")
	cmd.Env = append(os.Environ(), fmt.Sprintf("WASMER_CACHE_DIR=%s", p.config.CacheDir))

	// Running with --help will trigger package download if not cached
	_ = cmd.Run() // Ignore errors, package may not support --help

	return nil
}

var _ provider.Provider = (*Provider)(nil)

// Instance represents a Wasmer sandbox instance.
type Instance struct {
	id         string
	provider   *Provider
	sandboxDir string
	workDir    string
	config     *Config
	timeout    time.Duration
	env        map[string]string
	mu         sync.RWMutex
	stopped    bool
}

// ID returns the instance ID.
func (i *Instance) ID() string {
	return i.id
}

// Execute runs code in the Wasmer sandbox.
func (i *Instance) Execute(ctx context.Context, code string, opts *executor.ExecutionOptions) (*executor.ExecutionResult, error) {
	i.mu.RLock()
	if i.stopped {
		i.mu.RUnlock()
		return nil, fmt.Errorf("sandbox stopped")
	}
	i.mu.RUnlock()

	if opts == nil {
		opts = executor.DefaultExecutionOptions()
	}

	runtime, ok := i.provider.runtimes[opts.Language]
	if !ok {
		return nil, fmt.Errorf("unsupported language for wasmer: %s (supported: Python, JavaScript, Lua, Ruby, PHP, Shell)", opts.Language)
	}

	// Write code to file
	codeFilename := "main" + runtime.FileExt
	codePath := filepath.Join(i.workDir, codeFilename)
	if err := os.WriteFile(codePath, []byte(code), 0644); err != nil {
		return nil, fmt.Errorf("write code file: %w", err)
	}

	// Write additional files
	for path, content := range opts.Files {
		fullPath := filepath.Join(i.workDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return nil, fmt.Errorf("create dir for %s: %w", path, err)
		}
		if err := os.WriteFile(fullPath, content, 0644); err != nil {
			return nil, fmt.Errorf("write file %s: %w", path, err)
		}
	}

	// Build wasmer command
	runCmd := i.buildWasmerCmd(runtime, codeFilename)

	// Set timeout
	execCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	start := time.Now()

	// Execute
	cmd := exec.CommandContext(execCtx, runCmd[0], runCmd[1:]...)
	cmd.Dir = i.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if opts.Stdin != "" {
		cmd.Stdin = strings.NewReader(opts.Stdin)
	}

	// Set environment
	cmd.Env = append(os.Environ(), fmt.Sprintf("WASMER_CACHE_DIR=%s", i.config.CacheDir))
	for k, v := range opts.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range i.env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if execCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("execution timeout")
		} else {
			return nil, fmt.Errorf("execution failed: %w", err)
		}
	}

	return &executor.ExecutionResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: time.Since(start),
		Language: opts.Language,
	}, nil
}

// buildWasmerCmd builds the wasmer command with all options.
func (i *Instance) buildWasmerCmd(runtime WasmRuntime, codeFilename string) []string {
	args := []string{
		i.config.WasmerPath,
		"run",
	}

	// Add WASI options for filesystem access (use "." since cmd.Dir is set to workDir)
	args = append(args, "--dir", ".")

	// Network access (if supported)
	if i.config.EnableNetwork {
		args = append(args, "--net")
	}

	// Add entrypoint
	args = append(args, "--entrypoint", runtime.EntryPoint)

	// Add the package name
	args = append(args, runtime.Package)

	// Separator for runtime arguments
	args = append(args, "--")

	// Add runtime-specific args
	args = append(args, runtime.Args...)

	// Add the code file
	args = append(args, codeFilename)

	return args
}

// ExecuteStream runs code with streaming output.
func (i *Instance) ExecuteStream(ctx context.Context, code string, opts *executor.ExecutionOptions, handler executor.StreamHandler) error {
	i.mu.RLock()
	if i.stopped {
		i.mu.RUnlock()
		return fmt.Errorf("sandbox stopped")
	}
	i.mu.RUnlock()

	if opts == nil {
		opts = executor.DefaultExecutionOptions()
	}

	runtime, ok := i.provider.runtimes[opts.Language]
	if !ok {
		return fmt.Errorf("unsupported language for wasmer: %s", opts.Language)
	}

	// Write code to file
	codeFilename := "main" + runtime.FileExt
	codePath := filepath.Join(i.workDir, codeFilename)
	if err := os.WriteFile(codePath, []byte(code), 0644); err != nil {
		return fmt.Errorf("write code file: %w", err)
	}

	// Build command
	runCmd := i.buildWasmerCmd(runtime, codeFilename)

	cmd := exec.CommandContext(ctx, runCmd[0], runCmd[1:]...)
	cmd.Dir = i.workDir
	cmd.Env = append(os.Environ(), fmt.Sprintf("WASMER_CACHE_DIR=%s", i.config.CacheDir))

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start command: %w", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// Stream stdout
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		for {
			n, err := stdoutPipe.Read(buf)
			if n > 0 {
				_ = handler(&executor.StreamEvent{
					Type:      executor.StreamStdout,
					Data:      string(buf[:n]),
					Timestamp: time.Now(),
				})
			}
			if err != nil {
				break
			}
		}
	}()

	// Stream stderr
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		for {
			n, err := stderrPipe.Read(buf)
			if n > 0 {
				_ = handler(&executor.StreamEvent{
					Type:      executor.StreamStderr,
					Data:      string(buf[:n]),
					Timestamp: time.Now(),
				})
			}
			if err != nil {
				break
			}
		}
	}()

	wg.Wait()

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	_ = handler(&executor.StreamEvent{
		Type:      executor.StreamComplete,
		ExitCode:  exitCode,
		Timestamp: time.Now(),
	})

	return nil
}

// RunCommand executes a shell command in the sandbox.
func (i *Instance) RunCommand(ctx context.Context, cmd string, args []string) (*executor.CommandResult, error) {
	i.mu.RLock()
	if i.stopped {
		i.mu.RUnlock()
		return nil, fmt.Errorf("sandbox stopped")
	}
	i.mu.RUnlock()

	start := time.Now()

	// Use bash shell for command execution
	shellRuntime := i.provider.runtimes["Shell"]
	wasmerArgs := []string{
		i.config.WasmerPath,
		"run",
		"--dir", ".",
		"--entrypoint", shellRuntime.EntryPoint,
		shellRuntime.Package,
		"--",
		"-c",
		cmd + " " + strings.Join(args, " "),
	}

	execCmd := exec.CommandContext(ctx, wasmerArgs[0], wasmerArgs[1:]...)
	execCmd.Dir = i.workDir
	execCmd.Env = append(os.Environ(), fmt.Sprintf("WASMER_CACHE_DIR=%s", i.config.CacheDir))

	var stdout, stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	err := execCmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("run command: %w", err)
		}
	}

	return &executor.CommandResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: time.Since(start),
	}, nil
}

// FileSystem returns the file system handler.
func (i *Instance) FileSystem() fs.FileSystem {
	return &wasmerFS{instance: i}
}

// Network returns nil as Wasmer has limited networking support.
func (i *Instance) Network() provider.Network {
	return nil
}

// Stop terminates the sandbox and cleans up.
func (i *Instance) Stop(ctx context.Context) error {
	i.mu.Lock()
	if i.stopped {
		i.mu.Unlock()
		return nil
	}
	i.stopped = true
	i.mu.Unlock()

	// Clean up sandbox directory
	if i.sandboxDir != "" {
		_ = os.RemoveAll(i.sandboxDir)
	}

	// Remove from provider's instance map
	i.provider.mu.Lock()
	delete(i.provider.instances, i.id)
	i.provider.mu.Unlock()

	return nil
}

// Status returns the current status.
func (i *Instance) Status(ctx context.Context) (provider.InstanceStatus, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	if i.stopped {
		return provider.StatusStopped, nil
	}

	return provider.StatusRunning, nil
}

var _ provider.Instance = (*Instance)(nil)
