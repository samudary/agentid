package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/samudary/agentid/pkg/identity"
	"github.com/samudary/agentid/pkg/store"
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Create, inspect, and revoke task identities",
}

var (
	taskCreateAuthorizer string
	taskCreatePurpose    string
	taskCreateScopes     string
	taskCreateBundle     string
	taskCreateTTL        string
	taskCreateParent     string
)

var taskCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new task identity",
	Long: `Create a new task identity with scoped permissions.

For root tasks, provide --authorizer (e.g. "human:jane@company.com").
For sub-tasks, provide --parent (e.g. "task:01961c88-...").
Scopes can be specified directly with --scopes or via --bundle.`,
	RunE: runTaskCreate,
}

var taskInspectCmd = &cobra.Command{
	Use:   "inspect <task-id>",
	Short: "Show details for a task identity",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskInspect,
}

var taskRevokeReason string

var taskRevokeCmd = &cobra.Command{
	Use:   "revoke <task-id>",
	Short: "Revoke a task identity",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskRevoke,
}

func init() {
	taskCreateCmd.Flags().StringVar(&taskCreateAuthorizer, "authorizer", "", "authorizer identity (e.g. human:jane@company.com)")
	taskCreateCmd.Flags().StringVar(&taskCreatePurpose, "purpose", "", "human-readable purpose")
	taskCreateCmd.Flags().StringVar(&taskCreateScopes, "scopes", "", "comma-separated scopes")
	taskCreateCmd.Flags().StringVar(&taskCreateBundle, "bundle", "", "scope bundle name")
	taskCreateCmd.Flags().StringVar(&taskCreateTTL, "ttl", "30m", "credential TTL (e.g. 30m, 1h)")
	taskCreateCmd.Flags().StringVar(&taskCreateParent, "parent", "", "parent task ID for sub-task delegation")
	taskRevokeCmd.Flags().StringVar(&taskRevokeReason, "reason", "", "reason for revocation")

	taskCmd.AddCommand(taskCreateCmd)
	taskCmd.AddCommand(taskInspectCmd)
	taskCmd.AddCommand(taskRevokeCmd)
	rootCmd.AddCommand(taskCmd)
}

func runTaskCreate(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	svc, st, err := initService(cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	parentID := taskCreateParent
	if parentID == "" {
		if taskCreateAuthorizer == "" {
			return fmt.Errorf("either --authorizer or --parent is required")
		}
		parentID = taskCreateAuthorizer
	}

	ttl, err := time.ParseDuration(taskCreateTTL)
	if err != nil {
		return fmt.Errorf("invalid --ttl: %w", err)
	}

	var scopes []string
	if taskCreateScopes != "" {
		for _, s := range strings.Split(taskCreateScopes, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				scopes = append(scopes, s)
			}
		}
	}

	var bundles []string
	if taskCreateBundle != "" {
		bundles = append(bundles, taskCreateBundle)
	}

	if len(scopes) == 0 && len(bundles) == 0 {
		return fmt.Errorf("either --scopes or --bundle is required")
	}

	ctx := context.Background()
	cred, err := svc.CreateTask(ctx, identity.TaskRequest{
		ParentID: parentID,
		Purpose:  taskCreatePurpose,
		Scopes:   scopes,
		Bundles:  bundles,
		TTL:      ttl,
	})
	if err != nil {
		return fmt.Errorf("create task: %w", err)
	}

	fmt.Println()
	fmt.Println(headerStyle.Render("Task Created"))
	fmt.Println()
	fmt.Printf("  %s %s\n", labelStyle.Render("Task ID:"), monoStyle.Render(cred.TaskID))
	fmt.Printf("  %s %s\n", labelStyle.Render("Expires:"), valueStyle.Render(cred.ExpiresAt.Format(time.RFC3339)))
	fmt.Printf("  %s %s\n", labelStyle.Render("Status:"), successStyle.Render("active"))
	fmt.Println()
	fmt.Printf("  %s\n", labelStyle.Render("Scopes:"))
	for _, s := range cred.Scopes {
		fmt.Printf("    %s %s\n", mutedStyle.Render("-"), valueStyle.Render(s))
	}
	fmt.Println()
	fmt.Printf("  %s\n", labelStyle.Render("Delegation Chain:"))
	for i, link := range cred.Chain {
		marker := mutedStyle.Render(fmt.Sprintf("  %d.", i+1))
		id := monoStyle.Render(link.ID)
		narrowed := ""
		if link.ScopeNarrowed {
			narrowed = warnStyle.Render(" (scope narrowed)")
		}
		fmt.Printf("  %s %s [%s]%s\n", marker, id, link.Type, narrowed)
	}
	fmt.Println()
	fmt.Printf("  %s\n", labelStyle.Render("JWT:"))
	fmt.Printf("  %s\n", monoStyle.Render(cred.JWT))
	fmt.Println()

	return nil
}

