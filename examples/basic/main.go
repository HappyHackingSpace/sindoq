// Package main demonstrates basic usage of the sindoq SDK.
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

	// Create a sandbox using Docker provider (default)
	fmt.Println("Creating sandbox...")
	sb, err := sindoq.Create(ctx, sindoq.WithProvider("docker"))
	if err != nil {
		log.Fatalf("Failed to create sandbox: %v", err)
	}
	defer func() { _ = sb.Stop(ctx) }()

	fmt.Printf("Sandbox created: %s (Provider: %s)\n", sb.ID(), sb.Provider())

	// Execute Python code (language auto-detected)
	pythonCode := `
import json

data = {"message": "Hello from sindoq!", "numbers": [1, 2, 3]}
print(json.dumps(data, indent=2))
`

	fmt.Println("\nExecuting Python code...")
	result, err := sb.Execute(ctx, pythonCode)
	if err != nil {
		log.Fatalf("Execution failed: %v", err)
	}

	fmt.Printf("Exit code: %d\n", result.ExitCode)
	fmt.Printf("Duration: %v\n", result.Duration)
	fmt.Printf("Language: %s\n", result.Language)
	fmt.Println("Output:")
	fmt.Println(result.Stdout)

	// Execute JavaScript code
	jsCode := `
const greeting = "Hello from JavaScript!";
const numbers = Array.from({length: 5}, (_, i) => i * i);
console.log(greeting);
console.log("Squares:", numbers);
`

	fmt.Println("Executing JavaScript code...")
	result, err = sb.Execute(ctx, jsCode)
	if err != nil {
		log.Fatalf("Execution failed: %v", err)
	}

	fmt.Printf("Exit code: %d\n", result.ExitCode)
	fmt.Printf("Language: %s\n", result.Language)
	fmt.Println("Output:")
	fmt.Println(result.Stdout)
}
