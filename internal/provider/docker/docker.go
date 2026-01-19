// Package docker provides the Docker container provider for sindoq.
package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
	// Register the Docker provider
	factory.Register("docker", func(config any) (provider.Provider, error) {
		cfg, ok := config.(*Config)
		if !ok && config != nil {
			return nil, fmt.Errorf("invalid config type for docker provider")
		}
		return New(cfg)
	})
}

// Config holds Docker provider configuration.
type Config struct {
	Host         string
	APIVersion   string
	TLSVerify    bool
	CertPath     string
	RegistryAuth map[string]string
	DefaultImage string
}

// DefaultConfig returns default Docker configuration.
func DefaultConfig() *Config {
	return &Config{
		DefaultImage: "python:3.12-slim",
	}
}

// Provider implements the Docker container provider.
type Provider struct {
	config *Config
	client *client.Client
	mu     sync.RWMutex
}

// New creates a new Docker provider.
func New(cfg *Config) (*Provider, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	opts := []client.Opt{
		client.WithAPIVersionNegotiation(),
	}

	if cfg.Host != "" {
		opts = append(opts, client.WithHost(cfg.Host))
	}

	if cfg.APIVersion != "" {
		opts = append(opts, client.WithVersion(cfg.APIVersion))
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
	return "docker"
}

// Create initializes a new container sandbox.
func (p *Provider) Create(ctx context.Context, opts *provider.CreateOptions) (provider.Instance, error) {
	if opts == nil {
		opts = provider.DefaultCreateOptions()
	}

	// Determine image
	image := opts.Image
	if image == "" {
		// Try to get image from runtime
		if opts.Runtime != "" {
			if info, ok := langdetect.GetRuntimeInfo(opts.Runtime); ok {
				image = info.DockerImage
			}
		}
		if image == "" {
			image = p.config.DefaultImage
		}
	}

	// Pull image if needed
	if err := p.ensureImage(ctx, image); err != nil {
		return nil, fmt.Errorf("ensure image: %w", err)
	}

	// Build environment variables
	env := make([]string, 0, len(opts.Environment))
	for k, v := range opts.Environment {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Container configuration
	containerConfig := &container.Config{
		Image:        image,
		Env:          env,
		WorkingDir:   opts.WorkDir,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
		Labels:       opts.Labels,
		// Keep container running
		Entrypoint: []string{"tail", "-f", "/dev/null"},
	}

	// Host configuration
	hostConfig := &container.HostConfig{
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
		// Clean up on failure
		p.client.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
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

	if !client.IsErrNotFound(err) {
		if isDockerConnectionError(err) {
			return fmt.Errorf("docker connection failed: %w\n\nTroubleshooting:\n  - Is the Docker daemon running?\n  - Do you have permission to access /var/run/docker.sock?", err)
		}
		return fmt.Errorf("inspect image: %w", err)
	}

	reader, err := p.client.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		if isDockerConnectionError(err) {
			return fmt.Errorf("docker connection failed: %w\n\nTroubleshooting:\n  - Is the Docker daemon running?\n  - Do you have permission to access /var/run/docker.sock?", err)
		}
		return fmt.Errorf("pull image: %w", err)
	}
	defer reader.Close()

	if _, err := io.Copy(io.Discard, reader); err != nil {
		return fmt.Errorf("read image pull output: %w", err)
	}
	return nil
}

func isDockerConnectionError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "Cannot connect") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "/var/run/docker.sock")
}

// Capabilities returns Docker provider capabilities.
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

// Validate checks if Docker is available.
func (p *Provider) Validate(ctx context.Context) error {
	_, err := p.client.Ping(ctx)
	if err != nil {
		return fmt.Errorf("docker not available: %w", err)
	}
	return nil
}

// Close releases provider resources.
func (p *Provider) Close() error {
	return p.client.Close()
}

// Instance represents a running Docker container.
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

