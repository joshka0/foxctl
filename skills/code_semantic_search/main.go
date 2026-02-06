// Package main implements the code/semantic_search skill.
// Performs unified semantic search across code symbols, sessions, and memories,
// combining results with Reciprocal Rank Fusion and extracting context hints.
//
// Phase 3: Uses internal/retrieval infrastructure for symbol search with BM25 fallback.
// See docs/designs/unified_semantic_search.md and docs/designs/semantic_search_phase3_plan.md.
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/mathutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/domain/policy"
	"github.com/jkatigb/agentctl/internal/indexing/rerank"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/indexing/symbol"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	llmproviders "github.com/jkatigb/agentctl/internal/providers/llm"
	"github.com/jkatigb/agentctl/internal/retrieval"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
	"github.com/jkatigb/agentctl/internal/storage/graph"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

// Command is the envelope command for this skill.
const Command = "code/semantic_search"

// Error codes using canonical envelope codes.
const (
	ErrCodeInput         = "EARG"      // Missing query/scope, invalid workspace
	ErrCodeEmbedProvider = "ERUNTIME"  // Embedding provider error/rate limit
	ErrCodeSourceEmpty   = "ENOTFOUND" // All sources disabled/empty
	ErrCodePolicy        = "EPOLICY"   // PathValidator rejection
	ErrCodeRuntime       = "ERUNTIME"  // Unexpected internal error
)

// Default limits.
const (
	DefaultLimit           = 20
	DefaultMinSimilarity   = 0.3
	DefaultMaxContextHints = 3
	RRFConstant            = 60 // Standard RRF constant
)

// PageRank scoring weights (from plan: 0.50*Similarity + 0.30*PageRank + 0.20*Connection).
const (
	WeightSimilarity = 0.50
	WeightPageRank   = 0.30
	WeightConnection = 0.20
	DefaultMaxPR     = 0.1 // Max PageRank for normalization (typical converged max)
)

// Timeout constants.
const (
	DefaultSourceTimeout = 10 * time.Second // Per-source timeout (includes embedding generation)
	DefaultTotalTimeout  = 15 * time.Second // Total search timeout
)

// Supported scopes.
const (
	ScopeSymbols  = "symbols"
	ScopeSessions = "sessions"
	ScopeMemories = "memories"
	ScopeTasks    = "tasks"
	ScopeCodemaps = "codemaps"
)

// Input is the expected JSON input for code/semantic_search operations.
type Input struct {
	Query          string   `json:"query,omitempty"`           // Empty query with format=tree returns full repo tree
	Scope          []string `json:"scope,omitempty"`           // ["symbols", "sessions", "memories", "tasks"]
	Workspace      string   `json:"workspace,omitempty"`       // Workspace path (defaults to cwd)
	Limit          int      `json:"limit,omitempty"`           // Default: 20
	MinSimilarity  float64  `json:"min_similarity,omitempty"`  // Default: 0.3
	IncludeContext *bool    `json:"include_context,omitempty"` // Include session context hints (default: true)
	Summarize      bool     `json:"summarize,omitempty"`       // Send results to LLM for synthesis
	SummarizeModel string   `json:"summarize_model,omitempty"` // Override default LLM model
	Format         string   `json:"format,omitempty"`          // Output format: "json" (default), "tree"

	// Remote/cross-workspace options (requires Turso)
	Remote     bool     `json:"remote,omitempty"`     // Use remote Turso database
	Global     bool     `json:"global,omitempty"`     // Search across ALL workspaces (requires remote)
	Workspaces []string `json:"workspaces,omitempty"` // Specific workspaces to search (requires remote)

	// Reranking options (requires VOYAGE_API_KEY)
	RerankEnabled bool   `json:"rerank_enabled,omitempty"` // Enable reranking (default: from env)
	RerankTopK    int    `json:"rerank_top_k,omitempty"`   // Candidates to rerank (default: 50)
	RerankModel   string `json:"rerank_model,omitempty"`   // Override model (default: rerank-2.5)

	// Timeline options (enriches session results with chunk summaries and learnings)
	Timeline      bool     `json:"timeline,omitempty"`       // Enrich session results with timeline data
	TimelineLimit int      `json:"timeline_limit,omitempty"` // Max sessions to fetch timeline for (default: 3)
	TimelineTypes []string `json:"timeline_types,omitempty"` // Learning types to include (default: all)

	// Tree format options (when format="tree")
	TreeDepth               int   `json:"tree_depth,omitempty"`                 // Max directory depth for tree (default: 2)
	TreeMaxChildren         int   `json:"tree_max_children,omitempty"`          // Max children per directory node (default: 10)
	TreeIncludeSummaries    *bool `json:"tree_include_summaries,omitempty"`     // Include file summaries in tree (default: true, use ptr to detect explicit false)
	TreeMaxMissingSummaries int   `json:"tree_max_missing_summaries,omitempty"` // Max summaries to generate lazily (default: 20)
}

// Output is the JSON output structure for code/semantic_search results.
type Output struct {
	Query        string                `json:"query"`
	Results      []Result              `json:"results"`
	ContextHints []ContextHint         `json:"context_hints,omitempty"`
	Timelines    []SessionTimeline     `json:"timelines,omitempty"` // Present when timeline=true
	Stats        SearchStats           `json:"stats"`
	Summary      *SynthesisSummary     `json:"summary,omitempty"`   // Present when summarize=true
	TreeText     string                `json:"tree_text,omitempty"` // Present when format=tree
	Tree         *retrieval.TreeOutput `json:"tree,omitempty"`      // Structured tree output when format=tree
}

// SynthesisSummary contains the LLM-generated synthesis of search results.
type SynthesisSummary struct {
	Answer      string   `json:"answer"`       // Direct answer to the query
	KeyInsights []string `json:"key_insights"` // Important findings from results
	Gotchas     []string `json:"gotchas"`      // Warnings or caveats
	NextSteps   []string `json:"next_steps"`   // Suggested follow-up actions
	Model       string   `json:"model"`        // Model used for synthesis
	TokensUsed  int      `json:"tokens_used"`  // Approximate tokens consumed
}

// Result represents a single search result from any source.
type Result struct {
	Source      string           `json:"source"`                 // "symbol", "session", "memory"
	ID          string           `json:"id"`                     // Unique identifier (normalized)
	Name        string           `json:"name"`                   // Display name
	Path        string           `json:"path,omitempty"`         // File path (for symbols)
	Line        int              `json:"line,omitempty"`         // Line number (for symbols)
	Snippet     string           `json:"snippet,omitempty"`      // Code snippet (for symbols)
	Summary     string           `json:"summary,omitempty"`      // Summary text (for sessions/memories)
	Similarity  float64          `json:"similarity"`             // Similarity score (0-1)
	Rank        int              `json:"rank"`                   // Final rank after fusion
	RRFScore    float64          `json:"rrf_score,omitempty"`    // RRF score used for ranking
	PageRank    float64          `json:"pagerank,omitempty"`     // PageRank authority score (0-1 normalized)
	FinalScore  float64          `json:"final_score,omitempty"`  // Combined score with PageRank boost
	RerankScore float64          `json:"rerank_score,omitempty"` // Reranker relevance score (0-1)
	SourceRank  int              `json:"source_rank,omitempty"`  // Rank within source
	Timeline    *SessionTimeline `json:"timeline,omitempty"`     // Timeline data (for sessions when timeline=true)
}

// ContextHint represents a hint from related sessions for context enrichment.
type ContextHint struct {
	Type      string   `json:"type"`                // "past_solution", "gotcha", "decision"
	SessionID string   `json:"session_id"`          // Source session ID
	Summary   string   `json:"summary"`             // Brief summary
	Items     []string `json:"items,omitempty"`     // Related items (gotchas, decisions)
	KeyFiles  []string `json:"key_files,omitempty"` // Related files
}

// SearchStats contains search statistics and performance metrics.
type SearchStats struct {
	TotalResults        int            `json:"total_results"`
	SourceCounts        map[string]int `json:"source_counts"`
	SourceLatencies     map[string]int `json:"source_latencies_ms"` // Per-source latency
	SourcesMissing      []string       `json:"sources_missing,omitempty"`
	EmbeddingDimensions int            `json:"embedding_dimensions,omitempty"`
	Hint                string         `json:"hint,omitempty"` // Remediation hint

	// Reranking stats (populated when reranking is enabled)
	RerankEnabled   bool   `json:"rerank_enabled,omitempty"`
	RerankModel     string `json:"rerank_model,omitempty"`
	RerankLatencyMS int    `json:"rerank_latency_ms,omitempty"`
	RerankCount     int    `json:"rerank_count,omitempty"` // Number of candidates reranked
}

// SessionTimeline contains timeline data for a session with chunks and learnings.
type SessionTimeline struct {
	SessionID      string             `json:"session_id"`
	SessionName    string             `json:"session_name,omitempty"`
	Similarity     float64            `json:"similarity"`
	ChunkSummaries []TimelineChunk    `json:"chunk_summaries,omitempty"`
	Learnings      []TimelineLearning `json:"learnings,omitempty"`
	Rollup         *TimelineRollup    `json:"rollup,omitempty"`
	Status         string             `json:"status"`
	Message        string             `json:"message,omitempty"`
}

// TimelineChunk represents a summarized chunk in the session timeline.
type TimelineChunk struct {
	SummaryID     string   `json:"summary_id"`
	WindowIndex   int      `json:"window_index"`
	ChunkIndexMin int      `json:"chunk_index_min"`
	ChunkIndexMax int      `json:"chunk_index_max"`
	Summary       string   `json:"summary"`
	Tools         []string `json:"tools,omitempty"`
	Files         []string `json:"files,omitempty"`
	Errors        []string `json:"errors,omitempty"`
}

// TimelineLearning represents extracted learning in the timeline.
type TimelineLearning struct {
	Type        string `json:"type"`
	Summary     string `json:"summary"`
	WindowIndex int    `json:"window_index"`
}

