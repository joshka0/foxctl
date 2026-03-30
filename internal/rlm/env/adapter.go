package env

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	skillobs "github.com/jkatigb/agentctl/internal/adapters/skillslib/obs"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/platform/config"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/repoquery"
	"github.com/jkatigb/agentctl/internal/rlm"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

// ReadOnlyAdapter executes the first read-only RLM tool surface against current agentctl state.
type ReadOnlyAdapter struct {
	cfg           config.Config
	workspaceRoot string
	vaultPath     string
	companionDB   *sql.DB
	environment   rlm.Environment
	subcall       func(context.Context, rlm.Task, rlm.Environment) (rlm.Result, error)
	telemetry     adapterTelemetry
}

// NewReadOnlyAdapter creates a read-only adapter for one workspace/environment.
func NewReadOnlyAdapter(cfg config.Config, workspaceRoot, vaultPath string, companionDB *sql.DB, env rlm.Environment) *ReadOnlyAdapter {
	return &ReadOnlyAdapter{
		cfg:           cfg,
		workspaceRoot: strings.TrimSpace(workspaceRoot),
		vaultPath:     strings.TrimSpace(vaultPath),
		companionDB:   companionDB,
		environment:   env,
	}
}

// SetSubcall configures the bounded recursive callback for the experimental subcall tool.
func (a *ReadOnlyAdapter) SetSubcall(fn func(context.Context, rlm.Task, rlm.Environment) (rlm.Result, error)) {
	a.subcall = fn
}

type adapterTelemetry struct {
	mu           sync.Mutex
	toolCalls    map[string]int
	toolDuration map[string]int64
	models       map[string]struct{}
	inputTokens  int
	outputTokens int
	totalTokens  int
	totalCostUSD float64
}

func (t *adapterTelemetry) record(name string, duration time.Duration, usage *skillobs.TokenUsage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.toolCalls == nil {
		t.toolCalls = make(map[string]int)
	}
	if t.toolDuration == nil {
		t.toolDuration = make(map[string]int64)
	}
	name = strings.TrimSpace(name)
	if name != "" {
		t.toolCalls[name]++
		t.toolDuration[name] += duration.Milliseconds()
	}
	if usage == nil {
		return
	}
	if t.models == nil {
		t.models = make(map[string]struct{})
	}
	if model := strings.TrimSpace(usage.Model); model != "" {
		t.models[model] = struct{}{}
	}
	t.inputTokens += usage.InputTokens
	t.outputTokens += usage.OutputTokens
	t.totalTokens += usage.TotalTokens
	t.totalCostUSD += usage.TotalCostUSD
}

func (t *adapterTelemetry) snapshot() map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	toolUsage := make(map[string]int, len(t.toolCalls))
	for key, value := range t.toolCalls {
		toolUsage[key] = value
	}
	toolDuration := make(map[string]int64, len(t.toolDuration))
	for key, value := range t.toolDuration {
		toolDuration[key] = value
	}
	models := make([]string, 0, len(t.models))
	for model := range t.models {
		models = append(models, model)
	}
	sort.Strings(models)
	totalToolCalls := 0
	for _, count := range t.toolCalls {
		totalToolCalls += count
	}
	return map[string]any{
		"tool_usage":       toolUsage,
		"tool_duration_ms": toolDuration,
		"total_tool_calls": totalToolCalls,
		"input_tokens":     t.inputTokens,
		"output_tokens":    t.outputTokens,
		"total_tokens":     t.totalTokens,
		"total_cost_usd":   t.totalCostUSD,
		"models":           models,
	}
}

// Execute runs one read-only tool call and returns a typed JSON-like payload.
func (a *ReadOnlyAdapter) Execute(ctx context.Context, name string, args json.RawMessage) (map[string]any, error) {
	switch strings.TrimSpace(name) {
	case "get_top_of_mind":
		return a.getTopOfMind(), nil
	case "get_latest_handoff":
		return a.getLatestHandoff(), nil
	case "search_artifacts":
		return a.searchArtifacts(ctx, args)
	case "load_artifact":
		return a.loadArtifact(ctx, args)
	case "search_repo":
		return a.searchRepo(ctx, args)
	case "semantic_search_code":
		return a.semanticSearchCode(ctx, args)
	case "smart_search_code":
		return a.smartSearchCode(ctx, args)
	case "ripgrep_code":
		return a.ripgrepCode(ctx, args)
	case "expand_repo_graph":
		return a.expandRepoGraph(ctx, args)
	case "load_file":
		return a.loadFile(args)
	case "search_vault":
		return a.searchVault(ctx, args)
	case "read_note":
		return a.readNote(args)
	case "memory_ensemble_retrieve":
		return a.memoryEnsembleRetrieve(ctx, args)
	case "code_search_ensemble":
		return a.codeSearchEnsemble(ctx, args)
	case "search_scenes":
		return a.searchScenes(ctx, args)
	case "get_scene":
		return a.getScene(ctx, args)
	case "subcall":
		return a.subcallTool(ctx, args)
	default:
		return nil, fmt.Errorf("rlm env adapter: unknown tool %q", name)
	}
}

