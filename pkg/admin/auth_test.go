package admin_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samudary/agentid/pkg/admin"
)

func TestNewAPIKeyAuthRejectsEmpty(t *testing.T) {
	_, err := admin.NewAPIKeyAuth("")
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestAPIKeyAuthSuccess(t *testing.T) {
	auth, err := admin.NewAPIKeyAuth("test-admin-key-12345")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer test-admin-key-12345")

	callerID, err := auth.Authenticate(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callerID != "api-key:test-adm" {
		t.Errorf("callerID = %q, want %q", callerID, "api-key:test-adm")
	}
}

func TestAPIKeyAuthWrongKey(t *testing.T) {
	auth, err := admin.NewAPIKeyAuth("correct-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")

	_, err = auth.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for wrong key")
	}
}

func TestAPIKeyAuthMissingHeader(t *testing.T) {
	auth, err := admin.NewAPIKeyAuth("some-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	_, err = auth.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for missing header")
	}
}

func TestAPIKeyAuthBadScheme(t *testing.T) {
	auth, err := admin.NewAPIKeyAuth("some-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

	_, err = auth.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for non-Bearer scheme")
	}
}

func TestMiddlewareBlocksUnauthenticated(t *testing.T) {
	auth, _ := admin.NewAPIKeyAuth("secret-key")
	handler := admin.Middleware(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMiddlewarePassesAuthenticated(t *testing.T) {
	auth, _ := admin.NewAPIKeyAuth("secret-key")

	var gotCallerID string
	handler := admin.Middleware(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCallerID = admin.CallerID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer secret-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotCallerID != "api-key:secret-k" {
		t.Errorf("callerID = %q, want %q", gotCallerID, "api-key:secret-k")
	}
}

func TestCallerIDFromContextWithoutMiddleware(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	callerID := admin.CallerID(req.Context())
	if callerID != "" {
		t.Errorf("expected empty callerID without middleware, got %q", callerID)
	}
}

func TestAPIKeyAuthShortKey(t *testing.T) {
	// Keys shorter than 8 chars should still work, using full key as prefix
	auth, err := admin.NewAPIKeyAuth("abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer abc")

	callerID, err := auth.Authenticate(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callerID != "api-key:abc" {
		t.Errorf("callerID = %q, want %q", callerID, "api-key:abc")
	}
}
