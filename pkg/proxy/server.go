package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/samudary/agentid/pkg/audit"
	"github.com/samudary/agentid/pkg/identity"
)

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      any             `json:"id"`
}

type jsonRPCResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
	ID      any       `json:"id"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Server is the MCP proxy HTTP server.
type Server struct {
	identity *identity.Service
	audit    *audit.Logger
	router   *Router
	mux      *http.ServeMux
}

// NewServer creates a new MCP proxy server.
func NewServer(identitySvc *identity.Service, auditLog *audit.Logger, router *Router) *Server {
	s := &Server{
		identity: identitySvc,
		audit:    auditLog,
		router:   router,
		mux:      http.NewServeMux(),
	}
	s.mux.HandleFunc("POST /mcp", s.handleMCP)
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	// 1. Extract JWT from Authorization header
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeRPCError(w, nil, -32000, "missing or invalid Authorization header")
		return
	}
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")

	// 2. Validate credential
	claims, err := s.identity.ValidateCredential(r.Context(), tokenString)
	if err != nil {
		writeRPCError(w, nil, -32001, fmt.Sprintf("authentication failed: %v", err))
		return
	}

	// 3. Parse JSON-RPC request
	var req jsonRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, nil, -32700, "parse error")
		return
	}

	if req.JSONRPC != "2.0" {
		writeRPCError(w, req.ID, -32600, "invalid JSON-RPC version")
		return
	}

	// 4. Route by method
	switch req.Method {
	case "initialize":
		s.handleInitialize(w, req)
	case "notifications/initialized":
		// JSON-RPC notification — no response required.
		return
	case "tools/list":
		s.handleToolsList(w, req, claims)
	case "tools/call":
		s.handleToolsCall(w, r.Context(), req, claims)
	default:
		writeRPCError(w, req.ID, -32601, fmt.Sprintf("method not found: %q", req.Method))
	}
}

func (s *Server) handleInitialize(w http.ResponseWriter, req jsonRPCRequest) {
	writeRPCResult(w, req.ID, map[string]any{
		"protocolVersion": "2025-11-25",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "agentid",
			"version": "0.1.0",
		},
	})
}

func (s *Server) handleToolsList(w http.ResponseWriter, req jsonRPCRequest, _ *identity.TaskClaims) {
	tools := s.router.AllTools()
	writeRPCResult(w, req.ID, map[string]any{"tools": tools})
}

func (s *Server) handleToolsCall(w http.ResponseWriter, ctx context.Context, req jsonRPCRequest, claims *identity.TaskClaims) {
	// Parse params
	var params toolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(w, req.ID, -32602, "invalid params")
		return
	}

	// Resolve tool
	adapter, requiredScope, err := s.router.Resolve(params.Name)
	if err != nil {
		writeRPCError(w, req.ID, -32602, err.Error())
		return
	}

	// Check scope
	if !hasScope(claims.Scopes, requiredScope) {
		// Emit scope denied audit event
		s.audit.Emit(ctx, audit.EventScopeDenied, claims.Subject, claims.DelegationChain, map[string]any{
			"tool":           params.Name,
			"required_scope": requiredScope,
			"task_scopes":    claims.Scopes,
		})
		writeRPCError(w, req.ID, -32003, fmt.Sprintf("insufficient scope: requires %q", requiredScope))
		return
	}

	// Invoke tool
	result, err := adapter.Invoke(ctx, params.Name, params.Arguments)
	if err != nil {
		s.audit.Emit(ctx, audit.EventToolInvoked, claims.Subject, claims.DelegationChain, map[string]any{
			"tool":   params.Name,
			"result": "error",
			"error":  err.Error(),
		})
		writeRPCError(w, req.ID, -32000, fmt.Sprintf("tool invocation failed: %v", err))
		return
	}

	// Emit success audit event
	s.audit.Emit(ctx, audit.EventToolInvoked, claims.Subject, claims.DelegationChain, map[string]any{
		"tool":   params.Name,
		"result": "success",
	})

	writeRPCResult(w, req.ID, result)
}

// hasScope checks if the task's scopes include the required scope.
// Uses the same narrowing logic: the task scope must cover the required scope.
func hasScope(taskScopes []string, required string) bool {
	for _, s := range taskScopes {
		if identity.Narrows(required, s) {
			return true
		}
	}
	return false
}

func writeRPCResult(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jsonRPCResponse{
		JSONRPC: "2.0",
		Result:  result,
		ID:      id,
	})
}

func writeRPCError(w http.ResponseWriter, id any, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	status := http.StatusOK // JSON-RPC errors are still 200 OK
	if code == -32000 || code == -32001 {
		status = http.StatusUnauthorized
	}
	if code == -32003 {
		status = http.StatusForbidden
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(jsonRPCResponse{
		JSONRPC: "2.0",
		Error:   &rpcError{Code: code, Message: message},
		ID:      id,
	})
}
