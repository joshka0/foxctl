package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
	"github.com/joshka0/foxctl/internal/intelligence/repoquery"
	repoqueryadapters "github.com/joshka0/foxctl/internal/intelligence/repoquery/adapters"
	"github.com/joshka0/foxctl/internal/platform/symbolutil"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/spf13/cobra"
)

func newIndexRepoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage the repo graph index",
	}
	cmd.AddCommand(
		newIndexRepoBuildCommand(),
		newIndexRepoEnrichCommand(),
		newIndexRepoStatusCommand(),
		newIndexRepoSearchCommand(),
		newIndexRepoExpandCommand(),
		newIndexRepoOpenCommand(),
		newIndexRepoTracePathCommand(),
		newIndexRepoSmartContextCommand(),
		newIndexRepoBlastRadiusCommand(),
		newIndexRepoAskCommand(),
	)
	return cmd
}

func newIndexRepoBuildCommand() *cobra.Command {
	var workspace string
	var patterns []string
	var includeGo bool
	var includePython bool
	var includeRust bool
	var includeCSharp bool
	var includeTS bool
	var includeElixir bool
	var includeTerraform bool
	var includeKubernetes bool
	var includeShell bool
	var includeTests bool
	var dryRun bool
	progress := true
	incremental := true
	var includeSemanticAnchors bool
	var includeCoChange bool

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build the repo graph index",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexRepoBuild(cmd, workspace, patterns, includeGo, includePython, includeRust, includeCSharp, includeTS, includeElixir, includeTerraform, includeKubernetes, includeShell, includeTests, dryRun, progress, incremental, includeSemanticAnchors, includeCoChange)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringSliceVar(&patterns, "go-pattern", []string{"./..."}, "Go package patterns to index")
	cmd.Flags().BoolVar(&includeGo, "go", true, "Include Go sources")
	cmd.Flags().BoolVar(&includePython, "python", false, "Include Python sources")
	cmd.Flags().BoolVar(&includeRust, "rust", false, "Include Rust sources")
	cmd.Flags().BoolVar(&includeCSharp, "csharp", false, "Include C# sources")
	cmd.Flags().BoolVar(&includeTS, "typescript", true, "Include TypeScript sources")
	cmd.Flags().BoolVar(&includeElixir, "elixir", false, "Include Elixir sources")
	cmd.Flags().BoolVar(&includeTerraform, "terraform", false, "Include Terraform files as file/concept graph components")
	cmd.Flags().BoolVar(&includeKubernetes, "kubernetes", false, "Include Kubernetes YAML manifests as file/resource graph components")
	cmd.Flags().BoolVar(&includeShell, "shell", false, "Include shell scripts as file/command graph components")
	cmd.Flags().BoolVar(&includeTests, "include-tests", false, "Include test files")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Build without writing to the index")
	cmd.Flags().BoolVar(&progress, "progress", true, "Write coarse build progress logs to stderr")
	cmd.Flags().BoolVar(&incremental, "incremental", true, "Skip rebuild when stored per-file state and current workspace have no delta; set false to force a full rebuild")
	cmd.Flags().BoolVar(&includeSemanticAnchors, "semantic-anchors", false, "Include semantic anchor concept nodes and edges")
	cmd.Flags().BoolVar(&includeCoChange, "cochange", false, "Include empirical git co-change file edges")

	return cmd
}

func newIndexRepoEnrichCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enrich",
		Short: "Enrich an existing repo graph index",
	}
	cmd.AddCommand(newIndexRepoEnrichSummariesCommand())
	return cmd
}

func newIndexRepoEnrichSummariesCommand() *cobra.Command {
	var workspace string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "summaries",
		Short: "Attach stored file and symbol summaries to repo graph nodes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexRepoEnrichSummaries(cmd, workspace, dryRun)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report summary updates without writing to the index")

	return cmd
}

func newIndexRepoStatusCommand() *cobra.Command {
	var workspace string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show repo graph index status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexRepoStatus(cmd, workspace)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")

	return cmd
}

func newIndexRepoSearchCommand() *cobra.Command {
	var workspace string
	var query string
	var limit int

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search nodes by text",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexRepoSearch(cmd, workspace, query, limit)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringVar(&query, "query", "", "FTS query string")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum results")
	_ = cmd.MarkFlagRequired("query")

	return cmd
}

