package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/samudary/agentid/pkg/adapters"
	_ "github.com/samudary/agentid/pkg/adapters/github" // register github adapter
	"github.com/samudary/agentid/pkg/adapters/rest"
	"github.com/samudary/agentid/pkg/admin"
	"github.com/samudary/agentid/pkg/audit"
	"github.com/samudary/agentid/pkg/config"
	"github.com/samudary/agentid/pkg/proxy"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the AgentID MCP proxy server",
	RunE:  runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	svc, st, err := initService(cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	auditLog := audit.NewLogger(st)

	var adapterList []adapters.Adapter
	for name, toolCfg := range cfg.Tools {
		adapter, err := buildAdapter(name, toolCfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v, skipping\n", err)
			continue
		}
		adapterList = append(adapterList, adapter)
	}

	router := proxy.NewRouter(adapterList)
	server := proxy.NewServer(svc, auditLog, router)

	// Register task management API if admin auth is configured
	adminKey := cfg.Admin.Auth.ResolveAdminKey()
	if adminKey != "" {
		adminAuth, err := admin.NewAPIKeyAuth(adminKey)
		if err != nil {
			return fmt.Errorf("configure admin auth: %w", err)
		}
		server.RegisterTaskAPI(adminAuth)
		fmt.Printf("  Task API:    enabled (POST/GET/DELETE /api/v1/tasks)\n")
	} else {
		keyEnvHint := cfg.Admin.Auth.KeyEnv
		if keyEnvHint == "" {
			keyEnvHint = "admin.auth.key_env in config"
		}
		fmt.Printf("  Task API:    disabled (set %s to enable)\n", keyEnvHint)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: server,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		fmt.Println(headerStyle.Render("AgentID MCP Proxy"))
		fmt.Printf("  Listening on %s\n", monoStyle.Render(addr))
		fmt.Printf("  Tools:       %d adapter(s) registered\n", len(adapterList))
		fmt.Printf("  Audit DB:    %s\n", cfg.Audit.DBPath)
		fmt.Println()
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	fmt.Println("\nShutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	fmt.Println("Server stopped.")
	return nil
}

func buildAdapter(name string, toolCfg config.ToolConfig) (adapters.Adapter, error) {
	token, username, password, headerValue := toolCfg.Auth.ResolveAuth()
	authCfg := adapters.UpstreamAuth{
		Type:        adapters.UpstreamAuthType(toolCfg.Auth.Type),
		Token:       token,
		Username:    username,
		Password:    password,
		HeaderName:  toolCfg.Auth.HeaderName,
		HeaderValue: headerValue,
	}

	adapterType := toolCfg.Type
	if adapterType == "" {
		adapterType = name
	}

	if adapterType == "rest" {
		return rest.New(name, toolCfg.Upstream, authCfg, toolCfg.Operations)
	}

	factory, err := adapters.Lookup(adapterType)
	if err != nil {
		return nil, err
	}

	adapter, err := factory(toolCfg.Upstream, authCfg)
	if err != nil {
		return nil, fmt.Errorf("create adapter %q: %w", name, err)
	}

	return adapter, nil
}
