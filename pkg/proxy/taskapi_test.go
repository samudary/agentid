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
	"github.com/samudary/agentid/pkg/admin"
	"github.com/samudary/agentid/pkg/audit"
	"github.com/samudary/agentid/pkg/identity"
	"github.com/samudary/agentid/pkg/proxy"
	"github.com/samudary/agentid/pkg/store"
	"github.com/samudary/agentid/pkg/store/sqlite"
)

// taskAPIStack holds components for task API tests.
type taskAPIStack struct {
	server   *httptest.Server
	adminKey string
	auditLog *audit.Logger
}

// setupTaskAPIStack creates the full stack with task API endpoints registered.
func setupTaskAPIStack(t *testing.T) *taskAPIStack {
	t.Helper()

	dir := t.TempDir()
	store, err := sqlite.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	kp, err := identity.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}

	identitySvc := identity.NewService(store, kp, identity.ServiceConfig{
		MaxTTL:             30 * time.Minute,
		MaxDelegationDepth: 5,
	})

	auditLog := audit.NewLogger(store)

	// Mock upstream (not used by task API but required for proxy server)
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(mockUpstream.Close)

	ghAuth := adapters.UpstreamAuth{Type: adapters.AuthBearer, Token: "tok"}
	ghAdapter := github.New(mockUpstream.URL, ghAuth)
	router := proxy.NewRouter([]adapters.Adapter{ghAdapter})

	proxyServer := proxy.NewServer(identitySvc, auditLog, router)

	// Register task API with admin auth
	adminKey := "test-admin-key-for-api"
	adminAuth, err := admin.NewAPIKeyAuth(adminKey)
	if err != nil {
		t.Fatalf("create admin auth: %v", err)
	}
	proxyServer.RegisterTaskAPI(adminAuth)

	srv := httptest.NewServer(proxyServer)
	t.Cleanup(srv.Close)

	return &taskAPIStack{
		server:   srv,
		adminKey: adminKey,
		auditLog: auditLog,
	}
}

