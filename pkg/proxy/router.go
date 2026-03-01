package proxy

import (
	"fmt"

	"github.com/samudary/agentid/pkg/adapters"
)

// Router maps tool names to their adapter and required scope.
type Router struct {
	adapters map[string]adapters.Adapter // adapter name -> adapter
	tools    map[string]adapters.Adapter // tool name -> adapter
}

// NewRouter creates a router from a set of adapters.
func NewRouter(adapterList []adapters.Adapter) *Router {
	r := &Router{
		adapters: make(map[string]adapters.Adapter),
		tools:    make(map[string]adapters.Adapter),
	}
	for _, a := range adapterList {
		r.adapters[a.Name()] = a
		for _, tool := range a.Tools() {
			r.tools[tool.Name] = a
		}
	}
	return r
}

// Resolve returns the adapter for a tool name and the required scope.
func (r *Router) Resolve(toolName string) (adapters.Adapter, string, error) {
	adapter, ok := r.tools[toolName]
	if !ok {
		return nil, "", fmt.Errorf("unknown tool: %q", toolName)
	}
	scope := adapter.ScopeForTool(toolName)
	return adapter, scope, nil
}

// AllTools returns all tool definitions across all adapters.
func (r *Router) AllTools() []adapters.ToolDefinition {
	var all []adapters.ToolDefinition
	for _, a := range r.adapters {
		all = append(all, a.Tools()...)
	}
	return all
}
