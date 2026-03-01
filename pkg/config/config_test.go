package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "configs", "example-gateway.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "0.0.0.0")
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 8080)
	}

	if cfg.Identity.MaxTTL != "60m" {
		t.Errorf("Identity.MaxTTL = %q, want %q", cfg.Identity.MaxTTL, "60m")
	}
	if cfg.Identity.MaxDelegationDepth != 5 {
		t.Errorf("Identity.MaxDelegationDepth = %d, want %d", cfg.Identity.MaxDelegationDepth, 5)
	}

	if cfg.Audit.DBPath != "agentid.db" {
		t.Errorf("Audit.DBPath = %q, want %q", cfg.Audit.DBPath, "agentid.db")
	}

	if len(cfg.ScopeBundles) != 2 {
		t.Fatalf("ScopeBundles count = %d, want 2", len(cfg.ScopeBundles))
	}
	contributor, ok := cfg.ScopeBundles["code-contributor"]
	if !ok {
		t.Fatal("missing scope bundle: code-contributor")
	}
	if len(contributor.Scopes) != 4 {
		t.Errorf("code-contributor scopes count = %d, want 4", len(contributor.Scopes))
	}

	reader, ok := cfg.ScopeBundles["code-reader"]
	if !ok {
		t.Fatal("missing scope bundle: code-reader")
	}
	if len(reader.Scopes) != 2 {
		t.Errorf("code-reader scopes count = %d, want 2", len(reader.Scopes))
	}

	if len(cfg.Tools) != 1 {
		t.Fatalf("Tools count = %d, want 1", len(cfg.Tools))
	}
	gh, ok := cfg.Tools["github"]
	if !ok {
		t.Fatal("missing tool: github")
	}
	if gh.Upstream != "https://api.github.com" {
		t.Errorf("github upstream = %q, want %q", gh.Upstream, "https://api.github.com")
	}
	if gh.Auth.Type != "bearer_token" {
		t.Errorf("github auth type = %q, want %q", gh.Auth.Type, "bearer_token")
	}
	if gh.Auth.TokenEnv != "GITHUB_TOKEN" {
		t.Errorf("github auth token_env = %q, want %q", gh.Auth.TokenEnv, "GITHUB_TOKEN")
	}
	if len(gh.Operations) != 4 {
		t.Errorf("github operations count = %d, want 4", len(gh.Operations))
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	// Create a minimal config with just the server host set.
	dir := t.TempDir()
	minimal := filepath.Join(dir, "minimal.yaml")
	if err := os.WriteFile(minimal, []byte("server:\n  host: localhost\n"), 0644); err != nil {
		t.Fatalf("write minimal config: %v", err)
	}

	cfg, err := Load(minimal)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Port != 8080 {
		t.Errorf("default Server.Port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Identity.MaxTTL != "60m" {
		t.Errorf("default Identity.MaxTTL = %q, want %q", cfg.Identity.MaxTTL, "60m")
	}
	if cfg.Identity.MaxDelegationDepth != 5 {
		t.Errorf("default Identity.MaxDelegationDepth = %d, want 5", cfg.Identity.MaxDelegationDepth)
	}
	if cfg.Audit.DBPath != "agentid.db" {
		t.Errorf("default Audit.DBPath = %q, want %q", cfg.Audit.DBPath, "agentid.db")
	}
}

func TestMaxTTLDuration(t *testing.T) {
	cfg := &Config{Identity: IdentityConfig{MaxTTL: "60m"}}
	d, err := cfg.MaxTTLDuration()
	if err != nil {
		t.Fatalf("MaxTTLDuration: %v", err)
	}
	if d != 60*time.Minute {
		t.Errorf("MaxTTLDuration = %v, want %v", d, 60*time.Minute)
	}
}

func TestMaxTTLDurationInvalid(t *testing.T) {
	cfg := &Config{Identity: IdentityConfig{MaxTTL: "invalid"}}
	_, err := cfg.MaxTTLDuration()
	if err == nil {
		t.Error("expected error for invalid duration, got nil")
	}
}

func TestResolveAuth(t *testing.T) {
	const envKey = "AGENTID_TEST_TOKEN_XYZ"
	const envVal = "secret-token-123"

	t.Setenv(envKey, envVal)

	auth := AuthConfig{
		Type:     "bearer_token",
		TokenEnv: envKey,
	}

	token, username, password, headerValue := auth.ResolveAuth()
	if token != envVal {
		t.Errorf("resolved token = %q, want %q", token, envVal)
	}
	if username != "" {
		t.Errorf("resolved username = %q, want empty", username)
	}
	if password != "" {
		t.Errorf("resolved password = %q, want empty", password)
	}
	if headerValue != "" {
		t.Errorf("resolved headerValue = %q, want empty", headerValue)
	}
}

func TestResolveAuthUnset(t *testing.T) {
	auth := AuthConfig{
		Type:     "bearer_token",
		TokenEnv: "AGENTID_DEFINITELY_NOT_SET_12345",
	}
	token, _, _, _ := auth.ResolveAuth()
	if token != "" {
		t.Errorf("expected empty token for unset env var, got %q", token)
	}
}
