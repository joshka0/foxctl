package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// Sender provides convenient methods for sending messages.
type Sender struct {
	store   MailboxStore
	myNS    string
	nextID  func() (string, error)
	nowTime func() (int64, error)
}

// SenderOption configures a Sender.
type SenderOption func(*Sender)

// WithSenderClock sets a custom clock for the Sender.
func WithSenderClock(clock func() time.Time) SenderOption {
	return func(s *Sender) {
		s.nowTime = func() (int64, error) {
			return clock().Unix(), nil
		}
	}
}

// WithSenderIDGenerator sets a custom ID generator for the Sender.
func WithSenderIDGenerator(gen func() string) SenderOption {
	return func(s *Sender) {
		s.nextID = func() (string, error) {
			return gen(), nil
		}
	}
}

func defaultNextID() (string, error) {
	return ulid.Make().String(), nil
}

func defaultNowTime() (int64, error) {
	return time.Now().Unix(), nil
}

// MailboxStore is the interface for sending and receiving messages.
// This matches the mailbox.Store interface.
type MailboxStore interface {
	Send(ctx context.Context, msg Message) error
	Poll(ctx context.Context, agentNS string, leaseDuration time.Duration, maxMessages int) ([]Message, error)
	Ack(ctx context.Context, messageID string) error
	Nack(ctx context.Context, messageID string, visibilityTimeout time.Duration) error
}

// NewSender creates a new Sender for the given namespace.
// Default ID generator uses ULID; default time uses time.Now().Unix().
func NewSender(store MailboxStore, namespace string, opts ...SenderOption) *Sender {
	sender := &Sender{
		store:   store,
		myNS:    namespace,
		nextID:  defaultNextID,
		nowTime: defaultNowTime,
	}
	for _, opt := range opts {
		opt(sender)
	}
	return sender
}

func (s *Sender) nextIDValue() (string, error) {
	if s.nextID == nil {
		return "", fmt.Errorf("sender id generator is required")
	}
	return s.nextID()
}

func (s *Sender) nowTimeValue() (int64, error) {
	if s.nowTime == nil {
		return 0, fmt.Errorf("sender clock is required")
	}
	return s.nowTime()
}

// SendAsk sends a request and returns the ask ID for correlation.
func (s *Sender) SendAsk(ctx context.Context, toNS, question string, opts ...AskOpt) (string, error) {
	cfg := &AskConfig{
		TTL:         5 * time.Minute,
		SessionID:   "",
		Workspace:   "",
		Context:     nil,
		Correlation: "",
	}

	for _, opt := range opts {
		opt(cfg)
	}

	now, err := s.nowTimeValue()
	if err != nil {
		return "", err
	}
	msgID, err := s.nextIDValue()
	if err != nil {
		return "", err
	}

	msg := NewAgentAsk().
		WithID(msgID).
		WithTimestampUnix(now).
		FromNS(s.myNS).
		ToNS(toNS).
		Question(question).
		WithTTL(cfg.TTL)

	if cfg.SessionID != "" {
		msg = msg.WithSessionID(cfg.SessionID)
	}
	if cfg.Workspace != "" {
		msg = msg.WithWorkspace(cfg.Workspace)
	}
	if cfg.Context != nil {
		msg = msg.WithContext(cfg.Context)
	}
	if cfg.Correlation != "" {
		msg = msg.WithHeader("correlation", cfg.Correlation)
	}
	if cfg.Kind != "" {
		msg = msg.WithKind(cfg.Kind)
	}

	built, err := msg.Build()
	if err != nil {
		return "", fmt.Errorf("build message: %w", err)
	}

	return built.AskID(), s.store.Send(ctx, built)
}

// SendReply sends a reply to an original message.
func (s *Sender) SendReply(ctx context.Context, original Message, answer map[string]any) error {
	data, err := ParsePayload[AskData](original)
	if err != nil {
		return fmt.Errorf("parse original payload: %w", err)
	}

	now, err := s.nowTimeValue()
	if err != nil {
		return err
	}
	msgID, err := s.nextIDValue()
	if err != nil {
		return err
	}

	msg, err := ReplyTo(original).
		AskID(data.AskID).
		Answer(answer).
		WithID(msgID).
		WithTimestampUnix(now).
		Build()
	if err != nil {
		return fmt.Errorf("build message: %w", err)
	}

	return s.store.Send(ctx, msg)
}