// TimelineRollup aggregates timeline metadata from chunks and learnings.
type TimelineRollup struct {
	SummaryLines []string `json:"summary_lines,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	Files        []string `json:"files,omitempty"`
	Errors       []string `json:"errors,omitempty"`
	Decisions    []string `json:"decisions,omitempty"`
	Gotchas      []string `json:"gotchas,omitempty"`
}

// main is the skill entry point for code/semantic_search.
func main() {
	skillmain.Main(Command, run)
}

// run orchestrates unified semantic search across multiple data sources.
//
// Index:
// - Purpose: Perform unified semantic search across symbols, sessions, memories, tasks, and codemaps with RRF fusion
// - Flow: validate input → generate scoped embeddings → parallel search sources → fuse results → apply PageRank boost → rerank (optional) → synthesize (optional)
// - SideEffects: embedding API calls; database queries; LLM API calls (synthesis); artifact persistence
// - FailureModes: missing API keys, database errors, embedding failures, LLM errors, timeout errors
// - Observability: emits query/results/context_hints/timelines/stats/summary/tree_text/tree/artifact
// - Related: search, generateScopedEmbeddings, reciprocalRankFusion, applyPageRankBoost, applyReranking, synthesizeResults, buildFullRepoTree
// - Keywords: code/semantic_search, unified, symbols, sessions, memories, tasks, codemaps, rrf, pagerank, rerank, synthesis, tree
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply defaults
	if len(in.Scope) == 0 {
		in.Scope = []string{ScopeSymbols, ScopeSessions, ScopeMemories, ScopeTasks, ScopeCodemaps}
	}
	in.Limit = mathutil.DefaultPositiveInt(in.Limit, DefaultLimit)
	in.MinSimilarity = mathutil.DefaultPositiveFloat(in.MinSimilarity, DefaultMinSimilarity)
	if in.Workspace == "" {
		in.Workspace = rc.PathValidator.Workspace()
	}
	if in.IncludeContext == nil {
		defaultTrue := true
		in.IncludeContext = &defaultTrue
	}

	// Handle empty query - return full repo tree if format=tree
	if in.Query == "" {
		if in.Format != "tree" {
			return skillerr.Validationf("query is required (or use format=tree for full repo tree)")
		}
		// Validate and canonicalize workspace before full scan
		workspacePath := in.Workspace
		if absPath, err := filepath.Abs(workspacePath); err == nil {
			workspacePath = absPath
		}
		// Validate workspace with PathValidator to prevent unauthorized scans
		if _, err := policy.NewPathValidator(workspacePath, []string{rc.Config.Home}); err != nil {
			return skillerr.WrapArg("invalid workspace path", err)
		}
		in.Workspace = workspacePath
		out, err := buildFullRepoTree(ctx, rc.Logger, rc.Config, &in)
		if err != nil {
			return err
		}
		return skillout.Emit(rc, Command, out)
	}

	// Validate scope values
	validScopes := map[string]bool{ScopeSymbols: true, ScopeSessions: true, ScopeMemories: true, ScopeTasks: true, ScopeCodemaps: true}
	for _, s := range in.Scope {
		if !validScopes[s] {
			return skillerr.Validationf("invalid scope: %s (valid: symbols, sessions, memories, tasks, codemaps)", s)
		}
	}

	// FC/IS: Read API keys at boundary and pass through
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")

	out, err := search(ctx, rc.Logger, rc.Config, &in, voyageKey, geminiKey)
	if err != nil {
		return err
	}

	return skillout.Emit(rc, Command, out)
}

// search performs the core search logic with parallel source queries and result fusion.
func search(ctx context.Context, logger zerolog.Logger, cfg config.Config, in *Input, voyageKey, geminiKey string) (*Output, error) {
	// Apply total timeout
	searchCtx, cancel := context.WithTimeout(ctx, DefaultTotalTimeout)
	defer cancel()

	out := &Output{
		Query:   in.Query,
		Results: []Result{},
		Stats: SearchStats{
			SourceCounts:    make(map[string]int),
			SourceLatencies: make(map[string]int),
		},
	}

	// Validate workspace path with PathValidator
	// FC/IS: Use cfg.Home from boundary instead of os.Getenv
	agentctlHome := cfg.Home

	// Workspace is already resolved in parseInput (prefers AGENTCTL_WORKSPACE over cwd)
	workspacePath := in.Workspace
	// Canonicalize the workspace path
	if absPath, err := filepath.Abs(workspacePath); err == nil {
		workspacePath = absPath
	}

	// Create PathValidator to ensure workspace is valid (for path validation, not workspace ID)
	validator, err := policy.NewPathValidator(workspacePath, []string{agentctlHome})
	if err != nil {
		return nil, skillerr.WrapArg("invalid workspace path", err)
	}
	_ = validator // PathValidator available for path validation if needed

	// Use a stable workspace ID derived from repo root (fallback to path hash).
	workspaceID := workspace.ID(workspacePath)

	// Build scope set first (needed for scoped embeddings)
	scopeSet := make(map[string]bool)
	for _, s := range in.Scope {
		scopeSet[s] = true
	}

	// Detect provider and get scope-specific model configuration
	// FC/IS: API keys passed from boundary (run function)
	var providerName string
	if voyageKey != "" {
		providerName = "voyage"
	} else if geminiKey != "" {
		providerName = "gemini"
	}

	// Generate scope-specific query embeddings in parallel
	// Model selection is configurable via EMBEDDING_MODEL_CODE, EMBEDDING_MODEL_MEMORY, EMBEDDING_MODEL_TEXT
	var scopedEmb scopedEmbeddings
	var embedProvider semantic.EmbeddingProvider // Keep for backward compat with some functions
	var fileSummaryProvider semantic.EmbeddingProvider
	var codeModel, memoryModel, textModel, fileSummaryModel string

	if providerName != "" {
		codeModel, memoryModel, textModel, fileSummaryModel = embeddingModelConfig(providerName, cfg)

		start := time.Now()
		var embErr error
		scopedEmb, embErr = generateScopedEmbeddings(searchCtx, cfg, in.Query, scopeSet, codeModel, memoryModel, textModel, voyageKey, geminiKey)
		if embErr != nil {
			out.Stats.Hint = fmt.Sprintf("embedding failed: %v; using BM25-only", embErr)
		} else {
			// Report embedding stats (use text embedding dims, or code if text not generated)
			if len(scopedEmb.text) > 0 {
				out.Stats.EmbeddingDimensions = len(scopedEmb.text)
			} else if len(scopedEmb.code) > 0 {
				out.Stats.EmbeddingDimensions = len(scopedEmb.code)
			}
			out.Stats.SourceLatencies["embedding"] = int(time.Since(start).Milliseconds())

			// Create a code provider for backward compat with searchSymbolsWithRetrieval
			if len(scopedEmb.code) > 0 {
				embedProvider, _ = createProviderWithModel(codeModel, cfg, voyageKey, geminiKey)
			}
		}
	} else {
		out.Stats.Hint = "no embedding API key set; set VOYAGE_API_KEY or GEMINI_API_KEY for vector search"
	}

	var fileSummaryEmbedding []float32
	if providerName != "" && in.Format == "tree" {
		if fileSummaryModel == codeModel && len(scopedEmb.code) > 0 {
			fileSummaryEmbedding = scopedEmb.code
			fileSummaryProvider = embedProvider
		} else if fileSummaryModel == memoryModel && len(scopedEmb.memory) > 0 {
			fileSummaryEmbedding = scopedEmb.memory
		} else if fileSummaryModel == textModel && len(scopedEmb.text) > 0 {
			fileSummaryEmbedding = scopedEmb.text
		}

		if fileSummaryProvider == nil && strings.TrimSpace(fileSummaryModel) != "" {
			provider, err := createProviderWithModel(fileSummaryModel, cfg, voyageKey, geminiKey)
			if err != nil {
				logger.Debug().Err(err).Msg("file summary embedding provider unavailable")
			} else {
				fileSummaryProvider = provider
			}
		}

		if len(fileSummaryEmbedding) == 0 && fileSummaryProvider != nil {
			embedding, err := fileSummaryProvider.Embed(searchCtx, in.Query)
			if err != nil {
				logger.Debug().Err(err).Msg("file summary embedding failed")
			} else {
				fileSummaryEmbedding = embedding
			}
		}
	}

	// Use appropriate embedding per scope
	queryEmbedding := scopedEmb.code    // For symbols scope
	memoryEmbedding := scopedEmb.memory // For memories scope (voyage-3-large)
	textEmbedding := scopedEmb.text     // For tasks, sessions, codemaps scopes (voyage-3.5)

	// Parallel search across enabled scopes
	var wg sync.WaitGroup
	resultsCh := make(chan sourceResults, 5) // symbols, sessions, memories, tasks, codemaps

	// Search symbols using retrieval.Generator (BM25 + semantic + ripgrep)
	if scopeSet[ScopeSymbols] {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			sourceCtx, sourceCancel := context.WithTimeout(searchCtx, DefaultSourceTimeout)
			defer sourceCancel()

			results, err := searchSymbolsWithRetrieval(
				sourceCtx,
				cfg,
				workspaceID,
				validator.Workspace(),
				in.Query,
				embedProvider,
				queryEmbedding,
				in.Limit*2,
			)
			resultsCh <- sourceResults{
				source:  ScopeSymbols,
				results: results,
				err:     err,
				latency: time.Since(start),
			}
		}()
	}

	// Search sessions (uses storage dir) with timeout
	if scopeSet[ScopeSessions] {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			sourceCtx, sourceCancel := context.WithTimeout(searchCtx, DefaultSourceTimeout)
			defer sourceCancel()

			// Session search requires embeddings for vector similarity (uses text model)
			if textEmbedding == nil {
				// Graceful skip: no embeddings available
				resultsCh <- sourceResults{
					source:  ScopeSessions,
					results: nil,
					err:     nil, // Not an error, just unavailable
					latency: time.Since(start),
					hint:    "session search requires embeddings; set VOYAGE_API_KEY or GEMINI_API_KEY",
				}
				return
			}

			results, err := searchSessions(sourceCtx, cfg, textEmbedding, in.Limit*2, in)
			resultsCh <- sourceResults{
				source:  ScopeSessions,
				results: results,
				err:     err,
				latency: time.Since(start),
			}
		}()
	}

	// Search memories (BM25 fallback; vector search when Turso + embeddings available)
	if scopeSet[ScopeMemories] {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			sourceCtx, sourceCancel := context.WithTimeout(searchCtx, DefaultSourceTimeout)
			defer sourceCancel()

			// Memory search requires embeddings for vector similarity (uses memory model: voyage-3-large)
			if memoryEmbedding == nil {
				resultsCh <- sourceResults{
					source:  ScopeMemories,
					results: nil,
					err:     nil,
					latency: time.Since(start),
					hint:    "memory search requires embeddings; set VOYAGE_API_KEY or GEMINI_API_KEY",
				}
				return
			}

			results, hint, err := searchMemories(sourceCtx, cfg, workspaceID, in.Query, memoryEmbedding, in.Limit*2, in)
			resultsCh <- sourceResults{
				source:  ScopeMemories,
				results: results,
				err:     err,
				latency: time.Since(start),
				hint:    hint,
			}
		}()
	}

	// Search tasks (task embeddings in named_memory)
	if scopeSet[ScopeTasks] {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			sourceCtx, sourceCancel := context.WithTimeout(searchCtx, DefaultSourceTimeout)
			defer sourceCancel()

			// Task search requires embeddings for vector similarity (uses text model)
			if textEmbedding == nil {
				resultsCh <- sourceResults{
					source:  ScopeTasks,
					results: nil,
					err:     nil,
					latency: time.Since(start),
					hint:    "task search requires embeddings; set VOYAGE_API_KEY or GEMINI_API_KEY",
				}
				return
			}

			results, err := searchTasks(sourceCtx, cfg, workspaceID, textEmbedding, in.Limit*2)
			resultsCh <- sourceResults{
				source:  ScopeTasks,
				results: results,
				err:     err,
				latency: time.Since(start),
			}
		}()
	}

	// Search codemaps (stored in named_memory with type="codemap")
	if scopeSet[ScopeCodemaps] {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			sourceCtx, sourceCancel := context.WithTimeout(searchCtx, DefaultSourceTimeout)
			defer sourceCancel()

			// Codemap search requires embeddings for vector similarity (uses text model)
			if textEmbedding == nil {
				resultsCh <- sourceResults{
					source:  ScopeCodemaps,
					results: nil,
					err:     nil,
					latency: time.Since(start),
					hint:    "codemap search requires embeddings; set VOYAGE_API_KEY or GEMINI_API_KEY",
				}
				return
			}

			results, err := searchCodemaps(sourceCtx, cfg, workspaceID, textEmbedding, in.Limit*2)
			resultsCh <- sourceResults{
				source:  ScopeCodemaps,
				results: results,
				err:     err,
				latency: time.Since(start),
			}
		}()
	}

	// Close channel when all searches complete
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// Collect results from all sources
	allResults := make(map[string][]Result) // source -> results
	var sourceHints []string
	for sr := range resultsCh {
		out.Stats.SourceLatencies[sr.source] = int(sr.latency.Milliseconds())
		if sr.err != nil {
			out.Stats.SourcesMissing = append(out.Stats.SourcesMissing, sr.source)
			// Include error in hints for debugging
			sourceHints = append(sourceHints, fmt.Sprintf("%s: %v", sr.source, sr.err))
			continue
		}
		// Collect hints (e.g., "vector search unavailable, using BM25 fallback")
		// Note: hints don't mean the source is missing - fallback results may still be valid
		if sr.hint != "" {
			sourceHints = append(sourceHints, sr.hint)
		}
		if len(sr.results) > 0 {
			allResults[sr.source] = sr.results
			out.Stats.SourceCounts[sr.source] = len(sr.results)
		}
	}
	// Append source hints if main hint not already set
	if out.Stats.Hint == "" && len(sourceHints) > 0 {
		out.Stats.Hint = strings.Join(sourceHints, "; ")
	}

	// Apply Reciprocal Rank Fusion to combine results
	fusedResults := reciprocalRankFusion(allResults, in.MinSimilarity)

	// Apply PageRank boost from dependency graph
		fusedResults = applyPageRankBoost(searchCtx, cfg, workspaceID, fusedResults)

	// Apply reranking if enabled
	fusedResults, rerankStats := applyReranking(searchCtx, logger, *in, fusedResults)
	if rerankStats.enabled {
		out.Stats.RerankEnabled = true
		out.Stats.RerankModel = rerankStats.model
		out.Stats.RerankLatencyMS = rerankStats.latencyMS
		out.Stats.RerankCount = rerankStats.count
	}

	// Limit results
	if len(fusedResults) > in.Limit {
		fusedResults = fusedResults[:in.Limit]
	}

	// Assign final ranks
	for i := range fusedResults {
		fusedResults[i].Rank = i + 1
	}

	out.Results = fusedResults
	out.Stats.TotalResults = len(fusedResults)

	// Extract context hints from session results
	if *in.IncludeContext {
		out.ContextHints = extractContextHints(allResults[ScopeSessions], DefaultMaxContextHints)
	}

	// Fetch timelines for session results if requested
	// Use fresh context to avoid issues with searchCtx timeout/cancellation
	if in.Timeline && len(allResults[ScopeSessions]) > 0 {
		timelineLimit := in.TimelineLimit
		if timelineLimit <= 0 {
			timelineLimit = 3 // default
		}
		timelineCtx, timelineCancel := context.WithTimeout(context.Background(), 30*time.Second)
		out.Timelines = fetchTimelines(timelineCtx, cfg, allResults[ScopeSessions], timelineLimit, in.TimelineTypes, workspaceID)
		timelineCancel()

		// Attach timeline data to matching session results for grouped output
		timelineMap := make(map[string]*SessionTimeline)
		for i := range out.Timelines {
			timelineMap[out.Timelines[i].SessionID] = &out.Timelines[i]
		}
		for i := range out.Results {
			if out.Results[i].Source == "session" {
				// Result.ID is "session:<id>", timeline uses raw "<id>"
				rawID := strings.TrimPrefix(out.Results[i].ID, "session:")
				if tl, ok := timelineMap[rawID]; ok {
					out.Results[i].Timeline = tl
				}
			}
		}
	}

	// Add hint if no results
	if len(fusedResults) == 0 && out.Stats.Hint == "" {
		if len(out.Stats.SourcesMissing) == len(in.Scope) {
			out.Stats.Hint = "All sources unavailable; ensure indexes exist and API key is set"
		} else {
			out.Stats.Hint = "No results found; try a different query or broader scope"
		}
	}

	// Synthesize results with LLM if requested
	if in.Summarize && len(fusedResults) > 0 {
		summary, err := synthesizeResults(ctx, in.Query, fusedResults, in.SummarizeModel)
		if err != nil {
			// Non-fatal: log hint but don't fail
			if out.Stats.Hint == "" {
				out.Stats.Hint = fmt.Sprintf("synthesis failed: %v", err)
			}
		} else {
			out.Summary = summary
		}
	}

	// Render tree view if requested
	if in.Format == "tree" {
		treeOpts := retrieval.TreeOptions{
			Depth:       in.TreeDepth,
			MaxChildren: in.TreeMaxChildren,
		}
		// Apply defaults if not set (note: Depth=0 means unlimited, don't override)
		if treeOpts.MaxChildren == 0 {
			treeOpts.MaxChildren = 10
		}
		// Default to including summaries unless explicitly set to false
		if in.TreeIncludeSummaries == nil || *in.TreeIncludeSummaries {
			treeOpts.IncludeSummaries = true
		}

		// Convert results to file entries first
		entries := resultsToFileEntries(fusedResults)

		// Fetch file_summary entries for broader tree coverage
		summaryLimit := treeOpts.MaxChildren * 4
		if summaryLimit < 50 {
			summaryLimit = 50
		}
		fileSummaryEntries, err := fetchFileSummaryEntries(
			ctx,
			cfg,
			workspaceID,
			in.Query,
			fileSummaryEmbedding,
			summaryLimit,
			logger,
		)
		if err != nil {
			logger.Debug().Err(err).Msg("file summary search error")
		} else {
			entries = retrieval.MergeFileEntries(fileSummaryEntries, entries)
		}

		// Enrich entries with summaries if enabled
		maxMissing := in.TreeMaxMissingSummaries
		if maxMissing == 0 {
			maxMissing = 20 // Default: generate up to 20 summaries
		}
		if treeOpts.IncludeSummaries && maxMissing > 0 {
			generated, err := enrichEntriesWithSummaries(ctx, cfg, workspaceID, entries, maxMissing, fileSummaryProvider, logger)
			if err != nil {
				logger.Debug().Err(err).Msg("summary enrichment error")
			}
			logger.Debug().Int("generated", generated).Int("entries", len(entries)).Msg("enriched tree entries with summaries")
		}

		// Generate root summary from top file summaries
		rootSummary := generateRootSummary(ctx, cfg, workspaceID, entries, fileSummaryProvider, logger)

		// Build tree from enriched entries
		builder := retrieval.NewTreeBuilder(treeOpts)
		treeOutput := builder.Build(entries, rootSummary)
		out.Tree = treeOutput
		out.TreeText = builder.RenderText(treeOutput)
	}

	return out, nil
}

// sourceResults holds results from a single search source with timing and error information.
type sourceResults struct {
	source  string
	results []Result
	err     error
	latency time.Duration
	hint    string // Optional hint when source unavailable but not an error
}

// scopedEmbeddings holds query embeddings for different scope groups.
// Different scopes may use different embedding models optimized for that content type.
type scopedEmbeddings struct {
	code   []float32 // For symbols scope (voyage-code-3)
	memory []float32 // For memories scope (voyage-3-large)
	text   []float32 // For tasks, sessions, codemaps (voyage-3.5)
}

// embeddingModelConfig returns the models to use for different scope categories.
// Configuration priority: config overrides > embedding.model fallback.
func embeddingModelConfig(providerName string, cfg config.Config) (codeModel, memoryModel, textModel, fileSummaryModel string) {
	codeModel = semantic.ResolveModelForScope(semantic.ScopeSymbols, cfg)
	memoryModel = semantic.ResolveModelForScope(semantic.ScopeMemory, cfg)
	textModel = semantic.ResolveModelForScope(semantic.ScopeTasks, cfg)
	fileSummaryModel = semantic.ResolveModelForScope(semantic.ScopeFileSummaries, cfg)

	if providerName == "gemini" {
		model := "gemini-embedding-001"
		if strings.HasPrefix(codeModel, "gemini-") {
			model = codeModel
		}
		if strings.HasPrefix(memoryModel, "gemini-") {
			model = memoryModel
		}
		if strings.HasPrefix(textModel, "gemini-") {
			model = textModel
		}
		codeModel = model
		memoryModel = model
		textModel = model
		fileSummaryModel = model
	}

	return codeModel, memoryModel, textModel, fileSummaryModel
}

// createProviderWithModel creates an embedding provider with a specific model.
// Supports both Voyage and Gemini based on available API keys.
// FC/IS: API keys passed from boundary instead of os.Getenv.
func createProviderWithModel(model string, cfg config.Config, voyageKey, geminiKey string) (semantic.EmbeddingProvider, error) {
	provider, err := semantic.NewProviderForModel(
		model,
		cfg,
		semantic.WithVoyageKey(voyageKey),
		semantic.WithGeminiKey(geminiKey),
	)
	if err != nil {
		return nil, skillerr.Auth("no embedding API key available", skillerr.WithHint("Set VOYAGE_API_KEY or GEMINI_API_KEY."))
	}
	return provider, nil
}

// generateScopedEmbeddings creates query embeddings for the requested scopes.
// Only generates embeddings for scope groups that are actually requested.
// Uses scope-specific models:
//   - code model for symbols (voyage-code-3)
//   - memory model for memories (voyage-3-large)
//   - text model for tasks, sessions, codemaps (voyage-3.5)
func generateScopedEmbeddings(ctx context.Context, cfg config.Config, query string, scopeSet map[string]bool, codeModel, memoryModel, textModel, voyageKey, geminiKey string) (scopedEmbeddings, error) {
	var emb scopedEmbeddings
	var wg sync.WaitGroup
	var codeErr, memoryErr, textErr error

	// Check which scope groups are requested
	needsCode := scopeSet[ScopeSymbols]
	needsMemory := scopeSet[ScopeMemories]
	needsText := scopeSet[ScopeSessions] || scopeSet[ScopeTasks] || scopeSet[ScopeCodemaps]

	// Optimization: if all needed models are the same, generate one embedding
	allSameModel := true
	baseModel := ""
	if needsCode {
		baseModel = codeModel
	}
	if needsMemory {
		if baseModel == "" {
			baseModel = memoryModel
		} else if baseModel != memoryModel {
			allSameModel = false
		}
	}
	if needsText {
		if baseModel == "" {
			baseModel = textModel
		} else if baseModel != textModel {
			allSameModel = false
		}
	}

	if allSameModel && baseModel != "" {
		provider, err := createProviderWithModel(baseModel, cfg, voyageKey, geminiKey)
		if err != nil {
			return emb, err
		}
		embedding, err := provider.Embed(ctx, query)
		if err != nil {
			return emb, err
		}
		if needsCode {
			emb.code = embedding
		}
		if needsMemory {
			emb.memory = embedding
		}
		if needsText {
			emb.text = embedding
		}
		return emb, nil
	}

	// Generate code embedding if needed
	if needsCode {
		wg.Add(1)
		go func() {
			defer wg.Done()
			provider, err := createProviderWithModel(codeModel, cfg, voyageKey, geminiKey)
			if err != nil {
				codeErr = err
				return
			}
			emb.code, codeErr = provider.Embed(ctx, query)
		}()
	}

	// Generate memory embedding if needed (separate from other text scopes)
	if needsMemory {
		wg.Add(1)
		go func() {
			defer wg.Done()
			provider, err := createProviderWithModel(memoryModel, cfg, voyageKey, geminiKey)
			if err != nil {
				memoryErr = err
				return
			}
			emb.memory, memoryErr = provider.Embed(ctx, query)
		}()
	}

	// Generate text embedding if needed (for tasks, sessions, codemaps)
	if needsText {
		wg.Add(1)
		go func() {
			defer wg.Done()
			provider, err := createProviderWithModel(textModel, cfg, voyageKey, geminiKey)
			if err != nil {
				textErr = err
				return
			}
			emb.text, textErr = provider.Embed(ctx, query)
		}()
	}

	wg.Wait()

	// Return first error encountered (if any)
	if codeErr != nil && memoryErr != nil && textErr != nil {
		return emb, skillerr.Runtimef("code: %v; memory: %v; text: %v", codeErr, memoryErr, textErr)
	}
	if codeErr != nil {
		return emb, codeErr
	}
	if memoryErr != nil {
		return emb, memoryErr
	}
	if textErr != nil {
		return emb, textErr
	}
	return emb, nil
}

// searchSymbolsWithRetrieval uses the retrieval.Generator for symbol search.
// This provides hybrid search (BM25 + vector when available) with ripgrep fallback.
func searchSymbolsWithRetrieval(
	ctx context.Context,
	cfg config.Config, workspaceID, workspacePath, query string,
	embedProvider semantic.EmbeddingProvider,
	queryEmbedding []float32,
	limit int,
) ([]Result, error) {
	// Open memory store for symbol index access
	memStore, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return nil, skillerr.WrapIO("open memory store", err)
	}
	defer memStore.Close()

	// Create logger (silent for skill context)
	logger := zerolog.Nop()

	// Create generator with embedding provider
	// The Generator handles hybrid search internally (BM25 + semantic when embedProvider is set)
	gen := retrieval.NewGenerator(memStore, embedProvider, workspacePath, logger)
	gen = gen.WithEmbedQueryMode(cfg.Embedding.Flags.QueryMode)

	// Wire up SearchableStore for vector search when embeddings are available
	if embedProvider != nil && queryEmbedding != nil {
		// Only available on concrete *memory.Store, not the interface
		if concreteStore, ok := memStore.(*memory.Store); ok {
			wrappedDB := dbdriver.WrapSQLDB(concreteStore.DB(), dbdriver.DriverSQLite)
			searchStore, searchErr := concreteStore.EnableSearch(wrappedDB, workspaceID)
			if searchErr == nil {
				gen = gen.WithSearchableStore(searchStore)
				logger.Debug().Msg("enabled SearchableStore for hybrid search")
			}
		}
		// If type assertion or EnableSearch fails, we fall back to BM25 (no error returned)
	}

	// Configure options for symbol search
	opts := retrieval.DefaultOptions()
	opts.EnableSymbols = true
	// Enable semantic/vector search when embeddings are available
	opts.EnableSemantic = queryEmbedding != nil
	opts.EnableRipgrep = true // Enable ripgrep fallback
	opts.MaxTotalCandidates = limit * 2
	opts.MaxSymbolCandidates = limit * 2
	opts.MaxSemanticCandidates = limit
	opts.MaxRipgrepCandidates = limit

	// Generate candidates
	genResult, err := gen.Generate(ctx, workspaceID, query, opts)
	if err != nil {
		return nil, skillerr.WrapRuntime("generate candidates", err)
	}

	// Convert candidates to results with normalized IDs
	results := make([]Result, 0, len(genResult.Candidates))
	for i, candidate := range genResult.Candidates {
		result := Result{
			Source:     "symbol",
			ID:         normalizeSymbolID(workspaceID, candidate),
			Name:       candidate.Name,
			Path:       candidate.Path,
			Line:       candidate.Line,
			Similarity: candidate.Score,
			SourceRank: i + 1,
		}

		// Use SymbolID if Name is empty
		if result.Name == "" && candidate.SymbolID != "" {
			result.Name = extractSymbolName(candidate.SymbolID)
		}

		if candidate.Documentation != "" {
			result.Summary = candidate.Documentation
		}

		// Extract code snippet for reranking (reads ~11 lines around the symbol)
		if candidate.Path != "" && candidate.Line > 0 {
			fullPath := candidate.Path
			// If path is relative, join with workspace path
			if !filepath.IsAbs(candidate.Path) {
				fullPath = filepath.Join(workspacePath, candidate.Path)
			}
			result.Snippet = extractSnippet(fullPath, candidate.Line, 5)
		}

		results = append(results, result)
	}

	return results, nil
}

// sessionSearcher abstracts the session search interface for both local and Turso stores.
type sessionSearcher interface {
	SearchSimilar(ctx context.Context, workspace string, embedding []float32, limit int) ([]storage.SimilarSession, error)
	Close() error
}

// globalSessionSearcher extends sessionSearcher with cross-workspace search capabilities.
type globalSessionSearcher interface {
	sessionSearcher
	SearchSimilarGlobal(ctx context.Context, embedding []float32, limit int) ([]storage.SimilarSession, error)
	SearchSimilarMultiWorkspace(ctx context.Context, workspaces []string, embedding []float32, limit int) ([]storage.SimilarSession, error)
}

// searchSessions searches sessions using vector similarity.
// When Turso is configured with vector enabled, it uses TursoStore for cloud-native vector search.
// When in.Remote is true, it uses cross-workspace search capabilities.
func searchSessions(ctx context.Context, cfg config.Config, queryEmbedding []float32, limit int, in *Input) ([]Result, error) {
	var store sessionSearcher
	var err error

	// Use Turso when:
	// 1. Remote mode is requested (always use Turso for cross-workspace)
	// 2. Driver is "turso" AND vector search is enabled
	useTurso := in.Remote || (cfg.Database.Driver == "turso" && cfg.Database.Vector.Enabled && cfg.Database.Turso.URL != "")

	if useTurso {
		// Determine vector dimensions from config (default to embedding dimensions or 1024)
		vectorDims := cfg.Database.Vector.Dimensions
		if vectorDims == 0 {
			vectorDims = cfg.Embedding.Dimensions
		}
		if vectorDims == 0 {
			vectorDims = dbdriver.GetDefaultVectorDimensions()
		}

		// Use Turso for cloud-native vector search
		tursoCfg := dbdriver.TursoConfig{
			URL:              cfg.Database.Turso.URL,
			AuthToken:        cfg.Database.Turso.AuthToken,
			VectorDimensions: vectorDims,
		}
		store, err = sessions.OpenTurso(ctx, tursoCfg)
		if err != nil {
			if in.Remote {
				// Remote mode requires Turso - fail if unavailable
				return nil, skillerr.WrapIO("open turso sessions store (remote mode)", err)
			}
			// Fallback to local store if Turso fails
			store, _, err = sessionkit.OpenSessions(ctx, cfg)
			if err != nil {
				return nil, skillerr.WrapIO("open sessions store (turso fallback)", err)
			}
		}
	} else {
		// Use local SQLite store
		store, _, err = sessionkit.OpenSessions(ctx, cfg)
		if err != nil {
			return nil, skillerr.WrapIO("open sessions store", err)
		}
	}
	defer func() { errs.Ignore(store.Close(), "close session store") }()

	// Use appropriate search method based on remote options
	var similar []storage.SimilarSession

	if in.Remote && (in.Global || len(in.Workspaces) > 0) {
		// Check if store supports global search
		globalStore, ok := store.(globalSessionSearcher)
		if !ok {
			return nil, skillerr.Validation("session store does not support cross-workspace search; Turso required")
		}

		if in.Global {
			similar, err = globalStore.SearchSimilarGlobal(ctx, queryEmbedding, limit)
		} else {
			similar, err = globalStore.SearchSimilarMultiWorkspace(ctx, in.Workspaces, queryEmbedding, limit)
		}
	} else {
		similar, err = store.SearchSimilar(ctx, in.Workspace, queryEmbedding, limit)
	}
	if err != nil {
		return nil, skillerr.WrapIO("search sessions", err)
	}

	results := make([]Result, 0, len(similar))
	rank := 0
	for _, s := range similar {
		// Skip sessions with empty or placeholder summaries
		if isEmptySessionSummary(s.Session.Summary) {
			continue
		}
		rank++
		result := Result{
			Source:     "session",
			ID:         normalizeSessionID(s.Session.ID),
			Name:       getSessionName(s.Session),
			Summary:    skillout.TruncateString(s.Session.Summary, 200),
			Similarity: s.Similarity,
			SourceRank: rank,
		}
		results = append(results, result)
	}

	return results, nil
}

// searchMemories searches named memories using vector search (Turso) or BM25 text search (SQLite).
// Uses Turso with native vector search when configured and embeddings are available.
// When in.Remote is true, it uses cross-workspace search capabilities.
// Returns results, optional hint (when vector search was skipped), and error.
func searchMemories(
	ctx context.Context,
	cfg config.Config, workspaceID, query string,
	queryEmbedding []float32,
	limit int,
	in *Input,
) ([]Result, string, error) {
	// Check if we should use Turso vector search
	// Use Turso when remote is requested OR when configured
	useTurso := in.Remote || (cfg.Database.Driver == "turso" && cfg.Database.Vector.Enabled && cfg.Database.Turso.URL != "" && queryEmbedding != nil)

	var scoredEntries []storage.ScoredEntry

	if useTurso {
		// Remote mode requires embeddings
		if queryEmbedding == nil {
			return nil, "memory remote search requires embeddings; set VOYAGE_API_KEY or GEMINI_API_KEY", nil
		}

		// Determine vector dimensions from config
		vectorDims := cfg.Database.Vector.Dimensions
		if vectorDims == 0 {
			vectorDims = cfg.Embedding.Dimensions
		}
		if vectorDims == 0 {
			vectorDims = dbdriver.GetDefaultVectorDimensions()
		}

		// Use Turso for vector search
		tursoCfg := dbdriver.TursoConfig{
			URL:              cfg.Database.Turso.URL,
			AuthToken:        cfg.Database.Turso.AuthToken,
			VectorDimensions: vectorDims,
		}
		tursoStore, err := memory.OpenTurso(ctx, tursoCfg)
		if err != nil {
			if in.Remote {
				// Remote mode requires Turso - fail if unavailable
				return nil, "", skillerr.WrapIO("open turso memory store (remote mode)", err)
			}
			// Fallback to BM25 if Turso fails, with hint about the failure
			hint := fmt.Sprintf("memory vector search unavailable: %v; using BM25 fallback", err)
			results, bm25Err := searchMemoriesBM25(ctx, cfg, workspaceID, query, limit)
			return results, hint, bm25Err
		}
		defer func() { errs.Ignore(tursoStore.Close(), "close turso memory store") }()

		// Use appropriate search method based on remote options
		if in.Remote && in.Global {
			scoredEntries, err = tursoStore.SearchSimilarGlobal(ctx, queryEmbedding, limit)
		} else if in.Remote && len(in.Workspaces) > 0 {
			scoredEntries, err = tursoStore.SearchSimilarMultiWorkspace(ctx, in.Workspaces, queryEmbedding, limit)
		} else {
			scoredEntries, err = tursoStore.SearchSimilar(ctx, workspaceID, queryEmbedding, limit)
		}
		if err != nil {
			if in.Remote {
				// Remote mode - don't fallback to BM25
				return nil, "", skillerr.WrapRuntime("memory remote search failed", err)
			}
			// Fallback to BM25 on error, with hint about the failure
			hint := fmt.Sprintf("memory vector search failed: %v; using BM25 fallback", err)
			results, bm25Err := searchMemoriesBM25(ctx, cfg, workspaceID, query, limit)
			return results, hint, bm25Err
		}

		// Fallback to BM25 if vector search returns empty (may indicate missing vectors)
		// Skip fallback for remote mode - empty results are valid
		if len(scoredEntries) == 0 && !in.Remote {
			hint := "memory vector search returned no results; trying BM25 fallback"
			results, bm25Err := searchMemoriesBM25(ctx, cfg, workspaceID, query, limit)
			if bm25Err != nil {
				return nil, hint, bm25Err
			}
			if len(results) > 0 {
				// BM25 found results, return with hint about vector search being empty
				return results, hint, nil
			}
			// Both searches empty - vector is authoritative
			return nil, "", nil
		}
	} else {
		// SQLite store - use vector search if embeddings available, otherwise BM25
		if queryEmbedding != nil {
			// Use in-memory cosine similarity search
			results, err := searchMemoriesVector(ctx, cfg, workspaceID, queryEmbedding, limit)
			if err == nil && len(results) > 0 {
				return results, "", nil
			}
			// Fall back to BM25 if vector search fails or returns empty
			if err != nil {
				hint := fmt.Sprintf("memory vector search failed: %v; using BM25 fallback", err)
				results, bm25Err := searchMemoriesBM25(ctx, cfg, workspaceID, query, limit)
				return results, hint, bm25Err
			}
		}
		// Use SQLite BM25 search (no hint needed - this is expected behavior)
		results, err := searchMemoriesBM25(ctx, cfg, workspaceID, query, limit)
		return results, "", err
	}

	// Convert scored entries to results
	results := make([]Result, 0, len(scoredEntries))
	rank := 1
	for _, scored := range scoredEntries {
		entry := scored.Entry
		// Skip code-related entries - they're handled by symbol search
		// Only include semantic memory types: gotcha, pattern, decision, note, etc.
		if entry.Type == "code_symbol" || entry.Type == "symbol" || entry.Type == "file_embedding" || entry.Type == "edit" {
			continue
		}

		result := Result{
			Source:     "memory",
			ID:         normalizeMemoryID(entry.Name),
			Name:       entry.Name,
			Summary:    skillout.TruncateString(entry.Summary, 200),
			Similarity: scored.Score,
			SourceRank: rank,
		}
		results = append(results, result)
		rank++

		if rank > limit {
			break
		}
	}

	return results, "", nil // Vector search succeeded, no hint needed
}

// searchMemoriesBM25 uses SQLite BM25-like text search for memories.
func searchMemoriesBM25(
	ctx context.Context,
	cfg config.Config, workspaceID, query string,
	limit int,
) ([]Result, error) {
	// Open memory store
	memStore, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return nil, skillerr.WrapIO("open memory store", err)
	}
	defer memStore.Close()

	// Request more items than limit to account for type filtering
	// Code-related types (code_symbol, file_embedding, edit) often dominate the memory store
	fetchLimit := max(limit*10, 100)

	// Use basic text search on memories (BM25-like)
	scoredEntries, err := memStore.Search(ctx, workspaceID, query, fetchLimit)
	if err != nil {
		return nil, skillerr.WrapIO("search memories", err)
	}

	// Filter out symbol-type entries (they're handled by symbol search)
	results := make([]Result, 0, len(scoredEntries))
	rank := 1
	for _, scored := range scoredEntries {
		entry := scored.Entry
		// Skip code-related entries - they're handled by symbol search
		// Only include semantic memory types: gotcha, pattern, decision, note, etc.
		if entry.Type == "code_symbol" || entry.Type == "symbol" || entry.Type == "file_embedding" || entry.Type == "edit" {
			continue
		}

		// BM25 scores are based on recency/frequency (0-1 range, typically <0.3)
		// Normalize to 0.3-1.0 range so results pass min_similarity filter
		// RRF uses ranks for fusion, so score mainly affects threshold filtering
		normalizedScore := 0.3 + (scored.Score * 0.7)
		if normalizedScore > 1.0 {
			normalizedScore = 1.0
		}

		result := Result{
			Source:     "memory",
			ID:         normalizeMemoryID(entry.Name),
			Name:       entry.Name,
			Summary:    skillout.TruncateString(entry.Summary, 200),
			Similarity: normalizedScore,
			SourceRank: rank,
		}
		results = append(results, result)
		rank++

		if rank > limit {
			break
		}
	}

	return results, nil
}

// searchMemoriesVector uses SQLite in-memory cosine similarity search.
func searchMemoriesVector(
	ctx context.Context,
	cfg config.Config, workspaceID string,
	queryEmbedding []float32,
	limit int,
) ([]Result, error) {
	// Open memory store
	memStore, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return nil, skillerr.WrapIO("open memory store", err)
	}
	defer memStore.Close()

	// Request more items than limit to account for type filtering
	// Code-related types (code_symbol, file_embedding, edit) often dominate the memory store
	fetchLimit := max(limit*10, 100)

	// Use vector similarity search
	scoredEntries, err := memStore.SearchSimilar(ctx, workspaceID, queryEmbedding, fetchLimit)
	if err != nil {
		return nil, skillerr.WrapIO("vector search memories", err)
	}

	// Filter out symbol-type entries (they're handled by symbol search)
	results := make([]Result, 0, len(scoredEntries))
	rank := 1
	for _, scored := range scoredEntries {
		entry := scored.Entry
		// Skip code-related entries - they're handled by symbol search
		// Only include semantic memory types: gotcha, pattern, decision, note, etc.
		if entry.Type == "code_symbol" || entry.Type == "symbol" || entry.Type == "file_embedding" || entry.Type == "edit" {
			continue
		}

		result := Result{
			Source:     "memory",
			ID:         normalizeMemoryID(entry.Name),
			Name:       entry.Name,
			Summary:    skillout.TruncateString(entry.Summary, 200),
			Similarity: scored.Score,
			SourceRank: rank,
		}
		results = append(results, result)
		rank++

		if rank > limit {
			break
		}
	}

	return results, nil
}

// ID normalization functions for canonical IDs

// normalizeSymbolID creates a canonical ID for symbol results.
func normalizeSymbolID(workspaceID string, candidate retrieval.Candidate) string {
	// Format: symbol:<workspace>:<path>#L<line>
	id := fmt.Sprintf("symbol:%s:%s", workspaceID, candidate.Path)
	if candidate.Line > 0 {
		id = fmt.Sprintf("%s#L%d", id, candidate.Line)
	}
	return id
}

// normalizeSessionID creates a canonical ID for session results.
func normalizeSessionID(sessionID string) string {
	// Format: session:<id>
	return fmt.Sprintf("session:%s", sessionID)
}

// normalizeMemoryID creates a canonical ID for memory results.
func normalizeMemoryID(name string) string {
	// Format: memory:<name>
	return fmt.Sprintf("memory:%s", name)
}

// normalizeTaskID creates a canonical ID for task results.
func normalizeTaskID(taskID string) string {
	// Format: task:<id>
	return fmt.Sprintf("task:%s", taskID)
}

// normalizeCodemapID creates a canonical ID for codemap results.
func normalizeCodemapID(codemapID string) string {
	// Format: codemap:<id>
	return fmt.Sprintf("codemap:%s", codemapID)
}

// searchTasks searches task embeddings using vector similarity.
func searchTasks(
	ctx context.Context,
	cfg config.Config, workspaceID string,
	queryEmbedding []float32,
	limit int,
) ([]Result, error) {
	// Open memory store (task embeddings are stored in named_memory)
	memStore, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return nil, skillerr.WrapIO("open memory store", err)
	}
	defer memStore.Close()

	// Search for similar task entries
	scoredEntries, err := memStore.SearchSimilarByType(ctx, workspaceID, "task_embedding", queryEmbedding, limit*2)
	if err != nil {
		return nil, skillerr.WrapIO("vector search tasks", err)
	}

	// No task embeddings indexed yet - return empty results (not an error)
	// This matches the behavior of other scopes (memories, codemaps) for consistency
	if len(scoredEntries) == 0 {
		return nil, nil
	}

	// Open tasks store to get full task details
	// Note: tasks.Open expects root directory, it appends "tasks.db" internally
	taskStore, cleanup, err := sessionkit.OpenTasks(ctx, cfg)
	if err != nil {
		return nil, skillerr.WrapIO("open tasks store", err)
	}
	defer cleanup()

	// Filter to only task_embedding entries and fetch task details
	results := make([]Result, 0, len(scoredEntries))
	rank := 1
	for _, scored := range scoredEntries {
		entry := scored.Entry

		// Only process task embedding entries
		if entry.Type != "task_embedding" {
			continue
		}

		// Extract task ID from name: "task://<task_id>"
		if !strings.HasPrefix(entry.Name, "task://") {
			continue
		}
		taskID := strings.TrimPrefix(entry.Name, "task://")

		// Fetch full task details
		task, err := taskStore.Get(ctx, taskID)
		if err != nil {
			// Task may have been deleted after embedding was created
			continue
		}

		// Build snippet from task content
		var snippetParts []string
		snippetParts = append(snippetParts, task.Title)
		if task.Notes != "" {
			snippetParts = append(snippetParts, "Notes: "+skillout.TruncateString(task.Notes, 100))
		}
		if task.Gotchas != "" {
			snippetParts = append(snippetParts, "Gotchas: "+skillout.TruncateString(task.Gotchas, 100))
		}
		snippet := strings.Join(snippetParts, " | ")

		result := Result{
			Source:     "task",
			ID:         normalizeTaskID(taskID),
			Name:       task.Title,
			Summary:    skillout.TruncateString(task.Description, 200),
			Snippet:    skillout.TruncateString(snippet, 300),
			Similarity: scored.Score,
			SourceRank: rank,
		}
		results = append(results, result)
		rank++

		if rank > limit {
			break
		}
	}

	return results, nil
}

// searchCodemaps searches codemap embeddings using vector similarity.
// Codemaps are stored in named_memory with type="codemap".
func searchCodemaps(
	ctx context.Context,
	cfg config.Config, workspaceID string,
	queryEmbedding []float32,
	limit int,
) ([]Result, error) {
	// Open memory store (codemap embeddings are stored in named_memory)
	memStore, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return nil, skillerr.WrapIO("open memory store", err)
	}
	defer memStore.Close()

	// Search for similar entries (codemaps + codemap chunks)
	scoredEntries := make([]storage.ScoredEntry, 0, limit*4)
	codemapEntries, err := memStore.SearchSimilarByType(ctx, workspaceID, "codemap", queryEmbedding, limit*2)
	if err != nil {
		return nil, skillerr.WrapIO("vector search codemaps", err)
	}
	chunkEntries, err := memStore.SearchSimilarByType(ctx, workspaceID, "codemap_chunk", queryEmbedding, limit*2)
	if err != nil {
		return nil, skillerr.WrapIO("vector search codemap chunks", err)
	}
	scoredEntries = append(scoredEntries, codemapEntries...)
	scoredEntries = append(scoredEntries, chunkEntries...)
	sort.Slice(scoredEntries, func(i, j int) bool {
		return scoredEntries[i].Score > scoredEntries[j].Score
	})

	// Filter to only codemap entries (including codemap chunks)
	results := make([]Result, 0, len(scoredEntries))
	seen := make(map[string]int)
	rank := 1
	for _, scored := range scoredEntries {
		entry := scored.Entry

		// Only process codemap entries
		if entry.Type != "codemap" && entry.Type != "codemap_chunk" {
			continue
		}

		// Extract codemap ID from name: "codemap://<id>" or "codemap://<id>#chunk:<id>"
		codemapID := strings.TrimPrefix(entry.Name, "codemap://")
		codemapID = strings.TrimPrefix(codemapID, "codemap:")
		if idx := strings.Index(codemapID, "#chunk:"); idx >= 0 {
			codemapID = codemapID[:idx]
		}
		if codemapID == "" {
			continue
		}

		// Build snippet from codemap data
		snippet := entry.Summary
		if entry.Type == "codemap" && snippet == "" && entry.Result != nil {
			// Try to extract title and description from result JSON
			var data map[string]any
			if json.Unmarshal(entry.Result, &data) == nil {
				if title, ok := data["title"].(string); ok {
					snippet = title
				}
				if desc, ok := data["description"].(string); ok {
					if snippet != "" {
						snippet += " - " + desc
					} else {
						snippet = desc
					}
				}
			}
		}

		if snippet == "" {
			snippet = entry.Name
		}

		displayName := entry.Name
		if entry.Type == "codemap_chunk" {
			displayName = "codemap://" + codemapID
		}

		summary := entry.Summary
		if summary == "" {
			summary = snippet
		}

		result := Result{
			Source:     "codemap",
			ID:         normalizeCodemapID(codemapID),
			Name:       displayName,
			Summary:    skillout.TruncateString(summary, 200),
			Snippet:    skillout.TruncateString(snippet, 300),
			Similarity: scored.Score,
			SourceRank: rank,
		}
		if idx, exists := seen[codemapID]; exists {
			if scored.Score > results[idx].Similarity {
				results[idx].Similarity = scored.Score
				results[idx].Snippet = skillout.TruncateString(snippet, 300)
				if entry.Summary != "" {
					results[idx].Summary = skillout.TruncateString(entry.Summary, 200)
				}
			}
			continue
		}

		results = append(results, result)
		seen[codemapID] = len(results) - 1
		rank++

		if rank > limit {
			break
		}
	}

	return results, nil
}

// isEmptySessionSummary returns true if the session summary is empty or a placeholder.
// isEmptySessionSummary returns true if the session summary is empty or a placeholder.
func isEmptySessionSummary(summary string) bool {
	s := strings.TrimSpace(strings.ToLower(summary))
	if s == "" {
		return true
	}
	// Filter common placeholder summaries from failed summarization
	placeholders := []string{
		"no coding session",
		"no conversation",
		"no session was provided",
		"empty conversation",
	}
	for _, p := range placeholders {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// getSessionName extracts a display name from a session.
func getSessionName(s storage.Session) string {
	if s.Summary != "" {
		// Use first line of summary as name
		lines := strings.SplitN(s.Summary, "\n", 2)
		if len(lines) > 0 {
			name := strings.TrimSpace(lines[0])
			if len(name) > 60 {
				name = name[:57] + "..."
			}
			return name
		}
	}
	if s.ProjectName != "" {
		return s.ProjectName
	}
	return s.ID
}

// reciprocalRankFusion combines results from multiple sources using RRF algorithm.
func reciprocalRankFusion(sourceResults map[string][]Result, minSimilarity float64) []Result {
	if len(sourceResults) == 0 {
		return []Result{}
	}

	// Optional per-source weights (tune as needed)
	sourceWeights := map[string]float64{
		ScopeSymbols:  1.0,
		ScopeTasks:    0.95, // Tasks are high-value for understanding work context
		ScopeCodemaps: 0.95, // Codemaps provide rich relationship context
		ScopeSessions: 0.9,
		ScopeMemories: 0.7,
	}

	// Calculate RRF scores
	scores := make(map[string]float64) // ID -> weighted RRF score
	items := make(map[string]Result)   // ID -> Result

	for _, results := range sourceResults {
		for rank, result := range results {
			if result.Similarity < minSimilarity {
				continue
			}

			weight := sourceWeights[result.Source]
			if weight == 0 {
				weight = 1.0
			}

			key := result.ID
			// RRF score: 1 / (k + rank)
			scores[key] += weight * (1.0 / float64(RRFConstant+rank+1))

			if _, exists := items[key]; !exists {
				items[key] = result
			}
		}
	}

	// Convert to sorted slice
	var results []Result
	for key, item := range items {
		item.RRFScore = scores[key]
		results = append(results, item)
	}

	// Sort by RRF score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].RRFScore > results[j].RRFScore
	})

	return results
}

// applyPageRankBoost looks up PageRank scores from the dependency graph and applies
// the weighted scoring formula: FinalScore = 0.50*RRFScore + 0.30*PageRank + 0.20*Connection
// Results are re-sorted by FinalScore only if meaningful PageRank data exists.
// When no PageRank data is available, FinalScore = RRFScore (no reordering).
func applyPageRankBoost(ctx context.Context, cfg config.Config, workspaceID string, results []Result) []Result {
	if len(results) == 0 {
		return results
	}

	// Open graph store
	graphStore, err := graph.Open(ctx, cfg.Storage.Root)
	if err != nil {
		// Graph unavailable - use RRFScore as FinalScore (no reordering)
		for i := range results {
			results[i].FinalScore = results[i].RRFScore
		}
		return results
	}
	defer func() { errs.Ignore(graphStore.Close(), "close graph store") }()

	// Build lookup maps by path and name for symbols
	pathRanks := make(map[string]float64)
	nameRanks := make(map[string]float64)

	// Get top nodes to build lookup (covers high-PageRank items)
	topNodes, err := graphStore.TopNodes(ctx, graph.TopNodesOptions{
		Workspace: workspaceID,
		Limit:     500, // Get enough to cover likely matches
	})
	if err == nil {
		for _, node := range topNodes {
			if node.CurrentPath != "" {
				pathRanks[node.CurrentPath] = node.PageRank
			}
			// Also index by title (symbol name)
			if node.Title != "" {
				nameRanks[node.Title] = node.PageRank
			}
		}
	}

	// If no PageRank data exists, use RRFScore as FinalScore (no reordering)
	// This prevents changing the scoring regime when graph_pagerank hasn't been run
	if len(topNodes) == 0 || (len(pathRanks) == 0 && len(nameRanks) == 0) {
		for i := range results {
			results[i].FinalScore = results[i].RRFScore
		}
		return results
	}

	// Find max PageRank for normalization
	maxPR := DefaultMaxPR
	if topNodes[0].PageRank > maxPR {
		maxPR = topNodes[0].PageRank
	}

	// Apply PageRank boost to each result
	for i := range results {
		r := &results[i]

		// Look up PageRank by path or name
		var pr float64
		if r.Path != "" {
			if rank, ok := pathRanks[r.Path]; ok {
				pr = rank
			}
		}
		if pr == 0 && r.Name != "" {
			if rank, ok := nameRanks[r.Name]; ok {
				pr = rank
			}
		}

		// Normalize PageRank to 0-1 range
		normalizedPR := pr / maxPR
		if normalizedPR > 1.0 {
			normalizedPR = 1.0
		}

		r.PageRank = normalizedPR

		// Calculate final score using weighted formula with RRFScore as base
		// ConnectionBoost is currently 0 (future: based on graph connectivity to active session/task)
		connectionBoost := 0.0

		r.FinalScore = WeightSimilarity*r.RRFScore + WeightPageRank*normalizedPR + WeightConnection*connectionBoost
	}

	// Re-sort by final score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].FinalScore > results[j].FinalScore
	})

	return results
}

// rerankStatsResult holds reranking statistics for output.
type rerankStatsResult struct {
	enabled   bool
	model     string
	latencyMS int
	count     int
}

// applyReranking applies Voyage rerank-2.5 to improve result relevance.
// Reranking uses cross-attention between query and documents for better precision.
// Returns original results unchanged if reranking is disabled or fails.
func applyReranking(ctx context.Context, logger zerolog.Logger, in Input, results []Result) ([]Result, rerankStatsResult) {
	stats := rerankStatsResult{}

	// Check if reranking is enabled
	rerankCfg := rerank.FromEnv()
	if in.RerankEnabled {
		rerankCfg.Enabled = true
	}
	if in.RerankTopK > 0 {
		rerankCfg.TopK = in.RerankTopK
	}
	if in.RerankModel != "" {
		rerankCfg.Model = in.RerankModel
	}

	if !rerankCfg.Enabled || len(results) == 0 {
		return results, stats
	}

	// Create reranker provider
	provider, err := rerank.NewVoyageProvider(rerankCfg.ToVoyageConfig())
	if err != nil {
		// Reranking unavailable (no API key) - return original results silently
		return results, stats
	}

	// Determine how many candidates to rerank
	topK := rerankCfg.TopK
	if topK <= 0 || topK > len(results) {
		topK = len(results)
	}

	// Convert results to rerank candidates
	candidates := make([]rerank.Candidate, topK)
	for i := 0; i < topK; i++ {
		r := results[i]
		content := buildRerankContent(r)
		candidates[i] = rerank.Candidate{
			ID:            r.ID,
			Content:       content,
			OriginalScore: r.FinalScore,
			Metadata: map[string]any{
				"index": i,
			},
		}
	}

	// Rerank candidates
	start := time.Now()
	reranked, err := provider.Rerank(ctx, in.Query, candidates, 0)
	latencyMS := int(time.Since(start).Milliseconds())

	if err != nil {
		// Reranking failed - return original results silently
		return results, stats
	}

	// Build reranked results
	rerankedResults := make([]Result, 0, len(reranked))
	for _, rr := range reranked {
		val, ok := rr.Metadata["index"]
		if !ok {
			logger.Warn().
				Str("rerank_id", rr.ID).
				Msg("rerank: missing index in metadata; skipping")
			continue
		}
		idx, ok := val.(int)
		if !ok {
			logger.Warn().
				Str("rerank_id", rr.ID).
				Interface("index_value", val).
				Msg("rerank: index is not int; skipping")
			continue
		}
		if idx < 0 || idx >= len(results) {
			logger.Warn().
				Str("rerank_id", rr.ID).
				Int("index", idx).
				Int("results_len", len(results)).
				Msg("rerank: index out of bounds; skipping")
			continue
		}
		r := results[idx]
		r.RerankScore = rr.RerankScore
		r.FinalScore = rr.FinalScore // Use reranker's final score
		rerankedResults = append(rerankedResults, r)
	}

	// Append remaining results that weren't reranked (keep original order)
	if topK < len(results) {
		rerankedResults = append(rerankedResults, results[topK:]...)
	}

	stats.enabled = true
	stats.model = provider.Model()
	stats.latencyMS = latencyMS
	stats.count = len(candidates)

	return rerankedResults, stats
}

const maxRerankContentLen = 2000

func buildRerankContent(r Result) string {
	summary := strings.TrimSpace(r.Summary)
	snippet := strings.TrimSpace(r.Snippet)
	var content string
	switch {
	case summary != "" && snippet != "":
		content = summary + "\n\n" + snippet
	case summary != "":
		content = summary
	case snippet != "":
		content = snippet
	default:
		content = strings.TrimSpace(r.Name)
	}
	return truncateForRerank(content, maxRerankContentLen)
}

func truncateForRerank(content string, maxLen int) string {
	if maxLen <= 0 || len(content) <= maxLen {
		return content
	}
	return content[:maxLen-3] + "..."
}

// extractContextHints extracts context hints from session results.
func extractContextHints(sessionResults []Result, maxHints int) []ContextHint {
	if len(sessionResults) == 0 {
		return nil
	}

	// For now, we create hints from the top session results
	// In a full implementation, we'd fetch full session data and extract gotchas/decisions
	hints := make([]ContextHint, 0, maxHints)

	for i, r := range sessionResults {
		if i >= maxHints {
			break
		}

		sessionID := strings.TrimPrefix(r.ID, "session:")
		hint := ContextHint{
			Type:      "past_solution",
			SessionID: sessionID,
			Summary:   r.Summary,
		}
		hints = append(hints, hint)
	}

	return hints
}

// extractSymbolName extracts the symbol name from a symbol ID string.
func extractSymbolName(symbolID string) string {
	// Symbol ID format: file.go:FunctionName or file.go:Type.Method
	parts := strings.SplitN(symbolID, ":", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return symbolID
}

// extractSnippet reads a code snippet from a file around the given line number.
// Returns up to contextLines before and after the target line (default: 5 lines each = ~11 lines total).
// Returns empty string if file cannot be read or line is out of bounds.
func extractSnippet(filePath string, targetLine, contextLines int) string {
	if contextLines <= 0 {
		contextLines = 5 // Default: 5 lines before and after
	}

	// Read file contents
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(data), "\n")
	if targetLine <= 0 || targetLine > len(lines) {
		return ""
	}

	// Calculate range (0-indexed)
	startIdx := targetLine - 1 - contextLines
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := targetLine - 1 + contextLines
	if endIdx >= len(lines) {
		endIdx = len(lines) - 1
	}

	// Extract snippet lines
	snippetLines := lines[startIdx : endIdx+1]

	// Join and truncate if too long
	snippet := strings.Join(snippetLines, "\n")
	const maxSnippetLen = 500
	if len(snippet) > maxSnippetLen {
		snippet = snippet[:maxSnippetLen-3] + "..."
	}

	return snippet
}

// LLMProvider represents an LLM provider for synthesis.
type LLMProvider = llmproviders.Provider

// synthesizeResults sends search results to an LLM for intelligent synthesis.
func synthesizeResults(ctx context.Context, query string, results []Result, modelOverride string) (*SynthesisSummary, error) {
	providers := llmproviders.SynthesisProviders(modelOverride)
	if len(providers) == 0 {
		return nil, skillerr.Auth("no LLM provider available", skillerr.WithHint("Set OPENROUTER_API_KEY, GROQ_API_KEY, or CEREBRAS_API_KEY."))
	}

	prompt := buildSynthesisPrompt(query, results)

	// Try providers in order
	var lastErr error
	for _, provider := range providers {
		summary, err := callLLMProvider(ctx, provider, prompt)
		if err != nil {
			lastErr = err
			continue
		}
		summary.Model = provider.Name
		return summary, nil
	}

	return nil, skillerr.WrapRuntime("all LLM providers failed", lastErr)
}

// callLLMProvider calls an OpenAI-compatible API endpoint for synthesis.
func callLLMProvider(ctx context.Context, provider LLMProvider, prompt string) (*SynthesisSummary, error) {
	reqBody := map[string]any{
		"model":      provider.Model,
		"max_tokens": 4096,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, skillerr.WrapRuntime("marshal request", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, skillerr.WrapRuntime("create request", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)

	// OpenRouter requires additional headers
	if strings.HasPrefix(provider.Name, "openrouter:") {
		req.Header.Set("HTTP-Referer", "https://github.com/jkatigb/agentctl")
		req.Header.Set("X-Title", "agentctl")
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, skillerr.WrapRuntime("send request", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, skillerr.Runtimef("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, skillerr.WrapParse("decode response", err)
	}

	if len(result.Choices) == 0 {
		return nil, skillerr.Runtimef("empty response from %s", provider.Name)
	}

	// Parse the JSON response
	summary, err := parseSynthesisResponse(result.Choices[0].Message.Content)
	if err != nil {
		return nil, err
	}
	summary.TokensUsed = result.Usage.TotalTokens
	return summary, nil
}

// buildSynthesisPrompt builds the prompt for LLM synthesis of search results.
func buildSynthesisPrompt(query string, results []Result) string {
	var sb strings.Builder
	sb.WriteString("You are a code analysis assistant. Synthesize these search results to answer the user's question.\n\n")
	sb.WriteString("## User Question\n")
	sb.WriteString(query)
	sb.WriteString("\n\n## Search Results\n\n")

	for i, r := range results {
		if i >= 10 { // Limit to top 10 results
			break
		}
		fmt.Fprintf(&sb, "### Result %d: %s (%s)\n", i+1, r.Name, r.Source)
		if r.Path != "" {
			fmt.Fprintf(&sb, "**Path:** %s\n", r.Path)
		}
		if r.Snippet != "" {
			fmt.Fprintf(&sb, "```\n%s\n```\n", r.Snippet)
		}
		if r.Summary != "" {
			fmt.Fprintf(&sb, "%s\n", r.Summary)
		}
		sb.WriteString("\n")
	}

	sb.WriteString(`## Instructions
Provide a structured JSON response with:
{
  "answer": "Direct answer to the question (2-4 sentences)",
  "key_insights": ["Important finding 1", "Important finding 2", ...],
  "gotchas": ["Warning or caveat 1", ...],
  "next_steps": ["Suggested follow-up action 1", ...]
}

Be concise and specific. Focus on actionable insights from the search results.
Respond ONLY with the JSON object, no other text.`)

	return sb.String()
}

// parseSynthesisResponse parses the LLM response into a SynthesisSummary.
func parseSynthesisResponse(content string) (*SynthesisSummary, error) {
	// Try to extract JSON from response (may have markdown code blocks)
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
		if idx := strings.Index(content, "```"); idx > 0 {
			content = content[:idx]
		}
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
		if idx := strings.Index(content, "```"); idx > 0 {
			content = content[:idx]
		}
	}
	content = strings.TrimSpace(content)

	var summary SynthesisSummary
	if err := json.Unmarshal([]byte(content), &summary); err != nil {
		// If JSON parsing fails, use the raw content as the answer
		summary.Answer = content
	}

	return &summary, nil
}

