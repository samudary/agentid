package httpclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samudary/agentid/pkg/adapters"
	"github.com/samudary/agentid/pkg/adapters/httpclient"
)

func TestDoSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("accept header = %q", r.Header.Get("Accept"))
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	client := httpclient.New(adapters.UpstreamAuth{
		Type:  adapters.AuthBearer,
		Token: "test-token",
	}, nil)

	result, err := client.Do(context.Background(), http.MethodGet, server.URL+"/test", nil, "")
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

func TestDoWithBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["key"] != "value" {
			t.Errorf("body key = %q, want %q", body["key"], "value")
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"created": "true"})
	}))
	defer server.Close()

	client := httpclient.New(adapters.UpstreamAuth{}, nil)
	result, err := client.Do(context.Background(), http.MethodPost, server.URL, map[string]string{"key": "value"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StatusCode != 201 {
		t.Errorf("status = %d, want 201", result.StatusCode)
	}
}

func TestDoUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	}))
	defer server.Close()

	client := httpclient.New(adapters.UpstreamAuth{}, nil)
	result, err := client.Do(context.Background(), http.MethodGet, server.URL, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for 404")
	}
	if result.StatusCode != 404 {
		t.Errorf("status = %d, want 404", result.StatusCode)
	}
}

func TestDoCustomAcceptHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github.v3+json" {
			t.Errorf("accept = %q", r.Header.Get("Accept"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := httpclient.New(adapters.UpstreamAuth{}, nil)
	_, err := client.Do(context.Background(), http.MethodGet, server.URL, nil, "application/vnd.github.v3+json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDoRawReturnsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "test-value")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("raw body"))
	}))
	defer server.Close()

	client := httpclient.New(adapters.UpstreamAuth{}, nil)
	resp, err := client.DoRaw(context.Background(), http.MethodGet, server.URL, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("X-Custom") != "test-value" {
		t.Errorf("custom header = %q", resp.Header.Get("X-Custom"))
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestDoBasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok {
			t.Error("expected basic auth")
		}
		if user != "admin" || pass != "secret" {
			t.Errorf("basic auth = %s:%s", user, pass)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := httpclient.New(adapters.UpstreamAuth{
		Type:     adapters.AuthBasic,
		Username: "admin",
		Password: "secret",
	}, nil)

	_, err := client.Do(context.Background(), http.MethodGet, server.URL, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
