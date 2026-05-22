package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/platform/config"
	wsutil "github.com/joshka0/foxctl/internal/platform/workspace"
	memorystore "github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/spf13/cobra"
)

const defaultContextMechanismMaxSymbols = 10000

type mechanismCandidateFilter struct {
	SymbolID   string
	Name       string
	File       string
	FilePrefix string
}

type mechanismCandidateView struct {
	SymbolID          string                             `json:"symbol_id"`
	NodeID            string                             `json:"node_id"`
	Name              string                             `json:"name,omitempty"`
	File              string                             `json:"file,omitempty"`
	Domain            string                             `json:"domain"`
	Summary           string                             `json:"summary"`
	MechanismTags     []string                           `json:"mechanism_tags,omitempty"`
	AbstractSchema    string                             `json:"abstract_schema,omitempty"`
	StructuralShape   contextplane.MemoryStructuralShape `json:"structural_shape"`
	LiteralDims       int                                `json:"literal_dims"`
	StructuralDims    int                                `json:"structural_dims"`
	SourceRefs        []string                           `json:"source_refs,omitempty"`
	LiteralVector     []float32                          `json:"literal_vector,omitempty"`
	StructuralVector  []float32                          `json:"structural_vector,omitempty"`
	CollisionScore    float64                            `json:"collision_score,omitempty"`
	LiteralSimilarity float64                            `json:"literal_similarity,omitempty"`
	StructSimilarity  float64                            `json:"structural_similarity,omitempty"`
}

func newContextMechanismsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mechanisms",
		Short: "Inspect blurred mechanism projections and structural collisions",
	}
	cmd.AddCommand(
		newContextMechanismsRepoSymbolsCommand(),
		newContextMechanismsCollisionCacheCommand(),
	)
	return cmd
}

func newContextMechanismsRepoSymbolsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo-symbols",
		Short: "Project embedded repo symbols into blurred mechanism memories",
	}
	cmd.AddCommand(
		newContextMechanismsRepoSymbolsPreviewCommand(),
		newContextMechanismsRepoSymbolsCollideCommand(),
		newContextMechanismsRepoSymbolsPersistCommand(),
		newContextMechanismsRepoSymbolsCollideMemoryCommand(),
		newContextMechanismsRepoSymbolsCollideMemoryAgentsCommand(),
		newContextMechanismsRepoSymbolsBlurAgentCommand(),
	)
	return cmd
}

