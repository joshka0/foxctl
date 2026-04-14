package api

import (
	"compress/gzip"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/context/sessionkit/claudejsonl"
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
	AgentType       string   `json:"agent_type,omitempty"`
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

	// Get session first to have archive path available
	session, err := store.Get(r.Context(), sessionID)
	if err != nil {
		log.Error().Err(err).Str("session_id", sessionID).Msg("failed to get session")
		httpError(w, http.StatusNotFound, "session not found")
		return
	}
	archivePath := session.RawJSONLPath

	// Get turns (messages) from database
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

	var messages []map[string]any
	var total int

	if len(turns) > 0 {
		// Convert turns to message format matching GUI's SessionMessage interface
		messages = make([]map[string]any, 0, len(turns))
		for _, t := range turns {
			// Map role to type expected by GUI
			msgType := t.Role
			if msgType == "human" {
				msgType = "user"
			}

			msg := map[string]any{
				"index":     t.TurnIndex,
				"type":      msgType,
				"timestamp": t.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
				"uuid":      t.ID,
			}

			// Provide content in format GUI can use
			// GUI checks: msg.summary, msg.message?.content, msg.error, msg.raw
			if t.ContentPreview != "" {
				msg["summary"] = t.ContentPreview
			}

			if t.HasError {
				msg["error"] = t.ErrorMessage
			}

			// Include tool calls as structured data
			if len(t.ToolCalls) > 0 {
				msg["tool_calls"] = t.ToolCalls
			}
			if len(t.FilesTouched) > 0 {
				msg["files_touched"] = t.FilesTouched
			}

			messages = append(messages, msg)
		}
		total = len(messages)
	} else if archivePath != "" {
		// Fallback: read from raw JSONL archive file
		messages, total, err = readMessagesFromArchive(archivePath, offset, limit)
		if err != nil {
			log.Warn().Err(err).Str("path", archivePath).Msg("failed to read archive, returning empty")
			messages = []map[string]any{}
			total = 0
		}
	} else {
		messages = []map[string]any{}
		total = 0
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"messages": messages,
		"total":    total,
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
			"id":            cw.ID,
			"window_index":  cw.WindowIndex,
			"message_count": cw.MessageCount,
			"summary":       cw.Summary,
			"trigger":       cw.Trigger,
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

// readMessagesFromArchive reads messages from a gzipped JSONL archive file.
// Returns messages for the given offset/limit, total count, and any error.
func readMessagesFromArchive(archivePath string, offset, limit int) ([]map[string]any, int, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()

	var reader *claudejsonl.Reader
	if strings.HasSuffix(archivePath, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return nil, 0, err
		}
		defer gzReader.Close()
		reader = claudejsonl.NewReader(gzReader)
	} else {
		reader = claudejsonl.NewReader(file)
	}

	var messages []map[string]any
	index := 0
	collected := 0

	for {
		rm, err := reader.Next()
		if err != nil {
			return nil, 0, err
		}
		if rm == nil {
			break // EOF
		}

		// Apply offset/limit
		if index >= offset && collected < limit {
			msg := convertJSONLMessageToGUIFormat(rm, index)
			messages = append(messages, msg)
			collected++
		}

		index++

		// If we've collected enough and are past the limit, we could stop early
		// but we need total count, so continue to count all messages
	}

	return messages, index, nil
}

// convertJSONLMessageToGUIFormat converts a claudejsonl.ReadMessage to the GUI's expected format.
func convertJSONLMessageToGUIFormat(rm *claudejsonl.ReadMessage, index int) map[string]any {
	m := rm.Message

	// Map type to what GUI expects
	msgType := m.Type
	if msgType == "human" {
		msgType = "user"
	}

	msg := map[string]any{
		"index":     index,
		"type":      msgType,
		"timestamp": m.Timestamp,
	}

	// Include the raw message content for the GUI to process
	// GUI expects message.content to be an array of content blocks
	if len(m.Message) > 0 {
		var nested map[string]any
		if err := json.Unmarshal(m.Message, &nested); err == nil {
			msg["message"] = nested
		}
	}

	// Also include content if present at top level
	if len(m.Content) > 0 {
		var content any
		if err := json.Unmarshal(m.Content, &content); err == nil {
			// If message doesn't have content, wrap it
			if msg["message"] == nil {
				msg["message"] = map[string]any{
					"role":    m.Role,
					"content": content,
				}
			}
		}
	}

	return msg
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
		AgentType:       s.AgentType,
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
