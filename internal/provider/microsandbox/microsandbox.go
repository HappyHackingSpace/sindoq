//go:build microsandbox && cgo

// Package microsandbox provides the microsandbox microVM provider for sindoq.
// It needs cgo and the "microsandbox" build tag because the microsandbox Go SDK
// embeds a native FFI library.
package microsandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"strings"
	"sync"
	"time"

	msb "github.com/superradcompany/microsandbox/sdk/go"

	"github.com/happyhackingspace/sindoq/internal/factory"
	"github.com/happyhackingspace/sindoq/internal/provider"
	"github.com/happyhackingspace/sindoq/pkg/executor"
	"github.com/happyhackingspace/sindoq/pkg/fs"
	"github.com/happyhackingspace/sindoq/pkg/langdetect"
)

func init() {
	factory.Register("microsandbox", func(config any) (provider.Provider, error) {
		cfg, ok := config.(*Config)
		if !ok && config != nil {
			return nil, fmt.Errorf("invalid config type for microsandbox provider")
		}
		return New(cfg)
	})
}

// Config holds microsandbox provider configuration.
type Config struct {
	// DefaultImage is the OCI image used when neither an image nor a runtime is set.
	DefaultImage string

	// SkipRuntimeInstall disables downloading the msb runtime to ~/.microsandbox
	// on first use. The runtime must then already be installed.
	SkipRuntimeInstall bool
}

// DefaultConfig returns default microsandbox configuration.
func DefaultConfig() *Config {
	return &Config{
		DefaultImage: "python:3.12-slim",
	}
}

// Provider implements the microsandbox microVM provider.
type Provider struct {
	config *Config

	mu           sync.Mutex
	runtimeReady bool
}

// New creates a new microsandbox provider.
func New(cfg *Config) (*Provider, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Provider{config: cfg}, nil
}

// Name returns the provider identifier.
func (p *Provider) Name() string {
	return "microsandbox"
}

// ensureRuntime resolves (or installs) the msb runtime once. Failures are not
// cached, so a cancelled context does not poison later calls.
func (p *Provider) ensureRuntime(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.runtimeReady {
		return nil
	}

	var err error
	if p.config.SkipRuntimeInstall {
		_, err = msb.ResolveRuntime(msb.RuntimeConfig{})
	} else {
		_, err = msb.EnsureRuntime(ctx, msb.RuntimeConfig{}, msb.InstallOptions{})
	}
	if err != nil {
		return fmt.Errorf("microsandbox runtime not available: %w", err)
	}

	p.runtimeReady = true
	return nil
}

// Create boots a new microVM sandbox.
func (p *Provider) Create(ctx context.Context, opts *provider.CreateOptions) (provider.Instance, error) {
	if opts == nil {
		opts = provider.DefaultCreateOptions()
	}

	if err := p.ensureRuntime(ctx); err != nil {
		return nil, err
	}

	name, err := newSandboxName()
	if err != nil {
		return nil, err
	}

	sb, err := msb.CreateSandbox(ctx, name, sandboxOptions(opts, p.config.DefaultImage)...)
	if err != nil {
		return nil, fmt.Errorf("create sandbox: %w", err)
	}

	instance := &Instance{sb: sb, workDir: opts.WorkDir}

	if opts.WorkDir != "" {
		if err := sb.FS().Mkdir(ctx, opts.WorkDir); err != nil {
			err = fmt.Errorf("create workdir %s: %w", opts.WorkDir, err)
			return nil, errors.Join(err, instance.Stop(context.WithoutCancel(ctx)))
		}
	}

	return instance, nil
}

// sandboxOptions maps sindoq create options onto microsandbox options.
func sandboxOptions(opts *provider.CreateOptions, defaultImage string) []msb.SandboxOption {
	sbOpts := []msb.SandboxOption{
		msb.WithImage(resolveImage(opts, defaultImage)),
		// Remove on-disk state, logs and DB rows once the sandbox stops.
		msb.WithEphemeral(true),
		msb.WithQuietLogs(),
	}

	if opts.Resources.MemoryMB > 0 {
		sbOpts = append(sbOpts, msb.WithMemory(uint32(opts.Resources.MemoryMB)))
	}
	if opts.Resources.CPUs > 0 {
		sbOpts = append(sbOpts, msb.WithCPUs(vcpuCount(opts.Resources.CPUs)))
	}
	if len(opts.Environment) > 0 {
		sbOpts = append(sbOpts, msb.WithEnv(opts.Environment))
	}
	if len(opts.Labels) > 0 {
		sbOpts = append(sbOpts, msb.WithLabels(opts.Labels))
	}
	// opts.Timeout is not mapped to msb.WithMaxDuration: sindoq fills it with the
	// default execution timeout (30s), which would end the sandbox after 30s.
	// Executions are bounded per call instead (see execOptions and ExecuteStream).
	if !opts.InternetAccess {
		sbOpts = append(sbOpts, msb.WithNetwork(msb.NetworkPolicy.None()))
	}

	return sbOpts
}

