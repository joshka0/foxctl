package console

import (
	"context"
	"time"

	domainconsole "github.com/jkatigb/agentctl/internal/domain/console"
)

// Message is a conversation message in a console session.
type Message struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	Timestamp  int64          `json:"timestamp"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []any          `json:"tool_calls,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// SessionInfo contains public console session metadata.
type SessionInfo struct {
	ID           string    `json:"id"`
	Workspace    string    `json:"workspace"`
	Profile      string    `json:"profile"`
	Created      time.Time `json:"created"`
	MessageCount int       `json:"message_count"`
	ClientCount  int       `json:"client_count"`
}

// SessionConfig configures a console session.
type SessionConfig struct {
	ID           string
	Workspace    string
	Profile      string
	SystemPrompt string
}

// EventType is the console event kind used for internal subscribers.
type EventType = domainconsole.PayloadType

const (
	EventTypeAsk   = domainconsole.PayloadTypeAsk
	EventTypeCmd   = domainconsole.PayloadTypeCmd
	EventTypeEvent = domainconsole.PayloadTypeEvent
	EventTypeReply = domainconsole.PayloadTypeReply
)

// Event is the internal console session event shape used by non-transport
// consumers such as chat bridges and SSE handlers.
type Event struct {
	Type          EventType      `json:"type"`
	ConsoleID     string         `json:"console_id,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	Content       string         `json:"content,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// AskRequest is the internal request shape for a console turn.
type AskRequest struct {
	CorrelationID string
	Content       string
	Metadata      map[string]any
}

// CommandRequest is the internal control-command shape for a console session.
type CommandRequest struct {
	Name          string
	CorrelationID string
}

// Session is the canonical console session contract exposed to non-transport
// consumers such as REST handlers and chat bridges.
type Session interface {
	ID() string
	Info() SessionInfo
	Messages() []Message
	InFlight() string
	Subscribe(buffer int) (<-chan Event, func())
	HandleAsk(ctx context.Context, req AskRequest)
	HandleCommand(ctx context.Context, req CommandRequest)
}

// SessionManager owns console session lifecycle independently of any specific
// transport implementation.
type SessionManager interface {
	GetSession(id string) Session
	CreateSession(cfg SessionConfig) Session
	RemoveSession(id string)
	ListSessions() []SessionInfo
}

// SessionHandle is the minimal runtime-facing console session surface used by
// runners and other non-transport execution code.
type SessionHandle interface {
	ID() string
	Workspace() string
	SystemPrompt() string
	Messages() []Message
	AddMessage(Message)
	InFlightMetadata(correlationID string) map[string]any
	BroadcastEvent(correlationID, content string, metadata map[string]any)
	BroadcastReply(correlationID, content string)
}

// Runner executes console turns against a session runtime handle.
type Runner interface {
	Run(ctx context.Context, session SessionHandle, userMessage string, correlationID string) error
}

// RunnerFactory creates a runner for a newly created console session.
type RunnerFactory func(session SessionHandle) Runner
