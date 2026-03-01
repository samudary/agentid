package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/samudary/agentid/pkg/identity"
)

var scopesCmd = &cobra.Command{
	Use:   "scopes",
	Short: "Inspect scope bundles defined in the config",
}

var scopesListBundlesCmd = &cobra.Command{
	Use:   "list-bundles",
	Short: "List all scope bundles",
	RunE:  runScopesListBundles,
}

var scopesExpandBundleCmd = &cobra.Command{
	Use:   "expand-bundle <bundle-name>",
	Short: "Show the expanded scopes for a bundle",
	Args:  cobra.ExactArgs(1),
	RunE:  runScopesExpandBundle,
}

func init() {
	scopesCmd.AddCommand(scopesListBundlesCmd)
	scopesCmd.AddCommand(scopesExpandBundleCmd)
	rootCmd.AddCommand(scopesCmd)
}

func runScopesListBundles(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if len(cfg.ScopeBundles) == 0 {
		fmt.Println(mutedStyle.Render("No scope bundles defined."))
		return nil
	}

	// Sort bundle names for deterministic output
	names := make([]string, 0, len(cfg.ScopeBundles))
	for name := range cfg.ScopeBundles {
		names = append(names, name)
	}
	sort.Strings(names)

	// Find the longest bundle name for alignment
	maxNameLen := 0
	for _, name := range names {
		if len(name) > maxNameLen {
			maxNameLen = len(name)
		}
	}

	fmt.Println()
	fmt.Println(headerStyle.Render("Scope Bundles"))
	fmt.Println()

	// Header row
	nameCol := labelStyle.Width(maxNameLen + 2).Render("BUNDLE")
	scopesCol := labelStyle.Width(8).Render("SCOPES")
	descCol := labelStyle.Render("DESCRIPTION")
	fmt.Printf("  %s %s %s\n", nameCol, scopesCol, descCol)

	fmt.Printf("  %s\n", mutedStyle.Render(strings.Repeat("-", maxNameLen+2+8+40)))

	for _, name := range names {
		bundle := cfg.ScopeBundles[name]
		nameStr := monoStyle.Width(maxNameLen + 2).Render(name)
		countStr := valueStyle.Width(8).Render(fmt.Sprintf("%d", len(bundle.Scopes)))
		descStr := valueStyle.Render(bundle.Description)
		fmt.Printf("  %s %s %s\n", nameStr, countStr, descStr)
	}
	fmt.Println()

	return nil
}

func runScopesExpandBundle(cmd *cobra.Command, args []string) error {
	bundleName := args[0]

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if _, ok := cfg.ScopeBundles[bundleName]; !ok {
		return fmt.Errorf("unknown bundle: %q", bundleName)
	}

	scopes, err := identity.ResolveBundles([]string{bundleName}, cfg.ScopeBundles)
	if err != nil {
		return fmt.Errorf("resolve bundle: %w", err)
	}

	bundle := cfg.ScopeBundles[bundleName]

	fmt.Println()
	fmt.Printf("  %s %s\n", headerStyle.Render("Bundle:"), monoStyle.Render(bundleName))
	fmt.Printf("  %s %s\n", labelStyle.Render("Description:"), valueStyle.Render(bundle.Description))
	fmt.Println()
	fmt.Printf("  %s\n", labelStyle.Render("Expanded Scopes:"))
	for _, s := range scopes {
		fmt.Printf("    %s %s\n", mutedStyle.Render("-"), valueStyle.Render(s))
	}
	fmt.Println()

	return nil
}
