// Package main implements the code/semantic_search skill.
// It performs unified semantic search across code symbols, sessions, and memories,
// combining results with Reciprocal Rank Fusion and extracting context hints.
//
// Phase 3: Uses internal/retrieval infrastructure for symbol search with BM25 fallback.
// See docs/designs/unified_semantic_search.md and docs/designs/semantic_search_phase3_plan.md.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/domain/policy"
	"github.com/jkatigb/agentctl/internal/indexing/rerank"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/retrieval"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
	"github.com/jkatigb/agentctl/internal/storage/graph"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
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
	DefaultSourceTimeout = 3 * time.Second // Per-source timeout (includes embedding generation)
	DefaultTotalTimeout  = 5 * time.Second // Total search timeout
)

// Supported scopes.
const (
	ScopeSymbols  = "symbols"
	ScopeSessions = "sessions"
	ScopeMemories = "memories"
	ScopeTasks    = "tasks"
	ScopeCodemaps = "codemaps"
)

// Input is the expected JSON input.
type Input struct {
	Query          string   `json:"query" validate:"required"`
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
}

// Output is the JSON output.
type Output struct {
	Query        string            `json:"query"`
	Results      []Result          `json:"results"`
	ContextHints []ContextHint     `json:"context_hints,omitempty"`
	Stats        SearchStats       `json:"stats"`
	Summary      *SynthesisSummary `json:"summary,omitempty"` // Present when summarize=true
	TreeText     string            `json:"tree_text,omitempty"` // Present when format=tree
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

// Result represents a single search result.
type Result struct {
	Source      string  `json:"source"`                 // "symbol", "session", "memory"
	ID          string  `json:"id"`                     // Unique identifier (normalized)
	Name        string  `json:"name"`                   // Display name
	Path        string  `json:"path,omitempty"`         // File path (for symbols)
	Line        int     `json:"line,omitempty"`         // Line number (for symbols)
	Snippet     string  `json:"snippet,omitempty"`      // Code snippet (for symbols)
	Summary     string  `json:"summary,omitempty"`      // Summary text (for sessions/memories)
	Similarity  float64 `json:"similarity"`             // Similarity score (0-1)
	Rank        int     `json:"rank"`                   // Final rank after fusion
	RRFScore    float64 `json:"rrf_score,omitempty"`    // RRF score used for ranking
	PageRank    float64 `json:"pagerank,omitempty"`     // PageRank authority score (0-1 normalized)
	FinalScore  float64 `json:"final_score,omitempty"`  // Combined score with PageRank boost
	RerankScore float64 `json:"rerank_score,omitempty"` // Reranker relevance score (0-1)
	SourceRank  int     `json:"source_rank,omitempty"`  // Rank within source
}

// ContextHint represents a hint from related sessions.
type ContextHint struct {
	Type      string   `json:"type"`                // "past_solution", "gotcha", "decision"
	SessionID string   `json:"session_id"`          // Source session ID
	Summary   string   `json:"summary"`             // Brief summary
	Items     []string `json:"items,omitempty"`     // Related items (gotchas, decisions)
	KeyFiles  []string `json:"key_files,omitempty"` // Related files
}

// SearchStats contains search statistics.
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

func main() {
	skillmain.Main(Command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply defaults
	if len(in.Scope) == 0 {
		in.Scope = []string{ScopeSymbols, ScopeSessions, ScopeMemories, ScopeTasks, ScopeCodemaps}
	}
	if in.Limit <= 0 {
		in.Limit = DefaultLimit
	}
	if in.MinSimilarity <= 0 {
		in.MinSimilarity = DefaultMinSimilarity
	}
	if in.Workspace == "" {
		in.Workspace = rc.PathValidator.Workspace()
	}
	if in.IncludeContext == nil {
		defaultTrue := true
		in.IncludeContext = &defaultTrue
	}

	// Validate scope values
	validScopes := map[string]bool{ScopeSymbols: true, ScopeSessions: true, ScopeMemories: true, ScopeTasks: true, ScopeCodemaps: true}
	for _, s := range in.Scope {
		if !validScopes[s] {
			return fmt.Errorf("invalid scope: %s (valid: symbols, sessions, memories, tasks, codemaps)", s)
		}
	}

	out, err := search(ctx, rc.Config, &in)
	if err != nil {
		return err
	}

	return skillout.Emit(rc, Command, out)
}

func search(ctx context.Context, cfg config.Config, in *Input) (*Output, error) {
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
	agentctlHome := os.Getenv("AGENTCTL_HOME")
	if agentctlHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("determine home directory: %w", err)
		}
		agentctlHome = filepath.Join(home, ".agentctl")
	}
	storageRoot := filepath.Join(agentctlHome, "storage")
	casRoot := filepath.Join(agentctlHome, "cas")

	// Workspace is already resolved in parseInput (prefers AGENTCTL_WORKSPACE over cwd)
	workspacePath := in.Workspace
	// Canonicalize the workspace path
	if absPath, err := filepath.Abs(workspacePath); err == nil {
		workspacePath = absPath
	}

	// Create PathValidator to ensure workspace is valid (for path validation, not workspace ID)
	validator, err := policy.NewPathValidator(workspacePath, []string{agentctlHome})
	if err != nil {
		return nil, fmt.Errorf("invalid workspace path: %w", err)
	}
	_ = validator // PathValidator available for path validation if needed

	// Use actual workspace path for storage scoping
	// This ensures consistency with code/incremental_index which indexes under the actual path
	workspaceID := workspacePath

	// Build scope set first (needed for scoped embeddings)
	scopeSet := make(map[string]bool)
	for _, s := range in.Scope {
		scopeSet[s] = true
	}

	// Detect provider and get scope-specific model configuration
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")

	var providerName string
	if voyageKey != "" {
		providerName = "voyage"
	} else if geminiKey != "" {
		providerName = "gemini"
	}

	// Generate scope-specific query embeddings in parallel
	// Model selection is configurable via EMBEDDING_MODEL_CODE and EMBEDDING_MODEL_TEXT
	var scopedEmb scopedEmbeddings
	var embedProvider semantic.EmbeddingProvider // Keep for backward compat with some functions
	var codeModel, textModel string

	if providerName != "" {
		codeModel, textModel = embeddingModelConfig(providerName)

		start := time.Now()
		var embErr error
		scopedEmb, embErr = generateScopedEmbeddings(searchCtx, in.Query, scopeSet, codeModel, textModel)
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
				embedProvider, _ = createProviderWithModel(codeModel)
			}
		}
	} else {
		out.Stats.Hint = "no embedding API key set; set VOYAGE_API_KEY or GEMINI_API_KEY for vector search"
	}

	// Use appropriate embedding per scope
	queryEmbedding := scopedEmb.code // For symbols scope
	textEmbedding := scopedEmb.text  // For memory, tasks, sessions, codemaps scopes

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
				storageRoot,
				casRoot,
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

			results, err := searchSessions(sourceCtx, cfg, storageRoot, textEmbedding, in.Limit*2, in)
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

			// Memory search requires embeddings for vector similarity (uses text model)
			if textEmbedding == nil {
				resultsCh <- sourceResults{
					source:  ScopeMemories,
					results: nil,
					err:     nil,
					latency: time.Since(start),
					hint:    "memory search requires embeddings; set VOYAGE_API_KEY or GEMINI_API_KEY",
				}
				return
			}

			results, hint, err := searchMemories(sourceCtx, cfg, storageRoot, casRoot, workspacePath, in.Query, textEmbedding, in.Limit*2, in)
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

			results, err := searchTasks(sourceCtx, cfg, storageRoot, casRoot, workspacePath, textEmbedding, in.Limit*2)
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

			results, err := searchCodemaps(sourceCtx, storageRoot, casRoot, workspacePath, textEmbedding, in.Limit*2)
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
	fusedResults = applyPageRankBoost(searchCtx, cfg, workspacePath, fusedResults)

	// Apply reranking if enabled
	fusedResults, rerankStats := applyReranking(searchCtx, *in, fusedResults)
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
		out.TreeText = renderResultsAsTree(fusedResults)
	}

	return out, nil
}

