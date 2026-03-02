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
	"github.com/samudary/agentid/pkg/adapters/github"
	"github.com/samudary/agentid/pkg/audit"
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
		switch name {
		case "github":
			token, _, _, _ := toolCfg.Auth.ResolveAuth()
			authCfg := adapters.UpstreamAuth{
				Type:  adapters.UpstreamAuthType(toolCfg.Auth.Type),
				Token: token,
			}
			adapterList = append(adapterList, github.New(toolCfg.Upstream, authCfg))
		default:
			fmt.Fprintf(os.Stderr, "warning: unknown tool adapter %q, skipping\n", name)
		}
	}

	router := proxy.NewRouter(adapterList)
	server := proxy.NewServer(svc, auditLog, router)

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