// Execute runs code in the container.
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

	// Get runtime info
	runtimeInfo, ok := langdetect.GetRuntimeInfo(opts.Language)
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", opts.Language)
	}

	// Write code to file
	codeFilename := "main" + runtimeInfo.FileExt
	codePath := opts.WorkDir + "/" + codeFilename
	if codePath == "" {
		codePath = i.workDir + "/" + codeFilename
	}

	if err := i.writeFile(ctx, codePath, []byte(code)); err != nil {
		return nil, fmt.Errorf("write code file: %w", err)
	}

	// Write additional files
	for path, content := range opts.Files {
		if err := i.writeFile(ctx, path, content); err != nil {
			return nil, fmt.Errorf("write file %s: %w", path, err)
		}
	}

	// Build command
	var cmd []string
	if runtimeInfo.CompileCmd != nil {
		// Compile step
		compileCmd := append(runtimeInfo.CompileCmd, codePath)
		if _, err := i.runExec(ctx, compileCmd, opts); err != nil {
			return nil, fmt.Errorf("compile: %w", err)
		}
		cmd = runtimeInfo.RunCommand
	} else {
		cmd = append(runtimeInfo.RunCommand, codePath)
	}

	// Set timeout
	execCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	start := time.Now()

	// Run the code
	result, err := i.runExec(execCtx, cmd, opts)
	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			if opts.Timeout > 0 {
				return nil, fmt.Errorf("execution timed out after %v: %v\n\nTip: You can increase the timeout using the -timeout flag (e.g., -timeout 10m)", opts.Timeout, err)
			}
			return nil, fmt.Errorf("execution timed out: %v\n\nTip: You can set a timeout using the -timeout flag (e.g., -timeout 5m)", err)
		}
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

	// Add environment variables
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

	// Write stdin if provided
	if opts.Stdin != "" {
		go func() {
			resp.Conn.Write([]byte(opts.Stdin))
			resp.CloseWrite()
		}()
	}

	// Read output using stdcopy to demultiplex stdout/stderr
	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, resp.Reader); err != nil {
		return nil, fmt.Errorf("read output: %w", err)
	}

	// Get exit code
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
	// Use tar archive to copy file
	var buf bytes.Buffer
	tw := newTarWriter(&buf)
	if err := tw.WriteFile(path, content); err != nil {
		return err
	}
	tw.Close()

	// Get directory
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

	// Get runtime info
	runtimeInfo, ok := langdetect.GetRuntimeInfo(opts.Language)
	if !ok {
		return fmt.Errorf("unsupported language: %s", opts.Language)
	}

	// Write code to file
	codeFilename := "main" + runtimeInfo.FileExt
	codePath := opts.WorkDir + "/" + codeFilename
	if codePath == "" {
		codePath = i.workDir + "/" + codeFilename
	}

	if err := i.writeFile(ctx, codePath, []byte(code)); err != nil {
		return fmt.Errorf("write code file: %w", err)
	}

	// Build command
	var cmd []string
	if runtimeInfo.CompileCmd != nil {
		compileCmd := append(runtimeInfo.CompileCmd, codePath)
		if _, err := i.runExec(ctx, compileCmd, opts); err != nil {
			return fmt.Errorf("compile: %w", err)
		}
		cmd = runtimeInfo.RunCommand
	} else {
		cmd = append(runtimeInfo.RunCommand, codePath)
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

	// Stream output
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	go func() {
		stdcopy.StdCopy(stdoutWriter, stderrWriter, resp.Reader)
		stdoutWriter.Close()
		stderrWriter.Close()
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	// Stream stdout
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		for {
			n, err := stdoutReader.Read(buf)
			if n > 0 {
				handler(&executor.StreamEvent{
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
			n, err := stderrReader.Read(buf)
			if n > 0 {
				handler(&executor.StreamEvent{
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

	// Get exit code
	inspectResp, err := i.client.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return fmt.Errorf("inspect exec: %w", err)
	}

	// Send completion event
	handler(&executor.StreamEvent{
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
	return &dockerFS{
		instance: i,
	}
}

// Network returns the network handler.
func (i *Instance) Network() provider.Network {
	return &dockerNetwork{
		instance: i,
	}
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

	// Stop container
	if err := i.client.ContainerStop(ctx, i.id, container.StopOptions{}); err != nil {
		// Ignore "not found" errors
		if !strings.Contains(err.Error(), "No such container") {
			return fmt.Errorf("stop container: %w", err)
		}
	}

	// Remove container
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

// Ensure Provider implements the interface
var _ provider.Provider = (*Provider)(nil)
var _ provider.Instance = (*Instance)(nil)
