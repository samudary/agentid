// Package admin provides authentication for the AgentID control plane API
// (task management endpoints). This is separate from the MCP proxy auth
// which uses task-scoped JWTs.
//
// The default implementation uses a static API key configured in the gateway
// YAML. The Authenticator interface allows swapping in more sophisticated
// schemes (admin JWTs, mTLS client certs, etc.) without changing the HTTP
// layer.
package admin

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
)

// Authenticator verifies that an incoming HTTP request is authorized to
// access the admin API. Implementations should return the caller identity
// (used for audit trails) or an error.
type Authenticator interface {
	// Authenticate inspects the request and returns a caller identity string
	// (e.g. "api-key:abc1" for key prefix, or "admin:alice@example.com" for
	// named admins). Returns an error if the request is not authenticated.
	Authenticate(r *http.Request) (callerID string, err error)
}

// APIKeyAuth authenticates requests using a static API key passed in the
// Authorization header as "Bearer <key>". The key is configured in the
// gateway YAML and resolved from an environment variable at startup.
type APIKeyAuth struct {
	key []byte // the expected API key
}

// NewAPIKeyAuth creates an API key authenticator. Returns an error if the
// key is empty, which prevents misconfigured gateways from running with
// an open admin API.
func NewAPIKeyAuth(key string) (*APIKeyAuth, error) {
	if key == "" {
		return nil, fmt.Errorf("admin API key must not be empty")
	}
	return &APIKeyAuth{key: []byte(key)}, nil
}

// Authenticate checks the Authorization header for a valid Bearer token
// matching the configured API key. Uses constant-time comparison to avoid
// timing attacks.
func (a *APIKeyAuth) Authenticate(r *http.Request) (string, error) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return "", fmt.Errorf("missing or invalid Authorization header")
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	if subtle.ConstantTimeCompare([]byte(token), a.key) != 1 {
		return "", fmt.Errorf("invalid API key")
	}

	// Return a caller ID using the first 8 chars of the key as a prefix
	// for audit traceability without exposing the full key.
	prefix := token
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	return "api-key:" + prefix, nil
}

// Middleware returns an HTTP middleware that enforces admin authentication.
// Unauthenticated requests receive a 401 response. Authenticated requests
// have the caller ID stored in the request context.
func Middleware(auth Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callerID, err := auth.Authenticate(r)
			if err != nil {
				http.Error(w, `{"error":"unauthorized","message":"`+err.Error()+`"}`, http.StatusUnauthorized)
				return
			}
			ctx := withCallerID(r.Context(), callerID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