// newSummaryLLM creates a new LLM client for file summaries.
// Returns nil if no provider is available.
func newSummaryLLM(logger zerolog.Logger) retrieval.SummaryLLM {
	providers := llmproviders.FileSummaryProviders()
	if len(providers) == 0 {
		return nil
	}
	llm := llmproviders.NewSummaryLLM(providers[0])
	if llm == nil {
		logger.Debug().Str("provider", providers[0].Name).Msg("summary LLM unavailable")
		return nil
	}
	return llm
}

// resultsToFileEntries converts search results to file entries for tree building.
// Deduplicates by path, keeping the highest scoring entry for each path.
func resultsToFileEntries(results []Result) []retrieval.FileEntry {
	var entries []retrieval.FileEntry
	seen := make(map[string]bool)

	for _, r := range results {
		if r.Path == "" {
			continue
		}
		// Deduplicate by path (keep highest score - results are already sorted by score)
		if seen[r.Path] {
			continue
		}
		seen[r.Path] = true

		score := r.FinalScore
		if score <= 0 {
			score = r.Similarity
		}
		entries = append(entries, retrieval.FileEntry{
			Path:    r.Path,
			Score:   score,
			Summary: r.Summary,
		})
	}

	return entries
}

// fetchFileSummaryEntries searches file_summary entries for tree coverage.
func fetchFileSummaryEntries(
	ctx context.Context,
	cfg config.Config,
	workspaceID string,
	query string,
	queryEmbedding []float32,
	limit int,
	logger zerolog.Logger,
) ([]retrieval.FileEntry, error) {
	memStore, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open memory store: %w", err)
	}
	defer memStore.Close()

	entries, err := retrieval.SearchFileSummaries(ctx, memStore, workspaceID, query, queryEmbedding, limit)
	if err != nil {
		logger.Debug().Err(err).Msg("file summary search failed")
		return nil, err
	}

	return entries, nil
}

