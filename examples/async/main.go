// Package main demonstrates async execution with sindoq.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/happyhackingspace/sindoq"
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

	// Execute code asynchronously
	code := `
import time
print("Starting long computation...")
time.sleep(3)  # Simulate work
result = sum(range(1000000))
print(f"Result: {result}")
`

	fmt.Println("Starting async execution...")
	resultChan, err := sb.ExecuteAsync(ctx, code)
	if err != nil {
		log.Fatalf("Failed to start async execution: %v", err)
	}

	// Do other work while execution is running
	fmt.Println("Execution started! Doing other work...")
	for i := 0; i < 5; i++ {
		fmt.Printf("  Other work tick %d...\n", i+1)
		time.Sleep(500 * time.Millisecond)
	}

	// Wait for result
	fmt.Println("\nWaiting for execution result...")
	result := <-resultChan

	if result.Error != nil {
		log.Fatalf("Execution failed: %v", result.Error)
	}

	fmt.Printf("\nExecution completed!\n")
	fmt.Printf("Exit code: %d\n", result.ExitCode)
	fmt.Printf("Duration: %v\n", result.Duration)
	fmt.Println("Output:")
	fmt.Println(result.Stdout)
}
