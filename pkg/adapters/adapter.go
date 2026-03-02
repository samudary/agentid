package adapters

import (
	"context"
	"encoding/json"
	"net/http"
)

// UpstreamAuthType defines how adapters authenticate to upstream services.
type UpstreamAuthType string

const (
	AuthBearer UpstreamAuthType = "bearer_token"
	AuthBasic  UpstreamAuthType = "basic_auth"
	AuthHeader UpstreamAuthType = "header"
)

// UpstreamAuth holds resolved credentials for authenticating to an upstream
// service. Values are resolved from environment variables at server startup
// and passed to adapters at construction time.
type UpstreamAuth struct {
	Type        UpstreamAuthType
	Token       string // For bearer_token
	Username    string // For basic_auth
	Password    string // For basic_auth
	HeaderName  string // For header
	HeaderValue string // For header
}

// Apply sets the appropriate authentication headers on an HTTP request.
func (a *UpstreamAuth) Apply(req *http.Request) {
	switch a.Type {
	case AuthBearer:
		req.Header.Set("Authorization", "Bearer "+a.Token)
	case AuthBasic:
		req.SetBasicAuth(a.Username, a.Password)
	case AuthHeader:
		req.Header.Set(a.HeaderName, a.HeaderValue)
	}
}

// ToolDefinition describes a tool that agents can discover and invoke via MCP.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolResult contains the response from a tool invocation.
type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock is a piece of tool result content.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Adapter defines the interface that tool-specific adapters must implement.
type Adapter interface {
	// Name returns the tool's identifier (e.g., "github").
	Name() string

	// Tools returns the MCP tool definitions this adapter provides.
	Tools() []ToolDefinition

	// ScopeForTool returns the required scope for a given tool name.
	// Returns empty string if the tool is not found.
	ScopeForTool(toolName string) string

	// Invoke executes a tool call and returns the result.
	Invoke(ctx context.Context, toolName string, input json.RawMessage) (*ToolResult, error)
}
