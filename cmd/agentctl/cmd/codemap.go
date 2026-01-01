package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
)

func newCodemapCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "codemap",
		Short: "Generate and manage semantic codemaps",
		Long: `Semantic codemaps show how different parts of a codebase connect.

A codemap contains:
- Traces: Execution paths or relationships
- Annotations: Specific code locations with explanations
- ASCII trees: Visual representation of connections

Use codemaps to understand complex codebases, trace data flows,
and document architectural relationships.`,
	}
	cmd.AddCommand(
		newCodemapGenerateCommand(),
		newCodemapListCommand(),
		newCodemapShowCommand(),
		newCodemapDeleteCommand(),
		newCodemapSearchCommand(),
	)
	return cmd
}

func newCodemapGenerateCommand() *cobra.Command {
	var workspace string
	var depth int
	var timeout string

	cmd := &cobra.Command{
		Use:   "generate <query>",
		Short: "Generate a semantic codemap for a query",
		Long: `Generate a semantic codemap that answers a natural language query.

The agent explores the codebase, gathering context from:
- Dependency graph (imports, calls, references)
- Code symbols (functions, types, methods)
- Pattern matching (ripgrep search)

Depth controls exploration thoroughness:
  1 = Quick (~5 tool calls)
  2 = Standard (~10 tool calls, default)
  3 = Detailed (~15 tool calls)
  4 = Deep (~25 tool calls)
  5 = Exhaustive (~40 tool calls)

Examples:
  agentctl codemap generate "how does authentication connect to database"
  agentctl codemap generate "trace the request handling flow" --depth 3
  agentctl codemap generate "what files share the logging package" --timeout 5m`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			payload := map[string]any{
				"query":          query,
				"depth":          depth,
				"correlation_id": ulid.Make().String(),
				"cli_command":    cmd.CommandPath(),
			}
			if workspace != "" {
				payload["workspace"] = workspace
			}
			return runCodemapSkillWithTimeout(cmd, "codemap/generate", payload, timeout)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path (defaults to current directory)")
	cmd.Flags().IntVar(&depth, "depth", 2, "Exploration depth: 1=quick, 2=standard, 3=detailed, 4=deep, 5=exhaustive")
	cmd.Flags().StringVar(&timeout, "timeout", "5m", "Maximum execution time (e.g., 30s, 2m, 5m)")
	return cmd
}

func newCodemapListCommand() *cobra.Command {
	var workspace string
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stored codemaps",
		Long: `List all codemaps stored for the workspace.

Codemaps are stored in the memory store with type="codemap".

Examples:
  agentctl codemap list
  agentctl codemap list --limit 20
  agentctl codemap list --workspace /path/to/project`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			// Resolve workspace
			workspacePath := workspace
			var err error
			if workspacePath == "" {
				workspacePath, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("get working directory: %w", err)
				}
			}
			workspacePath, err = filepath.Abs(workspacePath)
			if err != nil {
				return fmt.Errorf("resolve workspace: %w", err)
			}

			// Open memory store
			agentctlHome := os.Getenv("AGENTCTL_HOME")
			if agentctlHome == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("get home directory: %w", err)
				}
				agentctlHome = filepath.Join(home, ".agentctl")
			}
			storageRoot := filepath.Join(agentctlHome, "storage")
			casRoot := filepath.Join(agentctlHome, "cas")

			memStore, err := memory.Open(ctx, storageRoot, casRoot)
			if err != nil {
				return fmt.Errorf("open memory store: %w", err)
			}
			defer memStore.Close()

			// List all entries and filter by type=codemap
			allEntries, err := memStore.List(ctx, workspacePath, limit*5) // Fetch more to filter
			if err != nil {
				return fmt.Errorf("list entries: %w", err)
			}

			// Filter to only codemap entries
			var entries []memory.NamedEntry
			for _, entry := range allEntries {
				if entry.Type == "codemap" {
					entries = append(entries, entry)
					if len(entries) >= limit {
						break
					}
				}
			}

			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No codemaps found. Generate one with: agentctl codemap generate \"<query>\"")
				return nil
			}

			// Output as formatted list
			fmt.Fprintf(cmd.OutOrStdout(), "Found %d codemap(s):\n\n", len(entries))
			for i, entry := range entries {
				// Extract title and query from result JSON
				var data map[string]any
				title := entry.Name
				query := ""
				fileCount := 0
				if entry.Result != nil {
					if json.Unmarshal(entry.Result, &data) == nil {
						if t, ok := data["title"].(string); ok {
							title = t
						}
						if q, ok := data["query"].(string); ok {
							query = q
						}
						if fc, ok := data["file_count"].(float64); ok {
							fileCount = int(fc)
						}
					}
				}

				fmt.Fprintf(cmd.OutOrStdout(), "%d. %s\n", i+1, title)
				fmt.Fprintf(cmd.OutOrStdout(), "   ID: %s\n", entry.Name)
				if query != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "   Query: %s\n", truncateString(query, 60))
				}
				if fileCount > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "   Files: %d\n", fileCount)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "   Created: %s\n", entry.CreatedAt.Format("2006-01-02 15:04:05"))
				fmt.Fprintln(cmd.OutOrStdout())
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path (defaults to current directory)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of codemaps to list")
	return cmd
}

