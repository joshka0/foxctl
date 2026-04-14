//nolint:forbidigo // REST console handlers still thread zerolog until the wider observability migration is completed.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"

	consolepkg "github.com/joshka0/foxctl/internal/console"
	domainconsole "github.com/joshka0/foxctl/internal/domain/console"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/conversationsettings"
)

// ConsoleSessionsHandler returns a handler for GET /api/console/sessions.
// Lists all active console sessions.
func ConsoleSessionsHandler(hub consolepkg.SessionManager, cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			ConsoleSessionCreateHandler(hub, cfg, log)(w, r)
			return
		}
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Filter by workspace if provided
		workspace := r.URL.Query().Get("workspace")

		sessions := hub.ListSessions()
		if workspace != "" {
			filtered := make([]consolepkg.SessionInfo, 0)
			for _, s := range sessions {
				if s.Workspace == workspace {
					filtered = append(filtered, s)
				}
			}
			sessions = filtered
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"sessions": sessions,
			"count":    len(sessions),
		})
	}
}

// ConsoleSessionCreateHandler returns a handler for POST /api/console/sessions.
// ConsoleSessionCreateHandler returns an http.HandlerFunc that handles POST requests to create a new console session.
// It expects a JSON body with fields `workspace`, `profile`, and `system_prompt` (defaults `profile` to "explorer" when empty),
// creates a session via the provided hub, logs the creation, and responds with HTTP 201 and the session info.
// Responds with HTTP 400 for invalid JSON and HTTP 405 for non-POST methods.
func ConsoleSessionCreateHandler(hub consolepkg.SessionManager, cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var req struct {
			Workspace    string    `json:"workspace"`
			Profile      string    `json:"profile"`
			SystemPrompt string    `json:"system_prompt"`
			LLMProvider  *string   `json:"llm_provider,omitempty"`
			LLMModel     *string   `json:"llm_model,omitempty"`
			ToolsAllow   *[]string `json:"tools_allow,omitempty"`
			ExecMode     *string   `json:"exec_mode,omitempty"`
			// Story model defaults (mostly used by companion story mode, but stored here for consistency)
			StoryGatherModel   *string `json:"story_gather_model,omitempty"`
			StoryDialogueModel *string `json:"story_dialogue_model,omitempty"`
		}
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid json")
			return
		}

		if req.Profile == "" {
			req.Profile = "explorer"
		}

		session := hub.CreateSession(consolepkg.SessionConfig{
			ID:           ulid.Make().String(),
			Workspace:    req.Workspace,
			Profile:      req.Profile,
			SystemPrompt: req.SystemPrompt,
		})

		// Persist optional session settings under conversation_settings (keyed by session ID).
		if req.LLMProvider != nil || req.LLMModel != nil || req.ToolsAllow != nil || req.ExecMode != nil ||
			req.StoryGatherModel != nil || req.StoryDialogueModel != nil {
			settingsStore, err := conversationsettings.Open(r.Context(), cfg.Storage.Root)
			if err != nil {
				hub.RemoveSession(session.ID())
				log.Error().Err(err).Msg("failed to open conversation settings store")
				httpError(w, http.StatusInternalServerError, "failed to open conversation settings store")
				return
			}
			defer settingsStore.Close()

			_, err = settingsStore.Patch(r.Context(), session.ID(), conversationsettings.Patch{
				ToolsAllow:         req.ToolsAllow,
				LLMProvider:        req.LLMProvider,
				LLMModel:           req.LLMModel,
				ExecMode:           req.ExecMode,
				StoryGatherModel:   req.StoryGatherModel,
				StoryDialogueModel: req.StoryDialogueModel,
			})
			if err != nil {
				hub.RemoveSession(session.ID())
				if errors.Is(err, conversationsettings.ErrInvalid) {
					httpError(w, http.StatusBadRequest, err.Error())
					return
				}
				log.Error().Err(err).Msg("failed to persist conversation settings")
				httpError(w, http.StatusInternalServerError, "failed to persist conversation settings")
				return
			}
		}

		log.Info().
			Str("session_id", session.ID()).
			Str("workspace", req.Workspace).
			Str("profile", req.Profile).
			Msg("console session created via REST")

		writeJSON(w, http.StatusCreated, map[string]any{
			"session": session.Info(),
		})
	}
}