func taskAPIRequest(t *testing.T, method, url, adminKey string, body any) *http.Response {
	t.Helper()

	var reqBody *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if adminKey != "" {
		req.Header.Set("Authorization", "Bearer "+adminKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func TestCreateTaskAPI(t *testing.T) {
	stack := setupTaskAPIStack(t)

	resp := taskAPIRequest(t, "POST", stack.server.URL+"/api/v1/tasks", stack.adminKey, map[string]any{
		"parent_id": "human:admin@example.com",
		"purpose":   "test task creation",
		"scopes":    []string{"github:repo:read"},
		"ttl":       "15m",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	var result struct {
		TaskID    string   `json:"task_id"`
		JWT       string   `json:"jwt"`
		Scopes    []string `json:"scopes"`
		ExpiresAt string   `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if result.TaskID == "" {
		t.Error("task_id should not be empty")
	}
	if result.JWT == "" {
		t.Error("jwt should not be empty")
	}
	if len(result.Scopes) != 1 || result.Scopes[0] != "github:repo:read" {
		t.Errorf("scopes = %v, want [github:repo:read]", result.Scopes)
	}
	if result.ExpiresAt == "" {
		t.Error("expires_at should not be empty")
	}
}

func TestCreateTaskAPIWithoutAuth(t *testing.T) {
	stack := setupTaskAPIStack(t)

	resp := taskAPIRequest(t, "POST", stack.server.URL+"/api/v1/tasks", "", map[string]any{
		"parent_id": "human:admin@example.com",
		"purpose":   "should fail",
		"scopes":    []string{"github:repo:read"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestCreateTaskAPIAllowsCoordinatorTaskWithoutScopes(t *testing.T) {
	stack := setupTaskAPIStack(t)

	resp := taskAPIRequest(t, "POST", stack.server.URL+"/api/v1/tasks", stack.adminKey, map[string]any{
		"parent_id": "human:admin@example.com",
		"purpose":   "coordination-only task",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	var result struct {
		TaskID string   `json:"task_id"`
		Scopes []string `json:"scopes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if result.TaskID == "" {
		t.Fatal("task_id should not be empty")
	}
	if len(result.Scopes) != 0 {
		t.Errorf("scopes = %v, want empty", result.Scopes)
	}
}

func TestCreateTaskAPIWrongKey(t *testing.T) {
	stack := setupTaskAPIStack(t)

	resp := taskAPIRequest(t, "POST", stack.server.URL+"/api/v1/tasks", "wrong-key", map[string]any{
		"parent_id": "human:admin@example.com",
		"purpose":   "should fail",
		"scopes":    []string{"github:repo:read"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestCreateTaskAPIMissingFields(t *testing.T) {
	stack := setupTaskAPIStack(t)

	// Missing parent_id
	resp := taskAPIRequest(t, "POST", stack.server.URL+"/api/v1/tasks", stack.adminKey, map[string]any{
		"purpose": "missing parent",
		"scopes":  []string{"github:repo:read"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestGetTaskAPI(t *testing.T) {
	stack := setupTaskAPIStack(t)

	// Create a task first
	createResp := taskAPIRequest(t, "POST", stack.server.URL+"/api/v1/tasks", stack.adminKey, map[string]any{
		"parent_id": "human:admin@example.com",
		"purpose":   "test get",
		"scopes":    []string{"github:repo:read"},
		"ttl":       "15m",
		"metadata":  map[string]string{"env": "test"},
	})
	defer createResp.Body.Close()

	var created struct {
		TaskID string `json:"task_id"`
	}
	json.NewDecoder(createResp.Body).Decode(&created)

	// Get the task — strip "task:" prefix for the URL, API should normalize it
	taskUUID := created.TaskID[len("task:"):]
	getResp := taskAPIRequest(t, "GET", stack.server.URL+"/api/v1/tasks/"+taskUUID, stack.adminKey, nil)
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", getResp.StatusCode, http.StatusOK)
	}

	var detail struct {
		TaskID   string            `json:"task_id"`
		ParentID string            `json:"parent_id"`
		Purpose  string            `json:"purpose"`
		Scopes   []string          `json:"scopes"`
		Status   string            `json:"status"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&detail); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if detail.TaskID != created.TaskID {
		t.Errorf("task_id = %q, want %q", detail.TaskID, created.TaskID)
	}
	if detail.ParentID != "human:admin@example.com" {
		t.Errorf("parent_id = %q", detail.ParentID)
	}
	if detail.Purpose != "test get" {
		t.Errorf("purpose = %q", detail.Purpose)
	}
	if detail.Status != "active" {
		t.Errorf("status = %q, want active", detail.Status)
	}
	if detail.Metadata["env"] != "test" {
		t.Errorf("metadata env = %q", detail.Metadata["env"])
	}
}

func TestGetTaskAPINotFound(t *testing.T) {
	stack := setupTaskAPIStack(t)

	resp := taskAPIRequest(t, "GET", stack.server.URL+"/api/v1/tasks/nonexistent-uuid", stack.adminKey, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestRevokeTaskAPI(t *testing.T) {
	stack := setupTaskAPIStack(t)

	// Create a task
	createResp := taskAPIRequest(t, "POST", stack.server.URL+"/api/v1/tasks", stack.adminKey, map[string]any{
		"parent_id": "human:admin@example.com",
		"purpose":   "test revoke",
		"scopes":    []string{"github:repo:read"},
	})
	defer createResp.Body.Close()

	var created struct {
		TaskID string `json:"task_id"`
		JWT    string `json:"jwt"`
	}
	json.NewDecoder(createResp.Body).Decode(&created)

	// Revoke it
	taskUUID := created.TaskID[len("task:"):]
	revokeResp := taskAPIRequest(t, "DELETE", stack.server.URL+"/api/v1/tasks/"+taskUUID+"?reason=security+concern", stack.adminKey, nil)
	defer revokeResp.Body.Close()

	if revokeResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", revokeResp.StatusCode, http.StatusOK)
	}

	var revokeResult struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
	}
	json.NewDecoder(revokeResp.Body).Decode(&revokeResult)

	if revokeResult.Status != "revoked" {
		t.Errorf("status = %q, want revoked", revokeResult.Status)
	}

	// Verify the task shows as revoked (or the JWT is rejected)
	getResp := taskAPIRequest(t, "GET", stack.server.URL+"/api/v1/tasks/"+taskUUID, stack.adminKey, nil)
	defer getResp.Body.Close()

	var detail struct {
		Status string `json:"status"`
	}
	json.NewDecoder(getResp.Body).Decode(&detail)
	if detail.Status != "revoked" {
		t.Errorf("after revocation, status = %q, want revoked", detail.Status)
	}
}

func TestRevokeTaskAPIWithoutAuth(t *testing.T) {
	stack := setupTaskAPIStack(t)

	resp := taskAPIRequest(t, "DELETE", stack.server.URL+"/api/v1/tasks/some-uuid", "", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestRevokeTaskAPINotFound(t *testing.T) {
	stack := setupTaskAPIStack(t)

	resp := taskAPIRequest(t, "DELETE", stack.server.URL+"/api/v1/tasks/missing-uuid", stack.adminKey, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestCreateTaskAPIInvalidTTL(t *testing.T) {
	stack := setupTaskAPIStack(t)

	resp := taskAPIRequest(t, "POST", stack.server.URL+"/api/v1/tasks", stack.adminKey, map[string]any{
		"parent_id": "human:admin@example.com",
		"purpose":   "bad ttl",
		"scopes":    []string{"github:repo:read"},
		"ttl":       "not-a-duration",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestTaskAPIFullLifecycle(t *testing.T) {
	stack := setupTaskAPIStack(t)

	// 1. Create
	createResp := taskAPIRequest(t, "POST", stack.server.URL+"/api/v1/tasks", stack.adminKey, map[string]any{
		"parent_id": "human:operator@example.com",
		"purpose":   "lifecycle test",
		"scopes":    []string{"github:repo:read", "github:pulls:write"},
		"ttl":       "10m",
	})
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status = %d", createResp.StatusCode)
	}
	var created struct {
		TaskID string `json:"task_id"`
		JWT    string `json:"jwt"`
	}
	json.NewDecoder(createResp.Body).Decode(&created)
	createResp.Body.Close()

	// 2. Read
	taskUUID := created.TaskID[len("task:"):]
	getResp := taskAPIRequest(t, "GET", stack.server.URL+"/api/v1/tasks/"+taskUUID, stack.adminKey, nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get: status = %d", getResp.StatusCode)
	}
	var detail struct {
		Status string `json:"status"`
	}
	json.NewDecoder(getResp.Body).Decode(&detail)
	getResp.Body.Close()
	if detail.Status != "active" {
		t.Errorf("status = %q, want active", detail.Status)
	}

	// 3. Use the JWT for an MCP call (proves integration between task API and MCP proxy)
	mcpResp := jsonRPCCall(t, stack.server.URL, created.JWT, "tools/list", nil)
	mcpResult := parseRPCResponse(t, mcpResp)
	if mcpResult.Error != nil {
		t.Fatalf("MCP call with API-created JWT failed: %v", mcpResult.Error)
	}

	// 4. Revoke
	revokeResp := taskAPIRequest(t, "DELETE", stack.server.URL+"/api/v1/tasks/"+taskUUID, stack.adminKey, nil)
	if revokeResp.StatusCode != http.StatusOK {
		t.Fatalf("revoke: status = %d", revokeResp.StatusCode)
	}
	revokeResp.Body.Close()

	// 5. Verify JWT is now rejected by MCP proxy
	mcpResp2 := jsonRPCCall(t, stack.server.URL, created.JWT, "tools/list", nil)
	mcpResult2 := parseRPCResponse(t, mcpResp2)
	if mcpResult2.Error == nil {
		t.Fatal("expected MCP call to fail after revocation")
	}
	if mcpResult2.Error.Code != -32001 {
		t.Errorf("error code = %d, want -32001", mcpResult2.Error.Code)
	}
}

func TestRevokeTaskAPIAuditPreservesDelegationChain(t *testing.T) {
	stack := setupTaskAPIStack(t)

	createResp := taskAPIRequest(t, "POST", stack.server.URL+"/api/v1/tasks", stack.adminKey, map[string]any{
		"parent_id": "human:auditor@example.com",
		"purpose":   "audit chain test",
		"scopes":    []string{"github:repo:read"},
	})
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status = %d", createResp.StatusCode)
	}

	var created struct {
		TaskID string `json:"task_id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	createResp.Body.Close()

	taskUUID := created.TaskID[len("task:"):]
	revokeResp := taskAPIRequest(t, "DELETE", stack.server.URL+"/api/v1/tasks/"+taskUUID, stack.adminKey, nil)
	if revokeResp.StatusCode != http.StatusOK {
		t.Fatalf("revoke: status = %d", revokeResp.StatusCode)
	}
	revokeResp.Body.Close()

	events, err := stack.auditLog.Query(context.Background(), store.EventFilter{
		Event:      audit.EventCredentialRevoked,
		Authorizer: "human:auditor@example.com",
	})
	if err != nil {
		t.Fatalf("query audit events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("revocation audit events = %d, want 1", len(events))
	}
	if len(events[0].DelegationChain) == 0 {
		t.Fatal("revocation audit event should include delegation chain")
	}
}
