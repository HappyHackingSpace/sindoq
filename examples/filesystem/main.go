// Package main demonstrates filesystem operations with sindoq.
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

	// Create a sandbox
	fmt.Println("Creating sandbox...")
	sb, err := sindoq.Create(ctx, sindoq.WithProvider("docker"))
	if err != nil {
		log.Fatalf("Failed to create sandbox: %v", err)
	}
	defer func() { _ = sb.Stop(ctx) }()

	fmt.Printf("Sandbox created: %s\n\n", sb.ID())

	// Get filesystem interface
	fs := sb.Files()
	if fs == nil {
		log.Fatal("Filesystem not supported by this provider")
	}

	// Write a file
	fmt.Println("Writing file...")
	content := []byte(`name,age,city
Alice,30,NYC
Bob,25,LA
Charlie,35,Chicago`)

	if err := fs.Write(ctx, "/workspace/data.csv", content); err != nil {
		log.Fatalf("Failed to write file: %v", err)
	}
	fmt.Println("File written: /workspace/data.csv")

	// Read the file back
	fmt.Println("\nReading file...")
	data, err := fs.Read(ctx, "/workspace/data.csv")
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}
	fmt.Println("File content:")
	fmt.Println(string(data))

	// Execute code that uses the file
	code := `
import csv

with open('/workspace/data.csv', 'r') as f:
    reader = csv.DictReader(f)
    for row in reader:
        print(f"{row['name']} is {row['age']} years old from {row['city']}")
`

	fmt.Println("\nExecuting code that reads the file...")
	result, err := sb.Execute(ctx, code)
	if err != nil {
		log.Fatalf("Execution failed: %v", err)
	}

	fmt.Println("Output:")
	fmt.Println(result.Stdout)

	// Check if file exists
	exists, err := fs.Exists(ctx, "/workspace/data.csv")
	if err != nil {
		log.Fatalf("Failed to check file: %v", err)
	}
	fmt.Printf("File exists: %v\n", exists)

	// Delete the file
	fmt.Println("\nDeleting file...")
	if err := fs.Delete(ctx, "/workspace/data.csv"); err != nil {
		log.Fatalf("Failed to delete file: %v", err)
	}
	fmt.Println("File deleted")

	// Verify deletion
	exists, _ = fs.Exists(ctx, "/workspace/data.csv")
	fmt.Printf("File exists after delete: %v\n", exists)
}