// ConsoleSessionDetailHandler returns a handler for GET /api/console/sessions/:id.
func ConsoleSessionDetailHandler(hub consolepkg.SessionManager, cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract session ID from path: /api/console/sessions/{id}
		path := r.URL.Path
		const prefix = "/api/console/sessions/"

		// Handle nested routes
		remaining := strings.TrimPrefix(path, prefix)
		parts := strings.SplitN(remaining, "/", 2)
		sessionID := parts[0]

		if sessionID == "" {
			httpError(w, http.StatusBadRequest, "missing session ID")
			return
		}

		session := hub.GetSession(sessionID)
		if session == nil {
			httpError(w, http.StatusNotFound, "session not found")
			return
		}

		// Check for sub-routes
		if len(parts) > 1 {
			switch parts[1] {
			case "ask":
				handleSessionAsk(w, r, session, cfg, log)
				return
			case "cancel":
				handleSessionCancel(w, r, session, log)
				return
			case "events":
				handleSessionEvents(w, r, session, log)
				return
			case "messages":
				handleSessionMessages(w, r, session, log)
				return
			case "settings":
				handleSessionSettings(w, r, session, cfg, log)
				return
			}
		}

		// Default: return session detail or delete
		if r.Method == http.MethodDelete {
			hub.RemoveSession(sessionID)
			log.Info().Str("session_id", sessionID).Msg("console session deleted")
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":      true,
				"message": "session deleted",
			})
			return
		}
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"session":  session.Info(),
			"messages": session.Messages(),
			"inflight": session.InFlight(),
		})
	}
}

// handleSessionAsk processes an ask request for a console session and queues it for asynchronous handling.
//
// It accepts only POST requests and responds with HTTP 405 for other methods. The request body must be JSON
// containing a non-empty `content` field; malformed JSON or a missing `content` field produce HTTP 400.
// If `correlation_id` is not provided the handler generates one. The request is translated into a console payload
// and dispatched to the session for background processing with a 30-minute timeout; the work is started in the
// background so it can outlive the HTTP request. On success the handler responds with HTTP 202 and a JSON body
// containing the `correlation_id` and a confirmation message.
func handleSessionAsk(w http.ResponseWriter, r *http.Request, session consolepkg.Session, cfg config.Config, log zerolog.Logger) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Content       string `json:"content"`
		CorrelationID string `json:"correlation_id"`
		LLMProvider   string `json:"llm_provider,omitempty"`
		LLMModel      string `json:"llm_model,omitempty"`
	}
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if req.Content == "" {
		httpError(w, http.StatusBadRequest, "content required")
		return
	}

	if req.CorrelationID == "" {
		req.CorrelationID = ulid.Make().String()
	}

	// Resolve per-turn provider/model overrides against stored conversation settings.
	// Precedence: request overrides > conversation settings > engine defaults.
	effectiveProvider := strings.TrimSpace(req.LLMProvider)
	effectiveModel := strings.TrimSpace(req.LLMModel)
	if effectiveProvider == "" || effectiveModel == "" {
		settingsStore, err := conversationsettings.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			// Don't fail the ask if settings can't be loaded; fall back to defaults.
			log.Warn().Err(err).Str("session_id", session.ID()).Msg("failed to open conversation settings store; continuing without settings")
		} else {
			defer settingsStore.Close()
			settings, err := settingsStore.Get(r.Context(), session.ID())
			if err == nil {
				if effectiveProvider == "" {
					effectiveProvider = strings.TrimSpace(settings.LLMProvider)
				}
				if effectiveModel == "" {
					effectiveModel = strings.TrimSpace(settings.LLMModel)
				}
			}
		}
	}

	metadata := make(map[string]any)
	if effectiveProvider != "" {
		metadata["llm_provider"] = effectiveProvider
	}
	if effectiveModel != "" {
		metadata["llm_model"] = effectiveModel
	}

	// Handle the payload (async) - use timeout context to prevent unbounded execution
	// 30 minute timeout for long-running agent tasks
	// Note: We intentionally use context.Background() to let the work outlive this HTTP request.
	// The timeout ensures cleanup even if the session cancel mechanism isn't invoked.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	go func() {
		defer cancel() // Ensure context resources are freed when work completes
		session.HandleAsk(ctx, consolepkg.AskRequest{
			CorrelationID: req.CorrelationID,
			Content:       req.Content,
			Metadata:      metadata,
		})
	}()

	log.Info().
		Str("session_id", session.ID()).
		Str("correlation_id", req.CorrelationID).
		Msg("ask received via REST")

	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":             true,
		"correlation_id": req.CorrelationID,
		"message":        "request queued",
	})
}

// handleSessionCancel handles POST requests to cancel a console session.
// It accepts an optional JSON body with a `correlation_id`, sends a cancel
// command payload to the provided session using the request context, logs the
// cancellation, and responds with a JSON acknowledgement.
func handleSessionCancel(w http.ResponseWriter, r *http.Request, session consolepkg.Session, log zerolog.Logger) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		CorrelationID string `json:"correlation_id"`
	}
	_ = readJSON(w, r, &req) // Optional body

	session.HandleCommand(r.Context(), consolepkg.CommandRequest{
		Name:          domainconsole.CommandCancel,
		CorrelationID: req.CorrelationID,
	})

	log.Info().
		Str("session_id", session.ID()).
		Str("correlation_id", req.CorrelationID).
		Msg("cancel received via REST")

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "cancel requested",
	})
}

