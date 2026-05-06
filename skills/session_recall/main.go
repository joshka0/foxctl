// Package main implements the session/recall skill for semantic session retrieval.
package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/mathutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/storage/annotations"
	"github.com/joshka0/foxctl/internal/storage/sessions"
)

// Input defines the skill input parameters for semantic session retrieval with multiple granularity options.
type Input struct {
	Query                 string  `json:"query,omitempty"`
	Limit                 int     `json:"limit,omitempty"`
	MinSimilarity         float64 `json:"min_similarity,omitempty"`
	Workspace             string  `json:"workspace,omitempty"`
	Project               string  `json:"project,omitempty"`
	SessionID             string  `json:"session_id,omitempty"`
	ListSessions          bool    `json:"list_sessions,omitempty"`
	TOC                   bool    `json:"toc,omitempty"`
	WindowGranularity     bool    `json:"window_granularity,omitempty"`     // Search at context window level instead of session
	ChunkGranularity      bool    `json:"chunk_granularity,omitempty"`      // Search at chunk level (requires embeddings)
	AnnotationGranularity bool    `json:"annotation_granularity,omitempty"` // Search turn annotations (requires annotations.db embeddings)
	Sort                  string  `json:"sort,omitempty"`                   // "relevance" (default), "recent", "oldest" — for annotation results
	FilterCategory        string  `json:"filter_category,omitempty"`        // Filter annotation results by toc_category
	SortBy                string  `json:"sort_by,omitempty"`                // Comma-separated annotation sort keys: similarity,date,recent
	CategoryStats         bool    `json:"category_stats,omitempty"`         // Return annotation counts grouped by category
}

