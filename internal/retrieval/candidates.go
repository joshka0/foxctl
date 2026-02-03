package retrieval

import (
	"context"

	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/rs/zerolog"
)

// Source identifies where a candidate came from.
const (
	SourceSymbol   = "symbol"   // From code symbol index
	SourceSemantic = "semantic" // From semantic/vector search
	SourceRipgrep  = "ripgrep"  // From ripgrep fallback
)

// Candidate represents a file candidate for SWE grep processing.
type Candidate struct {
	// Path is the file path (required, relative to workspace root)
	Path string `json:"path"`

	// SymbolID is the unique symbol identifier, if from symbol index
	// Format: "file_path:symbol_name"
	SymbolID string `json:"symbol_id,omitempty"`

	// Name is the symbol name, if from symbol index
	Name string `json:"name,omitempty"`

	// Kind is the symbol kind (function, method, class, etc.)
	Kind string `json:"kind,omitempty"`

	// Documentation is the extracted doc comment for the symbol, if available.
	Documentation string `json:"documentation,omitempty"`

	// Score is the normalized relevance score (0-1 range after merging)
	Score float64 `json:"score"`

	// RawScore is the pre-normalization score from the source
	RawScore float64 `json:"raw_score,omitempty"`

	// Source identifies where this candidate came from
	Source string `json:"source"`

	// Line is an optional line number hint for targeted extraction
	Line int `json:"line,omitempty"`
}

// GenerateResult contains the output of candidate generation.
type GenerateResult struct {
	// Candidates is the ranked list of file candidates
	Candidates []Candidate `json:"candidates"`

	// Stats provides breakdown of candidates by source
	Stats GenerateStats `json:"stats"`
}

// GenerateStats tracks candidate counts by source.
type GenerateStats struct {
	SymbolCount   int `json:"symbol_count"`
	SemanticCount int `json:"semantic_count"`
	RipgrepCount  int `json:"ripgrep_count"`
	TotalRaw      int `json:"total_raw"`      // Before dedup
	TotalMerged   int `json:"total_merged"`   // After dedup
	TotalReturned int `json:"total_returned"` // After limit
}

// Generator generates ranked file candidates from multiple sources.
type Generator struct {
	store           storage.MemoryStore
	searchableStore *memory.SearchableStore    // Optional, for vector search
	embedProvider   semantic.EmbeddingProvider // nil = skip semantic search
	embedQueryMode  config.EmbedQueryMode
	workspaceRoot   string
	logger          zerolog.Logger
}

// NewGenerator creates a new candidate generator.
//
// Parameters:
//   - store: Memory store for symbol and semantic index access
//   - embedProvider: Optional embedding provider for semantic search (nil to disable)
//   - workspaceRoot: Absolute path to the workspace root
//   - logger: Logger for debug output
func NewGenerator(
	store storage.MemoryStore,
	embedProvider semantic.EmbeddingProvider,
	workspaceRoot string,
	logger zerolog.Logger,
) *Generator {
	return &Generator{
		store:         store,
		embedProvider: embedProvider,
		workspaceRoot: workspaceRoot,
		logger:        logger.With().Str("component", "retrieval").Logger(),
	}
}

// WithSearchableStore sets an optional SearchableStore for vector search.
// This enables hybrid search (BM25 + vector) when embeddings are available.
func (g *Generator) WithSearchableStore(ss *memory.SearchableStore) *Generator {
	g.searchableStore = ss
	return g
}

// WithEmbedQueryMode sets the embedding query mode used for semantic search.
func (g *Generator) WithEmbedQueryMode(mode config.EmbedQueryMode) *Generator {
	g.embedQueryMode = mode
	return g
}

