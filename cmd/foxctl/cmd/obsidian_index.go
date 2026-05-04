package cmd

import (
	"fmt"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
	"github.com/spf13/cobra"
)

func newObsidianIndexCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Build and query a local Obsidian vault index",
	}
	cmd.AddCommand(
		newObsidianIndexBuildCommand(),
		newObsidianIndexSearchCommand(),
		newObsidianIndexRelatedCommand(),
		newObsidianIndexHealthCommand(),
		newObsidianIndexStatsCommand(),
	)
	return cmd
}

func newObsidianIndexBuildCommand() *cobra.Command {
	var vaultPath string
	var semanticBuild bool
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Rebuild the local vault index",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if vaultPath == "" {
				return fmt.Errorf("--vault-path is required")
			}
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			store, err := obsidianindex.Open(ctx, cfg.Storage.Root, vaultPath)
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()
			result, err := store.Rebuild(ctx, vaultPath)
			if err != nil {
				return err
			}
			var semanticResult map[string]any
			if semanticBuild {
				provider := openObsidianSemanticProvider(cfg)
				if provider == nil {
					return fmt.Errorf("semantic note indexing requires FOXCTL_OBSIDIAN_SEMANTIC_ENABLED and a configured embedding provider")
				}
				noteCount, err := store.EnsureSemanticEmbeddings(ctx, provider)
				if err != nil {
					return err
				}
				chunkCount, err := store.EnsureChunkSemanticEmbeddings(ctx, provider)
				if err != nil {
					return err
				}
				refreshed, err := store.Stats(ctx)
				if err != nil {
					return err
				}
				result.SemanticEmbeddings = refreshed.SemanticEmbeddings
				result.ChunkSemanticEmbeddings = refreshed.ChunkSemanticEmbeddings
				semanticResult = map[string]any{
					"provider_model":            provider.Model(),
					"provider_dimensions":       provider.Dimensions(),
					"notes_embedded":            noteCount,
					"chunks_embedded":           chunkCount,
					"semantic_embeddings":       refreshed.SemanticEmbeddings,
					"chunk_semantic_embeddings": refreshed.ChunkSemanticEmbeddings,
				}
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/index_build", map[string]any{
				"result":   result,
				"semantic": semanticResult,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().BoolVar(&semanticBuild, "semantic", false, "Populate semantic note and chunk embeddings after rebuilding the vault index")
	return cmd
}

func newObsidianIndexSearchCommand() *cobra.Command {
	var vaultPath string
	var query string
	var limit int
	var semanticSearch bool
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search the local vault index",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if vaultPath == "" {
				return fmt.Errorf("--vault-path is required")
			}
			if query == "" {
				return fmt.Errorf("--query is required")
			}
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			store, err := obsidianindex.Open(ctx, cfg.Storage.Root, vaultPath)
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()
			var hits []obsidianindex.SearchHit
			if semanticSearch {
				provider := openObsidianSemanticProvider(cfg)
				if provider == nil {
					return fmt.Errorf("semantic note search requires FOXCTL_OBSIDIAN_SEMANTIC_ENABLED and a configured embedding provider")
				}
				hits, err = store.SearchNotesSemantic(ctx, query, provider, limit)
			} else {
				hits, err = store.SearchNotes(ctx, query, limit)
			}
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/index_search", map[string]any{
				"vault_path": vaultPath,
				"semantic":   semanticSearch,
				"hits":       hits,
				"count":      len(hits),
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().StringVar(&query, "query", "", "Search query")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum result count")
	cmd.Flags().BoolVar(&semanticSearch, "semantic", false, "Use semantic note search from stored note embeddings")
	return cmd
}

func newObsidianIndexRelatedCommand() *cobra.Command {
	var vaultPath string
	var notePath string
	var limit int
	cmd := &cobra.Command{
		Use:   "related",
		Short: "Find related notes from the local vault index",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if vaultPath == "" {
				return fmt.Errorf("--vault-path is required")
			}
			if notePath == "" {
				return fmt.Errorf("--path is required")
			}
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			store, err := obsidianindex.Open(ctx, cfg.Storage.Root, vaultPath)
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()
			hits, err := store.RelatedNotes(ctx, notePath, limit)
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/index_related", map[string]any{
				"vault_path": vaultPath,
				"path":       notePath,
				"hits":       hits,
				"count":      len(hits),
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().StringVar(&notePath, "path", "", "Indexed note path")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum result count")
	return cmd
}

func newObsidianIndexStatsCommand() *cobra.Command {
	var vaultPath string
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show local vault index stats",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if vaultPath == "" {
				return fmt.Errorf("--vault-path is required")
			}
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			store, err := obsidianindex.Open(ctx, cfg.Storage.Root, vaultPath)
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()
			stats, err := store.Stats(ctx)
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/index_stats", map[string]any{
				"vault_path": vaultPath,
				"stats":      stats,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	return cmd
}

func newObsidianIndexHealthCommand() *cobra.Command {
	var vaultPath string
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Show vault health signals from the local index",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if vaultPath == "" {
				return fmt.Errorf("--vault-path is required")
			}
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			store, err := obsidianindex.Open(ctx, cfg.Storage.Root, vaultPath)
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()
			report, err := store.Health(ctx)
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("obsidian/index_health", map[string]any{
				"vault_path": vaultPath,
				"health":     report,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	return cmd
}
