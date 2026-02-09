package consolews

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/oklog/ulid/v2"
)

// Session represents an active console chat session.
type Session struct {
	id           string
	workspace    string
	profile      string
	systemPrompt string

	hub         *Hub
	mu          sync.RWMutex
	clients     map[*Client]struct{}
	subMu       sync.RWMutex
	subscribers map[chan Payload]struct{}

	// Conversation state
	messages []Message

	// Cancellation for in-flight requests
	cancelMu sync.Mutex
	cancel   context.CancelFunc
	inflight string // correlation ID of in-flight request
	// inflightMeta stores per-request metadata for the current in-flight correlation_id.
	// This allows REST callers to supply per-turn overrides (e.g., provider/model).
	inflightMeta map[string]any

	// Runner callback (set by consoleapp)
	runner Runner

	created time.Time
}

// Runner executes LLM requests for the console.
type Runner interface {
	// Run executes a user message and streams responses.
	Run(ctx context.Context, session *Session, userMessage string, correlationID string) error
}

// ID returns the session ID.
func (s *Session) ID() string {
	return s.id
}

// Workspace returns the session workspace.
func (s *Session) Workspace() string {
	return s.workspace
}

// Profile returns the session profile.
func (s *Session) Profile() string {
	return s.profile
}

// SystemPrompt returns the session system prompt.
func (s *Session) SystemPrompt() string {
	return s.systemPrompt
}

// SetRunner sets the LLM runner for this session.
func (s *Session) SetRunner(runner Runner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runner = runner
}

// Info returns public session information.
func (s *Session) Info() SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return SessionInfo{
		ID:           s.id,
		Workspace:    s.workspace,
		Profile:      s.profile,
		Created:      s.created,
		MessageCount: len(s.messages),
		ClientCount:  len(s.clients),
	}
}

// Messages returns the conversation history.
func (s *Session) Messages() []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msgs := make([]Message, len(s.messages))
	copy(msgs, s.messages)
	return msgs
}

// AddMessage adds a message to the conversation history.
func (s *Session) AddMessage(msg Message) {
	s.mu.Lock()
	if msg.Timestamp == 0 {
		msg.Timestamp = time.Now().UnixMilli()
	}
	turnIndex := len(s.messages)
	s.messages = append(s.messages, msg)
	s.mu.Unlock()

	// Persist turn if persistence is configured on hub
	if s.hub != nil && s.hub.persistence != nil {
		persistAsync(s.hub.ctx, &s.hub.wg, "save_turn", func(ctx context.Context) error {
			return s.hub.persistence.SaveTurn(ctx, s.id, msg, turnIndex)
		})
	}
}

// AddClient registers a client with the session.
func (s *Session) AddClient(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[c] = struct{}{}
}

// RemoveClient unregisters a client from the session.
func (s *Session) RemoveClient(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.clients[c]; ok {
		delete(s.clients, c)
		close(c.send)
	}
}

// Broadcast sends a payload to all connected clients.
func (s *Session) Broadcast(p Payload) {
	data, err := json.Marshal(p)
	if err != nil {
		observability.Emit(context.Background(), observability.NewEvent("consolews.marshal_failed").
			WithComponent("consolews").
			WithSession(s.id, "").
			Error(err, 0))
		return
	}

	s.mu.RLock()
	for c := range s.clients {
		c.Send(data)
	}
	s.mu.RUnlock()

	s.subMu.RLock()
	for ch := range s.subscribers {
		select {
		case ch <- p:
		default:
			// Subscriber buffer full, best effort
		}
	}
	s.subMu.RUnlock()
}

// BroadcastEvent broadcasts an event to all clients.
func (s *Session) BroadcastEvent(correlationID, content string, metadata map[string]any) {
	s.Broadcast(NewEventPayload(s.id, correlationID, content, metadata))
}

// BroadcastReply broadcasts a reply to all clients.
func (s *Session) BroadcastReply(correlationID, content string) {
	s.Broadcast(NewReplyPayload(s.id, correlationID, content))
}

// HandlePayload processes an incoming payload from a client.
func (s *Session) HandlePayload(ctx context.Context, client *Client, p Payload) {
	switch p.Type {
	case PayloadTypeAsk:
		s.handleAsk(ctx, client, p)
	case PayloadTypeCmd:
		s.handleCmd(ctx, client, p)
	default:
		observability.Emit(ctx, observability.NewEvent("consolews.unknown_payload").
			WithComponent("consolews").
			WithSession(s.id, "").
			WithData("type", string(p.Type)).
			Error(nil, 0))
	}
}