// resolveImage picks the image: explicit image, then runtime default, then provider default.
func resolveImage(opts *provider.CreateOptions, defaultImage string) string {
	if opts.Image != "" {
		return opts.Image
	}
	if opts.Runtime != "" {
		if info, ok := langdetect.GetRuntimeInfo(opts.Runtime); ok && info.DockerImage != "" {
			return info.DockerImage
		}
	}
	if defaultImage != "" {
		return defaultImage
	}
	return DefaultConfig().DefaultImage
}

// vcpuCount rounds fractional CPU limits up to whole vCPUs (microVMs cannot
// get a fraction of a vCPU) and clamps to the SDK's uint8 range.
func vcpuCount(cpus float64) uint8 {
	return uint8(math.Min(math.Ceil(cpus), math.MaxUint8))
}

func newSandboxName() (string, error) {
	return randomName("sindoq-")
}

func randomName(prefix string) (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate name: %w", err)
	}
	return prefix + hex.EncodeToString(b), nil
}

// Capabilities returns microsandbox provider capabilities.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		SupportsStreaming:  true,
		SupportsAsync:      true,
		SupportsFileSystem: true,
		SupportedLanguages: langdetect.SupportedLanguages(),
	}
}

// Validate checks that the microsandbox runtime is available, installing it
// unless SkipRuntimeInstall is set.
func (p *Provider) Validate(ctx context.Context) error {
	return p.ensureRuntime(ctx)
}

// Close releases provider resources. Instances are stopped by their owners.
func (p *Provider) Close() error {
	return nil
}

// Instance represents a running microsandbox microVM.
type Instance struct {
	sb      *msb.Sandbox
	workDir string
	mu      sync.RWMutex
	stopped bool
}

// ID returns the sandbox name.
func (i *Instance) ID() string {
	return i.sb.Name()
}

func (i *Instance) checkRunning() error {
	i.mu.RLock()
	defer i.mu.RUnlock()
	if i.stopped {
		return fmt.Errorf("sandbox stopped")
	}
	return nil
}

