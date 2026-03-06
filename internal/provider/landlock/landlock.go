//go:build linux

// Package landlock provides a Linux Landlock sandbox provider for sindoq.
// Landlock uses the kernel's Landlock LSM (Linux 5.13+) for lightweight,
// process-level filesystem and network access control.
// It uses a self-exec helper pattern: the provider re-invokes the current
// executable with environment variables to apply Landlock restrictions
// before exec'ing the target command.
package landlock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	ll "github.com/landlock-lsm/go-landlock/landlock"

	"github.com/happyhackingspace/sindoq/internal/factory"
	"github.com/happyhackingspace/sindoq/internal/provider"
	"github.com/happyhackingspace/sindoq/pkg/executor"
	"github.com/happyhackingspace/sindoq/pkg/fs"
	"github.com/happyhackingspace/sindoq/pkg/langdetect"
)

const (
	envLandlock       = "_SINDOQ_LANDLOCK"
	envLandlockConfig = "_SINDOQ_LANDLOCK_CONFIG"
)

// landlockHelperConfig is passed via env var to the child process.
type landlockHelperConfig struct {
	BestEffort          bool     `json:"best_effort"`
	ReadPaths           []string `json:"read_paths,omitempty"`
	ReadExecPaths       []string `json:"read_exec_paths,omitempty"`
	WritePaths          []string `json:"write_paths,omitempty"`
	WriteExecPaths      []string `json:"write_exec_paths,omitempty"`
	NetworkConnectPorts []int    `json:"network_connect_ports,omitempty"`
	NetworkBindPorts    []int    `json:"network_bind_ports,omitempty"`
	EnableNetwork       bool     `json:"enable_network"`
}

func init() {
	// If we're the self-exec helper child, run the helper and never return.
	if os.Getenv(envLandlock) == "1" {
		runLandlockHelper()
		// runLandlockHelper calls syscall.Exec or os.Exit; should not reach here.
		os.Exit(1)
	}

	factory.Register("landlock", func(config any) (provider.Provider, error) {
		cfg, ok := config.(*Config)
		if !ok && config != nil {
			return nil, fmt.Errorf("invalid config type for landlock provider")
		}
		return New(cfg)
	})
}

// runLandlockHelper is executed in the child process.
// It applies Landlock restrictions and then exec's the target command.
func runLandlockHelper() {
	configJSON := os.Getenv(envLandlockConfig)
	if configJSON == "" {
		fmt.Fprintf(os.Stderr, "sindoq-landlock: missing config\n")
		os.Exit(1)
	}

	var cfg landlockHelperConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "sindoq-landlock: parse config: %v\n", err)
		os.Exit(1)
	}

	// Build Landlock rules
	var rules []ll.Rule

	// Read-only directories (with execute for libraries/binaries)
	for _, p := range cfg.ReadExecPaths {
		if _, err := os.Stat(p); err == nil {
			rules = append(rules, ll.RODirs(p))
		}
	}

	// Read-only paths (files only)
	for _, p := range cfg.ReadPaths {
		if _, err := os.Stat(p); err == nil {
			rules = append(rules, ll.ROFiles(p).IgnoreIfMissing())
		}
	}

	// Read-write directories
	for _, p := range cfg.WritePaths {
		if _, err := os.Stat(p); err == nil {
			rules = append(rules, ll.RWDirs(p))
		}
	}

	// Write-execute directories
	for _, p := range cfg.WriteExecPaths {
		if _, err := os.Stat(p); err == nil {
			rules = append(rules, ll.RWDirs(p))
		}
	}

	// Network rules
	for _, port := range cfg.NetworkConnectPorts {
		rules = append(rules, ll.ConnectTCP(uint16(port)))
	}
	for _, port := range cfg.NetworkBindPorts {
		rules = append(rules, ll.BindTCP(uint16(port)))
	}

	// Apply restrictions
	config := ll.V5
	if cfg.BestEffort {
		config = config.BestEffort()
	}

	if err := config.Restrict(rules...); err != nil {
		fmt.Fprintf(os.Stderr, "sindoq-landlock: restrict: %v\n", err)
		os.Exit(1)
	}

	// Clean up env vars
	_ = os.Unsetenv(envLandlock)
	_ = os.Unsetenv(envLandlockConfig)

	// The remaining args are the command to exec
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "sindoq-landlock: no command specified\n")
		os.Exit(1)
	}

	binary, err := exec.LookPath(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "sindoq-landlock: lookup %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}

	// Exec the target command (replaces this process)
	if err := syscall.Exec(binary, os.Args[1:], os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "sindoq-landlock: exec: %v\n", err)
		os.Exit(1)
	}
}