func newContextMechanismsRepoSymbolsPreviewCommand() *cobra.Command {
	var workspacePath string
	var workspaceID string
	var maxSymbols int
	var perNodeCap int
	var limit int
	var filter mechanismCandidateFilter
	var includeSchema bool
	var includeVectors bool

	cmd := &cobra.Command{
		Use:   "preview",
		Short: "Preview blurred mechanism candidates from repoindex + symbol embeddings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx, config.WithWorkspacePath(target))
			if err != nil {
				return err
			}

			result, resolvedWorkspaceID, err := buildRepoSymbolMechanismsForCommand(ctx, cfg, target, workspaceID, maxSymbols, perNodeCap)
			if err != nil {
				return err
			}
			filtered := filterMechanismCandidates(result.Candidates, filter)
			views := mechanismCandidateViews(filtered, limit, includeSchema, includeVectors)
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/mechanisms_repo_symbols_preview", map[string]any{
				"workspace_path":       target,
				"workspace_id":         resolvedWorkspaceID,
				"candidates_total":     len(result.Candidates),
				"candidates_filtered":  len(filtered),
				"skipped_unembedded":   result.SkippedUnembedded,
				"skipped_invalid":      result.SkippedInvalid,
				"max_symbols":          maxSymbols,
				"limit":                limit,
				"candidates":           views,
				"read_only":            true,
				"structural_vectoring": "graph_shape",
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	addContextMechanismRepoSymbolFlags(cmd, &workspacePath, &workspaceID, &maxSymbols, &perNodeCap)
	addContextMechanismFilterFlags(cmd, &filter)
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum candidates to include in output")
	cmd.Flags().BoolVar(&includeSchema, "include-schema", true, "Include blurred abstract schema text")
	cmd.Flags().BoolVar(&includeVectors, "include-vectors", false, "Include literal and structural vectors in output")
	return cmd
}

func newContextMechanismsRepoSymbolsCollideCommand() *cobra.Command {
	var workspacePath string
	var workspaceID string
	var maxSymbols int
	var perNodeCap int
	var limit int
	var filter mechanismCandidateFilter
	var query mechanismCandidateFilter
	var entropy float64
	var threshold float64
	var includeSameDomain bool
	var includeSchema bool
	var includeVectors bool

	cmd := &cobra.Command{
		Use:   "collide",
		Short: "Run structural collision planning over blurred repo-symbol mechanisms",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx, config.WithWorkspacePath(target))
			if err != nil {
				return err
			}

			result, resolvedWorkspaceID, err := buildRepoSymbolMechanismsForCommand(ctx, cfg, target, workspaceID, maxSymbols, perNodeCap)
			if err != nil {
				return err
			}
			corpus := filterMechanismCandidates(result.Candidates, filter)
			queryCandidate, ok := selectMechanismQueryCandidate(corpus, query)
			if !ok {
				return fmt.Errorf("query symbol not found in mechanism candidate corpus; increase --max-symbols or adjust query selector")
			}

			memories := make([]contextplane.MechanismMemory, 0, len(corpus)-1)
			for _, candidate := range corpus {
				if candidate.Projection.ID == queryCandidate.Projection.ID {
					continue
				}
				memories = append(memories, candidate.MechanismMemory())
			}
			plan := contextplane.PlanMemoryCollisionCells(contextplane.MemoryCollisionInput{
				WorkspaceID: resolvedWorkspaceID,
				Query: contextplane.MechanismQuery{
					ID:               queryCandidate.Projection.ID,
					Domain:           queryCandidate.Projection.OriginalDomain,
					Text:             queryCandidate.Projection.Summary,
					AbstractSchema:   queryCandidate.Projection.AbstractSchema,
					LiteralVector:    queryCandidate.LiteralVector,
					StructuralVector: queryCandidate.StructuralVector,
					SourceRefs:       queryCandidate.Projection.SourceRefs,
				},
				Memories:          memories,
				Entropy:           entropy,
				Threshold:         threshold,
				Limit:             limit,
				IncludeSameDomain: includeSameDomain,
			})

			collisions := mechanismCollisionViews(plan.Cells, corpus, includeSchema, includeVectors)
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/mechanisms_repo_symbols_collide", map[string]any{
				"workspace_path":      target,
				"workspace_id":        resolvedWorkspaceID,
				"query":               mechanismCandidateViewFor(queryCandidate, includeSchema, includeVectors),
				"collisions":          collisions,
				"collision_count":     len(collisions),
				"candidate_count":     len(corpus),
				"skipped":             plan.Skipped,
				"skipped_unembedded":  result.SkippedUnembedded,
				"skipped_invalid":     result.SkippedInvalid,
				"entropy":             entropy,
				"threshold":           threshold,
				"include_same_domain": includeSameDomain,
				"read_only":           true,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	addContextMechanismRepoSymbolFlags(cmd, &workspacePath, &workspaceID, &maxSymbols, &perNodeCap)
	addContextMechanismFilterFlags(cmd, &filter)
	cmd.Flags().StringVar(&query.SymbolID, "query-symbol-id", "", "Query symbol ID; accepts repoindex node ID, raw symbol key, or embedding symbol ID")
	cmd.Flags().StringVar(&query.Name, "query-name", "", "Exact query symbol name")
	cmd.Flags().StringVar(&query.File, "query-file", "", "Exact query symbol file")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum collision cells to include in output")
	cmd.Flags().Float64Var(&entropy, "entropy", 0.35, "Literal-similarity penalty and structural boost factor")
	cmd.Flags().Float64Var(&threshold, "threshold", 0.70, "Minimum collision score")
	cmd.Flags().BoolVar(&includeSameDomain, "include-same-domain", false, "Allow same-domain collisions")
	cmd.Flags().BoolVar(&includeSchema, "include-schema", true, "Include blurred abstract schema text")
	cmd.Flags().BoolVar(&includeVectors, "include-vectors", false, "Include literal and structural vectors in output")
	return cmd
}

func newContextMechanismsRepoSymbolsPersistCommand() *cobra.Command {
	var workspacePath string
	var workspaceID string
	var maxSymbols int
	var perNodeCap int
	var limit int
	var filter mechanismCandidateFilter
	var withEmbeddings bool

	cmd := &cobra.Command{
		Use:   "persist",
		Short: "Persist repo-symbol mechanisms into named memory as literal and structural views",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx, config.WithWorkspacePath(target))
			if err != nil {
				return err
			}

			result, resolvedWorkspaceID, err := buildRepoSymbolMechanismsForCommand(ctx, cfg, target, workspaceID, maxSymbols, perNodeCap)
			if err != nil {
				return err
			}
			corpus := filterMechanismCandidates(result.Candidates, filter)
			if limit <= 0 || limit > len(corpus) {
				limit = len(corpus)
			}
			corpus = corpus[:limit]

			memStore, err := memorystore.OpenWithConfig(ctx, cfg)
			if err != nil {
				return err
			}
			defer func() { _ = memStore.Close() }()

			var provider semantic.EmbeddingProvider
			var providerErr string
			if withEmbeddings {
				provider, err = semantic.NewProviderForScope(
					semantic.ScopeMemory,
					cfg,
					semantic.WithGeminiKey(os.Getenv("GEMINI_API_KEY")),
				)
				if err != nil {
					providerErr = err.Error()
				}
			}

			reports := make([]contextplane.MechanismMemoryPersistReport, 0, len(corpus))
			stored := 0
			embedded := 0
			for _, candidate := range corpus {
				report, err := contextplane.PersistMechanismMemoryProjection(ctx, memStore, provider, candidate.Projection)
				if err != nil {
					return err
				}
				stored += report.Stored
				embedded += report.Embedded
				reports = append(reports, report)
			}

			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/mechanisms_repo_symbols_persist", map[string]any{
				"workspace_path":             target,
				"workspace_id":               resolvedWorkspaceID,
				"candidates_total":           len(result.Candidates),
				"candidates_filtered":        len(filterMechanismCandidates(result.Candidates, filter)),
				"persisted_projections":      len(reports),
				"stored_artifacts":           stored,
				"embedded_artifacts":         embedded,
				"with_embeddings_requested":  withEmbeddings,
				"embedding_provider_enabled": provider != nil,
				"embedding_provider_error":   providerErr,
				"reports":                    reports,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	addContextMechanismRepoSymbolFlags(cmd, &workspacePath, &workspaceID, &maxSymbols, &perNodeCap)
	addContextMechanismFilterFlags(cmd, &filter)
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum mechanism projections to persist after filtering")
	cmd.Flags().BoolVar(&withEmbeddings, "with-embeddings", true, "Embed literal and structural mechanism views when an embedding provider is configured")
	return cmd
}

func newContextMechanismsRepoSymbolsCollideMemoryCommand() *cobra.Command {
	var workspacePath string
	var workspaceID string
	var maxSymbols int
	var perNodeCap int
	var query mechanismCandidateFilter
	var limit int
	var candidateLimit int
	var entropy float64
	var threshold float64
	var includeSameDomain bool
	var includeSchema bool
	var includeVectors bool

	cmd := &cobra.Command{
		Use:   "collide-memory",
		Short: "Query persisted mechanism memories with a repo-symbol mechanism",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx, config.WithWorkspacePath(target))
			if err != nil {
				return err
			}

			result, resolvedWorkspaceID, err := buildRepoSymbolMechanismsForCommand(ctx, cfg, target, workspaceID, maxSymbols, perNodeCap)
			if err != nil {
				return err
			}
			queryCandidate, ok := selectMechanismQueryCandidate(result.Candidates, query)
			if !ok {
				return fmt.Errorf("query symbol not found in mechanism candidate corpus; pass --query-name, --query-file, or --query-symbol-id")
			}

			memStore, err := memorystore.OpenWithConfig(ctx, cfg)
			if err != nil {
				return err
			}
			defer func() { _ = memStore.Close() }()

			provider, err := semantic.NewProviderForScope(
				semantic.ScopeMemory,
				cfg,
				semantic.WithGeminiKey(os.Getenv("GEMINI_API_KEY")),
			)
			if err != nil {
				return err
			}

			searchResult, err := contextplane.SearchMechanismMemoryCollisions(ctx, memStore, provider, queryCandidate.Projection, contextplane.MechanismMemoryCollisionSearchOptions{
				WorkspaceID:       resolvedWorkspaceID,
				CandidateLimit:    candidateLimit,
				Entropy:           entropy,
				Threshold:         threshold,
				Limit:             limit,
				IncludeSameDomain: includeSameDomain,
			})
			if err != nil {
				return err
			}

			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/mechanisms_repo_symbols_collide_memory", map[string]any{
				"workspace_path":           target,
				"workspace_id":             resolvedWorkspaceID,
				"query":                    mechanismCandidateViewFor(queryCandidate, includeSchema, includeVectors),
				"collisions":               searchResult.Plan.Cells,
				"collision_count":          len(searchResult.Plan.Cells),
				"candidate_count":          len(result.Candidates),
				"structural_candidates":    searchResult.StructuralCandidates,
				"memories_loaded":          searchResult.MemoriesLoaded,
				"skipped_pairs":            searchResult.SkippedPairs,
				"skipped":                  searchResult.Plan.Skipped,
				"skipped_unembedded":       result.SkippedUnembedded,
				"skipped_invalid":          result.SkippedInvalid,
				"entropy":                  entropy,
				"threshold":                threshold,
				"include_same_domain":      includeSameDomain,
				"embedding_provider_model": provider.Model(),
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	addContextMechanismRepoSymbolFlags(cmd, &workspacePath, &workspaceID, &maxSymbols, &perNodeCap)
	cmd.Flags().StringVar(&query.SymbolID, "query-symbol-id", "", "Query symbol ID; accepts repoindex node ID, raw symbol key, or embedding symbol ID")
	cmd.Flags().StringVar(&query.Name, "query-name", "", "Exact query symbol name")
	cmd.Flags().StringVar(&query.File, "query-file", "", "Exact query symbol file")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum collision cells to include in output")
	cmd.Flags().IntVar(&candidateLimit, "candidate-limit", 0, "Maximum persisted structural candidates to rehydrate before planning (default: derived from --limit)")
	cmd.Flags().Float64Var(&entropy, "entropy", 0.35, "Literal-similarity penalty and structural boost factor")
	cmd.Flags().Float64Var(&threshold, "threshold", 0.70, "Minimum collision score")
	cmd.Flags().BoolVar(&includeSameDomain, "include-same-domain", false, "Allow same-domain collisions")
	cmd.Flags().BoolVar(&includeSchema, "include-schema", true, "Include blurred abstract schema text in the query output")
	cmd.Flags().BoolVar(&includeVectors, "include-vectors", false, "Include literal and structural vectors in the query output")
	return cmd
}

func addContextMechanismRepoSymbolFlags(cmd *cobra.Command, workspacePath, workspaceID *string, maxSymbols, perNodeCap *int) {
	cmd.Flags().StringVar(workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(workspaceID, "workspace-id", "", "Embedding workspace ID (default: canonical ID for workspace path)")
	cmd.Flags().IntVar(maxSymbols, "max-symbols", defaultContextMechanismMaxSymbols, "Maximum repo symbols to scan")
	cmd.Flags().IntVar(perNodeCap, "per-node-cap", 200, "Maximum edges fetched per symbol direction")
}

func addContextMechanismFilterFlags(cmd *cobra.Command, filter *mechanismCandidateFilter) {
	cmd.Flags().StringVar(&filter.SymbolID, "symbol-id", "", "Restrict corpus to one symbol ID")
	cmd.Flags().StringVar(&filter.Name, "name", "", "Restrict corpus to exact symbol name")
	cmd.Flags().StringVar(&filter.File, "file", "", "Restrict corpus to exact file path")
	cmd.Flags().StringVar(&filter.FilePrefix, "file-prefix", "", "Restrict corpus to repo-relative file prefix")
}

func buildRepoSymbolMechanismsForCommand(ctx context.Context, cfg config.Config, workspacePath, workspaceID string, maxSymbols, perNodeCap int) (contextplane.RepoSymbolMechanismBuildResult, string, error) {
	resolvedWorkspaceID := resolveContextMechanismWorkspaceID(workspacePath, workspaceID)
	repo, err := repoindex.Open(ctx, cfg.Storage.Root, workspacePath)
	if err != nil {
		return contextplane.RepoSymbolMechanismBuildResult{}, "", err
	}
	defer func() { _ = repo.Close() }()

	embeddingStore, err := embedding.OpenStoreFromConfig(ctx, cfg)
	if err != nil {
		return contextplane.RepoSymbolMechanismBuildResult{}, "", err
	}
	defer func() { _ = embeddingStore.Close() }()

	result, err := contextplane.BuildRepoSymbolMechanismCandidates(ctx, repo, embeddingStore, contextplane.RepoSymbolMechanismBuildOptions{
		WorkspaceID: resolvedWorkspaceID,
		MaxSymbols:  maxSymbols,
		PerNodeCap:  perNodeCap,
	})
	return result, resolvedWorkspaceID, err
}

func resolveContextMechanismWorkspaceID(workspacePath, workspaceID string) string {
	if strings.TrimSpace(workspaceID) != "" {
		return wsutil.CanonicalID(workspaceID)
	}
	return wsutil.ID(workspacePath)
}

func filterMechanismCandidates(candidates []contextplane.RepoSymbolMechanismCandidate, filter mechanismCandidateFilter) []contextplane.RepoSymbolMechanismCandidate {
	filter.SymbolID = strings.TrimSpace(filter.SymbolID)
	filter.Name = strings.TrimSpace(filter.Name)
	filter.File = filepath.ToSlash(strings.TrimSpace(filter.File))
	filter.FilePrefix = filepath.ToSlash(strings.TrimSpace(filter.FilePrefix))
	if filter.SymbolID == "" && filter.Name == "" && filter.File == "" && filter.FilePrefix == "" {
		return candidates
	}
	out := make([]contextplane.RepoSymbolMechanismCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if mechanismCandidateMatches(candidate, filter) {
			out = append(out, candidate)
		}
	}
	return out
}

func selectMechanismQueryCandidate(candidates []contextplane.RepoSymbolMechanismCandidate, query mechanismCandidateFilter) (contextplane.RepoSymbolMechanismCandidate, bool) {
	query.SymbolID = strings.TrimSpace(query.SymbolID)
	query.Name = strings.TrimSpace(query.Name)
	query.File = filepath.ToSlash(strings.TrimSpace(query.File))
	if query.SymbolID == "" && query.Name == "" && query.File == "" {
		return contextplane.RepoSymbolMechanismCandidate{}, false
	}
	for _, candidate := range candidates {
		if mechanismCandidateMatches(candidate, query) {
			return candidate, true
		}
	}
	return contextplane.RepoSymbolMechanismCandidate{}, false
}

func mechanismCandidateMatches(candidate contextplane.RepoSymbolMechanismCandidate, filter mechanismCandidateFilter) bool {
	if filter.SymbolID != "" && !mechanismCandidateSymbolMatches(candidate, filter.SymbolID) {
		return false
	}
	if filter.Name != "" && candidate.Node.Name != filter.Name {
		return false
	}
	if filter.File != "" && filepath.ToSlash(candidate.Node.File) != filter.File {
		return false
	}
	if filter.FilePrefix != "" && !strings.HasPrefix(filepath.ToSlash(candidate.Node.File), filter.FilePrefix) {
		return false
	}
	return true
}

func mechanismCandidateSymbolMatches(candidate contextplane.RepoSymbolMechanismCandidate, symbolID string) bool {
	symbolID = strings.TrimSpace(symbolID)
	if symbolID == "" {
		return true
	}
	if candidate.SymbolID == symbolID || candidate.Node.ID == symbolID || candidate.Projection.ID == symbolID {
		return true
	}
	if strings.HasSuffix(candidate.Node.ID, ":"+symbolID) || strings.HasSuffix(candidate.Node.ID, "::"+symbolID) {
		return true
	}
	return false
}

func mechanismCandidateViews(candidates []contextplane.RepoSymbolMechanismCandidate, limit int, includeSchema, includeVectors bool) []mechanismCandidateView {
	if limit <= 0 || limit > len(candidates) {
		limit = len(candidates)
	}
	out := make([]mechanismCandidateView, 0, limit)
	for _, candidate := range candidates[:limit] {
		out = append(out, mechanismCandidateViewFor(candidate, includeSchema, includeVectors))
	}
	return out
}

func mechanismCandidateViewFor(candidate contextplane.RepoSymbolMechanismCandidate, includeSchema, includeVectors bool) mechanismCandidateView {
	view := mechanismCandidateView{
		SymbolID:        candidate.SymbolID,
		NodeID:          candidate.Node.ID,
		Name:            candidate.Node.Name,
		File:            candidate.Node.File,
		Domain:          candidate.Projection.OriginalDomain,
		Summary:         candidate.Projection.Summary,
		MechanismTags:   candidate.Projection.MechanismTags,
		StructuralShape: candidate.StructuralShape,
		LiteralDims:     len(candidate.LiteralVector),
		StructuralDims:  len(candidate.StructuralVector),
		SourceRefs:      contextplane.EvidenceRefsToStrings(candidate.Projection.SourceRefs),
	}
	if includeSchema {
		view.AbstractSchema = candidate.Projection.AbstractSchema
	}
	if includeVectors {
		view.LiteralVector = candidate.LiteralVector
		view.StructuralVector = candidate.StructuralVector
	}
	return view
}

func mechanismCollisionViews(cells []contextplane.MemoryCollisionCell, candidates []contextplane.RepoSymbolMechanismCandidate, includeSchema, includeVectors bool) []mechanismCandidateView {
	byMemoryID := make(map[string]contextplane.RepoSymbolMechanismCandidate, len(candidates))
	for _, candidate := range candidates {
		byMemoryID[candidate.Projection.ID] = candidate
	}
	out := make([]mechanismCandidateView, 0, len(cells))
	for _, cell := range cells {
		candidate, ok := byMemoryID[cell.MemoryID]
		if !ok {
			continue
		}
		view := mechanismCandidateViewFor(candidate, includeSchema, includeVectors)
		view.CollisionScore = cell.CollisionScore
		view.LiteralSimilarity = cell.LiteralSimilarity
		view.StructSimilarity = cell.StructuralSimilarity
		out = append(out, view)
	}
	return out
}
