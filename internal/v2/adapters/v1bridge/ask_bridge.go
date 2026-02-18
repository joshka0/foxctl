package v1bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/v2/core/ask"
	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
)

// LegacyMailboxSender is the minimal mailbox contract required for ask dispatch.
type LegacyMailboxSender interface {
	Send(ctx context.Context, msg agent.Message) error
}

// AskBridge adapts normalized v2 ask messages to the legacy mailbox envelope.
type AskBridge struct {
	mailbox LegacyMailboxSender
	now     func() time.Time
	newID   func() string
}

// NewAskBridge creates a v1 ask bridge.
func NewAskBridge(mailbox LegacyMailboxSender, now func() time.Time, newID func() string) *AskBridge {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if newID == nil {
		var seq atomic.Uint64
		newID = func() string {
			return fmt.Sprintf("msg-%06d", seq.Add(1))
		}
	}
	return &AskBridge{
		mailbox: mailbox,
		now:     now,
		newID:   newID,
	}
}

// Send transforms an ask message into a legacy mailbox message.
func (b *AskBridge) Send(ctx context.Context, msg ask.Message) (string, error) {
	if b == nil || b.mailbox == nil {
		return "", &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "legacy mailbox sender is not configured",
			Fatal:   true,
		}
	}

	toNS := strings.TrimSpace(msg.ToNS)
	if toNS == "" {
		return "", &v2errors.V2Error{
			Kind:    v2errors.ErrValidation,
			Message: "ask target namespace is required",
			Fatal:   true,
			Details: map[string]any{
				"field": "to_ns",
			},
		}
	}

	payload, err := json.Marshal(envelope.OK("agent.ask", agent.AskData{
		AskID:          strings.TrimSpace(msg.AskID),
		Kind:           strings.TrimSpace(msg.Kind),
		Question:       strings.TrimSpace(msg.Question),
		ConversationID: strings.TrimSpace(msg.ConversationID),
	}))
	if err != nil {
		return "", &v2errors.V2Error{
			Kind:    v2errors.ErrInternal,
			Message: "marshal legacy ask payload",
			Cause:   err,
			Fatal:   true,
		}
	}

	messageID := strings.TrimSpace(b.newID())
	now := b.now()
	out := agent.Message{
		ID:        messageID,
		FromNS:    strings.TrimSpace(msg.FromNS),
		ToNS:      toNS,
		Type:      agent.MessageTypeAsk,
		TTLMS:     msg.TTLMS,
		Headers:   map[string]string{"correlation": strings.TrimSpace(msg.AskID)},
		Payload:   payload,
		VisibleAt: now.Unix(),
		Timestamp: now.Unix(),
	}
	if err := b.mailbox.Send(ctx, out); err != nil {
		return "", &v2errors.V2Error{
			Kind:      v2errors.ErrDependency,
			Message:   "legacy mailbox send failed",
			Cause:     err,
			Fatal:     true,
			Retryable: true,
		}
	}
	return messageID, nil
}
