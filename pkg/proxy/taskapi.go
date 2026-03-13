package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/samudary/agentid/pkg/admin"
	"github.com/samudary/agentid/pkg/audit"
	"github.com/samudary/agentid/pkg/identity"
	"github.com/samudary/agentid/pkg/store"
)

// createTaskRequest is the JSON body for POST /api/v1/tasks.
type createTaskRequest struct {
	ParentID string            `json:"parent_id"` // "human:<email>" or "task:<uuid>"
	Purpose  string            `json:"purpose"`
	Scopes   []string          `json:"scopes"`
	Bundles  []string          `json:"bundles,omitempty"`
	TTL      string            `json:"ttl"` // Go duration string, e.g. "30m", "1h"
	Metadata map[string]string `json:"metadata,omitempty"`
}

// createTaskResponse is the JSON response for POST /api/v1/tasks.
type createTaskResponse struct {
	TaskID    string   `json:"task_id"`
	JWT       string   `json:"jwt"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expires_at"` // RFC3339
}

// taskDetailResponse is the JSON response for GET /api/v1/tasks/{id}.
type taskDetailResponse struct {
	TaskID    string            `json:"task_id"`
	ParentID  string            `json:"parent_id"`
	Purpose   string            `json:"purpose"`
	Scopes    []string          `json:"scopes"`
	Status    string            `json:"status"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt string            `json:"created_at"` // RFC3339
	ExpiresAt string            `json:"expires_at"` // RFC3339
}

// RegisterTaskAPI adds the task management REST endpoints to the server's mux.
// These endpoints are protected by the admin auth middleware.
func (s *Server) RegisterTaskAPI(adminAuth admin.Authenticator) {
	mw := admin.Middleware(adminAuth)

	s.mux.Handle("POST /api/v1/tasks", mw(http.HandlerFunc(s.handleCreateTask)))
	s.mux.Handle("GET /api/v1/tasks/{id}", mw(http.HandlerFunc(s.handleGetTask)))
	s.mux.Handle("DELETE /api/v1/tasks/{id}", mw(http.HandlerFunc(s.handleRevokeTask)))
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.ParentID == "" {
		writeJSONError(w, http.StatusBadRequest, "parent_id is required")
		return
	}

	var ttl time.Duration
	if req.TTL != "" {
		var err error
		ttl, err = time.ParseDuration(req.TTL)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid ttl: "+err.Error())
			return
		}
	}

	cred, err := s.identity.CreateTask(r.Context(), identity.TaskRequest{
		ParentID: req.ParentID,
		Purpose:  req.Purpose,
		Scopes:   req.Scopes,
		Bundles:  req.Bundles,
		TTL:      ttl,
		Metadata: req.Metadata,
	})
	if err != nil {
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// Emit audit event for task creation via API
	callerID := admin.CallerID(r.Context())
	s.audit.Emit(r.Context(), audit.EventTaskCreated, cred.TaskID, cred.Chain, map[string]any{
		"created_by": callerID,
		"parent_id":  req.ParentID,
		"purpose":    req.Purpose,
		"scopes":     cred.Scopes,
	})

	writeJSON(w, http.StatusCreated, createTaskResponse{
		TaskID:    cred.TaskID,
		JWT:       cred.JWT,
		Scopes:    cred.Scopes,
		ExpiresAt: cred.ExpiresAt.Format(time.RFC3339),
	})
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	if taskID == "" {
		writeJSONError(w, http.StatusBadRequest, "task ID is required")
		return
	}

	// Normalize: if caller passes bare UUID, prepend "task:" prefix
	if !strings.HasPrefix(taskID, "task:") {
		taskID = "task:" + taskID
	}

	task, err := s.identity.GetTask(r.Context(), taskID)
	if err != nil {
		if errors.Is(err, store.ErrTaskNotFound) {
			writeJSONError(w, http.StatusNotFound, fmt.Sprintf("task not found: %v", err))
			return
		}
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("lookup failed: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, taskDetailResponse{
		TaskID:    task.ID,
		ParentID:  task.ParentID,
		Purpose:   task.Purpose,
		Scopes:    task.Scopes,
		Status:    string(task.Status),
		Metadata:  task.Metadata,
		CreatedAt: task.CreatedAt.Format(time.RFC3339),
		ExpiresAt: task.ExpiresAt.Format(time.RFC3339),
	})
}

func (s *Server) handleRevokeTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	if taskID == "" {
		writeJSONError(w, http.StatusBadRequest, "task ID is required")
		return
	}

	if !strings.HasPrefix(taskID, "task:") {
		taskID = "task:" + taskID
	}

	task, err := s.identity.GetTask(r.Context(), taskID)
	if err != nil {
		if errors.Is(err, store.ErrTaskNotFound) {
			writeJSONError(w, http.StatusNotFound, fmt.Sprintf("revoke failed: %v", err))
			return
		}
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("revoke lookup failed: %v", err))
		return
	}

	// Parse optional reason from query or body
	reason := r.URL.Query().Get("reason")
	if reason == "" {
		reason = "revoked via API"
	}

	callerID := admin.CallerID(r.Context())
	if err := s.identity.RevokeTask(r.Context(), taskID, reason); err != nil {
		if errors.Is(err, store.ErrTaskNotFound) {
			writeJSONError(w, http.StatusNotFound, fmt.Sprintf("revoke failed: %v", err))
			return
		}
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("revoke failed: %v", err))
		return
	}

	// Emit audit event
	s.audit.Emit(r.Context(), audit.EventCredentialRevoked, taskID, task.DelegationChain, map[string]any{
		"revoked_by": callerID,
		"reason":     reason,
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"task_id": taskID,
		"status":  "revoked",
	})
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   http.StatusText(status),
		"message": message,
	})
}
