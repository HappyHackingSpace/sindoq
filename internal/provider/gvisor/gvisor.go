//go:build linux

// Package gvisor provides the gVisor sandbox provider for sindoq.
// gVisor intercepts application system calls and acts as the guest kernel,
// providing strong isolation without the overhead of a full VM.
// Requires runsc (gVisor runtime) to be installed.
package gvisor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/happyhackingspace/sindoq/internal/factory"
	"github.com/happyhackingspace/sindoq/internal/provider"
	"github.com/happyhackingspace/sindoq/pkg/executor"
	"github.com/happyhackingspace/sindoq/pkg/fs"
	"github.com/happyhackingspace/sindoq/pkg/langdetect"
)

func init() {
	factory.Register("gvisor", func(config any) (provider.Provider, error) {
		cfg, ok := config.(*Config)
		if !ok && config != nil {
			return nil, fmt.Errorf("invalid config type for gvisor provider")
		}
		return New(cfg)
	})
}

// Config holds gVisor provider configuration.
type Config struct {
	// RuntimeName is the Docker runtime name for gVisor (default: "runsc").
	RuntimeName string

	// RuntimePath is the path to runsc binary (for validation).
	RuntimePath string

	// Platform specifies gVisor platform: "ptrace" or "kvm".
	// KVM provides better performance but requires /dev/kvm.
	Platform string

	// Network specifies network mode: "sandbox" (isolated) or "host".
	Network string

	// DockerHost is the Docker daemon socket.
	DockerHost string

	// DefaultImage is the default container image.
	DefaultImage string

	// Debug enables gVisor debug logging.
	Debug bool
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		RuntimeName:  "runsc",
		RuntimePath:  "runsc",
		Platform:     "ptrace",
		Network:      "sandbox",
		DefaultImage: "python:3.12-slim",
		Debug:        false,
	}
}

// Provider implements the gVisor sandbox provider.
// It uses Docker with the runsc runtime for container management.
type Provider struct {
	config *Config
	client *client.Client
}

// New creates a new gVisor provider.
func New(cfg *Config) (*Provider, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	opts := []client.Opt{
		client.WithAPIVersionNegotiation(),
	}

	if cfg.DockerHost != "" {
		opts = append(opts, client.WithHost(cfg.DockerHost))
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	return &Provider{
		config: cfg,
		client: cli,
	}, nil
}

// Name returns the provider identifier.
func (p *Provider) Name() string {
	return "gvisor"
}

// Create initializes a new gVisor sandbox.
func (p *Provider) Create(ctx context.Context, opts *provider.CreateOptions) (provider.Instance, error) {
	if opts == nil {
		opts = provider.DefaultCreateOptions()
	}

	// Determine image
	imageName := opts.Image
	if imageName == "" {
		if opts.Runtime != "" {
			if info, ok := langdetect.GetRuntimeInfo(opts.Runtime); ok {
				imageName = info.DockerImage
			}
		}
		if imageName == "" {
			imageName = p.config.DefaultImage
		}
	}

	// Pull image if needed
	if err := p.ensureImage(ctx, imageName); err != nil {
		return nil, fmt.Errorf("ensure image: %w", err)
	}

	// Build environment variables
	env := make([]string, 0, len(opts.Environment))
	for k, v := range opts.Environment {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Container configuration
	containerConfig := &container.Config{
		Image:        imageName,
		Env:          env,
		WorkingDir:   opts.WorkDir,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
		Labels:       opts.Labels,
		Entrypoint:   []string{"tail", "-f", "/dev/null"},
	}

	// Host configuration with gVisor runtime
	hostConfig := &container.HostConfig{
		Runtime: p.config.RuntimeName,
		Resources: container.Resources{
			Memory:   int64(opts.Resources.MemoryMB) * 1024 * 1024,
			NanoCPUs: int64(opts.Resources.CPUs * 1e9),
		},
		AutoRemove: false,
	}

	// Network mode
	if !opts.InternetAccess {
		hostConfig.NetworkMode = "none"
	}

	// Create container
	resp, err := p.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "")
	if err != nil {
		return nil, fmt.Errorf("create container: %w", err)
	}

	// Start container
	if err := p.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = p.client.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return nil, fmt.Errorf("start container: %w", err)
	}

	return &Instance{
		id:      resp.ID,
		client:  p.client,
		config:  p.config,
		workDir: opts.WorkDir,
		timeout: opts.Timeout,
	}, nil
}

