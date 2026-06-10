package env

import (
	"context"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/intelligence/retrieval/memoryrecall"
	"github.com/joshka0/foxctl/internal/storage"
	memorystore "github.com/joshka0/foxctl/internal/storage/memory"
)

const namedMemoryRecallVectorSearchTimeout = 8 * time.Second

type namedMemoryGatherOptions struct {
	CoverageRepair       bool
	RequiredEvidence     []string
	CoverageRequirements []contextengine.CoverageRequirement
}

func (a *ReadOnlyAdapter) retrieveNamedMemory(ctx context.Context, cfg contextengine.LaneConfig, query string, limit int) (contextengine.EvidencePack, bool, error) {
	if strings.TrimSpace(query) == "" {
		return contextengine.EvidencePack{}, false, nil
	}
	if strings.TrimSpace(a.cfg.Storage.Root) == "" || strings.TrimSpace(a.workspaceRoot) == "" {
		return contextengine.EvidencePack{}, false, nil
	}

	memStore, err := memorystore.OpenWithConfig(ctx, a.cfg)
	if err != nil {
		return contextengine.EvidencePack{}, false, nil
	}
	defer func() { _ = memStore.Close() }()

	start := cfg.Clock()
	vectorCtx, cancelVectorSearch := namedMemoryRecallVectorContext(ctx)
	queryEmbedding, embeddingErr := a.embedNamedMemoryQuery(vectorCtx, memStore, cfg.WorkspaceID, query)
	if embeddingErr != nil && ctx.Err() != nil {
		cancelVectorSearch()
		return contextengine.EvidencePack{}, true, embeddingErr
	}
	response, err := memoryrecall.Search(ctx, memStore, memoryrecall.QueryRequest{
		Workspace:      cfg.WorkspaceID,
		Query:          query,
		QueryEmbedding: queryEmbedding,
		EmbeddingError: embeddingErr,
		VectorContext:  vectorCtx,
		Limit:          limit,
	})
	cancelVectorSearch()
	elapsed := cfg.Clock().Sub(start)
	if err != nil {
		return namedMemoryRecallErrorPack(ctx, cfg, query, elapsed, err), true, contextengine.LaneError{Lane: contextengine.LaneMemory, Err: err}
	}

	pack := a.namedMemoryRecallPack(ctx, cfg, query, response, elapsed)
	return pack, true, nil
}

func (a *ReadOnlyAdapter) embedNamedMemoryQuery(ctx context.Context, memStore storage.MemoryStore, workspaceID, query string) ([]float32, error) {
	embedder, err := semantic.NewEmbedderFromConfig(semantic.ScopeMemory, a.cfg)
	if err != nil {
		return nil, err
	}
	result, err := embedder.Embed(ctx, semantic.EnrichQuery(query))
	if err != nil {
		return nil, err
	}
	if err := memStore.ValidateEmbeddingDimensions(ctx, workspaceID, len(result.Vec)); err != nil {
		return nil, err
	}
	return result.Vec, nil
}

func namedMemoryRecallVectorContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if namedMemoryRecallVectorSearchTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, namedMemoryRecallVectorSearchTimeout)
}

func (a *ReadOnlyAdapter) namedMemoryGatherPacksFn(statuses []contextengine.ClaimStatus, scope contextengine.MemoryQueryScope, options namedMemoryGatherOptions) contextengine.ContextPackFunc {
	if scope.HasScope() || !contextengine.MemoryQueryAllowsNamedFallback(contextengine.EffectiveMemoryQueryStatuses(statuses, scope)) {
		return nil
	}
	return func(ctx context.Context, workspaceID, query string, limit int) ([]contextengine.EvidencePack, error) {
		cfg := a.laneConfig()
		if workspaceID = strings.TrimSpace(workspaceID); workspaceID != "" {
			cfg.WorkspaceID = workspaceID
		}
		queries := namedMemoryGatherQueries(query, options)
		packs := make([]contextengine.EvidencePack, 0, len(queries))
		seenRefs := map[string]struct{}{}
		var lastErr error
		for _, item := range queries {
			pack, ok, err := a.retrieveNamedMemory(ctx, cfg, item, limit)
			if err != nil {
				lastErr = err
			}
			if !ok || len(pack.Nodes) == 0 {
				continue
			}
			pack.Nodes = dedupeNamedMemoryPackNodes(pack.Nodes, seenRefs)
			if len(pack.Nodes) == 0 {
				continue
			}
			packs = append(packs, pack)
		}
		return packs, lastErr
	}
}

func namedMemoryGatherQueries(query string, options namedMemoryGatherOptions) []string {
	out := make([]string, 0, 6)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range out {
			if strings.EqualFold(existing, value) {
				return
			}
		}
		out = append(out, value)
	}
	add(query)
	if !options.CoverageRepair {
		return out
	}
	for _, req := range options.CoverageRequirements {
		if strings.EqualFold(strings.TrimSpace(req.Kind), "answer_slot") {
			continue
		}
		add(strings.TrimSpace(strings.Join(append([]string{req.Label}, req.Terms...), " ")))
		if len(out) >= 6 {
			return out
		}
	}
	for _, evidence := range options.RequiredEvidence {
		add(evidence)
		if len(out) >= 6 {
			return out
		}
	}
	return out
}

