//go:build linux

// Package firecracker provides the Firecracker microVM provider for sindoq.
// Firecracker requires Linux with KVM support.
package firecracker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/happyhackingspace/sindoq/internal/factory"
	"github.com/happyhackingspace/sindoq/internal/provider"
	"github.com/happyhackingspace/sindoq/pkg/executor"
	"github.com/happyhackingspace/sindoq/pkg/fs"
	"github.com/happyhackingspace/sindoq/pkg/langdetect"
)

func init() {
	factory.Register("firecracker", func(config any) (provider.Provider, error) {
		cfg, ok := config.(*Config)
		if !ok && config != nil {
			return nil, fmt.Errorf("invalid config type for firecracker provider")
		}
		return New(cfg)
	})
}

// Config holds Firecracker provider configuration.
type Config struct {
	// FirecrackerBinary is the path to the firecracker binary.
	FirecrackerBinary string

	// KernelImagePath is the path to the uncompressed Linux kernel image.
	KernelImagePath string

	// RootDrivePath is the path to the root filesystem image.
	RootDrivePath string

	// VCPUCount is the number of vCPUs for the VM.
	VCPUCount int64

	// MemSizeMiB is the memory size in MiB.
	MemSizeMiB int64

	// SocketDir is the directory for VM sockets.
	SocketDir string

	// EnableNetwork enables networking via TAP device.
	EnableNetwork bool

	// NetworkInterface is the host network interface for bridging.
	NetworkInterface string

	// SSHKeyPath is the path to SSH key for VM access (if using SSH).
	SSHKeyPath string

	// VMIPAddress is the IP address assigned to the VM.
	VMIPAddress string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		FirecrackerBinary: "firecracker",
		KernelImagePath:   "/var/lib/firecracker/vmlinux",
		RootDrivePath:     "/var/lib/firecracker/rootfs.ext4",
		VCPUCount:         2,
		MemSizeMiB:        1024,
		SocketDir:         "/tmp/firecracker",
		EnableNetwork:     false,
		VMIPAddress:       "172.16.0.2",
	}
}

// Provider implements the Firecracker microVM provider.
type Provider struct {
	config    *Config
	instances map[string]*Instance
	mu        sync.RWMutex
}

// New creates a new Firecracker provider.
func New(cfg *Config) (*Provider, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Ensure socket directory exists
	if err := os.MkdirAll(cfg.SocketDir, 0755); err != nil {
		return nil, fmt.Errorf("creating socket dir: %w", err)
	}

	return &Provider{
		config:    cfg,
		instances: make(map[string]*Instance),
	}, nil
}

// Name returns the provider identifier.
func (p *Provider) Name() string {
	return "firecracker"
}

// Create initializes a new Firecracker microVM sandbox.
func (p *Provider) Create(ctx context.Context, opts *provider.CreateOptions) (provider.Instance, error) {
	id := fmt.Sprintf("fc-%d", time.Now().UnixNano())
	socketPath := filepath.Join(p.config.SocketDir, id+".sock")

	// Build VM configuration
	fcCfg := firecracker.Config{
		SocketPath:      socketPath,
		KernelImagePath: p.config.KernelImagePath,
		KernelArgs:      "console=ttyS0 reboot=k panic=1 pci=off init=/sbin/init",
		Drives: []models.Drive{
			{
				DriveID:      firecracker.String("rootfs"),
				PathOnHost:   firecracker.String(p.config.RootDrivePath),
				IsRootDevice: firecracker.Bool(true),
				IsReadOnly:   firecracker.Bool(false),
			},
		},
		MachineCfg: models.MachineConfiguration{
			VcpuCount:  firecracker.Int64(p.config.VCPUCount),
			MemSizeMib: firecracker.Int64(p.config.MemSizeMiB),
		},
	}

	// Add network if enabled
	if p.config.EnableNetwork {
		fcCfg.NetworkInterfaces = []firecracker.NetworkInterface{
			{
				StaticConfiguration: &firecracker.StaticNetworkConfiguration{
					MacAddress:  "AA:FC:00:00:00:01",
					HostDevName: "tap0",
				},
			},
		}
	}

	// Find firecracker binary
	firecrackerBinary := p.config.FirecrackerBinary
	if _, err := exec.LookPath(firecrackerBinary); err != nil {
		return nil, fmt.Errorf("firecracker binary not found: %w", err)
	}

	// Create machine
	cmd := firecracker.VMCommandBuilder{}.
		WithBin(firecrackerBinary).
		WithSocketPath(socketPath).
		Build(ctx)

	machine, err := firecracker.NewMachine(ctx, fcCfg, firecracker.WithProcessRunner(cmd))
	if err != nil {
		return nil, fmt.Errorf("creating machine: %w", err)
	}

	// Start the VM
	if err := machine.Start(ctx); err != nil {
		return nil, fmt.Errorf("starting VM: %w", err)
	}

	// Wait for VM to be ready
	time.Sleep(2 * time.Second)

	instance := &Instance{
		id:         id,
		provider:   p,
		machine:    machine,
		socketPath: socketPath,
		status:     provider.StatusRunning,
		config:     p.config,
	}

	p.mu.Lock()
	p.instances[id] = instance
	p.mu.Unlock()

	return instance, nil
}