type sourceResults struct {
	source  string
	results []Result
	err     error
	latency time.Duration
	hint    string // Optional hint when source unavailable but not an error
}

// scopedEmbeddings holds query embeddings for different scope groups.
// Code scopes may use a different model than text scopes.
type scopedEmbeddings struct {
	code []float32 // For symbols scope
	text []float32 // For memory, tasks, sessions, codemaps
}

// embeddingModelConfig returns the models to use for code and text scopes.
// Configuration priority: env vars > defaults per provider.
//
// Environment variables:
//   - EMBEDDING_MODEL_CODE: Model for symbols scope (default: voyage-code-3 for Voyage)
//   - EMBEDDING_MODEL_TEXT: Model for text scopes (default: voyage-3.5 for Voyage)
//   - EMBEDDING_MODEL: Fallback for both if scope-specific not set
//
// For Gemini, all scopes use the same model (no code-specific model available).
func embeddingModelConfig(providerName string) (codeModel, textModel string) {
	// Check for scope-specific overrides first
	codeModel = os.Getenv("EMBEDDING_MODEL_CODE")
	textModel = os.Getenv("EMBEDDING_MODEL_TEXT")

	// Fall back to generic EMBEDDING_MODEL
	fallback := os.Getenv("EMBEDDING_MODEL")

	// Apply provider-specific defaults
	switch providerName {
	case "voyage":
		if codeModel == "" {
			if fallback != "" && !strings.HasPrefix(fallback, "gemini-") {
				codeModel = fallback
			} else {
				codeModel = "voyage-code-3" // Best for code retrieval
			}
		}
		if textModel == "" {
			if fallback != "" && !strings.HasPrefix(fallback, "gemini-") {
				textModel = fallback
			} else {
				textModel = "voyage-3.5" // Best price/performance for text
			}
		}
	case "gemini":
		// Gemini uses same model for all scopes
		model := "gemini-embedding-001"
		if fallback != "" && strings.HasPrefix(fallback, "gemini-") {
			model = fallback
		}
		if codeModel == "" {
			codeModel = model
		}
		if textModel == "" {
			textModel = model
		}
	}

	return codeModel, textModel
}