func dedupeNamedMemoryPackNodes(nodes []contextengine.EvidenceNode, seen map[string]struct{}) []contextengine.EvidenceNode {
	out := make([]contextengine.EvidenceNode, 0, len(nodes))
	for _, node := range nodes {
		key := strings.TrimSpace(contextengine.FormatEvidenceRef(node.Ref))
		if key == "" {
			key = strings.TrimSpace(node.ID)
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, node)
	}
	return out
}

func (a *ReadOnlyAdapter) namedMemoryRecallPack(ctx context.Context, cfg contextengine.LaneConfig, query string, response memoryrecall.QueryResponse, elapsed time.Duration) contextengine.EvidencePack {
	packID := cfg.IDGen()
	nodes := make([]contextengine.EvidenceNode, 0, len(response.Entries))
	suppressedLifecycle := 0
	for _, scored := range response.Entries {
		entry := scored.Entry
		if !memoryrecall.QuerySimilarityAllows(scored.Score, query, memoryrecall.DefaultMinSimilarity) {
			continue
		}
		if !memoryrecall.DefaultLifecycleAllows(entry.LifecycleState, scored.Score, query) {
			suppressedLifecycle++
			continue
		}
		refValue := strings.TrimSpace(entry.Name)
		if refValue == "" {
			refValue = strings.TrimSpace(entry.ID)
		}
		if refValue == "" {
			continue
		}
		nodes = append(nodes, contextengine.EvidenceNode{
			ID:          cfg.IDGen(),
			WorkspaceID: cfg.WorkspaceID,
			NodeType:    contextengine.EvidenceNodeTypeMemory,
			Ref: contextengine.EvidenceRef{
				Type:        contextengine.RefTypeNamedMemory,
				Ref:         refValue,
				WorkspaceID: cfg.WorkspaceID,
			},
			Statement:  memoryrecall.NamedEntryText(entry),
			Confidence: clampScore(scored.Score),
			Grounding:  contextengine.GroundingIndexed,
			Metadata: map[string]any{
				"source":          "named_memory",
				"source_profile":  string(contextengine.SourceProfileMemory),
				"source_id":       refValue,
				"memory_id":       entry.ID,
				"memory_type":     entry.Type,
				"path":            namedMemoryEvidencePath(refValue),
				"search_method":   response.Method,
				"score":           scored.Score,
				"lifecycle_state": entry.LifecycleState,
				"review_status":   entry.ReviewStatus,
				"entities":        entry.Entities,
				"keywords":        entry.Keywords,
			},
		})
	}

	pack := contextengine.EvidencePack{
		ID:          packID,
		WorkspaceID: cfg.WorkspaceID,
		Query:       query,
		Lane:        contextengine.LaneMemory,
		Nodes:       nodes,
		Telemetry: contextengine.EvidenceTelemetry{
			DurationMs: elapsed.Milliseconds(),
		},
		Metadata: map[string]any{
			"source":                  "named_memory",
			"search_method":           response.Method,
			"candidate_count":         len(response.Entries),
			"suppressed_by_lifecycle": suppressedLifecycle,
		},
	}
	if response.Hint != "" {
		pack.Metadata["hint"] = response.Hint
	}
	if len(nodes) > 0 {
		_ = recordNamedMemoryRecallPack(ctx, cfg, &pack, len(nodes), elapsed)
	}
	return pack
}

func namedMemoryEvidencePath(refValue string) string {
	refValue = strings.TrimSpace(refValue)
	if refValue == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(refValue) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	value := strings.Trim(b.String(), "-")
	if value == "" {
		return ""
	}
	if len(value) > 80 {
		value = strings.TrimRight(value[:80], "-")
	}
	return "memory/" + value
}

func namedMemoryRecallErrorPack(ctx context.Context, cfg contextengine.LaneConfig, query string, elapsed time.Duration, err error) contextengine.EvidencePack {
	pack := contextengine.EvidencePack{
		ID:          cfg.IDGen(),
		WorkspaceID: cfg.WorkspaceID,
		Query:       query,
		Lane:        contextengine.LaneMemory,
		Telemetry: contextengine.EvidenceTelemetry{
			DurationMs: elapsed.Milliseconds(),
		},
		Metadata: map[string]any{
			"source": "named_memory",
			"error":  err.Error(),
		},
	}
	_ = recordNamedMemoryRecallPack(ctx, cfg, &pack, 0, elapsed)
	return pack
}

func recordNamedMemoryRecallPack(ctx context.Context, cfg contextengine.LaneConfig, pack *contextengine.EvidencePack, hitCount int, elapsed time.Duration) error {
	if cfg.Store == nil || pack == nil {
		return nil
	}
	episodeID := cfg.IDGen()
	if pack.Metadata == nil {
		pack.Metadata = map[string]any{}
	}
	pack.Metadata["episode_id"] = episodeID
	_, packErr := cfg.Store.PutEvidencePack(ctx, *pack)
	_, episodeErr := cfg.Store.RecordRetrievalEpisode(ctx, contextengine.RetrievalEpisode{
		ID:          episodeID,
		WorkspaceID: cfg.WorkspaceID,
		Query:       pack.Query,
		Lane:        contextengine.LaneMemory,
		PackID:      pack.ID,
		DurationMs:  elapsed.Milliseconds(),
		HitCount:    hitCount,
		CreatedAt:   cfg.Clock(),
	})
	if packErr != nil {
		return packErr
	}
	return episodeErr
}
