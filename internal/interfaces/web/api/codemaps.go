package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/intelligence/codemap"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

// CodemapListItemResponse represents a codemap in list API responses.
type CodemapListItemResponse struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Query       string `json:"query"`
	FileCount   int    `json:"file_count"`
	SymbolCount int    `json:"symbol_count"`
	CreatedAt   string `json:"created_at"`
}

// CodemapsListHandler returns a handler for GET /api/codemaps.
func CodemapsListHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		limit := 50
		if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
			if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 200 {
				limit = n
			}
		}

		workspace := r.URL.Query().Get("workspace")
		// Default to current directory if no workspace specified
		if workspace == "" {
			workspace = "."
		}

		// Open memory store (codemaps are stored as type="codemap")
		store, err := memory.Open(r.Context(), cfg.Storage.Root, cfg.Paths.CAS)
		if err != nil {
			log.Error().Err(err).Msg("failed to open memory store")
			httpError(w, http.StatusInternalServerError, "failed to open memory store")
			return
		}
		defer store.Close()

		// List codemaps by type
		results, err := store.ListByType(r.Context(), workspace, "codemap", limit)
		if err != nil {
			log.Error().Err(err).Msg("failed to list codemaps")
			httpError(w, http.StatusInternalServerError, "failed to list codemaps")
			return
		}

		resp := make([]CodemapListItemResponse, 0, len(results))
		for _, r := range results {
			item, err := codemapListItemFromEntry(r.Entry)
			if err != nil {
				log.Warn().Err(err).Str("codemap", r.Entry.Name).Msg("failed to parse codemap entry")
			}
			resp = append(resp, item)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"codemaps": resp,
		})
	}
}