// buildFullRepoTree scans the workspace and returns a tree of all code files.
// Used when query is empty to provide a full repository overview.
func buildFullRepoTree(ctx context.Context, logger zerolog.Logger, cfg config.Config, in *Input) (*Output, error) {
	// Scan workspace for code files
	var entries []retrieval.FileEntry
	err := filepath.WalkDir(in.Workspace, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Skip hidden directories and common non-code directories
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__" || name == "dist" || name == "build" {
				return filepath.SkipDir
			}
			return nil
		}

		// Only include code files
		ext := strings.ToLower(filepath.Ext(path))
		codeExts := map[string]bool{
			".go": true, ".py": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true,
			".rs": true, ".rb": true, ".java": true, ".c": true, ".cpp": true, ".h": true,
			".cs": true, ".php": true, ".swift": true, ".kt": true, ".scala": true,
			".sh": true, ".bash": true, ".zsh": true, ".yaml": true, ".yml": true, ".toml": true,
			".json": true, ".md": true, ".sql": true, ".graphql": true, ".proto": true,
		}
		if !codeExts[ext] {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(in.Workspace, path)
		if err != nil {
			return nil
		}

		entries = append(entries, retrieval.FileEntry{
			Path:  relPath,
			Score: 1.0, // All files equally relevant for full repo tree
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan workspace: %w", err)
	}

	// Apply tree options
	treeOpts := retrieval.TreeOptions{
		Depth:       in.TreeDepth,
		MaxChildren: in.TreeMaxChildren,
	}
	if treeOpts.MaxChildren == 0 {
		treeOpts.MaxChildren = 50 // Higher default for full repo
	}
	if in.TreeIncludeSummaries == nil || *in.TreeIncludeSummaries {
		treeOpts.IncludeSummaries = true
	}

	// Enrich with summaries if enabled
	maxMissing := in.TreeMaxMissingSummaries
	if maxMissing == 0 {
		maxMissing = 50 // Higher default for full repo
	}
	if treeOpts.IncludeSummaries && maxMissing > 0 {
		_, err := enrichEntriesWithSummaries(ctx, cfg, in.Workspace, entries, maxMissing, nil, logger)
		if err != nil {
			logger.Debug().Err(err).Msg("failed to enrich summaries")
		}
	}

	// Build tree
	builder := retrieval.NewTreeBuilder(treeOpts)

	// Generate root summary from top entries
	rootSummary := generateRootSummary(ctx, cfg, in.Workspace, entries, nil, logger)

	tree := builder.Build(entries, rootSummary)
	treeText := builder.RenderText(tree)

	return &Output{
		Query:    "(full repository)",
		Results:  []Result{},
		TreeText: treeText,
		Tree:     tree,
		Stats: SearchStats{
			TotalResults:    len(entries),
			SourceCounts:    map[string]int{"files": len(entries)},
			SourceLatencies: map[string]int{},
		},
	}, nil
}

// extractGoFileMetadata extracts package name, doc comment, and top symbols from a Go file.
// Returns populated FileSummaryInput or minimal input if extraction fails.
func extractGoFileMetadata(workspace, filePath string) symbol.FileSummaryInput {
	input := symbol.FileSummaryInput{FilePath: filePath}

	// Only process Go files
	if !strings.HasSuffix(filePath, ".go") {
		return input
	}

	// Construct full path
	fullPath := filepath.Join(workspace, filePath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return input
	}

	// Parse the file
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, fullPath, content, parser.ParseComments)
	if err != nil {
		return input
	}

	// Extract package name
	if f.Name != nil {
		input.Package = f.Name.Name
	}

	// Extract package doc or first comment
	if f.Doc != nil {
		input.PackageDoc = strings.TrimSpace(f.Doc.Text())
	} else if len(f.Comments) > 0 && f.Comments[0] != nil {
		input.FirstComment = strings.TrimSpace(f.Comments[0].Text())
	}

	// Extract top exported symbols (functions, types, vars, consts)
	var symbols []string
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name != nil && ast.IsExported(d.Name.Name) {
				symbols = append(symbols, d.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name != nil && ast.IsExported(s.Name.Name) {
						symbols = append(symbols, s.Name.Name)
					}
				case *ast.ValueSpec:
					for _, name := range s.Names {
						if ast.IsExported(name.Name) {
							symbols = append(symbols, name.Name)
						}
					}
				}
			}
		}
		// Limit to top 10 symbols
		if len(symbols) >= 10 {
			break
		}
	}
	input.TopSymbols = symbols

	return symbol.NormalizeFileSummaryInput(input)
}