// handleAsk processes an ask payload (user message).
func (s *Session) handleAsk(ctx context.Context, client *Client, p Payload) {
	correlationID := p.CorrelationID
	if correlationID == "" {
		correlationID = ulid.Make().String()
	}

	// Add user message to history
	s.AddMessage(Message{
		Role:    "user",
		Content: p.Content,
	})

	// Check if runner is set
	s.mu.RLock()
	runner := s.runner
	s.mu.RUnlock()

	if runner == nil {
		// No runner - send error reply
		observability.Emit(ctx, observability.NewEvent("consolews.no_runner").
			WithComponent("consolews").
			WithSession(s.id, "").
			WithData("correlation_id", correlationID).
			Error(nil, 0))
		s.BroadcastReply(correlationID, "Console runner not configured. Please ensure the server has an LLM provider configured.")
		return
	}

	// Create cancellable context for this request
	reqCtx, cancel := context.WithCancel(ctx)

	s.cancelMu.Lock()
	// Cancel any previous in-flight request
	if s.cancel != nil {
		s.cancel()
	}
	s.cancel = cancel
	s.inflight = correlationID
	s.inflightMeta = cloneMetadata(p.Metadata)
	s.cancelMu.Unlock()

	// Run in background
	go func() {
		defer func() {
			s.cancelMu.Lock()
			if s.inflight == correlationID {
				s.cancel = nil
				s.inflight = ""
				s.inflightMeta = nil
			}
			s.cancelMu.Unlock()
		}()

		if err := runner.Run(reqCtx, s, p.Content, correlationID); err != nil {
			// Check if the error itself is due to context cancellation
			// (not just if the context happens to be cancelled)
			if errors.Is(err, context.Canceled) {
				s.BroadcastEvent(correlationID, "Request cancelled", map[string]any{"cancelled": true})
			} else {
				observability.Emit(reqCtx, observability.NewEvent("consolews.runner_error").
					WithComponent("consolews").
					WithSession(s.id, "").
					WithData("correlation_id", correlationID).
					WithData("error", err.Error()).
					Error(err, 0))
				s.BroadcastReply(correlationID, "Error: "+err.Error())
			}
		}
	}()
}

// handleCmd processes a command payload.
func (s *Session) handleCmd(ctx context.Context, client *Client, p Payload) {
	if p.Cmd == nil {
		return
	}

	switch p.Cmd.Name {
	case "cancel":
		s.handleCancel(p.Cmd.CorrelationID)
	default:
		observability.Emit(ctx, observability.NewEvent("consolews.unknown_command").
			WithComponent("consolews").
			WithSession(s.id, "").
			WithData("cmd", p.Cmd.Name).
			Error(nil, 0))
	}
}

// handleCancel cancels an in-flight request.
func (s *Session) handleCancel(correlationID string) {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()

	// If correlationID matches or is empty, cancel current request
	if s.cancel != nil && (correlationID == "" || s.inflight == correlationID) {
		s.cancel()
	}
}

// Close closes the session and all client connections.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cancel any in-flight request
	s.cancelMu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.cancelMu.Unlock()

	// Close all client connections
	for c := range s.clients {
		close(c.send)
		delete(s.clients, c)
	}

	s.subMu.Lock()
	for ch := range s.subscribers {
		close(ch)
		delete(s.subscribers, ch)
	}
	s.subMu.Unlock()
}

// InFlight returns the correlation ID of the in-flight request, if any.
func (s *Session) InFlight() string {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	return s.inflight
}

// InFlightMetadata returns a shallow copy of the in-flight request metadata when the
// given correlationID matches the current in-flight request. If correlationID is
// empty, it returns metadata for the current in-flight request.
func (s *Session) InFlightMetadata(correlationID string) map[string]any {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	if correlationID != "" && s.inflight != correlationID {
		return nil
	}
	return cloneMetadata(s.inflightMeta)
}

// Subscribe registers a subscriber channel to receive broadcast payloads.
func (s *Session) Subscribe(buffer int) (<-chan Payload, func()) {
	if buffer <= 0 {
		buffer = 1
	}
	ch := make(chan Payload, buffer)
	s.subMu.Lock()
	if s.subscribers == nil {
		s.subscribers = make(map[chan Payload]struct{})
	}
	s.subscribers[ch] = struct{}{}
	s.subMu.Unlock()

	unsubscribe := func() {
		s.subMu.Lock()
		if _, ok := s.subscribers[ch]; ok {
			delete(s.subscribers, ch)
			close(ch)
		}
		s.subMu.Unlock()
	}
	return ch, unsubscribe
}

func cloneMetadata(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
