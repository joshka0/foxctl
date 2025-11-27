package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/knowledge"
	"github.com/spf13/cobra"
)

func newKnowledgeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "knowledge",
		Short: "Manage the knowledge registry (packs, agents, commands)",
		Long: `The knowledge registry indexes Claude-facing knowledge packs, agents, and commands.

Knowledge packs are documentation bundles in docs/knowledge/ that provide domain guidance.
Agents are prompt profiles in .claude/agents/ for multi-step work.
Commands are prompt templates in .claude/commands/ for structured workflows.

This is distinct from agentctl skills, which are executable Go/WASI/exec plugins.`,
	}
	cmd.AddCommand(
		newKnowledgeSyncCommand(),
		newKnowledgeListCommand(),
		newKnowledgeSearchCommand(),
	)
	return cmd
}

func newKnowledgeSyncCommand() *cobra.Command {
	var workspaceDir string

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Index knowledge from the filesystem into SQLite",
		Long: `Walks docs/knowledge/, .claude/agents/, and .claude/commands/ and populates
the knowledge registry with items, triggers, and documents.

Triggers are extracted from:
- docs/knowledge/skill-rules.json (keywords, intent patterns, path patterns)
- Agent/command descriptions (keywords)`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			cfg, err := config.Load(ctx)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Determine workspace root
			if workspaceDir == "" {
				workspaceDir, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("get working directory: %w", err)
				}
			}

			// Open knowledge store
			store, err := knowledge.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return fmt.Errorf("open knowledge store: %w", err)
			}
			defer func() { _ = store.Close() }()

			// Run sync
			opts := knowledge.DefaultSyncOptions(workspaceDir)
			result, err := knowledge.Sync(ctx, store, opts)
			if err != nil {
				return fmt.Errorf("sync: %w", err)
			}

			// Emit JSON envelope
			data := map[string]any{
				"sync_result": result,
				"summary": fmt.Sprintf("Synced %d packs, %d agents, %d commands (%d triggers, %d documents)",
					result.PacksAdded+result.PacksUpdated,
					result.AgentsAdded+result.AgentsUpdated,
					result.CommandsAdded+result.CommandsUpdated,
					result.TriggersAdded,
					result.DocumentsAdded),
			}

			env := envelope.OK("knowledge/sync", data, envelope.WithMeta(envelope.Meta{
				Source: "cli",
			}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspaceDir, "workspace", "", "Workspace root directory (default: current directory)")
	return cmd
}

func newKnowledgeListCommand() *cobra.Command {
	var kindFilter string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all knowledge items",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			cfg, err := config.Load(ctx)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			store, err := knowledge.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return fmt.Errorf("open knowledge store: %w", err)
			}
			defer func() { _ = store.Close() }()

			var items []knowledge.Item
			if kindFilter != "" {
				kind := knowledge.ItemKind(kindFilter)
				items, err = store.ListItems(ctx, kind)
			} else {
				items, err = store.ListAllItems(ctx)
			}
			if err != nil {
				return fmt.Errorf("list items: %w", err)
			}

			// Convert to output format
			type itemOutput struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				Kind        string `json:"kind"`
				Description string `json:"description"`
				SourcePath  string `json:"source_path"`
				Priority    string `json:"priority"`
			}

			output := make([]itemOutput, len(items))
			for i, item := range items {
				output[i] = itemOutput{
					ID:          item.ID,
					Name:        item.Name,
					Kind:        string(item.Kind),
					Description: item.Description,
					SourcePath:  item.SourcePath,
					Priority:    item.Priority,
				}
			}

			data := map[string]any{
				"items": output,
				"count": len(output),
			}

			env := envelope.OK("knowledge/list", data, envelope.WithMeta(envelope.Meta{
				Source: "cli",
			}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&kindFilter, "kind", "", "Filter by kind (pack, agent, command)")
	return cmd
}

func newKnowledgeSearchCommand() *cobra.Command {
	var query string
	var filePath string

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search knowledge by query or file path",
		Long: `Search the knowledge registry using keywords or file path matching.

Examples:
  agentctl knowledge search --query "frontend component"
  agentctl knowledge search --path "src/components/Button.tsx"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			if query == "" && filePath == "" {
				return fmt.Errorf("either --query or --path is required")
			}

			cfg, err := config.Load(ctx)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			store, err := knowledge.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return fmt.Errorf("open knowledge store: %w", err)
			}
			defer func() { _ = store.Close() }()

			var items []knowledge.Item

			if query != "" {
				// Extract keywords from query
				keywords := extractQueryKeywords(query)
				items, err = store.MatchByKeyword(ctx, keywords)
				if err != nil {
					return fmt.Errorf("search by keyword: %w", err)
				}
			}

			if filePath != "" {
				pathItems, pathErr := store.MatchByPath(ctx, filePath)
				if pathErr != nil {
					return fmt.Errorf("search by path: %w", pathErr)
				}
				// Merge results (dedupe by ID)
				seen := make(map[string]bool)
				for _, item := range items {
					seen[item.ID] = true
				}
				for _, item := range pathItems {
					if !seen[item.ID] {
						items = append(items, item)
						seen[item.ID] = true
					}
				}
			}

			// Convert to output format
			type matchOutput struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				Kind        string `json:"kind"`
				Description string `json:"description"`
				SourcePath  string `json:"source_path"`
			}

			output := make([]matchOutput, len(items))
			for i, item := range items {
				output[i] = matchOutput{
					ID:          item.ID,
					Name:        item.Name,
					Kind:        string(item.Kind),
					Description: item.Description,
					SourcePath:  item.SourcePath,
				}
			}

			data := map[string]any{
				"matches": output,
				"count":   len(output),
				"query":   query,
				"path":    filePath,
			}

			env := envelope.OK("knowledge/search", data, envelope.WithMeta(envelope.Meta{
				Source: "cli",
			}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&query, "query", "", "Search query (keywords)")
	cmd.Flags().StringVar(&filePath, "path", "", "File path to match against path triggers")
	return cmd
}

func extractQueryKeywords(query string) []string {
	// Simple keyword extraction: split on whitespace, lowercase, filter short words
	words := strings.Fields(strings.ToLower(query))
	var keywords []string
	for _, w := range words {
		// Remove punctuation
		w = strings.Trim(w, ".,!?;:'\"")
		if len(w) > 2 {
			keywords = append(keywords, w)
		}
	}
	return keywords
}

func init() {
	rootCmd.AddCommand(newKnowledgeCommand())
}