// createProviderWithModel creates an embedding provider with a specific model.
// Supports both Voyage and Gemini based on available API keys.
func createProviderWithModel(model string) (semantic.EmbeddingProvider, error) {
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")

	// Determine provider from model name or API key availability
	if strings.HasPrefix(model, "voyage-") {
		if voyageKey == "" {
			return nil, fmt.Errorf("VOYAGE_API_KEY not set for model %s", model)
		}
		return semantic.NewVoyageProvider(semantic.VoyageConfig{
			APIKey: voyageKey,
			Model:  model,
		})
	} else if strings.HasPrefix(model, "gemini-") {
		if geminiKey == "" {
			return nil, fmt.Errorf("GEMINI_API_KEY not set for model %s", model)
		}
		return semantic.NewGeminiProvider(semantic.GeminiConfig{
			APIKey: geminiKey,
			Model:  model,
		})
	}

	// Auto-detect provider from API keys with scope-based model selection
	if voyageKey != "" {
		// Use scope-based default when model is empty
		if model == "" {
			model, _ = semantic.ScopeModelRecommendation(semantic.ScopeSymbols)
		}
		return semantic.NewVoyageProvider(semantic.VoyageConfig{
			APIKey: voyageKey,
			Model:  model,
		})
	} else if geminiKey != "" {
		// Gemini fallback (not recommended - dimension mismatch with Voyage)
		if model == "" {
			model = "gemini-embedding-001"
		}
		return semantic.NewGeminiProvider(semantic.GeminiConfig{
			APIKey: geminiKey,
			Model:  model,
		})
	}

	return nil, fmt.Errorf("no embedding API key available")
}

