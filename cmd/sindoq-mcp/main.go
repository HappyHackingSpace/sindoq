// Command sindoq-mcp exposes sindoq code execution to AI agents over the
// Model Context Protocol (stdio transport).
package main

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	server := newServer(runExecution)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("sindoq-mcp: %v", err)
	}
}
