package proxy_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/samudary/agentid/pkg/adapters"
	github "github.com/samudary/agentid/pkg/adapters/github"
	"github.com/samudary/agentid/pkg/audit"
	"github.com/samudary/agentid/pkg/identity"
	"github.com/samudary/agentid/pkg/proxy"
	"github.com/samudary/agentid/pkg/store"
	"github.com/samudary/agentid/pkg/store/sqlite"
)

// testStack holds all components needed for integration tests.
type testStack struct {
	proxyServer  *proxy.Server
	identitySvc  *identity.Service
	auditLog     *audit.Logger
	store        store.Store
	mockGitHub   *httptest.Server
	proxyTestSrv *httptest.Server
}

// setupStack creates the full integration stack:
// SQLite store -> identity service -> audit logger -> GitHub adapter -> router -> proxy server
func setupStack(t *testing.T) *testStack {
	t.Helper()

	// 1. SQLite store
	dir := t.TempDir()
	store, err := sqlite.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// 2. Key pair and identity service
	kp, err := identity.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}

	identitySvc := identity.NewService(store, kp, identity.ServiceConfig{
		MaxTTL:             30 * time.Minute,
		MaxDelegationDepth: 5,
	})

	// 3. Audit logger
	auditLog := audit.NewLogger(store)

	// 4. Mock GitHub server
	mockGitHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"mock":   true,
			"path":   r.URL.Path,
			"method": r.Method,
		})
	}))
	t.Cleanup(mockGitHub.Close)

	// 5. GitHub adapter pointing at mock server
	ghAuth := adapters.UpstreamAuth{
		Type:  adapters.AuthBearer,
		Token: "upstream-github-token",
	}
	ghAdapter := github.New(mockGitHub.URL, ghAuth)

	// 6. Router
	router := proxy.NewRouter([]adapters.Adapter{ghAdapter})

	// 7. Proxy server
	proxyServer := proxy.NewServer(identitySvc, auditLog, router)
	proxySrv := httptest.NewServer(proxyServer)
	t.Cleanup(proxySrv.Close)

	return &testStack{
		proxyServer:  proxyServer,
		identitySvc:  identitySvc,
		auditLog:     auditLog,
		store:        store,
		mockGitHub:   mockGitHub,
		proxyTestSrv: proxySrv,
	}
}

// createTaskJWT creates a task with the given scopes and returns its JWT.
func createTaskJWT(t *testing.T, stack *testStack, scopes []string) string {
	t.Helper()
	cred, err := stack.identitySvc.CreateTask(context.Background(), identity.TaskRequest{
		ParentID: "human:test@example.com",
		Purpose:  "integration test",
		Scopes:   scopes,
		TTL:      10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	return cred.JWT
}

// jsonRPCCall sends a JSON-RPC request to the proxy server and returns the response.
func jsonRPCCall(t *testing.T, serverURL, token, method string, params any) *http.Response {
	t.Helper()

	var paramsJSON json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
		paramsJSON = b
	}

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"id":      1,
	}
	if paramsJSON != nil {
		reqBody["params"] = json.RawMessage(paramsJSON)
	}

	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", serverURL+"/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	ID any `json:"id"`
}

func parseRPCResponse(t *testing.T, resp *http.Response) rpcResponse {
	t.Helper()
	defer resp.Body.Close()
	var r rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return r
}

