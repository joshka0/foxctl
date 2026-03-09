package retrievalv2

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/repoquery"
	"github.com/jkatigb/agentctl/internal/searchindex"
	"github.com/jkatigb/agentctl/internal/searchquery"
)

// Engine runs a retrieval-v2 request end-to-end.
type Engine struct {
	index    SearchIndex
	repo     RepoQueryService
	embedder semantic.EmbeddingProvider
}

// NewEngine builds a v2 Engine from a search index and optional embedder.
func NewEngine(index SearchIndex, embedder semantic.EmbeddingProvider) *Engine {
	return &Engine{index: index, embedder: embedder}
}

// WithRepoQueryService attaches an optional repoquery service for structural recall.
func (e *Engine) WithRepoQueryService(service *repoquery.QueryService) *Engine {
	if e == nil {
		return nil
	}
	e.repo = service
	return e
}

// Search returns fused and optionally grouped retrieval results.
func (e *Engine) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	if e == nil {
		return SearchResponse{}, ErrNoIndex
	}
	if e.index == nil {
		return SearchResponse{}, ErrNoIndex
	}
	if strings.TrimSpace(req.WorkspaceID) == "" {
		return SearchResponse{}, ErrNoWorkspace
	}
	if strings.TrimSpace(req.Query) == "" {
		return SearchResponse{}, ErrNoQuery
	}

	request := e.withDefaults(req)
	parsed := searchquery.ParseQuery(request.Query)
	lexicalQuery := searchquery.ComposeLexicalQuery(parsed)
	if strings.TrimSpace(lexicalQuery) == "" {
		lexicalQuery = strings.TrimSpace(parsed.Raw)
	}

	parsedQuery := ParsedQuery{
		Raw:          request.Query,
		LexicalQuery: lexicalQuery,
		Plan:         parsed,
	}

	stats := SearchStats{Sources: map[SourceID]SourceStats{}}
	sourceGroups := map[SourceID][]SourceHit{}

	embedding, embeddingErr := e.embedQuery(ctx, request.Query, request.QueryEmbeddingMode)
	if embeddingErr != nil && request.Sources.EnableVector {
		stats.Sources[SourceVector] = SourceStats{Err: embeddingErr, Requested: request.Sources.VectorLimit}
	}
	vectorAvailable := len(embedding) > 0

	sources := []Source{
		ExactSource{},
		LexicalSource{},
		VectorSource{},
	}

	for _, source := range sources {
		if !source.Enabled(request, vectorAvailable) {
			continue
		}

		sourceID := source.ID()
		limit := request.Sources.LexicalLimit
		minScore := request.Sources.LexicalMinScore
		query := lexicalQuery
		if sourceID == SourceExact {
			limit = request.Sources.ExactLimit
			minScore = 0
		} else if sourceID == SourceVector {
			limit = request.Sources.VectorLimit
			minScore = request.Sources.VectorMinScore
			query = ""
		} else if sourceID == SourceRepoIndex {
			limit = request.Sources.RepoIndexLimit
			minScore = 0
		}
		if limit == 0 {
			if sourceID == SourceVector {
				limit = request.Sources.VectorLimit
			} else {
				limit = request.Sources.LexicalLimit
			}
		}
		stats.Sources[sourceID] = SourceStats{Requested: limit}

		candidates, err := source.Recall(ctx, SourceCall{
			WorkspaceID: request.WorkspaceID,
			Query:       query,
			Limit:       limit,
			MinScore:    minScore,
			Index:       e.index,
			RepoQuery:   e.repo,
			Embedding:   embedding,
			Model:       request.Sources.VectorModel,
			RepoIndexMode: request.Sources.RepoIndexMode,
		})
		if err != nil {
			ts := stats.Sources[sourceID]
			ts.Err = err
			stats.Sources[sourceID] = ts
			continue
		}

		sourceGroups[sourceID] = candidates
		ts := stats.Sources[sourceID]
		ts.Returned = len(candidates)
		stats.Sources[sourceID] = ts
		stats.TotalRaw += len(candidates)
	}

	if request.Sources.EnableRepoIndex && shouldRunRepoIndex(parsed, sourceGroups) {
		source := RepoIndexSource{}
		sourceID := source.ID()
		limit := request.Sources.RepoIndexLimit
		stats.Sources[sourceID] = SourceStats{Requested: limit}
		candidates, err := source.Recall(ctx, SourceCall{
			WorkspaceID:   request.WorkspaceID,
			Query:         lexicalQuery,
			Limit:         limit,
			MinScore:      0,
			Index:         e.index,
			RepoQuery:     e.repo,
			Embedding:     embedding,
			Model:         request.Sources.VectorModel,
			RepoIndexMode: request.Sources.RepoIndexMode,
		})
		if err != nil {
			ts := stats.Sources[sourceID]
			ts.Err = err
			stats.Sources[sourceID] = ts
		} else {
			sourceGroups[sourceID] = candidates
			ts := stats.Sources[sourceID]
			ts.Returned = len(candidates)
			stats.Sources[sourceID] = ts
			stats.TotalRaw += len(candidates)
		}
	}

	if request.Sources.EnableVector && !vectorAvailable {
		if _, ok := stats.Sources[SourceVector]; !ok {
			stats.Sources[SourceVector] = SourceStats{Requested: request.Sources.VectorLimit, Returned: 0}
		}
	}

	effectiveFuse := tuneFuseForPlan(parsed, request.Fuse)
	fused, _ := Fuse(sourceGroups, effectiveFuse)
	fused = applyFeatureBoosts(parsed, fused)
	stats.TotalFused = len(fused)

	if request.MaxResults > 0 && len(fused) > request.MaxResults {
		fused = fused[:request.MaxResults]
		stats.TotalFused = len(fused)
	}

	groups := []Group{}
	if request.Group.Enabled {
		groups = GroupResults(fused, request.Group)
		stats.TotalGrouped = len(groups)
	}

	return SearchResponse{
		Query:    request.Query,
		Plan:     parsedQuery,
		Hits:     fused,
		Groups:   groups,
		Stats:    stats,
		Embedded: len(embedding) > 0,
	}, nil
}

