// Package main demonstrates using different providers with sindoq.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/happyhackingspace/sindoq"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	code := `print("Hello from sindoq!")`

	// Try Docker provider (local)
	fmt.Println("=== Docker Provider ===")
	runWithProvider(ctx, "docker", code, nil)

	// Try Vercel provider (requires token)
	if token := os.Getenv("VERCEL_TOKEN"); token != "" {
		fmt.Println("\n=== Vercel Provider ===")
		runWithProvider(ctx, "vercel", code, sindoq.WithVercelConfig(sindoq.VercelConfig{
			Token: token,
		}))
	} else {
		fmt.Println("\n=== Vercel Provider ===")
		fmt.Println("Skipped (set VERCEL_TOKEN to test)")
	}

	// Try E2B provider (requires API key)
	if apiKey := os.Getenv("E2B_API_KEY"); apiKey != "" {
		fmt.Println("\n=== E2B Provider ===")
		runWithProvider(ctx, "e2b", code, sindoq.WithE2BConfig(sindoq.E2BConfig{
			APIKey: apiKey,
		}))
	} else {
		fmt.Println("\n=== E2B Provider ===")
		fmt.Println("Skipped (set E2B_API_KEY to test)")
	}

}

func runWithProvider(ctx context.Context, provider, code string, opts ...sindoq.Option) {
	allOpts := append([]sindoq.Option{sindoq.WithProvider(provider)}, opts...)

	sb, err := sindoq.Create(ctx, allOpts...)
	if err != nil {
		fmt.Printf("Error creating sandbox: %v\n", err)
		return
	}
	defer func() { _ = sb.Stop(ctx) }()

	fmt.Printf("Sandbox ID: %s\n", sb.ID())

	result, err := sb.Execute(ctx, code)
	if err != nil {
		fmt.Printf("Error executing: %v\n", err)
		return
	}

	fmt.Printf("Exit code: %d\n", result.ExitCode)
	fmt.Printf("Output: %s", result.Stdout)
	if result.Stderr != "" {
		fmt.Printf("Stderr: %s", result.Stderr)
	}
}
