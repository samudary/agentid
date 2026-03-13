package github_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samudary/agentid/pkg/adapters"
	"github.com/samudary/agentid/pkg/adapters/adaptertest"
	github "github.com/samudary/agentid/pkg/adapters/github"
)

func newTestAdapter(t *testing.T, handler http.HandlerFunc) (*github.Adapter, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	auth := adapters.UpstreamAuth{
		Type:  adapters.AuthBearer,
		Token: "test-token-123",
	}
	adapter := github.New(server.URL, auth)
	return adapter, server
}

func TestGetFile(t *testing.T) {
	var receivedMethod, receivedPath, receivedAuth string

	adapter, _ := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		receivedAuth = r.Header.Get("Authorization")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"name":    "README.md",
			"path":    "README.md",
			"content": "SGVsbG8gV29ybGQ=",
		})
	})

	input, _ := json.Marshal(map[string]string{
		"owner": "octocat",
		"repo":  "hello-world",
		"path":  "README.md",
	})

	result, err := adapter.Invoke(context.Background(), "github_get_file", input)
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}

	if result.IsError {
		t.Fatal("expected success, got error result")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	if result.Content[0].Type != "text" {
		t.Errorf("content type = %q, want \"text\"", result.Content[0].Type)
	}

	// Verify the mock received the correct request
	if receivedMethod != "GET" {
		t.Errorf("method = %q, want GET", receivedMethod)
	}
	if receivedPath != "/repos/octocat/hello-world/contents/README.md" {
		t.Errorf("path = %q, want /repos/octocat/hello-world/contents/README.md", receivedPath)
	}
	if receivedAuth != "Bearer test-token-123" {
		t.Errorf("auth = %q, want \"Bearer test-token-123\"", receivedAuth)
	}

	// Verify the response contains expected data
	var respData map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &respData); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if respData["name"] != "README.md" {
		t.Errorf("name = %v, want README.md", respData["name"])
	}
}

func TestGetFileWithRef(t *testing.T) {
	var receivedQuery string

	adapter, _ := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"name": "file.go"})
	})

	input, _ := json.Marshal(map[string]string{
		"owner": "octocat",
		"repo":  "hello-world",
		"path":  "file.go",
		"ref":   "feature-branch",
	})

	result, err := adapter.Invoke(context.Background(), "github_get_file", input)
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	if result.IsError {
		t.Fatal("expected success, got error")
	}
	if receivedQuery != "ref=feature-branch" {
		t.Errorf("query = %q, want \"ref=feature-branch\"", receivedQuery)
	}
}

func TestGetFileNotFound(t *testing.T) {
	adapter, _ := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Not Found",
		})
	})

	input, _ := json.Marshal(map[string]string{
		"owner": "octocat",
		"repo":  "hello-world",
		"path":  "nonexistent.md",
	})

	result, err := adapter.Invoke(context.Background(), "github_get_file", input)
	if err != nil {
		t.Fatalf("invoke should not return error for 404, got: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for 404 response")
	}
}

func TestCreateBranch(t *testing.T) {
	var requestCount int
	var postBody map[string]string

	adapter, _ := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		switch {
		case r.Method == "GET" && r.URL.Path == "/repos/octocat/hello-world/git/ref/heads/main":
			// Step 1: Return the base ref SHA
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"ref": "refs/heads/main",
				"object": map[string]string{
					"sha":  "abc123def456",
					"type": "commit",
				},
			})

		case r.Method == "POST" && r.URL.Path == "/repos/octocat/hello-world/git/refs":
			// Step 2: Create the new ref
			body, _ := io.ReadAll(r.Body)
			json.Unmarshal(body, &postBody)

			w.WriteHeader(http.StatusCreated)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"ref": "refs/heads/feature-branch",
				"object": map[string]string{
					"sha": "abc123def456",
				},
			})

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	input, _ := json.Marshal(map[string]string{
		"owner":  "octocat",
		"repo":   "hello-world",
		"branch": "feature-branch",
	})

	result, err := adapter.Invoke(context.Background(), "github_create_branch", input)
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	// Verify both requests were made
	if requestCount != 2 {
		t.Errorf("expected 2 requests (GET ref + POST ref), got %d", requestCount)
	}

	// Verify the POST body
	if postBody["ref"] != "refs/heads/feature-branch" {
		t.Errorf("ref = %q, want \"refs/heads/feature-branch\"", postBody["ref"])
	}
	if postBody["sha"] != "abc123def456" {
		t.Errorf("sha = %q, want \"abc123def456\"", postBody["sha"])
	}
}