func runTaskInspect(cmd *cobra.Command, args []string) error {
	taskID := args[0]

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	_, st, err := initService(cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx := context.Background()
	task, err := st.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("get task: %w", err)
	}

	status := string(task.Status)
	if task.Status == store.TaskStatusActive && time.Now().After(task.ExpiresAt) {
		status = "expired"
	}

	fmt.Println()
	fmt.Println(headerStyle.Render("Task Details"))
	fmt.Println()
	fmt.Printf("  %s %s\n", labelStyle.Render("Task ID:"), monoStyle.Render(task.ID))
	fmt.Printf("  %s %s\n", labelStyle.Render("Parent:"), valueStyle.Render(task.ParentID))
	fmt.Printf("  %s %s\n", labelStyle.Render("Purpose:"), valueStyle.Render(task.Purpose))
	fmt.Printf("  %s %s\n", labelStyle.Render("Status:"), statusColor(status).Render(status))
	if task.StatusReason != "" {
		fmt.Printf("  %s %s\n", labelStyle.Render("Status Reason:"), valueStyle.Render(task.StatusReason))
	}
	fmt.Printf("  %s %s\n", labelStyle.Render("Created:"), valueStyle.Render(task.CreatedAt.Format(time.RFC3339)))
	fmt.Printf("  %s %s\n", labelStyle.Render("Expires:"), valueStyle.Render(task.ExpiresAt.Format(time.RFC3339)))
	if task.CompletedAt != nil {
		fmt.Printf("  %s %s\n", labelStyle.Render("Completed:"), valueStyle.Render(task.CompletedAt.Format(time.RFC3339)))
	}
	fmt.Println()
	fmt.Printf("  %s\n", labelStyle.Render("Scopes:"))
	for _, s := range task.Scopes {
		fmt.Printf("    %s %s\n", mutedStyle.Render("-"), valueStyle.Render(s))
	}
	fmt.Println()
	fmt.Printf("  %s\n", labelStyle.Render("Delegation Chain:"))
	for i, link := range task.DelegationChain {
		marker := mutedStyle.Render(fmt.Sprintf("  %d.", i+1))
		id := monoStyle.Render(link.ID)
		narrowed := ""
		if link.ScopeNarrowed {
			narrowed = warnStyle.Render(" (scope narrowed)")
		}
		fmt.Printf("  %s %s [%s]%s\n", marker, id, link.Type, narrowed)
	}
	if len(task.Metadata) > 0 {
		fmt.Println()
		fmt.Printf("  %s\n", labelStyle.Render("Metadata:"))
		for k, v := range task.Metadata {
			fmt.Printf("    %s %s\n", mutedStyle.Render(k+":"), valueStyle.Render(v))
		}
	}
	fmt.Println()

	return nil
}

func runTaskRevoke(cmd *cobra.Command, args []string) error {
	taskID := args[0]

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	svc, st, err := initService(cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx := context.Background()
	if err := svc.RevokeTask(ctx, taskID, taskRevokeReason); err != nil {
		return fmt.Errorf("revoke task: %w", err)
	}

	fmt.Println()
	fmt.Printf("  %s Task %s has been revoked.\n",
		errorStyle.Render("REVOKED"),
		monoStyle.Render(taskID),
	)
	if taskRevokeReason != "" {
		fmt.Printf("  %s %s\n", labelStyle.Render("Reason:"), valueStyle.Render(taskRevokeReason))
	}
	fmt.Println()

	return nil
}