type semanticSearchToolOutput struct {
	Results []struct {
		Path       string  `json:"path"`
		Name       string  `json:"name"`
		Source     string  `json:"source"`
		Summary    string  `json:"summary"`
		Similarity float64 `json:"similarity"`
	} `json:"results"`
	CandidateBundles []struct {
		Key          string   `json:"key"`
		PrimaryPath  string   `json:"primary_path"`
		RelatedPaths []string `json:"related_paths"`
		Symbols      []string `json:"symbols"`
		Sources      []string `json:"sources"`
		Score        float64  `json:"score"`
		MatchReason  string   `json:"match_reason"`
		Ambiguity    string   `json:"ambiguity"`
	} `json:"candidate_bundles"`
}

type smartSearchToolOutput struct {
	Candidates []struct {
		Path   string  `json:"path"`
		Score  float64 `json:"score"`
		Source string  `json:"source"`
	} `json:"candidates"`
}

type contextRipgrepToolOutput struct {
	Preview []struct {
		File       string `json:"file"`
		Language   string `json:"language"`
		StartLine  int    `json:"start_line"`
		EndLine    int    `json:"end_line"`
		SymbolName string `json:"symbol_name"`
		SymbolKind string `json:"symbol_kind"`
		MatchCount int    `json:"match_count"`
	} `json:"preview"`
}

func (a *ReadOnlyAdapter) getTopOfMind() map[string]any {
	if a.environment.TopOfMind == nil {
		return map[string]any{"top_of_mind": nil}
	}
	return map[string]any{"top_of_mind": a.environment.TopOfMind}
}

func (a *ReadOnlyAdapter) getLatestHandoff() map[string]any {
	if a.environment.LatestHandoff == nil {
		return map[string]any{"latest_handoff": nil}
	}
	return map[string]any{"latest_handoff": a.environment.LatestHandoff}
}

func (a *ReadOnlyAdapter) searchArtifacts(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	_ = json.Unmarshal(args, &input)
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}
	query := strings.ToLower(strings.TrimSpace(input.Query))

	handles := make([]string, 0, len(a.environment.ArtifactHandles))
	for _, handle := range a.environment.ArtifactHandles {
		if query == "" || strings.Contains(strings.ToLower(handle), query) {
			handles = append(handles, handle)
		}
		if len(handles) >= limit {
			break
		}
	}

	if strings.TrimSpace(a.cfg.Storage.Root) != "" && strings.TrimSpace(a.workspaceRoot) != "" {
		store, err := trajectory.Open(ctx, a.cfg.Storage.Root)
		if err == nil {
			defer func() { _ = store.Close() }()
			trajs, err := store.ListTrajectories(ctx, trajectory.ListFilter{
				WorkspaceID: ws.ID(a.workspaceRoot),
				Limit:       limit,
			})
			if err == nil {
				for _, traj := range trajs {
					if query == "" || strings.Contains(strings.ToLower(traj.Summary), query) {
						handles = append(handles, "trajectory:"+traj.ID)
					}
					if strings.TrimSpace(traj.ArtifactDigest) != "" && (query == "" || strings.Contains(strings.ToLower(traj.ArtifactDigest), query)) {
						handles = append(handles, "artifact:"+traj.ArtifactDigest)
					}
				}
			}
		}
	}

	return map[string]any{
		"query":   input.Query,
		"results": uniqueStrings(handles),
	}, nil
}