func (e *Engine) withDefaults(req SearchRequest) SearchRequest {
	_ = e
	if req.MaxResults <= 0 {
		req.MaxResults = DefaultSearchRequest(req.WorkspaceID, req.Query).MaxResults
	}
	if req.Sources == (SearchSourcesConfig{}) {
		req.Sources = DefaultSearchSourcesConfig()
	}
	if req.Fuse.Mode == "" && req.Fuse.TopK == 0 && req.Fuse.RRFK == 0 && req.Fuse.SourceWeights == nil && req.Fuse.MaxContributors == 0 {
		req.Fuse = DefaultFuseOptions()
	} else {
		if req.Fuse.TopK == 0 {
			req.Fuse.TopK = 60
		}
		if req.Fuse.RRFK == 0 {
			req.Fuse.RRFK = 60
		}
		if req.Fuse.SourceWeights == nil {
			req.Fuse.SourceWeights = DefaultFuseOptions().SourceWeights
		}
		if req.Fuse.MaxContributors == 0 {
			req.Fuse.MaxContributors = 4
		}
		if req.Fuse.Mode == "" {
			req.Fuse.Mode = FuseModeRRF
		}
	}
	if req.Group == (GroupOptions{}) {
		req.Group = DefaultGroupOptions()
	}
	if req.Sources.LexicalLimit == 0 {
		req.Sources.LexicalLimit = DefaultSearchSourcesConfig().LexicalLimit
	}
	if req.Sources.ExactLimit == 0 {
		req.Sources.ExactLimit = DefaultSearchSourcesConfig().ExactLimit
	}
	if req.Sources.VectorLimit == 0 {
		req.Sources.VectorLimit = DefaultSearchSourcesConfig().VectorLimit
	}
	if req.Sources.RepoIndexLimit == 0 {
		req.Sources.RepoIndexLimit = DefaultSearchSourcesConfig().RepoIndexLimit
	}
	if req.Sources.RepoIndexMode == "" {
		req.Sources.RepoIndexMode = DefaultSearchSourcesConfig().RepoIndexMode
	}
	if req.QueryEmbeddingMode == "" {
		req.QueryEmbeddingMode = QueryEmbeddingModeAuto
	}
	if !req.Sources.EnableExact && !req.Sources.EnableLexical && !req.Sources.EnableVector && !req.Sources.EnableRepoIndex {
		req.Sources = DefaultSearchSourcesConfig()
	}
	return req
}

func (e *Engine) embedQuery(ctx context.Context, query string, mode QueryEmbeddingMode) ([]float32, error) {
	if e.embedder == nil {
		return nil, nil
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}

	switch mode {
	case QueryEmbeddingModeEmbed:
		return e.embedder.Embed(ctx, q)
	case QueryEmbeddingModeEmbedQuery:
		if qp, ok := e.embedder.(semantic.QueryEmbeddingProvider); ok {
			return qp.EmbedQuery(ctx, q)
		}
		return e.embedder.Embed(ctx, q)
	case QueryEmbeddingModeAuto:
		if qp, ok := e.embedder.(semantic.QueryEmbeddingProvider); ok {
			return qp.EmbedQuery(ctx, q)
		}
		return e.embedder.Embed(ctx, q)
	default:
		return nil, fmt.Errorf("unsupported query embedding mode %q", mode)
	}
}

