package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newSearchCommand())
}

func newSearchCommand() *cobra.Command {
	var scopes []string
	var limit int
	var summarize bool
	var workspace string

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Semantic search across code, sessions, memories, and tasks",
		Long: `Unified semantic search using vector embeddings.

Searches across multiple scopes:
  - symbols:  Code symbols (functions, types) from indexed files
  - sessions: Past Claude Code session summaries
  - memories: Named memories and gotchas
  - tasks:    Task descriptions and notes

Examples:
  # Search all scopes
  agentctl search "authentication flow"

  # Search only code symbols
  agentctl search "error handling" --scope symbols

  # Search with summarization (requires OpenRouter API key)
  agentctl search "database migrations" --summarize

  # Limit results per scope
  agentctl search "API endpoints" --limit 5`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]

			// Build input for code/semantic_search skill
			input := map[string]any{
				"query":     query,
				"limit":     limit,
				"summarize": summarize,
			}

			if len(scopes) > 0 {
				input["scopes"] = scopes
			} else {
				// Default to all scopes
				input["scopes"] = []string{"symbols", "sessions", "memories", "tasks"}
			}

			if workspace != "" {
				input["workspace"] = workspace
			}

			inputBytes, err := json.Marshal(input)
			if err != nil {
				return fmt.Errorf("marshal input: %w", err)
			}

			// Use the run command to execute the skill
			runCmd := newRunCommand()
			runCmd.SetContext(cmd.Context())
			runCmd.SetOut(cmd.OutOrStdout())
			runCmd.SetErr(cmd.ErrOrStderr())
			runCmd.SetArgs([]string{"--input", string(inputBytes), "code/semantic_search"})
			return runCmd.Execute()
		},
	}

	cmd.Flags().StringSliceVar(&scopes, "scope", nil, "Scopes to search (symbols, sessions, memories, tasks)")
	cmd.Flags().IntVar(&limit, "limit", 10, "Max results per scope")
	cmd.Flags().BoolVar(&summarize, "summarize", false, "Generate AI summary of results (requires OPENROUTER_API_KEY)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path (default: current directory)")

	return cmd
}
