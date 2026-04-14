package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/storage/mailbox"
	v2ask "github.com/joshka0/foxctl/internal/v2/core/ask"
)

type mailboxAskDispatcher struct {
	store mailbox.Store
	now   func() time.Time
	newID func() string
}

func newMailboxAskDispatcher(store mailbox.Store, now func() time.Time, newID func() string) *mailboxAskDispatcher {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &mailboxAskDispatcher{
		store: store,
		now:   now,
		newID: newID,
	}
}

func (d *mailboxAskDispatcher) Send(ctx context.Context, msg v2ask.Message) (string, error) {
	if d == nil || d.store == nil {
		return "", errors.New("mailbox ask dispatcher is not configured")
	}

	payload, err := json.Marshal(envelope.OK("agent.ask", agent.AskData{
		AskID:          strings.TrimSpace(msg.AskID),
		Kind:           strings.TrimSpace(msg.Kind),
		Question:       strings.TrimSpace(msg.Question),
		ConversationID: strings.TrimSpace(msg.ConversationID),
	}))
	if err != nil {
		return "", err
	}

	messageID := ""
	if d.newID != nil {
		messageID = strings.TrimSpace(d.newID())
	}
	if messageID == "" {
		messageID = strings.TrimSpace(msg.RequestID)
	}
	if messageID == "" {
		return "", errors.New("ask message id is required")
	}

	now := d.now()
	out := agent.Message{
		ID:        messageID,
		FromNS:    strings.TrimSpace(msg.FromNS),
		ToNS:      strings.TrimSpace(msg.ToNS),
		Type:      agent.MessageTypeAsk,
		TTLMS:     msg.TTLMS,
		Headers:   map[string]string{"correlation": strings.TrimSpace(msg.AskID)},
		Payload:   payload,
		VisibleAt: now.Unix(),
		Timestamp: now.Unix(),
	}
	if err := d.store.Send(ctx, out); err != nil {
		return "", err
	}
	return messageID, nil
}
