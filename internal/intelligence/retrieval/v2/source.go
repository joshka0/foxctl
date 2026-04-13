package retrievalv2

import (
	"context"
	"strings"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/intelligence/repoquery"
	repoqueryadapters "github.com/jkatigb/agentctl/internal/intelligence/repoquery/adapters"
	"github.com/jkatigb/agentctl/internal/intelligence/searchindex"
)

// Source fetches recall hits for one retrieval input and one upstream source.
type Source interface {
	ID() SourceID
	Enabled(request SearchRequest, hasEmbedding bool) bool
	Recall(ctx context.Context, cfg SourceCall) ([]SourceHit, error)
}

// SourceCall passes required data into a source recall call.
type SourceCall struct {
	WorkspaceID   string
	Query         string
	Limit         int
	MinScore      float64
	Index         SearchIndex
	RepoQuery     RepoQueryService
	Embedding     []float32
	Model         string
	RepoIndexMode string
}

// ExactSource executes exact symbol/title/path recall from searchindex.
type ExactSource struct{}

func (ExactSource) ID() SourceID { return SourceExact }

func (ExactSource) Enabled(request SearchRequest, _ bool) bool {
	return request.Sources.EnableExact
}

func (ExactSource) Recall(ctx context.Context, cfg SourceCall) ([]SourceHit, error) {
	if cfg.Index == nil || cfg.Query == "" || cfg.Limit <= 0 {
		return nil, nil
	}
	hits, err := cfg.Index.ExactRecall(ctx, cfg.WorkspaceID, cfg.Query, searchindex.ExactRecallOptions{Limit: cfg.Limit})
	if err != nil {
		return nil, err
	}
	return searchHitsToSourceHits(SourceExact, hits), nil
}

// LexicalSource executes keyword/term recall from searchindex.
type LexicalSource struct{}

// ID returns this source identifier.
func (LexicalSource) ID() SourceID { return SourceLexical }

// Enabled returns whether lexical source should run.
func (LexicalSource) Enabled(request SearchRequest, _ bool) bool {
	return request.Sources.EnableLexical
}

// Recall runs lexical recall using the index.
func (LexicalSource) Recall(ctx context.Context, cfg SourceCall) ([]SourceHit, error) {
	if cfg.Index == nil {
		return nil, nil
	}
	if cfg.Query == "" || cfg.Limit <= 0 {
		return nil, nil
	}

	hits, err := cfg.Index.LexicalRecall(ctx, cfg.WorkspaceID, cfg.Query, searchindex.RecallOptions{
		Limit:    cfg.Limit,
		MinScore: cfg.MinScore,
	})
	if err != nil {
		return nil, err
	}

	out := make([]SourceHit, 0, len(hits))
	for i, hit := range hits {
		docID := hit.Doc.ID
		if docID == "" {
			docID = hit.Doc.Path
		}
		if docID == "" {
			continue
		}

		out = append(out, SourceHit{
			Source:   SourceLexical,
			ID:       docID,
			Document: hit.Doc,
			Score:    hit.Score,
			Rank:     i + 1,
		})
	}
	return out, nil
}

// VectorSource executes embedding recall from searchindex.
type VectorSource struct{}

// ID returns this source identifier.
func (VectorSource) ID() SourceID { return SourceVector }

// Enabled returns whether vector source should run.
func (VectorSource) Enabled(request SearchRequest, hasEmbedding bool) bool {
	return request.Sources.EnableVector && hasEmbedding
}

// Recall runs vector recall using the index.
func (VectorSource) Recall(ctx context.Context, cfg SourceCall) ([]SourceHit, error) {
	if cfg.Index == nil {
		return nil, nil
	}
	if len(cfg.Embedding) == 0 || cfg.Limit <= 0 {
		return nil, nil
	}

	hits, err := cfg.Index.VectorRecall(ctx, cfg.WorkspaceID, cfg.Embedding, searchindex.VectorRecallOptions{
		Limit:          cfg.Limit,
		MinScore:       cfg.MinScore,
		EmbeddingModel: cfg.Model,
	})
	if err != nil {
		return nil, err
	}

	out := make([]SourceHit, 0, len(hits))
	for i, hit := range hits {
		docID := hit.Doc.ID
		if docID == "" {
			docID = hit.Doc.Path
		}
		if docID == "" {
			continue
		}

		out = append(out, SourceHit{
			Source:   SourceVector,
			ID:       docID,
			Document: hit.Doc,
			Score:    hit.Score,
			Rank:     i + 1,
		})
	}
	return out, nil
}

