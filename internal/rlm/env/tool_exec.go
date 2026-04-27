package env

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/contextengine/adapters"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	ctxengstore "github.com/joshka0/foxctl/internal/storage/contextengine"
	"github.com/joshka0/foxctl/internal/storage/tasks"
)

// retrieveLaneInput is the shared input shape for the 5 retrieval composite tools.
type retrieveLaneInput struct {
	Query  string `json:"query"`
	Limit  int    `json:"limit,omitempty"`
	TaskID string `json:"task_id,omitempty"`
}

// loadEvidenceRefInput is the input shape for load_evidence_ref.
type loadEvidenceRefInput struct {
	Ref       string `json:"ref"`
	MaxTokens int    `json:"max_tokens,omitempty"`
}

// laneRetrievalStore is a no-op fallback when the SQLite store is unavailable.
// Episodes are recorded best-effort; a missing store must never break retrieval.
type laneRetrievalStore struct{}

func (laneRetrievalStore) RecordRetrievalEpisode(_ context.Context, ep contextengine.RetrievalEpisode) (contextengine.RetrievalEpisode, error) {
	return ep, nil
}

func (a *ReadOnlyAdapter) laneConfig() contextengine.LaneConfig {
	var store contextengine.RetrievalStore = laneRetrievalStore{}
	if a.ceStore != nil {
		store = a.ceStore
	}
	wsID := ""
	if a.workspaceRoot != "" {
		wsID = ws.ID(a.workspaceRoot)
	}
	return contextengine.LaneConfig{
		Store:       store,
		IDGen:       newIDGen(),
		Clock:       func() time.Time { return time.Now().UTC() },
		WorkspaceID: wsID,
	}
}

func newIDGen() contextengine.IDGen {
	return func() string {
		var buf [12]byte
		_, _ = rand.Read(buf[:])
		return "ce-" + hex.EncodeToString(buf[:])
	}
}

// codeSearchFn returns a CodeSearchFunc backed by the existing semantic_search_code skill.
func (a *ReadOnlyAdapter) codeSearchFn(limit int) contextengine.CodeSearchFunc {
	return func(ctx context.Context, query string) ([]contextengine.CodeSearchHit, error) {
		if strings.TrimSpace(a.workspaceRoot) == "" {
			return nil, nil
		}
		args := mustJSON(map[string]any{"query": query, "limit": limit})
		out, err := a.semanticSearchCode(ctx, args)
		if err != nil {
			return nil, err
		}
		results, _ := out["results"].([]map[string]any)
		hits := make([]contextengine.CodeSearchHit, 0, len(results))
		for _, r := range results {
			hit := contextengine.CodeSearchHit{
				Path:    stringValue(r["path"]),
				Snippet: stringValue(r["summary"]),
				Symbol:  stringValue(r["name"]),
			}
			if sim, ok := r["similarity"].(float64); ok {
				hit.Score = sim
			}
			hits = append(hits, hit)
		}
		return hits, nil
	}
}

// memoryQueryFn returns a MemoryQueryFunc backed by the contextengine claim store.
// If the SQLite store is unavailable, returns no claims.
func (a *ReadOnlyAdapter) memoryQueryFn(limit int) contextengine.MemoryQueryFunc {
	return func(ctx context.Context, workspaceID, query string) ([]contextengine.MemoryClaim, error) {
		if a.ceStore == nil {
			return nil, nil
		}
		// Fetch with no SQL-side query filtering; ClaimFilter does not yet
		// support substring search. We fetch up to a generous cap then
		// filter in memory by case-insensitive substring match on the
		// claim's textual fields.
		fetchLimit := limit
		if query != "" && fetchLimit > 0 {
			// When filtering, broaden the fetch so the post-filter result
			// can still satisfy the requested limit.
			fetchLimit = limit * 10
			if fetchLimit > 1000 {
				fetchLimit = 1000
			}
		}
		claims, err := a.ceStore.ListClaims(ctx, ctxengstore.ClaimFilter{
			WorkspaceID: workspaceID,
			Status:      contextengine.ClaimStatusCurrent,
			Limit:       fetchLimit,
		})
		if err != nil {
			return nil, err
		}
		q := strings.TrimSpace(strings.ToLower(query))
		if q == "" {
			return claims, nil
		}
		filtered := make([]contextengine.MemoryClaim, 0, len(claims))
		for _, c := range claims {
			if claimMatchesQuery(c, q) {
				filtered = append(filtered, c)
				if limit > 0 && len(filtered) >= limit {
					break
				}
			}
		}
		return filtered, nil
	}
}

