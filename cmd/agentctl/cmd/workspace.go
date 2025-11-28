package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/knowledge/builtin"
	"github.com/spf13/cobra"
)

func newWorkspaceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Manage workspace configuration",
		Long: `Initialize and manage workspace configuration for Claude Code integration.

This command helps set up .claude/ directories with agents, commands, and hooks.`,
	}
	cmd.AddCommand(
		newWorkspaceInitCommand(),
		newWorkspaceListAgentsCommand(),
	)
	return cmd
}

func newWorkspaceInitCommand() *cobra.Command {
	var (
		workspaceDir string
		agentSets    string
		force        bool
		dryRun       bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a workspace with Claude Code configuration",
		Long: `Initialize a workspace with .claude/ directory structure including agents, commands, and hooks.

Agent sets:
  core     - Essential agents (code-reviewer, debugger, refactorer, test-automator)
  factory  - Factory droids (orchestrator, backend-architect, frontend-developer)
  all      - All available builtin agents

Examples:
  agentctl workspace init                    # Initialize with core agents
  agentctl workspace init --agents all       # Initialize with all agents
  agentctl workspace init --agents core,factory  # Initialize with specific sets
  agentctl workspace init --dry-run          # Preview what would be created`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Determine workspace root
			if workspaceDir == "" {
				var err error
				workspaceDir, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("get working directory: %w", err)
				}
			}

			// Parse agent sets
			sets := parseAgentSets(agentSets)

			// Collect agents to install
			agents, err := collectAgents(sets)
			if err != nil {
				return fmt.Errorf("collect agents: %w", err)
			}

			// Initialize workspace
			result, err := initWorkspace(workspaceDir, agents, force, dryRun)
			if err != nil {
				return fmt.Errorf("init workspace: %w", err)
			}

			// Emit JSON envelope
			env := envelope.OK("workspace/init", result, envelope.WithMeta(envelope.Meta{
				Source: "cli",
			}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspaceDir, "workspace", "", "Workspace root directory (default: current directory)")
	cmd.Flags().StringVar(&agentSets, "agents", "core", "Agent sets to install (core, factory, all)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing files")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without writing files")
	return cmd
}

func newWorkspaceListAgentsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-agents",
		Short: "List available builtin agents",
		RunE: func(cmd *cobra.Command, _ []string) error {
			droids, err := builtin.ListFactoryDroids()
			if err != nil {
				return fmt.Errorf("list factory droids: %w", err)
			}

			agents, err := builtin.ListCoreAgents()
			if err != nil {
				return fmt.Errorf("list core agents: %w", err)
			}

			type agentInfo struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Set         string `json:"set"`
			}

			var all []agentInfo
			for _, d := range droids {
				all = append(all, agentInfo{
					Name:        d.Name,
					Description: d.Description,
					Set:         "factory",
				})
			}
			for _, a := range agents {
				all = append(all, agentInfo{
					Name:        a.Name,
					Description: a.Description,
					Set:         "core",
				})
			}

			data := map[string]any{
				"agents": all,
				"count":  len(all),
				"sets": map[string]int{
					"factory": len(droids),
					"core":    len(agents),
				},
			}

			env := envelope.OK("workspace/list-agents", data, envelope.WithMeta(envelope.Meta{
				Source: "cli",
			}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}
	return cmd
}

// parseAgentSets parses a comma-separated list of agent set names.
func parseAgentSets(s string) []string {
	if s == "" {
		return []string{"core"}
	}
	parts := strings.Split(s, ",")
	var sets []string
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			sets = append(sets, p)
		}
	}
	return sets
}

// collectAgents collects all agents matching the specified sets.
func collectAgents(sets []string) ([]builtin.Asset, error) {
	var agents []builtin.Asset

	includeCore := false
	includeFactory := false

	for _, set := range sets {
		switch set {
		case "all":
			includeCore = true
			includeFactory = true
		case "core":
			includeCore = true
		case "factory":
			includeFactory = true
		default:
			return nil, fmt.Errorf("unknown agent set: %s (valid: core, factory, all)", set)
		}
	}

	if includeFactory {
		droids, err := builtin.ListFactoryDroids()
		if err != nil {
			return nil, err
		}
		agents = append(agents, droids...)
	}

	if includeCore {
		coreAgents, err := builtin.ListCoreAgents()
		if err != nil {
			return nil, err
		}
		agents = append(agents, coreAgents...)
	}

	return agents, nil
}

// initWorkspace initializes a workspace with the specified agents.
func initWorkspace(workspaceDir string, agents []builtin.Asset, force, dryRun bool) (map[string]any, error) {
	claudeDir := filepath.Join(workspaceDir, ".claude")
	agentsDir := filepath.Join(claudeDir, "agents")

	created := make([]string, 0, len(agents))
	skipped := make([]string, 0, len(agents))
	errors := []string{}

	// Create directories
	if !dryRun {
		if err := os.MkdirAll(agentsDir, 0755); err != nil {
			return nil, fmt.Errorf("create agents directory: %w", err)
		}
	}

	// Write agent files
	for _, agent := range agents {
		// Extract filename from agent name (e.g., "core/agent/code-reviewer" -> "code-reviewer.md")
		parts := strings.Split(agent.Name, "/")
		filename := parts[len(parts)-1] + ".md"
		destPath := filepath.Join(agentsDir, filename)

		// Check if file exists
		if _, err := os.Stat(destPath); err == nil && !force {
			skipped = append(skipped, filename)
			continue
		}

		if dryRun {
			created = append(created, filename)
			continue
		}

		// Write file
		if err := os.WriteFile(destPath, []byte(agent.Body), 0644); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", filename, err))
			continue
		}
		created = append(created, filename)
	}

	result := map[string]any{
		"workspace":    workspaceDir,
		"agents_dir":   agentsDir,
		"created":      created,
		"skipped":      skipped,
		"errors":       errors,
		"dry_run":      dryRun,
		"total_agents": len(agents),
		"summary": fmt.Sprintf("Created %d agents, skipped %d, errors %d",
			len(created), len(skipped), len(errors)),
	}

	if len(errors) > 0 {
		return result, fmt.Errorf("failed to write %d agent file(s)", len(errors))
	}
	return result, nil
}

func init() {
	rootCmd.AddCommand(newWorkspaceCommand())
}
