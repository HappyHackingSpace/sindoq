// Package main demonstrates streaming execution with sindoq.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/happyhackingspace/sindoq"
	"github.com/happyhackingspace/sindoq/pkg/executor"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create a sandbox using Docker provider
	fmt.Println("Creating sandbox...")
	sb, err := sindoq.Create(ctx, sindoq.WithProvider("docker"))
	if err != nil {
		log.Fatalf("Failed to create sandbox: %v", err)
	}
	defer func() { _ = sb.Stop(ctx) }()

	fmt.Printf("Sandbox created: %s\n\n", sb.ID())

	// Execute code with streaming output
	code := `
import time
import sys

print("Starting long-running task...")
for i in range(5):
    print(f"Step {i+1}/5: Processing...")
    sys.stdout.flush()
    time.sleep(1)

print("Task completed!")
print("This is stderr", file=sys.stderr)
`

	fmt.Println("Executing with streaming output:")
	fmt.Println("---")

	// Stream handler receives events as they occur
	err = sb.ExecuteStream(ctx, code, func(event *executor.StreamEvent) error {
		switch event.Type {
		case executor.StreamStdout:
			fmt.Print(event.Data)
		case executor.StreamStderr:
			fmt.Printf("[STDERR] %s", event.Data)
		case executor.StreamComplete:
			fmt.Printf("\n---\nExecution complete. Exit code: %d\n", event.ExitCode)
		case executor.StreamError:
			fmt.Printf("[ERROR] %v\n", event.Error)
			return event.Error
		}
		return nil
	})

	if err != nil {
		log.Fatalf("Stream execution failed: %v", err)
	}
}
