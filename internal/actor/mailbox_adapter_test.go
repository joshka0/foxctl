package actor

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
)

// mockMailboxStore is a mock implementation of mailbox.Store for testing.
type mockMailboxStore struct {
	messages []agent.Message
	acked    []string
	nacked   []string
	db       *sql.DB
}

func (m *mockMailboxStore) Close() error { return nil }

func (m *mockMailboxStore) DB() *sql.DB { return m.db }

func (m *mockMailboxStore) Send(_ context.Context, msg agent.Message) error {
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockMailboxStore) Poll(_ context.Context, agentNS string, _ time.Duration, maxMessages int) ([]agent.Message, error) {
	var result []agent.Message
	for _, msg := range m.messages {
		if msg.ToNS == agentNS {
			result = append(result, msg)
			if len(result) >= maxMessages {
				break
			}
		}
	}
	return result, nil
}

func (m *mockMailboxStore) Ack(_ context.Context, messageID string) error {
	m.acked = append(m.acked, messageID)
	return nil
}

func (m *mockMailboxStore) Nack(_ context.Context, messageID string, _ time.Duration) error {
	m.nacked = append(m.nacked, messageID)
	return nil
}

func (m *mockMailboxStore) List(_ context.Context, _ string, _ int) ([]agent.Message, error) {
	return m.messages, nil
}

func (m *mockMailboxStore) Delete(_ context.Context, _ string) error {
	return nil
}

func (m *mockMailboxStore) ListBySession(_ context.Context, _ string, _ int) ([]agent.Message, error) {
	return nil, nil
}

func (m *mockMailboxStore) ListByWorkspace(_ context.Context, _ string, _ int) ([]agent.Message, error) {
	return nil, nil
}

func TestMailboxAdapter_Poll(t *testing.T) {
	store := &mockMailboxStore{
		messages: []agent.Message{
			{
				ID:        "msg-1",
				FromNS:    "sender",
				ToNS:      "receiver",
				Type:      agent.MessageTypeAsk,
				Payload:   json.RawMessage(`{"task": "test"}`),
				TTLMS:     60000,
				VisibleAt: time.Now().Unix(),
				Attempt:   0,
				Timestamp: time.Now().Unix(),
			},
		},
	}

	adapter := NewMailboxAdapter(store)

	msg, err := adapter.Poll(context.Background(), "receiver", time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg == nil {
		t.Fatal("expected message, got nil")
	}

	if msg.ID != "msg-1" {
		t.Errorf("expected ID msg-1, got %s", msg.ID)
	}

	if msg.FromNS != "sender" {
		t.Errorf("expected FromNS sender, got %s", msg.FromNS)
	}

	if msg.ToNS != "receiver" {
		t.Errorf("expected ToNS receiver, got %s", msg.ToNS)
	}

	if msg.Subject != string(agent.MessageTypeAsk) {
		t.Errorf("expected Subject %s, got %s", agent.MessageTypeAsk, msg.Subject)
	}
}

func TestMailboxAdapter_PollEmpty(t *testing.T) {
	store := &mockMailboxStore{}
	adapter := NewMailboxAdapter(store)

	msg, err := adapter.Poll(context.Background(), "nobody", time.Millisecond*10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg != nil {
		t.Errorf("expected nil, got %v", msg)
	}
}

func TestMailboxAdapter_Ack(t *testing.T) {
	store := &mockMailboxStore{}
	adapter := NewMailboxAdapter(store)

	err := adapter.Ack(context.Background(), "msg-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(store.acked) != 1 || store.acked[0] != "msg-1" {
		t.Errorf("expected acked [msg-1], got %v", store.acked)
	}
}

func TestMailboxAdapter_Nack(t *testing.T) {
	store := &mockMailboxStore{}
	adapter := NewMailboxAdapter(store, WithDefaultVisibilityTimeout(time.Minute))

	err := adapter.Nack(context.Background(), "msg-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(store.nacked) != 1 || store.nacked[0] != "msg-1" {
		t.Errorf("expected nacked [msg-1], got %v", store.nacked)
	}
}

func TestMailboxAdapter_Send(t *testing.T) {
	store := &mockMailboxStore{}
	adapter := NewMailboxAdapter(store)

	now := time.Now()
	msg := &Message{
		ID:        "msg-new",
		FromNS:    "sender",
		ToNS:      "receiver",
		Subject:   "agent.ask",
		Body:      json.RawMessage(`{"task": "new task"}`),
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}

	err := adapter.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(store.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(store.messages))
	}

	sent := store.messages[0]
	if sent.ID != "msg-new" {
		t.Errorf("expected ID msg-new, got %s", sent.ID)
	}

	if sent.Type != "agent.ask" {
		t.Errorf("expected Type agent.ask, got %s", sent.Type)
	}
}
