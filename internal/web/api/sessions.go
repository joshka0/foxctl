package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

// SessionResponse represents a session in API responses.
type SessionResponse struct {
	ID              string   `json:"id"`
	WorkspacePath   string   `json:"workspace_path"`
	ProjectName     string   `json:"project_name,omitempty"`
	GitBranch       string   `json:"git_branch,omitempty"`
	ClaudeVersion   string   `json:"claude_version,omitempty"`
	StartedAt       string   `json:"started_at"`
	EndedAt         string   `json:"ended_at,omitempty"`
	Summary         string   `json:"summary,omitempty"`
	Accomplished    []string `json:"accomplished,omitempty"`
	Decisions       []string `json:"decisions,omitempty"`
	Gotchas         []string `json:"gotchas,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	KeyFiles        []string `json:"key_files,omitempty"`
	ToolsPattern    string   `json:"tools_pattern,omitempty"`
	MessageCount    int      `json:"message_count"`
	UserTurns       int      `json:"user_turns"`
	ToolInvocations int      `json:"tool_invocations"`
	TotalTokens     int      `json:"total_tokens"`
	Status          string   `json:"status"`
	AgentID         string   `json:"agent_id"`
	ParentSessionID string   `json:"parent_session_id,omitempty"`
}

// SessionsListHandler returns a handler for GET /api/sessions.
func SessionsListHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Parse query params
		limit := 50
		if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
			if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}

		offset := 0
		if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
			if n, err := strconv.Atoi(offsetStr); err == nil && n >= 0 {
				offset = n
			}
		}

		workspace := r.URL.Query().Get("workspace")

		// Open sessions store
		store, err := sessions.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open sessions store")
			httpError(w, http.StatusInternalServerError, "failed to open sessions store")
			return
		}
		defer store.Close()

		// List sessions
		opts := storage.SessionListOptions{
			WorkspacePath: workspace,
			Limit:         limit,
			Offset:        offset,
		}

		sessionList, err := store.List(r.Context(), opts)
		if err != nil {
			log.Error().Err(err).Msg("failed to list sessions")
			httpError(w, http.StatusInternalServerError, "failed to list sessions")
			return
		}

		// Convert to response format
		resp := make([]SessionResponse, 0, len(sessionList))
		for _, s := range sessionList {
			sr := sessionToResponse(s)
			resp = append(resp, sr)
		}

		// Get total count for pagination
		stats, err := store.Stats(r.Context())
		var total int64
		if err == nil {
			total = stats.Count
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"sessions": resp,
			"total":    total,
			"limit":    limit,
			"offset":   offset,
		})
	}
}

// SessionDetailHandler returns a handler for GET /api/sessions/{id}.
func SessionDetailHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract session ID from path
		path := r.URL.Path
		const prefix = "/api/sessions/"

		remaining := strings.TrimPrefix(path, prefix)
		parts := strings.SplitN(remaining, "/", 2)
		sessionID := parts[0]

		if sessionID == "" {
			httpError(w, http.StatusBadRequest, "missing session id")
			return
		}

		// Handle special routes first
		if sessionID == "search" {
			handleSessionSearch(w, r, cfg, log)
			return
		}

		// Handle sub-routes
		if len(parts) > 1 {
			switch parts[1] {
			case "messages":
				handlePersistedSessionMessages(w, r, cfg, log, sessionID)
				return
			case "context-windows":
				handleSessionContextWindows(w, r, cfg, log, sessionID)
				return
			}
		}

		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Open sessions store
		store, err := sessions.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open sessions store")
			httpError(w, http.StatusInternalServerError, "failed to open sessions store")
			return
		}
		defer store.Close()

		// Get session
		session, err := store.Get(r.Context(), sessionID)
		if err != nil {
			log.Error().Err(err).Str("session_id", sessionID).Msg("failed to get session")
			httpError(w, http.StatusNotFound, "session not found")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"session": sessionToResponse(session),
		})
	}
}

// handleSessionSearch handles GET /api/sessions/search.
func handleSessionSearch(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	pattern := r.URL.Query().Get("pattern")
	if pattern == "" {
		httpError(w, http.StatusBadRequest, "pattern required")
		return
	}

	limit := 20
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	store, err := sessions.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open sessions store")
		httpError(w, http.StatusInternalServerError, "failed to open sessions store")
		return
	}
	defer store.Close()

	results, err := store.Search(r.Context(), pattern, limit)
	if err != nil {
		log.Error().Err(err).Msg("failed to search sessions")
		httpError(w, http.StatusInternalServerError, "failed to search sessions")
		return
	}

	resp := make([]SessionResponse, 0, len(results))
	for _, s := range results {
		resp = append(resp, sessionToResponse(s))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"results": resp,
		"total":   len(resp),
		"pattern": pattern,
	})
}

// handlePersistedSessionMessages handles GET /api/sessions/{id}/messages.
func handlePersistedSessionMessages(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, sessionID string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if n, err := strconv.Atoi(offsetStr); err == nil && n >= 0 {
			offset = n
		}
	}

	store, err := sessions.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open sessions store")
		httpError(w, http.StatusInternalServerError, "failed to open sessions store")
		return
	}
	defer store.Close()

	// Get turns (messages)
	opts := storage.SessionTurnListOptions{
		Limit:  limit,
		Offset: offset,
	}
	turns, err := store.GetTurns(r.Context(), sessionID, opts)
	if err != nil {
		log.Error().Err(err).Str("session_id", sessionID).Msg("failed to get turns")
		httpError(w, http.StatusInternalServerError, "failed to get messages")
		return
	}

	// Convert turns to message format
	messages := make([]map[string]any, 0, len(turns))
	for _, t := range turns {
		msg := map[string]any{
			"id":              t.ID,
			"turn_index":      t.TurnIndex,
			"role":            t.Role,
			"content_preview": t.ContentPreview,
			"timestamp":       t.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
			"has_error":       t.HasError,
		}
		if len(t.ToolCalls) > 0 {
			msg["tool_calls"] = t.ToolCalls
		}
		if len(t.FilesTouched) > 0 {
			msg["files_touched"] = t.FilesTouched
		}
		if t.ErrorMessage != "" {
			msg["error_message"] = t.ErrorMessage
		}
		messages = append(messages, msg)
	}

	// Get session for archive path
	session, err := store.Get(r.Context(), sessionID)
	archivePath := ""
	if err == nil && session.ID != "" {
		archivePath = session.RawJSONLPath
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"messages": messages,
		"total":    len(messages),
		"limit":    limit,
		"offset":   offset,
		"path":     archivePath,
	})
}

// handleSessionContextWindows handles GET /api/sessions/{id}/context-windows.
func handleSessionContextWindows(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, sessionID string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	store, err := sessions.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open sessions store")
		httpError(w, http.StatusInternalServerError, "failed to open sessions store")
		return
	}
	defer store.Close()

	windows, err := store.GetContextWindows(r.Context(), sessionID)
	if err != nil {
		log.Error().Err(err).Str("session_id", sessionID).Msg("failed to get context windows")
		httpError(w, http.StatusInternalServerError, "failed to get context windows")
		return
	}

	resp := make([]map[string]any, 0, len(windows))
	for _, cw := range windows {
		w := map[string]any{
			"id":           cw.ID,
			"window_index": cw.WindowIndex,
			"message_count": cw.MessageCount,
			"summary":      cw.Summary,
			"trigger":      cw.Trigger,
		}
		if !cw.StartedAt.IsZero() {
			w["started_at"] = cw.StartedAt.Format("2006-01-02T15:04:05Z07:00")
		}
		if !cw.EndedAt.IsZero() {
			w["ended_at"] = cw.EndedAt.Format("2006-01-02T15:04:05Z07:00")
		}
		resp = append(resp, w)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"context_windows": resp,
		"count":           len(resp),
	})
}

// sessionToResponse converts a storage.Session to SessionResponse.
func sessionToResponse(s storage.Session) SessionResponse {
	sr := SessionResponse{
		ID:              s.ID,
		WorkspacePath:   s.WorkspacePath,
		ProjectName:     s.ProjectName,
		GitBranch:       s.GitBranch,
		ClaudeVersion:   s.ClaudeVersion,
		Summary:         s.Summary,
		Accomplished:    s.Accomplished,
		Decisions:       s.Decisions,
		Gotchas:         s.Gotchas,
		Tags:            s.Tags,
		KeyFiles:        s.KeyFiles,
		ToolsPattern:    s.ToolsPattern,
		MessageCount:    s.MessageCount,
		UserTurns:       s.UserTurns,
		ToolInvocations: s.ToolInvocations,
		TotalTokens:     s.TotalTokens,
		Status:          s.Status,
		AgentID:         s.AgentID,
		ParentSessionID: s.ParentSessionID,
	}
	if !s.StartedAt.IsZero() {
		sr.StartedAt = s.StartedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	if !s.EndedAt.IsZero() {
		sr.EndedAt = s.EndedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return sr
}
