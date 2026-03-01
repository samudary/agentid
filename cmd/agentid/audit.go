package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	auditpkg "github.com/samudary/agentid/pkg/audit"
	"github.com/samudary/agentid/pkg/store"
	"github.com/samudary/agentid/pkg/store/sqlite"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Query and inspect audit events",
}

var (
	auditQueryTask       string
	auditQuerySince      string
	auditQueryEvent      string
	auditQueryAuthorizer string
	auditQueryLimit      int
)

var auditQueryCmd = &cobra.Command{
	Use:   "query",
	Short: "Query audit events with filters",
	RunE:  runAuditQuery,
}

var auditTailLimit int

var auditTailCmd = &cobra.Command{
	Use:   "tail",
	Short: "Show recent audit events",
	RunE:  runAuditTail,
}

func init() {
	auditQueryCmd.Flags().StringVar(&auditQueryTask, "task", "", "filter by task ID")
	auditQueryCmd.Flags().StringVar(&auditQuerySince, "since", "", "filter events since duration (e.g. 24h, 1h30m)")
	auditQueryCmd.Flags().StringVar(&auditQueryEvent, "event", "", "filter by event type")
	auditQueryCmd.Flags().StringVar(&auditQueryAuthorizer, "authorizer", "", "filter by authorizer in delegation chain")
	auditQueryCmd.Flags().IntVar(&auditQueryLimit, "limit", 100, "maximum events to return")
	auditTailCmd.Flags().IntVar(&auditTailLimit, "limit", 20, "number of recent events to show")

	auditCmd.AddCommand(auditQueryCmd)
	auditCmd.AddCommand(auditTailCmd)
	rootCmd.AddCommand(auditCmd)
}

func runAuditQuery(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	st, err := sqlite.New(cfg.Audit.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	logger := auditpkg.NewLogger(st)

	filter := store.EventFilter{
		TaskID:     auditQueryTask,
		Event:      auditQueryEvent,
		Authorizer: auditQueryAuthorizer,
		Limit:      auditQueryLimit,
	}

	if auditQuerySince != "" {
		dur, err := time.ParseDuration(auditQuerySince)
		if err != nil {
			return fmt.Errorf("invalid --since: %w", err)
		}
		filter.Since = time.Now().Add(-dur)
	}

	ctx := context.Background()
	events, err := logger.Query(ctx, filter)
	if err != nil {
		return fmt.Errorf("query events: %w", err)
	}

	printAuditEvents(events)
	return nil
}

func runAuditTail(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	st, err := sqlite.New(cfg.Audit.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	logger := auditpkg.NewLogger(st)

	ctx := context.Background()
	events, err := logger.Query(ctx, store.EventFilter{
		Limit: auditTailLimit,
	})
	if err != nil {
		return fmt.Errorf("query events: %w", err)
	}

	printAuditEvents(events)
	return nil
}

func printAuditEvents(events []*store.AuditEvent) {
	if len(events) == 0 {
		fmt.Println()
		fmt.Printf("  %s\n", mutedStyle.Render("No audit events found."))
		fmt.Println()
		return
	}

	fmt.Println()
	fmt.Println(headerStyle.Render("Audit Events"))
	fmt.Printf("  %s\n", mutedStyle.Render(fmt.Sprintf("%d event(s)", len(events))))
	fmt.Println()

	// Header row
	tsCol := labelStyle.Width(22).Render("TIMESTAMP")
	eventCol := labelStyle.Width(24).Render("EVENT")
	taskCol := labelStyle.Render("TASK ID")
	fmt.Printf("  %s %s %s\n", tsCol, eventCol, taskCol)
	fmt.Printf("  %s\n", mutedStyle.Render(strings.Repeat("-", 80)))

	for _, evt := range events {
		ts := mutedStyle.Width(22).Render(evt.Timestamp.Format("2006-01-02 15:04:05"))
		style := eventStyleFor(evt.Event)
		eventName := style.Width(24).Render(evt.Event)
		taskID := monoStyle.Render(evt.TaskID)
		fmt.Printf("  %s %s %s\n", ts, eventName, taskID)
	}
	fmt.Println()
}

// eventStyleFor returns a lipgloss.Style appropriate for the event type.
func eventStyleFor(event string) lipgloss.Style {
	switch {
	case strings.Contains(event, "denied") || strings.Contains(event, "revoked") || strings.Contains(event, "failed"):
		return errorStyle
	case strings.Contains(event, "created") || strings.Contains(event, "issued"):
		return successStyle
	case strings.Contains(event, "expired") || strings.Contains(event, "completed"):
		return warnStyle
	default:
		return valueStyle
	}
}
