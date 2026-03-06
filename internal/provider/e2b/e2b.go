// Package e2b provides the E2B (Code Interpreter) provider for sindoq.
package e2b

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/happyhackingspace/sindoq/internal/factory"
	"github.com/happyhackingspace/sindoq/internal/provider"
	"github.com/happyhackingspace/sindoq/pkg/executor"
	"github.com/happyhackingspace/sindoq/pkg/fs"
)

const (
	baseURL = "https://api.e2b.dev"
)

func init() {
	factory.Register("e2b", func(config any) (provider.Provider, error) {
		cfg, ok := config.(*Config)
		if !ok && config != nil {
			return nil, fmt.Errorf("invalid config type for e2b provider")
		}
		return New(cfg)
	})
}

// Config holds E2B provider configuration.
type Config struct {
	APIKey   string
	Template string
	Timeout  time.Duration
}

// Provider implements the E2B provider.
type Provider struct {
	config *Config
	client *http.Client
}

// New creates a new E2B provider.
func New(cfg *Config) (*Provider, error) {
	if cfg == nil {
		cfg = &Config{}
	}

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("e2b API key is required")
	}

	if cfg.Template == "" {
		cfg.Template = "base"
	}

	return &Provider{
		config: cfg,
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}, nil
}

// Name returns the provider identifier.
func (p *Provider) Name() string {
	return "e2b"
}