func (a *ReadOnlyAdapter) loadArtifact(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input struct {
		Handle string `json:"handle"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	handle := strings.TrimSpace(input.Handle)
	switch {
	case strings.HasPrefix(handle, "trajectory:"):
		return a.loadTrajectory(ctx, strings.TrimPrefix(handle, "trajectory:"))
	case strings.HasPrefix(handle, "event:"):
		return a.loadTrajectoryEvent(ctx, strings.TrimPrefix(handle, "event:"))
	case strings.HasPrefix(handle, "artifact:"):
		return a.loadCASArtifact(ctx, strings.TrimPrefix(handle, "artifact:"))
	case strings.HasPrefix(handle, "sha256:"):
		return a.loadCASArtifact(ctx, handle)
	case strings.HasPrefix(handle, "path:"):
		return a.loadFile(mustJSON(map[string]any{"path": strings.TrimPrefix(handle, "path:")}))
	case strings.HasPrefix(handle, "note:"):
		return a.readNote(mustJSON(map[string]any{"path": strings.TrimPrefix(handle, "note:")}))
	default:
		return map[string]any{
			"handle":    handle,
			"supported": false,
			"message":   "artifact handle is not recognized by the first runtime",
		}, nil
	}
}

func (a *ReadOnlyAdapter) searchRepo(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	if strings.TrimSpace(a.workspaceRoot) == "" {
		return map[string]any{"results": []any{}}, nil
	}
	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	store, err := repoindex.Open(ctx, a.cfg.Storage.Root, a.workspaceRoot)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	output, err := repoquery.NewQueryService(repoindex.NewQueryEngine(store)).SearchWithProjection(ctx, repoquery.SearchRequest{
		Query: strings.TrimSpace(input.Query),
		Limit: input.Limit,
	})
	if err != nil {
		return nil, err
	}
	anchors := make([]map[string]any, 0, len(output.Anchors))
	for _, anchor := range output.Anchors {
		anchors = append(anchors, map[string]any{
			"path":        anchor.Path,
			"symbol_name": anchor.SymbolName,
			"line_hint":   anchor.LineHint,
			"score":       anchor.Score,
			"summary":     anchor.Summary,
		})
	}
	return map[string]any{
		"query":   input.Query,
		"results": anchors,
	}, nil
}

func (a *ReadOnlyAdapter) semanticSearchCode(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	if strings.TrimSpace(a.workspaceRoot) == "" {
		return map[string]any{"results": []any{}}, nil
	}
	var input struct {
		Query         string   `json:"query"`
		Scope         []string `json:"scope"`
		Profile       string   `json:"profile"`
		Limit         int      `json:"limit"`
		RepoIndexMode string   `json:"repo_index_mode"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	payload := map[string]any{
		"query":           strings.TrimSpace(input.Query),
		"workspace":       a.workspaceRoot,
		"limit":           input.Limit,
		"repo_index_mode": strings.TrimSpace(input.RepoIndexMode),
	}
	if strings.TrimSpace(input.Profile) != "" {
		payload["profile"] = strings.TrimSpace(input.Profile)
	}
	if len(input.Scope) > 0 {
		payload["scope"] = input.Scope
	}
	if vaultPath := firstNonEmpty(strings.TrimSpace(a.vaultPath),
		strings.TrimSpace(os.Getenv("AGENTCTL_RLM_VAULT_PATH")),
		strings.TrimSpace(os.Getenv("AGENTCTL_ACA_VAULT_PATH")),
		strings.TrimSpace(os.Getenv("AGENTCTL_OBSIDIAN_VAULT_PATH")),
	); vaultPath != "" {
		payload["vault_path"] = vaultPath
	}
	var out semanticSearchToolOutput
	if err := a.runCurrentSkillDecode(ctx, "code/semantic_search", payload, &out); err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, len(out.Results))
	for _, result := range out.Results {
		results = append(results, map[string]any{
			"path":       result.Path,
			"name":       result.Name,
			"source":     result.Source,
			"summary":    result.Summary,
			"similarity": result.Similarity,
		})
	}
	candidateBundles := make([]map[string]any, 0, len(out.CandidateBundles))
	for _, bundle := range out.CandidateBundles {
		candidateBundles = append(candidateBundles, map[string]any{
			"key":           bundle.Key,
			"primary_path":  bundle.PrimaryPath,
			"related_paths": append([]string(nil), bundle.RelatedPaths...),
			"symbols":       append([]string(nil), bundle.Symbols...),
			"sources":       append([]string(nil), bundle.Sources...),
			"score":         bundle.Score,
			"match_reason":  bundle.MatchReason,
			"ambiguity":     bundle.Ambiguity,
		})
	}
	return map[string]any{
		"query":             input.Query,
		"results":           results,
		"candidate_bundles": candidateBundles,
	}, nil
}

