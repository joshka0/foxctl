package retrievalv2

import (
	"context"
	"errors"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/searchindex"
)

// SourceID identifies a recall source in the retrieval-v2 pipeline.
type SourceID string

const (
	// SourceExact performs exact symbol/title/path recall using the SQL search index.
	SourceExact SourceID = "exact"
	// SourceLexical performs BM25/keyword-style recall using the SQL search index.
	SourceLexical SourceID = "lexical"
	// SourceVector performs embedding recall using the SQL search index.
	SourceVector SourceID = "vector"
	// SourceRepoIndex performs structural recall using the repo graph index.
	SourceRepoIndex SourceID = "repo_index"
)

// QueryEmbeddingMode controls which embedder method is used for query embeddings.
type QueryEmbeddingMode string

const (
	// QueryEmbeddingModeAuto prefers query-optimized embedding when available.
	QueryEmbeddingModeAuto QueryEmbeddingMode = "auto"
	// QueryEmbeddingModeEmbed forces provider.Embed.
	QueryEmbeddingModeEmbed QueryEmbeddingMode = "embed"
	// QueryEmbeddingModeEmbedQuery uses provider.EmbedQuery when available.
	QueryEmbeddingModeEmbedQuery QueryEmbeddingMode = "embed_query"
)

// SearchIndex is the minimal read interface used by Engine.
type SearchIndex interface {
	ExactRecall(ctx context.Context, workspaceID, query string, opts searchindex.ExactRecallOptions) ([]searchindex.SearchHit, error)
	LexicalRecall(ctx context.Context, workspaceID, query string, opts searchindex.RecallOptions) ([]searchindex.SearchHit, error)
	VectorRecall(ctx context.Context, workspaceID string, embedding []float32, opts searchindex.VectorRecallOptions) ([]searchindex.SearchHit, error)
}

// Embedder is the minimal embedding contract used by retrieval-v2.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Model() string
	Dimensions() int
}

// QueryEmbedder extends Embedder with query-specific embedding support.
type QueryEmbedder interface {
	Embedder
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
}

// SearchRequest configures one retrieval search.
type SearchRequest struct {
	WorkspaceID string
	Query       string

	// MaxResults caps total post-fusion output.
	MaxResults int

	Sources SearchSourcesConfig
	Fuse    FuseOptions
	Group   GroupOptions

	// QueryEmbeddingMode controls how query embeddings are produced.
	QueryEmbeddingMode QueryEmbeddingMode
}

// SearchSourcesConfig controls recall source behavior.
type SearchSourcesConfig struct {
	EnableExact     bool
	EnableLexical   bool
	EnableVector    bool
	EnableRepoIndex bool

	ExactLimit     int
	LexicalLimit   int
	VectorLimit    int
	RepoIndexLimit int

	LexicalMinScore float64
	VectorMinScore  float64
	RepoIndexMode   string

	VectorModel string
}

const (
	// FuseModeRRF uses reciprocal-rank fusion.
	FuseModeRRF FuseMode = "rrf"
	// FuseModeWeighted uses source-weighted score summation.
	FuseModeWeighted FuseMode = "weighted"
)

// FuseOptions controls cross-source fusion.
type FuseMode string

type FuseOptions struct {
	Mode            FuseMode
	TopK            int
	RRFK            float64
	SourceWeights   map[SourceID]float64
	MaxContributors int
}

// GroupOptions controls final file/group-level grouping.
type GroupOptions struct {
	Enabled    bool
	MaxGroups  int
	MaxMembers int
}

type SourceHit struct {
	Source   SourceID             `json:"source"`
	ID       string               `json:"id"`
	Document searchindex.Document `json:"-"`
	Score    float64              `json:"score"`
	Rank     int                  `json:"rank"`
}

type SourceContribution struct {
	Source SourceID `json:"source"`
	Score  float64  `json:"score"`
	Rank   int      `json:"rank"`
}

type FusedHit struct {
	ID            string               `json:"-"`
	Document      searchindex.Document `json:"document"`
	Score         float64              `json:"score"`
	Sources       []SourceID           `json:"sources"`
	SourceScores  map[SourceID]float64 `json:"source_scores"`
	Contributions []SourceContribution `json:"contributions"`
}