// Output defines the skill output with comprehensive search results and match statistics.
type Output struct {
	Query               string              `json:"query"`
	Matches             []SessionMatch      `json:"matches,omitempty"`
	WindowMatches       []WindowMatch       `json:"window_matches,omitempty"`     // Populated when window_granularity is true
	ChunkMatches        []ChunkMatch        `json:"chunk_matches,omitempty"`      // Populated when chunk_granularity is true
	AnnotationMatches   []AnnotationMatch   `json:"annotation_matches,omitempty"` // Populated when annotation_granularity is true
	SessionList         []RecallSessionRef  `json:"session_list,omitempty"`
	TOCMatches          []RecallTOCMatch    `json:"toc_matches,omitempty"`
	CategoryCounts      []CategoryCountItem `json:"category_counts,omitempty"` // Populated when category_stats is true
	TotalWithEmbeddings int                 `json:"total_with_embeddings"`
	Status              string              `json:"status"`
	Message             string              `json:"message"`
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

// AnnotationMatch represents a matched turn annotation with similarity score.
type AnnotationMatch struct {
	SessionID      string   `json:"session_id"`
	TurnIndex      int      `json:"turn_index"`
	WindowIndex    int      `json:"window_index"`
	MatchedAt      string   `json:"matched_at,omitempty"`
	Role           string   `json:"role,omitempty"`
	ChunkType      string   `json:"chunk_type,omitempty"`
	ContentPreview string   `json:"content_preview,omitempty"`
	TOCLabel       string   `json:"toc_label,omitempty"`
	TOCCategory    string   `json:"toc_category,omitempty"`
	Intent         string   `json:"intent,omitempty"`
	FilePaths      []string `json:"file_paths,omitempty"`
	ToolsUsed      []string `json:"tools_used,omitempty"`
	Similarity     float64  `json:"similarity"`
}

// RecallSessionRef represents a session summary row used by list_sessions mode.
type RecallSessionRef struct {
	SessionID     string `json:"session_id"`
	WorkspacePath string `json:"workspace_path,omitempty"`
	ProjectName   string `json:"project_name,omitempty"`
	Status        string `json:"status,omitempty"`
	Summary       string `json:"summary,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
}

// RecallTOCMatch represents a table-of-contents match for a single session window.
type RecallTOCMatch struct {
	SessionID      string `json:"session_id"`
	TurnIndex      int    `json:"turn_index"`
	WindowIndex    int    `json:"window_index"`
	TOCLabel       string `json:"toc_label"`
	TOCCategory    string `json:"toc_category"`
	Intent         string `json:"intent,omitempty"`
	ContentPreview string `json:"content_preview,omitempty"`
}

// CategoryCountItem represents annotation counts for a category.
type CategoryCountItem struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

const (
	defaultLimit  = 5
	defaultMinSim = 0.3
	commandName   = "session/recall"
)

type annotationSortField string

const (
	sortFieldSimilarity annotationSortField = "similarity"
	sortFieldDate       annotationSortField = "date"
	sortFieldRecent     annotationSortField = "recent"
)

type annotationCandidate struct {
	Match       AnnotationMatch
	SortAt      time.Time
	Similarity  float64
	SessionID   string
	WindowIndex int
	TurnIndex   int
}

// main is the skill entry point for session/recall with semantic search capabilities.
func main() {
	skillmain.Main(commandName, run)
}

// run orchestrates semantic session retrieval with multiple granularity levels and fallback strategies.
//
// Index:
//   Purpose: Search and retrieve sessions, context windows, or chunks using semantic similarity with fallback to full-text search
//   Keywords: session/recall, semantic_search, session_retrieval, embedding_search, full_text_search
//   Related: normalizeInput, semantic.NewProviderForScope, semantic.EnrichQuery
//   Flow: validate input → detect embedding providers → generate query embedding → search at target granularity → format results → emit output
//   Resources: session store, annotations store, embedding provider
//   Events: semantic retrieval events
//   OutputFields: query, matches, window_matches, chunk_matches, annotation_matches, session_list
// [[domain:semantic-session-retrieval]]
// [[protocol:granularity-level-dispatch]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply defaults
	normalizeInput(&in, rc)
	granCount := 0
	if in.WindowGranularity {
		granCount++
	}
	if in.ChunkGranularity {
		granCount++
	}
	if in.AnnotationGranularity {
		granCount++
	}
	if granCount > 1 {
		return skillerr.Arg("window_granularity, chunk_granularity, and annotation_granularity are mutually exclusive")
	}
	if in.ListSessions && in.TOC {
		return skillerr.Arg("list_sessions and toc are mutually exclusive")
	}
	if in.CategoryStats && (in.ListSessions || in.TOC) {
		return skillerr.Arg("category_stats is mutually exclusive with list_sessions and toc")
	}
	if !in.ListSessions && !in.TOC && !in.CategoryStats && strings.TrimSpace(in.Query) == "" {
		return skillerr.Arg("query is required unless list_sessions, toc, or category_stats mode is enabled")
	}
	if !in.AnnotationGranularity && strings.TrimSpace(in.FilterCategory) != "" {
		return skillerr.Arg("filter_category is only valid when annotation_granularity is true")
	}
	if !in.AnnotationGranularity && strings.TrimSpace(in.SortBy) != "" {
		return skillerr.Arg("sort_by is only valid when annotation_granularity is true")
	}
	if !in.AnnotationGranularity && strings.TrimSpace(in.Sort) != "" {
		return skillerr.Arg("sort is only valid when annotation_granularity is true")
	}

	annotationSortFields, err := parseSortBy(in.SortBy, in.Sort)
	if err != nil {
		return err
	}

	// Open sessions store
	sessionStore, err := rc.Stores.Sessions(ctx)
	if err != nil {
		return skillerr.WrapIO("open sessions store", err)
	}

	output := Output{Query: in.Query}
	writeOutput := func(out Output) error {
		return skillout.Emit(rc, commandName, out)
	}

	if in.ListSessions {
		opts := sessions.ListOptions{
			WorkspacePath: in.Workspace,
			ProjectName:   in.Project,
			Limit:         in.Limit,
		}
		if opts.Limit <= 0 {
			opts.Limit = 20
		}

		sessionList, err := sessionStore.List(ctx, opts)
		if err != nil {
			return skillerr.WrapIO("list sessions", err)
		}

		for _, s := range sessionList {
			if in.SessionID != "" && s.ID != in.SessionID {
				continue
			}
			ref := RecallSessionRef{
				SessionID:     s.ID,
				WorkspacePath: s.WorkspacePath,
				ProjectName:   s.ProjectName,
				Status:        s.Status,
				Summary:       s.Summary,
			}
			if !s.StartedAt.IsZero() {
				ref.StartedAt = s.StartedAt.Format(time.RFC3339)
			}
			output.SessionList = append(output.SessionList, ref)
		}

		if len(output.SessionList) == 0 {
			output.Status = "no_matches"
			output.Message = "No sessions matched the listing filters"
		} else {
			output.Status = "ok"
			output.Message = fmt.Sprintf("Found %d sessions", len(output.SessionList))
		}

		return writeOutput(output)
	}

	if in.CategoryStats {
		annStore, annErr := annotations.Open(ctx, "")
		if annErr != nil {
			return skillerr.WrapIO("open annotations store", annErr)
		}
		defer annStore.Close()

		var sessionIDs []string
		if in.SessionID != "" {
			sessionIDs = []string{in.SessionID}
		}
		counts, countErr := annStore.CountByCategory(ctx, sessionIDs)
		if countErr != nil {
			return skillerr.WrapIO("count categories", countErr)
		}

		total := 0
		for _, cc := range counts {
			output.CategoryCounts = append(output.CategoryCounts, CategoryCountItem{
				Category: cc.Category,
				Count:    cc.Count,
			})
			total += cc.Count
		}

		output.Status = "ok"
		output.Message = fmt.Sprintf("Found %d annotations across %d categories", total, len(counts))
		return writeOutput(output)
	}

	if in.TOC {
		if strings.TrimSpace(in.SessionID) == "" {
			return skillerr.Arg("toc mode requires session_id")
		}

		windows, err := sessionStore.GetContextWindows(ctx, in.SessionID)
		if err != nil {
			return skillerr.WrapIO("load context windows", err)
		}

		for _, w := range windows {
			summary := compactText(w.Summary, 0)
			if summary == "" {
				continue
			}

			category := strings.TrimSpace(w.Trigger)
			if category == "" {
				category = "window"
			}

			output.TOCMatches = append(output.TOCMatches, RecallTOCMatch{
				SessionID:      w.SessionID,
				TurnIndex:      w.ChunkEnd,
				WindowIndex:    w.WindowIndex,
				TOCLabel:       compactText(summary, 120),
				TOCCategory:    category,
				ContentPreview: compactText(summary, 240),
			})
		}

		if len(output.TOCMatches) == 0 {
			output.Status = "no_matches"
			output.Message = "No TOC entries found for session"
		} else {
			output.Status = "ok"
			output.Message = fmt.Sprintf("Found %d TOC entries", len(output.TOCMatches))
		}

		return writeOutput(output)
	}

	// Check for embedding API key - prefer Voyage, fall back to Gemini, then FTS
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")
	useFTSFallback := semantic.DetectProviderForConfig(rc.Config, voyageKey, geminiKey) == ""

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

	if in.ChunkGranularity {
		if useFTSFallback {
			return skillerr.Arg("chunk_granularity requires embeddings (configure a repo embedding provider or set VOYAGE_API_KEY / GEMINI_API_KEY)")
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
			if in.SessionID != "" && r.Chunk.SessionID != in.SessionID {
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
	} else if in.AnnotationGranularity {
		if useFTSFallback {
			return skillerr.Arg("annotation_granularity requires embeddings (set VOYAGE_API_KEY or GEMINI_API_KEY)")
		}

		annStore, err := annotations.Open(ctx, "")
		if err != nil {
			return skillerr.WrapIO("open annotations store", err)
		}
		defer annStore.Close()

		searchOpts := annotations.AnnotationSearchOptions{
			Limit:       in.Limit * 3,
			TOCCategory: strings.TrimSpace(in.FilterCategory),
		}
		if in.SessionID != "" {
			searchOpts.SessionIDs = []string{in.SessionID}
		}

		annResults, err := annStore.SearchSimilarFiltered(ctx, queryEmbedding, searchOpts)
		if err != nil {
			return skillerr.WrapIO("search annotations", err)
		}

		sessionCache := make(map[string]*sessions.Session)
		candidates := make([]annotationCandidate, 0, len(annResults))
		for _, scored := range annResults {
			ann := scored.TurnAnnotation
			sim := scored.Similarity
			if sim < in.MinSimilarity {
				continue
			}
			if in.SessionID != "" && ann.SessionID != in.SessionID {
				continue
			}
			sess, ok := sessionCache[ann.SessionID]
			if !ok {
				s, getErr := sessionStore.Get(ctx, ann.SessionID)
				if getErr == nil {
					sess = &s
				}
				sessionCache[ann.SessionID] = sess
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

			sortAt := annotationMatchTime(ann)
			match := AnnotationMatch{
				SessionID:      ann.SessionID,
				TurnIndex:      ann.TurnIndex,
				WindowIndex:    ann.ContextWindowIndex,
				Role:           ann.Role,
				ChunkType:      ann.ChunkType,
				ContentPreview: ann.ContentPreview,
				TOCLabel:       ann.TOCLabel,
				TOCCategory:    ann.TOCCategory,
				Intent:         ann.Intent,
				FilePaths:      ann.FilePaths,
				ToolsUsed:      ann.ToolsUsed,
				Similarity:     sim,
			}
			if !sortAt.IsZero() {
				match.MatchedAt = sortAt.Format(time.RFC3339)
			}

			candidates = append(candidates, annotationCandidate{
				Match:       match,
				SortAt:      sortAt,
				Similarity:  sim,
				SessionID:   ann.SessionID,
				WindowIndex: ann.ContextWindowIndex,
				TurnIndex:   ann.TurnIndex,
			})
		}

		sortAnnotationCandidates(candidates, annotationSortFields)

		annMatches := make([]AnnotationMatch, 0, minInt(in.Limit, len(candidates)))
		for _, candidate := range candidates {
			annMatches = append(annMatches, candidate.Match)
			if len(annMatches) >= in.Limit {
				break
			}
		}

		output.AnnotationMatches = annMatches
		output.TotalWithEmbeddings = len(annResults)
		output.Status = "ok"
		output.Message = fmt.Sprintf("Found %d relevant annotations for query", len(annMatches))

		if len(annMatches) == 0 {
			output.Status = "no_matches"
			output.Message = "No annotations matched the query above the similarity threshold"
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
			if in.SessionID != "" && r.Window.SessionID != in.SessionID {
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
				if in.SessionID != "" && s.ID != in.SessionID {
					continue
				}
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
				if in.SessionID != "" && r.Session.ID != in.SessionID {
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

	return writeOutput(output)
}

// normalizeInput applies default values and validation to input parameters with bounds checking.
func normalizeInput(in *Input, rc *skillmain.RunContext) {
	in.Limit = mathutil.DefaultPositiveInt(in.Limit, defaultLimit)
	in.MinSimilarity = mathutil.DefaultPositiveFloat(in.MinSimilarity, defaultMinSim)
	if in.Workspace == "" {
		in.Workspace = rc.Workspace
	}
}

func parseSortBy(sortBy, legacySort string) ([]annotationSortField, error) {
	parseFields := func(raw string, legacy bool) ([]annotationSortField, error) {
		if strings.TrimSpace(raw) == "" {
			return nil, nil
		}
		parts := strings.Split(raw, ",")
		fields := make([]annotationSortField, 0, len(parts))
		seen := make(map[annotationSortField]struct{}, len(parts))
		for _, part := range parts {
			token := strings.ToLower(strings.TrimSpace(part))
			if token == "" {
				continue
			}
			field, ok := parseSortFieldToken(token, legacy)
			if !ok {
				return nil, skillerr.Arg(fmt.Sprintf("invalid sort field %q (allowed: similarity,date,recent)", token))
			}
			if _, exists := seen[field]; exists {
				continue
			}
			seen[field] = struct{}{}
			fields = append(fields, field)
		}
		if len(fields) == 0 {
			return nil, skillerr.Arg("sort field is empty")
		}
		return fields, nil
	}

	fields, err := parseFields(sortBy, false)
	if err != nil {
		return nil, err
	}
	legacyFields, err := parseFields(legacySort, true)
	if err != nil {
		return nil, err
	}

	if len(fields) == 0 && len(legacyFields) == 0 {
		return []annotationSortField{sortFieldSimilarity}, nil
	}
	if len(fields) > 0 && len(legacyFields) > 0 && !equalSortFields(fields, legacyFields) {
		return nil, skillerr.Arg("sort and sort_by conflict; use one sort syntax or matching values")
	}
	if len(fields) > 0 {
		return fields, nil
	}
	return legacyFields, nil
}

func parseSortFieldToken(token string, legacy bool) (annotationSortField, bool) {
	if legacy {
		switch token {
		case "relevance", "similarity":
			return sortFieldSimilarity, true
		case "recent":
			return sortFieldRecent, true
		case "oldest":
			return sortFieldDate, true
		default:
			return "", false
		}
	}

	switch token {
	case "similarity", "relevance":
		return sortFieldSimilarity, true
	case "date", "oldest":
		return sortFieldDate, true
	case "recent", "newest":
		return sortFieldRecent, true
	default:
		return "", false
	}
}

func equalSortFields(a, b []annotationSortField) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sortAnnotationCandidates(cands []annotationCandidate, fields []annotationSortField) {
	if len(fields) == 0 {
		fields = []annotationSortField{sortFieldSimilarity}
	}
	sort.Slice(cands, func(i, j int) bool {
		a := cands[i]
		b := cands[j]
		for _, field := range fields {
			switch field {
			case sortFieldSimilarity:
				if math.Abs(a.Similarity-b.Similarity) > 1e-9 {
					return a.Similarity > b.Similarity
				}
			case sortFieldDate:
				if cmp := compareAnnotationTimes(a.SortAt, b.SortAt); cmp != 0 {
					return cmp < 0
				}
			case sortFieldRecent:
				if cmp := compareAnnotationTimes(a.SortAt, b.SortAt); cmp != 0 {
					return cmp > 0
				}
			}
		}
		if a.SessionID != b.SessionID {
			return a.SessionID < b.SessionID
		}
		if a.WindowIndex != b.WindowIndex {
			return a.WindowIndex < b.WindowIndex
		}
		return a.TurnIndex < b.TurnIndex
	})
}

func compareAnnotationTimes(a, b time.Time) int {
	if a.IsZero() && b.IsZero() {
		return 0
	}
	if a.IsZero() {
		return 1
	}
	if b.IsZero() {
		return -1
	}
	switch {
	case a.Before(b):
		return -1
	case a.After(b):
		return 1
	default:
		return 0
	}
}

func annotationMatchTime(ann *annotations.TurnAnnotation) time.Time {
	if ann == nil {
		return time.Time{}
	}
	if !ann.Timestamp.IsZero() {
		return ann.Timestamp
	}
	return ann.CreatedAt
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func compactText(in string, max int) string {
	text := strings.TrimSpace(strings.Join(strings.Fields(in), " "))
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max]
}
