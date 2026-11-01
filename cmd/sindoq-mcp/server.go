package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/happyhackingspace/sindoq"
	"github.com/happyhackingspace/sindoq/pkg/executor"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const serverVersion = "0.1.0"

// executeCodeArgs is the input schema of the execute_code tool.
type executeCodeArgs struct {
	Code           string  `json:"code" jsonschema:"source code to execute"`
	Language       string  `json:"language,omitempty" jsonschema:"programming language of the code (see list_languages); auto-detected when omitted"`
	Provider       string  `json:"provider,omitempty" jsonschema:"sandbox provider to run on (see list_providers); defaults to docker"`
	TimeoutSeconds float64 `json:"timeout_seconds,omitempty" jsonschema:"execution timeout in seconds; defaults to 30"`
}

// executeCodeResult is the structured output of the execute_code tool.
type executeCodeResult struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMS int64  `json:"duration_ms"`
	Language   string `json:"language"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

type listProvidersResult struct {
	Providers []string `json:"providers"`
}

type listLanguagesResult struct {
	Languages []string `json:"languages"`
}

// executeFunc runs code in a fresh sandbox. main injects the sindoq-backed
// implementation; tests inject a fake.
type executeFunc func(ctx context.Context, code string, createOpts []sindoq.Option, execOpts []sindoq.ExecuteOption) (*executor.ExecutionResult, error)

// runExecution is the production executeFunc: one sandbox per call, stopped
// when the execution finishes (same shape as sindoq.Execute, plus per-call
// execute options).
func runExecution(ctx context.Context, code string, createOpts []sindoq.Option, execOpts []sindoq.ExecuteOption) (*executor.ExecutionResult, error) {
	sb, err := sindoq.Create(ctx, createOpts...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sb.Stop(context.Background()) }()
	return sb.Execute(ctx, code, execOpts...)
}

// buildOptions translates execute_code arguments into sindoq create and
// execute options. It mirrors the sindoq CLI: an explicit language selects
// both the sandbox runtime and the execution language.
func buildOptions(args executeCodeArgs) ([]sindoq.Option, []sindoq.ExecuteOption) {
	var createOpts []sindoq.Option
	var execOpts []sindoq.ExecuteOption

	if args.Provider != "" {
		createOpts = append(createOpts, sindoq.WithProvider(args.Provider))
	}
	if args.Language != "" {
		createOpts = append(createOpts, sindoq.WithRuntime(args.Language))
		execOpts = append(execOpts, sindoq.WithLanguage(args.Language))
	}
	if args.TimeoutSeconds > 0 {
		d := time.Duration(args.TimeoutSeconds * float64(time.Second))
		createOpts = append(createOpts, sindoq.WithTimeout(d))
		execOpts = append(execOpts, sindoq.WithExecutionTimeout(d))
	}

	return createOpts, execOpts
}

// toResult converts an ExecutionResult into the tool's structured output.
func toResult(r *executor.ExecutionResult) executeCodeResult {
	out := executeCodeResult{
		ExitCode:   r.ExitCode,
		Stdout:     r.Stdout,
		Stderr:     r.Stderr,
		DurationMS: r.Duration.Milliseconds(),
		Language:   r.Language,
		Success:    r.Success(),
	}
	if r.Error != nil {
		out.Error = r.Error.Error()
	}
	return out
}

// newServer builds the MCP server with the three sindoq tools, executing code
// through exec.
func newServer(exec executeFunc) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "sindoq",
		Title:   "sindoq code sandbox",
		Version: serverVersion,
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "execute_code",
		Description: "Execute code in an isolated sindoq sandbox and return exit code, stdout and stderr. The language is auto-detected unless specified.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args executeCodeArgs) (*mcp.CallToolResult, executeCodeResult, error) {
		if args.Code == "" {
			return nil, executeCodeResult{}, fmt.Errorf("code must not be empty")
		}
		createOpts, execOpts := buildOptions(args)
		result, err := exec(ctx, args.Code, createOpts, execOpts)
		if err != nil {
			return nil, executeCodeResult{}, err
		}
		return nil, toResult(result), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_providers",
		Description: "List the sandbox providers available on this host (e.g. docker, podman, wasmer).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, listProvidersResult, error) {
		providers := sindoq.ListProviders()
		sort.Strings(providers)
		return nil, listProvidersResult{Providers: providers}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_languages",
		Description: "List the programming languages sindoq can execute.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, listLanguagesResult, error) {
		languages := sindoq.SupportedLanguages()
		sort.Strings(languages)
		return nil, listLanguagesResult{Languages: languages}, nil
	})

	return server
}
