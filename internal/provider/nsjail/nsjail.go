//go:build linux

// Package nsjail provides a lightweight process isolation provider for sindoq.
// nsjail uses Linux namespaces, seccomp, and cgroups for fast, secure sandboxing.
// It's ideal for quick script execution with minimal overhead (~5ms startup).
package nsjail

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
	"github.com/happyhackingspace/sindoq/pkg/langdetect"
)

func init() {
	factory.Register("nsjail", func(config any) (provider.Provider, error) {
		cfg, ok := config.(*Config)
		if !ok && config != nil {
			return nil, fmt.Errorf("invalid config type for nsjail provider")
		}
		return New(cfg)
	})
}

// Config holds nsjail provider configuration.
type Config struct {
	// NsjailPath is the path to nsjail binary.
	NsjailPath string

	// Chroot is the chroot directory (default: "/" for host root).
	Chroot string

	// User and Group IDs for the sandboxed process.
	User  uint32
	Group uint32

	// TimeLimit is the maximum execution time in seconds.
	TimeLimit uint32

	// MaxMemoryMB is the memory limit in megabytes.
	MaxMemoryMB uint32

	// MaxCPUs limits CPU cores.
	MaxCPUs uint32

	// MaxPids limits the number of processes.
	MaxPids uint32

	// MaxFileSize limits file size in MB.
	MaxFileSizeMB uint32

	// EnableNetwork allows network access.
	EnableNetwork bool

	// MountProc mounts /proc in the sandbox.
	MountProc bool

	// MountTmp mounts a tmpfs at /tmp.
	MountTmp bool

	// ReadOnlyBindMounts are paths to bind-mount read-only.
	ReadOnlyBindMounts []string

	// ReadWriteBindMounts are paths to bind-mount read-write.
	ReadWriteBindMounts []string

	// WorkDir is the working directory inside the sandbox.
	WorkDir string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		NsjailPath:    "nsjail",
		Chroot:        "/",
		User:          65534, // nobody
		Group:         65534, // nogroup
		TimeLimit:     30,
		MaxMemoryMB:   256,
		MaxCPUs:       1,
		MaxPids:       64,
		MaxFileSizeMB: 64,
		EnableNetwork: false,
		MountProc:     true,
		MountTmp:      true,
		WorkDir:       "/tmp",
		ReadOnlyBindMounts: []string{
			"/bin",
			"/lib",
			"/lib64",
			"/usr",
			"/etc/alternatives",
			"/etc/ssl",
		},
	}
}

// Provider implements the nsjail sandbox provider.
type Provider struct {
	config    *Config
	instances map[string]*Instance
	mu        sync.RWMutex
}

// New creates a new nsjail provider.
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
	return "nsjail"
}

// Create initializes a new nsjail sandbox instance.
func (p *Provider) Create(ctx context.Context, opts *provider.CreateOptions) (provider.Instance, error) {
	if opts == nil {
		opts = provider.DefaultCreateOptions()
	}

	id := fmt.Sprintf("nsjail-%d", time.Now().UnixNano())

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

// Capabilities returns nsjail provider capabilities.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		SupportsStreaming:  true,
		SupportsAsync:      true,
		SupportsFileSystem: true,
		SupportsNetwork:    p.config.EnableNetwork,
		SupportedLanguages: langdetect.SupportedLanguages(),
		MaxExecutionTime:   time.Duration(p.config.TimeLimit) * time.Second,
		MaxMemoryMB:        int(p.config.MaxMemoryMB),
		MaxCPUs:            int(p.config.MaxCPUs),
	}
}