// Config holds landlock provider configuration.
type Config struct {
	// ABIVersion is the Landlock ABI version to target.
	ABIVersion int

	// BestEffort degrades gracefully on older kernels.
	BestEffort bool

	// IgnoreIfMissing skips paths that don't exist.
	IgnoreIfMissing bool

	// TimeLimit is the maximum execution time in seconds.
	TimeLimit uint32

	// ReadPaths are paths to allow read access (files).
	ReadPaths []string

	// ReadExecPaths are paths to allow read and execute access (directories).
	ReadExecPaths []string

	// WritePaths are paths to allow read-write access (directories).
	WritePaths []string

	// WriteExecPaths are paths to allow read-write-execute access (directories).
	WriteExecPaths []string

	// NetworkConnectPorts are TCP ports allowed for outbound connections.
	NetworkConnectPorts []int

	// NetworkBindPorts are TCP ports allowed for binding.
	NetworkBindPorts []int

	// EnableNetwork allows all network access (overrides port lists).
	EnableNetwork bool
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		BestEffort:      true,
		IgnoreIfMissing: true,
		TimeLimit:       30,
		ReadExecPaths: []string{
			"/bin",
			"/lib",
			"/lib64",
			"/usr",
			"/etc/alternatives",
			"/etc/ssl",
		},
		ReadPaths: []string{
			"/etc/passwd",
			"/etc/group",
		},
		EnableNetwork: false,
	}
}

// Provider implements the landlock sandbox provider.
type Provider struct {
	config    *Config
	instances map[string]*Instance
	mu        sync.RWMutex
}

// New creates a new landlock provider.
func New(cfg *Config) (*Provider, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	return &Provider{
		config:    cfg,
		instances: make(map[string]*Instance),
	}, nil
}

// Name returns the provider identifier.
func (p *Provider) Name() string {
	return "landlock"
}

// Create initializes a new landlock sandbox instance.
func (p *Provider) Create(ctx context.Context, opts *provider.CreateOptions) (provider.Instance, error) {
	if opts == nil {
		opts = provider.DefaultCreateOptions()
	}

	id := fmt.Sprintf("landlock-%d", time.Now().UnixNano())

	sandboxDir, err := os.MkdirTemp("", id)
	if err != nil {
		return nil, fmt.Errorf("create sandbox dir: %w", err)
	}

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

// Capabilities returns landlock provider capabilities.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		SupportsStreaming:  true,
		SupportsAsync:      true,
		SupportsFileSystem: true,
		SupportsNetwork:    p.config.EnableNetwork,
		SupportedLanguages: langdetect.SupportedLanguages(),
		MaxExecutionTime:   time.Duration(p.config.TimeLimit) * time.Second,
	}
}

// Validate checks if Landlock is available.
func (p *Provider) Validate(ctx context.Context) error {
	// Check that we can get our own executable path (needed for self-exec)
	if _, err := os.Executable(); err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}

	// Try a best-effort restrict with no rules to check kernel support
	err := ll.V5.BestEffort().Restrict()
	if err != nil {
		return fmt.Errorf("landlock not supported on this kernel: %w (requires Linux 5.13+)", err)
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

var _ provider.Provider = (*Provider)(nil)

// Instance represents a landlock sandbox instance.
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

// Execute runs code in the landlock sandbox.
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

	runtimeInfo, ok := langdetect.GetRuntimeInfo(opts.Language)
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", opts.Language)
	}

	// Write code to file
	codeFilename := "main" + runtimeInfo.FileExt
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

	// Build command using host paths
	codePathOnHost := filepath.Join(i.workDir, codeFilename)
	var runCmd []string
	if runtimeInfo.CompileCmd != nil {
		compileCmd := i.buildLandlockCmd(append(runtimeInfo.CompileCmd, codePathOnHost))
		compileExec := exec.CommandContext(ctx, compileCmd[0], compileCmd[1:]...)
		compileExec.Dir = i.workDir
		compileExec.Env = i.buildEnv(opts)
		if output, err := compileExec.CombinedOutput(); err != nil {
			return &executor.ExecutionResult{
				ExitCode: 1,
				Stderr:   string(output),
				Language: opts.Language,
			}, nil
		}
		runCmd = i.buildLandlockCmd(runtimeInfo.RunCommand)
	} else {
		runCmd = i.buildLandlockCmd(append(runtimeInfo.RunCommand, codePathOnHost))
	}

	// Set timeout
	execCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	start := time.Now()

	cmd := exec.CommandContext(execCtx, runCmd[0], runCmd[1:]...)
	cmd.Dir = i.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if opts.Stdin != "" {
		cmd.Stdin = strings.NewReader(opts.Stdin)
	}

	cmd.Env = i.buildEnv(opts)

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