// RepoIndexSource executes structural recall from repoindex.
type RepoIndexSource struct{}

// ID returns this source identifier.
func (RepoIndexSource) ID() SourceID { return SourceRepoIndex }

// Enabled returns whether repoindex source should run.
func (RepoIndexSource) Enabled(request SearchRequest, _ bool) bool {
	return request.Sources.EnableRepoIndex
}

// Recall runs repoindex search or dag_grep using the shared repoquery service.
func (RepoIndexSource) Recall(ctx context.Context, cfg SourceCall) ([]SourceHit, error) {
	if cfg.RepoQuery == nil || cfg.Query == "" || cfg.Limit <= 0 {
		return nil, nil
	}

	mode := normalizeRepoIndexMode(cfg.Query, cfg.RepoIndexMode)
	if mode == "dag" {
		req := RepoDAGGrepRequest{
			Query:      cfg.Query,
			Limit:      cfg.Limit,
			EdgeSets:   []string{"structural"},
			Depth:      2,
			Budget:     80,
			PerNodeCap: 20,
		}
		result, err := cfg.RepoQuery.DAGGrep(ctx, req)
		if err != nil {
			return nil, err
		}
		hits := repoqueryadapters.ToSearchHits((repoquery.Projector{}).FromNodes(result.Graph.Nodes))
		if len(hits) > cfg.Limit {
			hits = hits[:cfg.Limit]
		}
		return searchHitsToSourceHits(SourceRepoIndex, hits), nil
	}

	req := RepoSearchRequest{Query: cfg.Query, Limit: cfg.Limit}
	nodes, err := cfg.RepoQuery.Search(ctx, req)
	if err != nil {
		return nil, err
	}
	hits := repoqueryadapters.ToSearchHits((repoquery.Projector{}).FromNodes(nodes))
	if mode == "auto" && shouldEscalateRepoIndexAuto(cfg.Query, nodes, hits, cfg.Limit) {
		direction := repoIndexFallbackDirection(nodes)
		edgeSets := repoIndexFallbackEdgeSets(nodes)
		dagReq := RepoDAGGrepRequest{
			Query:      cfg.Query,
			Limit:      cfg.Limit,
			EdgeSets:   edgeSets,
			Direction:  direction,
			Depth:      2,
			Budget:     80,
			PerNodeCap: 20,
		}
		result, err := cfg.RepoQuery.DAGGrep(ctx, dagReq)
		if err != nil {
			return nil, err
		}
		hits := repoqueryadapters.ToSearchHits((repoquery.Projector{}).FromNodes(result.Graph.Nodes))
		if len(hits) > cfg.Limit {
			hits = hits[:cfg.Limit]
		}
		return searchHitsToSourceHits(SourceRepoIndex, hits), nil
	}
	return searchHitsToSourceHits(SourceRepoIndex, hits), nil
}

func searchHitsToSourceHits(source SourceID, hits []searchindex.SearchHit) []SourceHit {
	out := make([]SourceHit, 0, len(hits))
	for i, hit := range hits {
		docID := hit.Doc.ID
		if docID == "" {
			docID = hit.Doc.Path
		}
		if docID == "" {
			continue
		}
		out = append(out, SourceHit{
			Source:   source,
			ID:       docID,
			Document: hit.Doc,
			Score:    hit.Score,
			Rank:     i + 1,
		})
	}
	return out
}

func normalizeRepoIndexMode(query, mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "", "auto":
		return "auto"
	case "search":
		return "search"
	case "dag", "dag_grep", "repo_index_dag":
		return "dag"
	case "off", "none", "disabled":
		return "off"
	default:
		return "auto"
	}
}

func shouldEscalateRepoIndexAuto(query string, nodes []repoindex.Node, hits []searchindex.SearchHit, limit int) bool {
	_ = query
	_ = limit
	return len(nodes) == 0 || len(hits) == 0
}

func repoIndexFallbackDirection(nodes []repoindex.Node) string {
	if len(nodes) == 0 {
		return ""
	}
	allConcept := true
	for _, node := range nodes {
		if node.Kind != repoindex.NodeConcept {
			allConcept = false
			break
		}
	}
	if allConcept {
		return string(repoindex.DirIn)
	}
	return ""
}

func repoIndexFallbackEdgeSets(nodes []repoindex.Node) []string {
	if len(nodes) == 0 {
		return []string{"structural"}
	}
	allConcept := true
	for _, node := range nodes {
		if node.Kind != repoindex.NodeConcept {
			allConcept = false
			break
		}
	}
	if allConcept {
		return []string{"all"}
	}
	return []string{"structural"}
}
