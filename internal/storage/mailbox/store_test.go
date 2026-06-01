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

func TestMailboxSendRejectsInvalidPayloadJSON(t *testing.T) {
	ctx := context.Background()
	store := openTestMailboxStore(t)

	err := store.Send(ctx, agent.Message{
		ID:        "invalid-payload-msg",
		FromNS:    "sender",
		ToNS:      "receiver",
		Type:      agent.MessageTypeCmd,
		TTLMS:     60_000,
		Payload:   json.RawMessage(`{`),
		VisibleAt: time.Now().Unix(),
		Timestamp: time.Now().Unix(),
	})
	requireMailboxErrorContains(t, "Send", err, "payload")

	messages, err := store.List(ctx, "receiver", 10)
	if err != nil {
		t.Fatalf("list after rejected send: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("rejected invalid payload persisted %d messages", len(messages))
	}
}

func TestMailboxRejectsCorruptStoredPayloadJSON(t *testing.T) {
	ctx := context.Background()
	store := openTestMailboxStore(t)

	now := time.Now().Unix()
	msg := agent.Message{
		ID:        "corrupt-payload-msg",
		FromNS:    "sender",
		ToNS:      "receiver",
		Type:      agent.MessageTypeCmd,
		TTLMS:     60_000,
		Payload:   json.RawMessage(`{"ok":true}`),
		VisibleAt: now,
		Timestamp: now,
		SessionID: "session-1",
		Workspace: "/workspace",
	}
	if err := store.Send(ctx, msg); err != nil {
		t.Fatalf("send valid message: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE mailbox SET payload = $1 WHERE id = $2
	`, "{", msg.ID); err != nil {
		t.Fatalf("corrupt payload: %v", err)
	}

	_, err := store.List(ctx, "receiver", 10)
	requireMailboxErrorContains(t, "List", err, "payload")
	_, err = store.Poll(ctx, "receiver", time.Second, 1)
	requireMailboxErrorContains(t, "Poll", err, "payload")
	_, err = store.ListBySession(ctx, "session-1", 10)
	requireMailboxErrorContains(t, "ListBySession", err, "payload")
	_, err = store.ListByWorkspace(ctx, "/workspace", 10)
	requireMailboxErrorContains(t, "ListByWorkspace", err, "payload")
}

func TestMailboxRejectsCorruptStoredHeadersJSON(t *testing.T) {
	ctx := context.Background()
	store := openTestMailboxStore(t)

	now := time.Now().Unix()
	msg := agent.Message{
		ID:        "corrupt-headers-msg",
		FromNS:    "sender",
		ToNS:      "receiver",
		Type:      agent.MessageTypeCmd,
		TTLMS:     60_000,
		Headers:   map[string]string{"correlation": "task-123"},
		Payload:   json.RawMessage(`{"ok":true}`),
		VisibleAt: now,
		Timestamp: now,
		SessionID: "session-1",
		Workspace: "/workspace",
	}
	if err := store.Send(ctx, msg); err != nil {
		t.Fatalf("send valid message: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE mailbox SET headers = $1 WHERE id = $2
	`, "{", msg.ID); err != nil {
		t.Fatalf("corrupt headers: %v", err)
	}

	_, err := store.List(ctx, "receiver", 10)
	requireMailboxErrorContains(t, "List", err, "headers")
	_, err = store.Poll(ctx, "receiver", time.Second, 1)
	requireMailboxErrorContains(t, "Poll", err, "headers")
	_, err = store.ListBySession(ctx, "session-1", 10)
	requireMailboxErrorContains(t, "ListBySession", err, "headers")
	_, err = store.ListByWorkspace(ctx, "/workspace", 10)
	requireMailboxErrorContains(t, "ListByWorkspace", err, "headers")
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
		Payload:   json.RawMessage(`{"test":"lease"}`),
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

func TestMailboxPollPositiveSubsecondLeasePreventsImmediateRedelivery(t *testing.T) {
	ctx := context.Background()
	store := openTestMailboxStore(t)
	waitForStableUnixSecond(t, 200*time.Millisecond)

	now := time.Now()
	msg := agent.Message{
		ID:        "subsecond-lease-msg",
		FromNS:    "sender",
		ToNS:      "worker",
		Type:      agent.MessageTypeCmd,
		TTLMS:     60_000,
		Payload:   json.RawMessage(`{"test":"subsecond-lease"}`),
		VisibleAt: now.Unix(),
		Timestamp: now.Unix(),
	}
	if err := store.Send(ctx, msg); err != nil {
		t.Fatalf("send message: %v", err)
	}

	first, err := store.Poll(ctx, "worker", time.Millisecond, 1)
	if err != nil {
		t.Fatalf("first poll: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first poll returned %d messages, want 1", len(first))
	}
	if first[0].VisibleAt <= now.Unix() {
		t.Fatalf("positive subsecond lease visible_at=%d, want after %d", first[0].VisibleAt, now.Unix())
	}

	second, err := store.Poll(ctx, "worker", time.Second, 1)
	if err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("positive lease allowed immediate redelivery of %q", second[0].ID)
	}
}

func TestMailboxNackPositiveSubsecondTimeoutPreventsImmediateRedelivery(t *testing.T) {
	ctx := context.Background()
	store := openTestMailboxStore(t)

	now := time.Now()
	msg := agent.Message{
		ID:        "subsecond-nack-msg",
		FromNS:    "sender",
		ToNS:      "worker",
		Type:      agent.MessageTypeCmd,
		TTLMS:     60_000,
		Payload:   json.RawMessage(`{"test":"subsecond-nack"}`),
		VisibleAt: now.Unix(),
		Timestamp: now.Unix(),
	}
	if err := store.Send(ctx, msg); err != nil {
		t.Fatalf("send message: %v", err)
	}

	first, err := store.Poll(ctx, "worker", time.Minute, 1)
	if err != nil {
		t.Fatalf("initial poll: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("initial poll returned %d messages, want 1", len(first))
	}

	waitForStableUnixSecond(t, 200*time.Millisecond)
	nackStart := time.Now().Unix()
	if err := store.Nack(ctx, msg.ID, time.Millisecond); err != nil {
		t.Fatalf("nack message: %v", err)
	}

	listed, err := store.List(ctx, "worker", 1)
	if err != nil {
		t.Fatalf("list nacked message: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed %d messages, want 1", len(listed))
	}
	if listed[0].VisibleAt <= nackStart {
		t.Fatalf("positive subsecond nack visible_at=%d, want after %d", listed[0].VisibleAt, nackStart)
	}

	second, err := store.Poll(ctx, "worker", time.Second, 1)
	if err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("positive nack timeout allowed immediate redelivery of %q", second[0].ID)
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
			Payload:   json.RawMessage(`{"test":"ask"}`),
			VisibleAt: now,
			Timestamp: now,
		},
		{
			ID:        "reply-msg",
			FromNS:    "sender",
			ToNS:      "receiver",
			Type:      agent.MessageTypeReply,
			TTLMS:     300000,
			Payload:   json.RawMessage(`{"test":"reply"}`),
			VisibleAt: now,
			Timestamp: now,
		},
		{
			ID:        "cmd-msg",
			FromNS:    "sender",
			ToNS:      "receiver",
			Type:      agent.MessageTypeCmd,
			TTLMS:     300000,
			Payload:   json.RawMessage(`{"test":"cmd"}`),
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

func openTestMailboxStore(t *testing.T) Store {
	t.Helper()

	store, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open mailbox store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close mailbox store: %v", err)
		}
	})
	return store
}

func waitForStableUnixSecond(t *testing.T, minRemaining time.Duration) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		now := time.Now()
		nextSecond := time.Unix(now.Unix()+1, 0)
		if nextSecond.Sub(now) > minRemaining {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("could not find a stable unix second with %s remaining", minRemaining)
}

func requireMailboxErrorContains(t *testing.T, operation string, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s error = nil, want error containing %q", operation, want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("%s error = %v, want it to contain %q", operation, err, want)
	}
}