// enrichEntriesWithSummaries fetches or generates summaries for file entries.
// Uses cached summaries when available, generates new ones up to maxNew limit.
// Returns the number of summaries that were generated (not cached).
func enrichEntriesWithSummaries(
	ctx context.Context,
	cfg config.Config,
	workspace string,
	entries []retrieval.FileEntry,
	maxNew int,
	embedProvider semantic.EmbeddingProvider,
	logger zerolog.Logger,
) (int, error) {
	if len(entries) == 0 || maxNew <= 0 {
		return 0, nil
	}

	// Open memory store
	memStore, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return 0, fmt.Errorf("open memory store: %w", err)
	}
	defer memStore.Close()

	// Create LLM client for file summaries (Devstral via OpenRouter)
	llmClient := newSummaryLLM(logger)
	gen := retrieval.NewFileSummaryGenerator(memStore, llmClient, embedProvider, workspace)

	// Collect file paths that need summaries
	var paths []string
	pathToIdx := make(map[string]int)
	for i, entry := range entries {
		if entry.Summary == "" {
			paths = append(paths, entry.Path)
			pathToIdx[entry.Path] = i
		}
	}

	if len(paths) == 0 {
		return 0, nil
	}

	// Try to get existing summaries first
	existingSummaries, err := gen.GetSummaries(ctx, paths)
	if err != nil {
		logger.Debug().Err(err).Msg("failed to get existing summaries")
	}

	// Apply cached summaries
	for path, summary := range existingSummaries {
		if idx, ok := pathToIdx[path]; ok {
			entries[idx].Summary = summary
			delete(pathToIdx, path) // Remove from paths needing generation
		}
	}

	// Count how many still need generation
	needsGeneration := len(pathToIdx)
	if needsGeneration == 0 {
		return 0, nil
	}

	// Cap at maxNew
	toGenerate := needsGeneration
	if toGenerate > maxNew {
		toGenerate = maxNew
	}

	// Build inputs for generation with file metadata
	inputs := make([]symbol.FileSummaryInput, 0, toGenerate)
	generated := 0
	for path := range pathToIdx {
		if generated >= toGenerate {
			break
		}
		// Extract package, symbols, and doc comments for better LLM prompts
		input := extractGoFileMetadata(workspace, path)
		inputs = append(inputs, input)
		generated++
	}

	// Generate summaries (uses Devstral via OpenRouter, falls back to deterministic)
	created, err := gen.BatchCreateSummaries(ctx, inputs, toGenerate)
	if err != nil {
		logger.Debug().Err(err).Msg("batch summary generation error")
	}

	// Fetch newly created summaries
	if created > 0 {
		var newPaths []string
		for _, input := range inputs {
			newPaths = append(newPaths, input.FilePath)
		}
		newSummaries, err := gen.GetSummaries(ctx, newPaths)
		if err == nil {
			for path, summary := range newSummaries {
				if idx, ok := pathToIdx[path]; ok {
					entries[idx].Summary = summary
				}
			}
		}
	}

	// Backfill embeddings for existing file summaries missing vectors.
	if embedProvider != nil && maxNew > 0 {
		backfilled, err := gen.BackfillEmbeddings(ctx, maxNew)
		if err != nil {
			logger.Debug().Err(err).Msg("summary embedding backfill error")
		} else if backfilled > 0 {
			logger.Debug().Int("backfilled", backfilled).Msg("summary embedding backfill complete")
		}
	}

	return created, nil
}