// prepare writes the code and extra files into the sandbox, runs the compile
// step if the language needs one, and returns the run command. A non-nil
// compile result means compilation failed and should be reported as-is.
//
// The code goes into its own directory (codeDir) so concurrent executions in
// one sandbox cannot overwrite each other's source; the working directory and
// relative Files still resolve against workDir.
// NOTE: compiled languages still share the shared runtime table's fixed
// "-o /tmp/main" output, so concurrent C/C++/Rust runs can collide.
func (i *Instance) prepare(ctx context.Context, code string, opts *executor.ExecutionOptions) (cmd []string, compile *executor.ExecutionResult, codeDir string, err error) {
	runtimeInfo, ok := langdetect.GetRuntimeInfo(opts.Language)
	if !ok {
		return nil, nil, "", fmt.Errorf("unsupported language: %s", opts.Language)
	}

	workDir := i.execWorkDir(opts)
	if err := i.sb.FS().Mkdir(ctx, workDir); err != nil {
		return nil, nil, "", fmt.Errorf("create workdir %s: %w", workDir, err)
	}

	dirName, err := randomName("sindoq-exec-")
	if err != nil {
		return nil, nil, "", err
	}
	codeDir = path.Join("/tmp", dirName)
	codePath := path.Join(codeDir, "main"+runtimeInfo.FileExt)
	if err := i.writeFile(ctx, codePath, []byte(code)); err != nil {
		return nil, nil, "", fmt.Errorf("write code file: %w", err)
	}

	for name, content := range opts.Files {
		filePath := name
		if !path.IsAbs(filePath) {
			filePath = path.Join(workDir, filePath)
		}
		if err := i.writeFile(ctx, filePath, content); err != nil {
			return nil, nil, codeDir, fmt.Errorf("write file %s: %w", name, err)
		}
	}

	if runtimeInfo.CompileCmd == nil {
		return append(append([]string{}, runtimeInfo.RunCommand...), codePath), nil, codeDir, nil
	}

	compileCmd := append(append([]string{}, runtimeInfo.CompileCmd...), codePath)
	result, err := i.run(ctx, compileCmd, workDir, &executor.ExecutionOptions{Env: opts.Env, Timeout: opts.Timeout})
	if err != nil {
		return nil, nil, codeDir, fmt.Errorf("compile: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, result, codeDir, nil
	}
	return runtimeInfo.RunCommand, nil, codeDir, nil
}

// cleanup removes an execution's code directory unless artifacts are kept.
func (i *Instance) cleanup(ctx context.Context, codeDir string, opts *executor.ExecutionOptions) {
	if codeDir == "" || opts.KeepArtifacts {
		return
	}
	_ = i.sb.FS().RemoveDir(context.WithoutCancel(ctx), codeDir)
}

func (i *Instance) execWorkDir(opts *executor.ExecutionOptions) string {
	switch {
	case opts.WorkDir != "":
		return opts.WorkDir
	case i.workDir != "":
		return i.workDir
	default:
		return "/tmp"
	}
}

func execOptions(workDir string, opts *executor.ExecutionOptions) []msb.ExecOption {
	execOpts := []msb.ExecOption{msb.WithExecCwd(workDir)}
	if len(opts.Env) > 0 {
		execOpts = append(execOpts, msb.WithExecEnv(opts.Env))
	}
	return execOpts
}

// run executes argv and collects its output. Exec has no stdin, so input is
// fed through a streaming session when present.
func (i *Instance) run(ctx context.Context, argv []string, workDir string, opts *executor.ExecutionOptions) (*executor.ExecutionResult, error) {
	execOpts := execOptions(workDir, opts)
	if opts.Timeout > 0 {
		execOpts = append(execOpts, msb.WithExecTimeout(opts.Timeout))
	}

	var out *msb.ExecOutput
	var err error
	if opts.Stdin == "" {
		out, err = i.sb.Exec(ctx, argv[0], argv[1:], execOpts...)
	} else {
		out, err = i.runWithStdin(ctx, argv, opts.Stdin, execOpts)
	}
	if err != nil {
		return nil, err
	}

	return &executor.ExecutionResult{
		ExitCode: out.ExitCode(),
		Stdout:   out.Stdout(),
		Stderr:   out.Stderr(),
	}, nil
}

func (i *Instance) runWithStdin(ctx context.Context, argv []string, stdin string, execOpts []msb.ExecOption) (*msb.ExecOutput, error) {
	h, err := i.sb.ExecStream(ctx, argv[0], argv[1:], append(execOpts, msb.WithExecStdinPipe())...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = h.Close() }()

	sink := h.TakeStdin()
	if sink == nil {
		return nil, fmt.Errorf("stdin pipe not available")
	}
	// Write concurrently so a process that exits without reading cannot block us.
	go func() {
		_, _ = io.Copy(sink, strings.NewReader(stdin))
		_ = sink.Close()
	}()

	return h.Collect(ctx)
}

// Execute runs code in the sandbox.
func (i *Instance) Execute(ctx context.Context, code string, opts *executor.ExecutionOptions) (*executor.ExecutionResult, error) {
	if err := i.checkRunning(); err != nil {
		return nil, err
	}
	if opts == nil {
		opts = executor.DefaultExecutionOptions()
	}

	start := time.Now()

	cmd, compileResult, codeDir, err := i.prepare(ctx, code, opts)
	defer i.cleanup(ctx, codeDir, opts)
	if err != nil {
		return nil, err
	}

	result := compileResult
	if result == nil {
		result, err = i.run(ctx, cmd, i.execWorkDir(opts), opts)
		if err != nil {
			return nil, err
		}
	}

	result.Duration = time.Since(start)
	result.Language = opts.Language
	return result, nil
}

// ExecuteStream runs code with streaming output.
func (i *Instance) ExecuteStream(ctx context.Context, code string, opts *executor.ExecutionOptions, handler executor.StreamHandler) error {
	if err := i.checkRunning(); err != nil {
		return err
	}
	if opts == nil {
		opts = executor.DefaultExecutionOptions()
	}

	cmd, compileResult, codeDir, err := i.prepare(ctx, code, opts)
	defer i.cleanup(ctx, codeDir, opts)
	if err != nil {
		return err
	}
	if compileResult != nil {
		if err := handler(&executor.StreamEvent{Type: executor.StreamStderr, Data: compileResult.Stderr, Timestamp: time.Now()}); err != nil {
			return err
		}
		return handler(&executor.StreamEvent{Type: executor.StreamComplete, ExitCode: compileResult.ExitCode, Timestamp: time.Now()})
	}

	execOpts := execOptions(i.execWorkDir(opts), opts)
	if opts.Stdin != "" {
		execOpts = append(execOpts, msb.WithExecStdinPipe())
	}

	// The timeout is enforced here rather than with WithExecTimeout so that, like
	// Execute, an expired timeout is reported as an error instead of exit code -1.
	recvCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		recvCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	h, err := i.sb.ExecStream(ctx, cmd[0], cmd[1:], execOpts...)
	if err != nil {
		return fmt.Errorf("start exec: %w", err)
	}
	defer func() { _ = h.Close() }()

	if opts.Stdin != "" {
		if sink := h.TakeStdin(); sink != nil {
			go func() {
				_, _ = io.Copy(sink, strings.NewReader(opts.Stdin))
				_ = sink.Close()
			}()
		}
	}

	exitCode := 0
	for {
		ev, err := h.Recv(recvCtx)
		if err != nil {
			// Don't leave the guest process running once we stop reading.
			_ = h.Kill(context.WithoutCancel(ctx))
			if ctx.Err() == nil && recvCtx.Err() != nil {
				return fmt.Errorf("exec timed out after %v", opts.Timeout)
			}
			return fmt.Errorf("receive exec event: %w", err)
		}

		// StreamStart is not forwarded: sindoq.ExecuteStream emits it for every provider.
		var streamEvent *executor.StreamEvent
		switch ev.Kind {
		case msb.ExecEventStdout:
			streamEvent = &executor.StreamEvent{Type: executor.StreamStdout, Data: string(ev.Data)}
		case msb.ExecEventStderr:
			streamEvent = &executor.StreamEvent{Type: executor.StreamStderr, Data: string(ev.Data)}
		case msb.ExecEventExited:
			exitCode = ev.ExitCode
		case msb.ExecEventFailed:
			return fmt.Errorf("exec failed: %s", failureMessage(ev.Failure))
		case msb.ExecEventStdinError:
			// The process stopped reading stdin (e.g. it never reads it); keep its output.
		case msb.ExecEventDone:
			return handler(&executor.StreamEvent{Type: executor.StreamComplete, ExitCode: exitCode, Timestamp: time.Now()})
		}

		if streamEvent == nil {
			continue
		}
		streamEvent.Timestamp = time.Now()
		if err := handler(streamEvent); err != nil {
			// The caller asked to stop; don't leave the guest process running.
			_ = h.Kill(context.WithoutCancel(ctx))
			return err
		}
	}
}

func failureMessage(f *msb.ExecFailure) string {
	if f == nil {
		return "unknown error"
	}
	return f.Message
}

// RunCommand executes a command in the sandbox.
func (i *Instance) RunCommand(ctx context.Context, cmd string, args []string) (*executor.CommandResult, error) {
	if err := i.checkRunning(); err != nil {
		return nil, err
	}

	start := time.Now()
	workDir := i.workDir
	if workDir == "" {
		workDir = "/"
	}
	out, err := i.sb.Exec(ctx, cmd, args, msb.WithExecCwd(workDir))
	if err != nil {
		return nil, err
	}

	return &executor.CommandResult{
		ExitCode: out.ExitCode(),
		Stdout:   out.Stdout(),
		Stderr:   out.Stderr(),
		Duration: time.Since(start),
	}, nil
}

// writeFile writes data to path, creating parent directories (the SDK does not).
func (i *Instance) writeFile(ctx context.Context, filePath string, data []byte) error {
	if err := i.sb.FS().Mkdir(ctx, path.Dir(filePath)); err != nil {
		return err
	}
	return i.sb.FS().Write(ctx, filePath, data)
}

// FileSystem returns the file system handler.
func (i *Instance) FileSystem() fs.FileSystem {
	return &microsandboxFS{instance: i}
}

// Network returns nil: ports can only be published at sandbox creation.
func (i *Instance) Network() provider.Network {
	return nil
}

// Stop shuts the microVM down. The sandbox is ephemeral, so its state is
// removed by the runtime once stopped.
func (i *Instance) Stop(ctx context.Context) error {
	i.mu.Lock()
	if i.stopped {
		i.mu.Unlock()
		return nil
	}
	i.stopped = true
	i.mu.Unlock()

	stopErr := i.sb.Stop(ctx)
	closeErr := i.sb.Close()
	if stopErr != nil {
		return fmt.Errorf("stop sandbox: %w", stopErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close sandbox: %w", closeErr)
	}
	return nil
}

// Status returns the current status.
func (i *Instance) Status(ctx context.Context) (provider.InstanceStatus, error) {
	if err := i.checkRunning(); err != nil {
		return provider.StatusStopped, nil
	}

	handle, err := msb.GetSandbox(ctx, i.sb.Name())
	if err != nil {
		return provider.StatusError, err
	}
	return mapStatus(handle.Status()), nil
}

func mapStatus(s msb.SandboxStatus) provider.InstanceStatus {
	switch s {
	case msb.SandboxStatusCreated, msb.SandboxStatusStarting:
		return provider.StatusCreating
	case msb.SandboxStatusRunning, msb.SandboxStatusDraining:
		return provider.StatusRunning
	case msb.SandboxStatusPaused:
		return provider.StatusPaused
	case msb.SandboxStatusStopped:
		return provider.StatusStopped
	default:
		return provider.StatusError
	}
}

var (
	_ provider.Provider = (*Provider)(nil)
	_ provider.Instance = (*Instance)(nil)
)
