package console

// PayloadType defines the type of console message.
type PayloadType string

const (
	// PayloadTypeAsk is a user request expecting a response.
	PayloadTypeAsk PayloadType = "ask"
	// PayloadTypeReply is the final response to an ask.
	PayloadTypeReply PayloadType = "reply"
	// PayloadTypeEvent is a streaming/progress update.
	PayloadTypeEvent PayloadType = "event"
	// PayloadTypeCmd is a control command (e.g., cancel).
	PayloadTypeCmd PayloadType = "cmd"
)

// Payload is the JSON body inside mailbox messages for console I/O.
//
// This is the main envelope for all console communication between
// the agentctl_viewer TUI and actor handlers.
type Payload struct {
	// Type is one of: ask, reply, event, cmd
	Type PayloadType `json:"type"`

	// ActorID is the namespace of the target actor
	ActorID string `json:"actor_id"`

	// ConsoleID is the ULID of the console session
	ConsoleID string `json:"console_id"`

	// CorrelationID is the ULID linking ask to reply/events
	CorrelationID string `json:"correlation_id"`

	// Content is the text message or chunk
	Content string `json:"content"`

	// Metadata provides additional context
	Metadata *Metadata `json:"metadata,omitempty"`

	// Cmd is populated for control commands
	Cmd *Command `json:"cmd,omitempty"`
}

// Metadata provides additional context for console messages.
type Metadata struct {
	// MIME type: text/plain, text/markdown, application/json
	MIME string `json:"mime,omitempty"`

	// Partial indicates this is a streaming chunk, not complete
	Partial bool `json:"partial,omitempty"`

	// ExitCode is set on final reply if a command was run
	ExitCode *int `json:"exit_code,omitempty"`

	// Error contains error details if something went wrong
	Error string `json:"error,omitempty"`

	// Progress provides percentage completion info
	Progress *Progress `json:"progress,omitempty"`

	// Tool is the name of the tool being invoked
	Tool string `json:"tool,omitempty"`

	// CASDigest is set when content references CAS storage
	CASDigest string `json:"cas_digest,omitempty"`
}

// Progress indicates completion status for long-running operations.
type Progress struct {
	// Pct is the percentage complete (0-100)
	Pct int `json:"pct"`

	// Phase describes the current operation phase
	Phase string `json:"phase"`
}

// Command is a control command from console to actor.
type Command struct {
	// Name is the command name (e.g., "cancel")
	Name string `json:"name"`

	// CorrelationID is the correlation to cancel (for cancel command)
	CorrelationID string `json:"correlation_id,omitempty"`
}

// CommandName constants for control commands.
const (
	CommandCancel = "cancel"
)

// NewAskPayload creates a new ask payload.
func NewAskPayload(actorID, consoleID, correlationID, content string) Payload {
	return Payload{
		Type:          PayloadTypeAsk,
		ActorID:       actorID,
		ConsoleID:     consoleID,
		CorrelationID: correlationID,
		Content:       content,
	}
}

// NewReplyPayload creates a new reply payload.
func NewReplyPayload(actorID, consoleID, correlationID, content string) Payload {
	return Payload{
		Type:          PayloadTypeReply,
		ActorID:       actorID,
		ConsoleID:     consoleID,
		CorrelationID: correlationID,
		Content:       content,
	}
}

// NewEventPayload creates a new streaming event payload.
func NewEventPayload(actorID, consoleID, correlationID, content string, partial bool) Payload {
	return Payload{
		Type:          PayloadTypeEvent,
		ActorID:       actorID,
		ConsoleID:     consoleID,
		CorrelationID: correlationID,
		Content:       content,
		Metadata: &Metadata{
			Partial: partial,
		},
	}
}

// NewCancelPayload creates a cancel command payload.
func NewCancelPayload(actorID, consoleID, targetCorrelationID string) Payload {
	return Payload{
		Type:      PayloadTypeCmd,
		ActorID:   actorID,
		ConsoleID: consoleID,
		Cmd: &Command{
			Name:          CommandCancel,
			CorrelationID: targetCorrelationID,
		},
	}
}

// IsComplete returns true if this is a final (non-partial) message.
func (p Payload) IsComplete() bool {
	if p.Type == PayloadTypeReply {
		return true
	}
	if p.Metadata != nil && !p.Metadata.Partial {
		return true
	}
	return false
}

// HasError returns true if this payload contains an error.
func (p Payload) HasError() bool {
	return p.Metadata != nil && p.Metadata.Error != ""
}
