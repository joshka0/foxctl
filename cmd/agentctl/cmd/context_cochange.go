package cmd

import (
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	memorystore "github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/spf13/cobra"
)

func newContextCoChangeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cochange",
		Short: "Build and inspect ACA co-change cluster artifacts",
	}
	cmd.AddCommand(
		newContextCoChangeBuildCommand(),
		newContextCoChangeSearchCommand(),
	)
	return cmd
}

func newContextCoChangeBuildCommand() *cobra.Command {
	var workspacePath string
	var commitLimit int
	var maxFilesPerCommit int
	var halfLifeDays int
	var maxClusters int
	var maxNeighbors int
	var withEmbeddings bool

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build co-change cluster artifacts into workspace memory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx, config.WithWorkspacePath(target))
			if err != nil {
				return err
			}
			memStore, err := memorystore.OpenWithConfig(ctx, cfg)
			if err != nil {
				return err
			}
			defer func() { _ = memStore.Close() }()

			var provider semantic.EmbeddingProvider
			if withEmbeddings {
				provider, _ = semantic.NewProviderForScope(
					semantic.ScopeMemory,
					cfg,
					semantic.WithVoyageKey(os.Getenv("VOYAGE_API_KEY")),
					semantic.WithGeminiKey(os.Getenv("GEMINI_API_KEY")),
				)
			}

			clusters, err := contextplane.BuildCoChangeArtifacts(ctx, target, memStore, provider, contextplane.CoChangeArtifactBuildOptions{
				CommitLimit:       commitLimit,
				MaxFilesPerCommit: maxFilesPerCommit,
				HalfLifeDays:      halfLifeDays,
				MaxClusters:       maxClusters,
				MaxNeighbors:      maxNeighbors,
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/cochange_build", map[string]any{
				"workspace_path": target,
				"clusters":       clusters,
				"count":          len(clusters),
				"embedded":       provider != nil,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().IntVar(&commitLimit, "commit-limit", 80, "Maximum recent commits to inspect")
	cmd.Flags().IntVar(&maxFilesPerCommit, "max-files-per-commit", 20, "Ignore commits touching more than this many files")
	cmd.Flags().IntVar(&halfLifeDays, "half-life-days", 90, "Recency half-life in days for co-change weighting")
	cmd.Flags().IntVar(&maxClusters, "max-clusters", 25, "Maximum cluster artifacts to build")
	cmd.Flags().IntVar(&maxNeighbors, "max-neighbors", 6, "Maximum co-change neighbors per cluster")
	cmd.Flags().BoolVar(&withEmbeddings, "with-embeddings", true, "Embed the cluster summaries when an embedding provider is configured")
	return cmd
}

func newContextCoChangeSearchCommand() *cobra.Command {
	var workspacePath string
	var query string
	var limit int
	var semanticSearch bool

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search built co-change cluster artifacts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx, config.WithWorkspacePath(target))
			if err != nil {
				return err
			}
			memStore, err := memorystore.OpenWithConfig(ctx, cfg)
			if err != nil {
				return err
			}
			defer func() { _ = memStore.Close() }()

			var provider semantic.EmbeddingProvider
			if semanticSearch {
				provider, _ = semantic.NewProviderForScope(
					semantic.ScopeMemory,
					cfg,
					semantic.WithVoyageKey(os.Getenv("VOYAGE_API_KEY")),
					semantic.WithGeminiKey(os.Getenv("GEMINI_API_KEY")),
				)
			}

			hits, err := contextplane.SearchCoChangeArtifacts(ctx, target, query, limit, memStore, provider)
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/cochange_search", map[string]any{
				"workspace_path": target,
				"query":          strings.TrimSpace(query),
				"hits":           hits,
				"count":          len(hits),
				"semantic":       provider != nil,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&query, "query", "", "Search query")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum result count")
	cmd.Flags().BoolVar(&semanticSearch, "semantic", true, "Use semantic search when an embedding provider is configured")
	return cmd
}