func TestCreateBranchFromCustomRef(t *testing.T) {
	var getPath string

	adapter, _ := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			getPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"ref": "refs/heads/develop",
				"object": map[string]string{
					"sha":  "deadbeef",
					"type": "commit",
				},
			})
		} else {
			w.WriteHeader(http.StatusCreated)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"ref": "refs/heads/new-branch"})
		}
	})

	input, _ := json.Marshal(map[string]string{
		"owner":    "octocat",
		"repo":     "hello-world",
		"branch":   "new-branch",
		"from_ref": "develop",
	})

	result, err := adapter.Invoke(context.Background(), "github_create_branch", input)
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	if result.IsError {
		t.Fatal("expected success")
	}

	// Should have requested the "develop" ref, not "main"
	expectedPath := "/repos/octocat/hello-world/git/ref/heads/develop"
	if getPath != expectedPath {
		t.Errorf("get path = %q, want %q", getPath, expectedPath)
	}
}

func TestCreatePR(t *testing.T) {
	var receivedBody map[string]string
	var receivedMethod, receivedPath string

	adapter, _ := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path

		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number":   42,
			"title":    "Add feature",
			"html_url": "https://github.com/octocat/hello-world/pull/42",
		})
	})

	input, _ := json.Marshal(map[string]string{
		"owner": "octocat",
		"repo":  "hello-world",
		"title": "Add feature",
		"body":  "This PR adds a new feature",
		"head":  "feature-branch",
		"base":  "develop",
	})

	result, err := adapter.Invoke(context.Background(), "github_create_pr", input)
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	if receivedMethod != "POST" {
		t.Errorf("method = %q, want POST", receivedMethod)
	}
	if receivedPath != "/repos/octocat/hello-world/pulls" {
		t.Errorf("path = %q, want /repos/octocat/hello-world/pulls", receivedPath)
	}
	if receivedBody["title"] != "Add feature" {
		t.Errorf("title = %q, want \"Add feature\"", receivedBody["title"])
	}
	if receivedBody["head"] != "feature-branch" {
		t.Errorf("head = %q, want \"feature-branch\"", receivedBody["head"])
	}
	if receivedBody["base"] != "develop" {
		t.Errorf("base = %q, want \"develop\"", receivedBody["base"])
	}
}

func TestCreatePRDefaultBase(t *testing.T) {
	var receivedBody map[string]string

	adapter, _ := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"number": 1})
	})

	input, _ := json.Marshal(map[string]string{
		"owner": "octocat",
		"repo":  "hello-world",
		"title": "Quick fix",
		"head":  "fix-branch",
	})

	result, err := adapter.Invoke(context.Background(), "github_create_pr", input)
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	if result.IsError {
		t.Fatal("expected success")
	}

	// Base should default to "main"
	if receivedBody["base"] != "main" {
		t.Errorf("base = %q, want \"main\"", receivedBody["base"])
	}
}

func TestGetCIStatus(t *testing.T) {
	var receivedMethod, receivedPath string

	adapter, _ := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"state":       "success",
			"total_count": 3,
			"statuses": []map[string]string{
				{"state": "success", "context": "ci/build"},
				{"state": "success", "context": "ci/test"},
				{"state": "success", "context": "ci/lint"},
			},
		})
	})

	input, _ := json.Marshal(map[string]string{
		"owner": "octocat",
		"repo":  "hello-world",
		"ref":   "abc123",
	})

	result, err := adapter.Invoke(context.Background(), "github_get_ci_status", input)
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	if result.IsError {
		t.Fatal("expected success")
	}

	if receivedMethod != "GET" {
		t.Errorf("method = %q, want GET", receivedMethod)
	}
	if receivedPath != "/repos/octocat/hello-world/commits/abc123/status" {
		t.Errorf("path = %q, want /repos/octocat/hello-world/commits/abc123/status", receivedPath)
	}

	// Parse response and check state
	var respData map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &respData); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if respData["state"] != "success" {
		t.Errorf("state = %v, want \"success\"", respData["state"])
	}
}

func TestScopeMapping(t *testing.T) {
	auth := adapters.UpstreamAuth{Type: adapters.AuthBearer, Token: "x"}
	adapter := github.New("https://api.github.com", auth)

	tests := []struct {
		tool  string
		scope string
	}{
		{"github_get_file", "github:repo:read"},
		{"github_create_branch", "github:repo:write"},
		{"github_create_pr", "github:pulls:write"},
		{"github_get_ci_status", "github:actions:read"},
		{"nonexistent_tool", ""},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			got := adapter.ScopeForTool(tt.tool)
			if got != tt.scope {
				t.Errorf("ScopeForTool(%q) = %q, want %q", tt.tool, got, tt.scope)
			}
		})
	}
}

