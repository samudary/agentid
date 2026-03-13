// Package rest implements a generic REST adapter that is driven entirely
// by YAML configuration. Each operation defined in the gateway config becomes
// an MCP tool — no Go code is needed to add simple single-call REST tools.
//
// This adapter is the "quick start" path for integrating new services. If
// an operation requires multi-step orchestration, custom error handling, or
// domain-specific input validation, use a purpose-built adapter instead.
//
// Path parameters use {param} syntax (e.g., "/repos/{owner}/{repo}/contents/{path}").
// For GET/DELETE requests, remaining non-path parameters become query string
// parameters. For POST/PUT/PATCH requests, they become the JSON request body.
package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/samudary/agentid/pkg/adapters"
	"github.com/samudary/agentid/pkg/adapters/httpclient"
	"github.com/samudary/agentid/pkg/config"
)

// operation holds a parsed operation config ready for execution.
type operation struct {
	config     config.OperationConfig
	pathParams []string // parameter names extracted from path template
	tool       adapters.ToolDefinition
}

// Adapter implements adapters.Adapter for generic REST operations defined
// in YAML configuration.
type Adapter struct {
	name       string
	baseURL    string
	client     *httpclient.Client
	operations map[string]*operation // tool name -> operation
	tools      []adapters.ToolDefinition
}

var _ adapters.Adapter = (*Adapter)(nil)

// New creates a generic REST adapter from config. The name parameter
// identifies this adapter instance (e.g., "launchdarkly", "pagerduty").
// Operations must have at minimum Name, Scope, Method, and Path defined.
func New(name, baseURL string, auth adapters.UpstreamAuth, ops []config.OperationConfig) (*Adapter, error) {
	a := &Adapter{
		name:       name,
		baseURL:    strings.TrimRight(baseURL, "/"),
		client:     httpclient.New(auth, nil),
		operations: make(map[string]*operation, len(ops)),
	}

	for _, opCfg := range ops {
		if opCfg.Name == "" {
			return nil, fmt.Errorf("operation missing name")
		}
		if opCfg.Method == "" {
			return nil, fmt.Errorf("operation %q missing method", opCfg.Name)
		}
		if opCfg.Path == "" {
			return nil, fmt.Errorf("operation %q missing path", opCfg.Name)
		}

		pathParams := extractPathParams(opCfg.Path)

		// Build input schema from config or generate a minimal one
		var inputSchema json.RawMessage
		if opCfg.InputSchema != nil {
			b, err := json.Marshal(opCfg.InputSchema)
			if err != nil {
				return nil, fmt.Errorf("operation %q: marshal input_schema: %w", opCfg.Name, err)
			}
			inputSchema = b
		} else {
			// Generate a minimal schema from path parameters
			inputSchema = generateSchemaFromPathParams(pathParams)
		}

		description := opCfg.Description
		if description == "" {
			description = fmt.Sprintf("%s %s", opCfg.Method, opCfg.Path)
		}

		tool := adapters.ToolDefinition{
			Name:        opCfg.Name,
			Description: description,
			InputSchema: inputSchema,
		}

		op := &operation{
			config:     opCfg,
			pathParams: pathParams,
			tool:       tool,
		}

		a.operations[opCfg.Name] = op
		a.tools = append(a.tools, tool)
	}

	return a, nil
}

func (a *Adapter) Name() string                     { return a.name }
func (a *Adapter) Tools() []adapters.ToolDefinition { return a.tools }

func (a *Adapter) ScopeForTool(toolName string) string {
	if op, ok := a.operations[toolName]; ok {
		return op.config.Scope
	}
	return ""
}

func (a *Adapter) Invoke(ctx context.Context, toolName string, input json.RawMessage) (*adapters.ToolResult, error) {
	op, ok := a.operations[toolName]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %q", toolName)
	}

	// Parse input arguments
	var args map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
	}
	if args == nil {
		args = make(map[string]any)
	}

	// Substitute path parameters
	path := op.config.Path
	consumed := make(map[string]bool)
	for _, param := range op.pathParams {
		val, ok := args[param]
		if !ok {
			return nil, fmt.Errorf("missing required path parameter: %q", param)
		}
		strVal := fmt.Sprintf("%v", val)
		if strVal == "" {
			return nil, fmt.Errorf("path parameter %q must not be empty", param)
		}
		path = strings.ReplaceAll(path, "{"+param+"}", url.PathEscape(strVal))
		consumed[param] = true
	}

	reqURL := a.baseURL + path
	method := strings.ToUpper(op.config.Method)

	// Remaining args go to query string (GET/DELETE/HEAD) or body (POST/PUT/PATCH)
	remaining := make(map[string]any)
	for k, v := range args {
		if !consumed[k] {
			remaining[k] = v
		}
	}

	var body any
	switch method {
	case http.MethodGet, http.MethodDelete, http.MethodHead:
		if len(remaining) > 0 {
			u, err := url.Parse(reqURL)
			if err != nil {
				return nil, fmt.Errorf("parse URL: %w", err)
			}
			q := u.Query()
			for k, v := range remaining {
				q.Set(k, fmt.Sprintf("%v", v))
			}
			u.RawQuery = q.Encode()
			reqURL = u.String()
		}
	default:
		if len(remaining) > 0 {
			body = remaining
		}
	}

	return a.client.Do(ctx, method, reqURL, body, "")
}

// extractPathParams returns the parameter names from a path template.
// For example, "/repos/{owner}/{repo}/contents/{path}" returns
// ["owner", "repo", "path"].
func extractPathParams(path string) []string {
	var params []string
	for {
		start := strings.Index(path, "{")
		if start == -1 {
			break
		}
		end := strings.Index(path[start:], "}")
		if end == -1 {
			break
		}
		params = append(params, path[start+1:start+end])
		path = path[start+end+1:]
	}
	return params
}

// generateSchemaFromPathParams creates a minimal JSON Schema with required
// string properties for each path parameter. Used when the config doesn't
// provide an explicit input_schema.
func generateSchemaFromPathParams(params []string) json.RawMessage {
	if len(params) == 0 {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	props := make(map[string]any, len(params))
	for _, p := range params {
		props[p] = map[string]string{"type": "string"}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": props,
		"required":   params,
	}
	b, _ := json.Marshal(schema)
	return b
}