// Generate produces ranked candidates for a question.
//
// The generation process:
//  1. Search symbol index for matching code symbols
//  2. Search semantic index for similar files (if embeddings available)
//  3. Fall back to ripgrep if results are sparse
//  4. Merge, deduplicate, and rank all candidates
//  5. Apply final limit and return
//
// Index:
// - Purpose: Merge symbol, semantic, and ripgrep sources into ranked candidates
// - Flow: search symbols → search semantic → optional ripgrep → merge scores → trim limit
// - SideEffects: may call embedding providers; runs ripgrep subprocess
// - FailureModes: search errors, embedding errors, ripgrep failures
// - Related: searchSymbolIndex, searchSemanticIndex, ripgrepFallback, mergeCandidates
// - Keywords: candidates, semantic, symbols, ripgrep, max_total_candidates
func (g *Generator) Generate(ctx context.Context, workspaceID, question string, opts Options) (*GenerateResult, error) {
	if opts.MaxTotalCandidates == 0 {
		opts = DefaultOptions()
	}

	var allCandidates [][]Candidate
	var stats GenerateStats

	// 1. Symbol index search
	if opts.EnableSymbols && g.store != nil {
		candidates, err := g.searchSymbolIndex(ctx, workspaceID, question, opts.MaxSymbolCandidates)
		if err != nil {
			g.logger.Warn().Err(err).Msg("symbol search failed, continuing")
		} else if len(candidates) > 0 {
			allCandidates = append(allCandidates, candidates)
			stats.SymbolCount = len(candidates)
			g.logger.Debug().Int("count", len(candidates)).Msg("symbol search results")
		}
	}

	// 2. Semantic/vector search
	if opts.EnableSemantic && g.embedProvider != nil && g.store != nil {
		candidates, err := g.searchSemanticIndex(ctx, workspaceID, question, opts.MaxSemanticCandidates)
		if err != nil {
			g.logger.Warn().Err(err).Msg("semantic search failed, continuing")
		} else if len(candidates) > 0 {
			allCandidates = append(allCandidates, candidates)
			stats.SemanticCount = len(candidates)
			g.logger.Debug().Int("count", len(candidates)).Msg("semantic search results")
		}
	}

	// Count raw candidates
	for _, batch := range allCandidates {
		stats.TotalRaw += len(batch)
	}

	// 3. Ripgrep fallback if we have few candidates
	if opts.EnableRipgrep && stats.TotalRaw < opts.MinTotalCandidates {
		candidates, err := g.ripgrepFallback(ctx, question, opts.MaxRipgrepCandidates)
		if err != nil {
			g.logger.Warn().Err(err).Msg("ripgrep fallback failed")
		} else if len(candidates) > 0 {
			allCandidates = append(allCandidates, candidates)
			stats.RipgrepCount = len(candidates)
			stats.TotalRaw += len(candidates)
			g.logger.Debug().Int("count", len(candidates)).Msg("ripgrep fallback results")
		}
	}

	// 4. Merge and rank
	weights := map[string]float64{
		SourceSymbol:   opts.SymbolWeight,
		SourceSemantic: opts.SemanticWeight,
		SourceRipgrep:  opts.RipgrepWeight,
	}

	merged := mergeCandidates(allCandidates, weights, opts.MaxTotalCandidates)
	stats.TotalMerged = len(merged)

	// 5. Apply final limit
	if len(merged) > opts.MaxTotalCandidates {
		merged = merged[:opts.MaxTotalCandidates]
	}
	stats.TotalReturned = len(merged)

	g.logger.Debug().
		Int("symbol", stats.SymbolCount).
		Int("semantic", stats.SemanticCount).
		Int("ripgrep", stats.RipgrepCount).
		Int("merged", stats.TotalMerged).
		Int("returned", stats.TotalReturned).
		Msg("candidate generation complete")

	return &GenerateResult{
		Candidates: merged,
		Stats:      stats,
	}, nil
}

// GenerateSimple is a convenience method that returns just the candidates.
func (g *Generator) GenerateSimple(ctx context.Context, workspaceID, question string, opts Options) ([]Candidate, error) {
	result, err := g.Generate(ctx, workspaceID, question, opts)
	if err != nil {
		return nil, err
	}
	return result.Candidates, nil
}