// CodemapDetailHandler returns a handler for GET/DELETE /api/codemaps/{id}.
func CodemapDetailHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract codemap ID from path
		path := r.URL.Path
		const prefix = "/api/codemaps/"
		if !strings.HasPrefix(path, prefix) {
			httpError(w, http.StatusNotFound, "not found")
			return
		}

		remaining := strings.TrimPrefix(path, prefix)
		parts := strings.SplitN(remaining, "/", 2)
		codemapID := parts[0]

		if codemapID == "" {
			httpError(w, http.StatusBadRequest, "missing codemap id")
			return
		}

		// Handle search sub-route
		if codemapID == "search" {
			handleCodemapSearch(w, r, cfg, log)
			return
		}

		store, err := memory.Open(r.Context(), cfg.Storage.Root, cfg.Paths.CAS)
		if err != nil {
			log.Error().Err(err).Msg("failed to open memory store")
			httpError(w, http.StatusInternalServerError, "failed to open memory store")
			return
		}
		defer store.Close()

		// Get workspace from query param
		workspace := r.URL.Query().Get("workspace")
		if workspace == "" {
			workspace = "."
		}

		switch r.Method {
		case http.MethodGet:
			// Get codemap by name (ID may be prefixed or raw)
			entry, err := getCodemapEntry(r.Context(), store, workspace, codemapID)
			if err != nil {
				log.Error().Err(err).Str("id", codemapID).Msg("failed to get codemap")
				httpError(w, http.StatusNotFound, "codemap not found")
				return
			}

			cm, err := codemapFromEntry(entry)
			if err != nil {
				log.Error().Err(err).Str("id", codemapID).Msg("failed to parse codemap")
				httpError(w, http.StatusInternalServerError, "failed to parse codemap")
				return
			}

			writeJSON(w, http.StatusOK, cm)

		case http.MethodDelete:
			entry, err := getCodemapEntry(r.Context(), store, workspace, codemapID)
			if err != nil {
				log.Error().Err(err).Str("id", codemapID).Msg("failed to find codemap")
				httpError(w, http.StatusNotFound, "codemap not found")
				return
			}
			if err := store.Delete(r.Context(), entry.Name, workspace); err != nil {
				log.Error().Err(err).Str("id", codemapID).Msg("failed to delete codemap")
				httpError(w, http.StatusInternalServerError, "failed to delete codemap")
				return
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

// handleCodemapSearch handles GET /api/codemaps/search.
func handleCodemapSearch(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger) {
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
	results, err := store.Search(r.Context(), workspace, query, limit)
	if err != nil {
		log.Error().Err(err).Msg("failed to search codemaps")
		httpError(w, http.StatusInternalServerError, "failed to search codemaps")
		return
	}

	// Filter to only codemaps
	resp := make([]SearchResultResponse, 0)
	for _, r := range results {
		if r.Entry.Type == "codemap" {
			path := r.Entry.Name
			var title string
			var codemapID string
			if cm, err := codemapFromEntry(r.Entry); err == nil {
				title = cm.Title
				codemapID = cm.ID
				if cm.Query != "" {
					path = cm.Query
				}
			} else {
				codemapID = codemapIDFromName(r.Entry.Name)
			}
			if title == "" {
				title = r.Entry.Name
			}
			if path == "" {
				path = r.Entry.ID
			}
			resp = append(resp, SearchResultResponse{
				Source:     "codemap",
				ID:         codemapID,
				Name:       title,
				Path:       path,
				Snippet:    r.Entry.Summary,
				Summary:    r.Entry.Summary,
				Similarity: r.Score,
				FinalScore: r.Score,
				Rank:       len(resp) + 1,
				SourceRank: len(resp) + 1,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"results": resp,
	})
}

func codemapListItemFromEntry(entry memory.NamedEntry) (CodemapListItemResponse, error) {
	cm, err := codemapFromEntry(entry)
	if err != nil {
		id := codemapIDFromName(entry.Name)
		title := entry.Summary
		if title == "" {
			title = entry.Name
		}
		return CodemapListItemResponse{
			ID:          id,
			Title:       title,
			Query:       "",
			FileCount:   0,
			SymbolCount: 0,
			CreatedAt:   formatCodemapTime(entry.CreatedAt),
		}, err
	}

	return CodemapListItemResponse{
		ID:          cm.ID,
		Title:       cm.Title,
		Query:       cm.Query,
		FileCount:   cm.FileCount,
		SymbolCount: cm.SymbolCount,
		CreatedAt:   formatCodemapTime(cm.CreatedAt),
	}, nil
}

func codemapFromEntry(entry memory.NamedEntry) (codemap.Codemap, error) {
	if ws, ok, err := codemap.ParseWindsurfCodemap(entry.Result); err != nil {
		return codemap.Codemap{}, err
	} else if ok {
		converted := ws.ToCodemap()
		if converted == nil {
			return codemap.Codemap{}, errors.New("parse codemap: empty Windsurf conversion")
		}
		if converted.ID == "" {
			converted.ID = codemapIDFromName(entry.Name)
		}
		if converted.Workspace == "" {
			converted.Workspace = entry.Workspace
		}
		if converted.CreatedAt.IsZero() {
			converted.CreatedAt = entry.CreatedAt
		}
		return *converted, nil
	}

	var cm codemap.Codemap
	if err := json.Unmarshal(entry.Result, &cm); err != nil {
		return cm, err
	}
	if cm.ID == "" {
		cm.ID = codemapIDFromName(entry.Name)
	}
	if cm.Workspace == "" {
		cm.Workspace = entry.Workspace
	}
	if cm.CreatedAt.IsZero() {
		cm.CreatedAt = entry.CreatedAt
	}
	return cm, nil
}

func codemapIDFromName(name string) string {
	name = strings.TrimPrefix(name, "codemap://")
	name = strings.TrimPrefix(name, "codemap:")
	return name
}

func codemapNameFromID(id string) string {
	if strings.HasPrefix(id, "codemap://") {
		return id
	}
	if strings.HasPrefix(id, "codemap:") {
		return "codemap://" + strings.TrimPrefix(id, "codemap:")
	}
	return "codemap://" + id
}

func getCodemapEntry(ctx context.Context, store *memory.Store, workspace, codemapID string) (memory.NamedEntry, error) {
	names := []string{codemapID}
	if codemapID != "" && !strings.HasPrefix(codemapID, "codemap://") && !strings.HasPrefix(codemapID, "codemap:") {
		names = append(names, codemapNameFromID(codemapID))
		names = append(names, "codemap:"+codemapID)
	} else if strings.HasPrefix(codemapID, "codemap:") && !strings.HasPrefix(codemapID, "codemap://") {
		names = append(names, codemapNameFromID(codemapID))
	}

	for _, name := range names {
		entry, err := store.Get(ctx, name, workspace)
		if err == nil {
			return entry, nil
		}
		if !errors.Is(err, memory.ErrNotFound) {
			return memory.NamedEntry{}, err
		}
	}

	entries, err := store.ListByType(ctx, workspace, "codemap", 500)
	if err != nil {
		return memory.NamedEntry{}, err
	}
	for _, scored := range entries {
		name := scored.Entry.Name
		if strings.Contains(name, codemapID) {
			return scored.Entry, nil
		}
	}

	return memory.NamedEntry{}, memory.ErrNotFound
}

func formatCodemapTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02T15:04:05Z07:00")
}
