package cmd

import (
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	memorystore "github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/spf13/cobra"
)

func newContextMotifsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "motifs",
		Short: "Build and inspect deterministic repo motif artifacts",
	}
	cmd.AddCommand(
		newContextMotifsBuildCommand(),
		newContextMotifsSearchCommand(),
	)
	return cmd
}

func newContextMotifsBuildCommand() *cobra.Command {
	var workspacePath string
	var maxSeeds int
	var maxMotifs int
	var depth int
	var budget int
	var perNodeCap int
	var maxRelated int
	var withEmbeddings bool
	var includeTests bool
	var includeImports bool

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build deterministic repo motif artifacts into workspace memory",
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

			repo, err := repoindex.Open(ctx, cfg.Storage.Root, target)
			if err != nil {
				return err
			}
			defer func() { _ = repo.Close() }()

			var provider semantic.EmbeddingProvider
			if withEmbeddings {
				provider, _ = semantic.NewProviderForScope(
					semantic.ScopeMemory,
					cfg,
					semantic.WithVoyageKey(os.Getenv("VOYAGE_API_KEY")),
					semantic.WithGeminiKey(os.Getenv("GEMINI_API_KEY")),
				)
			}

			motifs, err := contextplane.BuildRepoMotifArtifacts(ctx, target, repo, memStore, provider, contextplane.RepoMotifBuildOptions{
				MaxSeeds:       maxSeeds,
				MaxMotifs:      maxMotifs,
				Depth:          depth,
				Budget:         budget,
				PerNodeCap:     perNodeCap,
				MaxRelated:     maxRelated,
				IncludeTests:   includeTests,
				IncludeImports: includeImports,
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/motifs_build", map[string]any{
				"workspace_path": target,
				"motifs":         motifs,
				"count":          len(motifs),
				"embedded":       provider != nil,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().IntVar(&maxSeeds, "max-seeds", 300, "Maximum repo file seeds to inspect")
	cmd.Flags().IntVar(&maxMotifs, "max-motifs", 150, "Maximum motif artifacts to persist")
	cmd.Flags().IntVar(&depth, "depth", 2, "Repo graph expansion depth")
	cmd.Flags().IntVar(&budget, "budget", 30, "Repo graph expansion budget per seed")
	cmd.Flags().IntVar(&perNodeCap, "per-node-cap", 20, "Maximum edges fetched per expanded node")
	cmd.Flags().IntVar(&maxRelated, "max-related", 3, "Maximum related files recorded per motif")
	cmd.Flags().BoolVar(&withEmbeddings, "with-embeddings", true, "Embed motif summaries when an embedding provider is configured")
	cmd.Flags().BoolVar(&includeTests, "include-tests", false, "Include test-file motifs")
	cmd.Flags().BoolVar(&includeImports, "include-imports", true, "Include import edges when building motifs")
	return cmd
}

func newContextMotifsSearchCommand() *cobra.Command {
	var workspacePath string
	var query string
	var limit int
	var semanticSearch bool

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search built repo motif artifacts",
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

			hits, err := contextplane.SearchRepoMotifArtifacts(ctx, target, query, limit, memStore, provider)
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/motifs_search", map[string]any{
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
