// Package vercel provides the Vercel Sandbox provider for sindoq.
package vercel

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
	"github.com/happyhackingspace/sindoq/pkg/langdetect"
)

const (
	baseURL = "https://api.vercel.com"
)

func init() {
	// Register the Vercel provider
	factory.Register("vercel", func(config any) (provider.Provider, error) {
		cfg, ok := config.(*Config)
		if !ok && config != nil {
			return nil, fmt.Errorf("invalid config type for vercel provider")
		}
		return New(cfg)
	})
}

// Config holds Vercel Sandbox provider configuration.
type Config struct {
	Token     string
	TeamID    string
	ProjectID string
	Runtime   string // "node22" or "python313"
}

// Provider implements the Vercel Sandbox provider.
type Provider struct {
	config *Config
	client *http.Client
}

// New creates a new Vercel Sandbox provider.
func New(cfg *Config) (*Provider, error) {
	if cfg == nil {
		cfg = &Config{}
	}

	if cfg.Token == "" {
		return nil, fmt.Errorf("vercel token is required")
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
	return "vercel"
}

// Create initializes a new Vercel sandbox.
func (p *Provider) Create(ctx context.Context, opts *provider.CreateOptions) (provider.Instance, error) {
	if opts == nil {
		opts = provider.DefaultCreateOptions()
	}

	// Determine runtime
	runtime := p.config.Runtime
	if runtime == "" {
		runtime = "python313" // Default to Python
	}

	// Create sandbox via API
	reqBody := map[string]any{
		"runtime": runtime,
	}

	if opts.Timeout > 0 {
		reqBody["timeout"] = int(opts.Timeout.Seconds())
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/v1/sandbox", bytes.NewReader(body))
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
		ID      string `json:"id"`
		Runtime string `json:"runtime"`
		URL     string `json:"url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &Instance{
		id:       result.ID,
		url:      result.URL,
		runtime:  result.Runtime,
		provider: p,
		workDir:  opts.WorkDir,
	}, nil
}

func (p *Provider) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+p.config.Token)
	req.Header.Set("Content-Type", "application/json")
	if p.config.TeamID != "" {
		req.Header.Set("x-vercel-team-id", p.config.TeamID)
	}
}

// Capabilities returns Vercel Sandbox provider capabilities.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		SupportsStreaming:  true,
		SupportsAsync:      true,
		SupportsFileSystem: true,
		SupportsNetwork:    true,
		SupportedLanguages: []string{"Python", "JavaScript", "TypeScript"},
		MaxExecutionTime:   5 * time.Hour,
		MaxMemoryMB:        4096,
		MaxCPUs:            4,
	}
}

// Validate checks if Vercel API is accessible.
func (p *Provider) Validate(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/user", nil)
	if err != nil {
		return err
	}
	p.setHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("vercel API not accessible: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("vercel API returned: %s", resp.Status)
	}

	return nil
}

// Close releases provider resources.
func (p *Provider) Close() error {
	return nil
}

// Instance represents a Vercel sandbox instance.
type Instance struct {
	id       string
	url      string
	runtime  string
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

	// Build command based on language
	var cmd string
	var args []string

	runtimeInfo, _ := langdetect.GetRuntimeInfo(opts.Language)
	if runtimeInfo != nil {
		cmd = runtimeInfo.Runtime
		// For Python, we can use -c to run code directly
		if opts.Language == "Python" {
			args = []string{"-c", code}
		} else {
			// For other languages, write to file first
			filename := "main" + runtimeInfo.FileExt
			if err := i.writeFile(ctx, "/workspace/"+filename, []byte(code)); err != nil {
				return nil, fmt.Errorf("write code file: %w", err)
			}
			args = []string{"/workspace/" + filename}
		}
	} else {
		// Default to shell execution
		cmd = "sh"
		args = []string{"-c", code}
	}

	// Execute via API
	reqBody := map[string]any{
		"cmd":  cmd,
		"args": args,
	}

	if opts.Stdin != "" {
		reqBody["stdin"] = opts.Stdin
	}
	if len(opts.Env) > 0 {
		reqBody["env"] = opts.Env
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/v1/sandbox/"+i.id+"/exec", bytes.NewReader(body))
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
		ExitCode int    `json:"exitCode"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &executor.ExecutionResult{
		ExitCode: result.ExitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		Duration: time.Since(start),
		Language: opts.Language,
	}, nil
}

// writeFile writes content to a file in the sandbox.
func (i *Instance) writeFile(ctx context.Context, path string, content []byte) error {
	reqBody := map[string]any{
		"path":    path,
		"content": string(content),
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/v1/sandbox/"+i.id+"/files", bytes.NewReader(body))
	if err != nil {
		return err
	}
	i.provider.setHeaders(req)

	resp, err := i.provider.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("write file failed: %s - %s", resp.Status, string(bodyBytes))
	}

	return nil
}

// ExecuteStream runs code with streaming output.
func (i *Instance) ExecuteStream(ctx context.Context, code string, opts *executor.ExecutionOptions, handler executor.StreamHandler) error {
	// Vercel supports streaming, but for simplicity we'll use non-streaming here
	// and emit events at the end
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
	reqBody := map[string]any{
		"cmd":  cmd,
		"args": args,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/v1/sandbox/"+i.id+"/exec", bytes.NewReader(body))
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
		ExitCode int    `json:"exitCode"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
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
	return &vercelFS{instance: i}
}

// Network returns the network handler.
func (i *Instance) Network() provider.Network {
	return &vercelNetwork{instance: i}
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

	req, err := http.NewRequestWithContext(ctx, "DELETE", baseURL+"/v1/sandbox/"+i.id, nil)
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

	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/sandbox/"+i.id, nil)
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

// Ensure Provider and Instance implement interfaces
var _ provider.Provider = (*Provider)(nil)
var _ provider.Instance = (*Instance)(nil)
