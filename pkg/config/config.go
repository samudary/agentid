package config

import (
	"fmt"
	"os"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	"github.com/samudary/agentid/pkg/identity"
)

// Config is the top-level configuration for the AgentID gateway.
type Config struct {
	Server       ServerConfig                     `koanf:"server"`
	Identity     IdentityConfig                   `koanf:"identity"`
	Tools        map[string]ToolConfig            `koanf:"tools"`
	ScopeBundles map[string]identity.BundleConfig `koanf:"scope_bundles"`
	Audit        AuditConfig                      `koanf:"audit"`
}

// ServerConfig defines the HTTP listener settings.
type ServerConfig struct {
	Host string `koanf:"host"`
	Port int    `koanf:"port"`
}

// IdentityConfig controls task identity behavior.
type IdentityConfig struct {
	MaxTTL             string `koanf:"max_ttl"`
	MaxDelegationDepth int    `koanf:"max_delegation_depth"`
	KeyFile            string `koanf:"key_file"`
}

// ToolConfig defines an upstream tool service and its operations.
type ToolConfig struct {
	Upstream   string            `koanf:"upstream"`
	Auth       AuthConfig        `koanf:"auth"`
	Operations []OperationConfig `koanf:"operations"`
}

// AuthConfig holds the auth strategy and environment variable references
// for resolving credentials at runtime.
type AuthConfig struct {
	Type        string `koanf:"type"`
	TokenEnv    string `koanf:"token_env"`
	UsernameEnv string `koanf:"username_env"`
	PasswordEnv string `koanf:"password_env"`
	HeaderName  string `koanf:"header_name"`
	ValueEnv    string `koanf:"value_env"`
}

// OperationConfig maps a named operation to an HTTP method, path, and scope.
type OperationConfig struct {
	Name   string `koanf:"name"`
	Scope  string `koanf:"scope"`
	Method string `koanf:"method"`
	Path   string `koanf:"path"`
}

// AuditConfig controls the audit log storage.
type AuditConfig struct {
	DBPath string `koanf:"db_path"`
}

// Load reads a YAML config file and returns a validated Config.
func Load(path string) (*Config, error) {
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("load config %q: %w", path, err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Identity.MaxTTL == "" {
		cfg.Identity.MaxTTL = "60m"
	}
	if cfg.Identity.MaxDelegationDepth == 0 {
		cfg.Identity.MaxDelegationDepth = 5
	}
	if cfg.Audit.DBPath == "" {
		cfg.Audit.DBPath = "agentid.db"
	}

	return &cfg, nil
}

// MaxTTLDuration parses the MaxTTL string into a time.Duration.
func (c *Config) MaxTTLDuration() (time.Duration, error) {
	return time.ParseDuration(c.Identity.MaxTTL)
}

// ResolveAuth resolves environment variable references in auth config
// to actual credential values.
func (a *AuthConfig) ResolveAuth() (resolvedToken, resolvedUsername, resolvedPassword, resolvedHeaderValue string) {
	if a.TokenEnv != "" {
		resolvedToken = os.Getenv(a.TokenEnv)
	}
	if a.UsernameEnv != "" {
		resolvedUsername = os.Getenv(a.UsernameEnv)
	}
	if a.PasswordEnv != "" {
		resolvedPassword = os.Getenv(a.PasswordEnv)
	}
	if a.ValueEnv != "" {
		resolvedHeaderValue = os.Getenv(a.ValueEnv)
	}
	return
}
