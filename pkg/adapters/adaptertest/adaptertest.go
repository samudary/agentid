// Package adaptertest provides a reusable conformance test suite for any
// implementation of the adapters.Adapter interface. Adapter-specific test
// files call TestAdapter(t, a) with a concrete Adapter to verify it meets
// the interface contract.
//
// The suite validates:
//   - Name() returns a non-empty string
//   - Tools() returns at least one tool definition
//   - Each tool has a valid name, description, and parseable InputSchema
//   - ScopeForTool() returns a scope for every registered tool
//   - Invoke() returns an error for unknown tool names
//   - Invoke() can be called for each registered tool (with empty input)
package adaptertest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/samudary/agentid/pkg/adapters"
)

// TestAdapter runs the full conformance suite against the provided Adapter.
// The adapter should be configured to talk to a mock or test server so
// that Invoke calls succeed.
func TestAdapter(t *testing.T, a adapters.Adapter) {
	t.Run("Name", func(t *testing.T) { testName(t, a) })
	t.Run("Tools", func(t *testing.T) { testTools(t, a) })
	t.Run("ScopeForTool", func(t *testing.T) { testScopeForTool(t, a) })
	t.Run("InvokeUnknownTool", func(t *testing.T) { testInvokeUnknownTool(t, a) })
}

// TestAdapterWithInvoke runs the full conformance suite including Invoke
// tests for every registered tool. Each tool is invoked with the provided
// sample inputs. If no sample input is provided for a tool, it is invoked
// with an empty JSON object.
//
// sampleInputs maps tool name -> JSON input to use for testing Invoke.
func TestAdapterWithInvoke(t *testing.T, a adapters.Adapter, sampleInputs map[string]json.RawMessage) {
	TestAdapter(t, a)
	t.Run("InvokeRegisteredTools", func(t *testing.T) {
		testInvokeRegistered(t, a, sampleInputs)
	})
}

func testName(t *testing.T, a adapters.Adapter) {
	name := a.Name()
	if name == "" {
		t.Fatal("Name() must return a non-empty string")
	}
}

func testTools(t *testing.T, a adapters.Adapter) {
	tools := a.Tools()
	if len(tools) == 0 {
		t.Fatal("Tools() must return at least one tool definition")
	}

	seen := make(map[string]bool)
	for i, tool := range tools {
		if tool.Name == "" {
			t.Errorf("tool[%d]: Name must not be empty", i)
		}
		if seen[tool.Name] {
			t.Errorf("tool[%d]: duplicate tool name %q", i, tool.Name)
		}
		seen[tool.Name] = true

		if tool.Description == "" {
			t.Errorf("tool %q: Description must not be empty", tool.Name)
		}

		if len(tool.InputSchema) == 0 {
			t.Errorf("tool %q: InputSchema must not be empty", tool.Name)
			continue
		}

		// Verify InputSchema is valid JSON
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Errorf("tool %q: InputSchema is not valid JSON: %v", tool.Name, err)
			continue
		}

		// Verify it looks like a JSON Schema (has "type" field)
		if _, ok := schema["type"]; !ok {
			t.Errorf("tool %q: InputSchema missing 'type' field", tool.Name)
		}
	}
}

func testScopeForTool(t *testing.T, a adapters.Adapter) {
	for _, tool := range a.Tools() {
		scope := a.ScopeForTool(tool.Name)
		if scope == "" {
			t.Errorf("ScopeForTool(%q) returned empty string; every tool must have a scope", tool.Name)
		}
	}

	// Unknown tool should return empty scope
	scope := a.ScopeForTool("__nonexistent_tool_name__")
	if scope != "" {
		t.Errorf("ScopeForTool(unknown) = %q, want empty string", scope)
	}
}

func testInvokeUnknownTool(t *testing.T, a adapters.Adapter) {
	_, err := a.Invoke(context.Background(), "__nonexistent_tool_name__", json.RawMessage(`{}`))
	if err == nil {
		t.Error("Invoke(unknown tool) must return an error")
	}
}

func testInvokeRegistered(t *testing.T, a adapters.Adapter, sampleInputs map[string]json.RawMessage) {
	for _, tool := range a.Tools() {
		t.Run(tool.Name, func(t *testing.T) {
			input, ok := sampleInputs[tool.Name]
			if !ok {
				input = json.RawMessage(`{}`)
			}

			result, err := a.Invoke(context.Background(), tool.Name, input)
			if err != nil {
				t.Fatalf("Invoke(%q) returned error: %v", tool.Name, err)
			}
			if result == nil {
				t.Fatal("Invoke returned nil result")
			}
			if len(result.Content) == 0 {
				t.Error("Invoke returned empty content")
			}
			for i, block := range result.Content {
				if block.Type == "" {
					t.Errorf("content[%d]: Type must not be empty", i)
				}
			}
		})
	}
}