// generateRootSummary creates a summary for the tree root based on top file summaries.
// Uses the FileSummaryGenerator's root summary generation (deterministic fallback).
func generateRootSummary(
	ctx context.Context,
	cfg config.Config,
	workspace string,
	entries []retrieval.FileEntry,
	embedProvider semantic.EmbeddingProvider,
	logger zerolog.Logger,
) string {
	if len(entries) == 0 {
		return ""
	}

	// Collect top file summaries (already sorted by score)
	var topSummaries []string
	for _, entry := range entries {
		if entry.Summary != "" {
			topSummaries = append(topSummaries, entry.Summary)
			if len(topSummaries) >= 5 { // Use top 5 summaries
				break
			}
		}
	}

	if len(topSummaries) == 0 {
		return ""
	}

	// Open memory store for FileSummaryGenerator
	memStore, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		logger.Debug().Err(err).Msg("failed to open memory store for root summary")
		return ""
	}
	defer memStore.Close()

	// Create generator with LLM client for root summary
	llmClient := newSummaryLLM(logger)
	gen := retrieval.NewFileSummaryGenerator(memStore, llmClient, embedProvider, workspace)

	// Generate root summary
	rootSummary, err := gen.GenerateRootSummary(ctx, topSummaries)
	if err != nil {
		logger.Debug().Err(err).Msg("failed to generate root summary")
		return ""
	}

	return rootSummary
}

