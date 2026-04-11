package webterm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/jkatigb/agentctl/internal/gateway/static"
	"github.com/rs/zerolog"
)

// Handler returns HTTP handlers for web terminal routes.
// It provides:
//   - GET /terminal/{room-id} → embedded xterm.js HTML
//   - GET /ws/terminal/{room-id} → WebSocket upgrade to PTY bridge
type Handler struct {
	hub *Hub
	log zerolog.Logger
}

// NewHandler creates a new web terminal handler.
func NewHandler(hub *Hub, log zerolog.Logger) *Handler {
	return &Handler{
		hub: hub,
		log: log.With().Str("component", "webterm-handler").Logger(),
	}
}

// RegisterRoutes registers the web terminal routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/terminal/", h.handleTerminal)
	mux.HandleFunc("/ws/terminal/", h.handleWebSocket)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(static.Assets))))
}

// handleTerminal serves the embedded xterm.js HTML for a room.
// Path pattern: GET /terminal/{room-id}
func (h *Handler) handleTerminal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorJSON(w, http.StatusMethodNotAllowed, "EARG", "method not allowed", "URL path must be GET /terminal/{room-id}")
		return
	}

	roomID := extractRoomID(r.URL.Path, "/terminal/")
	if roomID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "EARG", "missing room ID", "URL path must be /terminal/{room-id}")
		return
	}

	// Check if room is registered
	if !h.hub.HasRoom(roomID) {
		writeErrorJSON(w, http.StatusNotFound, "ENOTFOUND",
			fmt.Sprintf("room not found: %s", roomID),
			fmt.Sprintf("Register room %s before accessing terminal", roomID))
		return
	}

	// Serve the embedded index.html
	data, err := static.Assets.ReadFile("index.html")
	if err != nil {
		h.log.Error().Err(err).Msg("Failed to read embedded index.html")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleWebSocket upgrades a WebSocket connection and bridges it to the room's PTY.
// Path pattern: GET /ws/terminal/{room-id}
func (h *Handler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	roomID := extractRoomID(r.URL.Path, "/ws/terminal/")
	if roomID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "EARG", "missing room ID", "URL path must be /ws/terminal/{room-id}")
		return
	}

	// Check if room is registered
	if !h.hub.HasRoom(roomID) {
		writeErrorJSON(w, http.StatusNotFound, "ENOTFOUND",
			fmt.Sprintf("room not found: %s", roomID),
			fmt.Sprintf("Register room %s before accessing terminal", roomID))
		return
	}

	// Parse initial size from query params (default 80x24)
	cols, rows := parseSizeQuery(r)

	// Upgrade to WebSocket
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: getAllowedOrigins(),
	})
	if err != nil {
		h.log.Error().Err(err).Str("room", roomID).Msg("WebSocket upgrade failed")
		return
	}

	// Check connection limit before creating client
	client := NewClient(conn, h.hub, h.log)

	// Run the client (blocks until disconnect)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	if err := client.Run(ctx, roomID, cols, rows); err != nil {
		switch limitErr := err.(type) {
		case *ConnectionLimitError:
			// Send 429-like message through WebSocket before closing
			errMsg, _ := json.Marshal(map[string]any{
				"type":    "error",
				"code":    "429",
				"message": limitErr.Error(),
			})
			writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
			_ = conn.Write(writeCtx, websocket.MessageText, errMsg)
			writeCancel()
			_ = conn.Close(websocket.StatusPolicyViolation, limitErr.Error())
		case *RoomNotFoundError:
			_ = conn.Close(websocket.StatusInternalError, err.Error())
		default:
			h.log.Debug().Err(err).Str("room", roomID).Msg("Client ended with error")
		}
	}
}

// extractRoomID extracts the room ID from a URL path.
// path must start with prefix, and the room ID is the segment after.
func extractRoomID(path, prefix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	roomID := strings.TrimPrefix(path, prefix)
	// Remove trailing slash
	roomID = strings.TrimRight(roomID, "/")
	// Only take the first path segment
	if idx := strings.Index(roomID, "/"); idx >= 0 {
		roomID = roomID[:idx]
	}
	return roomID
}

// parseSizeQuery parses cols and rows from query parameters.
func parseSizeQuery(r *http.Request) (uint16, uint16) {
	cols := uint16(DefaultInitialCols)
	rows := uint16(DefaultInitialRows)

	if v := r.URL.Query().Get("cols"); v != "" {
		var c int
		if _, err := fmt.Sscanf(v, "%d", &c); err == nil && c > 0 {
			cols = uint16(c)
		}
	}
	if v := r.URL.Query().Get("rows"); v != "" {
		var r2 int
		if _, err := fmt.Sscanf(v, "%d", &r2); err == nil && r2 > 0 {
			rows = uint16(r2)
		}
	}

	return cols, rows
}

// getAllowedOrigins returns the list of allowed WebSocket origins.
func getAllowedOrigins() []string {
	if origins := os.Getenv("AGENTCTL_GATEWAY_WS_ALLOWED_ORIGINS"); origins != "" {
		return strings.Split(origins, ",")
	}
	return []string{
		"http://localhost:*",
		"http://127.0.0.1:*",
		"https://localhost:*",
		"https://127.0.0.1:*",
		"*.ts.net",
		"*.tail*", // tailnet patterns
	}
}

// writeErrorJSON writes a JSON error response.
func writeErrorJSON(w http.ResponseWriter, statusCode int, code, message, hint string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)

	resp := map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	}
	if hint != "" {
		resp["data"] = map[string]string{
			"hint": hint,
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(resp)
}