func TestToolDefinitions(t *testing.T) {
	auth := adapters.UpstreamAuth{Type: adapters.AuthBearer, Token: "x"}
	adapter := github.New("https://api.github.com", auth)

	tools := adapter.Tools()
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}

	for _, tool := range tools {
		if tool.Name == "" {
			t.Error("tool has empty name")
		}
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
		if len(tool.InputSchema) == 0 {
			t.Errorf("tool %q has empty input schema", tool.Name)
		}

		// Verify schema is valid JSON
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Errorf("tool %q has invalid input schema JSON: %v", tool.Name, err)
		}
	}
}

func TestAdapterName(t *testing.T) {
	auth := adapters.UpstreamAuth{Type: adapters.AuthBearer, Token: "x"}
	adapter := github.New("https://api.github.com", auth)

	if adapter.Name() != "github" {
		t.Errorf("Name() = %q, want \"github\"", adapter.Name())
	}
}

func TestGetFilePathTraversal(t *testing.T) {
	adapter, _ := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make HTTP request for path traversal input")
	})

	tests := []struct {
		name  string
		input map[string]string
	}{
		{
			name:  "path traversal in path",
			input: map[string]string{"owner": "octocat", "repo": "hello-world", "path": "../../etc/passwd"},
		},
		{
			name:  "traversal in owner",
			input: map[string]string{"owner": "../evil", "repo": "hello-world", "path": "README.md"},
		},
		{
			name:  "slash in owner",
			input: map[string]string{"owner": "octocat/evil", "repo": "hello-world", "path": "README.md"},
		},
		{
			name:  "query string in repo",
			input: map[string]string{"owner": "octocat", "repo": "hello-world?admin=true", "path": "README.md"},
		},
		{
			name:  "null byte in path",
			input: map[string]string{"owner": "octocat", "repo": "hello-world", "path": "README.md\x00.evil"},
		},
		{
			name:  "traversal in ref",
			input: map[string]string{"owner": "octocat", "repo": "hello-world", "path": "README.md", "ref": "../../main"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, _ := json.Marshal(tt.input)
			_, err := adapter.Invoke(context.Background(), "github_get_file", input)
			if err == nil {
				t.Error("expected validation error, got nil")
			}
		})
	}
}

func TestCreateBranchInputValidation(t *testing.T) {
	adapter, _ := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make HTTP request for invalid input")
	})

	tests := []struct {
		name  string
		input map[string]string
	}{
		{
			name:  "traversal in from_ref",
			input: map[string]string{"owner": "octocat", "repo": "hello-world", "branch": "new", "from_ref": "../../main"},
		},
		{
			name:  "query string in branch",
			input: map[string]string{"owner": "octocat", "repo": "hello-world", "branch": "new?foo=bar"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, _ := json.Marshal(tt.input)
			_, err := adapter.Invoke(context.Background(), "github_create_branch", input)
			if err == nil {
				t.Error("expected validation error, got nil")
			}
		})
	}
}

func TestValidRefWithSlashes(t *testing.T) {
	// Branch names like "feature/foo" should be accepted
	var receivedPath string
	adapter, _ := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"name": "file.go"})
	})

	input, _ := json.Marshal(map[string]string{
		"owner": "octocat",
		"repo":  "hello-world",
		"path":  "src/main.go",
		"ref":   "feature/my-branch",
	})

	result, err := adapter.Invoke(context.Background(), "github_get_file", input)
	if err != nil {
		t.Fatalf("valid ref with slash should be accepted, got error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected success")
	}
	if receivedPath != "/repos/octocat/hello-world/contents/src/main.go" {
		t.Errorf("path = %q, want /repos/octocat/hello-world/contents/src/main.go", receivedPath)
	}
}

func TestInvokeUnknownTool(t *testing.T) {
	adapter, _ := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make HTTP request for unknown tool")
	})

	_, err := adapter.Invoke(context.Background(), "nonexistent_tool", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestAdapterConformance(t *testing.T) {
	// Use the conformance test harness with sample inputs and a mock server
	adapter, _ := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Handle the two-step create branch flow
		if r.Method == "GET" && r.URL.Path == "/repos/octocat/hello-world/git/ref/heads/main" {
			json.NewEncoder(w).Encode(map[string]any{
				"object": map[string]string{"sha": "abc123"},
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]any{"mock": true, "path": r.URL.Path})
	})

	sampleInputs := map[string]json.RawMessage{
		"github_get_file":      json.RawMessage(`{"owner":"octocat","repo":"hello-world","path":"README.md"}`),
		"github_create_branch": json.RawMessage(`{"owner":"octocat","repo":"hello-world","branch":"test-branch"}`),
		"github_create_pr":     json.RawMessage(`{"owner":"octocat","repo":"hello-world","title":"Test PR","head":"feature"}`),
		"github_get_ci_status": json.RawMessage(`{"owner":"octocat","repo":"hello-world","ref":"abc123"}`),
	}

	adaptertest.TestAdapterWithInvoke(t, adapter, sampleInputs)
}