// Capabilities returns Firecracker provider capabilities.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		SupportsStreaming:  true,
		SupportsAsync:      true,
		SupportsFileSystem: true,
		SupportsNetwork:    p.config.EnableNetwork,
		SupportedLanguages: langdetect.SupportedLanguages(),
		MaxExecutionTime:   24 * time.Hour,
		MaxMemoryMB:        int(p.config.MemSizeMiB),
		MaxCPUs:            int(p.config.VCPUCount),
	}
}

// Validate checks if Firecracker is accessible.
func (p *Provider) Validate(ctx context.Context) error {
	// Check for KVM
	if _, err := os.Stat("/dev/kvm"); os.IsNotExist(err) {
		return fmt.Errorf("KVM not available: /dev/kvm not found (requires Linux with KVM support)")
	}

	// Check firecracker binary
	if _, err := exec.LookPath(p.config.FirecrackerBinary); err != nil {
		return fmt.Errorf("firecracker binary not found: %w", err)
	}

	// Check kernel image
	if _, err := os.Stat(p.config.KernelImagePath); os.IsNotExist(err) {
		return fmt.Errorf("kernel image not found: %s", p.config.KernelImagePath)
	}

	// Check rootfs
	if _, err := os.Stat(p.config.RootDrivePath); os.IsNotExist(err) {
		return fmt.Errorf("root filesystem not found: %s", p.config.RootDrivePath)
	}

	return nil
}