// generateScopedEmbeddings creates query embeddings for the requested scopes.
// Only generates embeddings for scope groups that are actually requested.
// Uses scope-specific models: code model for symbols, text model for others.
func generateScopedEmbeddings(ctx context.Context, query string, scopeSet map[string]bool, codeModel, textModel string) (scopedEmbeddings, error) {
	var emb scopedEmbeddings
	var wg sync.WaitGroup
	var codeErr, textErr error

	// Check if code scope is requested
	needsCode := scopeSet[ScopeSymbols]
	// Check if any text scope is requested
	needsText := scopeSet[ScopeSessions] || scopeSet[ScopeMemories] || scopeSet[ScopeTasks] || scopeSet[ScopeCodemaps]

	// Optimization: if both models are the same, only generate one embedding
	if needsCode && needsText && codeModel == textModel {
		provider, err := createProviderWithModel(codeModel)
		if err != nil {
			return emb, err
		}
		embedding, err := provider.Embed(ctx, query)
		if err != nil {
			return emb, err
		}
		emb.code = embedding
		emb.text = embedding
		return emb, nil
	}

	// Generate code embedding if needed
	if needsCode {
		wg.Add(1)
		go func() {
			defer wg.Done()
			provider, err := createProviderWithModel(codeModel)
			if err != nil {
				codeErr = err
				return
			}
			emb.code, codeErr = provider.Embed(ctx, query)
		}()
	}

	// Generate text embedding if needed
	if needsText {
		wg.Add(1)
		go func() {
			defer wg.Done()
			provider, err := createProviderWithModel(textModel)
			if err != nil {
				textErr = err
				return
			}
			emb.text, textErr = provider.Embed(ctx, query)
		}()
	}

	wg.Wait()

	// Return first error encountered (if any)
	if codeErr != nil && textErr != nil {
		return emb, fmt.Errorf("code: %v; text: %v", codeErr, textErr)
	}
	if codeErr != nil {
		return emb, codeErr
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
	storageRoot, casRoot, workspaceID, workspacePath, query string,
	embedProvider semantic.EmbeddingProvider,
	queryEmbedding []float32,
	limit int,
) ([]Result, error) {
	// Open memory store for symbol index access
	memStore, err := memory.Open(ctx, storageRoot, casRoot)
	if err != nil {
		return nil, fmt.Errorf("open memory store: %w", err)
	}
	defer memStore.Close()

	// Create logger (silent for skill context)
	logger := zerolog.Nop()

	// Create generator with embedding provider
	// The Generator handles hybrid search internally (BM25 + semantic when embedProvider is set)
	gen := retrieval.NewGenerator(memStore, embedProvider, workspacePath, logger)

	// Wire up SearchableStore for vector search when embeddings are available
	if embedProvider != nil && queryEmbedding != nil {
		wrappedDB := dbdriver.WrapSQLDB(memStore.DB(), dbdriver.DriverSQLite)
		searchStore, searchErr := memStore.EnableSearch(wrappedDB, workspaceID)
		if searchErr == nil {
			gen = gen.WithSearchableStore(searchStore)
			logger.Debug().Msg("enabled SearchableStore for hybrid search")
		}
		// If EnableSearch fails, we fall back to BM25 (no error returned)
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
		return nil, fmt.Errorf("generate candidates: %w", err)
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
	SearchSimilar(ctx context.Context, embedding []float32, limit int) ([]storage.SimilarSession, error)
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
func searchSessions(ctx context.Context, cfg config.Config, storageRoot string, queryEmbedding []float32, limit int, in *Input) ([]Result, error) {
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
				return nil, fmt.Errorf("open turso sessions store (remote mode): %w", err)
			}
			// Fallback to local store if Turso fails
			store, err = sessions.Open(ctx, storageRoot)
			if err != nil {
				return nil, fmt.Errorf("open sessions store (turso fallback): %w", err)
			}
		}
	} else {
		// Use local SQLite store
		store, err = sessions.Open(ctx, storageRoot)
		if err != nil {
			return nil, fmt.Errorf("open sessions store: %w", err)
		}
	}
	defer func() { errs.Ignore(store.Close(), "close session store") }()

	// Use appropriate search method based on remote options
	var similar []storage.SimilarSession

	if in.Remote && (in.Global || len(in.Workspaces) > 0) {
		// Check if store supports global search
		globalStore, ok := store.(globalSessionSearcher)
		if !ok {
			return nil, fmt.Errorf("session store does not support cross-workspace search; Turso required")
		}

		if in.Global {
			similar, err = globalStore.SearchSimilarGlobal(ctx, queryEmbedding, limit)
		} else {
			similar, err = globalStore.SearchSimilarMultiWorkspace(ctx, in.Workspaces, queryEmbedding, limit)
		}
	} else {
		similar, err = store.SearchSimilar(ctx, queryEmbedding, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("search sessions: %w", err)
	}

	results := make([]Result, 0, len(similar))
	for i, s := range similar {
		result := Result{
			Source:     "session",
			ID:         normalizeSessionID(s.Session.ID),
			Name:       getSessionName(s.Session),
			Summary:    truncate(s.Session.Summary, 200),
			Similarity: s.Similarity,
			SourceRank: i + 1,
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
	cfg config.Config,
	storageRoot, casRoot, workspaceID, query string,
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
				return nil, "", fmt.Errorf("open turso memory store (remote mode): %w", err)
			}
			// Fallback to BM25 if Turso fails, with hint about the failure
			hint := fmt.Sprintf("memory vector search unavailable: %v; using BM25 fallback", err)
			results, bm25Err := searchMemoriesBM25(ctx, storageRoot, casRoot, workspaceID, query, limit)
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
				return nil, "", fmt.Errorf("memory remote search failed: %w", err)
			}
			// Fallback to BM25 on error, with hint about the failure
			hint := fmt.Sprintf("memory vector search failed: %v; using BM25 fallback", err)
			results, bm25Err := searchMemoriesBM25(ctx, storageRoot, casRoot, workspaceID, query, limit)
			return results, hint, bm25Err
		}

		// Fallback to BM25 if vector search returns empty (may indicate missing vectors)
		// Skip fallback for remote mode - empty results are valid
		if len(scoredEntries) == 0 && !in.Remote {
			hint := "memory vector search returned no results; trying BM25 fallback"
			results, bm25Err := searchMemoriesBM25(ctx, storageRoot, casRoot, workspaceID, query, limit)
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
			results, err := searchMemoriesVector(ctx, storageRoot, casRoot, workspaceID, queryEmbedding, limit)
			if err == nil && len(results) > 0 {
				return results, "", nil
			}
			// Fall back to BM25 if vector search fails or returns empty
			if err != nil {
				hint := fmt.Sprintf("memory vector search failed: %v; using BM25 fallback", err)
				results, bm25Err := searchMemoriesBM25(ctx, storageRoot, casRoot, workspaceID, query, limit)
				return results, hint, bm25Err
			}
		}
		// Use SQLite BM25 search (no hint needed - this is expected behavior)
		results, err := searchMemoriesBM25(ctx, storageRoot, casRoot, workspaceID, query, limit)
		return results, "", err
	}

	// Convert scored entries to results
	results := make([]Result, 0, len(scoredEntries))
	rank := 1
	for _, scored := range scoredEntries {
		entry := scored.Entry
		// Skip symbol entries - they're handled separately
		if entry.Type == "symbol" {
			continue
		}

		result := Result{
			Source:     "memory",
			ID:         normalizeMemoryID(entry.Name),
			Name:       entry.Name,
			Summary:    truncate(entry.Summary, 200),
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
	storageRoot, casRoot, workspaceID, query string,
	limit int,
) ([]Result, error) {
	// Open memory store
	memStore, err := memory.Open(ctx, storageRoot, casRoot)
	if err != nil {
		return nil, fmt.Errorf("open memory store: %w", err)
	}
	defer memStore.Close()

	// Use basic text search on memories (BM25-like)
	scoredEntries, err := memStore.Search(ctx, workspaceID, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}

	// Filter out symbol-type entries (they're handled by symbol search)
	results := make([]Result, 0, len(scoredEntries))
	rank := 1
	for _, scored := range scoredEntries {
		entry := scored.Entry
		// Skip symbol entries - they're handled separately
		if entry.Type == "symbol" {
			continue
		}

		result := Result{
			Source:     "memory",
			ID:         normalizeMemoryID(entry.Name),
			Name:       entry.Name,
			Summary:    truncate(entry.Summary, 200),
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

// searchMemoriesVector uses SQLite in-memory cosine similarity search.
func searchMemoriesVector(
	ctx context.Context,
	storageRoot, casRoot, workspaceID string,
	queryEmbedding []float32,
	limit int,
) ([]Result, error) {
	// Open memory store
	memStore, err := memory.Open(ctx, storageRoot, casRoot)
	if err != nil {
		return nil, fmt.Errorf("open memory store: %w", err)
	}
	defer memStore.Close()

	// Use vector similarity search
	scoredEntries, err := memStore.SearchSimilar(ctx, workspaceID, queryEmbedding, limit)
	if err != nil {
		return nil, fmt.Errorf("vector search memories: %w", err)
	}

	// Filter out symbol-type entries (they're handled by symbol search)
	results := make([]Result, 0, len(scoredEntries))
	rank := 1
	for _, scored := range scoredEntries {
		entry := scored.Entry
		// Skip symbol entries - they're handled separately
		if entry.Type == "symbol" {
			continue
		}

		result := Result{
			Source:     "memory",
			ID:         normalizeMemoryID(entry.Name),
			Name:       entry.Name,
			Summary:    truncate(entry.Summary, 200),
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

func normalizeSymbolID(workspaceID string, candidate retrieval.Candidate) string {
	// Format: symbol:<workspace>:<path>#L<line>
	id := fmt.Sprintf("symbol:%s:%s", workspaceID, candidate.Path)
	if candidate.Line > 0 {
		id = fmt.Sprintf("%s#L%d", id, candidate.Line)
	}
	return id
}

func normalizeSessionID(sessionID string) string {
	// Format: session:<id>
	return fmt.Sprintf("session:%s", sessionID)
}

func normalizeMemoryID(name string) string {
	// Format: memory:<name>
	return fmt.Sprintf("memory:%s", name)
}

func normalizeTaskID(taskID string) string {
	// Format: task:<id>
	return fmt.Sprintf("task:%s", taskID)
}

func normalizeCodemapID(codemapID string) string {
	// Format: codemap:<id>
	return fmt.Sprintf("codemap:%s", codemapID)
}

// searchTasks searches task embeddings using vector similarity.
func searchTasks(
	ctx context.Context,
	cfg config.Config,
	storageRoot, casRoot, workspaceID string,
	queryEmbedding []float32,
	limit int,
) ([]Result, error) {
	// Open memory store (task embeddings are stored in named_memory)
	memStore, err := memory.Open(ctx, storageRoot, casRoot)
	if err != nil {
		return nil, fmt.Errorf("open memory store: %w", err)
	}
	defer memStore.Close()

	// Search for similar entries
	scoredEntries, err := memStore.SearchSimilar(ctx, workspaceID, queryEmbedding, limit*2)
	if err != nil {
		return nil, fmt.Errorf("vector search tasks: %w", err)
	}

	// Debug: if no entries found, report workspace for investigation
	if len(scoredEntries) == 0 {
		return nil, fmt.Errorf("no embeddings found for workspace=%q (expected task embeddings)", workspaceID)
	}

	// Open tasks store to get full task details
	// Note: tasks.Open expects root directory, it appends "tasks.db" internally
	taskStore, err := tasks.Open(ctx, storageRoot)
	if err != nil {
		return nil, fmt.Errorf("open tasks store: %w", err)
	}
	defer taskStore.Close()

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
			snippetParts = append(snippetParts, "Notes: "+truncate(task.Notes, 100))
		}
		if task.Gotchas != "" {
			snippetParts = append(snippetParts, "Gotchas: "+truncate(task.Gotchas, 100))
		}
		snippet := strings.Join(snippetParts, " | ")

		result := Result{
			Source:     "task",
			ID:         normalizeTaskID(taskID),
			Name:       task.Title,
			Summary:    truncate(task.Description, 200),
			Snippet:    truncate(snippet, 300),
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
	storageRoot, casRoot, workspaceID string,
	queryEmbedding []float32,
	limit int,
) ([]Result, error) {
	// Open memory store (codemap embeddings are stored in named_memory)
	memStore, err := memory.Open(ctx, storageRoot, casRoot)
	if err != nil {
		return nil, fmt.Errorf("open memory store: %w", err)
	}
	defer memStore.Close()

	// Search for similar entries
	scoredEntries, err := memStore.SearchSimilar(ctx, workspaceID, queryEmbedding, limit*2)
	if err != nil {
		return nil, fmt.Errorf("vector search codemaps: %w", err)
	}

	// Filter to only codemap entries
	results := make([]Result, 0, len(scoredEntries))
	rank := 1
	for _, scored := range scoredEntries {
		entry := scored.Entry

		// Only process codemap entries
		if entry.Type != "codemap" {
			continue
		}

		// Extract codemap ID from name: "codemap://<id>" or just use name
		codemapID := entry.Name
		if strings.HasPrefix(entry.Name, "codemap://") {
			codemapID = strings.TrimPrefix(entry.Name, "codemap://")
		}

		// Build snippet from codemap data
		snippet := entry.Summary
		if snippet == "" && entry.Result != nil {
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

		result := Result{
			Source:     "codemap",
			ID:         normalizeCodemapID(codemapID),
			Name:       entry.Name,
			Summary:    truncate(entry.Summary, 200),
			Snippet:    truncate(snippet, 300),
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
func applyPageRankBoost(ctx context.Context, cfg config.Config, workspacePath string, results []Result) []Result {
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
		Workspace: workspacePath,
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
func applyReranking(ctx context.Context, in Input, results []Result) ([]Result, rerankStatsResult) {
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
		// Use snippet for symbols, summary for sessions/memories
		content := r.Snippet
		if content == "" {
			content = r.Summary
		}
		if content == "" {
			content = r.Name
		}
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
			_, _ = fmt.Fprintf(os.Stderr, "rerank: missing index in metadata for id=%s, skipping\n", rr.ID)
			continue
		}
		idx, ok := val.(int)
		if !ok {
			_, _ = fmt.Fprintf(os.Stderr, "rerank: index is not int for id=%s (got %T), skipping\n", rr.ID, val)
			continue
		}
		if idx < 0 || idx >= len(results) {
			_, _ = fmt.Fprintf(os.Stderr, "rerank: index %d out of bounds (len=%d) for id=%s, skipping\n", idx, len(results), rr.ID)
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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}


// LLMProvider represents an LLM provider for synthesis.
type LLMProvider struct {
	Name     string
	Endpoint string
	APIKey   string
	Model    string
}

// getLLMProviders returns available LLM providers in priority order.
// Priority: OpenRouter (devstral free) → Groq → Cerebras
func getLLMProviders(modelOverride string) []LLMProvider {
	var providers []LLMProvider

	// OpenRouter - devstral is the preferred free model
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		model := modelOverride
		if model == "" {
			model = os.Getenv("OPENROUTER_MODELS")
		}
		if model == "" {
			model = "mistralai/devstral-2512:free"
		}
		providers = append(providers, LLMProvider{
			Name:     "openrouter:" + model,
			Endpoint: "https://openrouter.ai/api/v1/chat/completions",
			APIKey:   key,
			Model:    model,
		})
	}

	// Groq - fast and cheap
	if key := os.Getenv("GROQ_API_KEY"); key != "" {
		model := "llama-3.3-70b-versatile"
		if modelOverride != "" {
			model = modelOverride
		}
		providers = append(providers, LLMProvider{
			Name:     "groq",
			Endpoint: "https://api.groq.com/openai/v1/chat/completions",
			APIKey:   key,
			Model:    model,
		})
	}

	// Cerebras - fastest inference
	if key := os.Getenv("CEREBRAS_API_KEY"); key != "" {
		model := "llama-3.3-70b"
		if modelOverride != "" {
			model = modelOverride
		}
		providers = append(providers, LLMProvider{
			Name:     "cerebras",
			Endpoint: "https://api.cerebras.ai/v1/chat/completions",
			APIKey:   key,
			Model:    model,
		})
	}

	return providers
}

// synthesizeResults sends search results to an LLM for intelligent synthesis.
func synthesizeResults(ctx context.Context, query string, results []Result, modelOverride string) (*SynthesisSummary, error) {
	providers := getLLMProviders(modelOverride)
	if len(providers) == 0 {
		return nil, fmt.Errorf("no LLM provider available (set OPENROUTER_API_KEY, GROQ_API_KEY, or CEREBRAS_API_KEY)")
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

	return nil, fmt.Errorf("all LLM providers failed: %w", lastErr)
}

// callLLMProvider calls an OpenAI-compatible API endpoint.
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
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
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
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
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
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("empty response from %s", provider.Name)
	}

	// Parse the JSON response
	summary, err := parseSynthesisResponse(result.Choices[0].Message.Content)
	if err != nil {
		return nil, err
	}
	summary.TokensUsed = result.Usage.TotalTokens
	return summary, nil
}

// buildSynthesisPrompt builds the prompt for LLM synthesis.
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

// renderResultsAsTree builds a tree visualization from result file paths.
// Groups files by directory structure and renders with ASCII tree characters.
func renderResultsAsTree(results []Result) string {
	// Extract unique paths from results (only symbol results have paths)
	pathSet := make(map[string]bool)
	for _, r := range results {
		if r.Path != "" {
			pathSet[r.Path] = true
		}
	}

	if len(pathSet) == 0 {
		return "(no file paths in results)"
	}

	// Sort paths for consistent output
	paths := make([]string, 0, len(pathSet))
	for p := range pathSet {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	// Build tree structure: map of path components to children
	type treeNode struct {
		children map[string]*treeNode
		isFile   bool
	}

	root := &treeNode{children: make(map[string]*treeNode)}

	for _, path := range paths {
		parts := strings.Split(path, "/")
		current := root
		for i, part := range parts {
			if current.children[part] == nil {
				current.children[part] = &treeNode{
					children: make(map[string]*treeNode),
					isFile:   i == len(parts)-1,
				}
			}
			current = current.children[part]
		}
	}

	// Render tree recursively
	var sb strings.Builder
	sb.WriteString(".\n")

	var renderNode func(node *treeNode, prefix string)
	renderNode = func(node *treeNode, prefix string) {
		// Get sorted children
		names := make([]string, 0, len(node.children))
		for name := range node.children {
			names = append(names, name)
		}
		sort.Strings(names)

		for i, name := range names {
			child := node.children[name]
			isLast := i == len(names)-1

			connector := "├── "
			if isLast {
				connector = "└── "
			}

			suffix := ""
			if !child.isFile {
				suffix = "/"
			}

			sb.WriteString(prefix + connector + name + suffix + "\n")

			if !child.isFile {
				newPrefix := prefix
				if isLast {
					newPrefix += "    "
				} else {
					newPrefix += "│   "
				}
				renderNode(child, newPrefix)
			}
		}
	}

	renderNode(root, "")
	sb.WriteString(fmt.Sprintf("\n📂 %d related files", len(paths)))

	return sb.String()
}
