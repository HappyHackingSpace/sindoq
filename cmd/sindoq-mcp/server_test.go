package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/happyhackingspace/sindoq"
	"github.com/happyhackingspace/sindoq/pkg/executor"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestBuildOptionsFullMapping(t *testing.T) {
	createOpts, execOpts := buildOptions(executeCodeArgs{
		Code:           "print(1)",
		Language:       "Python",
		Provider:       "wasmer",
		TimeoutSeconds: 90,
	})

	cfg := sindoq.DefaultConfig()
	for _, opt := range createOpts {
		opt(cfg)
	}
	if cfg.Provider != "wasmer" {
		t.Errorf("Provider = %q, want %q", cfg.Provider, "wasmer")
	}
	if cfg.Runtime != "Python" {
		t.Errorf("Runtime = %q, want %q", cfg.Runtime, "Python")
	}
	if cfg.DefaultTimeout != 90*time.Second {
		t.Errorf("DefaultTimeout = %v, want %v", cfg.DefaultTimeout, 90*time.Second)
	}

	execCfg := sindoq.DefaultExecuteConfig()
	for _, opt := range execOpts {
		opt(execCfg)
	}
	if execCfg.Language != "Python" {
		t.Errorf("Language = %q, want %q", execCfg.Language, "Python")
	}
	if execCfg.Timeout != 90*time.Second {
		t.Errorf("Timeout = %v, want %v", execCfg.Timeout, 90*time.Second)
	}
}

func TestBuildOptionsDefaults(t *testing.T) {
	createOpts, execOpts := buildOptions(executeCodeArgs{Code: "print(1)"})
	if len(createOpts) != 0 {
		cfg := sindoq.DefaultConfig()
		for _, opt := range createOpts {
			opt(cfg)
		}
		t.Errorf("createOpts = %d options, want none (config after applying: %+v)", len(createOpts), cfg)
	}
	if len(execOpts) != 0 {
		t.Errorf("execOpts = %d options, want none", len(execOpts))
	}

	execCfg := sindoq.DefaultExecuteConfig()
	for _, opt := range execOpts {
		opt(execCfg)
	}
	if execCfg.Language != "" {
		t.Errorf("Language = %q, want empty (auto-detect)", execCfg.Language)
	}
	if execCfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want default %v", execCfg.Timeout, 30*time.Second)
	}
}

func TestToResult(t *testing.T) {
	got := toResult(&executor.ExecutionResult{
		ExitCode: 3,
		Stdout:   "out",
		Stderr:   "err",
		Duration: 1500 * time.Millisecond,
		Language: "Python",
		Error:    errors.New("boom"),
	})
	want := executeCodeResult{
		ExitCode:   3,
		Stdout:     "out",
		Stderr:     "err",
		DurationMS: 1500,
		Language:   "Python",
		Success:    false,
		Error:      "boom",
	}
	if got != want {
		t.Errorf("toResult = %+v, want %+v", got, want)
	}

	ok := toResult(&executor.ExecutionResult{ExitCode: 0, Stdout: "hi"})
	if !ok.Success || ok.Error != "" {
		t.Errorf("toResult(exit 0) = %+v, want success with no error", ok)
	}
}

// connect wires a client to a newServer(exec) instance over in-memory
// transports and returns the client session.
func connect(t *testing.T, exec executeFunc) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server := newServer(exec)
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return clientSession
}

func TestToolRegistration(t *testing.T) {
	session := connect(t, nil)

	res, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	tools := make(map[string]*mcp.Tool, len(res.Tools))
	for _, tool := range res.Tools {
		tools[tool.Name] = tool
	}
	for _, name := range []string{"execute_code", "list_providers", "list_languages"} {
		if _, ok := tools[name]; !ok {
			t.Errorf("tool %q not registered", name)
		}
	}
	if len(res.Tools) != 3 {
		t.Errorf("registered %d tools, want 3", len(res.Tools))
	}

	exec, ok := tools["execute_code"]
	if !ok {
		t.Fatal("execute_code missing")
	}
	schemaJSON, err := json.Marshal(exec.InputSchema)
	if err != nil {
		t.Fatalf("marshal input schema: %v", err)
	}
	var schema struct {
		Type       string         `json:"type"`
		Properties map[string]any `json:"properties"`
		Required   []string       `json:"required"`
	}
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		t.Fatalf("unmarshal input schema: %v", err)
	}
	if schema.Type != "object" {
		t.Errorf("input schema type = %q, want object", schema.Type)
	}
	for _, prop := range []string{"code", "language", "provider", "timeout_seconds"} {
		if _, ok := schema.Properties[prop]; !ok {
			t.Errorf("input schema missing property %q", prop)
		}
	}
	requiresCode := false
	for _, r := range schema.Required {
		if r == "code" {
			requiresCode = true
		}
	}
	if !requiresCode {
		t.Errorf("input schema required = %v, want it to include code", schema.Required)
	}
}

