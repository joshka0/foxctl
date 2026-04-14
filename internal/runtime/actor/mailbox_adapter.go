package actor

import (
	"context"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/storage/mailbox"
)

// MailboxAdapter wraps mailbox.Store to provide the MailboxStore interface
// expected by the Supervisor.
//
// The adapter handles:
// - Converting between agent.Message and actor.Message
// - Adapting Poll signature ([]agent.Message → *Message)
// - Adapting Nack signature (adds default visibility timeout)
type MailboxAdapter struct {
	store             mailbox.Store
	defaultVisTimeout time.Duration
}

// MailboxAdapterOption configures a MailboxAdapter.
type MailboxAdapterOption func(*MailboxAdapter)

// WithDefaultVisibilityTimeout sets the default visibility timeout for Nack.
func WithDefaultVisibilityTimeout(d time.Duration) MailboxAdapterOption {
	return func(a *MailboxAdapter) {
		a.defaultVisTimeout = d
	}
}

// NewMailboxAdapter creates a new MailboxAdapter wrapping the given store.
func NewMailboxAdapter(store mailbox.Store, opts ...MailboxAdapterOption) *MailboxAdapter {
	a := &MailboxAdapter{
		store:             store,
		defaultVisTimeout: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// ttlMillis computes TTL in milliseconds; if ExpiresAt is zero, falls back to default 5m.
// If CreatedAt is zero but ExpiresAt is set, also falls back to default TTL to avoid
// returning an absolute timestamp instead of a duration.
func (m *Message) ttlMillis() int64 {
	if m.ExpiresAt.IsZero() {
		return int64((5 * time.Minute).Milliseconds())
	}
	if m.CreatedAt.IsZero() {
		// Fall back to default TTL when CreatedAt is unknown
		return int64((5 * time.Minute).Milliseconds())
	}
	return int64(m.ExpiresAt.Sub(m.CreatedAt).Milliseconds())
}

// Poll atomically claims the next available message for a namespace.
// Returns nil if no messages are available within the timeout.
func (a *MailboxAdapter) Poll(ctx context.Context, namespace string, leaseTimeout time.Duration) (*Message, error) {
	// The underlying store returns a slice; we want just one message
	messages, err := a.store.Poll(ctx, namespace, leaseTimeout, 1)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, nil
	}

	return a.toActorMessage(&messages[0]), nil
}

// Ack acknowledges successful processing of a message.
func (a *MailboxAdapter) Ack(ctx context.Context, messageID string) error {
	return a.store.Ack(ctx, messageID)
}

// Nack returns a message to the queue for retry.
func (a *MailboxAdapter) Nack(ctx context.Context, messageID string) error {
	return a.store.Nack(ctx, messageID, a.defaultVisTimeout)
}

// Send sends a message through the underlying store.
func (a *MailboxAdapter) Send(ctx context.Context, msg *Message) error {
	return a.store.Send(ctx, a.toAgentMessage(msg))
}

// toActorMessage converts an agent.Message to an actor.Message.
func (a *MailboxAdapter) toActorMessage(am *agent.Message) *Message {
	return &Message{
		ID:        am.ID,
		FromNS:    am.FromNS,
		ToNS:      am.ToNS,
		Subject:   string(am.Type), // Map Type to Subject
		Headers:   am.Headers,
		Body:      am.Payload,
		Priority:  0, // Default priority
		CreatedAt: time.Unix(am.Timestamp, 0),
		ExpiresAt: time.Unix(am.Timestamp, 0).Add(time.Duration(am.TTLMS) * time.Millisecond),
		LeaseID:   am.ID,
		LeasedAt:  time.Unix(am.VisibleAt, 0),
		Retries:   am.Attempt,
		SessionID: am.SessionID,
		Workspace: am.Workspace,
		AgentID:   am.AgentID,
	}
}

// toAgentMessage converts an actor.Message to an agent.Message.
func (a *MailboxAdapter) toAgentMessage(m *Message) agent.Message {
	created := m.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	// Note: TTL is computed by m.ttlMillis() which handles ExpiresAt.IsZero()
	leasedAt := m.LeasedAt
	if leasedAt.IsZero() {
		leasedAt = created
	}

	ttlMs := m.ttlMillis()
	return agent.Message{
		ID:        m.ID,
		FromNS:    m.FromNS,
		ToNS:      m.ToNS,
		Type:      agent.MessageType(m.Subject), // Map Subject to Type
		Payload:   m.Body,
		Headers:   m.Headers,
		TTLMS:     ttlMs,
		VisibleAt: leasedAt.Unix(),
		Attempt:   m.Retries,
		Timestamp: created.Unix(),
		SessionID: m.SessionID,
		Workspace: m.Workspace,
		AgentID:   m.AgentID,
	}
}

// Ensure MailboxAdapter implements MailboxStore.
var _ MailboxStore = (*MailboxAdapter)(nil)
