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

	"github.com/jkatigb/agentctl/internal/engine"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/indexing/symbol"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/memory"
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
	var includeTS bool
	var includeTests bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build the repo graph index",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexRepoBuild(cmd, workspace, patterns, includeGo, includeTS, includeTests, dryRun)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringSliceVar(&patterns, "go-pattern", []string{"./..."}, "Go package patterns to index")
	cmd.Flags().BoolVar(&includeGo, "go", true, "Include Go sources")
	cmd.Flags().BoolVar(&includeTS, "typescript", true, "Include TypeScript sources")
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

func runIndexRepoBuild(cmd *cobra.Command, workspace string, patterns []string, includeGo, includeTS, includeTests, dryRun bool) error {
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
		IncludeTypescript:     includeTS,
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

	data := map[string]any{
		"workspace":   absWorkspace,
		"store_path":  store.Path(),
		"result":      result,
		"duration_ms": time.Since(start).Milliseconds(),
		"dry_run":     dryRun,
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

	queryEngine := repoindex.NewQueryEngine(store)
	results, err := queryEngine.Search(ctx, query, limit)
	if err != nil {
		return fmt.Errorf("queryEngine.Search failed for query=%q limit=%d: %w", query, limit, err)
	}

	data := map[string]any{
		"workspace": absWorkspace,
		"query":     query,
		"count":     len(results),
		"results":   results,
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

	edgeTypes = normalizeEdgeTypes(edgeTypes)
	parsedTypes, err := parseRepoEdgeTypes(edgeTypes)
	if err != nil {
		hint := "Use --edge with known types (CONTAINS, IMPORTS, REFERS_TO, CALLS, IMPLEMENTS, EMBEDS, TESTS)."
		data := protocol.ErrorData{Hint: hint}
		env := protocol.Error("index.repo.expand", protocol.ErrorCodeEARG, err.Error(), data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		if writeErr := protocol.Write(cmd.OutOrStdout(), env); writeErr != nil {
			return fmt.Errorf("write repo index expand error envelope: %w", writeErr)
		}
		return fmt.Errorf("parse edge types: %w", err)
	}
	dir := repoindex.Direction(strings.ToLower(direction))
	if dir != repoindex.DirOut && dir != repoindex.DirIn {
		return fmt.Errorf("invalid direction: %s", direction)
	}
	opts := repoindex.ExpandOptions{
		Direction:  dir,
		EdgeTypes:  parsedTypes,
		Depth:      depth,
		Budget:     budget,
		PerNodeCap: perNodeCap,
	}

	queryEngine := repoindex.NewQueryEngine(store)
	result, err := queryEngine.Expand(ctx, seeds, opts)
	if err != nil {
		return fmt.Errorf("queryEngine.Expand failed: %w", err)
	}

	data := map[string]any{
		"workspace": absWorkspace,
		"seeds":     seeds,
		"edges":     edgeTypes,
		"result":    result,
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

	queryEngine := repoindex.NewQueryEngine(store)
	node, err := queryEngine.Open(ctx, id)
	if err != nil {
		return fmt.Errorf("queryEngine.Open failed for id %q: %w", id, err)
	}

	data := map[string]any{
		"workspace": absWorkspace,
		"node":      node,
	}

	env := protocol.OK("index.repo.open", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

const repoIndexAskPrompt = "You are a repo index navigator. Use repo.index.search, repo.index.expand, and repo.index.open to answer questions. Prefer search to find seed nodes, expand for relationships, and open nodes for details. Include node IDs and file paths when citing results."

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

	askResult, err := engine.RunRepoIndexAsk(ctx, engine.RepoIndexAskConfig{
		Store:         store,
		Question:      question,
		SystemPrompt:  repoIndexAskPrompt,
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
			fmt.Fprintf(os.Stderr, "observability emit failed: %v\n", emitErr)
		}
		return fmt.Errorf("engine run: %w", err)
	}
	output := askResult.Output
	if output.StopReason == engine.StopReasonError {
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
			fmt.Fprintf(os.Stderr, "observability emit failed: %v\n", emitErr)
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
		fmt.Fprintf(os.Stderr, "observability emit failed: %v\n", emitErr)
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

func parseRepoEdgeTypes(values []string) ([]repoindex.EdgeType, error) {
	if len(values) == 0 {
		return nil, nil
	}

	allowed := map[string]struct{}{
		string(repoindex.EdgeContains):   {},
		string(repoindex.EdgeImports):    {},
		string(repoindex.EdgeRefersTo):   {},
		string(repoindex.EdgeCalls):      {},
		string(repoindex.EdgeImplements): {},
		string(repoindex.EdgeEmbeds):     {},
		string(repoindex.EdgeTests):      {},
	}

	var types []repoindex.EdgeType
	for _, value := range values {
		if value == "" {
			continue
		}
		value = strings.ToUpper(value)
		if _, ok := allowed[value]; !ok {
			return nil, fmt.Errorf("unknown edge type: %s", value)
		}
		types = append(types, repoindex.EdgeType(value))
	}

	return types, nil
}

func normalizeEdgeTypes(values []string) []string {
	var result []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			result = append(result, part)
		}
	}
	return result
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

func (p *memorySymbolSummaryProvider) Summary(ctx context.Context, symbolID string) (string, error) {
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
			entryName := symbol.SymbolSummaryEntryName(workspace, id)
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