// claimMatchesQuery reports whether any of the claim's searchable text
// fields contain the lowercased substring q. Caller must lowercase q.
func claimMatchesQuery(c contextengine.MemoryClaim, q string) bool {
	if q == "" {
		return true
	}
	if strings.Contains(strings.ToLower(c.Summary), q) {
		return true
	}
	if strings.Contains(strings.ToLower(c.ClaimType), q) {
		return true
	}
	if strings.Contains(strings.ToLower(c.Reason), q) {
		return true
	}
	if strings.Contains(strings.ToLower(c.Scope.Path), q) {
		return true
	}
	return false
}

// contextQueryFn returns a ContextQueryFunc that synthesizes a ContextPacket
// from the environment's TopOfMind projection.
func (a *ReadOnlyAdapter) contextQueryFn() contextengine.ContextQueryFunc {
	return func(_ context.Context, workspaceID string) (*contextengine.ContextPacket, error) {
		top := a.environment.TopOfMind
		if top == nil {
			return nil, nil
		}
		packet := &contextengine.ContextPacket{
			WorkspaceID: workspaceID,
			Objective:   stringValue(top["objective"]),
			Phase:       stringValue(top["phase"]),
		}
		return packet, nil
	}
}

// taskQueryFn returns a TaskQueryFunc backed by the tasks SQLite store. It
// fetches a task by ID and projects it into a TaskContext via the existing
// adapters.ConvertTask helper. If the task store is unavailable, the task is
// missing, or it does not match the workspace, returns nil with no error so
// the lane records an empty pack rather than a failure.
func (a *ReadOnlyAdapter) taskQueryFn() contextengine.TaskQueryFunc {
	return func(ctx context.Context, workspaceID, taskID string) (*contextengine.TaskContext, error) {
		if a.taskStore == nil || strings.TrimSpace(taskID) == "" {
			return nil, nil
		}
		t, err := a.taskStore.Get(ctx, taskID)
		if err != nil {
			return nil, nil
		}
		if workspaceID != "" && t.WorkspaceID != "" && t.WorkspaceID != workspaceID {
			return nil, nil
		}
		tc := adapters.ConvertTask(t)
		return &tc, nil
	}
}

// taskListFn returns a TaskListFunc backed by the tasks SQLite store. It lists
// active and recently-completed tasks for the workspace, then applies a plain
// case-insensitive substring filter against title/description/scope/notes
// using the supplied query. The query is captured at the call site
// (retrieveTask / retrieveMixed) since TaskListFunc itself does not accept a
// query parameter.
func (a *ReadOnlyAdapter) taskListFn(query string) contextengine.TaskListFunc {
	return func(ctx context.Context, workspaceID string) ([]string, error) {
		if a.taskStore == nil {
			return nil, nil
		}
		opts := tasks.ListOptions{
			Statuses: []string{
				tasks.StatusPending,
				tasks.StatusInProgress,
				tasks.StatusReadyForReview,
				tasks.StatusBlocked,
				tasks.StatusCompleted,
			},
			Limit: 100,
		}
		all, err := a.taskStore.ListWithOptions(ctx, workspaceID, opts)
		if err != nil {
			return nil, err
		}
		needle := strings.ToLower(strings.TrimSpace(query))
		out := make([]string, 0, len(all))
		for _, t := range all {
			if !taskMatchesQuery(t, needle) {
				continue
			}
			out = append(out, t.ID)
		}
		return out, nil
	}
}

// taskMatchesQuery reports whether any of the task's searchable text fields
// contain the lowercased substring needle. An empty needle matches every
// task. Caller must pre-lowercase needle.
func taskMatchesQuery(t tasks.Task, needle string) bool {
	if needle == "" {
		return true
	}
	fields := []string{t.Title, t.Description, t.AtomicDescription, t.ScopePath, t.Notes, t.PlanSection}
	for _, f := range fields {
		if f == "" {
			continue
		}
		if strings.Contains(strings.ToLower(f), needle) {
			return true
		}
	}
	return false
}

func (a *ReadOnlyAdapter) retrieveCode(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input retrieveLaneInput
	_ = json.Unmarshal(args, &input)
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}
	cfg := a.laneConfig()
	pack, err := contextengine.RetrieveCode(ctx, cfg, a.codeSearchFn(limit), strings.TrimSpace(input.Query))
	if err != nil {
		return nil, err
	}
	return packToMap(pack), nil
}

