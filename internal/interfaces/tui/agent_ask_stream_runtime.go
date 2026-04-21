package tui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const defaultAgentAskStreamBufferSize = 16

// ---------------------------------------------------------------------------
// AgentAskCanceler — submits cancel requests for in-flight ask-streams.
// ---------------------------------------------------------------------------

// AgentAskCanceler submits a cancel request for an agent's ask-stream.
type AgentAskCanceler interface {
	CancelAsk(ctx context.Context, agentID string) error
}

// AgentAskCancelerFunc adapts a function into an AgentAskCanceler.
type AgentAskCancelerFunc func(ctx context.Context, agentID string) error

// CancelAsk implements AgentAskCanceler.
func (fn AgentAskCancelerFunc) CancelAsk(ctx context.Context, agentID string) error {
	if fn == nil {
		return errors.New("agent ask canceler function is required")
	}
	return fn(ctx, agentID)
}

// ---------------------------------------------------------------------------
// AgentAskStreamEvent — normalized SSE event from the daemon's ask-stream.
// ---------------------------------------------------------------------------

// AgentAskStreamEvent is a normalized event from POST /api/agents/{id}/ask-stream.
type AgentAskStreamEvent struct {
	Phase          string         `json:"phase"`
	AgentID        string         `json:"agent_id,omitempty"`
	ConversationID string         `json:"conversation_id,omitempty"`
	CorrelationID  string         `json:"correlation_id,omitempty"`
	Content        string         `json:"content,omitempty"`
	ContentDelta   string         `json:"content_delta,omitempty"`
	ToolName       string         `json:"tool_name,omitempty"`
	ToolCallID     string         `json:"tool_call_id,omitempty"`
	ToolArguments  any            `json:"tool_arguments,omitempty"`
	ToolOutput     string         `json:"tool_output,omitempty"`
	Error          string         `json:"error,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// ---------------------------------------------------------------------------
// AgentAskStreamSource — reads agent ask-stream events.
// ---------------------------------------------------------------------------

// AgentAskStreamSource reads agent ask-stream events and invokes onEvent for each.
type AgentAskStreamSource interface {
	Stream(ctx context.Context, onEvent func(AgentAskStreamEvent) error) error
}

// AgentAskStreamSourceFunc adapts a function into an AgentAskStreamSource.
type AgentAskStreamSourceFunc func(ctx context.Context, onEvent func(AgentAskStreamEvent) error) error

// Stream implements AgentAskStreamSource.
func (fn AgentAskStreamSourceFunc) Stream(ctx context.Context, onEvent func(AgentAskStreamEvent) error) error {
	if fn == nil {
		return errors.New("agent ask stream source function is required")
	}
	return fn(ctx, onEvent)
}

// ---------------------------------------------------------------------------
// HTTPAgentAskStreamSource — consumes the daemon's SSE ask-stream.
// ---------------------------------------------------------------------------

// NewHTTPAgentAskStreamSource creates a source that POSTs to
// /api/agents/{id}/ask-stream and then reads the SSE response body.
func NewHTTPAgentAskStreamSource(client *APIClient, agentID string) AgentAskStreamSource {
	return AgentAskStreamSourceFunc(func(ctx context.Context, onEvent func(AgentAskStreamEvent) error) error {
		if client == nil {
			return errors.New("api client is required")
		}
		agentID = strings.TrimSpace(agentID)
		if agentID == "" {
			return errors.New("agent id is required")
		}

		path := "/api/agents/" + url.PathEscape(agentID) + "/ask-stream"
		reqURL, err := client.endpointURL(path)
		if err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(`{"message":"stream"}`))
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return &HTTPStatusError{
				Method:     http.MethodPost,
				URL:        reqURL,
				StatusCode: resp.StatusCode,
				Status:     resp.Status,
			}
		}

		return parseAgentAskEventStream(resp.Body, onEvent)
	})
}

// parseAgentAskEventStream reads an SSE stream and emits normalized agent ask events.
// Unrecognised or unparseable events are emitted as malformed events rather than
// silently dropped, so the UI can surface a visible indicator.
func parseAgentAskEventStream(r io.Reader, onEvent func(AgentAskStreamEvent) error) error {
	return ParseConsoleEventStream(r, func(event ConsoleStreamEvent) error {
		askEvent := mapConsoleStreamEventToAgentAsk(event)
		return onEvent(askEvent)
	})
}

// mapConsoleStreamEventToAgentAsk converts a ConsoleStreamEvent (from SSE) to an
// AgentAskStreamEvent.  Unrecognised event types and unparseable payloads are
// returned with Phase == "malformed" so the runtime can emit a visible indicator
// instead of silently dropping the frame.
func mapConsoleStreamEventToAgentAsk(event ConsoleStreamEvent) AgentAskStreamEvent {
	if event.Payload != nil {
		payload := event.Payload
		phase := normalizeStreamType(payload.Type)
		if phase != "" && isKnownAskPhase(phase) {
			return AgentAskStreamEvent{
				Phase:         phase,
				CorrelationID: payload.CorrelationID,
				Content:       payload.Content,
			}
		}
		// Payload present but phase is unknown → malformed.
		return AgentAskStreamEvent{
			Phase:   "malformed",
			Content: payload.Type,
		}
	}

	// Try to decode from raw Data if Payload is missing.
	if len(event.Data) > 0 {
		var ask AgentAskStreamEvent
		if err := json.Unmarshal(event.Data, &ask); err == nil {
			ask.Phase = normalizeStreamType(ask.Phase)
			if ask.Phase == "" || isKnownAskPhase(ask.Phase) {
				return ask
			}
			// Parsed OK but phase is unknown → malformed.
			return AgentAskStreamEvent{
				Phase:   "malformed",
				Content: ask.Phase,
			}
		}
		// Invalid JSON → malformed.
		return AgentAskStreamEvent{
			Phase:   "malformed",
			Content: string(event.Data),
		}
	}

	// Empty event → malformed (should be rare, but safe).
	return AgentAskStreamEvent{
		Phase: "malformed",
	}
}

// isKnownAskPhase reports whether phase is a recognised ask-stream phase.
func isKnownAskPhase(phase string) bool {
	switch phase {
	case "started", "delta", "tool_call", "tool_result", "completed", "error", "cancelled":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// AgentAskStreamUpdate — typed updates emitted by the runtime.
// ---------------------------------------------------------------------------

// AgentAskUpdateType defines the kind of ask-stream update.
type AgentAskUpdateType string

const (
	AgentAskUpdateStarted    AgentAskUpdateType = "started"
	AgentAskUpdateToken      AgentAskUpdateType = "token"
	AgentAskUpdateToolCall   AgentAskUpdateType = "tool_call"
	AgentAskUpdateToolResult AgentAskUpdateType = "tool_result"
	AgentAskUpdateDone       AgentAskUpdateType = "done"
	AgentAskUpdateError      AgentAskUpdateType = "error"
	AgentAskUpdateCancelled  AgentAskUpdateType = "cancelled"
	AgentAskUpdateRejected   AgentAskUpdateType = "rejected"
	AgentAskUpdateMalformed  AgentAskUpdateType = "malformed"
)

// AgentAskStreamUpdate is a typed notification for ask-stream consumers.
type AgentAskStreamUpdate struct {
	Type       AgentAskUpdateType
	Started    *AgentAskStarted
	Token      *AgentAskToken
	ToolCall   *AgentAskToolCall
	ToolResult *AgentAskToolResult
	Done       *AgentAskDone
	Error      *AgentAskError
	Cancelled  *AgentAskCancelled
	Rejected   *AgentAskRejected
	Malformed  *AgentAskMalformed
}

// AgentAskStarted signals the stream has begun.
type AgentAskStarted struct {
	CorrelationID string
}

// AgentAskToken carries one delta token.
type AgentAskToken struct {
	Delta string
}

// AgentAskToolCall signals a tool invocation.
type AgentAskToolCall struct {
	ToolName      string
	ToolCallID    string
	ToolArguments any
}

// AgentAskToolResult signals a tool invocation result.
type AgentAskToolResult struct {
	ToolCallID string
	ToolName   string
	Output     string
}

// AgentAskDone signals successful stream completion.
type AgentAskDone struct {
	OK bool
}

// AgentAskError signals a stream-level error.
type AgentAskError struct {
	Err error
}

// AgentAskRejected signals a double-submit rejection.
type AgentAskRejected struct {
	Reason string
}

// AgentAskCancelled signals the stream was cancelled by the user.
type AgentAskCancelled struct{}

// AgentAskMalformed signals an unrecognised or malformed SSE event.
type AgentAskMalformed struct {
	RawPhase string
	RawData  string
}

// ---------------------------------------------------------------------------
// AgentAskStreamRuntime — source-driven runtime for agent ask-streams.
// ---------------------------------------------------------------------------

// AgentAskStreamRuntime owns one stream-reading goroutine and emits bounded
// typed updates. It guards against double-submit: only one stream may be
// in flight at a time.
type AgentAskStreamRuntime struct {
	source   AgentAskStreamSource
	canceler AgentAskCanceler
	updates  chan AgentAskStreamUpdate

	parentCtx    context.Context
	parentCancel context.CancelFunc

	stopOnce  sync.Once
	waitGroup sync.WaitGroup

	mu           sync.Mutex
	inFlight     bool
	agentID      string
	message      string
	streamCancel context.CancelFunc // cancel func for the current stream
}

// NewAgentAskStreamRuntime creates and starts a bounded agent ask-stream runtime.
func NewAgentAskStreamRuntime(
	parent context.Context,
	source AgentAskStreamSource,
	bufferSize int,
) (*AgentAskStreamRuntime, error) {
	if source == nil {
		return nil, errors.New("agent ask stream source is required")
	}
	if parent == nil {
		parent = context.Background()
	}
	if bufferSize < 0 {
		return nil, errors.New("buffer size must be >= 0")
	}
	if bufferSize == 0 {
		bufferSize = defaultAgentAskStreamBufferSize
	}

	ctx, cancel := context.WithCancel(parent)
	rt := &AgentAskStreamRuntime{
		source:       source,
		updates:      make(chan AgentAskStreamUpdate, bufferSize),
		parentCtx:    ctx,
		parentCancel: cancel,
	}
	return rt, nil
}

// SetCanceler sets the canceler used by Cancel(). Call before Submit.
func (rt *AgentAskStreamRuntime) SetCanceler(canceler AgentAskCanceler) {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.canceler = canceler
}

// Submit starts a new ask-stream for the given agent and message.
// Returns an error if a stream is already in flight (double-submit guard).
func (rt *AgentAskStreamRuntime) Submit(agentID, message string) error {
	if rt == nil {
		return errors.New("agent ask stream runtime is required")
	}

	rt.mu.Lock()
	if rt.inFlight {
		rt.mu.Unlock()
		return errors.New("stream in flight — press Ctrl+X to cancel first")
	}
	rt.inFlight = true
	rt.agentID = strings.TrimSpace(agentID)
	rt.message = strings.TrimSpace(message)
	// Recreate the updates channel for each new stream so consumers get a
	// fresh channel and the previous stream's close doesn't interfere.
	rt.updates = make(chan AgentAskStreamUpdate, cap(rt.updates))
	streamCtx, streamCancel := context.WithCancel(rt.parentCtx)
	rt.streamCancel = streamCancel
	rt.mu.Unlock()

	rt.waitGroup.Add(1)
	go rt.run(streamCtx)
	return nil
}

// Cancel cancels the in-flight ask-stream by calling the cancel endpoint
// and cancelling the request context. Returns an error if no stream is
// in flight.
func (rt *AgentAskStreamRuntime) Cancel() error {
	if rt == nil {
		return errors.New("agent ask stream runtime is required")
	}

	rt.mu.Lock()
	if !rt.inFlight {
		rt.mu.Unlock()
		return errors.New("no stream in flight")
	}
	agentID := rt.agentID
	canceler := rt.canceler
	rt.mu.Unlock()

	// Call the cancel endpoint first (best effort).
	if canceler != nil {
		// Use a short timeout for the cancel request so it doesn't block.
		cancelCtx, cancel := context.WithTimeout(rt.parentCtx, 5*time.Second)
		_ = canceler.CancelAsk(cancelCtx, agentID)
		cancel()
	}

	// Cancel the stream context to abort the in-flight HTTP request.
	rt.mu.Lock()
	streamCancel := rt.streamCancel
	rt.mu.Unlock()
	if streamCancel != nil {
		streamCancel()
	}
	return nil
}

// Updates returns the bounded receive-only update channel.
func (rt *AgentAskStreamRuntime) Updates() <-chan AgentAskStreamUpdate {
	if rt == nil {
		return nil
	}
	return rt.updates
}

// Stop cancels stream reading and waits for the goroutine to exit.
func (rt *AgentAskStreamRuntime) Stop() {
	if rt == nil {
		return
	}
	rt.stopOnce.Do(func() {
		rt.parentCancel()
		rt.waitGroup.Wait()
	})
}

// Close is an alias for Stop.
func (rt *AgentAskStreamRuntime) Close() {
	rt.Stop()
}

// IsInFlight reports whether a stream is currently active.
func (rt *AgentAskStreamRuntime) IsInFlight() bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.inFlight
}

func (rt *AgentAskStreamRuntime) run(streamCtx context.Context) {
	defer rt.waitGroup.Done()
	defer close(rt.updates)
	defer func() {
		rt.mu.Lock()
		rt.inFlight = false
		rt.streamCancel = nil
		rt.mu.Unlock()
	}()

	streamErr := rt.source.Stream(streamCtx, func(event AgentAskStreamEvent) error {
		update := rt.mapEventToUpdate(event)
		if update == nil {
			return nil
		}
		return rt.sendUpdate(*update)
	})

	if streamErr != nil {
		if errors.Is(streamErr, context.Canceled) {
			rt.sendTerminalUpdate(AgentAskStreamUpdate{
				Type:      AgentAskUpdateCancelled,
				Cancelled: &AgentAskCancelled{},
			})
			return
		}
		if !errors.Is(streamErr, context.DeadlineExceeded) {
			rt.sendTerminalUpdate(AgentAskStreamUpdate{
				Type:  AgentAskUpdateError,
				Error: &AgentAskError{Err: streamErr},
			})
			return
		}
	}

	rt.sendTerminalUpdate(AgentAskStreamUpdate{
		Type: AgentAskUpdateDone,
		Done: &AgentAskDone{OK: true},
	})
}

func (rt *AgentAskStreamRuntime) mapEventToUpdate(event AgentAskStreamEvent) *AgentAskStreamUpdate {
	switch event.Phase {
	case "started":
		return &AgentAskStreamUpdate{
			Type:    AgentAskUpdateStarted,
			Started: &AgentAskStarted{CorrelationID: event.CorrelationID},
		}
	case "delta":
		return &AgentAskStreamUpdate{
			Type:  AgentAskUpdateToken,
			Token: &AgentAskToken{Delta: event.ContentDelta},
		}
	case "tool_call":
		return &AgentAskStreamUpdate{
			Type:     AgentAskUpdateToolCall,
			ToolCall: &AgentAskToolCall{ToolName: event.ToolName, ToolCallID: event.ToolCallID, ToolArguments: event.ToolArguments},
		}
	case "tool_result":
		return &AgentAskStreamUpdate{
			Type:       AgentAskUpdateToolResult,
			ToolResult: &AgentAskToolResult{ToolCallID: event.ToolCallID, ToolName: event.ToolName, Output: event.ToolOutput},
		}
	case "completed":
		return nil // terminal done is emitted after Stream returns.
	case "error":
		return &AgentAskStreamUpdate{
			Type:  AgentAskUpdateError,
			Error: &AgentAskError{Err: errors.New(event.Error)},
		}
	case "cancelled":
		return &AgentAskStreamUpdate{
			Type:      AgentAskUpdateCancelled,
			Cancelled: &AgentAskCancelled{},
		}
	case "malformed":
		return &AgentAskStreamUpdate{
			Type:      AgentAskUpdateMalformed,
			Malformed: &AgentAskMalformed{RawPhase: event.Phase, RawData: event.Content},
		}
	default:
		// Unknown phase → emit as malformed so the UI surfaces a visible
		// indicator instead of silently dropping the frame.
		return &AgentAskStreamUpdate{
			Type:      AgentAskUpdateMalformed,
			Malformed: &AgentAskMalformed{RawPhase: event.Phase, RawData: event.Content},
		}
	}
}

func (rt *AgentAskStreamRuntime) sendUpdate(update AgentAskStreamUpdate) error {
	select {
	case <-rt.parentCtx.Done():
		return rt.parentCtx.Err()
	case rt.updates <- update:
		return nil
	}
}

func (rt *AgentAskStreamRuntime) sendTerminalUpdate(update AgentAskStreamUpdate) {
	select {
	case <-rt.parentCtx.Done():
		return
	case rt.updates <- update:
		return
	}
}