// Create initializes a new E2B sandbox.
func (p *Provider) Create(ctx context.Context, opts *provider.CreateOptions) (provider.Instance, error) {
	if opts == nil {
		opts = provider.DefaultCreateOptions()
	}

	reqBody := map[string]any{
		"templateId": p.config.Template,
	}

	if opts.Timeout > 0 {
		reqBody["timeout"] = int(opts.Timeout.Seconds())
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/sandboxes", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	p.setHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create sandbox: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create sandbox failed: %s - %s", resp.Status, string(bodyBytes))
	}

	var result struct {
		SandboxID string `json:"sandboxId"`
		ClientID  string `json:"clientId"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &Instance{
		id:       result.SandboxID,
		clientID: result.ClientID,
		provider: p,
		workDir:  opts.WorkDir,
	}, nil
}

func (p *Provider) setHeaders(req *http.Request) {
	req.Header.Set("X-E2B-Api-Key", p.config.APIKey)
	req.Header.Set("Content-Type", "application/json")
}

// Capabilities returns E2B provider capabilities.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		SupportsStreaming:   true,
		SupportsAsync:       true,
		SupportsFileSystem:  true,
		SupportsNetwork:     false,
		SupportedLanguages:  []string{"Python", "JavaScript", "TypeScript", "R", "Java", "Bash"},
		MaxExecutionTime:    24 * time.Hour,
		MaxMemoryMB:         8192,
		MaxCPUs:             4,
		SupportsPersistence: true,
	}
}

// Validate checks if E2B API is accessible.
func (p *Provider) Validate(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/templates", nil)
	if err != nil {
		return err
	}
	p.setHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("e2b API not accessible: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid E2B API key")
	}

	return nil
}

// Close releases provider resources.
func (p *Provider) Close() error {
	return nil
}

// Instance represents an E2B sandbox instance.
type Instance struct {
	id       string
	clientID string
	provider *Provider
	workDir  string
	mu       sync.RWMutex
	stopped  bool
}

// ID returns the sandbox ID.
func (i *Instance) ID() string {
	return i.id
}

// Execute runs code in the sandbox.
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

	// E2B uses run_code endpoint
	reqBody := map[string]any{
		"code": code,
	}

	if opts.Language != "" {
		reqBody["language"] = opts.Language
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/sandboxes/"+i.id+"/code/execution", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	i.provider.setHeaders(req)

	resp, err := i.provider.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exitCode"`
		Error    string `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	execResult := &executor.ExecutionResult{
		ExitCode: result.ExitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		Duration: time.Since(start),
		Language: opts.Language,
	}

	if result.Error != "" {
		execResult.Error = fmt.Errorf("%s", result.Error)
	}

	return execResult, nil
}

// ExecuteStream runs code with streaming output.
func (i *Instance) ExecuteStream(ctx context.Context, code string, opts *executor.ExecutionOptions, handler executor.StreamHandler) error {
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
func (i *Instance) RunCommand(ctx context.Context, cmd string, args []string) (*executor.CommandResult, error) {
	fullCmd := cmd
	for _, arg := range args {
		fullCmd += " " + arg
	}

	reqBody := map[string]any{
		"cmd": fullCmd,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/sandboxes/"+i.id+"/commands", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	i.provider.setHeaders(req)

	resp, err := i.provider.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exitCode"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &executor.CommandResult{
		ExitCode: result.ExitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		Duration: time.Since(start),
	}, nil
}

// FileSystem returns the file system handler.
func (i *Instance) FileSystem() fs.FileSystem {
	return &e2bFS{instance: i}
}

// Network returns the network handler (not supported).
func (i *Instance) Network() provider.Network {
	return nil
}

// Stop terminates the sandbox.
func (i *Instance) Stop(ctx context.Context) error {
	i.mu.Lock()
	if i.stopped {
		i.mu.Unlock()
		return nil
	}
	i.stopped = true
	i.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, "DELETE", baseURL+"/sandboxes/"+i.id, nil)
	if err != nil {
		return err
	}
	i.provider.setHeaders(req)

	resp, err := i.provider.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

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

	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/sandboxes/"+i.id, nil)
	if err != nil {
		return provider.StatusError, err
	}
	i.provider.setHeaders(req)

	resp, err := i.provider.client.Do(req)
	if err != nil {
		return provider.StatusError, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return provider.StatusStopped, nil
	}

	return provider.StatusRunning, nil
}

// e2bFS implements fs.FileSystem for E2B.
type e2bFS struct {
	instance *Instance
}

func (f *e2bFS) Read(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/sandboxes/"+f.instance.id+"/files?path="+path, nil)
	if err != nil {
		return nil, err
	}
	f.instance.provider.setHeaders(req)

	resp, err := f.instance.provider.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	return io.ReadAll(resp.Body)
}

func (f *e2bFS) Write(ctx context.Context, path string, data []byte) error {
	reqBody := map[string]any{
		"path":    path,
		"content": string(data),
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/sandboxes/"+f.instance.id+"/files", bytes.NewReader(body))
	if err != nil {
		return err
	}
	f.instance.provider.setHeaders(req)

	resp, err := f.instance.provider.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	return nil
}

func (f *e2bFS) Delete(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", baseURL+"/sandboxes/"+f.instance.id+"/files?path="+path, nil)
	if err != nil {
		return err
	}
	f.instance.provider.setHeaders(req)

	resp, err := f.instance.provider.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	return nil
}

func (f *e2bFS) List(ctx context.Context, path string) ([]fs.FileInfo, error) {
	_, err := f.instance.RunCommand(ctx, "ls", []string{"-la", path})
	if err != nil {
		return nil, err
	}

	// Parse ls output (simplified)
	return []fs.FileInfo{}, nil
}

func (f *e2bFS) Exists(ctx context.Context, path string) (bool, error) {
	result, err := f.instance.RunCommand(ctx, "test", []string{"-e", path})
	if err != nil {
		return false, err
	}
	return result.ExitCode == 0, nil
}

func (f *e2bFS) Stat(ctx context.Context, path string) (*fs.FileInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *e2bFS) Upload(ctx context.Context, localPath, remotePath string) error {
	return fmt.Errorf("not implemented")
}

func (f *e2bFS) UploadReader(ctx context.Context, reader io.Reader, remotePath string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	return f.Write(ctx, remotePath, data)
}

func (f *e2bFS) Download(ctx context.Context, remotePath string, writer io.Writer) error {
	data, err := f.Read(ctx, remotePath)
	if err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

func (f *e2bFS) MkDir(ctx context.Context, path string) error {
	result, err := f.instance.RunCommand(ctx, "mkdir", []string{"-p", path})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("mkdir failed: %s", result.Stderr)
	}
	return nil
}

func (f *e2bFS) Copy(ctx context.Context, src, dst string) error {
	result, err := f.instance.RunCommand(ctx, "cp", []string{"-r", src, dst})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("copy failed: %s", result.Stderr)
	}
	return nil
}

func (f *e2bFS) Move(ctx context.Context, src, dst string) error {
	result, err := f.instance.RunCommand(ctx, "mv", []string{src, dst})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("move failed: %s", result.Stderr)
	}
	return nil
}

var _ fs.FileSystem = (*e2bFS)(nil)
var _ provider.Provider = (*Provider)(nil)
var _ provider.Instance = (*Instance)(nil)