func newIndexRepoExpandCommand() *cobra.Command {
	var workspace string
	var seeds []string
	var edgeTypes []string
	var depth int
	var budget int
	var perNodeCap int
	var direction string
	var includeSemanticAnchors bool

	cmd := &cobra.Command{
		Use:   "expand",
		Short: "Expand the graph from seed nodes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexRepoExpand(cmd, workspace, seeds, edgeTypes, depth, budget, perNodeCap, direction, includeSemanticAnchors)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringSliceVar(&seeds, "seed", nil, "Seed node IDs (repeatable)")
	cmd.Flags().StringSliceVar(&edgeTypes, "edge", nil, "Edge types to traverse (repeatable)")
	cmd.Flags().IntVar(&depth, "depth", 1, "Traversal depth")
	cmd.Flags().IntVar(&budget, "budget", 50, "Max nodes to return")
	cmd.Flags().IntVar(&perNodeCap, "per-node", 50, "Max edges per node per hop")
	cmd.Flags().StringVar(&direction, "direction", string(repoindex.DirOut), "Traversal direction: out or in")
	cmd.Flags().BoolVar(&includeSemanticAnchors, "semantic-anchors", false, "Include semantic anchor edges in traversal")
	_ = cmd.MarkFlagRequired("seed")

	return cmd
}

func newIndexRepoOpenCommand() *cobra.Command {
	var workspace string
	var id string

	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open a node by ID",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexRepoOpen(cmd, workspace, id)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringVar(&id, "id", "", "Node ID")
	_ = cmd.MarkFlagRequired("id")

	return cmd
}

func newIndexRepoTracePathCommand() *cobra.Command {
	var workspace string
	var srcID string
	var dstID string
	var edgeTypes []string
	var maxDepth int
	var perNodeCap int
	var allowStale bool

	cmd := &cobra.Command{
		Use:     "trace-path",
		Aliases: []string{"trace"},
		Short:   "Find a shortest stored repo graph path between two nodes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexRepoTracePath(cmd, workspace, srcID, dstID, edgeTypes, maxDepth, perNodeCap, allowStale)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringVar(&srcID, "src-id", "", "Source node ID")
	cmd.Flags().StringVar(&dstID, "dst-id", "", "Destination node ID")
	cmd.Flags().StringSliceVar(&edgeTypes, "edge", nil, "Edge types to traverse (repeatable)")
	cmd.Flags().IntVar(&maxDepth, "max-depth", 5, "Maximum path depth")
	cmd.Flags().IntVar(&perNodeCap, "per-node", 50, "Max outgoing edges per node")
	cmd.Flags().BoolVar(&allowStale, "allow-stale", false, "Allow querying a stale or dirty repo index")
	_ = cmd.MarkFlagRequired("src-id")
	_ = cmd.MarkFlagRequired("dst-id")

	return cmd
}

func newIndexRepoSmartContextCommand() *cobra.Command {
	var workspace string
	var nodeID string
	var limit int
	var allowStale bool

	cmd := &cobra.Command{
		Use:     "smart-context",
		Aliases: []string{"context"},
		Short:   "Return stable one-hop repo graph context sections for a node",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexRepoSmartContext(cmd, workspace, nodeID, limit, allowStale)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringVar(&nodeID, "node-id", "", "Node ID")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum edges per context section")
	cmd.Flags().BoolVar(&allowStale, "allow-stale", false, "Allow querying a stale or dirty repo index")
	_ = cmd.MarkFlagRequired("node-id")

	return cmd
}

func newIndexRepoBlastRadiusCommand() *cobra.Command {
	var workspace string
	var nodeID string
	var edgeTypes []string
	var maxDepth int
	var limit int
	var perNodeCap int
	var allowStale bool

	cmd := &cobra.Command{
		Use:     "blast-radius",
		Aliases: []string{"blast"},
		Short:   "Expand bounded downstream repo graph impact for a node",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexRepoBlastRadius(cmd, workspace, nodeID, edgeTypes, maxDepth, limit, perNodeCap, allowStale)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringVar(&nodeID, "node-id", "", "Node ID")
	cmd.Flags().StringSliceVar(&edgeTypes, "edge", nil, "Edge types to traverse (repeatable)")
	cmd.Flags().IntVar(&maxDepth, "max-depth", 3, "Maximum expansion depth")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum nodes to return")
	cmd.Flags().IntVar(&perNodeCap, "per-node", 50, "Max outgoing edges per node")
	cmd.Flags().BoolVar(&allowStale, "allow-stale", false, "Allow querying a stale or dirty repo index")
	_ = cmd.MarkFlagRequired("node-id")

	return cmd
}

func newIndexRepoAskCommand() *cobra.Command {
	var workspace string
	var question string
	var provider string
	var model string
	var apiKey string
	var maxIterations int
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "ask",
		Short: "Ask a question using the repo index",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexRepoAsk(cmd, workspace, question, provider, model, apiKey, maxIterations, timeout)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringVar(&question, "question", "", "Question to ask")
	cmd.Flags().StringVar(&provider, "provider", "", "LLM provider (cerebras|openrouter|groq|openai|gemini|anthropic)")
	cmd.Flags().StringVar(&model, "model", "", "LLM model")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "LLM API key override")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 12, "Maximum tool-call iterations")
	cmd.Flags().DurationVar(&timeout, "timeout", 60*time.Second, "LLM request timeout")
	_ = cmd.MarkFlagRequired("question")

	return cmd
}

func runIndexRepoBuild(cmd *cobra.Command, workspace string, patterns []string, includeGo, includePython, includeRust, includeCSharp, includeTS, includeElixir, includeTerraform, includeKubernetes, includeShell, includeTests, dryRun, progress, incremental, includeSemanticAnchors, includeCoChange bool) error {
	ctx := cmd.Context()
	start := time.Now()

	var progressFn func(repoindex.BuildProgress)
	var progressLogger *repoIndexBuildProgressLogger
	var stopProgressHeartbeat func()
	if progress {
		progressLogger = newRepoIndexBuildProgressLogger(cmd.ErrOrStderr(), start)
		progressFn = progressLogger.Report
		stopProgressHeartbeat = progressLogger.StartHeartbeat(ctx, 15*time.Second)
		defer stopProgressHeartbeat()
		progressLogger.ReportPhase("init", "starting repoindex build")
	}

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		progressLogger.ReportPhase("error", fmt.Sprintf("resolve workspace failed: %v", err))
		return fmt.Errorf("resolve workspace: %w", err)
	}
	progressLogger.ReportPhase("init", repoIndexBuildOptionsMessage(absWorkspace, patterns, includeGo, includePython, includeRust, includeCSharp, includeTS, includeElixir, includeTerraform, includeKubernetes, includeShell, includeTests, dryRun, incremental, includeSemanticAnchors, includeCoChange))

	progressLogger.ReportPhase("config", "loading foxctl configuration")
	cfg, err := loadConfig(ctx)
	if err != nil {
		progressLogger.ReportPhase("error", fmt.Sprintf("load config failed: %v", err))
		return fmt.Errorf("load config: %w", err)
	}

	progressLogger.ReportPhase("storage", "opening repoindex store")
	store, err := repoindex.Open(ctx, cfg.Storage.Root, absWorkspace)
	if err != nil {
		progressLogger.ReportPhase("error", fmt.Sprintf("open repoindex store failed: %v", err))
		return fmt.Errorf("open repoindex store: %w", err)
	}
	defer store.Close()
	progressLogger.ReportPhase("storage", fmt.Sprintf("repoindex store ready path=%s", store.Path()))

	var delta repoindex.WorkspaceDelta
	var deltaAvailable bool
	if incremental {
		progressLogger.ReportPhase("delta", "computing workspace delta")
		if computed, err := store.ComputeDelta(ctx); err == nil {
			delta = computed
			deltaAvailable = true
			progressLogger.ReportPhase("delta", fmt.Sprintf("workspace delta %s", formatRepoIndexDeltaCounts(computed)))
			meta, metaErr := store.GetMeta(ctx)
			current := repoindex.ResolveGitSnapshot(ctx, absWorkspace)
			freshness := repoindex.CompareIndexFreshness(meta, current)
			if metaErr == nil && repoIndexBuildCanSkip(computed, meta, current) {
				progressLogger.ReportPhase("done", "workspace diff unchanged; skipping repoindex rebuild")
				data := map[string]any{
					"workspace":    absWorkspace,
					"store_path":   store.Path(),
					"delta":        computed,
					"delta_counts": repoIndexDeltaCounts(computed),
					"freshness":    freshness,
					"duration_ms":  time.Since(start).Milliseconds(),
					"dry_run":      dryRun,
					"incremental":  true,
					"skipped":      true,
					"reason":       "workspace_diff_unchanged",
					"skip_basis":   "head_and_dirty_status_hash",
				}
				data["meta"] = meta
				env := protocol.OK("index.repo.build", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				progressLogger.ReportPhase("done", "writing success envelope")
				return protocol.Write(cmd.OutOrStdout(), env)
			}
		} else {
			progressLogger.ReportPhase("delta", fmt.Sprintf("workspace delta unavailable; running full build: %v", err))
		}
	}

	builder := repoindex.NewBuilder(store, absWorkspace)
	buildOpts := repoindex.BuildOptions{
		RepoRoot:               absWorkspace,
		Patterns:               patterns,
		IncludeTests:           includeTests,
		IncludeGo:              includeGo,
		IncludePython:          includePython,
		IncludeRust:            includeRust,
		IncludeCSharp:          includeCSharp,
		IncludeTypescript:      includeTS,
		IncludeElixir:          includeElixir,
		IncludeTerraform:       includeTerraform,
		IncludeKubernetes:      includeKubernetes,
		IncludeShell:           includeShell,
		IncludeSemanticAnchors: includeSemanticAnchors,
		IncludeCoChange:        includeCoChange,
		DryRun:                 dryRun,
		Progress:               progressFn,
	}
	var result repoindex.BuildResult
	var deltaBuild repoindex.BuildDeltaResult
	if incremental && deltaAvailable {
		progressLogger.ReportPhase("build", "running incremental repoindex build")
	} else {
		progressLogger.ReportPhase("build", "running full repoindex build")
	}
	if incremental && deltaAvailable {
		deltaBuild, err = builder.BuildDelta(ctx, buildOpts, delta)
		result = deltaBuild.Result
	} else {
		result, err = builder.Build(ctx, buildOpts)
	}
	if err != nil {
		progressLogger.ReportPhase("error", fmt.Sprintf("repo index build failed: %v", err))
		hint := "Verify repo index configuration, input files, and permissions."
		data := protocol.ErrorData{Hint: hint}
		env := protocol.Error("index.repo.build", protocol.ErrorCodeERuntime, "repo index build failed", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		if writeErr := protocol.Write(cmd.OutOrStdout(), env); writeErr != nil {
			return fmt.Errorf("write repo index build error envelope: %w", writeErr)
		}
		return fmt.Errorf("repo index build failed: %w", err)

	}
	meta, metaErr := store.GetMeta(ctx)
	progressLogger.ReportPhaseResult("done", "repoindex build complete", result)

	data := map[string]any{
		"workspace":        absWorkspace,
		"store_path":       store.Path(),
		"result":           result,
		"duration_ms":      time.Since(start).Milliseconds(),
		"dry_run":          dryRun,
		"incremental":      incremental,
		"semantic_anchors": includeSemanticAnchors,
	}
	if deltaAvailable {
		data["delta"] = delta
		data["delta_counts"] = repoIndexDeltaCounts(delta)
		if incremental {
			data["delta_build"] = map[string]any{
				"mode":          deltaBuild.Mode,
				"reason":        deltaBuild.Reason,
				"full_fallback": deltaBuild.FullFallback,
			}
		}
	}
	if metaErr == nil && !dryRun {
		data["meta"] = meta
	}

	env := protocol.OK("index.repo.build", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	progressLogger.ReportPhase("done", "writing success envelope")
	return protocol.Write(cmd.OutOrStdout(), env)
}

type repoIndexSummaryEnrichResult struct {
	FileNodesScanned          int `json:"file_nodes_scanned"`
	FileSummariesApplied      int `json:"file_summaries_applied,omitempty"`
	FileSummariesWouldApply   int `json:"file_summaries_would_apply,omitempty"`
	FileSummariesSkipped      int `json:"file_summaries_skipped,omitempty"`
	FileSummariesMissing      int `json:"file_summaries_missing,omitempty"`
	SymbolNodesScanned        int `json:"symbol_nodes_scanned"`
	SymbolSummariesApplied    int `json:"symbol_summaries_applied,omitempty"`
	SymbolSummariesWouldApply int `json:"symbol_summaries_would_apply,omitempty"`
	SymbolSummariesSkipped    int `json:"symbol_summaries_skipped,omitempty"`
	SymbolSummariesMissing    int `json:"symbol_summaries_missing,omitempty"`
}

func runIndexRepoEnrichSummaries(cmd *cobra.Command, workspace string, dryRun bool) error {
	ctx := cmd.Context()
	start := time.Now()

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	cfg, err := loadConfig(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	store, err := repoindex.Open(ctx, cfg.Storage.Root, absWorkspace)
	if err != nil {
		return fmt.Errorf("open repoindex store: %w", err)
	}
	defer store.Close()

	casDir := cfg.Paths.CAS
	if casDir == "" {
		casDir = filepath.Join(cfg.Home, "cas")
	}
	memStore, err := memory.Open(ctx, cfg.Storage.Root, casDir)
	if err != nil {
		return fmt.Errorf("open memory store: %w", err)
	}
	defer memStore.Close()

	summaryMemory, err := loadRepoIndexSummaryMemory(ctx, memStore, absWorkspace)
	if err != nil {
		return fmt.Errorf("load summary memory: %w", err)
	}
	result := repoIndexSummaryEnrichResult{}

	fileNodes, err := store.ListAllNodesByKind(ctx, repoindex.NodeFile)
	if err != nil {
		return fmt.Errorf("list file nodes: %w", err)
	}
	result.FileNodesScanned = len(fileNodes)
	for _, node := range fileNodes {
		if strings.TrimSpace(node.Summary) != "" {
			result.FileSummariesSkipped++
			continue
		}
		summary, ok := summaryMemory.FileSummary(node.File)
		if !ok {
			result.FileSummariesMissing++
			continue
		}
		if dryRun {
			result.FileSummariesWouldApply++
			continue
		}
		if err := store.UpdateNodeSummary(ctx, node.ID, summary); err != nil {
			return fmt.Errorf("update file node summary %s: %w", node.ID, err)
		}
		result.FileSummariesApplied++
	}

	symbolNodes, err := store.ListAllNodesByKind(ctx, repoindex.NodeSymbol)
	if err != nil {
		return fmt.Errorf("list symbol nodes: %w", err)
	}
	result.SymbolNodesScanned = len(symbolNodes)
	for _, node := range symbolNodes {
		if strings.TrimSpace(node.Summary) != "" {
			result.SymbolSummariesSkipped++
			continue
		}
		summaryPkg := symbolutil.DeriveSymbolPackage(node.File, repoIndexSummaryLanguageFromPackageID(node.Pkg))
		symbolKey := repoIndexSummarySymbolKey(node)
		symbolID := repoIndexSummarySymbolID(node)
		summary, ok := summaryMemory.SymbolSummary(symbolID, symbolKey, summaryPkg)
		if !ok {
			result.SymbolSummariesMissing++
			continue
		}
		if dryRun {
			result.SymbolSummariesWouldApply++
			continue
		}
		if err := store.UpdateNodeSummary(ctx, node.ID, summary); err != nil {
			return fmt.Errorf("update symbol node summary %s: %w", node.ID, err)
		}
		result.SymbolSummariesApplied++
	}

	data := map[string]any{
		"workspace":   absWorkspace,
		"store_path":  store.Path(),
		"dry_run":     dryRun,
		"result":      result,
		"duration_ms": time.Since(start).Milliseconds(),
	}
	env := protocol.OK("index.repo.enrich.summaries", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

type repoIndexSummaryMemory struct {
	fileSummaries      map[string]string
	symbolKeySummaries map[string]string
	symbolIDSummaries  map[string]string
}

func loadRepoIndexSummaryMemory(ctx context.Context, store *memory.Store, workspace string) (repoIndexSummaryMemory, error) {
	index := repoIndexSummaryMemory{
		fileSummaries:      make(map[string]string),
		symbolKeySummaries: make(map[string]string),
		symbolIDSummaries:  make(map[string]string),
	}
	for _, candidate := range summaryWorkspaceCandidates(workspace) {
		if err := loadRepoIndexFileSummaryMemory(ctx, store, candidate, index.fileSummaries); err != nil {
			return repoIndexSummaryMemory{}, err
		}
		if err := loadRepoIndexSymbolSummaryMemory(ctx, store, candidate, index.symbolKeySummaries, index.symbolIDSummaries); err != nil {
			return repoIndexSummaryMemory{}, err
		}
	}
	return index, nil
}

func loadRepoIndexFileSummaryMemory(ctx context.Context, store *memory.Store, workspace string, out map[string]string) error {
	return listRepoIndexSummaryEntries(ctx, store, workspace, symbol.FileSummaryType, func(entry memory.NamedEntry) {
		path, ok := repoIndexFileSummaryPath(entry.Name, workspace)
		if !ok {
			return
		}
		addRepoIndexSummaryValue(out, filepath.ToSlash(path), entry.Summary)
	})
}

func loadRepoIndexSymbolSummaryMemory(ctx context.Context, store *memory.Store, workspace string, keyed, byID map[string]string) error {
	return listRepoIndexSummaryEntries(ctx, store, workspace, symbol.SymbolSummaryType, func(entry memory.NamedEntry) {
		pkg, symbolKey, ok := repoIndexSymbolSummaryKey(entry.Name, workspace)
		if ok {
			addRepoIndexSummaryValue(keyed, repoIndexSymbolSummaryMapKey(pkg, symbolKey), entry.Summary)
			return
		}
		if symbolID, ok := repoIndexSymbolSummaryID(entry.Name, workspace); ok {
			addRepoIndexSummaryValue(byID, symbolID, entry.Summary)
		}
	})
}

func listRepoIndexSummaryEntries(ctx context.Context, store *memory.Store, workspace, typ string, visit func(memory.NamedEntry)) error {
	const batchSize = 1000
	filter := memory.ListFilter{Types: []string{typ}}
	for offset := 0; ; {
		entries, total, err := store.ListFiltered(ctx, workspace, filter, batchSize, offset)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			visit(entry)
		}
		offset += len(entries)
		if len(entries) == 0 || offset >= total {
			return nil
		}
	}
}

func addRepoIndexSummaryValue(out map[string]string, key, summary string) {
	key = strings.TrimSpace(key)
	summary = strings.TrimSpace(summary)
	if key == "" || summary == "" {
		return
	}
	if _, exists := out[key]; exists {
		return
	}
	out[key] = summary
}

func (m repoIndexSummaryMemory) FileSummary(path string) (string, bool) {
	for _, candidate := range summaryPathCandidates(path) {
		if summary, ok := m.fileSummaries[filepath.ToSlash(candidate)]; ok {
			return summary, true
		}
	}
	return "", false
}

func (m repoIndexSummaryMemory) SymbolSummary(symbolID, symbolKey, pkg string) (string, bool) {
	if strings.TrimSpace(pkg) != "" && strings.TrimSpace(symbolKey) != "" {
		summary, ok := m.symbolKeySummaries[repoIndexSymbolSummaryMapKey(pkg, symbolKey)]
		return summary, ok
	}
	for _, candidate := range summarySymbolIDCandidates(symbolID) {
		if summary, ok := m.symbolIDSummaries[candidate]; ok {
			return summary, true
		}
	}
	return "", false
}

func repoIndexFileSummaryPath(name, workspace string) (string, bool) {
	prefix := "file://" + workspace + "/"
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	path := strings.TrimSpace(strings.TrimPrefix(name, prefix))
	return path, path != ""
}

func repoIndexSymbolSummaryKey(name, workspace string) (pkg string, symbolKey string, ok bool) {
	prefix := "symbol-summary://" + workspace + "/"
	if !strings.HasPrefix(name, prefix) {
		return "", "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(name, prefix))
	idx := strings.LastIndex(rest, "::")
	if idx <= 0 || idx+2 >= len(rest) {
		return "", "", false
	}
	pkg = strings.TrimSpace(rest[:idx])
	symbolKey = strings.TrimSpace(rest[idx+2:])
	return pkg, symbolKey, pkg != "" && symbolKey != ""
}

func repoIndexSymbolSummaryID(name, workspace string) (string, bool) {
	prefix := "symbol-summary://" + workspace + "/"
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	id := strings.TrimSpace(strings.TrimPrefix(name, prefix))
	if strings.Contains(id, "::") {
		return "", false
	}
	return id, id != ""
}

func repoIndexSymbolSummaryMapKey(pkg, symbolKey string) string {
	return strings.TrimSpace(pkg) + "\x00" + strings.TrimSpace(symbolKey)
}

func repoIndexSummarySymbolID(node repoindex.Node) string {
	_, raw := repoindex.SplitNamespacedID(node.ID)
	prefix := "sym:" + strings.TrimSpace(node.Pkg) + ":"
	if !strings.HasPrefix(raw, prefix) {
		return strings.TrimSpace(node.ID)
	}
	return strings.TrimSpace(strings.TrimPrefix(raw, prefix))
}

func repoIndexSummarySymbolKey(node repoindex.Node) string {
	_, raw := repoindex.SplitNamespacedID(node.ID)
	prefix := "sym:" + strings.TrimSpace(node.Pkg) + ":"
	if !strings.HasPrefix(raw, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(raw, prefix))
}

func repoIndexSummaryLanguageFromPackageID(pkgID string) string {
	switch {
	case strings.HasPrefix(pkgID, "go:"):
		return "go"
	case strings.HasPrefix(pkgID, "ts:"):
		return "typescript"
	case strings.HasPrefix(pkgID, "py:"):
		return "python"
	case strings.HasPrefix(pkgID, "rs:"):
		return "rust"
	case strings.HasPrefix(pkgID, "cs:"):
		return "csharp"
	case strings.HasPrefix(pkgID, "ex:"):
		return "elixir"
	default:
		return ""
	}
}

type repoIndexBuildProgressLogger struct {
	mu      sync.Mutex
	writer  io.Writer
	start   time.Time
	last    repoindex.BuildProgress
	lastAt  time.Time
	written bool
}

func newRepoIndexBuildProgressLogger(writer io.Writer, start time.Time) *repoIndexBuildProgressLogger {
	if writer == nil {
		writer = io.Discard
	}
	if start.IsZero() {
		start = time.Now()
	}
	return &repoIndexBuildProgressLogger{writer: writer, start: start}
}

func (l *repoIndexBuildProgressLogger) Report(progress repoindex.BuildProgress) {
	if l == nil {
		return
	}
	line := formatRepoIndexBuildProgress(l.start, progress)
	l.mu.Lock()
	l.last = progress
	l.lastAt = time.Now()
	l.written = true
	fmt.Fprintln(l.writer, line)
	l.mu.Unlock()
}

func (l *repoIndexBuildProgressLogger) ReportPhase(phase, message string) {
	if l == nil {
		return
	}
	l.ReportPhaseResult(phase, message, repoindex.BuildResult{})
}

func (l *repoIndexBuildProgressLogger) ReportPhaseResult(phase, message string, result repoindex.BuildResult) {
	if l == nil {
		return
	}
	l.Report(repoindex.BuildProgress{
		Phase:   phase,
		Message: message,
		Time:    time.Now().UTC(),
		Result:  result,
	})
}

func (l *repoIndexBuildProgressLogger) StartHeartbeat(ctx context.Context, interval time.Duration) func() {
	if l == nil || interval <= 0 {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				l.writeHeartbeat(interval)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func (l *repoIndexBuildProgressLogger) writeHeartbeat(interval time.Duration) {
	l.mu.Lock()
	if !l.written {
		l.mu.Unlock()
		return
	}
	last := l.last
	lastAt := l.lastAt
	start := l.start
	if time.Since(lastAt) < interval {
		l.mu.Unlock()
		return
	}
	fmt.Fprintln(l.writer, formatRepoIndexBuildHeartbeat(start, last))
	l.mu.Unlock()
}

func formatRepoIndexBuildProgress(start time.Time, progress repoindex.BuildProgress) string {
	return fmt.Sprintf("repoindex build: %s phase=%s %s%s",
		repoIndexProgressElapsed(start, progress.ElapsedMs),
		emptyFallback(progress.Phase, "unknown"),
		strings.TrimSpace(progress.Message),
		formatRepoIndexBuildCounts(progress.Result),
	)
}

func formatRepoIndexBuildHeartbeat(start time.Time, progress repoindex.BuildProgress) string {
	message := strings.TrimSpace(progress.Message)
	if message != "" {
		message = " last=\"" + message + "\""
	}
	return fmt.Sprintf("repoindex build: %s still running phase=%s%s%s",
		repoIndexProgressElapsed(start, progress.ElapsedMs),
		emptyFallback(progress.Phase, "unknown"),
		message,
		formatRepoIndexBuildCounts(progress.Result),
	)
}

func repoIndexProgressElapsed(start time.Time, elapsedMs int64) time.Duration {
	if !start.IsZero() {
		return time.Since(start).Round(time.Millisecond)
	}
	if elapsedMs > 0 {
		return (time.Duration(elapsedMs) * time.Millisecond).Round(time.Millisecond)
	}
	return 0
}

func formatRepoIndexBuildCounts(result repoindex.BuildResult) string {
	if result.Packages == 0 && result.Files == 0 && result.Symbols == 0 && result.Nodes == 0 && result.Edges == 0 {
		return ""
	}
	return fmt.Sprintf(" (packages=%d files=%d symbols=%d nodes=%d edges=%d)", result.Packages, result.Files, result.Symbols, result.Nodes, result.Edges)
}

func repoIndexBuildOptionsMessage(absWorkspace string, patterns []string, includeGo, includePython, includeRust, includeCSharp, includeTS, includeElixir, includeTerraform, includeKubernetes, includeShell, includeTests, dryRun, incremental, includeSemanticAnchors, includeCoChange bool) string {
	families := repoIndexBuildFamilies(includeGo, includePython, includeRust, includeCSharp, includeTS, includeElixir, includeTerraform, includeKubernetes, includeShell)
	return fmt.Sprintf("workspace=%s families=%s go_patterns=%s include_tests=%t semantic_anchors=%t cochange=%t incremental=%t dry_run=%t",
		absWorkspace,
		strings.Join(families, ","),
		strings.Join(patterns, ","),
		includeTests,
		includeSemanticAnchors,
		includeCoChange,
		incremental,
		dryRun,
	)
}

func repoIndexBuildCanSkip(delta repoindex.WorkspaceDelta, meta repoindex.IndexMeta, current repoindex.GitSnapshot) bool {
	if delta.Unchanged == 0 {
		return false
	}
	if len(delta.Modified) > 0 || len(delta.Deleted) > 0 {
		return false
	}
	if strings.TrimSpace(meta.HeadSHA) == "" || strings.TrimSpace(current.HeadSHA) == "" {
		return false
	}
	if meta.HeadSHA != current.HeadSHA {
		return false
	}
	if meta.WorktreeDirty != current.WorktreeDirty {
		return false
	}
	if meta.WorktreeDirty && (meta.DirtyStatusHash == "" || current.DirtyStatusHash == "") {
		return false
	}
	return meta.DirtyStatusHash == current.DirtyStatusHash
}

func repoIndexBuildFamilies(includeGo, includePython, includeRust, includeCSharp, includeTS, includeElixir, includeTerraform, includeKubernetes, includeShell bool) []string {
	var families []string
	if includeGo {
		families = append(families, "go")
	}
	if includePython {
		families = append(families, "python")
	}
	if includeRust {
		families = append(families, "rust")
	}
	if includeCSharp {
		families = append(families, "csharp")
	}
	if includeTS {
		families = append(families, "typescript")
	}
	if includeElixir {
		families = append(families, "elixir")
	}
	if includeTerraform {
		families = append(families, "terraform")
	}
	if includeKubernetes {
		families = append(families, "kubernetes")
	}
	if includeShell {
		families = append(families, "shell")
	}
	if len(families) == 0 {
		return []string{"none"}
	}
	return families
}

func formatRepoIndexDeltaCounts(delta repoindex.WorkspaceDelta) string {
	counts := repoIndexDeltaCounts(delta)
	return fmt.Sprintf("added=%d modified=%d deleted=%d untracked=%d unchanged=%d",
		counts["added"],
		counts["modified"],
		counts["deleted"],
		counts["untracked"],
		counts["unchanged"],
	)
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func runIndexRepoStatus(cmd *cobra.Command, workspace string) error {
	ctx := cmd.Context()

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	cfg, err := loadConfig(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	store, err := repoindex.Open(ctx, cfg.Storage.Root, absWorkspace)
	if err != nil {
		return fmt.Errorf("open repoindex store: %w", err)
	}
	defer store.Close()

	meta, err := store.GetMeta(ctx)
	if err != nil {
		hint := "Failed to read repo metadata. Verify the index path and permissions."
		data := protocol.ErrorData{Hint: hint}
		env := protocol.Error("index.repo.status", protocol.ErrorCodeERuntime, "repo index status failed", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		if writeErr := protocol.Write(cmd.OutOrStdout(), env); writeErr != nil {
			return fmt.Errorf("write repo index status error envelope: %w", writeErr)
		}
		return fmt.Errorf("get meta: %w", err)
	}
	stats, err := store.Stats(ctx)
	if err != nil {
		hint := "Failed to compute repo index stats. Verify the index path and permissions."
		data := protocol.ErrorData{Hint: hint}
		env := protocol.Error("index.repo.status", protocol.ErrorCodeERuntime, "repo index status failed", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		if writeErr := protocol.Write(cmd.OutOrStdout(), env); writeErr != nil {
			return fmt.Errorf("write repo index status error envelope: %w", writeErr)
		}
		return fmt.Errorf("get stats: %w", err)
	}

	data := map[string]any{
		"workspace":  absWorkspace,
		"store_path": store.Path(),
		"meta":       meta,
		"stats":      stats,
	}
	currentSnapshot := repoindex.ResolveGitSnapshot(ctx, absWorkspace)
	freshness := repoindex.CompareIndexFreshness(meta, currentSnapshot)
	if currentSnapshot.HeadSHA != "" {
		data["current_head_sha"] = currentSnapshot.HeadSHA
		data["index_matches_head"] = currentSnapshot.HeadSHA == meta.HeadSHA
	}
	data["current_worktree_dirty"] = currentSnapshot.WorktreeDirty
	data["freshness"] = freshness
	if delta, err := store.ComputeDelta(ctx); err == nil {
		data["delta"] = delta
		data["delta_counts"] = repoIndexDeltaCounts(delta)
	}

	env := protocol.OK("index.repo.status", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func repoIndexDeltaCounts(delta repoindex.WorkspaceDelta) map[string]int {
	return map[string]int{
		"added":     len(delta.Added),
		"modified":  len(delta.Modified),
		"deleted":   len(delta.Deleted),
		"untracked": len(delta.Untracked),
		"unchanged": delta.Unchanged,
	}
}

func runIndexRepoSearch(cmd *cobra.Command, workspace, query string, limit int) error {
	ctx := cmd.Context()

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	cfg, err := loadConfig(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	store, err := repoindex.Open(ctx, cfg.Storage.Root, absWorkspace)
	if err != nil {
		return fmt.Errorf("open repoindex store: %w", err)
	}
	defer store.Close()

	service := repoquery.NewQueryService(repoindex.NewQueryEngine(store))
	req, err := repoquery.NewSearchRequest(query, limit)
	if err != nil {
		return err
	}
	result, err := service.SearchWithProjection(ctx, req)
	if err != nil {
		return fmt.Errorf("queryEngine.SearchWithProjection failed for query=%q limit=%d: %w", query, limit, err)
	}

	data := map[string]any{
		"workspace": absWorkspace,
		"query":     query,
		"count":     len(result.Nodes),
		"results":   result.Nodes,
		"anchors":   result.Anchors,
	}

	env := protocol.OK("index.repo.search", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func runIndexRepoExpand(cmd *cobra.Command, workspace string, seeds, edgeTypes []string, depth, budget, perNodeCap int, direction string, includeSemanticAnchors bool) error {
	ctx := cmd.Context()

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	cfg, err := loadConfig(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	store, err := repoindex.Open(ctx, cfg.Storage.Root, absWorkspace)
	if err != nil {
		return fmt.Errorf("open repoindex store: %w", err)
	}
	defer store.Close()

	service := repoquery.NewQueryService(repoindex.NewQueryEngine(store))
	req, err := repoquery.NewExpandRequest(seeds, edgeTypes, direction, depth, budget, perNodeCap)
	if err != nil {
		hint := "Use --edge with known types (CONTAINS, IMPORTS, REFERS_TO, CALLS, IMPLEMENTS, EMBEDS, TESTS)."
		data := protocol.ErrorData{Hint: hint}
		env := protocol.Error("index.repo.expand", protocol.ErrorCodeEARG, err.Error(), data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		if writeErr := protocol.Write(cmd.OutOrStdout(), env); writeErr != nil {
			return fmt.Errorf("write repo index expand error envelope: %w", writeErr)
		}
		return fmt.Errorf("build expand request: %w", err)
	}
	req.IncludeSemanticAnchors = includeSemanticAnchors
	result, err := service.ExpandWithProjection(ctx, req)
	if err != nil {
		return fmt.Errorf("repo query expand failed: %w", err)
	}

	data := map[string]any{
		"workspace":                absWorkspace,
		"seeds":                    req.Seeds,
		"edges":                    repoquery.EdgeTypeValues(req.EdgeTypes),
		"include_semantic_anchors": req.IncludeSemanticAnchors,
		"result":                   result.Result,
		"anchors":                  result.Anchors,
	}

	env := protocol.OK("index.repo.expand", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func runIndexRepoOpen(cmd *cobra.Command, workspace, id string) error {
	ctx := cmd.Context()

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	cfg, err := loadConfig(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	store, err := repoindex.Open(ctx, cfg.Storage.Root, absWorkspace)
	if err != nil {
		return fmt.Errorf("open repoindex store: %w", err)
	}
	defer store.Close()

	service := repoquery.NewQueryService(repoindex.NewQueryEngine(store))
	req, err := repoquery.NewOpenRequest(id)
	if err != nil {
		return err
	}
	result, err := service.OpenWithProjection(ctx, req)
	if err != nil {
		resolvedID, resolveErr := resolveRepoOpenFallbackID(ctx, absWorkspace, service, id)
		if resolveErr != nil {
			return fmt.Errorf("repo query open failed for id %q: %w", id, err)
		}
		req, err = repoquery.NewOpenRequest(resolvedID)
		if err != nil {
			return fmt.Errorf("repo query open failed for id %q: %w", id, err)
		}
		result, err = service.OpenWithProjection(ctx, req)
		if err != nil {
			return fmt.Errorf("repo query open failed for id %q (fallback %q): %w", id, resolvedID, err)
		}
	}

	data := map[string]any{
		"workspace": absWorkspace,
		"node":      result.Node,
		"anchor":    result.Anchor,
	}

	env := protocol.OK("index.repo.open", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

type repoIndexQueryContext struct {
	Workspace string
	Store     *repoindex.Store
	Freshness repoindex.IndexFreshnessStatus
}

func openFreshRepoIndexQueryContext(ctx context.Context, workspace, command string, allowStale bool, out io.Writer) (repoIndexQueryContext, error) {
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return repoIndexQueryContext{}, fmt.Errorf("resolve workspace: %w", err)
	}

	cfg, err := loadConfig(ctx)
	if err != nil {
		return repoIndexQueryContext{}, fmt.Errorf("load config: %w", err)
	}

	store, err := repoindex.Open(ctx, cfg.Storage.Root, absWorkspace)
	if err != nil {
		return repoIndexQueryContext{}, fmt.Errorf("open repoindex store: %w", err)
	}

	meta, err := store.GetMeta(ctx)
	if err != nil {
		_ = store.Close()
		data := protocol.ErrorData{Hint: "Failed to read repo index metadata. Verify the index path and permissions."}
		env := protocol.Error(command, protocol.ErrorCodeERuntime, "repo index metadata read failed", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		if writeErr := protocol.Write(out, env); writeErr != nil {
			return repoIndexQueryContext{}, fmt.Errorf("write repo index metadata error envelope: %w", writeErr)
		}
		return repoIndexQueryContext{}, fmt.Errorf("get meta: %w", err)
	}
	current := repoindex.ResolveGitSnapshot(ctx, absWorkspace)
	freshness := repoindex.CompareIndexFreshness(meta, current)
	if !repoIndexNavigationFreshnessOK(freshness, allowStale) {
		_ = store.Close()
		data := protocol.ErrorData{
			Hint: "Rebuild the repo index for this workspace or pass --allow-stale to query the stored graph explicitly.",
			Context: map[string]any{
				"freshness":        freshness,
				"store_path":       store.Path(),
				"allow_stale_flag": "--allow-stale",
			},
		}
		env := protocol.Error(command, protocol.ErrorCodeERuntime, "repo index is stale for graph navigation", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		if writeErr := protocol.Write(out, env); writeErr != nil {
			return repoIndexQueryContext{}, fmt.Errorf("write repo index freshness error envelope: %w", writeErr)
		}
		return repoIndexQueryContext{}, fmt.Errorf("repo index freshness is %s", freshness.Level)
	}

	return repoIndexQueryContext{
		Workspace: absWorkspace,
		Store:     store,
		Freshness: freshness,
	}, nil
}

func repoIndexNavigationFreshnessOK(freshness repoindex.IndexFreshnessStatus, allowStale bool) bool {
	if allowStale {
		return true
	}
	return freshness.Level == repoindex.FreshnessCurrent || freshness.Level == repoindex.FreshnessBehind
}

func runIndexRepoTracePath(cmd *cobra.Command, workspace, srcID, dstID string, edgeTypes []string, maxDepth, perNodeCap int, allowStale bool) error {
	ctx := cmd.Context()
	command := "index.repo.trace_path"
	req, err := repoquery.NewTracePathRequest(srcID, dstID, edgeTypes, maxDepth, perNodeCap)
	if err != nil {
		hint := "Use --src-id and --dst-id with stored repoindex node IDs. Use --edge for known edge types when needed."
		data := protocol.ValidationErrorData{Reason: err.Error(), Hint: hint}
		env := protocol.Error(command, protocol.ErrorCodeEARG, err.Error(), data, protocol.WithSource("cli"))
		if writeErr := protocol.Write(cmd.OutOrStdout(), env); writeErr != nil {
			return fmt.Errorf("write repo trace-path argument error envelope: %w", writeErr)
		}
		return fmt.Errorf("build trace path request: %w", err)
	}

	queryCtx, err := openFreshRepoIndexQueryContext(ctx, workspace, command, allowStale, cmd.OutOrStdout())
	if err != nil {
		return err
	}
	defer queryCtx.Store.Close()

	service := repoquery.NewQueryService(repoindex.NewQueryEngine(queryCtx.Store))
	result, err := service.TracePathWithProjection(ctx, req)
	if err != nil {
		return fmt.Errorf("repo query trace path failed: %w", err)
	}

	data := map[string]any{
		"workspace":  queryCtx.Workspace,
		"store_path": queryCtx.Store.Path(),
		"freshness":  queryCtx.Freshness,
		"src_id":     req.SrcID,
		"dst_id":     req.DstID,
		"edges":      repoquery.EdgeTypeValues(repoIndexTracePathEdgeTypes(req.EdgeTypes)),
		"result":     result.Result,
		"anchors":    result.Anchors,
	}

	env := protocol.OK(command, data, protocol.WithSource("cli"), protocol.WithWorkspace(queryCtx.Workspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func runIndexRepoSmartContext(cmd *cobra.Command, workspace, nodeID string, limit int, allowStale bool) error {
	ctx := cmd.Context()
	command := "index.repo.smart_context"
	req, err := repoquery.NewSmartContextRequest(nodeID, limit)
	if err != nil {
		hint := "Use --node-id with a stored repoindex node ID."
		data := protocol.ValidationErrorData{Reason: err.Error(), Hint: hint}
		env := protocol.Error(command, protocol.ErrorCodeEARG, err.Error(), data, protocol.WithSource("cli"))
		if writeErr := protocol.Write(cmd.OutOrStdout(), env); writeErr != nil {
			return fmt.Errorf("write repo smart-context argument error envelope: %w", writeErr)
		}
		return fmt.Errorf("build smart context request: %w", err)
	}

	queryCtx, err := openFreshRepoIndexQueryContext(ctx, workspace, command, allowStale, cmd.OutOrStdout())
	if err != nil {
		return err
	}
	defer queryCtx.Store.Close()

	service := repoquery.NewQueryService(repoindex.NewQueryEngine(queryCtx.Store))
	result, err := service.SmartContextWithProjection(ctx, req)
	if err != nil {
		return fmt.Errorf("repo query smart context failed: %w", err)
	}

	data := map[string]any{
		"workspace":  queryCtx.Workspace,
		"store_path": queryCtx.Store.Path(),
		"freshness":  queryCtx.Freshness,
		"node_id":    req.NodeID,
		"result":     result.Result,
		"anchors":    result.Anchors,
	}

	env := protocol.OK(command, data, protocol.WithSource("cli"), protocol.WithWorkspace(queryCtx.Workspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func runIndexRepoBlastRadius(cmd *cobra.Command, workspace, nodeID string, edgeTypes []string, maxDepth, limit, perNodeCap int, allowStale bool) error {
	ctx := cmd.Context()
	command := "index.repo.blast_radius"
	req, err := repoquery.NewBlastRadiusRequest(nodeID, edgeTypes, maxDepth, limit, perNodeCap)
	if err != nil {
		hint := "Use --node-id with a stored repoindex node ID. Use --edge for known edge types when needed."
		data := protocol.ValidationErrorData{Reason: err.Error(), Hint: hint}
		env := protocol.Error(command, protocol.ErrorCodeEARG, err.Error(), data, protocol.WithSource("cli"))
		if writeErr := protocol.Write(cmd.OutOrStdout(), env); writeErr != nil {
			return fmt.Errorf("write repo blast-radius argument error envelope: %w", writeErr)
		}
		return fmt.Errorf("build blast radius request: %w", err)
	}

	queryCtx, err := openFreshRepoIndexQueryContext(ctx, workspace, command, allowStale, cmd.OutOrStdout())
	if err != nil {
		return err
	}
	defer queryCtx.Store.Close()

	service := repoquery.NewQueryService(repoindex.NewQueryEngine(queryCtx.Store))
	result, err := service.BlastRadiusWithProjection(ctx, req)
	if err != nil {
		return fmt.Errorf("repo query blast radius failed: %w", err)
	}

	data := map[string]any{
		"workspace":  queryCtx.Workspace,
		"store_path": queryCtx.Store.Path(),
		"freshness":  queryCtx.Freshness,
		"node_id":    req.NodeID,
		"edges":      repoquery.EdgeTypeValues(repoIndexBlastRadiusEdgeTypes(req.EdgeTypes)),
		"result":     result.Result,
		"anchors":    result.Anchors,
	}

	env := protocol.OK(command, data, protocol.WithSource("cli"), protocol.WithWorkspace(queryCtx.Workspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func repoIndexTracePathEdgeTypes(edgeTypes []repoindex.EdgeType) []repoindex.EdgeType {
	if len(edgeTypes) == 0 {
		return repoindex.DefaultTracePathEdgeTypes()
	}
	return edgeTypes
}

func repoIndexBlastRadiusEdgeTypes(edgeTypes []repoindex.EdgeType) []repoindex.EdgeType {
	if len(edgeTypes) == 0 {
		return repoindex.DefaultBlastRadiusEdgeTypes()
	}
	return edgeTypes
}

func resolveRepoOpenFallbackID(ctx context.Context, workspace string, service *repoquery.QueryService, id string) (string, error) {
	candidates := repoOpenFallbackQueries(workspace, id)
	for _, candidate := range candidates {
		result, err := service.SearchWithProjection(ctx, repoquery.SearchRequest{
			Query: candidate,
			Limit: 10,
		})
		if err != nil || len(result.Nodes) == 0 {
			continue
		}
		if node, ok := pickBestRepoOpenFallbackNode(result.Nodes, candidate); ok {
			return node.ID, nil
		}
	}
	return "", errors.New("fallback open candidate not found")
}

func repoOpenFallbackQueries(workspace, id string) []string {
	trimmed := strings.TrimSpace(filepath.ToSlash(id))
	if trimmed == "" {
		return nil
	}
	repoBase := filepath.Base(strings.TrimSpace(workspace))
	candidates := make([]string, 0, 8)
	add := func(value string) {
		value = strings.TrimSpace(filepath.ToSlash(value))
		if value == "" {
			return
		}
		for _, existing := range candidates {
			if existing == value {
				return
			}
		}
		candidates = append(candidates, value)
	}
	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == ':' || r == ' ' || r == '\t' || r == '\n'
	})
	for _, part := range parts {
		part = strings.TrimSpace(filepath.ToSlash(part))
		if part == "" {
			continue
		}
		if repoBase != "" {
			if idx := strings.Index(part, repoBase+"/"); idx >= 0 {
				add(part[idx+len(repoBase)+1:])
			}
		}
		for _, marker := range []string{"internal/", "cmd/", "docs/", "deploy/", "configs/", "scripts/", "skills/", "testdata/"} {
			if idx := strings.Index(part, marker); idx >= 0 {
				add(part[idx:])
			}
		}
		if strings.Contains(part, "/") {
			add(part)
			add(filepath.Base(part))
		}
	}
	add(filepath.Base(trimmed))
	return candidates
}

func pickBestRepoOpenFallbackNode(nodes []repoindex.Node, query string) (repoindex.Node, bool) {
	query = filepath.ToSlash(strings.TrimSpace(query))
	if query == "" || len(nodes) == 0 {
		return repoindex.Node{}, false
	}
	bestIdx := -1
	bestScore := -1
	for i, node := range nodes {
		score := repoOpenFallbackScore(node, query)
		if score > bestScore {
			bestIdx = i
			bestScore = score
		}
	}
	if bestIdx < 0 {
		return repoindex.Node{}, false
	}
	return nodes[bestIdx], true
}

func repoOpenFallbackScore(node repoindex.Node, query string) int {
	file := filepath.ToSlash(strings.TrimSpace(node.File))
	id := filepath.ToSlash(strings.TrimSpace(node.ID))
	base := filepath.Base(query)
	switch {
	case file == query:
		return 5
	case strings.HasSuffix(file, "/"+query):
		return 4
	case strings.HasSuffix(id, query):
		return 3
	case base != "" && filepath.Base(file) == base:
		return 2
	case base != "" && filepath.Base(id) == base:
		return 1
	default:
		return 0
	}
}

func runIndexRepoAsk(cmd *cobra.Command, workspace, question, provider, model, apiKey string, maxIterations int, timeout time.Duration) error {
	ctx := cmd.Context()
	start := time.Now()

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	cfg, err := loadConfig(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = strings.TrimSpace(cfg.LLM.Provider)
	}
	if provider == "" {
		provider = "cerebras"
	}

	model = strings.TrimSpace(model)
	if model == "" {
		model = cfg.LLM.ResolveModel(provider)
	}
	if model == "" && provider == "cerebras" {
		model = "zai-4.7"
	}
	if model == "" {
		return fmt.Errorf("LLM model required")
	}

	if apiKey == "" {
		apiKey = cfg.LLM.ResolveAPIKey(provider)
	}
	if apiKey == "" {
		return fmt.Errorf("LLM API key required for provider %s", provider)
	}

	if maxIterations <= 0 {
		maxIterations = 12
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	store, err := repoindex.Open(ctx, cfg.Storage.Root, absWorkspace)
	if err != nil {
		return fmt.Errorf("open repoindex store: %w", err)
	}
	defer store.Close()

	askResult, err := repoqueryadapters.RunAsk(ctx, repoqueryadapters.AskConfig{
		Store:         store,
		Question:      question,
		SystemPrompt:  repoqueryadapters.RepoIndexAskPrompt,
		Provider:      provider,
		Model:         model,
		APIKey:        apiKey,
		MaxIterations: maxIterations,
		Timeout:       timeout,
	})
	if err != nil {
		if emitErr := observability.WriteRepoIndexEvent(ctx, observability.RepoIndexEvent{
			Ts:          time.Now().UTC(),
			Command:     "index.repo.ask",
			WorkspaceID: absWorkspace,
			Source:      "cli",
			QueryHash:   observability.HashQuestion(question),
			Provider:    provider,
			Model:       model,
			DurationMS:  time.Since(start).Milliseconds(),
			Error:       err.Error(),
		}); emitErr != nil {
			fmt.Fprintf(os.Stderr, "observability emit failed: %v\n", emitErr) //nolint:forbidigo // fallback for obs emit failures
		}
		return fmt.Errorf("engine run: %w", err)
	}
	output := askResult.Output
	if string(output.StopReason) == "error" {
		if emitErr := observability.WriteRepoIndexEvent(ctx, observability.RepoIndexEvent{
			Ts:          time.Now().UTC(),
			Command:     "index.repo.ask",
			WorkspaceID: absWorkspace,
			Source:      "cli",
			QueryHash:   observability.HashQuestion(question),
			Provider:    provider,
			Model:       model,
			StopReason:  string(output.StopReason),
			ToolCalls:   len(output.ToolCalls),
			DurationMS:  time.Since(start).Milliseconds(),
			Error:       output.Error,
		}); emitErr != nil {
			fmt.Fprintf(os.Stderr, "observability emit failed: %v\n", emitErr) //nolint:forbidigo // fallback for obs emit failures
		}
		return fmt.Errorf("llm error: %s", output.Error)
	}

	if emitErr := observability.WriteRepoIndexEvent(ctx, observability.RepoIndexEvent{
		Ts:          time.Now().UTC(),
		Command:     "index.repo.ask",
		WorkspaceID: absWorkspace,
		Source:      "cli",
		QueryHash:   observability.HashQuestion(question),
		Provider:    provider,
		Model:       model,
		StopReason:  string(output.StopReason),
		ToolCalls:   len(output.ToolCalls),
		DurationMS:  time.Since(start).Milliseconds(),
	}); emitErr != nil {
		fmt.Fprintf(os.Stderr, "observability emit failed: %v\n", emitErr) //nolint:forbidigo // fallback for obs emit failures
	}

	type toolCallSummary struct {
		ID             string `json:"id,omitempty"`
		Name           string `json:"name,omitempty"`
		Arguments      string `json:"arguments,omitempty"`
		ArgumentsValid bool   `json:"arguments_valid"`
	}

	toolCalls := make([]toolCallSummary, 0, len(output.ToolCalls))
	for _, call := range output.ToolCalls {
		summary := toolCallSummary{
			ID:   call.ID,
			Name: call.Name,
		}
		if len(call.Arguments) > 0 {
			summary.Arguments = string(call.Arguments)
			summary.ArgumentsValid = json.Valid(call.Arguments)
		}
		toolCalls = append(toolCalls, summary)
	}

	data := map[string]any{
		"workspace":    absWorkspace,
		"question":     question,
		"provider":     provider,
		"model":        model,
		"response":     output.AssistantText,
		"tool_calls":   toolCalls,
		"tool_results": output.ToolResults,
		"stop_reason":  output.StopReason,
		"tokens":       output.Tokens,
		"duration_ms":  time.Since(start).Milliseconds(),
	}

	env := protocol.OK("index.repo.ask", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func summaryWorkspaceCandidates(workspace string) []string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(workspace)
	add(filepath.Clean(workspace))
	if abs, err := filepath.Abs(workspace); err == nil {
		add(abs)
		add(filepath.Clean(abs))
	}
	return out
}

func summaryPathCandidates(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(path)
	add(filepath.Clean(path))
	add(filepath.ToSlash(path))
	return out
}

func summarySymbolIDCandidates(symbolID string) []string {
	symbolID = strings.TrimSpace(symbolID)
	if symbolID == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(symbolID)
	add(strings.ReplaceAll(symbolID, "\\", "/"))
	add(filepath.ToSlash(symbolID))
	return out
}