func (a *ReadOnlyAdapter) smartSearchCode(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	if strings.TrimSpace(a.workspaceRoot) == "" {
		return map[string]any{"results": []any{}}, nil
	}
	var input struct {
		Question      string `json:"question"`
		RepoIndexMode string `json:"repo_index_mode"`
		MaxCandidates int    `json:"max_candidates"`
		MaxSnippets   int    `json:"max_snippets"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	payload := map[string]any{
		"question":        strings.TrimSpace(input.Question),
		"workspace_id":    ws.ID(a.workspaceRoot),
		"repo_index_mode": strings.TrimSpace(input.RepoIndexMode),
	}
	if input.MaxCandidates > 0 || input.MaxSnippets > 0 {
		payload["limits"] = map[string]any{
			"max_candidates": input.MaxCandidates,
			"max_snippets":   input.MaxSnippets,
		}
	}
	var out smartSearchToolOutput
	if err := a.runCurrentSkillDecode(ctx, "code/smart_search", payload, &out); err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, len(out.Candidates))
	for _, candidate := range out.Candidates {
		results = append(results, map[string]any{
			"path":   candidate.Path,
			"score":  candidate.Score,
			"source": candidate.Source,
		})
	}
	return map[string]any{
		"question": input.Question,
		"results":  results,
	}, nil
}

func (a *ReadOnlyAdapter) ripgrepCode(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	if strings.TrimSpace(a.workspaceRoot) == "" {
		return map[string]any{"results": []any{}}, nil
	}
	var input struct {
		Pattern    string   `json:"pattern"`
		Path       string   `json:"path"`
		Glob       []string `json:"glob"`
		GlobNot    []string `json:"glob_not"`
		MaxMatches int      `json:"max_matches"`
		MaxBlocks  int      `json:"max_blocks"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	payload := map[string]any{
		"pattern":     strings.TrimSpace(input.Pattern),
		"path":        strings.TrimSpace(input.Path),
		"glob":        input.Glob,
		"glob_not":    input.GlobNot,
		"max_matches": input.MaxMatches,
		"max_blocks":  input.MaxBlocks,
	}
	var out contextRipgrepToolOutput
	if err := a.runCurrentSkillDecode(ctx, "code/context_ripgrep", payload, &out); err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, len(out.Preview))
	for _, preview := range out.Preview {
		results = append(results, map[string]any{
			"path":        preview.File,
			"language":    preview.Language,
			"start_line":  preview.StartLine,
			"end_line":    preview.EndLine,
			"symbol_name": preview.SymbolName,
			"symbol_kind": preview.SymbolKind,
			"match_count": preview.MatchCount,
		})
	}
	return map[string]any{
		"pattern": input.Pattern,
		"results": results,
	}, nil
}