// buildLandlockCmd builds the self-exec command with Landlock env vars.
func (i *Instance) buildLandlockCmd(innerCmd []string) []string {
	selfExe, err := os.Executable()
	if err != nil {
		// Fallback: just run the command directly without sandboxing
		return innerCmd
	}

	// The self-exec binary is invoked with the target command as args
	args := []string{selfExe}
	args = append(args, innerCmd...)

	return args
}

// buildHelperConfig creates the JSON config for the child process.
func (i *Instance) buildHelperConfig() string {
	cfg := landlockHelperConfig{
		BestEffort:          i.config.BestEffort,
		ReadPaths:           i.config.ReadPaths,
		ReadExecPaths:       i.config.ReadExecPaths,
		WritePaths:          append(i.config.WritePaths, i.sandboxDir, "/tmp"),
		WriteExecPaths:      append(i.config.WriteExecPaths, i.sandboxDir, "/tmp"),
		NetworkConnectPorts: i.config.NetworkConnectPorts,
		NetworkBindPorts:    i.config.NetworkBindPorts,
		EnableNetwork:       i.config.EnableNetwork,
	}

	data, _ := json.Marshal(cfg)
	return string(data)
}

// buildEnv builds the environment variables for the command.
func (i *Instance) buildEnv(opts *executor.ExecutionOptions) []string {
	env := os.Environ()
	// Add Landlock helper env vars
	env = append(env, envLandlock+"=1")
	env = append(env, envLandlockConfig+"="+i.buildHelperConfig())
	env = append(env, "PATH=/usr/local/bin:/usr/bin:/bin")
	for k, v := range opts.Env {
		env = append(env, k+"="+v)
	}
	for k, v := range i.env {
		env = append(env, k+"="+v)
	}
	return env
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

	runtimeInfo, ok := langdetect.GetRuntimeInfo(opts.Language)
	if !ok {
		return fmt.Errorf("unsupported language: %s", opts.Language)
	}

	// Write code to file
	codeFilename := "main" + runtimeInfo.FileExt
	codePath := filepath.Join(i.workDir, codeFilename)
	if err := os.WriteFile(codePath, []byte(code), 0644); err != nil {
		return fmt.Errorf("write code file: %w", err)
	}

	// Build command
	codePathOnHost := filepath.Join(i.workDir, codeFilename)
	var runCmd []string
	if runtimeInfo.CompileCmd != nil {
		compileCmd := i.buildLandlockCmd(append(runtimeInfo.CompileCmd, codePathOnHost))
		compileExec := exec.CommandContext(ctx, compileCmd[0], compileCmd[1:]...)
		compileExec.Dir = i.workDir
		compileExec.Env = i.buildEnv(opts)
		if output, err := compileExec.CombinedOutput(); err != nil {
			_ = handler(&executor.StreamEvent{
				Type:      executor.StreamStderr,
				Data:      string(output),
				Timestamp: time.Now(),
			})
			_ = handler(&executor.StreamEvent{
				Type:      executor.StreamComplete,
				ExitCode:  1,
				Timestamp: time.Now(),
			})
			return nil
		}
		runCmd = i.buildLandlockCmd(runtimeInfo.RunCommand)
	} else {
		runCmd = i.buildLandlockCmd(append(runtimeInfo.RunCommand, codePathOnHost))
	}

	cmd := exec.CommandContext(ctx, runCmd[0], runCmd[1:]...)
	cmd.Dir = i.workDir
	cmd.Env = i.buildEnv(opts)

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

	fullCmd := append([]string{cmd}, args...)
	landlockCmd := i.buildLandlockCmd(fullCmd)

	execCmd := exec.CommandContext(ctx, landlockCmd[0], landlockCmd[1:]...)
	execCmd.Dir = i.workDir
	execCmd.Env = i.buildEnv(executor.DefaultExecutionOptions())

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
	return &landlockFS{instance: i}
}

// Network returns nil as landlock doesn't support dynamic networking.
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

	if i.sandboxDir != "" {
		_ = os.RemoveAll(i.sandboxDir)
	}

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