func TestToolsListFilteredByScope(t *testing.T) {
	stack := setupStack(t)

	// Task with only github:repo:read should only see github_get_file
	jwt := createTaskJWT(t, stack, []string{"github:repo:read"})

	resp := jsonRPCCall(t, stack.proxyTestSrv.URL, jwt, "tools/list", nil)
	rpc := parseRPCResponse(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if rpc.Error != nil {
		t.Fatalf("unexpected error: %v", rpc.Error)
	}

	var result struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rpc.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if len(result.Tools) != 1 {
		t.Errorf("expected 1 tool for github:repo:read scope, got %d", len(result.Tools))
		for _, tool := range result.Tools {
			t.Logf("  tool: %s", tool.Name)
		}
	}
	if len(result.Tools) > 0 && result.Tools[0].Name != "github_get_file" {
		t.Errorf("expected github_get_file, got %q", result.Tools[0].Name)
	}
}

func TestToolsListAllWithWildcard(t *testing.T) {
	stack := setupStack(t)

	// Wildcard scope should see all tools
	jwt := createTaskJWT(t, stack, []string{"github:*:*"})

	resp := jsonRPCCall(t, stack.proxyTestSrv.URL, jwt, "tools/list", nil)
	rpc := parseRPCResponse(t, resp)

	if rpc.Error != nil {
		t.Fatalf("unexpected error: %v", rpc.Error)
	}

	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rpc.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if len(result.Tools) != 4 {
		t.Errorf("expected 4 tools with wildcard scope, got %d", len(result.Tools))
	}
}

func TestToolCallSuccess(t *testing.T) {
	stack := setupStack(t)
	jwt := createTaskJWT(t, stack, []string{"github:repo:read"})

	params := map[string]any{
		"name": "github_get_file",
		"arguments": map[string]string{
			"owner": "octocat",
			"repo":  "hello-world",
			"path":  "README.md",
		},
	}

	resp := jsonRPCCall(t, stack.proxyTestSrv.URL, jwt, "tools/call", params)
	rpc := parseRPCResponse(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if rpc.Error != nil {
		t.Fatalf("unexpected error: code=%d message=%s", rpc.Error.Code, rpc.Error.Message)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(rpc.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.IsError {
		t.Error("expected IsError=false")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	if result.Content[0].Type != "text" {
		t.Errorf("content type = %q, want \"text\"", result.Content[0].Type)
	}
}

func TestToolCallInsufficientScope(t *testing.T) {
	stack := setupStack(t)
	jwt := createTaskJWT(t, stack, []string{"github:repo:read"})

	params := map[string]any{
		"name": "github_create_branch",
		"arguments": map[string]string{
			"owner":  "octocat",
			"repo":   "hello-world",
			"branch": "new-branch",
		},
	}

	resp := jsonRPCCall(t, stack.proxyTestSrv.URL, jwt, "tools/call", params)
	rpc := parseRPCResponse(t, resp)

	// JSON-RPC 2.0: errors always return HTTP 200 with error info in the body
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (JSON-RPC errors use HTTP 200)", resp.StatusCode)
	}
	if rpc.Error == nil {
		t.Fatal("expected error response")
	}
	if rpc.Error.Code != -32003 {
		t.Errorf("error code = %d, want -32003", rpc.Error.Code)
	}
}

func TestToolCallNoAuth(t *testing.T) {
	stack := setupStack(t)

	// No token provided
	resp := jsonRPCCall(t, stack.proxyTestSrv.URL, "", "tools/list", nil)
	rpc := parseRPCResponse(t, resp)

	// JSON-RPC 2.0: auth errors still return HTTP 200 with error in body
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (JSON-RPC errors use HTTP 200)", resp.StatusCode)
	}
	if rpc.Error == nil {
		t.Fatal("expected error response")
	}
	if rpc.Error.Code != -32000 {
		t.Errorf("error code = %d, want -32000", rpc.Error.Code)
	}
}

func TestToolCallExpiredToken(t *testing.T) {
	stack := setupStack(t)

	// Create a task with a very short TTL, then wait for it to expire
	cred, err := stack.identitySvc.CreateTask(context.Background(), identity.TaskRequest{
		ParentID: "human:test@example.com",
		Purpose:  "expired test",
		Scopes:   []string{"github:repo:read"},
		TTL:      1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Wait for the token to expire
	time.Sleep(100 * time.Millisecond)

	resp := jsonRPCCall(t, stack.proxyTestSrv.URL, cred.JWT, "tools/list", nil)
	rpc := parseRPCResponse(t, resp)

	// JSON-RPC 2.0: auth errors still return HTTP 200 with error in body
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (JSON-RPC errors use HTTP 200)", resp.StatusCode)
	}
	if rpc.Error == nil {
		t.Fatal("expected error response")
	}
	if rpc.Error.Code != -32001 {
		t.Errorf("error code = %d, want -32001", rpc.Error.Code)
	}
}

func TestToolCallRevokedToken(t *testing.T) {
	stack := setupStack(t)
	ctx := context.Background()

	cred, err := stack.identitySvc.CreateTask(ctx, identity.TaskRequest{
		ParentID: "human:test@example.com",
		Purpose:  "revocation test",
		Scopes:   []string{"github:repo:read"},
		TTL:      10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Verify it works first
	resp := jsonRPCCall(t, stack.proxyTestSrv.URL, cred.JWT, "tools/list", nil)
	rpc := parseRPCResponse(t, resp)
	if rpc.Error != nil {
		t.Fatalf("expected success before revocation, got error: %v", rpc.Error)
	}

	if err := stack.identitySvc.RevokeTask(ctx, cred.TaskID, "test revocation"); err != nil {
		t.Fatalf("revoke task: %v", err)
	}

	// Try again which should fail
	resp = jsonRPCCall(t, stack.proxyTestSrv.URL, cred.JWT, "tools/list", nil)
	rpc = parseRPCResponse(t, resp)

	// JSON-RPC 2.0: auth errors still return HTTP 200 with error in body
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (JSON-RPC errors use HTTP 200)", resp.StatusCode)
	}
	if rpc.Error == nil {
		t.Fatal("expected error response after revocation")
	}
	if rpc.Error.Code != -32001 {
		t.Errorf("error code = %d, want -32001", rpc.Error.Code)
	}
}

func TestToolCallUnknownTool(t *testing.T) {
	stack := setupStack(t)
	jwt := createTaskJWT(t, stack, []string{"github:repo:read"})

	params := map[string]any{
		"name":      "nonexistent_tool",
		"arguments": map[string]string{},
	}

	resp := jsonRPCCall(t, stack.proxyTestSrv.URL, jwt, "tools/call", params)
	rpc := parseRPCResponse(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (JSON-RPC error is still 200)", resp.StatusCode)
	}
	if rpc.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
	if rpc.Error.Code != -32602 {
		t.Errorf("error code = %d, want -32602", rpc.Error.Code)
	}
}

func TestToolCallUnknownMethod(t *testing.T) {
	stack := setupStack(t)
	jwt := createTaskJWT(t, stack, []string{"github:repo:read"})

	resp := jsonRPCCall(t, stack.proxyTestSrv.URL, jwt, "nonexistent/method", nil)
	rpc := parseRPCResponse(t, resp)

	if rpc.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if rpc.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", rpc.Error.Code)
	}
}

func TestToolCallWithWildcardScope(t *testing.T) {
	stack := setupStack(t)
	// Wildcard scope should grant access to all github tools
	jwt := createTaskJWT(t, stack, []string{"github:*:*"})

	// Should be able to call github_get_file (requires github:repo:read)
	params := map[string]any{
		"name": "github_get_file",
		"arguments": map[string]string{
			"owner": "octocat",
			"repo":  "hello-world",
			"path":  "README.md",
		},
	}

	resp := jsonRPCCall(t, stack.proxyTestSrv.URL, jwt, "tools/call", params)
	rpc := parseRPCResponse(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if rpc.Error != nil {
		t.Fatalf("unexpected error with wildcard scope: code=%d message=%s", rpc.Error.Code, rpc.Error.Message)
	}
}

func TestJWKSEndpoint(t *testing.T) {
	stack := setupStack(t)

	resp, err := http.Get(stack.proxyTestSrv.URL + "/.well-known/jwks.json")
	if err != nil {
		t.Fatalf("GET JWKS: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=3600" {
		t.Errorf("Cache-Control = %q, want \"public, max-age=3600\"", cc)
	}

	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Crv string `json:"crv"`
			X   string `json:"x"`
			Y   string `json:"y"`
			Use string `json:"use"`
			Alg string `json:"alg"`
			Kid string `json:"kid"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		t.Fatalf("decode JWKS: %v", err)
	}

	if len(jwks.Keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(jwks.Keys))
	}
	key := jwks.Keys[0]
	if key.Kty != "EC" {
		t.Errorf("kty = %q, want EC", key.Kty)
	}
	if key.Crv != "P-256" {
		t.Errorf("crv = %q, want P-256", key.Crv)
	}
	if key.Alg != "ES256" {
		t.Errorf("alg = %q, want ES256", key.Alg)
	}
	if key.Use != "sig" {
		t.Errorf("use = %q, want sig", key.Use)
	}
	if key.Kid == "" {
		t.Error("kid should not be empty")
	}
	if key.X == "" || key.Y == "" {
		t.Error("x and y coordinates should not be empty")
	}
}

// TestEndToEnd exercises the full flow:
// 1. Create task with scopes
// 2. Call a tool via MCP JSON-RPC
// 3. Verify the audit event was emitted with enriched fields
func TestEndToEnd(t *testing.T) {
	stack := setupStack(t)
	ctx := context.Background()

	// 1. Create a task
	cred, err := stack.identitySvc.CreateTask(ctx, identity.TaskRequest{
		ParentID: "human:operator@example.com",
		Purpose:  "end-to-end integration test",
		Scopes:   []string{"github:repo:read"},
		TTL:      10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// 2. Call a tool via the MCP proxy
	params := map[string]any{
		"name": "github_get_file",
		"arguments": map[string]string{
			"owner": "octocat",
			"repo":  "hello-world",
			"path":  "README.md",
		},
	}
	resp := jsonRPCCall(t, stack.proxyTestSrv.URL, cred.JWT, "tools/call", params)
	rpc := parseRPCResponse(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if rpc.Error != nil {
		t.Fatalf("unexpected error: code=%d message=%s", rpc.Error.Code, rpc.Error.Message)
	}

	// 3. Query audit events and verify enrichment
	events, err := stack.auditLog.Query(ctx, store.EventFilter{
		TaskID: cred.TaskID,
		Event:  audit.EventToolInvoked,
	})
	if err != nil {
		t.Fatalf("query audit events: %v", err)
	}

	if len(events) == 0 {
		t.Fatal("expected at least one tool.invoked audit event")
	}

	event := events[0]
	if event.TaskID != cred.TaskID {
		t.Errorf("event task ID = %q, want %q", event.TaskID, cred.TaskID)
	}
	if event.Event != audit.EventToolInvoked {
		t.Errorf("event type = %q, want %q", event.Event, audit.EventToolInvoked)
	}

	// Verify enriched payload fields
	payload := event.Payload
	if payload["tool"] != "github_get_file" {
		t.Errorf("payload.tool = %v, want github_get_file", payload["tool"])
	}
	if payload["result"] != "success" {
		t.Errorf("payload.result = %v, want success", payload["result"])
	}

	// duration_ms should be present and non-negative
	durationMs, ok := payload["duration_ms"]
	if !ok {
		t.Error("payload.duration_ms is missing")
	} else {
		// JSON numbers are float64 when unmarshalled via map[string]any
		if d, ok := durationMs.(float64); ok {
			if d < 0 {
				t.Errorf("payload.duration_ms = %v, want >= 0", d)
			}
		} else {
			t.Errorf("payload.duration_ms has unexpected type %T", durationMs)
		}
	}

	// upstream_status should be present (mock returns 200)
	upstreamStatus, ok := payload["upstream_status"]
	if !ok {
		t.Error("payload.upstream_status is missing")
	} else {
		if s, ok := upstreamStatus.(float64); ok {
			if int(s) != 200 {
				t.Errorf("payload.upstream_status = %v, want 200", s)
			}
		} else {
			t.Errorf("payload.upstream_status has unexpected type %T", upstreamStatus)
		}
	}

	// Verify delegation chain is present on the event
	if len(event.DelegationChain) == 0 {
		t.Error("expected non-empty delegation chain on audit event")
	}
}
