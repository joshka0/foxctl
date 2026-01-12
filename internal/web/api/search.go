package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

// SearchResultResponse represents a search result in API responses.
type SearchResultResponse struct {
	Source      string  `json:"source"`
	ID          string  `json:"id"`
	Name        string  `json:"name,omitempty"`
	Path        string  `json:"path"`
	Snippet     string  `json:"snippet,omitempty"`
	Summary     string  `json:"summary,omitempty"`
	Similarity  float64 `json:"similarity"`
	RerankScore float64 `json:"rerank_score,omitempty"`
	FinalScore  float64 `json:"final_score"`
	Rank        int     `json:"rank"`
	SourceRank  int     `json:"source_rank"`
}

// SearchStatsResponse contains search statistics.
type SearchStatsResponse struct {
	TotalResults        int            `json:"total_results"`
	SourceCounts        map[string]int `json:"source_counts"`
	Reranked            bool           `json:"reranked"`
	EmbeddingDimensions int            `json:"embedding_dimensions"`
	LatencyMs           int64          `json:"latency_ms"`
}

// SearchHandler returns a handler for GET /api/search.
func SearchHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		query := r.URL.Query().Get("q")
		if query == "" {
			httpError(w, http.StatusBadRequest, "q parameter required")
			return
		}

		limit := 20
		if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
			if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 100 {
				limit = n
			}
		}

		scope := r.URL.Query().Get("scope")
		if scope == "" {
			scope = "memory"
		}
		sourceDefault := normalizeSearchScope(scope)
		if sourceDefault == "" {
			sourceDefault = "memory"
		}
		rerank := r.URL.Query().Get("rerank") == "true"

		// For now, search memory store
		// Full implementation would search across multiple scopes
		store, err := memory.Open(r.Context(), cfg.Storage.Root, cfg.Paths.CAS)
		if err != nil {
			log.Error().Err(err).Msg("failed to open memory store")
			httpError(w, http.StatusInternalServerError, "failed to open memory store")
			return
		}
		defer store.Close()

		workspace := r.URL.Query().Get("workspace")
		if workspace == "" {
			workspace = "."
		}
		start := time.Now()
		results, err := store.Search(r.Context(), workspace, query, limit)
		if err != nil {
			log.Error().Err(err).Msg("failed to search")
			httpError(w, http.StatusInternalServerError, "failed to search")
			return
		}

		resp := make([]SearchResultResponse, 0, len(results))
		sourceCounts := map[string]int{}
		for i, r := range results {
			source := sourceDefault
			if normalized := normalizeSearchScope(r.Entry.Type); normalized != "" {
				source = normalized
			}
			path := r.Entry.Name
			if path == "" {
				path = r.Entry.ID
			}
			sourceCounts[source]++

			resp = append(resp, SearchResultResponse{
				Source:     source,
				ID:         r.Entry.ID,
				Name:       r.Entry.Name,
				Path:       path,
				Snippet:    r.Entry.Summary,
				Summary:    r.Entry.Summary,
				Similarity: r.Score,
				FinalScore: r.Score,
				Rank:       i + 1,
				SourceRank: i + 1,
			})
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"results": resp,
			"stats": SearchStatsResponse{
				TotalResults:        len(resp),
				SourceCounts:        sourceCounts,
				Reranked:            rerank,
				EmbeddingDimensions: 0,
				LatencyMs:           time.Since(start).Milliseconds(),
			},
		})
	}
}

func normalizeSearchScope(scope string) string {
	switch scope {
	case "memories", "memory":
		return "memory"
	case "sessions", "session":
		return "session"
	case "symbols", "symbol":
		return "symbol"
	case "tasks", "task":
		return "task"
	default:
		return ""
	}
}