// ensureImage pulls the image if it doesn't exist locally.
func (p *Provider) ensureImage(ctx context.Context, imageName string) error {
	_, err := p.client.ImageInspect(ctx, imageName)
	if err == nil {
		return nil
	}

	reader, err := p.client.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}
	defer func() { _ = reader.Close() }()

	_, err = io.Copy(io.Discard, reader)
	return err
}

// Capabilities returns gVisor provider capabilities.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		SupportsStreaming:  true,
		SupportsAsync:      true,
		SupportsFileSystem: true,
		SupportsNetwork:    true,
		SupportedLanguages: langdetect.SupportedLanguages(),
		MaxExecutionTime:   30 * time.Minute,
		MaxMemoryMB:        4096,
		MaxCPUs:            4,
	}
}

// Validate checks if gVisor runtime is available.
func (p *Provider) Validate(ctx context.Context) error {
	// Check runsc binary
	if _, err := exec.LookPath(p.config.RuntimePath); err != nil {
		return fmt.Errorf("runsc not found: %w (install from https://gvisor.dev/docs/user_guide/install/)", err)
	}

	// Check Docker is available
	if _, err := p.client.Ping(ctx); err != nil {
		return fmt.Errorf("docker not available: %w", err)
	}

	// Check if runsc runtime is configured in Docker
	info, err := p.client.Info(ctx)
	if err != nil {
		return fmt.Errorf("get docker info: %w", err)
	}

	runtimeFound := false
	for name := range info.Runtimes {
		if name == p.config.RuntimeName {
			runtimeFound = true
			break
		}
	}

	if !runtimeFound {
		return fmt.Errorf("gVisor runtime '%s' not configured in Docker. Add to /etc/docker/daemon.json: {\"runtimes\": {\"runsc\": {\"path\": \"/usr/local/bin/runsc\"}}}", p.config.RuntimeName)
	}

	return nil
}

// Close releases provider resources.
func (p *Provider) Close() error {
	return p.client.Close()
}

var _ provider.Provider = (*Provider)(nil)

// Instance represents a running gVisor sandbox.
type Instance struct {
	id      string
	client  *client.Client
	config  *Config
	workDir string
	timeout time.Duration
	mu      sync.RWMutex
	stopped bool
}

// ID returns the container ID.
func (i *Instance) ID() string {
	return i.id
}

// Execute runs code in the gVisor sandbox.
func (i *Instance) Execute(ctx context.Context, code string, opts *executor.ExecutionOptions) (*executor.ExecutionResult, error) {
	i.mu.RLock()
	if i.stopped {
		i.mu.RUnlock()
		return nil, fmt.Errorf("container stopped")
	}
	i.mu.RUnlock()

	if opts == nil {
		opts = executor.DefaultExecutionOptions()
	}

	runtimeInfo, ok := langdetect.GetRuntimeInfo(opts.Language)
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", opts.Language)
	}

	codeFilename := "main" + runtimeInfo.FileExt
	codePath := opts.WorkDir + "/" + codeFilename
	if codePath == "" {
		codePath = i.workDir + "/" + codeFilename
	}

	if err := i.writeFile(ctx, codePath, []byte(code)); err != nil {
		return nil, fmt.Errorf("write code file: %w", err)
	}

	for path, content := range opts.Files {
		if err := i.writeFile(ctx, path, content); err != nil {
			return nil, fmt.Errorf("write file %s: %w", path, err)
		}
	}

	var cmd []string
	if runtimeInfo.CompileCmd != nil {
		compileCmd := make([]string, len(runtimeInfo.CompileCmd)+1)
		copy(compileCmd, runtimeInfo.CompileCmd)
		compileCmd[len(runtimeInfo.CompileCmd)] = codePath
		if _, err := i.runExec(ctx, compileCmd, opts); err != nil {
			return nil, fmt.Errorf("compile: %w", err)
		}
		cmd = runtimeInfo.RunCommand
	} else {
		cmd = make([]string, len(runtimeInfo.RunCommand)+1)
		copy(cmd, runtimeInfo.RunCommand)
		cmd[len(runtimeInfo.RunCommand)] = codePath
	}

	execCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	start := time.Now()

	result, err := i.runExec(execCtx, cmd, opts)
	if err != nil {
		return nil, err
	}

	result.Duration = time.Since(start)
	result.Language = opts.Language

	return result, nil
}

