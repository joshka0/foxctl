package agent

import (
	"encoding/json"
	"fmt"
	"time"
)

// Builder provides a fluent API for constructing messages.
//
// Example:
//
//	msg := builder.NewAgentAsk().
//	    FromNS("agent:a").
//	    ToNS("agent:b").
//	    Question("What is the status?").
//	    WithSessionID(sessionID).
//	    WithWorkspace(workspace).
//	    Build()
type Builder struct {
	message *Message
	data    any
}

// NewAgentAsk creates a builder for an agent.ask message.
func NewAgentAsk() *Builder {
	return &Builder{
		message: &Message{
			Type:    MessageTypeAsk,
			TTLMS:   defaultTTLMS(),
			Attempt: 0,
		},
		data: &AskData{},
	}
}

// NewAgentReply creates a builder for an agent.reply message.
func NewAgentReply() *Builder {
	return &Builder{
		message: &Message{
			Type:    MessageTypeReply,
			TTLMS:   defaultTTLMS(),
			Attempt: 0,
		},
		data: &ReplyData{},
	}
}

// NewAgentCmd creates a builder for an agent.cmd message.
func NewAgentCmd() *Builder {
	return &Builder{
		message: &Message{
			Type:    MessageTypeCmd,
			TTLMS:   defaultTTLMS(),
			Attempt: 0,
		},
		data: &CmdData{},
	}
}

// NewAgentEvent creates a builder for an agent.event message.
func NewAgentEvent() *Builder {
	return &Builder{
		message: &Message{
			Type:    MessageTypeEvent,
			TTLMS:   defaultTTLMS(),
			Attempt: 0,
		},
		data: &EventData{},
	}
}

// NewConsoleAsk creates a builder for a console.ask message.
func NewConsoleAsk() *Builder {
	return &Builder{
		message: &Message{
			Type:    MessageTypeConsoleAsk,
			TTLMS:   defaultTTLMS(),
			Attempt: 0,
		},
		data: &ConsoleAskData{},
	}
}

// NewConsoleReply creates a builder for a console.reply message.
func NewConsoleReply() *Builder {
	return &Builder{
		message: &Message{
			Type:    MessageTypeConsoleReply,
			TTLMS:   defaultTTLMS(),
			Attempt: 0,
		},
		data: &ConsoleReplyData{},
	}
}

// NewConsoleEvent creates a builder for a console.event message.
func NewConsoleEvent() *Builder {
	return &Builder{
		message: &Message{
			Type:    MessageTypeConsoleEvent,
			TTLMS:   defaultTTLMS(),
			Attempt: 0,
		},
		data: &ConsoleEventData{},
	}
}

// WithID sets the message ID.
func (b *Builder) WithID(id string) *Builder {
	b.message.ID = id
	return b
}

// WithTimestamp sets the message timestamp from a time.Time and VisibleAt.
func (b *Builder) WithTimestamp(t time.Time) *Builder {
	return b.WithTimestampUnix(t.Unix())
}

// WithTimestampUnix sets the message timestamp (Unix seconds) and VisibleAt.
func (b *Builder) WithTimestampUnix(ts int64) *Builder {
	b.message.Timestamp = ts
	if b.message.VisibleAt == 0 {
		b.message.VisibleAt = ts
	}
	return b
}

// FromNS sets the source namespace.
func (b *Builder) FromNS(ns string) *Builder {
	b.message.FromNS = ns
	return b
}

// ToNS sets the destination namespace.
func (b *Builder) ToNS(ns string) *Builder {
	b.message.ToNS = ns
	return b
}

// WithSessionID sets the session ID.
func (b *Builder) WithSessionID(sessionID string) *Builder {
	b.message.SessionID = sessionID
	return b
}

// WithWorkspace sets the workspace path.
func (b *Builder) WithWorkspace(workspace string) *Builder {
	b.message.Workspace = workspace
	return b
}

// WithAgentID sets the agent ID.
func (b *Builder) WithAgentID(agentID string) *Builder {
	b.message.AgentID = agentID
	return b
}

// WithTTL sets the time-to-live for the message.
func (b *Builder) WithTTL(ttl time.Duration) *Builder {
	b.message.TTLMS = int64(ttl.Milliseconds())
	return b
}

// WithHeader adds a header to the message.
func (b *Builder) WithHeader(key, value string) *Builder {
	if b.message.Headers == nil {
		b.message.Headers = make(map[string]string)
	}
	b.message.Headers[key] = value
	return b
}

// WithHeaders sets all headers.
func (b *Builder) WithHeaders(headers map[string]string) *Builder {
	b.message.Headers = headers
	return b
}

// AskData methods

