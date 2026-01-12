package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/web/consolews"
)

// ConsoleSessionsHandler returns a handler for GET /api/console/sessions.
// Lists all active console sessions.
func ConsoleSessionsHandler(hub *consolews.Hub, cfg config.Config, log zerolog.Logger) http.HandlerFunc {
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
			filtered := make([]consolews.SessionInfo, 0)
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
// Creates a new console session.
func ConsoleSessionCreateHandler(hub *consolews.Hub, cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var req struct {
			Workspace    string `json:"workspace"`
			Profile      string `json:"profile"`
			SystemPrompt string `json:"system_prompt"`
		}
		if err := readJSON(r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid json")
			return
		}

		if req.Profile == "" {
			req.Profile = "explorer"
		}

		session := hub.CreateSession(consolews.SessionConfig{
			ID:           ulid.Make().String(),
			Workspace:    req.Workspace,
			Profile:      req.Profile,
			SystemPrompt: req.SystemPrompt,
		})

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
func ConsoleSessionDetailHandler(hub *consolews.Hub, cfg config.Config, log zerolog.Logger) http.HandlerFunc {
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
				handleSessionAsk(w, r, session, log)
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

// handleSessionAsk handles POST /api/console/sessions/:id/ask.
func handleSessionAsk(w http.ResponseWriter, r *http.Request, session *consolews.Session, log zerolog.Logger) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Content       string `json:"content"`
		CorrelationID string `json:"correlation_id"`
	}
	if err := readJSON(r, &req); err != nil {
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

	// Create a fake payload and handle it
	payload := consolews.Payload{
		Type:          consolews.PayloadTypeAsk,
		ConsoleID:     session.ID(),
		CorrelationID: req.CorrelationID,
		Content:       req.Content,
	}

	// Handle the payload (async)
	session.HandlePayload(r.Context(), nil, payload)

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

// handleSessionCancel handles POST /api/console/sessions/:id/cancel.
func handleSessionCancel(w http.ResponseWriter, r *http.Request, session *consolews.Session, log zerolog.Logger) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		CorrelationID string `json:"correlation_id"`
	}
	_ = readJSON(r, &req) // Optional body

	// Create cancel command payload
	payload := consolews.Payload{
		Type:      consolews.PayloadTypeCmd,
		ConsoleID: session.ID(),
		Cmd: &consolews.CmdPayload{
			Name:          "cancel",
			CorrelationID: req.CorrelationID,
		},
	}

	session.HandlePayload(r.Context(), nil, payload)

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
func handleSessionMessages(w http.ResponseWriter, r *http.Request, session *consolews.Session, log zerolog.Logger) {
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

// handleSessionEvents handles GET /api/console/sessions/:id/events (SSE).
func handleSessionEvents(w http.ResponseWriter, r *http.Request, session *consolews.Session, log zerolog.Logger) {
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
