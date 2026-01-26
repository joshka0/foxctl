package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/observability"
)

// RepoIndexToolExecutor provides repo index tools for LLM access.
type RepoIndexToolExecutor struct {
	engine      *repoindex.QueryEngine
	workspaceID string
}

// NewRepoIndexToolExecutor creates a new repo index tool executor.
func NewRepoIndexToolExecutor(store *repoindex.Store) *RepoIndexToolExecutor {
	var engine *repoindex.QueryEngine
	workspaceID := ""
	if store != nil {
		engine = repoindex.NewQueryEngine(store)
		workspaceID = store.RepoRoot()
	}
	return &RepoIndexToolExecutor{
		engine:      engine,
		workspaceID: workspaceID,
	}
}

// Execute implements ToolExecutor.
func (e *RepoIndexToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if e == nil || e.engine == nil {
		return "", errors.New("repoindex executor not configured")
	}

	switch name {
	case repoindex.ToolSearch:
		return e.executeSearch(ctx, args)
	case repoindex.ToolExpand:
		return e.executeExpand(ctx, args)
	case repoindex.ToolOpen:
		return e.executeOpen(ctx, args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// List implements ToolExecutor.
func (e *RepoIndexToolExecutor) List() []ToolDef {
	return repoIndexToolDefs()
}

type repoIndexSearchInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

const (
	defaultRepoIndexDepth      = 1
	defaultRepoIndexBudget     = 50
	defaultRepoIndexPerNodeCap = 50
)

type repoIndexExpandInput struct {
	Seeds      []string `json:"seeds"`
	EdgeTypes  []string `json:"edge_types,omitempty"`
	Direction  string   `json:"direction,omitempty"`
	Depth      int      `json:"depth,omitempty"`
	Budget     int      `json:"budget,omitempty"`
	PerNodeCap int      `json:"per_node_cap,omitempty"`
}

type repoIndexOpenInput struct {
	ID string `json:"id"`
}

func (e *RepoIndexToolExecutor) executeSearch(ctx context.Context, args json.RawMessage) (string, error) {
	start := time.Now()
	var input repoIndexSearchInput
	if err := json.Unmarshal(args, &input); err != nil {
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolSearch,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return "", fmt.Errorf("parse search args: %w", err)
	}

	query := strings.TrimSpace(input.Query)
	if query == "" {
		err := errors.New("query is required")
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolSearch,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return "", err
	}
	if input.Limit <= 0 {
		input.Limit = 20
	}

	results, err := e.engine.Search(ctx, query, input.Limit)
	ev := observability.RepoIndexEvent{
		Command:     repoindex.ToolSearch,
		Source:      "tool",
		QueryHash:   observability.HashQuestion(query),
		ResultCount: len(results),
		DurationMS:  time.Since(start).Milliseconds(),
	}
	if err != nil {
		ev.Error = err.Error()
	}
	defer e.writeEvent(ctx, ev)
	if err != nil {
		return "", err
	}

	output := map[string]any{
		"count":   len(results),
		"results": results,
	}
	return marshalToolOutput(output)
}

func (e *RepoIndexToolExecutor) executeExpand(ctx context.Context, args json.RawMessage) (string, error) {
	start := time.Now()
	var input repoIndexExpandInput
	if err := json.Unmarshal(args, &input); err != nil {
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolExpand,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return "", fmt.Errorf("parse expand args: %w", err)
	}
	if len(input.Seeds) == 0 {
		err := errors.New("seeds are required")
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolExpand,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return "", err
	}

	edgeTypes := normalizeRepoIndexEdgeTypes(input.EdgeTypes)
	parsedTypes, err := parseRepoIndexEdgeTypes(edgeTypes)
	if err != nil {
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolExpand,
			Source:     "tool",
			SeedCount:  len(input.Seeds),
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return "", err
	}

	direction := repoindex.DirOut
	if input.Direction != "" {
		direction = repoindex.Direction(strings.ToLower(strings.TrimSpace(input.Direction)))
		if direction != repoindex.DirOut && direction != repoindex.DirIn {
			err := fmt.Errorf("invalid direction: %s", input.Direction)
			e.writeEvent(ctx, observability.RepoIndexEvent{
				Command:    repoindex.ToolExpand,
				Source:     "tool",
				SeedCount:  len(input.Seeds),
				EdgeTypes:  edgeTypesFrom(parsedTypes),
				Direction:  string(direction),
				DurationMS: time.Since(start).Milliseconds(),
				Error:      err.Error(),
			})
			return "", err
		}
	}

	depth := input.Depth
	if depth <= 0 {
		depth = defaultRepoIndexDepth
	}
	budget := input.Budget
	if budget <= 0 {
		budget = defaultRepoIndexBudget
	}
	perNodeCap := input.PerNodeCap
	if perNodeCap <= 0 {
		perNodeCap = defaultRepoIndexPerNodeCap
	}

	opts := repoindex.ExpandOptions{
		Direction:  direction,
		EdgeTypes:  parsedTypes,
		Depth:      depth,
		Budget:     budget,
		PerNodeCap: perNodeCap,
	}

	result, err := e.engine.Expand(ctx, input.Seeds, opts)
	ev := observability.RepoIndexEvent{
		Command:     repoindex.ToolExpand,
		Source:      "tool",
		SeedCount:   len(input.Seeds),
		EdgeTypes:   edgeTypesFrom(parsedTypes),
		Direction:   string(direction),
		Depth:       opts.Depth,
		Budget:      opts.Budget,
		PerNodeCap:  opts.PerNodeCap,
		ResultCount: len(result.Nodes),
		DurationMS:  time.Since(start).Milliseconds(),
	}
	if err != nil {
		ev.Error = err.Error()
	}
	defer e.writeEvent(ctx, ev)
	if err != nil {
		return "", err
	}

	output := map[string]any{
		"result": result,
	}
	return marshalToolOutput(output)
}

func (e *RepoIndexToolExecutor) executeOpen(ctx context.Context, args json.RawMessage) (string, error) {
	start := time.Now()
	var input repoIndexOpenInput
	if err := json.Unmarshal(args, &input); err != nil {
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolOpen,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return "", fmt.Errorf("parse open args: %w", err)
	}
	if strings.TrimSpace(input.ID) == "" {
		err := errors.New("id is required")
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolOpen,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return "", err
	}

	node, err := e.engine.Open(ctx, input.ID)
	ev := observability.RepoIndexEvent{
		Command:     repoindex.ToolOpen,
		Source:      "tool",
		NodeID:      input.ID,
		ResultCount: 1,
		DurationMS:  time.Since(start).Milliseconds(),
	}
	if err != nil {
		ev.Error = err.Error()
		if errors.Is(err, repoindex.ErrNotFound) {
			ev.ResultCount = 0
		}
	}
	defer e.writeEvent(ctx, ev)
	if err != nil {
		return "", err
	}

	output := map[string]any{
		"node": node,
	}
	return marshalToolOutput(output)
}

func repoIndexToolDefs() []ToolDef {
	return []ToolDef{
		{
			Name:        repoindex.ToolSearch,
			Description: "Search the repo index for nodes that match a text query.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"FTS query string"},"limit":{"type":"integer","description":"Maximum results","default":20}},"required":["query"]}`),
		},
		{
			Name:        repoindex.ToolExpand,
			Description: "Expand the repo index graph from seed node IDs.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"seeds":{"type":"array","items":{"type":"string"},"description":"Seed node IDs"},"edge_types":{"type":"array","items":{"type":"string"},"description":"Edge types to traverse"},"direction":{"type":"string","enum":["out","in"],"description":"Traversal direction"},"depth":{"type":"integer","description":"Traversal depth","default":1},"budget":{"type":"integer","description":"Max nodes to return","default":50},"per_node_cap":{"type":"integer","description":"Max edges per node per hop","default":50}},"required":["seeds"]}`),
		},
		{
			Name:        repoindex.ToolOpen,
			Description: "Open a repo index node by ID.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"Node ID"}},"required":["id"]}`),
		},
	}
}

func parseRepoIndexEdgeTypes(values []string) ([]repoindex.EdgeType, error) {
	if len(values) == 0 {
		return nil, nil
	}

	allowed := map[string]repoindex.EdgeType{
		string(repoindex.EdgeContains):   repoindex.EdgeContains,
		string(repoindex.EdgeImports):    repoindex.EdgeImports,
		string(repoindex.EdgeRefersTo):   repoindex.EdgeRefersTo,
		string(repoindex.EdgeCalls):      repoindex.EdgeCalls,
		string(repoindex.EdgeImplements): repoindex.EdgeImplements,
		string(repoindex.EdgeEmbeds):     repoindex.EdgeEmbeds,
		string(repoindex.EdgeTests):      repoindex.EdgeTests,
	}

	var types []repoindex.EdgeType
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		upper := strings.ToUpper(trimmed)
		edgeType, ok := allowed[upper]
		if !ok {
			return nil, fmt.Errorf("unknown edge type: %s", value)
		}
		types = append(types, edgeType)
	}

	return types, nil
}

func normalizeRepoIndexEdgeTypes(values []string) []string {
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

func marshalToolOutput(output map[string]any) (string, error) {
	b, err := json.Marshal(output)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func edgeTypesFrom(types []repoindex.EdgeType) []string {
	if len(types) == 0 {
		return nil
	}
	out := make([]string, 0, len(types))
	for _, edgeType := range types {
		out = append(out, string(edgeType))
	}
	return out
}

func (e *RepoIndexToolExecutor) writeEvent(ctx context.Context, ev observability.RepoIndexEvent) {
	if ev.Ts.IsZero() {
		ev.Ts = time.Now().UTC()
	}
	if ev.WorkspaceID == "" {
		ev.WorkspaceID = e.workspaceID
	}
	_ = observability.WriteRepoIndexEvent(ctx, ev)
}

const RepoIndexAskPrompt = "You are a repo index assistant. Use repo.index.search to find multiple relevant nodes, repo.index.expand to map relationships, and repo.index.open for details. Only use edge types: CONTAINS, IMPORTS, REFERS_TO, CALLS, IMPLEMENTS, EMBEDS, TESTS. When answering, list up to 5 relevant files or symbols with node IDs and file paths, plus a 1-2 sentence summary (use node summaries when available). If a tool call fails, retry with valid arguments; if unsure, say so."

type RepoIndexAskConfig struct {
	Store         *repoindex.Store
	Question      string
	Provider      string
	Model         string
	APIKey        string
	MaxIterations int
	Timeout       time.Duration
	SystemPrompt  string
}

type RepoIndexAskResult struct {
	Output EngineOutput
}

func RunRepoIndexAsk(ctx context.Context, cfg RepoIndexAskConfig) (RepoIndexAskResult, error) {
	question := strings.TrimSpace(cfg.Question)
	if question == "" {
		return RepoIndexAskResult{}, errors.New("question is required")
	}
	if cfg.Store == nil {
		return RepoIndexAskResult{}, errors.New("repo index store is required")
	}
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 12
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	systemPrompt := strings.TrimSpace(cfg.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = RepoIndexAskPrompt
	}

	toolExecutor := NewRepoIndexToolExecutor(cfg.Store)
	toolRunner := NewToolRunner(toolExecutor, nil, DefaultToolRunnerConfig())

	engineCfg := LLMChatConfig{
		Provider:      cfg.Provider,
		APIKey:        cfg.APIKey,
		Model:         cfg.Model,
		MaxIterations: cfg.MaxIterations,
		Timeout:       cfg.Timeout,
		StatelessMode: true,
	}

	llmEngine, err := NewLLMChatEngine(engineCfg)
	if err != nil {
		return RepoIndexAskResult{}, err
	}
	llmEngine.SetToolRunner(toolRunner)

	input := EngineInput{
		SystemPrompt: systemPrompt,
		Messages: []Message{
			NewUserMessage(question),
		},
		Tools: toolExecutor.List(),
	}

	output, err := llmEngine.Run(ctx, input)
	if err != nil {
		return RepoIndexAskResult{}, err
	}
	return RepoIndexAskResult{Output: output}, nil
}