// AnchorHit preserves anchor-level detail when grouping file-oriented results.
type AnchorHit struct {
	Anchor     searchindex.Anchor `json:"anchor"`
	Score      float64            `json:"score"`
	Source     SourceID           `json:"source"`
	SymbolID   string             `json:"symbol_id,omitempty"`
	SymbolName string             `json:"symbol_name,omitempty"`
}

// Group is a file/group-level aggregate result.
type Group struct {
	Key      string      `json:"key"`
	Path     string      `json:"path"`
	Kind     string      `json:"kind"`
	Summary  string      `json:"summary,omitempty"`
	Score    float64     `json:"score"`
	Sources  []SourceID  `json:"sources"`
	Hits     []FusedHit  `json:"hits"`
	Anchors  []AnchorHit `json:"anchors,omitempty"`
	HitCount int         `json:"hit_count"`
}

// ParsedQuery captures normalized plan output for downstream use.
type ParsedQuery struct {
	Raw          string
	LexicalQuery string
	Plan         QueryPlan
}

// SourceStats tracks recall request/return/error counts for one source.
type SourceStats struct {
	Requested int
	Returned  int
	Err       error
}

// SearchStats tracks end-to-end retrieval execution stats.
type SearchStats struct {
	Sources      map[SourceID]SourceStats
	TotalRaw     int
	TotalFused   int
	TotalGrouped int
}

// SearchResponse is the engine output.
type SearchResponse struct {
	Query    string      `json:"query"`
	Plan     ParsedQuery `json:"plan"`
	Hits     []FusedHit  `json:"hits"`
	Groups   []Group     `json:"groups"`
	Stats    SearchStats `json:"stats"`
	Embedded bool        `json:"embedded"`
}

type RepoSearchRequest struct {
	Query string
	Limit int
}

type RepoDAGGrepRequest struct {
	Query      string
	Limit      int
	EdgeSets   []string
	Direction  string
	Depth      int
	Budget     int
	PerNodeCap int
	Render     string
}

// RepoQueryService is the minimal graph-query interface used by Engine.
type RepoQueryService interface {
	Search(ctx context.Context, req RepoSearchRequest) ([]repoindex.Node, error)
	DAGGrep(ctx context.Context, req RepoDAGGrepRequest) (repoindex.DAGGrepResult, error)
}

// DefaultSearchRequest returns a conservative starting configuration.
func DefaultSearchRequest(workspaceID, query string) SearchRequest {
	return SearchRequest{
		WorkspaceID:        workspaceID,
		Query:              query,
		MaxResults:         25,
		Sources:            DefaultSearchSourcesConfig(),
		Fuse:               DefaultFuseOptions(),
		Group:              DefaultGroupOptions(),
		QueryEmbeddingMode: QueryEmbeddingModeAuto,
	}
}

// DefaultSearchSourcesConfig returns source defaults for broad recall.
func DefaultSearchSourcesConfig() SearchSourcesConfig {
	return SearchSourcesConfig{
		EnableLexical:   true,
		EnableExact:     true,
		EnableVector:    true,
		EnableRepoIndex: false,
		ExactLimit:      10,
		LexicalLimit:    80,
		VectorLimit:     40,
		RepoIndexLimit:  20,
		RepoIndexMode:   "search",
	}
}

// DefaultFuseOptions returns a safe default cross-source fusion policy.
func DefaultFuseOptions() FuseOptions {
	return FuseOptions{
		Mode:            FuseModeRRF,
		TopK:            60,
		RRFK:            60,
		MaxContributors: 4,
		SourceWeights: map[SourceID]float64{
			SourceExact:     2.5,
			SourceLexical:   1.0,
			SourceVector:    0.85,
			SourceRepoIndex: 1.2,
		},
	}
}

// DefaultGroupOptions returns defaults that preserve source-level detail.
func DefaultGroupOptions() GroupOptions {
	return GroupOptions{
		Enabled:    true,
		MaxGroups:  25,
		MaxMembers: 5,
	}
}

var (
	errNoWorkspace = errors.New("search request missing workspace_id")
	errNoQuery     = errors.New("search request missing query")
	errNoIndex     = errors.New("search request has no index")
)

// Err... are exported for callers that want to branch on validation failures.
var (
	ErrNoWorkspace = errNoWorkspace
	ErrNoQuery     = errNoQuery
	ErrNoIndex     = errNoIndex
)