// runExec runs a command in the container.
func (i *Instance) runExec(ctx context.Context, cmd []string, opts *executor.ExecutionOptions) (*executor.ExecutionResult, error) {
	execConfig := container.ExecOptions{
		Cmd:          cmd,
		WorkingDir:   opts.WorkDir,
		AttachStdout: true,
		AttachStderr: true,
		AttachStdin:  opts.Stdin != "",
	}

	for k, v := range opts.Env {
		execConfig.Env = append(execConfig.Env, fmt.Sprintf("%s=%s", k, v))
	}

	execID, err := i.client.ContainerExecCreate(ctx, i.id, execConfig)
	if err != nil {
		return nil, fmt.Errorf("create exec: %w", err)
	}

	resp, err := i.client.ContainerExecAttach(ctx, execID.ID, container.ExecAttachOptions{})
	if err != nil {
		return nil, fmt.Errorf("attach exec: %w", err)
	}
	defer resp.Close()

	if opts.Stdin != "" {
		go func() {
			_, _ = resp.Conn.Write([]byte(opts.Stdin))
			_ = resp.CloseWrite()
		}()
	}

	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, resp.Reader); err != nil {
		return nil, fmt.Errorf("read output: %w", err)
	}

	inspectResp, err := i.client.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return nil, fmt.Errorf("inspect exec: %w", err)
	}

	return &executor.ExecutionResult{
		ExitCode: inspectResp.ExitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

// writeFile writes content to a file in the container.
func (i *Instance) writeFile(ctx context.Context, path string, content []byte) error {
	var buf bytes.Buffer
	tw := newTarWriter(&buf)
	if err := tw.WriteFile(path, content); err != nil {
		return err
	}
	_ = tw.Close()

	dir := path[:strings.LastIndex(path, "/")]
	if dir == "" {
		dir = "/"
	}

	return i.client.CopyToContainer(ctx, i.id, dir, &buf, container.CopyToContainerOptions{})
}

// ExecuteStream runs code with streaming output.
func (i *Instance) ExecuteStream(ctx context.Context, code string, opts *executor.ExecutionOptions, handler executor.StreamHandler) error {
	i.mu.RLock()
	if i.stopped {
		i.mu.RUnlock()
		return fmt.Errorf("container stopped")
	}
	i.mu.RUnlock()

	if opts == nil {
		opts = executor.DefaultExecutionOptions()
	}

	runtimeInfo, ok := langdetect.GetRuntimeInfo(opts.Language)
	if !ok {
		return fmt.Errorf("unsupported language: %s", opts.Language)
	}

	codeFilename := "main" + runtimeInfo.FileExt
	codePath := opts.WorkDir + "/" + codeFilename
	if codePath == "" {
		codePath = i.workDir + "/" + codeFilename
	}

	if err := i.writeFile(ctx, codePath, []byte(code)); err != nil {
		return fmt.Errorf("write code file: %w", err)
	}

	var cmd []string
	if runtimeInfo.CompileCmd != nil {
		compileCmd := make([]string, len(runtimeInfo.CompileCmd)+1)
		copy(compileCmd, runtimeInfo.CompileCmd)
		compileCmd[len(runtimeInfo.CompileCmd)] = codePath
		if _, err := i.runExec(ctx, compileCmd, opts); err != nil {
			return fmt.Errorf("compile: %w", err)
		}
		cmd = runtimeInfo.RunCommand
	} else {
		cmd = make([]string, len(runtimeInfo.RunCommand)+1)
		copy(cmd, runtimeInfo.RunCommand)
		cmd[len(runtimeInfo.RunCommand)] = codePath
	}

	execConfig := container.ExecOptions{
		Cmd:          cmd,
		WorkingDir:   opts.WorkDir,
		AttachStdout: true,
		AttachStderr: true,
	}

	execID, err := i.client.ContainerExecCreate(ctx, i.id, execConfig)
	if err != nil {
		return fmt.Errorf("create exec: %w", err)
	}

	resp, err := i.client.ContainerExecAttach(ctx, execID.ID, container.ExecAttachOptions{})
	if err != nil {
		return fmt.Errorf("attach exec: %w", err)
	}
	defer resp.Close()

	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	go func() {
		_, _ = stdcopy.StdCopy(stdoutWriter, stderrWriter, resp.Reader)
		_ = stdoutWriter.Close()
		_ = stderrWriter.Close()
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		for {
			n, err := stdoutReader.Read(buf)
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
			n, err := stderrReader.Read(buf)
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

	inspectResp, err := i.client.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return fmt.Errorf("inspect exec: %w", err)
	}

	_ = handler(&executor.StreamEvent{
		Type:      executor.StreamComplete,
		ExitCode:  inspectResp.ExitCode,
		Timestamp: time.Now(),
	})

	return nil
}

// RunCommand executes a shell command.
func (i *Instance) RunCommand(ctx context.Context, cmd string, args []string) (*executor.CommandResult, error) {
	i.mu.RLock()
	if i.stopped {
		i.mu.RUnlock()
		return nil, fmt.Errorf("container stopped")
	}
	i.mu.RUnlock()

	fullCmd := append([]string{cmd}, args...)

	execConfig := container.ExecOptions{
		Cmd:          fullCmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	execID, err := i.client.ContainerExecCreate(ctx, i.id, execConfig)
	if err != nil {
		return nil, fmt.Errorf("create exec: %w", err)
	}

	start := time.Now()

	resp, err := i.client.ContainerExecAttach(ctx, execID.ID, container.ExecAttachOptions{})
	if err != nil {
		return nil, fmt.Errorf("attach exec: %w", err)
	}
	defer resp.Close()

	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, resp.Reader); err != nil {
		return nil, fmt.Errorf("read output: %w", err)
	}

	inspectResp, err := i.client.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return nil, fmt.Errorf("inspect exec: %w", err)
	}

	return &executor.CommandResult{
		ExitCode: inspectResp.ExitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: time.Since(start),
	}, nil
}

// FileSystem returns the file system handler.
func (i *Instance) FileSystem() fs.FileSystem {
	return &gvisorFS{instance: i}
}

// Network returns the network handler.
func (i *Instance) Network() provider.Network {
	return &gvisorNetwork{instance: i}
}

// Stop terminates the container.
func (i *Instance) Stop(ctx context.Context) error {
	i.mu.Lock()
	if i.stopped {
		i.mu.Unlock()
		return nil
	}
	i.stopped = true
	i.mu.Unlock()

	if err := i.client.ContainerStop(ctx, i.id, container.StopOptions{}); err != nil {
		if !strings.Contains(err.Error(), "No such container") {
			return fmt.Errorf("stop container: %w", err)
		}
	}

	if err := i.client.ContainerRemove(ctx, i.id, container.RemoveOptions{Force: true}); err != nil {
		if !strings.Contains(err.Error(), "No such container") {
			return fmt.Errorf("remove container: %w", err)
		}
	}

	return nil
}

// Status returns the current status.
func (i *Instance) Status(ctx context.Context) (provider.InstanceStatus, error) {
	i.mu.RLock()
	if i.stopped {
		i.mu.RUnlock()
		return provider.StatusStopped, nil
	}
	i.mu.RUnlock()

	info, err := i.client.ContainerInspect(ctx, i.id)
	if err != nil {
		return provider.StatusError, err
	}

	if info.State.Running {
		return provider.StatusRunning, nil
	}
	if info.State.Paused {
		return provider.StatusPaused, nil
	}

	return provider.StatusStopped, nil
}

var _ provider.Instance = (*Instance)(nil)