// fetchTimelines retrieves timeline data for the top N session results.
func fetchTimelines(ctx context.Context, cfg config.Config, sessionResults []Result, limit int, types []string, workspace string) []SessionTimeline {
	if len(sessionResults) == 0 {
		return nil
	}

	// Limit sessions to process
	if len(sessionResults) > limit {
		sessionResults = sessionResults[:limit]
	}

	storeCtx := context.Background()

	// Open session store for chunk summaries only
	sessionStore, cleanup, err := sessionkit.OpenSessions(storeCtx, cfg)
	if err != nil {
		return nil
	}
	defer cleanup()

	// Open memory store for learnings
	memStore, memCleanup, err := sessionkit.OpenMemory(storeCtx, cfg)
	if err != nil {
		memStore = nil
	}
	if memStore != nil {
		defer memCleanup()
	}

	// Open a SEPARATE db connection for context windows queries
	// This bypasses the sessionStore entirely to test if the issue is there
	dbPath := filepath.Join(cfg.Storage.Root, "sessions.db")
	windowsDB, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return []SessionTimeline{{
			SessionID: "debug",
			Status:    "error",
			Message:   fmt.Sprintf("open windows db: %v", err),
		}}
	}
	defer windowsDB.Close()

	timelines := make([]SessionTimeline, 0, len(sessionResults))

	for _, result := range sessionResults {
		// Strip normalized prefix if present
		sessionID := strings.TrimPrefix(result.ID, "session:")

		timeline := SessionTimeline{
			SessionID:   sessionID,
			SessionName: result.Name,
			Similarity:  result.Similarity,
			Status:      "ok",
		}

		// Fetch context windows using SEPARATE db connection
		windows, err := queryContextWindowsDirect(storeCtx, windowsDB, sessionID)
		if err != nil {
			timeline.Status = "error"
			timeline.Message = fmt.Sprintf("get context windows: %v", err)
			timelines = append(timelines, timeline)
			continue
		}

		// Check if session has context windows (required for chunk summaries)
		if len(windows) == 0 {
			timeline.Status = "no_windows"
			timeline.Message = "session has no context windows"
			timelines = append(timelines, timeline)
			continue
		}

		// Collect chunk summaries from all windows
		chunks := collectChunkSummaries(storeCtx, sessionStore, sessionID, windows)
		timeline.ChunkSummaries = chunks

		// Fetch learnings from memory store
		learnings := fetchTimelineLearnings(storeCtx, memStore, sessionID, workspace, types)
		timeline.Learnings = learnings

		// Build rollup
		timeline.Rollup = buildTimelineRollup(chunks, learnings)

		timelines = append(timelines, timeline)
	}

	return timelines
}

