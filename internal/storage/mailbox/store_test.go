package mailbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"

	_ "modernc.org/sqlite"
)

func TestMailboxStore(t *testing.T) {
	ctx := context.Background()

	// Create temporary directory for test
	tmpDir := t.TempDir()

	// Open store
	store, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	// Test Send
	t.Run("Send", func(t *testing.T) {
		payload := map[string]any{
			"skill": "test/skill",
			"params": map[string]any{
				"arg1": "value1",
			},
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("failed to marshal payload: %v", err)
		}

		msg := agent.Message{
			ID:        "msg-001",
			FromNS:    "org/app/agent-001",
			ToNS:      "org/app/agent-002",
			Type:      agent.MessageTypeCmd,
			TTLMS:     300000,
			Headers:   map[string]string{"correlation": "task-123"},
			Payload:   json.RawMessage(payloadBytes),
			VisibleAt: time.Now().Unix(),
			Attempt:   0,
			Timestamp: time.Now().Unix(),
		}

		if err := store.Send(ctx, msg); err != nil {
			t.Fatalf("failed to send message: %v", err)
		}
	})

	// Test List
	t.Run("List", func(t *testing.T) {
		messages, err := store.List(ctx, "org/app/agent-002", 10)
		if err != nil {
			t.Fatalf("failed to list messages: %v", err)
		}

		if len(messages) != 1 {
			t.Errorf("expected 1 message, got %d", len(messages))
		}

		msg := messages[0]
		if msg.ID != "msg-001" {
			t.Errorf("expected ID msg-001, got %s", msg.ID)
		}
		if msg.Type != agent.MessageTypeCmd {
			t.Errorf("expected type agent.cmd, got %s", msg.Type)
		}
	})

	// Test Poll (leases immediately)
	t.Run("Poll", func(t *testing.T) {
		messages, err := store.Poll(ctx, "org/app/agent-002", 100*time.Millisecond, 10)
		if err != nil {
			t.Fatalf("failed to poll messages: %v", err)
		}

		if len(messages) != 1 {
			t.Errorf("expected 1 message, got %d", len(messages))
		}

		// Verify visibility timeout was updated
		if messages[0].Attempt != 1 {
			t.Errorf("expected attempt 1, got %d", messages[0].Attempt)
		}
	})

	// Test Ack
	t.Run("Ack", func(t *testing.T) {
		if err := store.Ack(ctx, "msg-001"); err != nil {
			t.Fatalf("failed to ack message: %v", err)
		}

		// Verify message is deleted
		messages, err := store.List(ctx, "org/app/agent-002", 10)
		if err != nil {
			t.Fatalf("failed to list messages: %v", err)
		}

		if len(messages) != 0 {
			t.Errorf("expected 0 messages after ack, got %d", len(messages))
		}
	})

	// Test Send and Nack
	t.Run("SendAndNack", func(t *testing.T) {
		payload := map[string]any{
			"test": "data",
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("failed to marshal payload: %v", err)
		}

		msg := agent.Message{
			ID:        "msg-002",
			FromNS:    "org/app/agent-001",
			ToNS:      "org/app/agent-003",
			Type:      agent.MessageTypeAsk,
			TTLMS:     300000,
			Payload:   json.RawMessage(payloadBytes),
			VisibleAt: time.Now().Unix(),
			Timestamp: time.Now().Unix(),
		}

		if err := store.Send(ctx, msg); err != nil {
			t.Fatalf("failed to send message: %v", err)
		}

		// Nack the message
		if err := store.Nack(ctx, "msg-002", 5*time.Second); err != nil {
			t.Fatalf("failed to nack message: %v", err)
		}

		// Message should not be immediately visible
		messages, err := store.Poll(ctx, "org/app/agent-003", 10*time.Millisecond, 10)
		if err != nil {
			t.Fatalf("failed to poll messages: %v", err)
		}

		if len(messages) != 0 {
			t.Errorf("expected 0 messages immediately after nack, got %d", len(messages))
		}
	})

	// Test Delete
	t.Run("Delete", func(t *testing.T) {
		if err := store.Delete(ctx, "msg-002"); err != nil {
			t.Fatalf("failed to delete message: %v", err)
		}

		messages, err := store.List(ctx, "org/app/agent-003", 10)
		if err != nil {
			t.Fatalf("failed to list messages: %v", err)
		}

		if len(messages) != 0 {
			t.Errorf("expected 0 messages after delete, got %d", len(messages))
		}
	})
}

func TestMigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if err := migrate(ctx, db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestMigrateReturnsIndexCreationError(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ExecContext(ctx, `CREATE TABLE idx_mailbox_session (id TEXT)`); err != nil {
		t.Fatal(err)
	}

	err = migrate(ctx, db)
	if err == nil {
		t.Fatal("expected migration error")
	}
	if !strings.Contains(err.Error(), "idx_mailbox_session") {
		t.Fatalf("error=%q want idx_mailbox_session context", err)
	}
}

func TestMailboxPollNoBlock(t *testing.T) {
	ctx := context.Background()

	// Create temporary directory for test
	tmpDir := t.TempDir()

	// Open store
	store, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	// Test Poll returns quickly when no messages
	t.Run("PollReturnsQuickly", func(t *testing.T) {
		start := time.Now()
		messages, err := store.Poll(ctx, "org/app/agent-999", 200*time.Millisecond, 10)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("failed to poll messages: %v", err)
		}

		if len(messages) != 0 {
			t.Errorf("expected 0 messages, got %d", len(messages))
		}

		if elapsed > 50*time.Millisecond {
			t.Errorf("poll took too long: %v", elapsed)
		}
	})
}

func TestMailboxPollRespectsLeaseDuration(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	now := time.Now()
	msg := agent.Message{
		ID:        "lease-msg",
		FromNS:    "sender",
		ToNS:      "worker",
		Type:      agent.MessageTypeCmd,
		TTLMS:     60_000,
		VisibleAt: now.Unix(),
		Timestamp: now.Unix(),
	}
	if err := store.Send(ctx, msg); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	lease := 1 * time.Second
	start := time.Now()
	messages, err := store.Poll(ctx, "worker", lease, 1)
	if err != nil {
		t.Fatalf("poll error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	got := messages[0]
	expectedMin := start.Add(lease - 150*time.Millisecond).Unix() // allow small scheduling jitter
	expectedMax := start.Add(lease + 150*time.Millisecond).Unix()
	if got.VisibleAt < expectedMin || got.VisibleAt > expectedMax {
		t.Fatalf("visible_at %d not within expected lease window [%d, %d]", got.VisibleAt, expectedMin, expectedMax)
	}
	if got.Attempt != 1 {
		t.Fatalf("attempt = %d, want 1", got.Attempt)
	}
}

func TestMailboxMessageTypes(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	messageTypes := []agent.MessageType{
		agent.MessageTypeAsk,
		agent.MessageTypeReply,
		agent.MessageTypeCmd,
		agent.MessageTypeEvent,
	}

	for i, msgType := range messageTypes {
		t.Run(string(msgType), func(t *testing.T) {
			payload := map[string]any{
				"type": msgType,
			}
			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("failed to marshal payload: %v", err)
			}

			msg := agent.Message{
				ID:        fmt.Sprintf("msg-type-%d", i),
				FromNS:    "sender",
				ToNS:      "receiver",
				Type:      msgType,
				TTLMS:     300000,
				Payload:   json.RawMessage(payloadBytes),
				VisibleAt: time.Now().Unix(),
				Timestamp: time.Now().Unix(),
			}

			if err := store.Send(ctx, msg); err != nil {
				t.Fatalf("failed to send %s message: %v", msgType, err)
			}

			// Verify we can retrieve it
			messages, err := store.List(ctx, "receiver", 10)
			if err != nil {
				t.Fatalf("failed to list messages: %v", err)
			}

			found := false
			for _, m := range messages {
				if m.Type == msgType {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("message type %s not found in list", msgType)
			}
		})
	}
}

func TestMailboxPollByTypes(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	msgs := []agent.Message{
		{
			ID:        "ask-msg",
			FromNS:    "sender",
			ToNS:      "receiver",
			Type:      agent.MessageTypeAsk,
			TTLMS:     300000,
			VisibleAt: now,
			Timestamp: now,
		},
		{
			ID:        "reply-msg",
			FromNS:    "sender",
			ToNS:      "receiver",
			Type:      agent.MessageTypeReply,
			TTLMS:     300000,
			VisibleAt: now,
			Timestamp: now,
		},
		{
			ID:        "cmd-msg",
			FromNS:    "sender",
			ToNS:      "receiver",
			Type:      agent.MessageTypeCmd,
			TTLMS:     300000,
			VisibleAt: now,
			Timestamp: now,
		},
	}

	for _, msg := range msgs {
		if err := store.Send(ctx, msg); err != nil {
			t.Fatalf("failed to send message: %v", err)
		}
	}

	filtered, err := store.PollByTypes(ctx, "receiver", time.Second, 10, []agent.MessageType{
		agent.MessageTypeAsk,
		agent.MessageTypeCmd,
	})
	if err != nil {
		t.Fatalf("poll by types failed: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(filtered))
	}
	for _, msg := range filtered {
		if msg.Type == agent.MessageTypeReply {
			t.Fatalf("unexpected reply message in filtered poll")
		}
	}

	replies, err := store.PollByTypes(ctx, "receiver", time.Second, 10, []agent.MessageType{agent.MessageTypeReply})
	if err != nil {
		t.Fatalf("poll replies failed: %v", err)
	}
	if len(replies) != 1 {
		t.Fatalf("expected 1 reply message, got %d", len(replies))
	}
	if replies[0].Type != agent.MessageTypeReply {
		t.Fatalf("expected reply message, got %s", replies[0].Type)
	}
}
