package consolews

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	consolepkg "github.com/joshka0/foxctl/internal/console"
	domainconsole "github.com/joshka0/foxctl/internal/domain/console"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/oklog/ulid/v2"
)

// getAllowedOrigins returns the list of allowed WebSocket origins.
// Reads from FOXCTL_WS_ALLOWED_ORIGINS env var (comma-separated).
// Defaults to localhost patterns for development.
func getAllowedOrigins() []string {
	if origins := os.Getenv("FOXCTL_WS_ALLOWED_ORIGINS"); origins != "" {
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

// Hub manages WebSocket connections for console sessions.
type Hub struct {
	mu            sync.RWMutex
	sessions      map[string]*Session
	persistence   consolepkg.SessionPersistence
	runnerFactory consolepkg.RunnerFactory

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

// SetPersistence sets the console-owned persistence adapter for saving sessions.
func (h *Hub) SetPersistence(p consolepkg.SessionPersistence) {
	h.persistence = p
}

// persistAsync runs a persistence function asynchronously with a timeout.
// The goroutine is tracked by wg for graceful shutdown, and derives its
// context from parentCtx (FC/IS compliant - no context.Background()).
func persistAsync(parentCtx context.Context, wg *sync.WaitGroup, name string, fn func(ctx context.Context) error) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			observability.Emit(ctx, observability.NewEvent("consolews.persistence_failed").
				WithComponent("consolews").
				WithData("op", name).
				Error(err, 0))
		}
	}()
}

// SetRunnerFactory sets the factory for creating LLM runners for sessions.
func (h *Hub) SetRunnerFactory(f consolepkg.RunnerFactory) {
	h.runnerFactory = f
}

func (h *Hub) getSession(id string) *Session {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sessions[id]
}

// GetSession returns an existing session by ID.
func (h *Hub) GetSession(id string) consolepkg.Session {
	session := h.getSession(id)
	if session == nil {
		return nil
	}
	return session
}

func (h *Hub) createSession(cfg consolepkg.SessionConfig) *Session {
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
		subscribers:  make(map[chan consolepkg.Event]struct{}),
		messages:     make([]consolepkg.Message, 0),
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

// CreateSession creates a new console session.
func (h *Hub) CreateSession(cfg consolepkg.SessionConfig) consolepkg.Session {
	return h.createSession(cfg)
}

func (h *Hub) removeSession(id string) {
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

// RemoveSession removes a session from the hub.
func (h *Hub) RemoveSession(id string) {
	h.removeSession(id)
}

// ListSessions returns all active session IDs.
func (h *Hub) ListSessions() []consolepkg.SessionInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	infos := make([]consolepkg.SessionInfo, 0, len(h.sessions))
	for _, s := range h.sessions {
		infos = append(infos, s.Info())
	}
	return infos
}

// Client represents a WebSocket client connected to a session.
type Client struct {
	conn    *websocket.Conn
	session *Session
	send    chan []byte
}

// HandleWebSocket handles WebSocket connections at /ws/console/:sessionID.
//
// Index:
// - Purpose: Accept console websocket connections and attach them to sessions
// - Flow: parse session ID → get/create session → upgrade to websocket → spawn pumps
// - SideEffects: upgrades HTTP to WebSocket; spawns goroutines
// - FailureModes: invalid session ID, websocket accept failures
// - Observability: emits consolews.accept_failed
// - Related: Hub.GetSession, Hub.CreateSession, Client.readPump
// - Keywords: consolews, websocket, session_id, accept_failed
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
		session := hub.getSession(sessionID)
		if session == nil {
			// Auto-create session with query params
			workspace := r.URL.Query().Get("workspace")
			profile := r.URL.Query().Get("profile")
			if profile == "" {
				profile = "explorer"
			}

			session = hub.createSession(consolepkg.SessionConfig{
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
		var payload domainconsole.Payload
		err := wsjson.Read(ctx, c.conn, &payload)
		if err != nil {
			// Normal closure is expected, don't log
			return
		}

		switch payload.Type {
		case domainconsole.PayloadTypeAsk:
			c.session.HandleAsk(ctx, consolepkg.AskRequest{
				CorrelationID: payload.CorrelationID,
				Content:       payload.Content,
				Metadata:      payload.Metadata,
			})
		case domainconsole.PayloadTypeCmd:
			if payload.Cmd == nil {
				continue
			}
			c.session.HandleCommand(ctx, consolepkg.CommandRequest{
				Name:          payload.Cmd.Name,
				CorrelationID: payload.Cmd.CorrelationID,
			})
		default:
			observability.Emit(ctx, observability.NewEvent("consolews.unknown_payload").
				WithComponent("consolews").
				WithSession(c.session.id, "").
				WithData("type", string(payload.Type)).
				Error(nil, 0))
		}
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
