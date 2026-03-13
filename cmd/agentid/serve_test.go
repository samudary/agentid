package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/samudary/agentid/pkg/adapters"
	"github.com/samudary/agentid/pkg/config"
)

type stubAdapter struct {
	name string
}

func (s *stubAdapter) Name() string                     { return s.name }
func (s *stubAdapter) Tools() []adapters.ToolDefinition { return nil }
func (s *stubAdapter) ScopeForTool(string) string       { return "" }
func (s *stubAdapter) Invoke(context.Context, string, json.RawMessage) (*adapters.ToolResult, error) {
	return nil, nil
}

func TestBuildAdapterSupportsExplicitRestType(t *testing.T) {
	adapter, err := buildAdapter("launchdarkly", config.ToolConfig{
		Type:     "rest",
		Upstream: "https://example.com",
		Operations: []config.OperationConfig{
			{
				Name:   "ld_get_flag",
				Scope:  "launchdarkly:flags:read",
				Method: "GET",
				Path:   "/api/v2/flags/{project}/{flag}",
			},
		},
	})
	if err != nil {
		t.Fatalf("buildAdapter returned error: %v", err)
	}

	if adapter.Name() != "launchdarkly" {
		t.Fatalf("adapter name = %q, want %q", adapter.Name(), "launchdarkly")
	}
	if len(adapter.Tools()) != 1 {
		t.Fatalf("tool count = %d, want 1", len(adapter.Tools()))
	}
}

func TestBuildAdapterUsesConfiguredImplementationType(t *testing.T) {
	const adapterType = "serve-test-stub"

	adapters.Register(adapterType, func(baseURL string, auth adapters.UpstreamAuth) (adapters.Adapter, error) {
		return &stubAdapter{name: "custom-instance"}, nil
	})

	adapter, err := buildAdapter("github-enterprise", config.ToolConfig{
		Type:     adapterType,
		Upstream: "https://example.com",
	})
	if err != nil {
		t.Fatalf("buildAdapter returned error: %v", err)
	}

	if adapter.Name() != "custom-instance" {
		t.Fatalf("adapter name = %q, want %q", adapter.Name(), "custom-instance")
	}
}
