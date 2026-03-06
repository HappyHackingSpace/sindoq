// Package main demonstrates the event system with sindoq.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/happyhackingspace/sindoq"
	"github.com/happyhackingspace/sindoq/pkg/event"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create a sandbox
	fmt.Println("Creating sandbox...")
	sb, err := sindoq.Create(ctx, sindoq.WithProvider("docker"))
	if err != nil {
		log.Fatalf("Failed to create sandbox: %v", err)
	}
	defer func() { _ = sb.Stop(ctx) }()

	fmt.Printf("Sandbox created: %s\n\n", sb.ID())

	// Subscribe to output events
	unsubStdout := sb.Subscribe(event.EventOutputStdout, func(e *event.Event) {
		if data, ok := e.Data.(*event.OutputData); ok {
			fmt.Printf("[STDOUT] %s\n", data.Content)
		}
	})
	defer unsubStdout()

	// Subscribe to execution events
	unsubExec := sb.Subscribe(event.EventExecutionComplete, func(e *event.Event) {
		if data, ok := e.Data.(*event.ExecutionCompleteData); ok {
			fmt.Printf("[EVENT] Execution completed: exit=%d, duration=%v, lang=%s\n",
				data.ExitCode, data.Duration, data.Language)
		}
	})
	defer unsubExec()

	// Subscribe to error events
	unsubErr := sb.Subscribe(event.EventExecutionError, func(e *event.Event) {
		fmt.Printf("[ERROR] Execution error: %v\n", e.Error)
	})
	defer unsubErr()

	// Execute some code
	code := `
print("Hello from sindoq!")
print("Line 2")
print("Line 3")
`

	fmt.Println("Executing code with event subscriptions...")
	result, err := sb.Execute(ctx, code)
	if err != nil {
		log.Fatalf("Execution failed: %v", err)
	}

	fmt.Printf("\nDirect result: exit=%d\n", result.ExitCode)
}