// AskID sets the AskID field (for Ask, Reply, ConsoleAsk, ConsoleReply, and ConsoleEvent messages).
func (b *Builder) AskID(askID string) *Builder {
	if ask, ok := b.data.(*AskData); ok {
		ask.AskID = askID
	}
	if reply, ok := b.data.(*ReplyData); ok {
		reply.AskID = askID
	}
	if consoleAsk, ok := b.data.(*ConsoleAskData); ok {
		consoleAsk.AskID = askID
	}
	if consoleReply, ok := b.data.(*ConsoleReplyData); ok {
		consoleReply.AskID = askID
	}
	if consoleEvent, ok := b.data.(*ConsoleEventData); ok {
		consoleEvent.AskID = askID
	}
	return b
}

// Question sets the Question field (for Ask messages).
func (b *Builder) Question(question string) *Builder {
	if ask, ok := b.data.(*AskData); ok {
		ask.Question = question
	}
	return b
}

// WithKind sets the Kind field (for Ask messages).
func (b *Builder) WithKind(kind string) *Builder {
	if ask, ok := b.data.(*AskData); ok {
		ask.Kind = kind
	}
	return b
}

// WithNeedsBy sets the NeedsBy field (for Ask messages).
func (b *Builder) WithNeedsBy(deadline time.Time) *Builder {
	if ask, ok := b.data.(*AskData); ok {
		ask.NeedsByMS = deadline.UnixMilli()
	}
	return b
}

// WithContext sets the Context field (for Ask and ConsoleAsk messages).
func (b *Builder) WithContext(ctx map[string]any) *Builder {
	if ask, ok := b.data.(*AskData); ok {
		ask.Context = ctx
	}
	if consoleAsk, ok := b.data.(*ConsoleAskData); ok {
		consoleAsk.Context = ctx
	}
	return b
}

// Answer sets the Answer field (for Reply messages).
func (b *Builder) Answer(answer map[string]any) *Builder {
	if reply, ok := b.data.(*ReplyData); ok {
		reply.Answer = answer
	}
	return b
}

// CmdData methods

// CmdID sets the CmdID field (for Cmd messages).
func (b *Builder) CmdID(cmdID string) *Builder {
	if cmd, ok := b.data.(*CmdData); ok {
		cmd.CmdID = cmdID
	}
	return b
}

// Action sets the Action field (for Cmd messages).
func (b *Builder) Action(action string) *Builder {
	if cmd, ok := b.data.(*CmdData); ok {
		cmd.Action = action
	}
	if consoleCmd, ok := b.data.(*ConsoleCmdData); ok {
		consoleCmd.Action = action
	}
	return b
}

// WithSkill sets the Skill field (for Cmd messages).
func (b *Builder) WithSkill(skill string) *Builder {
	if cmd, ok := b.data.(*CmdData); ok {
		cmd.Skill = skill
	}
	return b
}

// WithArgs sets the Args field (for Cmd messages).
func (b *Builder) WithArgs(args map[string]any) *Builder {
	if cmd, ok := b.data.(*CmdData); ok {
		cmd.Args = args
	}
	return b
}

// EventData methods

// EventID sets the EventID field (for Event messages).
func (b *Builder) EventID(eventID string) *Builder {
	if event, ok := b.data.(*EventData); ok {
		event.EventID = eventID
	}
	return b
}

// EventKind sets the Kind field (for Event messages).
func (b *Builder) EventKind(kind string) *Builder {
	if event, ok := b.data.(*EventData); ok {
		event.Kind = kind
	}
	if consoleEvent, ok := b.data.(*ConsoleEventData); ok {
		consoleEvent.Kind = kind
	}
	return b
}

// WithJobCount sets the JobCount field (for Event messages).
func (b *Builder) WithJobCount(count int) *Builder {
	if event, ok := b.data.(*EventData); ok {
		event.JobCount = count
	}
	return b
}

// WithCustomData sets the Custom field (for Event messages).
func (b *Builder) WithCustomData(custom map[string]any) *Builder {
	if event, ok := b.data.(*EventData); ok {
		event.Custom = custom
	}
	return b
}

// ConsoleAskData methods

// Prompt sets the Prompt field (for ConsoleAsk messages).
func (b *Builder) Prompt(prompt string) *Builder {
	if ask, ok := b.data.(*ConsoleAskData); ok {
		ask.Prompt = prompt
	}
	return b
}

// ConsoleID sets the ConsoleID field (for ConsoleAsk messages).
func (b *Builder) ConsoleID(consoleID string) *Builder {
	if ask, ok := b.data.(*ConsoleAskData); ok {
		ask.ConsoleID = consoleID
	}
	return b
}

// ConsoleReplyData methods

// Response sets the Response field (for ConsoleReply messages).
func (b *Builder) Response(response string) *Builder {
	if reply, ok := b.data.(*ConsoleReplyData); ok {
		reply.Response = response
	}
	return b
}