// Close releases provider resources.
func (p *Provider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var lastErr error
	for _, instance := range p.instances {
		if err := instance.Stop(context.Background()); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

var _ provider.Provider = (*Provider)(nil)

// Instance represents a running Firecracker microVM.
type Instance struct {
	id         string
	provider   *Provider
	machine    *firecracker.Machine
	socketPath string
	status     provider.InstanceStatus
	config     *Config
	mu         sync.RWMutex
	stopped    bool
}

// ID returns the instance ID.
func (i *Instance) ID() string {
	return i.id
}

// Execute runs code in the microVM via serial console or SSH.
func (i *Instance) Execute(ctx context.Context, code string, opts *executor.ExecutionOptions) (*executor.ExecutionResult, error) {
	i.mu.RLock()
	if i.stopped {
		i.mu.RUnlock()
		return nil, fmt.Errorf("VM stopped")
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

	start := time.Now()

	// Execute via SSH if network is enabled and SSH key is configured
	if i.config.EnableNetwork && i.config.SSHKeyPath != "" {
		result, err := i.executeViaSSH(ctx, code, runtimeInfo, opts)
		if err != nil {
			return nil, err
		}
		result.Duration = time.Since(start)
		result.Language = opts.Language
		return result, nil
	}

	// Fallback: execute via serial console (limited functionality)
	result, err := i.executeViaSerial(ctx, code, runtimeInfo, opts)
	if err != nil {
		return nil, err
	}
	result.Duration = time.Since(start)
	result.Language = opts.Language
	return result, nil
}

// executeViaSSH runs code through SSH connection to the VM.
func (i *Instance) executeViaSSH(ctx context.Context, code string, runtimeInfo *langdetect.RuntimeInfo, opts *executor.ExecutionOptions) (*executor.ExecutionResult, error) {
	// Build the code file content
	codeFilename := "main" + runtimeInfo.FileExt

	// Build SSH command to write file and execute
	sshArgs := []string{
		"-i", i.config.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=5",
		fmt.Sprintf("root@%s", i.config.VMIPAddress),
	}

	// Write code to file
	writeCmd := fmt.Sprintf("cat > /tmp/%s << 'SINDOQ_EOF'\n%s\nSINDOQ_EOF", codeFilename, code)
	sshWriteArgs := make([]string, len(sshArgs)+1)
	copy(sshWriteArgs, sshArgs)
	sshWriteArgs[len(sshArgs)] = writeCmd

	writeExec := exec.CommandContext(ctx, "ssh", sshWriteArgs...)
	if err := writeExec.Run(); err != nil {
		return nil, fmt.Errorf("write code to VM: %w", err)
	}

	// Build run command
	var runCmd string
	codePath := "/tmp/" + codeFilename
	if runtimeInfo.CompileCmd != nil {
		compileParts := make([]string, len(runtimeInfo.CompileCmd)+1)
		copy(compileParts, runtimeInfo.CompileCmd)
		compileParts[len(runtimeInfo.CompileCmd)] = codePath
		compileCmd := strings.Join(compileParts, " ")
		execCmd := strings.Join(runtimeInfo.RunCommand, " ")
		runCmd = fmt.Sprintf("%s && %s", compileCmd, execCmd)
	} else {
		runParts := make([]string, len(runtimeInfo.RunCommand)+1)
		copy(runParts, runtimeInfo.RunCommand)
		runParts[len(runtimeInfo.RunCommand)] = codePath
		runCmd = strings.Join(runParts, " ")
	}

	// Add stdin handling
	if opts.Stdin != "" {
		runCmd = fmt.Sprintf("echo '%s' | %s", opts.Stdin, runCmd)
	}

	// Add environment variables
	var envPrefix string
	for k, v := range opts.Env {
		envPrefix += fmt.Sprintf("%s=%s ", k, v)
	}
	if envPrefix != "" {
		runCmd = envPrefix + runCmd
	}

	sshRunArgs := make([]string, len(sshArgs)+1)
	copy(sshRunArgs, sshArgs)
	sshRunArgs[len(sshArgs)] = runCmd
	runExec := exec.CommandContext(ctx, "ssh", sshRunArgs...)

	var stdout, stderr bytes.Buffer
	runExec.Stdout = &stdout
	runExec.Stderr = &stderr

	err := runExec.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("execute in VM: %w", err)
		}
	}

	return &executor.ExecutionResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

// executeViaSerial provides basic execution via serial console.
// This is a simplified implementation - full serial console handling requires more complex I/O management.
func (i *Instance) executeViaSerial(ctx context.Context, code string, runtimeInfo *langdetect.RuntimeInfo, opts *executor.ExecutionOptions) (*executor.ExecutionResult, error) {
	// Serial console execution is limited - return an error suggesting SSH setup
	return nil, fmt.Errorf("serial console execution not fully implemented; enable network and configure SSHKeyPath for full functionality")
}

// ExecuteStream runs code with streaming output.
func (i *Instance) ExecuteStream(ctx context.Context, code string, opts *executor.ExecutionOptions, handler executor.StreamHandler) error {
	i.mu.RLock()
	if i.stopped {
		i.mu.RUnlock()
		return fmt.Errorf("VM stopped")
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

	if !i.config.EnableNetwork || i.config.SSHKeyPath == "" {
		return fmt.Errorf("streaming requires network and SSH; enable network and configure SSHKeyPath")
	}

	codeFilename := "main" + runtimeInfo.FileExt

	// Write code to VM
	sshArgs := []string{
		"-i", i.config.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=5",
		fmt.Sprintf("root@%s", i.config.VMIPAddress),
	}

	writeCmd := fmt.Sprintf("cat > /tmp/%s << 'SINDOQ_EOF'\n%s\nSINDOQ_EOF", codeFilename, code)
	sshWriteArgs2 := make([]string, len(sshArgs)+1)
	copy(sshWriteArgs2, sshArgs)
	sshWriteArgs2[len(sshArgs)] = writeCmd

	writeExec := exec.CommandContext(ctx, "ssh", sshWriteArgs2...)
	if err := writeExec.Run(); err != nil {
		return fmt.Errorf("write code to VM: %w", err)
	}

	// Build run command
	var runCmd string
	streamCodePath := "/tmp/" + codeFilename
	if runtimeInfo.CompileCmd != nil {
		compileParts := make([]string, len(runtimeInfo.CompileCmd)+1)
		copy(compileParts, runtimeInfo.CompileCmd)
		compileParts[len(runtimeInfo.CompileCmd)] = streamCodePath
		compileCmd := strings.Join(compileParts, " ")
		execCmd := strings.Join(runtimeInfo.RunCommand, " ")
		runCmd = fmt.Sprintf("%s && %s", compileCmd, execCmd)
	} else {
		runParts := make([]string, len(runtimeInfo.RunCommand)+1)
		copy(runParts, runtimeInfo.RunCommand)
		runParts[len(runtimeInfo.RunCommand)] = streamCodePath
		runCmd = strings.Join(runParts, " ")
	}

	sshRunArgs := make([]string, len(sshArgs)+1)
	copy(sshRunArgs, sshArgs)
	sshRunArgs[len(sshArgs)] = runCmd
	runExec := exec.CommandContext(ctx, "ssh", sshRunArgs...)

	stdoutPipe, err := runExec.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	stderrPipe, err := runExec.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe: %w", err)
	}

	if err := runExec.Start(); err != nil {
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
	if err := runExec.Wait(); err != nil {
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

// RunCommand executes a shell command in the VM.
func (i *Instance) RunCommand(ctx context.Context, cmd string, args []string) (*executor.CommandResult, error) {
	i.mu.RLock()
	if i.stopped {
		i.mu.RUnlock()
		return nil, fmt.Errorf("VM stopped")
	}
	i.mu.RUnlock()

	if !i.config.EnableNetwork || i.config.SSHKeyPath == "" {
		return nil, fmt.Errorf("RunCommand requires network and SSH; enable network and configure SSHKeyPath")
	}

	start := time.Now()

	// Build full command
	fullCmd := cmd
	if len(args) > 0 {
		fullCmd = cmd + " " + strings.Join(args, " ")
	}

	sshArgs := []string{
		"-i", i.config.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=5",
		fmt.Sprintf("root@%s", i.config.VMIPAddress),
		fullCmd,
	}

	sshExec := exec.CommandContext(ctx, "ssh", sshArgs...)

	var stdout, stderr bytes.Buffer
	sshExec.Stdout = &stdout
	sshExec.Stderr = &stderr

	err := sshExec.Run()
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
	return &firecrackerFS{
		instance: i,
	}
}

// Network returns the network handler.
func (i *Instance) Network() provider.Network {
	if !i.config.EnableNetwork {
		return nil
	}
	return &firecrackerNetwork{
		instance: i,
	}
}

// Stop terminates the VM.
func (i *Instance) Stop(ctx context.Context) error {
	i.mu.Lock()
	if i.stopped {
		i.mu.Unlock()
		return nil
	}
	i.stopped = true
	i.status = provider.StatusStopped
	i.mu.Unlock()

	// Stop the VM
	if i.machine != nil {
		if err := i.machine.StopVMM(); err != nil {
			// Log but don't fail
			_ = err
		}
		if err := i.machine.Shutdown(ctx); err != nil {
			// Ignore shutdown errors
			_ = err
		}
	}

	// Clean up socket
	if i.socketPath != "" {
		_ = os.Remove(i.socketPath)
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

	return i.status, nil
}

var _ provider.Instance = (*Instance)(nil)

// firecrackerFS implements filesystem operations for Firecracker VMs.
type firecrackerFS struct {
	instance *Instance
}

func (f *firecrackerFS) Read(ctx context.Context, path string) ([]byte, error) {
	if !f.instance.config.EnableNetwork || f.instance.config.SSHKeyPath == "" {
		return nil, fmt.Errorf("filesystem operations require network and SSH")
	}

	sshArgs := []string{
		"-i", f.instance.config.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("root@%s", f.instance.config.VMIPAddress),
		fmt.Sprintf("cat %s", path),
	}

	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return output, nil
}

func (f *firecrackerFS) Write(ctx context.Context, path string, data []byte) error {
	if !f.instance.config.EnableNetwork || f.instance.config.SSHKeyPath == "" {
		return fmt.Errorf("filesystem operations require network and SSH")
	}

	sshArgs := []string{
		"-i", f.instance.config.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("root@%s", f.instance.config.VMIPAddress),
		fmt.Sprintf("cat > %s", path),
	}

	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	cmd.Stdin = bytes.NewReader(data)
	return cmd.Run()
}

func (f *firecrackerFS) Delete(ctx context.Context, path string) error {
	if !f.instance.config.EnableNetwork || f.instance.config.SSHKeyPath == "" {
		return fmt.Errorf("filesystem operations require network and SSH")
	}

	sshArgs := []string{
		"-i", f.instance.config.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("root@%s", f.instance.config.VMIPAddress),
		fmt.Sprintf("rm -f %s", path),
	}

	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	return cmd.Run()
}

func (f *firecrackerFS) Exists(ctx context.Context, path string) (bool, error) {
	if !f.instance.config.EnableNetwork || f.instance.config.SSHKeyPath == "" {
		return false, fmt.Errorf("filesystem operations require network and SSH")
	}

	sshArgs := []string{
		"-i", f.instance.config.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("root@%s", f.instance.config.VMIPAddress),
		fmt.Sprintf("test -e %s && echo yes || echo no", path),
	}

	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(output)) == "yes", nil
}

func (f *firecrackerFS) List(ctx context.Context, path string) ([]fs.FileInfo, error) {
	if !f.instance.config.EnableNetwork || f.instance.config.SSHKeyPath == "" {
		return nil, fmt.Errorf("filesystem operations require network and SSH")
	}

	sshArgs := []string{
		"-i", f.instance.config.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("root@%s", f.instance.config.VMIPAddress),
		fmt.Sprintf("ls -la %s", path),
	}

	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list directory: %w", err)
	}

	// Parse ls output (simplified)
	var files []fs.FileInfo
	lines := strings.Split(string(output), "\n")
	for _, line := range lines[1:] { // Skip "total" line
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}
		name := fields[8]
		if name == "." || name == ".." {
			continue
		}
		files = append(files, fs.FileInfo{
			Name:  name,
			IsDir: fields[0][0] == 'd',
		})
	}
	return files, nil
}

func (f *firecrackerFS) MkDir(ctx context.Context, path string) error {
	if !f.instance.config.EnableNetwork || f.instance.config.SSHKeyPath == "" {
		return fmt.Errorf("filesystem operations require network and SSH")
	}

	sshArgs := []string{
		"-i", f.instance.config.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("root@%s", f.instance.config.VMIPAddress),
		fmt.Sprintf("mkdir -p %s", path),
	}

	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	return cmd.Run()
}

func (f *firecrackerFS) Stat(ctx context.Context, path string) (*fs.FileInfo, error) {
	if !f.instance.config.EnableNetwork || f.instance.config.SSHKeyPath == "" {
		return nil, fmt.Errorf("filesystem operations require network and SSH")
	}

	sshArgs := []string{
		"-i", f.instance.config.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("root@%s", f.instance.config.VMIPAddress),
		fmt.Sprintf("stat -c '%%n %%s %%F' %s", path),
	}

	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	fields := strings.Fields(strings.TrimSpace(string(output)))
	if len(fields) < 3 {
		return nil, fmt.Errorf("unexpected stat output")
	}

	var size int64
	_, _ = fmt.Sscanf(fields[1], "%d", &size)

	return &fs.FileInfo{
		Name:  filepath.Base(fields[0]),
		Path:  path,
		Size:  size,
		IsDir: strings.Contains(fields[2], "directory"),
	}, nil
}

func (f *firecrackerFS) UploadReader(ctx context.Context, reader io.Reader, remotePath string) error {
	if !f.instance.config.EnableNetwork || f.instance.config.SSHKeyPath == "" {
		return fmt.Errorf("filesystem operations require network and SSH")
	}

	sshArgs := []string{
		"-i", f.instance.config.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("root@%s", f.instance.config.VMIPAddress),
		fmt.Sprintf("cat > %s", remotePath),
	}

	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	cmd.Stdin = reader
	return cmd.Run()
}

func (f *firecrackerFS) Move(ctx context.Context, src, dst string) error {
	if !f.instance.config.EnableNetwork || f.instance.config.SSHKeyPath == "" {
		return fmt.Errorf("filesystem operations require network and SSH")
	}

	sshArgs := []string{
		"-i", f.instance.config.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("root@%s", f.instance.config.VMIPAddress),
		fmt.Sprintf("mv %s %s", src, dst),
	}

	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	return cmd.Run()
}

func (f *firecrackerFS) Copy(ctx context.Context, src, dst string) error {
	if !f.instance.config.EnableNetwork || f.instance.config.SSHKeyPath == "" {
		return fmt.Errorf("filesystem operations require network and SSH")
	}

	sshArgs := []string{
		"-i", f.instance.config.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("root@%s", f.instance.config.VMIPAddress),
		fmt.Sprintf("cp -r %s %s", src, dst),
	}

	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	return cmd.Run()
}

func (f *firecrackerFS) Upload(ctx context.Context, localPath, remotePath string) error {
	if !f.instance.config.EnableNetwork || f.instance.config.SSHKeyPath == "" {
		return fmt.Errorf("filesystem operations require network and SSH")
	}

	// Read local file
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read local file: %w", err)
	}

	return f.Write(ctx, remotePath, data)
}

func (f *firecrackerFS) Download(ctx context.Context, remotePath string, writer io.Writer) error {
	if !f.instance.config.EnableNetwork || f.instance.config.SSHKeyPath == "" {
		return fmt.Errorf("filesystem operations require network and SSH")
	}

	data, err := f.Read(ctx, remotePath)
	if err != nil {
		return err
	}

	_, err = writer.Write(data)
	return err
}

var _ fs.FileSystem = (*firecrackerFS)(nil)

// firecrackerNetwork implements network operations for Firecracker VMs.
type firecrackerNetwork struct {
	instance *Instance
	ports    map[int]*provider.PublishedPort
	mu       sync.RWMutex
}

func (n *firecrackerNetwork) PublishPort(ctx context.Context, port int) (*provider.PublishedPort, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.ports == nil {
		n.ports = make(map[int]*provider.PublishedPort)
	}

	// For Firecracker, port publishing is typically done via iptables rules on the host
	// This is a simplified implementation
	published := &provider.PublishedPort{
		LocalPort:  port,
		PublicPort: port,
		Protocol:   "tcp",
		PublicURL:  fmt.Sprintf("http://%s:%d", n.instance.config.VMIPAddress, port),
	}

	n.ports[port] = published
	return published, nil
}

func (n *firecrackerNetwork) GetPublicURL(port int) (string, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if p, ok := n.ports[port]; ok {
		return p.PublicURL, nil
	}
	return fmt.Sprintf("http://%s:%d", n.instance.config.VMIPAddress, port), nil
}

func (n *firecrackerNetwork) ListPorts(ctx context.Context) ([]*provider.PublishedPort, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	ports := make([]*provider.PublishedPort, 0, len(n.ports))
	for _, p := range n.ports {
		ports = append(ports, p)
	}
	return ports, nil
}

func (n *firecrackerNetwork) UnpublishPort(ctx context.Context, port int) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	delete(n.ports, port)
	return nil
}

var _ provider.Network = (*firecrackerNetwork)(nil)