// SendCmd sends a fire-and-forget command.
func (s *Sender) SendCmd(ctx context.Context, toNS, action string, args map[string]any, opts ...CmdOpt) (string, error) {
	cfg := &CmdConfig{
		TTL:       30 * time.Minute,
		SessionID: "",
		Workspace: "",
		Skill:     "",
	}

	for _, opt := range opts {
		opt(cfg)
	}

	now, err := s.nowTimeValue()
	if err != nil {
		return "", err
	}
	msgID, err := s.nextIDValue()
	if err != nil {
		return "", err
	}
	cmdID, err := s.nextIDValue()
	if err != nil {
		return "", err
	}

	msg := NewAgentCmd().
		WithID(msgID).
		WithTimestampUnix(now).
		FromNS(s.myNS).
		ToNS(toNS).
		CmdID(cmdID).
		Action(action).
		WithArgs(args).
		WithTTL(cfg.TTL)

	if cfg.SessionID != "" {
		msg = msg.WithSessionID(cfg.SessionID)
	}
	if cfg.Workspace != "" {
		msg = msg.WithWorkspace(cfg.Workspace)
	}
	if cfg.Skill != "" {
		msg = msg.WithSkill(cfg.Skill)
	}

	built, err := msg.Build()
	if err != nil {
		return "", fmt.Errorf("build message: %w", err)
	}

	return built.CmdID(), s.store.Send(ctx, built)
}

// SendEvent broadcasts an event.
func (s *Sender) SendEvent(ctx context.Context, eventKind string, customData map[string]any, opts ...EventOpt) error {
	cfg := &EventConfig{
		TTL:           5 * time.Minute,
		SessionID:     "",
		Workspace:     "",
		DestinationNS: "broadcast",
	}

	for _, opt := range opts {
		opt(cfg)
	}

	now, err := s.nowTimeValue()
	if err != nil {
		return err
	}
	msgID, err := s.nextIDValue()
	if err != nil {
		return err
	}
	eventID, err := s.nextIDValue()
	if err != nil {
		return err
	}

	msg := NewAgentEvent().
		WithID(msgID).
		WithTimestampUnix(now).
		FromNS(s.myNS).
		ToNS(cfg.DestinationNS).
		EventID(eventID).
		EventKind(eventKind).
		WithCustomData(customData).
		WithTTL(cfg.TTL)

	if cfg.SessionID != "" {
		msg = msg.WithSessionID(cfg.SessionID)
	}
	if cfg.Workspace != "" {
		msg = msg.WithWorkspace(cfg.Workspace)
	}

	built, err := msg.Build()
	if err != nil {
		return fmt.Errorf("build message: %w", err)
	}

	return s.store.Send(ctx, built)
}

// AskOpt configures an Ask message.
type AskOpt func(*AskConfig)

// AskConfig holds configuration for Ask messages.
type AskConfig struct {
	TTL         time.Duration
	Kind        string
	SessionID   string
	Workspace   string
	Context     map[string]any
	Correlation string
}

// WithAskTTL sets the TTL for an Ask message.
func WithAskTTL(ttl time.Duration) AskOpt {
	return func(cfg *AskConfig) {
		cfg.TTL = ttl
	}
}

// WithAskKind sets the kind for an Ask message.
func WithAskKind(kind string) AskOpt {
	return func(cfg *AskConfig) {
		cfg.Kind = kind
	}
}

// WithAskSession sets the session ID for an Ask message.
func WithAskSession(sessionID string) AskOpt {
	return func(cfg *AskConfig) {
		cfg.SessionID = sessionID
	}
}

// WithAskWorkspace sets the workspace for an Ask message.
func WithAskWorkspace(workspace string) AskOpt {
	return func(cfg *AskConfig) {
		cfg.Workspace = workspace
	}
}

// WithAskContext sets the context for an Ask message.
func WithAskContext(context map[string]any) AskOpt {
	return func(cfg *AskConfig) {
		cfg.Context = context
	}
}

// WithAskCorrelation sets the correlation ID for an Ask message.
func WithAskCorrelation(correlation string) AskOpt {
	return func(cfg *AskConfig) {
		cfg.Correlation = correlation
	}
}

// CmdOpt configures a Cmd message.
type CmdOpt func(*CmdConfig)

// CmdConfig holds configuration for Cmd messages.
type CmdConfig struct {
	TTL       time.Duration
	SessionID string
	Workspace string
	Skill     string
}

// WithCmdTTL sets the TTL for a Cmd message.
func WithCmdTTL(ttl time.Duration) CmdOpt {
	return func(cfg *CmdConfig) {
		cfg.TTL = ttl
	}
}

// WithCmdSession sets the session ID for a Cmd message.
func WithCmdSession(sessionID string) CmdOpt {
	return func(cfg *CmdConfig) {
		cfg.SessionID = sessionID
	}
}

// WithCmdWorkspace sets the workspace for a Cmd message.
func WithCmdWorkspace(workspace string) CmdOpt {
	return func(cfg *CmdConfig) {
		cfg.Workspace = workspace
	}
}

// WithCmdSkill sets the skill for a Cmd message.
func WithCmdSkill(skill string) CmdOpt {
	return func(cfg *CmdConfig) {
		cfg.Skill = skill
	}
}

// EventOpt configures an Event message.
type EventOpt func(*EventConfig)

// EventConfig holds configuration for Event messages.
type EventConfig struct {
	TTL           time.Duration
	SessionID     string
	Workspace     string
	DestinationNS string
}

