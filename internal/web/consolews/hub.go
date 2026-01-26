package consolews

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/oklog/ulid/v2"
)

// getAllowedOrigins returns the list of allowed WebSocket origins.
// Reads from AGENTCTL_WS_ALLOWED_ORIGINS env var (comma-separated).
// Defaults to localhost patterns for development.
func getAllowedOrigins() []string {
	if origins := os.Getenv("AGENTCTL_WS_ALLOWED_ORIGINS"); origins != "" {
		return strings.Split(origins, ",")
	}
	// Default to localhost patterns for development
	return []string{
		"http://localhost:*",
		"http://127.0.0.1:*",
		"https://localhost:*",
		"https://127.0.0.1:*",
	}
}

// RunnerFactory creates a Runner for a new session.
type RunnerFactory func(session *Session) Runner

// Hub manages WebSocket connections for console sessions.
type Hub struct {
	mu            sync.RWMutex
	sessions      map[string]*Session
	persistence   *PersistenceAdapter
	runnerFactory RunnerFactory

	// ctx is the hub's lifecycle context - cancelled on shutdown.
	ctx context.Context
	// wg tracks outstanding persistence goroutines for graceful shutdown.
	wg sync.WaitGroup
}

// NewHub creates a new console WebSocket hub.
func NewHub(ctx context.Context) *Hub {
	return &Hub{
		ctx:      ctx,
		sessions: make(map[string]*Session),
	}
}

// Wait blocks until all outstanding persistence goroutines complete.
// Call this during graceful shutdown after cancelling the hub's context.
func (h *Hub) Wait() {
	h.wg.Wait()
}

// SetPersistence sets the persistence adapter for saving sessions.
func (h *Hub) SetPersistence(p *PersistenceAdapter) {
	h.persistence = p
}

// SetRunnerFactory sets the factory for creating LLM runners for sessions.
func (h *Hub) SetRunnerFactory(f RunnerFactory) {
	h.runnerFactory = f
}

// GetSession returns an existing session by ID.
func (h *Hub) GetSession(id string) *Session {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sessions[id]
}

// CreateSession creates a new console session.
func (h *Hub) CreateSession(cfg SessionConfig) *Session {
	h.mu.Lock()
	defer h.mu.Unlock()

	if cfg.ID == "" {
		cfg.ID = ulid.Make().String()
	}

	session := &Session{
		id:           cfg.ID,
		workspace:    cfg.Workspace,
		profile:      cfg.Profile,
		systemPrompt: cfg.SystemPrompt,
		hub:          h,
		clients:      make(map[*Client]struct{}),
		subscribers:  make(map[chan Payload]struct{}),
		messages:     make([]Message, 0),
		created:      time.Now(),
	}

	h.sessions[cfg.ID] = session

	observability.Emit(h.ctx, observability.NewEvent("consolews.session_created").
		WithComponent("consolews").
		WithSession(cfg.ID, "").
		WithData("workspace", cfg.Workspace).
		Success(0))

	// Set runner if factory is configured
	if h.runnerFactory != nil {
		runner := h.runnerFactory(session)
		if runner != nil {
			session.SetRunner(runner)
		}
	}

	// Persist session if adapter is configured
	if h.persistence != nil {
		persistAsync(h.ctx, &h.wg, "create_session", func(ctx context.Context) error {
			return h.persistence.CreateSession(ctx, session)
		})
	}

	return session
}

// RemoveSession removes a session from the hub.
func (h *Hub) RemoveSession(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if session, ok := h.sessions[id]; ok {
		session.Close()
		delete(h.sessions, id)

		observability.Emit(h.ctx, observability.NewEvent("consolews.session_removed").
			WithComponent("consolews").
			WithSession(id, "").
			Success(0))

		// End persistent session if adapter is configured
		if h.persistence != nil {
			persistAsync(h.ctx, &h.wg, "end_session", func(ctx context.Context) error {
				return h.persistence.EndSession(ctx, id, "ok")
			})
		}
	}
}

// ListSessions returns all active session IDs.
func (h *Hub) ListSessions() []SessionInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	infos := make([]SessionInfo, 0, len(h.sessions))
	for _, s := range h.sessions {
		infos = append(infos, s.Info())
	}
	return infos
}

// SessionInfo contains public session metadata.
type SessionInfo struct {
	ID           string    `json:"id"`
	Workspace    string    `json:"workspace"`
	Profile      string    `json:"profile"`
	Created      time.Time `json:"created"`
	MessageCount int       `json:"message_count"`
	ClientCount  int       `json:"client_count"`
}

