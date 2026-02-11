// Package main implements the session/recall skill for semantic session retrieval.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/mathutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

// Input defines the skill input parameters for semantic session retrieval with multiple granularity options.
type Input struct {
	Query             string  `json:"query" validate:"required"`
	Limit             int     `json:"limit,omitempty"`
	MinSimilarity     float64 `json:"min_similarity,omitempty"`
	Workspace         string  `json:"workspace,omitempty"`
	Project           string  `json:"project,omitempty"`
	WindowGranularity bool    `json:"window_granularity,omitempty"` // Search at context window level instead of session
	ChunkGranularity  bool    `json:"chunk_granularity,omitempty"`  // Search at chunk level (requires embeddings)
}

// Output defines the skill output with comprehensive search results and match statistics.
type Output struct {
	Query               string         `json:"query"`
	Matches             []SessionMatch `json:"matches,omitempty"`
	WindowMatches       []WindowMatch  `json:"window_matches,omitempty"` // Populated when window_granularity is true
	ChunkMatches        []ChunkMatch   `json:"chunk_matches,omitempty"`  // Populated when chunk_granularity is true
	TotalWithEmbeddings int            `json:"total_with_embeddings"`
	Status              string         `json:"status"`
	Message             string         `json:"message"`
}

// SessionMatch represents a matched session with similarity score and comprehensive session metadata.
type SessionMatch struct {
	SessionID    string   `json:"session_id"`
	ProjectName  string   `json:"project_name"`
	GitBranch    string   `json:"git_branch,omitempty"`
	Summary      string   `json:"summary"`
	Accomplished []string `json:"accomplished,omitempty"`
	Decisions    []string `json:"decisions,omitempty"`
	Gotchas      []string `json:"gotchas,omitempty"`
	UserInsights []string `json:"user_insights,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	KeyFiles     []string `json:"key_files,omitempty"`
	Similarity   float64  `json:"similarity"`
	StartedAt    string   `json:"started_at,omitempty"`
}

// WindowMatch represents a matched context window with similarity score and window metadata.
type WindowMatch struct {
	SessionID        string  `json:"session_id"`
	WindowIndex      int     `json:"window_index"`
	Trigger          string  `json:"trigger,omitempty"`
	PreCompactTokens int     `json:"pre_compact_tokens,omitempty"`
	Summary          string  `json:"summary,omitempty"`
	MessageCount     int     `json:"message_count"`
	StartedAt        string  `json:"started_at,omitempty"`
	EndedAt          string  `json:"ended_at,omitempty"`
	Similarity       float64 `json:"similarity"`
}

// ChunkMatch represents a matched chunk with similarity score and detailed chunk information.
type ChunkMatch struct {
	SessionID      string  `json:"session_id"`
	WindowIndex    int     `json:"window_index"`
	ChunkIndex     int     `json:"chunk_index"`
	ChunkType      string  `json:"chunk_type,omitempty"`
	ContentPreview string  `json:"content_preview,omitempty"`
	SummaryID      string  `json:"summary_id,omitempty"`
	Summary        string  `json:"summary,omitempty"`
	SummaryModel   string  `json:"summary_model,omitempty"`
	ChunkIndexMin  int     `json:"chunk_index_min,omitempty"`
	ChunkIndexMax  int     `json:"chunk_index_max,omitempty"`
	Similarity     float64 `json:"similarity"`
}

const (
	defaultLimit  = 5
	defaultMinSim = 0.3
)

// main is the skill entry point for session/recall with semantic search capabilities.
func main() {
	skillmain.Main("session/recall", run)
}

// run orchestrates semantic session retrieval with multiple granularity levels and fallback strategies.
//
// Index:
// - Purpose: Search and retrieve sessions, context windows, or chunks using semantic similarity with fallback to full-text search
// - Flow: validate input → detect embedding providers → generate query embedding → search at target granularity → format results → emit output
// - SideEffects: reads session store; performs embedding generation; searches multiple granularity levels; caches session data
// - FailureModes: missing embedding providers, session store access failures, embedding generation errors, search failures
// - Observability: emits search results, similarity scores, match counts, provider information, and comprehensive search statistics
// - Related: normalizeInput, semantic.NewProviderForScope, semantic.EnrichQuery
// - Keywords: session/recall, semantic_search, session_retrieval, embedding_search, full_text_search
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply defaults
	normalizeInput(&in, rc)
	if in.WindowGranularity && in.ChunkGranularity {
		return skillerr.Arg("window_granularity and chunk_granularity are mutually exclusive")
	}

	// Check for embedding API key - prefer Voyage, fall back to Gemini, then FTS
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")
	useFTSFallback := voyageKey == "" && geminiKey == ""

	// Open sessions store
	sessionStore, err := rc.Stores.Sessions(ctx)
	if err != nil {
		return skillerr.WrapIO("open sessions store", err)
	}

	// Generate embedding for the query - prefer Voyage, fall back to Gemini, then FTS
	var queryEmbedding []float32
	if !useFTSFallback {
		provider, err := semantic.NewProviderForScope(
			semantic.ScopeSessions,
			rc.Config,
			semantic.WithVoyageKey(voyageKey),
			semantic.WithGeminiKey(geminiKey),
			semantic.WithRateLimitWait(true),
		)
		if err != nil {
			return skillerr.WrapRuntime("embedding provider", err)
		}

		enrichedQuery := semantic.EnrichQuery(in.Query)
		if queryProvider, ok := provider.(semantic.QueryEmbeddingProvider); ok {
			queryEmbedding, err = queryProvider.EmbedQuery(ctx, enrichedQuery)
		} else {
			queryEmbedding, err = provider.Embed(ctx, enrichedQuery)
		}
		if err != nil {
			return skillerr.WrapRuntime("generate query embedding", err)
		}
	}

	var output Output
	output.Query = in.Query

	if in.ChunkGranularity {
		if useFTSFallback {
			return skillerr.Arg("chunk_granularity requires embeddings (set VOYAGE_API_KEY or GEMINI_API_KEY)")
		}

		chunkResults, err := sessionStore.SearchChunks(ctx, queryEmbedding, in.Limit*3)
		if err != nil {
			return skillerr.WrapIO("search chunks", err)
		}

		sessionCache := make(map[string]*sessions.Session)
		windowCache := make(map[string][]sessions.ContextWindow)
		summaryCache := make(map[string][]sessions.SessionChunkSummary)

		resolveWindowIndex := func(sessionID string, chunk sessions.SessionChunk) int {
			if chunk.ContextWindowIndex != 0 {
				return chunk.ContextWindowIndex
			}
			if windows, ok := windowCache[sessionID]; ok {
				for _, win := range windows {
					if chunk.ChunkIndex >= win.ChunkStart && chunk.ChunkIndex <= win.ChunkEnd {
						return win.WindowIndex
					}
				}
				return chunk.ContextWindowIndex
			}
			windows, err := sessionStore.GetContextWindows(ctx, sessionID)
			if err != nil {
				return chunk.ContextWindowIndex
			}
			windowCache[sessionID] = windows
			for _, win := range windows {
				if chunk.ChunkIndex >= win.ChunkStart && chunk.ChunkIndex <= win.ChunkEnd {
					return win.WindowIndex
				}
			}
			return chunk.ContextWindowIndex
		}

		loadSummaries := func(sessionID string, windowIndex int) []sessions.SessionChunkSummary {
			cacheKey := fmt.Sprintf("%s:%d", sessionID, windowIndex)
			if summaries, ok := summaryCache[cacheKey]; ok {
				return summaries
			}
			summaries, err := sessionStore.GetChunkSummaries(ctx, sessionID, windowIndex)
			if err != nil {
				return nil
			}
			summaryCache[cacheKey] = summaries
			return summaries
		}

		findSummary := func(summaries []sessions.SessionChunkSummary, chunkIndex int) *sessions.SessionChunkSummary {
			for _, summary := range summaries {
				if summary.ChunkIndexMin <= chunkIndex && summary.ChunkIndexMax >= chunkIndex {
					return &summary
				}
			}
			for _, summary := range summaries {
				for _, idx := range summary.ChunkIndices {
					if idx == chunkIndex {
						return &summary
					}
				}
			}
			return nil
		}

		chunkMatches := make([]ChunkMatch, 0)
		for _, r := range chunkResults {
			if r.Similarity < in.MinSimilarity {
				continue
			}

			sess, ok := sessionCache[r.Chunk.SessionID]
			if !ok {
				s, err := sessionStore.Get(ctx, r.Chunk.SessionID)
				if err == nil {
					sess = &s
					sessionCache[r.Chunk.SessionID] = sess
				}
			}
			if sess == nil {
				continue
			}
			if in.Workspace != "" && sess.WorkspacePath != in.Workspace {
				continue
			}
			if in.Project != "" && sess.ProjectName != in.Project {
				continue
			}

			windowIndex := resolveWindowIndex(r.Chunk.SessionID, r.Chunk)
			summaries := loadSummaries(r.Chunk.SessionID, windowIndex)
			summary := findSummary(summaries, r.Chunk.ChunkIndex)

			match := ChunkMatch{
				SessionID:      r.Chunk.SessionID,
				WindowIndex:    windowIndex,
				ChunkIndex:     r.Chunk.ChunkIndex,
				ChunkType:      r.Chunk.ChunkType,
				ContentPreview: r.Chunk.ContentPreview,
				Similarity:     r.Similarity,
			}
			if summary != nil {
				match.SummaryID = summary.ID
				match.Summary = summary.Summary
				match.SummaryModel = summary.SummaryModel
				match.ChunkIndexMin = summary.ChunkIndexMin
				match.ChunkIndexMax = summary.ChunkIndexMax
			}

			chunkMatches = append(chunkMatches, match)
			if len(chunkMatches) >= in.Limit {
				break
			}
		}

		output.ChunkMatches = chunkMatches
		output.TotalWithEmbeddings = len(chunkResults)
		output.Status = "ok"
		output.Message = fmt.Sprintf("Found %d relevant chunks for query", len(chunkMatches))

		if len(chunkMatches) == 0 {
			output.Status = "no_matches"
			output.Message = "No chunks matched the query above the similarity threshold"
		}
	} else if in.WindowGranularity {
		// Search at context window level for more granular retrieval
		windowResults, err := sessionStore.SearchContextWindows(ctx, queryEmbedding, in.Limit*2)
		if err != nil {
			return skillerr.WrapIO("search context windows", err)
		}

		windowMatches := make([]WindowMatch, 0)
		for _, r := range windowResults {
			if r.Similarity < in.MinSimilarity {
				continue
			}

			match := WindowMatch{
				SessionID:        r.Window.SessionID,
				WindowIndex:      r.Window.WindowIndex,
				Trigger:          r.Window.Trigger,
				PreCompactTokens: r.Window.PreCompactTokens,
				Summary:          r.Window.Summary,
				MessageCount:     r.Window.MessageCount,
				Similarity:       r.Similarity,
			}

			if !r.Window.StartedAt.IsZero() {
				match.StartedAt = r.Window.StartedAt.Format(time.RFC3339)
			}
			if !r.Window.EndedAt.IsZero() {
				match.EndedAt = r.Window.EndedAt.Format(time.RFC3339)
			}

			windowMatches = append(windowMatches, match)

			if len(windowMatches) >= in.Limit {
				break
			}
		}

		output.WindowMatches = windowMatches
		output.TotalWithEmbeddings = len(windowResults)
		output.Status = "ok"
		output.Message = fmt.Sprintf("Found %d relevant context windows for query", len(windowMatches))

		if len(windowMatches) == 0 {
			output.Status = "no_matches"
			output.Message = "No context windows matched the query above the similarity threshold"
		}
	} else {
		// Search at session level (default)
		matches := make([]SessionMatch, 0)
		var totalSearched int

		if useFTSFallback {
			// Full-text search fallback when no embedding API available
			ftsResults, err := sessionStore.Search(ctx, in.Query, in.Limit*2)
			if err != nil {
				return skillerr.WrapIO("text search", err)
			}
			totalSearched = len(ftsResults)

			for _, s := range ftsResults {
				if in.Workspace != "" && s.WorkspacePath != in.Workspace {
					continue
				}
				if in.Project != "" && s.ProjectName != in.Project {
					continue
				}

				match := SessionMatch{
					SessionID:    s.ID,
					ProjectName:  s.ProjectName,
					GitBranch:    s.GitBranch,
					Summary:      s.Summary,
					Accomplished: s.Accomplished,
					Decisions:    s.Decisions,
					Gotchas:      s.Gotchas,
					UserInsights: s.UserInsights,
					Tags:         s.Tags,
					KeyFiles:     s.KeyFiles,
					Similarity:   1.0, // FTS doesn't have similarity scores
				}

				if !s.StartedAt.IsZero() {
					match.StartedAt = s.StartedAt.Format(time.RFC3339)
				}

				matches = append(matches, match)
				if len(matches) >= in.Limit {
					break
				}
			}

			output.Status = "ok"
			output.Message = fmt.Sprintf("Found %d sessions via full-text search (no API key for semantic)", len(matches))
		} else {
			// Semantic search with embeddings
			results, err := sessionStore.SearchSimilar(ctx, in.Workspace, queryEmbedding, in.Limit*2)
			if err != nil {
				return skillerr.WrapIO("search similar sessions", err)
			}
			totalSearched = len(results)

			for _, r := range results {
				if r.Similarity < in.MinSimilarity {
					continue
				}

				if in.Workspace != "" && r.Session.WorkspacePath != in.Workspace {
					continue
				}

				if in.Project != "" && r.Session.ProjectName != in.Project {
					continue
				}

				match := SessionMatch{
					SessionID:    r.Session.ID,
					ProjectName:  r.Session.ProjectName,
					GitBranch:    r.Session.GitBranch,
					Summary:      r.Session.Summary,
					Accomplished: r.Session.Accomplished,
					Decisions:    r.Session.Decisions,
					Gotchas:      r.Session.Gotchas,
					UserInsights: r.Session.UserInsights,
					Tags:         r.Session.Tags,
					KeyFiles:     r.Session.KeyFiles,
					Similarity:   r.Similarity,
				}

				if !r.Session.StartedAt.IsZero() {
					match.StartedAt = r.Session.StartedAt.Format(time.RFC3339)
				}

				matches = append(matches, match)

				if len(matches) >= in.Limit {
					break
				}
			}

			output.Status = "ok"
			output.Message = fmt.Sprintf("Found %d relevant sessions for query", len(matches))
		}

		output.Matches = matches
		output.TotalWithEmbeddings = totalSearched

		if len(matches) == 0 {
			output.Status = "no_matches"
			if useFTSFallback {
				output.Message = "No sessions matched the query via full-text search"
			} else {
				output.Message = "No sessions matched the query above the similarity threshold"
			}
		}
	}

	return skillout.Emit(rc, "session/recall", output)
}

// normalizeInput applies default values and validation to input parameters with bounds checking.
func normalizeInput(in *Input, rc *skillmain.RunContext) {
	in.Limit = mathutil.DefaultPositiveInt(in.Limit, defaultLimit)
	in.MinSimilarity = mathutil.DefaultPositiveFloat(in.MinSimilarity, defaultMinSim)
	if in.Workspace == "" {
		in.Workspace = rc.Workspace
	}
}
