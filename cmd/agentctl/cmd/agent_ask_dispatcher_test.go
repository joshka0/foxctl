package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
	v2ask "github.com/jkatigb/agentctl/internal/v2/core/ask"
)

func TestMailboxAskDispatcher_Send(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := mailbox.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("mailbox.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Date(2026, time.March, 2, 12, 0, 0, 0, time.UTC)
	dispatcher := newMailboxAskDispatcher(store, func() time.Time { return now }, func() string { return "msg-1" })

	messageID, err := dispatcher.Send(ctx, v2ask.Message{
		AskID:          "ask-1",
		RequestID:      "req-1",
		Kind:           "context",
		Question:       "What did you find?",
		ConversationID: "conv-1",
		FromNS:         "cli:1",
		ToNS:           "agent:1",
		TTLMS:          30_000,
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if messageID != "msg-1" {
		t.Fatalf("message_id=%q want msg-1", messageID)
	}

	msgs, err := store.List(ctx, "agent:1", 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("message count=%d want 1", len(msgs))
	}

	got := msgs[0]
	if got.ID != "msg-1" {
		t.Fatalf("stored id=%q want msg-1", got.ID)
	}
	if got.FromNS != "cli:1" {
		t.Fatalf("from_ns=%q want cli:1", got.FromNS)
	}
	if got.ToNS != "agent:1" {
		t.Fatalf("to_ns=%q want agent:1", got.ToNS)
	}
	if got.Type != agent.MessageTypeAsk {
		t.Fatalf("type=%q want %q", got.Type, agent.MessageTypeAsk)
	}
	if got.TTLMS != 30_000 {
		t.Fatalf("ttl_ms=%d want 30000", got.TTLMS)
	}
	if got.Headers["correlation"] != "ask-1" {
		t.Fatalf("correlation header=%q want ask-1", got.Headers["correlation"])
	}
	if got.VisibleAt != now.Unix() {
		t.Fatalf("visible_at=%d want %d", got.VisibleAt, now.Unix())
	}
	if got.Timestamp != now.Unix() {
		t.Fatalf("timestamp=%d want %d", got.Timestamp, now.Unix())
	}

	askData, err := agent.ParsePayload[agent.AskData](got)
	if err != nil {
		t.Fatalf("ParsePayload() error = %v", err)
	}
	if askData.AskID != "ask-1" {
		t.Fatalf("ask_id=%q want ask-1", askData.AskID)
	}
	if askData.Kind != "context" {
		t.Fatalf("kind=%q want context", askData.Kind)
	}
	if askData.Question != "What did you find?" {
		t.Fatalf("question=%q want expected", askData.Question)
	}
	if askData.ConversationID != "conv-1" {
		t.Fatalf("conversation_id=%q want conv-1", askData.ConversationID)
	}
}

func TestMailboxAskDispatcher_SendRequiresStore(t *testing.T) {
	t.Parallel()

	dispatcher := newMailboxAskDispatcher(nil, nil, nil)
	_, err := dispatcher.Send(context.Background(), v2ask.Message{
		AskID:    "ask-1",
		Kind:     "context",
		Question: "q",
		FromNS:   "cli:1",
		ToNS:     "agent:1",
	})
	if err == nil {
		t.Fatal("expected error when dispatcher has no store")
	}
}
