package rest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samudary/agentid/pkg/adapters"
	"github.com/samudary/agentid/pkg/adapters/rest"
	"github.com/samudary/agentid/pkg/config"
)

func TestNewValidatesOperations(t *testing.T) {
	auth := adapters.UpstreamAuth{}
	tests := []struct {
		name    string
		ops     []config.OperationConfig
		wantErr string
	}{
		{
			name:    "missing name",
			ops:     []config.OperationConfig{{Method: "GET", Path: "/test"}},
			wantErr: "missing name",
		},
		{
			name:    "missing method",
			ops:     []config.OperationConfig{{Name: "test_op", Path: "/test"}},
			wantErr: "missing method",
		},
		{
			name:    "missing path",
			ops:     []config.OperationConfig{{Name: "test_op", Method: "GET"}},
			wantErr: "missing path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := rest.New("test", "http://localhost", auth, tt.ops)
			if err == nil {
				t.Fatal("expected error")
			}
			if !contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestToolDefinitionsFromConfig(t *testing.T) {
	auth := adapters.UpstreamAuth{}
	ops := []config.OperationConfig{
		{
			Name:        "get_item",
			Description: "Get an item by ID",
			Scope:       "items:read",
			Method:      "GET",
			Path:        "/items/{id}",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]string{"type": "string", "description": "Item ID"},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:   "list_items",
			Scope:  "items:read",
			Method: "GET",
			Path:   "/items",
		},
	}

	adapter, err := rest.New("myservice", "http://localhost", auth, ops)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if adapter.Name() != "myservice" {
		t.Errorf("name = %q, want %q", adapter.Name(), "myservice")
	}

	tools := adapter.Tools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	// First tool: explicit description and schema
	if tools[0].Name != "get_item" {
		t.Errorf("tools[0].Name = %q", tools[0].Name)
	}
	if tools[0].Description != "Get an item by ID" {
		t.Errorf("tools[0].Description = %q", tools[0].Description)
	}

	// Second tool: auto-generated description from method+path
	if tools[1].Description != "GET /items" {
		t.Errorf("tools[1].Description = %q, want %q", tools[1].Description, "GET /items")
	}
}

func TestScopeForTool(t *testing.T) {
	auth := adapters.UpstreamAuth{}
	ops := []config.OperationConfig{
		{Name: "read_item", Scope: "items:read", Method: "GET", Path: "/items/{id}"},
		{Name: "write_item", Scope: "items:write", Method: "POST", Path: "/items"},
	}

	adapter, err := rest.New("test", "http://localhost", auth, ops)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if scope := adapter.ScopeForTool("read_item"); scope != "items:read" {
		t.Errorf("scope = %q, want %q", scope, "items:read")
	}
	if scope := adapter.ScopeForTool("write_item"); scope != "items:write" {
		t.Errorf("scope = %q, want %q", scope, "items:write")
	}
	if scope := adapter.ScopeForTool("nonexistent"); scope != "" {
		t.Errorf("scope for unknown tool = %q, want empty", scope)
	}
}

func TestInvokeGETWithPathParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/items/abc-123" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/items/abc-123")
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"id": "abc-123", "name": "Test Item"})
	}))
	defer server.Close()

	adapter, err := rest.New("test", server.URL, adapters.UpstreamAuth{}, []config.OperationConfig{
		{Name: "get_item", Scope: "items:read", Method: "GET", Path: "/items/{id}"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := adapter.Invoke(context.Background(), "get_item", json.RawMessage(`{"id":"abc-123"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected IsError=false")
	}
	if result.StatusCode != 200 {
		t.Errorf("status = %d, want 200", result.StatusCode)
	}
}

func TestInvokeGETWithQueryParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/items" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "10" {
			t.Errorf("query limit = %q, want %q", r.URL.Query().Get("limit"), "10")
		}
		if r.URL.Query().Get("offset") != "20" {
			t.Errorf("query offset = %q, want %q", r.URL.Query().Get("offset"), "20")
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]string{"item1", "item2"})
	}))
	defer server.Close()

	adapter, err := rest.New("test", server.URL, adapters.UpstreamAuth{}, []config.OperationConfig{
		{Name: "list_items", Scope: "items:read", Method: "GET", Path: "/items"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := adapter.Invoke(context.Background(), "list_items", json.RawMessage(`{"limit":10,"offset":20}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected IsError=false")
	}
}

func TestInvokePOSTWithBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/items" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "New Item" {
			t.Errorf("body name = %v", body["name"])
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": "new-id"})
	}))
	defer server.Close()

	adapter, err := rest.New("test", server.URL, adapters.UpstreamAuth{}, []config.OperationConfig{
		{Name: "create_item", Scope: "items:write", Method: "POST", Path: "/items"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := adapter.Invoke(context.Background(), "create_item", json.RawMessage(`{"name":"New Item"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected IsError=false")
	}
	if result.StatusCode != 201 {
		t.Errorf("status = %d, want 201", result.StatusCode)
	}
}

func TestInvokePOSTWithPathAndBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/acme/items" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/orgs/acme/items")
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		// "org" should be consumed by path, not in body
		if _, exists := body["org"]; exists {
			t.Error("path param 'org' should not appear in body")
		}
		if body["name"] != "Widget" {
			t.Errorf("body name = %v", body["name"])
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer server.Close()

	adapter, err := rest.New("test", server.URL, adapters.UpstreamAuth{}, []config.OperationConfig{
		{Name: "create_org_item", Scope: "items:write", Method: "POST", Path: "/orgs/{org}/items"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	input := `{"org":"acme","name":"Widget"}`
	result, err := adapter.Invoke(context.Background(), "create_org_item", json.RawMessage(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected IsError=false")
	}
}

func TestInvokeMissingPathParam(t *testing.T) {
	adapter, err := rest.New("test", "http://localhost", adapters.UpstreamAuth{}, []config.OperationConfig{
		{Name: "get_item", Scope: "items:read", Method: "GET", Path: "/items/{id}"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = adapter.Invoke(context.Background(), "get_item", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for missing path param")
	}
	if !contains(err.Error(), "missing required path parameter") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestInvokeUnknownTool(t *testing.T) {
	adapter, err := rest.New("test", "http://localhost", adapters.UpstreamAuth{}, []config.OperationConfig{
		{Name: "get_item", Scope: "items:read", Method: "GET", Path: "/items/{id}"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = adapter.Invoke(context.Background(), "nonexistent", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestInvokeUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer server.Close()

	adapter, err := rest.New("test", server.URL, adapters.UpstreamAuth{}, []config.OperationConfig{
		{Name: "get_item", Scope: "items:read", Method: "GET", Path: "/items/{id}"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := adapter.Invoke(context.Background(), "get_item", json.RawMessage(`{"id":"abc"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for 403")
	}
	if result.StatusCode != 403 {
		t.Errorf("status = %d, want 403", result.StatusCode)
	}
}

func TestInvokeWithAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer my-token" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	auth := adapters.UpstreamAuth{Type: adapters.AuthBearer, Token: "my-token"}
	adapter, err := rest.New("test", server.URL, auth, []config.OperationConfig{
		{Name: "get_item", Scope: "items:read", Method: "GET", Path: "/items/{id}"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = adapter.Invoke(context.Background(), "get_item", json.RawMessage(`{"id":"abc"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAutoGeneratedSchema(t *testing.T) {
	adapter, err := rest.New("test", "http://localhost", adapters.UpstreamAuth{}, []config.OperationConfig{
		{Name: "get_item", Scope: "items:read", Method: "GET", Path: "/items/{id}"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tools := adapter.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	var schema map[string]any
	if err := json.Unmarshal(tools[0].InputSchema, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	if schema["type"] != "object" {
		t.Errorf("schema type = %v", schema["type"])
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties map")
	}
	if _, exists := props["id"]; !exists {
		t.Error("expected 'id' in properties")
	}

	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatal("expected required array")
	}
	if len(required) != 1 || required[0] != "id" {
		t.Errorf("required = %v, want [id]", required)
	}
}

func TestPathEscaping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The path should be URL-encoded
		if r.URL.RawPath != "" && r.URL.RawPath != "/items/hello%20world" {
			t.Errorf("raw path = %q", r.URL.RawPath)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	adapter, err := rest.New("test", server.URL, adapters.UpstreamAuth{}, []config.OperationConfig{
		{Name: "get_item", Scope: "items:read", Method: "GET", Path: "/items/{id}"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = adapter.Invoke(context.Background(), "get_item", json.RawMessage(`{"id":"hello world"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
