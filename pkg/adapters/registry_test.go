package adapters_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/samudary/agentid/pkg/adapters"
)

// stubAdapter is a minimal adapter for testing the registry.
type stubAdapter struct{ name string }

func (s *stubAdapter) Name() string                     { return s.name }
func (s *stubAdapter) Tools() []adapters.ToolDefinition { return nil }
func (s *stubAdapter) ScopeForTool(string) string       { return "" }
func (s *stubAdapter) Invoke(context.Context, string, json.RawMessage) (*adapters.ToolResult, error) {
	return nil, nil
}

func TestRegistryLookup(t *testing.T) {
	adapters.ResetRegistry()
	defer adapters.ResetRegistry()

	adapters.Register("test-adapter", func(baseURL string, auth adapters.UpstreamAuth) (adapters.Adapter, error) {
		return &stubAdapter{name: "test-adapter"}, nil
	})

	factory, err := adapters.Lookup("test-adapter")
	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}

	a, err := factory("http://example.com", adapters.UpstreamAuth{})
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}
	if a.Name() != "test-adapter" {
		t.Errorf("name = %q, want %q", a.Name(), "test-adapter")
	}
}

func TestRegistryLookupUnknown(t *testing.T) {
	adapters.ResetRegistry()
	defer adapters.ResetRegistry()

	_, err := adapters.Lookup("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown adapter")
	}
}

func TestRegistryDuplicatePanics(t *testing.T) {
	adapters.ResetRegistry()
	defer adapters.ResetRegistry()

	factory := func(baseURL string, auth adapters.UpstreamAuth) (adapters.Adapter, error) {
		return &stubAdapter{}, nil
	}
	adapters.Register("dup", factory)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	adapters.Register("dup", factory)
}

func TestRegisteredNames(t *testing.T) {
	adapters.ResetRegistry()
	defer adapters.ResetRegistry()

	factory := func(baseURL string, auth adapters.UpstreamAuth) (adapters.Adapter, error) {
		return &stubAdapter{}, nil
	}
	adapters.Register("bravo", factory)
	adapters.Register("alpha", factory)

	names := adapters.RegisteredNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
	if names[0] != "alpha" || names[1] != "bravo" {
		t.Errorf("names = %v, want [alpha bravo]", names)
	}
}
