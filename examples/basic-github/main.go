// Package main demonstrates using a standard MCP client with AgentID.
//
// This example uses the mcp-go library (github.com/mark3labs/mcp-go),
// the community-standard Go MCP implementation. AgentID speaks standard
// MCP over JSON-RPC 2.0
//
// Prerequisites:
//
//	agentid serve --config configs/example-gateway.yaml
//	export TASK_JWT=$(agentid task create \
//	  --authorizer "human:you@company.com" \
//	  --bundle code-reader \
//	  --ttl 30m \
//	  --config configs/example-gateway.yaml 2>/dev/null | grep JWT | awk '{print $NF}')
//
// Run:
//
//	go run ./examples/basic-github
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

func main() {
	serverURL := envOrDefault("AGENTID_URL", "http://localhost:8080")
	jwt := os.Getenv("TASK_JWT")
	if jwt == "" {
		fmt.Fprintln(os.Stderr, "TASK_JWT environment variable is required")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Create one with:")
		fmt.Fprintln(os.Stderr, `  agentid task create \`)
		fmt.Fprintln(os.Stderr, `    --authorizer "human:you@company.com" \`)
		fmt.Fprintln(os.Stderr, `    --bundle code-reader \`)
		fmt.Fprintln(os.Stderr, `    --ttl 30m \`)
		fmt.Fprintln(os.Stderr, `    --config configs/example-gateway.yaml`)
		os.Exit(1)
	}

	// Create a standard MCP client over Streamable HTTP.
	// The only AgentID-specific part is passing the task JWT as a Bearer token.
	mcpClient, err := client.NewStreamableHttpClient(
		serverURL+"/mcp",
		transport.WithHTTPHeaders(map[string]string{
			"Authorization": "Bearer " + jwt,
		}),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create client: %v\n", err)
		os.Exit(1)
	}
	defer mcpClient.Close()

	ctx := context.Background()

	// MCP handshake — required by the protocol before any tool calls.
	if err := mcpClient.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "start client: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("=== Available Tools ===")
	toolsResult, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "list tools: %v\n", err)
		os.Exit(1)
	}
	for _, t := range toolsResult.Tools {
		fmt.Printf("  %s -- %s\n", t.Name, t.Description)
	}
	fmt.Println()

	// Call a tool
	owner := envOrDefault("GITHUB_OWNER", "samudary")
	repo := envOrDefault("GITHUB_REPO", "agentid")

	fmt.Printf("=== Getting README.md from %s/%s ===\n", owner, repo)
	result, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "github_get_file",
			Arguments: map[string]any{
				"owner": owner,
				"repo":  repo,
				"path":  "README.md",
			},
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "call tool: %v\n", err)
		os.Exit(1)
	}

	if result.IsError {
		fmt.Println("  Error:", result.Content[0].(mcp.TextContent).Text)
	} else if len(result.Content) > 0 {
		text := result.Content[0].(mcp.TextContent).Text
		if len(text) > 500 {
			text = text[:500] + "..."
		}
		fmt.Println(text)
	}

	fmt.Println()
	fmt.Println("=== Done ===")
	fmt.Println("Check the audit trail with:")
	fmt.Println("  agentid audit tail --config configs/example-gateway.yaml")
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