// queryContextWindowsDirect queries context windows using a direct db connection.
func queryContextWindowsDirect(ctx context.Context, db *sql.DB, sessionID string) ([]storage.ContextWindow, error) {
	rows, err := db.QueryContext(ctx, `
SELECT id, session_id, window_index, started_at, ended_at, pre_compact_tokens,
       trigger, chunk_start, chunk_end, message_count, summary, embedding, embedding_model, created_at
FROM session_context_windows
WHERE session_id = ?
ORDER BY window_index ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query context windows: %w", err)
	}
	defer rows.Close()

	var windows []storage.ContextWindow
	for rows.Next() {
		var w storage.ContextWindow
		var embedding []byte
		var startedAt, endedAt, createdAt string
		err := rows.Scan(
			&w.ID, &w.SessionID, &w.WindowIndex, &startedAt, &endedAt,
			&w.PreCompactTokens, &w.Trigger, &w.ChunkStart, &w.ChunkEnd,
			&w.MessageCount, &w.Summary, &embedding, &w.EmbeddingModel, &createdAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan context window: %w", err)
		}
		// Parse timestamps
		if startedAt != "" {
			w.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAt)
		}
		if endedAt != "" {
			w.EndedAt, _ = time.Parse(time.RFC3339Nano, endedAt)
		}
		if createdAt != "" {
			w.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		}
		windows = append(windows, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return windows, nil
}

// collectChunkSummaries extracts chunk summaries from session context windows.
func collectChunkSummaries(ctx context.Context, sessionStore *sessions.Store, sessionID string, windows []storage.ContextWindow) []TimelineChunk {
	var chunks []TimelineChunk

	for _, window := range windows {
		// Fetch chunk summaries for this window
		summaries, err := sessionStore.GetChunkSummaries(ctx, sessionID, window.WindowIndex)
		if err != nil {
			continue
		}

		for _, cs := range summaries {
			chunk := TimelineChunk{
				SummaryID:     cs.ID,
				WindowIndex:   window.WindowIndex,
				ChunkIndexMin: cs.ChunkIndexMin,
				ChunkIndexMax: cs.ChunkIndexMax,
				Summary:       cs.Summary,
				Tools:         cs.Tools,
				Files:         cs.Files,
				Errors:        cs.Errors,
			}
			chunks = append(chunks, chunk)
		}
	}

	return chunks
}

// fetchTimelineLearnings retrieves learnings for a session from the memory store.
func fetchTimelineLearnings(ctx context.Context, store *memory.Store, sessionID, workspace string, types []string) []TimelineLearning {
	// Default learning types
	if len(types) == 0 {
		types = []string{"decision", "gotcha", "preference", "anti_pattern", "learning"}
	}

	var learnings []TimelineLearning

	for _, memType := range types {
		entries, err := store.ListByType(ctx, memType, workspace, 50)
		if err != nil {
			continue
		}

		for _, scored := range entries {
			entry := scored.Entry

			// Filter by session ID if stored
			if entry.SessionID != "" && entry.SessionID != sessionID {
				continue
			}

			learning := TimelineLearning{
				Type:    memType,
				Summary: entry.Summary,
			}

			// Try to parse Result as JSON to get window_index
			if len(entry.Result) > 0 {
				var payload map[string]any
				if json.Unmarshal(entry.Result, &payload) == nil {
					if wi, ok := payload["window_index"].(float64); ok {
						learning.WindowIndex = int(wi)
					}
				}
			}

			learnings = append(learnings, learning)
		}
	}

	return learnings
}

// buildTimelineRollup aggregates timeline metadata from chunks and learnings.
func buildTimelineRollup(chunks []TimelineChunk, learnings []TimelineLearning) *TimelineRollup {
	if len(chunks) == 0 && len(learnings) == 0 {
		return nil
	}

	rollup := &TimelineRollup{}

	// Dedupe helpers
	toolSet := make(map[string]bool)
	fileSet := make(map[string]bool)
	errorSet := make(map[string]bool)

	for _, chunk := range chunks {
		rollup.SummaryLines = append(rollup.SummaryLines, chunk.Summary)
		for _, t := range chunk.Tools {
			if !toolSet[t] {
				toolSet[t] = true
				rollup.Tools = append(rollup.Tools, t)
			}
		}
		for _, f := range chunk.Files {
			if !fileSet[f] {
				fileSet[f] = true
				rollup.Files = append(rollup.Files, f)
			}
		}
		for _, e := range chunk.Errors {
			if !errorSet[e] {
				errorSet[e] = true
				rollup.Errors = append(rollup.Errors, e)
			}
		}
	}

	for _, learning := range learnings {
		switch learning.Type {
		case "decision":
			rollup.Decisions = append(rollup.Decisions, learning.Summary)
		case "gotcha":
			rollup.Gotchas = append(rollup.Gotchas, learning.Summary)
		}
	}

	return rollup
}