// Status sets the Status field (for ConsoleReply messages).
func (b *Builder) Status(status string) *Builder {
	if reply, ok := b.data.(*ConsoleReplyData); ok {
		reply.Status = status
	}
	return b
}

// ConsoleEventData methods

// Content sets the Content field (for ConsoleEvent messages).
func (b *Builder) Content(content string) *Builder {
	if event, ok := b.data.(*ConsoleEventData); ok {
		event.Content = content
	}
	return b
}

// Seq sets the Seq field (for ConsoleEvent messages).
func (b *Builder) Seq(seq int) *Builder {
	if event, ok := b.data.(*ConsoleEventData); ok {
		event.Seq = seq
	}
	return b
}

// Iteration sets the Iteration field (for ConsoleEvent messages).
func (b *Builder) Iteration(iteration int) *Builder {
	if event, ok := b.data.(*ConsoleEventData); ok {
		event.Iteration = iteration
	}
	return b
}

// ToolName sets the ToolName field (for ConsoleEvent messages).
func (b *Builder) ToolName(toolName string) *Builder {
	if event, ok := b.data.(*ConsoleEventData); ok {
		event.ToolName = toolName
	}
	return b
}

// Build constructs the final Message.
func (b *Builder) Build() (Message, error) {
	if b.message.ID == "" {
		return Message{}, fmt.Errorf("message id is required")
	}
	if b.message.Timestamp == 0 {
		return Message{}, fmt.Errorf("message timestamp is required")
	}
	if b.message.VisibleAt == 0 {
		b.message.VisibleAt = b.message.Timestamp
	}

	// Auto-populate primary IDs for message types that have them
	// Note: Reply types (ReplyData, ConsoleReplyData, ConsoleEventData) don't auto-populate
	// AskID because it should only be set when explicitly correlating with an Ask
	if ask, ok := b.data.(*AskData); ok && ask.AskID == "" {
		ask.AskID = b.message.ID
	}
	if consoleAsk, ok := b.data.(*ConsoleAskData); ok && consoleAsk.AskID == "" {
		consoleAsk.AskID = b.message.ID
	}
	if cmd, ok := b.data.(*CmdData); ok && cmd.CmdID == "" {
		cmd.CmdID = b.message.ID
	}
	if event, ok := b.data.(*EventData); ok && event.EventID == "" {
		event.EventID = b.message.ID
	}

	metaTS := time.Unix(b.message.Timestamp, 0).UTC().Format(time.RFC3339)
	envelope := map[string]any{
		"version": 1,
		"status":  "ok",
		"command": string(b.message.Type),
		"data":    b.data,
		"meta": map[string]any{
			"ts": metaTS,
		},
		"error": map[string]any{},
	}

	payload, err := json.Marshal(envelope)
	if err != nil {
		return Message{}, fmt.Errorf("marshal envelope: %w", err)
	}

	b.message.Payload = payload
	return *b.message, nil
}

// MustBuild builds the message and panics on error.
func (b *Builder) MustBuild() Message {
	msg, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("build message: %v", err))
	}
	return msg
}

// ReplyTo creates a reply builder configured from the original message.
func ReplyTo(msg Message) *Builder {
	// Clone headers to avoid aliasing
	headers := make(map[string]string, len(msg.Headers))
	for k, v := range msg.Headers {
		headers[k] = v
	}
	b := NewAgentReply().
		FromNS(msg.ToNS).
		ToNS(msg.FromNS).
		WithSessionID(msg.SessionID).
		WithWorkspace(msg.Workspace).
		WithHeaders(headers)
	// Extract and set AskID from the original Ask message
	if msg.Type == MessageTypeAsk {
		if data, err := ParsePayload[AskData](msg); err == nil {
			b = b.AskID(data.AskID)
		}
	}
	return b
}

// ParsePayload parses the envelope payload into the target type.
func ParsePayload[T any](msg Message) (T, error) {
	var envelope struct {
		Data T `json:"data"`
	}

	if err := json.Unmarshal(msg.Payload, &envelope); err != nil {
		var zero T
		return zero, fmt.Errorf("unmarshal envelope: %w", err)
	}

	return envelope.Data, nil
}

// ParsePayloadMap parses the envelope payload as a map.
func ParsePayloadMap(msg Message) (map[string]any, error) {
	var envelope struct {
		Data map[string]any `json:"data"`
	}

	if err := json.Unmarshal(msg.Payload, &envelope); err != nil {
		return nil, fmt.Errorf("unmarshal envelope: %w", err)
	}

	return envelope.Data, nil
}

// defaultTTLMS returns the default TTL in milliseconds (5 minutes).
func defaultTTLMS() int64 {
	return int64((5 * time.Minute).Milliseconds())
}
