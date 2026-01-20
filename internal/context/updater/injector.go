package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// MessageSender sends messages to the blackboard/BoardStore.
type MessageSender interface {
	// SendMessage sends a message to a stream for a session.
	// The workspace parameter scopes the message to the session's workspace.
	SendMessage(ctx context.Context, sessionID, workspace, stream string, payload []byte) error
}

// Injector delivers context to sessions via the BoardStore.
type Injector struct {
	sender MessageSender
	stream string // Stream name for context messages (default: "context-updater")
}

// InjectorConfig configures the context injector.
type InjectorConfig struct {
	// Stream is the BoardStore stream name for context messages.
	Stream string
}

// DefaultInjectorConfig returns default injector configuration.
func DefaultInjectorConfig() InjectorConfig {
	return InjectorConfig{
		Stream: "context-updater",
	}
}

// NewInjector creates a new context injector.
func NewInjector(sender MessageSender, config InjectorConfig) *Injector {
	if config.Stream == "" {
		config.Stream = "context-updater"
	}
	return &Injector{
		sender: sender,
		stream: config.Stream,
	}
}

// ContextMessage is the message format for injected context.
type ContextMessage struct {
	// ID uniquely identifies this context injection
	ID string `json:"id"`

	// Type is the kind of context: memory, session, codemap, file
	Type string `json:"type"`

	// Content is the formatted context to display
	Content string `json:"content"`

	// Source describes where this came from
	Source string `json:"source"`

	// Reason explains why this was surfaced
	Reason string `json:"reason"`

	// Score is the relevance score
	Score float32 `json:"score"`

	// Query is the search query that found this
	Query string `json:"query"`

	// Timestamp is when this was injected
	Timestamp time.Time `json:"timestamp"`

	// Priority indicates importance (0=normal, 1=high)
	Priority int `json:"priority,omitempty"`
}

// Inject delivers context to a session via the BoardStore.
func (i *Injector) Inject(ctx context.Context, sessionID, workspace string, candidate ContextCandidate, reason string) error {
	if i.sender == nil {
		return fmt.Errorf("no message sender configured")
	}

	msg := ContextMessage{
		ID:        candidate.ID,
		Type:      candidate.Type,
		Content:   candidate.Content,
		Source:    candidate.Source,
		Reason:    reason,
		Score:     candidate.Score,
		Query:     candidate.Query,
		Timestamp: time.Now(),
	}

	// High priority for gotchas
	if candidate.Type == "memory" && containsGotcha(candidate.Source) {
		msg.Priority = 1
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	return i.sender.SendMessage(ctx, sessionID, workspace, i.stream, payload)
}

// InjectBatch delivers multiple contexts to a session.
func (i *Injector) InjectBatch(ctx context.Context, sessionID, workspace string, candidates []ContextCandidate, reasons []string) error {
	for idx, candidate := range candidates {
		reason := "Potentially relevant context"
		if idx < len(reasons) {
			reason = reasons[idx]
		}
		if err := i.Inject(ctx, sessionID, workspace, candidate, reason); err != nil {
			return err
		}
	}
	return nil
}

// Stream returns the configured stream name.
func (i *Injector) Stream() string {
	return i.stream
}

func containsGotcha(source string) bool {
	return len(source) >= 12 && source[7:13] == "gotcha"
}

// NoOpInjector is an injector that does nothing (for testing).
type NoOpInjector struct{}

// Inject does nothing.
func (NoOpInjector) Inject(ctx context.Context, sessionID, workspace string, candidate ContextCandidate, reason string) error {
	return nil
}

// DrainMessage represents a message to be drained by hooks.
// This is the format hooks read when checking for context to inject.
type DrainMessage struct {
	// Messages are the context messages to surface
	Messages []ContextMessage `json:"messages"`

	// SessionID is the session these messages are for
	SessionID string `json:"session_id"`

	// DrainedAt is when this batch was drained
	DrainedAt time.Time `json:"drained_at"`
}

// FormatForHook formats context messages for hook injection.
// Returns a string suitable for adding to hook context.
func FormatForHook(messages []ContextMessage) string {
	if len(messages) == 0 {
		return ""
	}

	var result string
	for _, msg := range messages {
		if result != "" {
			result += "\n\n"
		}
		result += msg.Content
		if msg.Reason != "" {
			result += fmt.Sprintf("\n*(Surfaced because: %s)*", msg.Reason)
		}
	}

	return "**Relevant Context:**\n\n" + result
}