func shouldRunRepoIndex(plan searchquery.QueryPlan, sourceGroups map[SourceID][]SourceHit) bool {
	if looksStructuralPlan(plan) {
		return true
	}
	if len(sourceGroups[SourceExact]) > 0 {
		return false
	}
	if len(plan.Identifiers) > 0 {
		if len(sourceGroups[SourceLexical]) > 0 || len(sourceGroups[SourceVector]) > 0 {
			return false
		}
	}

	lexicalHits := sourceGroups[SourceLexical]
	vectorHits := sourceGroups[SourceVector]
	if len(lexicalHits) == 0 && len(vectorHits) == 0 {
		return true
	}
	if len(lexicalHits) < 3 {
		return true
	}
	if len(vectorHits) > 0 && vectorHits[0].Score < 0.35 {
		return true
	}
	if looksSparsePlan(plan) {
		return true
	}
	return false
}

func looksSparsePlan(plan searchquery.QueryPlan) bool {
	if len(plan.PathHints) > 0 {
		return true
	}
	if len(plan.Identifiers) > 0 {
		return false
	}
	return len(plan.Terms) <= 2
}

func looksStructuralPlan(plan searchquery.QueryPlan) bool {
	q := strings.ToLower(strings.TrimSpace(plan.Raw))
	if q == "" {
		return false
	}
	structuralTokens := []string{
		"call", "calls", "caller", "callee", "flow", "graph", "dag", "expand",
		"relationship", "chain", "refers", "imports", "depends", "used by",
	}
	for _, token := range structuralTokens {
		if strings.Contains(q, token) {
			return true
		}
	}
	return false
}

func isGenericStructuralIdentifier(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "dag", "graph", "flow", "call", "calls", "grep", "edge", "edges", "repo", "index", "expand":
		return true
	default:
		return false
	}
}

func applyFeatureBoosts(plan searchquery.QueryPlan, hits []FusedHit) []FusedHit {
	structural := looksStructuralPlan(plan)
	for i := range hits {
		bonus := 0.0
		doc := hits[i].Document
		for _, id := range plan.Identifiers {
			if structural && isGenericStructuralIdentifier(strings.ToLower(strings.TrimSpace(id.Value))) {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(doc.SymbolName), strings.TrimSpace(id.Value)) {
				if structural {
					bonus += 0.08
				} else {
					bonus += 0.25
				}
				break
			}
			if strings.EqualFold(strings.TrimSpace(doc.Title), strings.TrimSpace(id.Value)) {
				if structural {
					bonus += 0.05
				} else {
					bonus += 0.20
				}
				break
			}
		}
		for _, hint := range plan.PathHints {
			if hint.Path != "" && strings.Contains(strings.ToLower(doc.Path), strings.ToLower(hint.Path)) {
				bonus += 0.35
				break
			}
		}
		if doc.Kind == searchindex.KindSymbol {
			bonus += 0.03
		}
		if structural {
			if _, ok := hits[i].SourceScores[SourceRepoIndex]; ok {
				bonus += 0.12
			}
			if strings.Contains(strings.ToLower(doc.Path), "repoindex") || strings.Contains(strings.ToLower(doc.Path), "repoquery") {
				bonus += 0.08
			}
		}
		hits[i].Score += bonus
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].Document.Path < hits[j].Document.Path
		}
		return hits[i].Score > hits[j].Score
	})
	return hits
}

func tuneFuseForPlan(plan searchquery.QueryPlan, opts FuseOptions) FuseOptions {
	if !looksStructuralPlan(plan) {
		return opts
	}
	tuned := opts
	tuned.SourceWeights = copySourceWeights(opts.SourceWeights)
	if tuned.SourceWeights == nil {
		tuned.SourceWeights = DefaultFuseOptions().SourceWeights
	}
	tuned.SourceWeights[SourceExact] = 0.85
	tuned.SourceWeights[SourceLexical] = 1.0
	tuned.SourceWeights[SourceVector] = 0.8
	tuned.SourceWeights[SourceRepoIndex] = 1.9
	return tuned
}

func copySourceWeights(in map[SourceID]float64) map[SourceID]float64 {
	if in == nil {
		return nil
	}
	out := make(map[SourceID]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