func (a *ReadOnlyAdapter) retrieveMemory(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input retrieveLaneInput
	_ = json.Unmarshal(args, &input)
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}
	cfg := a.laneConfig()
	pack, err := contextengine.RetrieveMemory(ctx, cfg, a.memoryQueryFn(limit), strings.TrimSpace(input.Query))
	if err != nil {
		return nil, err
	}
	return packToMap(pack), nil
}

func (a *ReadOnlyAdapter) retrieveContext(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input retrieveLaneInput
	_ = json.Unmarshal(args, &input)
	cfg := a.laneConfig()
	pack, err := contextengine.RetrieveContext(ctx, cfg, a.contextQueryFn(), strings.TrimSpace(input.Query))
	if err != nil {
		return nil, err
	}
	return packToMap(pack), nil
}

func (a *ReadOnlyAdapter) retrieveTask(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input retrieveLaneInput
	_ = json.Unmarshal(args, &input)
	cfg := a.laneConfig()
	query := strings.TrimSpace(input.Query)
	pack, err := contextengine.RetrieveTask(ctx, cfg, a.taskQueryFn(), a.taskListFn(query), strings.TrimSpace(input.TaskID), query)
	if err != nil {
		return nil, err
	}
	return packToMap(pack), nil
}

func (a *ReadOnlyAdapter) retrieveMixed(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input retrieveLaneInput
	_ = json.Unmarshal(args, &input)
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}
	cfg := a.laneConfig()
	query := strings.TrimSpace(input.Query)
	pack, err := contextengine.RetrieveMixed(
		ctx, cfg,
		a.codeSearchFn(limit),
		a.memoryQueryFn(limit),
		a.contextQueryFn(),
		a.taskQueryFn(),
		a.taskListFn(query),
		strings.TrimSpace(input.TaskID),
		query,
	)
	if err != nil {
		return nil, err
	}
	return packToMap(pack), nil
}

// loadEvidenceRef resolves a typed EvidenceRef and returns its bounded body.
func (a *ReadOnlyAdapter) loadEvidenceRef(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	var input loadEvidenceRefInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	refStr := strings.TrimSpace(input.Ref)
	if refStr == "" {
		return nil, fmt.Errorf("ref is required")
	}
	ref, err := contextengine.ParseEvidenceRef(refStr)
	if err != nil {
		return map[string]any{
			"ref":     refStr,
			"error":   err.Error(),
			"loaded":  false,
		}, nil
	}
	switch ref.Type {
	case contextengine.RefTypePath:
		return a.loadFile(mustJSON(map[string]any{"path": ref.Ref}))
	case contextengine.RefTypeNote:
		return a.readNote(mustJSON(map[string]any{"path": ref.Ref}))
	case contextengine.RefTypeArtifact, contextengine.RefTypeTrajectory:
		return a.loadArtifact(ctx, mustJSON(map[string]any{"handle": refStr}))
	case contextengine.RefTypeMemoryClaim:
		if a.ceStore == nil {
			return map[string]any{"ref": refStr, "loaded": false, "error": "contextengine store unavailable"}, nil
		}
		claim, err := a.ceStore.GetClaim(ctx, ref.Ref)
		if err != nil {
			return map[string]any{"ref": refStr, "loaded": false, "error": err.Error()}, nil
		}
		return map[string]any{
			"ref":    refStr,
			"loaded": true,
			"claim":  claim,
		}, nil
	default:
		return map[string]any{
			"ref":    refStr,
			"loaded": false,
			"error":  "unsupported ref type for load_evidence_ref",
		}, nil
	}
}

// packToMap renders an EvidencePack as a JSON-friendly map.
func packToMap(pack contextengine.EvidencePack) map[string]any {
	nodes := make([]map[string]any, 0, len(pack.Nodes))
	for _, node := range pack.Nodes {
		nodes = append(nodes, map[string]any{
			"id":          node.ID,
			"node_type":   string(node.NodeType),
			"ref":         contextengine.FormatEvidenceRef(node.Ref),
			"ref_type":    string(node.Ref.Type),
			"ref_value":   node.Ref.Ref,
			"statement":   node.Statement,
			"confidence":  node.Confidence,
			"grounding":   string(node.Grounding),
			"metadata":    node.Metadata,
		})
	}
	return map[string]any{
		"id":           pack.ID,
		"workspace_id": pack.WorkspaceID,
		"query":        pack.Query,
		"lane":         string(pack.Lane),
		"nodes":        nodes,
		"telemetry": map[string]any{
			"duration_ms": pack.Telemetry.DurationMs,
			"lanes_fused": pack.Telemetry.LanesFused,
		},
		"metadata": pack.Metadata,
	}
}
