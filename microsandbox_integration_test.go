//go:build microsandbox && cgo && integration

package sindoq

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/happyhackingspace/sindoq/pkg/executor"
)

func TestMicrosandboxEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	sb, err := Create(ctx,
		WithMicrosandboxConfig(MicrosandboxConfig{DefaultImage: "python:3.12-slim"}),
		WithResources(ResourceConfig{MemoryMB: 512, CPUs: 1}),
	)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer func() { _ = sb.Stop(context.Background()) }()

	if sb.Provider() != "microsandbox" {
		t.Errorf("Provider() = %q", sb.Provider())
	}

	result, err := sb.Execute(ctx, "import sys\nprint(sys.version_info.major)")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Language != "Python" || strings.TrimSpace(result.Stdout) != "3" {
		t.Errorf("auto-detected execute: language=%q stdout=%q stderr=%q", result.Language, result.Stdout, result.Stderr)
	}

	results, err := sb.ExecuteAsync(ctx, `print("async")`, WithLanguage("Python"))
	if err != nil {
		t.Fatalf("ExecuteAsync() error = %v", err)
	}
	if r := <-results; r == nil || strings.TrimSpace(r.Stdout) != "async" {
		t.Errorf("ExecuteAsync() result = %+v", r)
	}

	counts := map[executor.StreamEventType]int{}
	err = sb.ExecuteStream(ctx, `print("streamed")`, func(e *executor.StreamEvent) error {
		counts[e.Type]++
		return nil
	}, WithLanguage("Python"))
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	if counts[executor.StreamStart] != 1 || counts[executor.StreamComplete] != 1 {
		t.Errorf("want exactly one start and one complete event, got %v", counts)
	}

	if err := sb.Files().Write(ctx, "/workspace/hello.txt", []byte("hi")); err != nil {
		t.Fatalf("Files().Write() error = %v", err)
	}
	cmd, err := sb.RunCommand(ctx, "cat", "/workspace/hello.txt")
	if err != nil || cmd.Stdout != "hi" {
		t.Errorf("RunCommand() = %+v, %v", cmd, err)
	}
}