// SessionConfig configures a new session.
type SessionConfig struct {
	ID           string
	Workspace    string
	Profile      string
	SystemPrompt string
}

// Client represents a WebSocket client connected to a session.
type Client struct {
	conn    *websocket.Conn
	session *Session
	send    chan []byte
}

// HandleWebSocket handles WebSocket connections at /ws/console/:sessionID.
func HandleWebSocket(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract session ID from path: /ws/console/{sessionID}
		path := r.URL.Path
		const prefix = "/ws/console/"
		if len(path) <= len(prefix) {
			http.Error(w, "missing session ID", http.StatusBadRequest)
			return
		}
		sessionID := path[len(prefix):]

		// Get or create session
		session := hub.GetSession(sessionID)
		if session == nil {
			// Auto-create session with query params
			workspace := r.URL.Query().Get("workspace")
			profile := r.URL.Query().Get("profile")
			if profile == "" {
				profile = "explorer"
			}

			session = hub.CreateSession(SessionConfig{
				ID:        sessionID,
				Workspace: workspace,
				Profile:   profile,
			})
		}

		// Upgrade to WebSocket with origin validation
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: getAllowedOrigins(),
		})
		if err != nil {
			observability.Emit(r.Context(), observability.NewEvent("consolews.accept_failed").
				WithComponent("consolews").
				Error(err, 0))
			return
		}

		client := &Client{
			conn:    conn,
			session: session,
			send:    make(chan []byte, 256),
		}

		session.AddClient(client)

		// Start client goroutines
		go client.writePump(r.Context())
		go client.readPump(r.Context())
	}
}

// readPump reads messages from the WebSocket connection.
func (c *Client) readPump(ctx context.Context) {
	defer func() {
		c.session.RemoveClient(c)
		c.conn.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		var payload Payload
		err := wsjson.Read(ctx, c.conn, &payload)
		if err != nil {
			// Normal closure is expected, don't log
			return
		}

		// Handle the payload
		c.session.HandlePayload(ctx, c, payload)
	}
}

// writePump writes messages to the WebSocket connection.
func (c *Client) writePump(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case message, ok := <-c.send:
			if !ok {
				return
			}
			if err := c.conn.Write(ctx, websocket.MessageText, message); err != nil {
				// Connection closed, exit silently
				return
			}
		case <-ticker.C:
			// Send ping
			if err := c.conn.Ping(ctx); err != nil {
				// Connection closed, exit silently
				return
			}
		}
	}
}

// Send queues a message for sending to the client.
func (c *Client) Send(data []byte) {
	select {
	case c.send <- data:
	default:
		// Buffer full, drop message (best effort)
	}
}

// SendPayload sends a typed payload to the client.
func (c *Client) SendPayload(p Payload) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	c.Send(data)
	return nil
}

// Payload types

// PayloadType identifies the payload type.
type PayloadType string

const (
	PayloadTypeAsk   PayloadType = "ask"
	PayloadTypeCmd   PayloadType = "cmd"
	PayloadTypeEvent PayloadType = "event"
	PayloadTypeReply PayloadType = "reply"
)

// Payload is the base payload structure.
type Payload struct {
	Type          PayloadType    `json:"type"`
	ActorID       string         `json:"actor_id,omitempty"`
	ConsoleID     string         `json:"console_id,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	Content       string         `json:"content,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	Cmd           *CmdPayload    `json:"cmd,omitempty"`
}

// CmdPayload is the command payload.
type CmdPayload struct {
	Name          string `json:"name"`
	CorrelationID string `json:"correlation_id,omitempty"`
}

// Message is a conversation message in the session.
type Message struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	Timestamp  int64          `json:"timestamp"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []any          `json:"tool_calls,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// NewEventPayload creates an event payload.
func NewEventPayload(consoleID, correlationID, content string, metadata map[string]any) Payload {
	return Payload{
		Type:          PayloadTypeEvent,
		ConsoleID:     consoleID,
		CorrelationID: correlationID,
		Content:       content,
		Metadata:      metadata,
	}
}

// NewReplyPayload creates a reply payload.
func NewReplyPayload(consoleID, correlationID, content string) Payload {
	return Payload{
		Type:          PayloadTypeReply,
		ConsoleID:     consoleID,
		CorrelationID: correlationID,
		Content:       content,
	}
}
