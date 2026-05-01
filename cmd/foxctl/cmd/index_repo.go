package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
	"github.com/joshka0/foxctl/internal/intelligence/repoquery"
	repoqueryadapters "github.com/joshka0/foxctl/internal/intelligence/repoquery/adapters"
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
		newIndexRepoStatusCommand(),
		newIndexRepoSearchCommand(),
		newIndexRepoExpandCommand(),
		newIndexRepoOpenCommand(),
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
	var includeTS bool
	var includeElixir bool
	var includeTerraform bool
	var includeKubernetes bool
	var includeShell bool
	var includeTests bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build the repo graph index",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexRepoBuild(cmd, workspace, patterns, includeGo, includePython, includeRust, includeTS, includeElixir, includeTerraform, includeKubernetes, includeShell, includeTests, dryRun)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringSliceVar(&patterns, "go-pattern", []string{"./..."}, "Go package patterns to index")
	cmd.Flags().BoolVar(&includeGo, "go", true, "Include Go sources")
	cmd.Flags().BoolVar(&includePython, "python", false, "Include Python sources")
	cmd.Flags().BoolVar(&includeRust, "rust", false, "Include Rust sources")
	cmd.Flags().BoolVar(&includeTS, "typescript", true, "Include TypeScript sources")
	cmd.Flags().BoolVar(&includeElixir, "elixir", false, "Include Elixir sources")
	cmd.Flags().BoolVar(&includeTerraform, "terraform", false, "Include Terraform files as file/concept graph components")
	cmd.Flags().BoolVar(&includeKubernetes, "kubernetes", false, "Include Kubernetes YAML manifests as file/resource graph components")
	cmd.Flags().BoolVar(&includeShell, "shell", false, "Include shell scripts as file/command graph components")
	cmd.Flags().BoolVar(&includeTests, "include-tests", false, "Include test files")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Build without writing to the index")

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

	cmd := &cobra.Command{
		Use:   "expand",
		Short: "Expand the graph from seed nodes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexRepoExpand(cmd, workspace, seeds, edgeTypes, depth, budget, perNodeCap, direction)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringSliceVar(&seeds, "seed", nil, "Seed node IDs (repeatable)")
	cmd.Flags().StringSliceVar(&edgeTypes, "edge", nil, "Edge types to traverse (repeatable)")
	cmd.Flags().IntVar(&depth, "depth", 1, "Traversal depth")
	cmd.Flags().IntVar(&budget, "budget", 50, "Max nodes to return")
	cmd.Flags().IntVar(&perNodeCap, "per-node", 50, "Max edges per node per hop")
	cmd.Flags().StringVar(&direction, "direction", string(repoindex.DirOut), "Traversal direction: out or in")
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

func runIndexRepoBuild(cmd *cobra.Command, workspace string, patterns []string, includeGo, includePython, includeRust, includeTS, includeElixir, includeTerraform, includeKubernetes, includeShell, includeTests, dryRun bool) error {
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

	casDir := cfg.Paths.CAS
	if casDir == "" {
		casDir = filepath.Join(cfg.Home, "cas")
	}
	memStore, err := memory.Open(ctx, cfg.Storage.Root, casDir)
	if err != nil {
		return fmt.Errorf("open memory store: %w", err)
	}
	defer memStore.Close()

	store, err := repoindex.Open(ctx, cfg.Storage.Root, absWorkspace)
	if err != nil {
		return fmt.Errorf("open repoindex store: %w", err)
	}
	defer store.Close()

	summaryProvider := &memorySummaryProvider{store: memStore, workspace: absWorkspace}
	symbolSummaryProvider := &memorySymbolSummaryProvider{store: memStore, workspace: absWorkspace}
	builder := repoindex.NewBuilder(store, absWorkspace)
	result, err := builder.Build(ctx, repoindex.BuildOptions{
		RepoRoot:              absWorkspace,
		Patterns:              patterns,
		IncludeTests:          includeTests,
		IncludeGo:             includeGo,
		IncludePython:         includePython,
		IncludeRust:           includeRust,
		IncludeTypescript:     includeTS,
		IncludeElixir:         includeElixir,
		IncludeTerraform:      includeTerraform,
		IncludeKubernetes:     includeKubernetes,
		IncludeShell:          includeShell,
		DryRun:                dryRun,
		SummaryProvider:       summaryProvider,
		SymbolSummaryProvider: symbolSummaryProvider,
	})
	if err != nil {
		hint := "Verify repo index configuration, input files, and permissions."
		data := protocol.ErrorData{Hint: hint}
		env := protocol.Error("index.repo.build", protocol.ErrorCodeERuntime, "repo index build failed", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		if writeErr := protocol.Write(cmd.OutOrStdout(), env); writeErr != nil {
			return fmt.Errorf("write repo index build error envelope: %w", writeErr)
		}
		return fmt.Errorf("repo index build failed: %w", err)

	}
	meta, metaErr := store.GetMeta(ctx)

	data := map[string]any{
		"workspace":   absWorkspace,
		"store_path":  store.Path(),
		"result":      result,
		"duration_ms": time.Since(start).Milliseconds(),
		"dry_run":     dryRun,
	}
	if metaErr == nil && !dryRun {
		data["meta"] = meta
	}

	env := protocol.OK("index.repo.build", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
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
	if currentHead := repoindex.ResolveGitHead(ctx, absWorkspace); currentHead != "" {
		data["current_head_sha"] = currentHead
		data["index_matches_head"] = currentHead == meta.HeadSHA
	}
	data["current_worktree_dirty"] = repoindex.ResolveGitDirty(ctx, absWorkspace)

	env := protocol.OK("index.repo.status", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
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

func runIndexRepoExpand(cmd *cobra.Command, workspace string, seeds, edgeTypes []string, depth, budget, perNodeCap int, direction string) error {
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
	result, err := service.ExpandWithProjection(ctx, req)
	if err != nil {
		return fmt.Errorf("repo query expand failed: %w", err)
	}

	data := map[string]any{
		"workspace": absWorkspace,
		"seeds":     req.Seeds,
		"edges":     repoquery.EdgeTypeValues(req.EdgeTypes),
		"result":    result.Result,
		"anchors":   result.Anchors,
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

type memorySummaryProvider struct {
	store     *memory.Store
	workspace string
}

func (p *memorySummaryProvider) Summary(ctx context.Context, filePath string) (string, error) {
	if p == nil || p.store == nil {
		return "", fmt.Errorf("summary provider not configured")
	}
	workspaces := summaryWorkspaceCandidates(p.workspace)
	paths := summaryPathCandidates(filePath)
	if len(workspaces) == 0 || len(paths) == 0 {
		return "", fmt.Errorf("summary provider missing workspace or file path")
	}
	var lastErr error
	for _, workspace := range workspaces {
		for _, path := range paths {
			entryName := symbol.FileSummaryEntryName(workspace, path)
			entry, err := p.store.Get(ctx, entryName, workspace)
			if err != nil {
				if errors.Is(err, memory.ErrNotFound) {
					lastErr = err
					continue
				}
				return "", err
			}
			return entry.Summary, nil
		}
	}
	if lastErr == nil {
		lastErr = memory.ErrNotFound
	}
	return "", lastErr
}

// memorySymbolSummaryProvider resolves symbol summaries from named memory.
type memorySymbolSummaryProvider struct {
	store     *memory.Store
	workspace string
}

func (p *memorySymbolSummaryProvider) Summary(ctx context.Context, symbolID, symbolKey, pkg string) (string, error) {
	if p == nil || p.store == nil {
		return "", fmt.Errorf("symbol summary provider not configured")
	}
	workspaces := summaryWorkspaceCandidates(p.workspace)
	ids := summarySymbolIDCandidates(symbolID)
	if len(workspaces) == 0 || len(ids) == 0 {
		return "", fmt.Errorf("symbol summary provider missing workspace or symbol ID")
	}
	var lastErr error
	for _, workspace := range workspaces {
		for _, id := range ids {
			if pkg != "" && symbolKey != "" {
				keyEntryName := symbol.SymbolSummaryKeyEntryName(workspace, pkg, symbolKey)
				entry, err := p.store.Get(ctx, keyEntryName, workspace)
				if err == nil {
					return entry.Summary, nil
				}
				if !errors.Is(err, memory.ErrNotFound) {
					return "", err
				}
			}
			// Legacy fallback: try old entry name format
			entryName := symbol.SymbolSummaryEntryName(workspace, id)
			entry, err := p.store.Get(ctx, entryName, workspace)
			if err == nil {
				return entry.Summary, nil
			}
			if errors.Is(err, memory.ErrNotFound) {
				lastErr = err
				continue
			}
			return "", err
		}
	}
	if lastErr == nil {
		lastErr = memory.ErrNotFound
	}
	return "", lastErr
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