func newCodemapShowCommand() *cobra.Command {
	var workspace string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show a stored codemap",
		Long: `Display a stored codemap by ID.

The codemap is rendered with traces, annotations, and ASCII trees.

Examples:
  agentctl codemap show my-codemap-id
  agentctl codemap show my-codemap-id --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			codemapID := args[0]
			ctx := cmd.Context()

			// Resolve workspace
			workspacePath := workspace
			var err error
			if workspacePath == "" {
				workspacePath, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("get working directory: %w", err)
				}
			}
			workspacePath, err = filepath.Abs(workspacePath)
			if err != nil {
				return fmt.Errorf("resolve workspace: %w", err)
			}

			// Open memory store
			agentctlHome := os.Getenv("AGENTCTL_HOME")
			if agentctlHome == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("get home directory: %w", err)
				}
				agentctlHome = filepath.Join(home, ".agentctl")
			}
			storageRoot := filepath.Join(agentctlHome, "storage")
			casRoot := filepath.Join(agentctlHome, "cas")

			memStore, err := memory.Open(ctx, storageRoot, casRoot)
			if err != nil {
				return fmt.Errorf("open memory store: %w", err)
			}
			defer memStore.Close()

			// Fetch the codemap
			entry, err := memStore.Get(ctx, codemapID, workspacePath)
			if err != nil {
				return fmt.Errorf("get codemap: %w", err)
			}

			if jsonOutput {
				// Output raw JSON
				if entry.Result != nil {
					fmt.Fprintln(cmd.OutOrStdout(), string(entry.Result))
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "{}")
				}
				return nil
			}

			// Parse and render formatted output
			return renderCodemap(cmd, entry.Result)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path (defaults to current directory)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output raw JSON")
	return cmd
}

func newCodemapDeleteCommand() *cobra.Command {
	var workspace string

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a stored codemap",
		Long: `Delete a codemap by ID.

Examples:
  agentctl codemap delete my-codemap-id`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			codemapID := args[0]
			ctx := cmd.Context()

			// Resolve workspace
			workspacePath := workspace
			var err error
			if workspacePath == "" {
				workspacePath, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("get working directory: %w", err)
				}
			}
			workspacePath, err = filepath.Abs(workspacePath)
			if err != nil {
				return fmt.Errorf("resolve workspace: %w", err)
			}

			// Open memory store
			agentctlHome := os.Getenv("AGENTCTL_HOME")
			if agentctlHome == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("get home directory: %w", err)
				}
				agentctlHome = filepath.Join(home, ".agentctl")
			}
			storageRoot := filepath.Join(agentctlHome, "storage")
			casRoot := filepath.Join(agentctlHome, "cas")

			memStore, err := memory.Open(ctx, storageRoot, casRoot)
			if err != nil {
				return fmt.Errorf("open memory store: %w", err)
			}
			defer memStore.Close()

			// Delete the codemap
			if err := memStore.Delete(ctx, codemapID, workspacePath); err != nil {
				return fmt.Errorf("delete codemap: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted codemap: %s\n", codemapID)
			return nil
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path (defaults to current directory)")
	return cmd
}

func newCodemapSearchCommand() *cobra.Command {
	var workspace string
	var limit int

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search codemaps using semantic similarity",
		Long: `Search stored codemaps using semantic similarity.

This uses the code/semantic_search skill with scope=codemaps.

Examples:
  agentctl codemap search "authentication flow"
  agentctl codemap search "database connections" --limit 5`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			payload := map[string]any{
				"query":          query,
				"scope":          []string{"codemaps"},
				"limit":          limit,
				"correlation_id": ulid.Make().String(),
				"cli_command":    cmd.CommandPath(),
			}
			if workspace != "" {
				payload["workspace"] = workspace
			}
			return runCodemapSkill(cmd, "code/semantic_search", payload)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path (defaults to current directory)")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum number of results")
	return cmd
}

func runCodemapSkill(cmd *cobra.Command, skillName string, payload map[string]any) error {
	return runCodemapSkillWithTimeout(cmd, skillName, payload, "")
}

func runCodemapSkillWithTimeout(cmd *cobra.Command, skillName string, payload map[string]any, timeout string) error {
	cfg, err := config.Load(cmd.Context())
	if err != nil {
		return err
	}
	_, err = findSkill(cfg, skillName)
	if err != nil {
		return fmt.Errorf("%s skill not found (run make skills-install): %w", skillName, err)
	}
	input, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	runCmd := newRunCommand()
	runCmd.SetContext(cmd.Context())
	runCmd.SetOut(cmd.OutOrStdout())
	runCmd.SetErr(cmd.ErrOrStderr())
	// Silence usage/errors since we're calling this programmatically
	runCmd.SilenceUsage = true
	runCmd.SilenceErrors = true
	// Skill name must come first as the positional arg, then flags
	args := []string{skillName, "--input", string(input)}
	if timeout != "" {
		args = append(args, "--timeout", timeout)
	}
	runCmd.SetArgs(args)
	return runCmd.Execute()
}

func renderCodemap(cmd *cobra.Command, data []byte) error {
	if data == nil {
		return fmt.Errorf("codemap has no data")
	}

	var codemap struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Query       string `json:"query"`
		FileCount   int    `json:"file_count"`
		SymbolCount int    `json:"symbol_count"`
		Traces      []struct {
			Number      int    `json:"number"`
			Title       string `json:"title"`
			Summary     string `json:"summary"`
			Tree        string `json:"tree"`
			Annotations []struct {
				Label       string `json:"label"`
				Title       string `json:"title"`
				Description string `json:"description"`
				Path        string `json:"path"`
			} `json:"annotations"`
		} `json:"traces"`
	}

	if err := json.Unmarshal(data, &codemap); err != nil {
		return fmt.Errorf("parse codemap: %w", err)
	}

	out := cmd.OutOrStdout()

	// Header
	fmt.Fprintf(out, "# %s\n\n", codemap.Title)
	if codemap.Description != "" {
		fmt.Fprintf(out, "%s\n\n", codemap.Description)
	}
	fmt.Fprintf(out, "**Query:** %s\n", codemap.Query)
	fmt.Fprintf(out, "**Files:** %d | **Symbols:** %d\n\n", codemap.FileCount, codemap.SymbolCount)

	// Traces
	for _, trace := range codemap.Traces {
		fmt.Fprintf(out, "## Trace %d: %s\n\n", trace.Number, trace.Title)
		if trace.Summary != "" {
			fmt.Fprintf(out, "%s\n\n", trace.Summary)
		}
		if trace.Tree != "" {
			fmt.Fprintf(out, "```\n%s\n```\n\n", trace.Tree)
		}

		// Annotations
		if len(trace.Annotations) > 0 {
			fmt.Fprintln(out, "### Annotations")
			for _, ann := range trace.Annotations {
				fmt.Fprintf(out, "**[%s] %s**\n", ann.Label, ann.Title)
				fmt.Fprintf(out, "  %s\n", ann.Description)
				fmt.Fprintf(out, "  `%s`\n\n", ann.Path)
			}
		}
	}

	return nil
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func init() {
	rootCmd.AddCommand(newCodemapCommand())
}
