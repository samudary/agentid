package main

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/samudary/agentid/pkg/config"
	"github.com/samudary/agentid/pkg/identity"
	"github.com/samudary/agentid/pkg/store/sqlite"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("12"))

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Width(22)

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15"))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("2"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("1"))

	warnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("3"))

	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))

	monoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("14"))
)

// statusColor returns the appropriate style for a task status.
func statusColor(status string) lipgloss.Style {
	switch status {
	case "active":
		return successStyle
	case "revoked":
		return errorStyle
	case "failed":
		return errorStyle
	case "expired":
		return warnStyle
	case "completed":
		return warnStyle
	default:
		return valueStyle
	}
}

// loadConfig loads and returns the config, or exits with an error message.
func loadConfig() (*config.Config, error) {
	if cfgFile == "" {
		return nil, fmt.Errorf("--config flag is required")
	}
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

// initService initializes the SQLite store, generates a key pair, and creates
// an identity service from the given config. Returns a cleanup function that
// closes the store.
func initService(cfg *config.Config) (*identity.Service, *sqlite.SQLiteStore, error) {
	st, err := sqlite.New(cfg.Audit.DBPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open store: %w", err)
	}

	kp, err := identity.GenerateKeyPair()
	if err != nil {
		st.Close()
		return nil, nil, fmt.Errorf("generate key pair: %w", err)
	}

	maxTTL, err := cfg.MaxTTLDuration()
	if err != nil {
		st.Close()
		return nil, nil, fmt.Errorf("parse max TTL: %w", err)
	}

	svc := identity.NewService(st, kp, identity.ServiceConfig{
		MaxTTL:             maxTTL,
		MaxDelegationDepth: cfg.Identity.MaxDelegationDepth,
		Bundles:            cfg.ScopeBundles,
	})

	return svc, st, nil
}