// WithEventTTL sets the TTL for an Event message.
func WithEventTTL(ttl time.Duration) EventOpt {
	return func(cfg *EventConfig) {
		cfg.TTL = ttl
	}
}

// WithEventSession sets the session ID for an Event message.
func WithEventSession(sessionID string) EventOpt {
	return func(cfg *EventConfig) {
		cfg.SessionID = sessionID
	}
}

// WithEventWorkspace sets the workspace for an Event message.
func WithEventWorkspace(workspace string) EventOpt {
	return func(cfg *EventConfig) {
		cfg.Workspace = workspace
	}
}

// WithEventDestination sets the destination namespace for an Event message.
func WithEventDestination(destinationNS string) EventOpt {
	return func(cfg *EventConfig) {
		cfg.DestinationNS = destinationNS
	}
}

// ReplyToBuilder creates a ReplyBuilder from an original message using the builder.
// This delegates to the ReplyTo function which handles defensive header copying.
func ReplyToBuilder(original Message) *Builder {
	// Delegate to ReplyTo which properly clones headers to avoid aliasing
	return ReplyTo(original)
}

// AskID extracts the AskID from the message payload.
func (m Message) AskID() string {
	switch m.Type {
	case MessageTypeAsk:
		if data, err := ParsePayload[AskData](m); err == nil {
			return data.AskID
		}
	case MessageTypeReply:
		if data, err := ParsePayload[ReplyData](m); err == nil {
			return data.AskID
		}
	case MessageTypeConsoleAsk:
		if data, err := ParsePayload[ConsoleAskData](m); err == nil {
			return data.AskID
		}
	case MessageTypeConsoleReply:
		if data, err := ParsePayload[ConsoleReplyData](m); err == nil {
			return data.AskID
		}
	case MessageTypeConsoleEvent:
		if data, err := ParsePayload[ConsoleEventData](m); err == nil {
			return data.AskID
		}
	}
	return ""
}

// CmdID extracts the CmdID from the message payload.
func (m Message) CmdID() string {
	if m.Type == MessageTypeCmd {
		if data, err := ParsePayload[CmdData](m); err == nil {
			return data.CmdID
		}
	}
	return ""
}

// EventID extracts the EventID from the message payload.
func (m Message) EventID() string {
	if m.Type == MessageTypeEvent {
		if data, err := ParsePayload[EventData](m); err == nil {
			return data.EventID
		}
	}
	return ""
}

// UnmarshalPayload unmarshals the envelope payload into the target type.
// This is a convenience method on Message itself.
func (m Message) UnmarshalPayload(target any) error {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(m.Payload, &envelope); err != nil {
		return fmt.Errorf("unmarshal envelope: %w", err)
	}

	if err := json.Unmarshal(envelope.Data, target); err != nil {
		return fmt.Errorf("unmarshal payload data: %w", err)
	}

	return nil
}

// Receiver provides convenient methods for receiving messages.
type Receiver struct {
	store    MailboxStore
	targetNS string
}

// NewReceiver creates a new Receiver for the given target namespace.
func NewReceiver(store MailboxStore, targetNS string) *Receiver {
	return &Receiver{
		store:    store,
		targetNS: targetNS,
	}
}

// PollOnce polls for a message and returns it, or nil if no message available.
func (r *Receiver) PollOnce(ctx context.Context, lease time.Duration) (*Message, error) {
	messages, err := r.store.Poll(ctx, r.targetNS, lease, 1)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, nil
	}
	return &messages[0], nil
}

// PollAll polls for all available messages up to maxMessages.
func (r *Receiver) PollAll(ctx context.Context, lease time.Duration, maxMessages int) ([]Message, error) {
	return r.store.Poll(ctx, r.targetNS, lease, maxMessages)
}

// Ack acknowledges a message as successfully processed.
func (r *Receiver) Ack(ctx context.Context, msg *Message) error {
	return r.store.Ack(ctx, msg.ID)
}

// Nack returns a message to the queue for retry with a visibility delay.
func (r *Receiver) Nack(ctx context.Context, msg *Message, delay time.Duration) error {
	return r.store.Nack(ctx, msg.ID, delay)
}

// WithEvents filters messages by event kind.
func WithEvents(messages []Message, kind string) []Message {
	var filtered []Message
	for _, msg := range messages {
		if msg.Type == MessageTypeEvent {
			if data, err := ParsePayload[EventData](msg); err == nil && data.Kind == kind {
				filtered = append(filtered, msg)
			}
		}
	}
	return filtered
}

// WithAskID filters messages by ask ID (for replies).
func WithAskID(messages []Message, askID string) []Message {
	var filtered []Message
	for _, msg := range messages {
		if msg.Type == MessageTypeReply {
			if data, err := ParsePayload[ReplyData](msg); err == nil && data.AskID == askID {
				filtered = append(filtered, msg)
			}
		}
	}
	return filtered
}