func decodeStructured(t *testing.T, res *mcp.CallToolResult, out any) {
	t.Helper()
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
}

func TestExecuteCodeCall(t *testing.T) {
	var gotCode string
	var gotCfg *sindoq.Config
	var gotExecCfg *sindoq.ExecuteConfig

	fake := func(ctx context.Context, code string, createOpts []sindoq.Option, execOpts []sindoq.ExecuteOption) (*executor.ExecutionResult, error) {
		gotCode = code
		gotCfg = sindoq.DefaultConfig()
		for _, opt := range createOpts {
			opt(gotCfg)
		}
		gotExecCfg = sindoq.DefaultExecuteConfig()
		for _, opt := range execOpts {
			opt(gotExecCfg)
		}
		return &executor.ExecutionResult{
			ExitCode: 0,
			Stdout:   "hello\n",
			Duration: 2 * time.Second,
			Language: "Python",
		}, nil
	}

	session := connect(t, fake)
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "execute_code",
		Arguments: map[string]any{
			"code":            "print('hello')",
			"language":        "Python",
			"provider":        "docker",
			"timeout_seconds": 5,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool returned IsError: %v", res.Content)
	}

	if gotCode != "print('hello')" {
		t.Errorf("executed code = %q", gotCode)
	}
	if gotCfg.Provider != "docker" || gotCfg.Runtime != "Python" || gotCfg.DefaultTimeout != 5*time.Second {
		t.Errorf("create config = provider %q runtime %q timeout %v", gotCfg.Provider, gotCfg.Runtime, gotCfg.DefaultTimeout)
	}
	if gotExecCfg.Language != "Python" || gotExecCfg.Timeout != 5*time.Second {
		t.Errorf("execute config = language %q timeout %v", gotExecCfg.Language, gotExecCfg.Timeout)
	}

	var out executeCodeResult
	decodeStructured(t, res, &out)
	want := executeCodeResult{
		ExitCode:   0,
		Stdout:     "hello\n",
		DurationMS: 2000,
		Language:   "Python",
		Success:    true,
	}
	if out != want {
		t.Errorf("structured result = %+v, want %+v", out, want)
	}
}

func TestExecuteCodeEmptyCode(t *testing.T) {
	called := false
	fake := func(ctx context.Context, code string, createOpts []sindoq.Option, execOpts []sindoq.ExecuteOption) (*executor.ExecutionResult, error) {
		called = true
		return &executor.ExecutionResult{}, nil
	}

	session := connect(t, fake)
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "execute_code",
		Arguments: map[string]any{"code": ""},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Error("empty code should produce a tool error")
	}
	if called {
		t.Error("executor must not run for empty code")
	}
}

func TestExecuteCodeExecutorError(t *testing.T) {
	fake := func(ctx context.Context, code string, createOpts []sindoq.Option, execOpts []sindoq.ExecuteOption) (*executor.ExecutionResult, error) {
		return nil, fmt.Errorf("provider unavailable")
	}

	session := connect(t, fake)
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "execute_code",
		Arguments: map[string]any{"code": "print(1)"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Error("executor error should produce a tool error result")
	}
}

func TestListProviders(t *testing.T) {
	session := connect(t, nil)
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_providers"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_providers IsError: %v", res.Content)
	}

	var out listProvidersResult
	decodeStructured(t, res, &out)
	found := false
	for _, p := range out.Providers {
		if p == "docker" {
			found = true
		}
	}
	if !found {
		t.Errorf("providers %v do not include docker", out.Providers)
	}
}

func TestListLanguages(t *testing.T) {
	session := connect(t, nil)
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_languages"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_languages IsError: %v", res.Content)
	}

	var out listLanguagesResult
	decodeStructured(t, res, &out)
	want := map[string]bool{"Python": false, "JavaScript": false, "Go": false}
	for _, l := range out.Languages {
		if _, ok := want[l]; ok {
			want[l] = true
		}
	}
	for lang, seen := range want {
		if !seen {
			t.Errorf("languages do not include %s", lang)
		}
	}
}