// handleSessionMessages handles GET /api/console/sessions/:id/messages.
func handleSessionMessages(w http.ResponseWriter, r *http.Request, session consolepkg.Session, log zerolog.Logger) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	messages := session.Messages()
	writeJSON(w, http.StatusOK, map[string]any{
		"messages": messages,
		"count":    len(messages),
	})
}

// handleSessionSettings handles GET/PATCH /api/console/sessions/:id/settings.
func handleSessionSettings(w http.ResponseWriter, r *http.Request, session consolepkg.Session, cfg config.Config, log zerolog.Logger) {
	store, err := conversationsettings.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open conversation settings store")
		httpError(w, http.StatusInternalServerError, "failed to open conversation settings store")
		return
	}
	defer store.Close()

	sessionID := session.ID()

	switch r.Method {
	case http.MethodGet:
		settings, err := store.Get(r.Context(), sessionID)
		if err != nil {
			if errors.Is(err, conversationsettings.ErrNotFound) {
				settings = conversationsettings.Settings{ConversationID: sessionID}
			} else {
				log.Error().Err(err).Str("session_id", sessionID).Msg("failed to get session settings")
				httpError(w, http.StatusInternalServerError, "failed to get session settings")
				return
			}
		}

		writeJSON(w, http.StatusOK, envelope.OK("session.settings.get", map[string]any{
			"session_id": sessionID,
			"settings":   settings,
		}))

	case http.MethodPatch:
		var patch conversationsettings.Patch
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		settings, err := store.Patch(r.Context(), sessionID, patch)
		if err != nil {
			if errors.Is(err, conversationsettings.ErrInvalid) {
				httpError(w, http.StatusBadRequest, err.Error())
				return
			}
			log.Error().Err(err).Str("session_id", sessionID).Msg("failed to patch session settings")
			httpError(w, http.StatusInternalServerError, "failed to patch session settings")
			return
		}

		writeJSON(w, http.StatusOK, envelope.OK("session.settings.patch", map[string]any{
			"session_id": sessionID,
			"settings":   settings,
		}))

	case http.MethodDelete:
		if err := store.Delete(r.Context(), sessionID); err != nil {
			log.Error().Err(err).Str("session_id", sessionID).Msg("failed to delete session settings")
			httpError(w, http.StatusInternalServerError, "failed to delete session settings")
			return
		}
		writeJSON(w, http.StatusOK, envelope.OK("session.settings.delete", map[string]any{
			"message": "settings deleted",
		}))

	default:
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleSessionEvents handles GET /api/console/sessions/:id/events (SSE).
func handleSessionEvents(w http.ResponseWriter, r *http.Request, session consolepkg.Session, log zerolog.Logger) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Check if SSE is supported
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Determine format
	format := r.URL.Query().Get("format")

	events, unsubscribe := session.Subscribe(64)
	defer unsubscribe()

	// Send initial connection event
	writeSSEEvent(w, "connected", map[string]any{
		"session_id": session.ID(),
		"timestamp":  time.Now().UnixMilli(),
	}, format)
	flusher.Flush()

	// Heartbeat ticker
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			// Send heartbeat
			writeSSEEvent(w, "heartbeat", map[string]any{
				"timestamp": time.Now().UnixMilli(),
			}, format)
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			// Send event
			if format == "payload" {
				// Raw payload format
				data, _ := json.Marshal(event)
				if _, err := w.Write([]byte("data: ")); err != nil {
					return
				}
				if _, err := w.Write(data); err != nil {
					return
				}
				if _, err := w.Write([]byte("\n\n")); err != nil {
					return
				}
			} else {
				// Wrapped format
				writeSSEEvent(w, string(event.Type), event, format)
			}
			flusher.Flush()
		}
	}
}

// writeSSEEvent writes an SSE event.
func writeSSEEvent(w http.ResponseWriter, eventType string, data any, format string) {
	var payload []byte
	if format == "payload" {
		payload, _ = json.Marshal(data)
	} else {
		payload, _ = json.Marshal(map[string]any{
			"type": eventType,
			"data": data,
			"ts":   time.Now().UnixMilli(),
		})
	}

	if _, err := w.Write([]byte("event: " + eventType + "\n")); err != nil {
		return
	}
	if _, err := w.Write([]byte("data: ")); err != nil {
		return
	}
	if _, err := w.Write(payload); err != nil {
		return
	}
	_, _ = w.Write([]byte("\n\n"))
}
