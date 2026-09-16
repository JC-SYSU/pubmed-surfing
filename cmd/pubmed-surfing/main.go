package main

import (
	"context"
	"fmt"
	"os"

	"github.com/JC-SYSU/pubmed-surfing/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if err := mcpserver.New(nil).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintf(os.Stderr, "PubMed Surfing MCP server failed to start: %v\n", err)
		os.Exit(1)
	}
}