// Validate checks if nsjail is available.
func (p *Provider) Validate(ctx context.Context) error {
	if _, err := exec.LookPath(p.config.NsjailPath); err != nil {
		return fmt.Errorf("nsjail not found: %w (install from https://github.com/google/nsjail)", err)
	}

	// Test nsjail works
	cmd := exec.CommandContext(ctx, p.config.NsjailPath, "--help")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("nsjail not working: %w", err)
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

// Instance represents an nsjail sandbox instance.
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

// Execute runs code in the nsjail sandbox.
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

	// Build nsjail command
	sandboxCodePath := "/workspace/" + codeFilename
	var runCmd []string
	if runtimeInfo.CompileCmd != nil {
		// For compiled languages, compile first then run
		compileCmd := i.buildNsjailCmd(append(runtimeInfo.CompileCmd, sandboxCodePath), opts)
		compileExec := exec.CommandContext(ctx, compileCmd[0], compileCmd[1:]...)
		if output, err := compileExec.CombinedOutput(); err != nil {
			return &executor.ExecutionResult{
				ExitCode: 1,
				Stderr:   string(output),
				Language: opts.Language,
			}, nil
		}
		runCmd = i.buildNsjailCmd(runtimeInfo.RunCommand, opts)
	} else {
		runCmd = i.buildNsjailCmd(append(runtimeInfo.RunCommand, sandboxCodePath), opts)
	}

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

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if opts.Stdin != "" {
		cmd.Stdin = strings.NewReader(opts.Stdin)
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

// buildNsjailCmd builds the nsjail command with all options.
func (i *Instance) buildNsjailCmd(innerCmd []string, opts *executor.ExecutionOptions) []string {
	args := []string{
		i.config.NsjailPath,
		"--mode", "o", // once mode
		"--chroot", i.config.Chroot,
		"--user", fmt.Sprintf("%d", i.config.User),
		"--group", fmt.Sprintf("%d", i.config.Group),
		"--time_limit", fmt.Sprintf("%d", i.config.TimeLimit),
		"--rlimit_as", fmt.Sprintf("%d", i.config.MaxMemoryMB),
		"--rlimit_cpu", fmt.Sprintf("%d", i.config.TimeLimit),
		"--rlimit_fsize", fmt.Sprintf("%d", i.config.MaxFileSizeMB),
		"--rlimit_nofile", "64",
		"--rlimit_nproc", fmt.Sprintf("%d", i.config.MaxPids),
		"--cgroup_pids_max", fmt.Sprintf("%d", i.config.MaxPids),
		"--cgroup_mem_max", fmt.Sprintf("%d", i.config.MaxMemoryMB*1024*1024),
	}

	// CPU limit
	if i.config.MaxCPUs > 0 {
		args = append(args, "--cgroup_cpu_ms_per_sec", fmt.Sprintf("%d", i.config.MaxCPUs*1000))
	}

	// Network
	if !i.config.EnableNetwork {
		args = append(args, "--disable_clone_newnet")
	}

	// Mount proc
	if i.config.MountProc {
		args = append(args, "--mount", "proc:/proc:proc")
	}

	// Mount tmp
	if i.config.MountTmp {
		args = append(args, "--mount", "tmpfs:/tmp:tmpfs:size=64M")
	}

	// Read-only bind mounts
	for _, path := range i.config.ReadOnlyBindMounts {
		if _, err := os.Stat(path); err == nil {
			args = append(args, "--bindmount_ro", path)
		}
	}

	// Read-write bind mounts
	for _, path := range i.config.ReadWriteBindMounts {
		if _, err := os.Stat(path); err == nil {
			args = append(args, "--bindmount", path)
		}
	}

	// Mount workspace
	args = append(args, "--bindmount", fmt.Sprintf("%s:/workspace", i.workDir))

	// Working directory
	args = append(args, "--cwd", "/workspace")

	// Environment variables
	for k, v := range opts.Env {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range i.env {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	// Add PATH
	args = append(args, "--env", "PATH=/usr/local/bin:/usr/bin:/bin")

	// Quiet mode (less nsjail output)
	args = append(args, "--really_quiet")

	// Add the command separator and inner command
	args = append(args, "--")
	args = append(args, innerCmd...)

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
	sandboxCodePath := "/workspace/" + codeFilename
	var runCmd []string
	if runtimeInfo.CompileCmd != nil {
		compileCmd := i.buildNsjailCmd(append(runtimeInfo.CompileCmd, sandboxCodePath), opts)
		compileExec := exec.CommandContext(ctx, compileCmd[0], compileCmd[1:]...)
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
		runCmd = i.buildNsjailCmd(runtimeInfo.RunCommand, opts)
	} else {
		runCmd = i.buildNsjailCmd(append(runtimeInfo.RunCommand, sandboxCodePath), opts)
	}

	cmd := exec.CommandContext(ctx, runCmd[0], runCmd[1:]...)

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

	fullCmd := append([]string{cmd}, args...)
	nsjailCmd := i.buildNsjailCmd(fullCmd, executor.DefaultExecutionOptions())

	execCmd := exec.CommandContext(ctx, nsjailCmd[0], nsjailCmd[1:]...)

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
	return &nsjailFS{instance: i}
}

// Network returns nil as nsjail doesn't support dynamic networking.
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