func (a *ReadOnlyAdapter) expandRepoGraph(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	if strings.TrimSpace(a.workspaceRoot) == "" {
		return map[string]any{"results": []any{}}, nil
	}
	var input struct {
		Seed   string `json:"seed"`
		Depth  int    `json:"depth"`
		Budget int    `json:"budget"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	seed := strings.TrimSpace(strings.TrimPrefix(input.Seed, "repo:"))
	if seed == "" {
		return nil, fmt.Errorf("seed is required")
	}
	store, err := repoindex.Open(ctx, a.cfg.Storage.Root, a.workspaceRoot)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	output, err := repoquery.NewQueryService(repoindex.NewQueryEngine(store)).ExpandWithProjection(ctx, repoquery.ExpandRequest{
		Seeds:      []string{seed},
		EdgeTypes:  repoindex.EdgeSetStructural,
		Direction:  repoindex.DirOut,
		Depth:      input.Depth,
		Budget:     input.Budget,
		PerNodeCap: 20,
	})
	if err != nil {
		return nil, err
	}
	anchors := make([]map[string]any, 0, len(output.Anchors))
	for _, anchor := range output.Anchors {
		anchors = append(anchors, map[string]any{
			"path":        anchor.Path,
			"symbol_name": anchor.SymbolName,
			"line_hint":   anchor.LineHint,
			"summary":     anchor.Summary,
		})
	}
	return map[string]any{
		"seed":    seed,
		"results": anchors,
	}, nil
}

func (a *ReadOnlyAdapter) loadFile(args json.RawMessage) (map[string]any, error) {
	var input struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	path := strings.TrimSpace(input.Path)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	fullPath := path
	if !filepath.IsAbs(path) {
		fullPath = filepath.Join(a.workspaceRoot, path)
	}
	body, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"path":       path,
		"full_path":  fullPath,
		"content":    sliceLines(string(body), input.StartLine, input.EndLine),
		"start_line": input.StartLine,
		"end_line":   input.EndLine,
	}, nil
}

func (a *ReadOnlyAdapter) searchVault(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	vaultPath := firstNonEmpty(strings.TrimSpace(a.vaultPath),
		strings.TrimSpace(os.Getenv("AGENTCTL_RLM_VAULT_PATH")),
		strings.TrimSpace(os.Getenv("AGENTCTL_ACA_VAULT_PATH")),
		strings.TrimSpace(os.Getenv("AGENTCTL_OBSIDIAN_VAULT_PATH")),
	)
	if vaultPath == "" {
		return map[string]any{"results": []any{}}, nil
	}
	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	index, err := obsidianindex.Open(ctx, a.cfg.Storage.Root, vaultPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = index.Close() }()
	hits, err := index.SearchNotes(ctx, strings.TrimSpace(input.Query), input.Limit)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(hits))
	for _, hit := range hits {
		out = append(out, map[string]any{
			"path":    hit.Path,
			"title":   hit.Title,
			"type":    hit.Type,
			"trust":   hit.Trust,
			"score":   hit.Score,
			"snippet": hit.Snippet,
		})
	}
	return map[string]any{
		"query":   input.Query,
		"results": out,
	}, nil
}

func (a *ReadOnlyAdapter) readNote(args json.RawMessage) (map[string]any, error) {
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	vaultPath := firstNonEmpty(strings.TrimSpace(a.vaultPath),
		strings.TrimSpace(os.Getenv("AGENTCTL_RLM_VAULT_PATH")),
		strings.TrimSpace(os.Getenv("AGENTCTL_ACA_VAULT_PATH")),
		strings.TrimSpace(os.Getenv("AGENTCTL_OBSIDIAN_VAULT_PATH")),
	)
	if vaultPath == "" {
		return nil, fmt.Errorf("vault path not configured")
	}
	path := strings.TrimSpace(strings.TrimPrefix(input.Path, "note:"))
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	fullPath := path
	if !filepath.IsAbs(path) {
		fullPath = filepath.Join(vaultPath, path)
	}
	body, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"path":      path,
		"full_path": fullPath,
		"content":   string(body),
	}, nil
}

func (a *ReadOnlyAdapter) searchScenes(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	if a.companionDB == nil {
		return map[string]any{"results": []any{}}, nil
	}
	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}
	query := "%" + strings.ToLower(strings.TrimSpace(input.Query)) + "%"
	rows, err := a.companionDB.QueryContext(ctx, `
		SELECT id, conversation_id, summary
		FROM companion_soft_episodes
		WHERE lower(summary) LIKE ?
		ORDER BY id DESC
		LIMIT ?`, query, limit)
	if err != nil {
		return map[string]any{"results": []any{}}, nil
	}
	defer rows.Close()
	out := make([]map[string]any, 0, limit)
	for rows.Next() {
		var id int64
		var conversationID, summary string
		if err := rows.Scan(&id, &conversationID, &summary); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"handle":          fmt.Sprintf("episode:%d", id),
			"conversation_id": conversationID,
			"summary":         summary,
		})
	}
	return map[string]any{"results": out}, nil
}

func (a *ReadOnlyAdapter) getScene(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	if a.companionDB == nil {
		return map[string]any{"scene": nil}, nil
	}
	var input struct {
		Handle string `json:"handle"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	handle := strings.TrimSpace(input.Handle)
	switch {
	case strings.HasPrefix(handle, "episode:"):
		id := strings.TrimPrefix(handle, "episode:")
		row := a.companionDB.QueryRowContext(ctx, `
			SELECT id, conversation_id, summary
			FROM companion_soft_episodes
			WHERE id = ?`, id)
		var episodeID int64
		var conversationID, summary string
		if err := row.Scan(&episodeID, &conversationID, &summary); err != nil {
			if err == sql.ErrNoRows {
				return map[string]any{"scene": nil}, nil
			}
			return nil, err
		}
		return map[string]any{
			"scene": map[string]any{
				"handle":          fmt.Sprintf("episode:%d", episodeID),
				"conversation_id": conversationID,
				"summary":         summary,
			},
		}, nil
	case strings.HasPrefix(handle, "conversation:"):
		conversationID := strings.TrimPrefix(handle, "conversation:")
		rows, err := a.companionDB.QueryContext(ctx, `
			SELECT id, role, content
			FROM companion_turns
			WHERE conversation_id = ?
			ORDER BY created_at DESC
			LIMIT 5`, conversationID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		turns := make([]map[string]any, 0, 5)
		for rows.Next() {
			var id, role, content string
			if err := rows.Scan(&id, &role, &content); err != nil {
				return nil, err
			}
			turns = append(turns, map[string]any{
				"id":      id,
				"role":    role,
				"content": content,
			})
		}
		return map[string]any{
			"scene": map[string]any{
				"handle":          handle,
				"conversation_id": conversationID,
				"turns":           turns,
			},
		}, nil
	default:
		return map[string]any{"scene": nil}, nil
	}
}

func (a *ReadOnlyAdapter) loadTrajectory(ctx context.Context, trajectoryID string) (map[string]any, error) {
	if strings.TrimSpace(a.cfg.Storage.Root) == "" || strings.TrimSpace(a.workspaceRoot) == "" {
		return map[string]any{"trajectory": nil}, nil
	}
	store, err := trajectory.Open(ctx, a.cfg.Storage.Root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	traj, err := store.GetTrajectory(ctx, ws.ID(a.workspaceRoot), strings.TrimSpace(trajectoryID))
	if err != nil {
		return map[string]any{"trajectory": nil}, nil
	}
	events, _ := store.ListEvents(ctx, trajectory.EventFilter{TrajectoryID: traj.ID, Limit: 20})
	return map[string]any{
		"trajectory": traj,
		"events":     events,
	}, nil
}

func (a *ReadOnlyAdapter) loadTrajectoryEvent(ctx context.Context, handle string) (map[string]any, error) {
	parts := strings.SplitN(strings.TrimSpace(handle), ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid event handle")
	}
	trajID := strings.TrimSpace(parts[0])
	eventID := strings.TrimSpace(parts[1])
	if strings.TrimSpace(a.cfg.Storage.Root) == "" {
		return map[string]any{"event": nil}, nil
	}
	store, err := trajectory.Open(ctx, a.cfg.Storage.Root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	events, err := store.ListEvents(ctx, trajectory.EventFilter{TrajectoryID: trajID, Limit: 200})
	if err != nil {
		return nil, err
	}
	for _, event := range events {
		if strings.TrimSpace(event.ID) == eventID {
			return map[string]any{"event": event}, nil
		}
	}
	return map[string]any{"event": nil}, nil
}

func (a *ReadOnlyAdapter) loadCASArtifact(ctx context.Context, digest string) (map[string]any, error) {
	if strings.TrimSpace(a.cfg.Paths.CAS) == "" {
		return map[string]any{"artifact": nil}, nil
	}
	store, err := cas.NewStore(a.cfg.Paths.CAS)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	reader, meta, err := store.Get(ctx, strings.TrimSpace(digest))
	if err != nil {
		return map[string]any{"artifact": nil}, nil
	}
	defer func() { _ = reader.Close() }()
	body, err := io.ReadAll(io.LimitReader(reader, 64*1024))
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"artifact": map[string]any{
			"digest":  meta.Digest,
			"size":    meta.Size,
			"kind":    meta.Kind,
			"content": string(body),
		},
	}, nil
}

func (a *ReadOnlyAdapter) subcallTool(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	if a.subcall == nil {
		return map[string]any{
			"supported": false,
			"message":   "subcall is not configured",
		}, nil
	}
	var input struct {
		Prompt          string   `json:"prompt"`
		Role            string   `json:"role"`
		RepoHandles     []string `json:"repo_handles"`
		VaultHandles    []string `json:"vault_handles"`
		SceneHandles    []string `json:"scene_handles"`
		ArtifactHandles []string `json:"artifact_handles"`
		MaxDepth        int      `json:"max_depth"`
		MaxIterations   int      `json:"max_iterations"`
		MaxSubcalls     int      `json:"max_subcalls"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	childEnv := a.environment
	if len(input.RepoHandles) > 0 {
		childEnv.RepoHandles = uniqueStrings(input.RepoHandles)
	}
	if len(input.VaultHandles) > 0 {
		childEnv.VaultHandles = uniqueStrings(input.VaultHandles)
	}
	if len(input.SceneHandles) > 0 {
		childEnv.SceneHandles = uniqueStrings(input.SceneHandles)
	}
	if len(input.ArtifactHandles) > 0 {
		childEnv.ArtifactHandles = uniqueStrings(input.ArtifactHandles)
	}
	result, err := a.subcall(ctx, rlm.Task{
		Prompt:        strings.TrimSpace(input.Prompt),
		Role:          NormalizeScoutRole(input.Role),
		WorkspaceRoot: a.workspaceRoot,
		MaxDepth:      input.MaxDepth,
		MaxIterations: input.MaxIterations,
		MaxSubcalls:   input.MaxSubcalls,
	}, childEnv)
	if err != nil {
		return nil, err
	}
	return map[string]any{"result": result}, nil
}

func mustJSON(value map[string]any) json.RawMessage {
	body, _ := json.Marshal(value)
	return body
}

func (a *ReadOnlyAdapter) runCurrentSkillDecode(ctx context.Context, skill string, payload map[string]any, dst any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	start := time.Now()
	result := a.runSkillDirect(runCtx, skill, body)
	if result.Err != nil {
		repoRoot, repoErr := resolveAgentctlRepoRoot()
		if repoErr != nil {
			a.telemetry.record(skill, time.Since(start), nil)
			return fmt.Errorf("run %s: %w%s", skill, result.Err, formatCmdFailureStderr(result))
		}
		executable, execErr := os.Executable()
		if execErr != nil {
			executable = "agentctl"
		}
		searchPaths := resolveSkillSearchPaths(a.cfg, a.workspaceRoot)
		env := []string{}
		if len(searchPaths) > 0 {
			env = append(env, "AGENTCTL_SKILLS_PATH="+strings.Join(searchPaths, string(os.PathListSeparator)))
		}
		result = executil.RunWithInputEnv(runCtx, repoRoot, executable, env, body, "run", skill, "--input-file", "-")
		if result.Err != nil {
			a.telemetry.record(skill, time.Since(start), nil)
			return fmt.Errorf("run %s: %w%s", skill, result.Err, formatCmdFailureStderr(result))
		}
	}
	env, err := protocol.DecodeEnvelope(result.Stdout)
	if err != nil {
		a.telemetry.record(skill, time.Since(start), nil)
		return err
	}
	if env.Status == envelope.StatusError {
		a.telemetry.record(skill, time.Since(start), extractSkillTokenUsage(env.Data))
		return protocol.EnvelopeStatusErrorFromEnvelope(env)
	}
	a.telemetry.record(skill, time.Since(start), extractSkillTokenUsage(env.Data))
	if err := protocol.DecodeEnvelopeDataInto(env, dst); err != nil {
		return err
	}
	return nil
}

func (a *ReadOnlyAdapter) runSkillDirect(ctx context.Context, skillName string, input []byte) executil.CmdResult {
	manifestPath, artifact, err := a.resolvePreferredSkillArtifact(skillName)
	if err != nil {
		return executil.CmdResult{Err: err, ExitCode: -1}
	}
	return executil.RunWithInput(ctx, filepath.Dir(manifestPath), artifact, input)
}

func resolveSkillSearchPaths(cfg config.Config, workspaceRoot string) []string {
	paths := append([]string{}, skill.EnvSearchPaths()...)
	paths = append(paths, workspaceSkillSearchPaths(workspaceRoot)...)
	if strings.TrimSpace(cfg.Paths.Skills) != "" {
		paths = append(paths, cfg.Paths.Skills)
	}
	paths = append(paths, skill.UserSearchPaths()...)
	paths = append(paths, skill.BuiltinSearchPaths()...)
	paths = append(paths, skill.DevSearchPaths()...)
	return skill.NormalizeSearchPaths(paths)
}

func workspaceSkillSearchPaths(workspaceRoot string) []string {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return nil
	}
	return skill.NormalizeSearchPaths([]string{
		filepath.Join(root, "skills"),
		filepath.Join(root, "dist", "skills"),
	})
}

func (a *ReadOnlyAdapter) resolvePreferredSkillArtifact(skillName string) (string, string, error) {
	skillName = strings.TrimSpace(skillName)
	if skillName == "" {
		return "", "", fmt.Errorf("skill name is required")
	}
	normalized := skill.NormalizeSkillName(skillName)
	preferredRoots := []string{}
	preferredRoots = append(preferredRoots, skill.EnvSearchPaths()...)
	preferredRoots = append(preferredRoots, workspaceSkillSearchPaths(a.workspaceRoot)...)
	if strings.TrimSpace(a.cfg.Paths.Skills) != "" {
		preferredRoots = append(preferredRoots, a.cfg.Paths.Skills)
	}
	preferredRoots = append(preferredRoots, skill.UserSearchPaths()...)
	for _, root := range skill.NormalizeSearchPaths(preferredRoots) {
		dir := filepath.Join(root, normalized)
		manifestPath := filepath.Join(dir, "skill.yaml")
		if _, err := os.Stat(manifestPath); err != nil {
			continue
		}
		manifest, artifact, err := skill.LoadManifestAndArtifact(manifestPath, skill.ArtifactOptions{PreferCGO: true})
		if err != nil {
			continue
		}
		if manifest.Distribution.Type != "exec" {
			continue
		}
		return manifestPath, artifact, nil
	}

	resolver := skill.NewResolver(skill.WithSearchPaths(resolveSkillSearchPaths(a.cfg, a.workspaceRoot)...))
	handle, err := resolver.Resolve(skillName)
	if err != nil {
		return "", "", err
	}
	manifest, artifact, err := skill.LoadManifestAndArtifact(handle.ManifestPath, skill.ArtifactOptions{PreferCGO: true})
	if err != nil {
		return "", "", err
	}
	if manifest.Distribution.Type != "exec" {
		return "", "", fmt.Errorf("unsupported non-exec skill %q", skillName)
	}
	return handle.ManifestPath, artifact, nil
}

func extractSkillTokenUsage(data any) *skillobs.TokenUsage {
	if data == nil {
		return nil
	}
	body, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	if usage := usageFromMap(payload); usage != nil {
		return usage
	}
	if summary, ok := payload["summary"].(map[string]any); ok {
		model := stringValue(summary["model"])
		tokens := intValue(summary["tokens_used"])
		if tokens > 0 || model != "" {
			return &skillobs.TokenUsage{
				Model:       model,
				TotalTokens: tokens,
			}
		}
	}
	if stats, ok := payload["stats"].(map[string]any); ok {
		if usage := usageFromMap(stats); usage != nil {
			return usage
		}
	}
	return nil
}

func formatCmdFailureStderr(result executil.CmdResult) string {
	stderr := strings.TrimSpace(string(result.Stderr))
	if stderr == "" {
		return ""
	}
	if len(stderr) > 240 {
		stderr = stderr[:240] + "..."
	}
	return ": stderr=" + strconv.Quote(stderr)
}

func usageFromMap(m map[string]any) *skillobs.TokenUsage {
	if m == nil {
		return nil
	}
	if raw, ok := m["token_usage"].(map[string]any); ok {
		return buildTokenUsage(raw)
	}
	if raw, ok := m["usage"].(map[string]any); ok {
		return buildTokenUsage(raw)
	}
	return nil
}

func buildTokenUsage(m map[string]any) *skillobs.TokenUsage {
	if m == nil {
		return nil
	}
	usage := &skillobs.TokenUsage{
		Model:         stringValue(m["model"]),
		InputTokens:   intValue(m["input_tokens"]),
		OutputTokens:  intValue(m["output_tokens"]),
		TotalTokens:   intValue(m["total_tokens"]),
		InputCostUSD:  floatValue(m["input_cost_usd"]),
		OutputCostUSD: floatValue(m["output_cost_usd"]),
		TotalCostUSD:  floatValue(m["total_cost_usd"]),
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	if usage.Model == "" && usage.TotalTokens == 0 && usage.TotalCostUSD == 0 {
		return nil
	}
	return usage
}

func floatValue(v any) float64 {
	switch value := v.(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int32:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}

func resolveAgentctlRepoRoot() (string, error) {
	if _, file, _, ok := runtime.Caller(0); ok {
		candidate := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
		if _, err := os.Stat(filepath.Join(candidate, "skills", "code_semantic_search", "main.go")); err == nil {
			return candidate, nil
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "skills", "code_semantic_search", "main.go")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("resolve agentctl repo root: could not locate skills/code_semantic_search/main.go")
}

func sliceLines(content string, start, end int) string {
	if start <= 0 && end <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	if start <= 0 {
		start = 1
	}
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if start > len(lines) || start > end {
		return ""
	}
	return strings.Join(lines[start-1:end], "\n")
}
