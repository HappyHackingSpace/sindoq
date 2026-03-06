//go:build darwin

// Package seatbelt provides a macOS Seatbelt (sandbox-exec) sandbox provider for sindoq.
// It uses the built-in sandbox-exec command with dynamically generated SBPL profiles
// for kernel-level process sandboxing without containers or VMs.
package seatbelt

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
	factory.Register("seatbelt", func(config any) (provider.Provider, error) {
		cfg, ok := config.(*Config)
		if !ok && config != nil {
			return nil, fmt.Errorf("invalid config type for seatbelt provider")
		}
		return New(cfg)
	})
}

// Config holds seatbelt provider configuration.
type Config struct {
	// TimeLimit is the maximum execution time in seconds.
	TimeLimit uint32

	// ReadPaths are paths to allow read access.
	ReadPaths []string

	// ReadExecPaths are paths to allow read and execute access.
	ReadExecPaths []string

	// WritePaths are paths to allow write access.
	WritePaths []string

	// EnableNetwork allows network access.
	EnableNetwork bool

	// AllowMachLookup allows Mach IPC lookups for essential services.
	AllowMachLookup bool

	// CustomProfile is an optional full SBPL profile string that overrides generation.
	CustomProfile string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		TimeLimit: 30,
		ReadExecPaths: []string{
			"/usr",
			"/bin",
			"/sbin",
			"/Library",
			"/opt/homebrew",
			"/usr/local",
		},
		ReadPaths: []string{
			"/etc",
			"/private/etc",
			"/System",
		},
		EnableNetwork:   false,
		AllowMachLookup: true,
	}
}

// Provider implements the seatbelt sandbox provider.
type Provider struct {
	config    *Config
	instances map[string]*Instance
	mu        sync.RWMutex
}

// New creates a new seatbelt provider.
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
	return "seatbelt"
}

// Create initializes a new seatbelt sandbox instance.
func (p *Provider) Create(ctx context.Context, opts *provider.CreateOptions) (provider.Instance, error) {
	if opts == nil {
		opts = provider.DefaultCreateOptions()
	}

	id := fmt.Sprintf("seatbelt-%d", time.Now().UnixNano())

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

// Capabilities returns seatbelt provider capabilities.
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

// Validate checks if sandbox-exec is available.
func (p *Provider) Validate(ctx context.Context) error {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		return fmt.Errorf("sandbox-exec not found: %w (requires macOS 10.5+)", err)
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

// Instance represents a seatbelt sandbox instance.
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

// Execute runs code in the seatbelt sandbox.
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

	// Build command
	codePathInWorkspace := filepath.Join(i.workDir, codeFilename)
	var runCmd []string
	if runtimeInfo.CompileCmd != nil {
		compileCmd := i.buildSeatbeltCmd(append(runtimeInfo.CompileCmd, codePathInWorkspace))
		compileExec := exec.CommandContext(ctx, compileCmd[0], compileCmd[1:]...)
		compileExec.Dir = i.workDir
		if output, err := compileExec.CombinedOutput(); err != nil {
			return &executor.ExecutionResult{
				ExitCode: 1,
				Stderr:   string(output),
				Language: opts.Language,
			}, nil
		}
		runCmd = i.buildSeatbeltCmd(runtimeInfo.RunCommand)
	} else {
		runCmd = i.buildSeatbeltCmd(append(runtimeInfo.RunCommand, codePathInWorkspace))
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

	// Set environment
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

// buildSeatbeltCmd builds the sandbox-exec command.
func (i *Instance) buildSeatbeltCmd(innerCmd []string) []string {
	profile := i.generateProfile()

	args := []string{
		"sandbox-exec",
		"-p", profile,
		"--",
	}
	args = append(args, innerCmd...)

	return args
}

// generateProfile builds an SBPL profile string from config.
//
// The security model: deny-default, then allow reads broadly (runtimes need many paths),
// restrict writes to workspace + /tmp only, and deny network unless explicitly enabled.
func (i *Instance) generateProfile() string {
	if i.config.CustomProfile != "" {
		return i.config.CustomProfile
	}

	var b strings.Builder

	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n\n")

	// Process
	b.WriteString(";; Process\n")
	b.WriteString("(allow process-exec)\n")
	b.WriteString("(allow process-fork)\n")
	b.WriteString("(allow signal (target same-sandbox))\n")
	b.WriteString("(allow process-info* (target same-sandbox))\n\n")

	// Sysctl
	b.WriteString(";; Sysctl (system info reads)\n")
	b.WriteString("(allow sysctl-read)\n\n")

	// Mach IPC
	if i.config.AllowMachLookup {
		b.WriteString(";; Mach IPC\n")
		b.WriteString("(allow mach-lookup)\n\n")
	}

	// IOKit + IPC
	b.WriteString(";; IOKit + IPC\n")
	b.WriteString("(allow iokit-open)\n")
	b.WriteString("(allow ipc-posix-shm-read*)\n")
	b.WriteString("(allow ipc-posix-sem)\n\n")

	// File reads — allowed broadly since runtimes (Python, Node, etc.) need
	// access to many system paths (dyld cache, framework dirs, home config, etc.)
	b.WriteString(";; File reads (broad — security comes from write + network restrictions)\n")
	b.WriteString("(allow file-read*)\n")
	b.WriteString("(allow file-map-executable)\n\n")

	// File writes — restricted to workspace, temp dirs, and /dev/null
	b.WriteString(";; File writes (restricted to workspace and temp)\n")
	writePaths := []string{
		fmt.Sprintf("(subpath %q)", i.sandboxDir),
		"(subpath \"/private/tmp\")",
		"(subpath \"/tmp\")",
	}
	for _, p := range i.config.WritePaths {
		writePaths = append(writePaths, fmt.Sprintf("(subpath %q)", p))
	}
	fmt.Fprintf(&b, "(allow file-write* %s)\n", strings.Join(writePaths, " "))
	b.WriteString("(allow file-write-data (literal \"/dev/null\"))\n\n")

	// Network
	if i.config.EnableNetwork {
		b.WriteString(";; Network\n")
		b.WriteString("(allow network-outbound)\n")
		b.WriteString("(allow network-inbound)\n")
		b.WriteString("(allow network-bind)\n")
		b.WriteString("(allow system-socket)\n\n")
	}

	return b.String()
}

// buildEnv builds the environment variables for the command.
func (i *Instance) buildEnv(opts *executor.ExecutionOptions) []string {
	env := os.Environ()
	env = append(env, "PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin")
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
	codePathInWorkspace := filepath.Join(i.workDir, codeFilename)
	var runCmd []string
	if runtimeInfo.CompileCmd != nil {
		compileCmd := i.buildSeatbeltCmd(append(runtimeInfo.CompileCmd, codePathInWorkspace))
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
		runCmd = i.buildSeatbeltCmd(runtimeInfo.RunCommand)
	} else {
		runCmd = i.buildSeatbeltCmd(append(runtimeInfo.RunCommand, codePathInWorkspace))
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
	seatbeltCmd := i.buildSeatbeltCmd(fullCmd)

	execCmd := exec.CommandContext(ctx, seatbeltCmd[0], seatbeltCmd[1:]...)
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
	return &seatbeltFS{instance: i}
}

// Network returns nil as seatbelt doesn't support dynamic networking.
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
