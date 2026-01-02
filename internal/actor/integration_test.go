//go:build sqlite_mattn

package actor

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// testMailboxStore is a test implementation of MailboxStore for integration tests.
type testMailboxStore struct {
	mu       sync.Mutex
	messages map[string][]*Message // namespace -> messages
	acked    []string
	nacked   []string
}

func newTestMailboxStore() *testMailboxStore {
	return &testMailboxStore{
		messages: make(map[string][]*Message),
	}
}

func (m *testMailboxStore) Send(ctx context.Context, msg *Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[msg.ToNS] = append(m.messages[msg.ToNS], msg)
	return nil
}

func (m *testMailboxStore) Poll(ctx context.Context, namespace string, leaseTimeout time.Duration) (*Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	msgs := m.messages[namespace]
	if len(msgs) == 0 {
		return nil, nil
	}

	// Pop first message
	msg := msgs[0]
	m.messages[namespace] = msgs[1:]
	return msg, nil
}

func (m *testMailboxStore) Ack(ctx context.Context, messageID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acked = append(m.acked, messageID)
	return nil
}

func (m *testMailboxStore) Nack(ctx context.Context, messageID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nacked = append(m.nacked, messageID)
	return nil
}

func (m *testMailboxStore) getAcked() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.acked...)
}

func (m *testMailboxStore) getNacked() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.nacked...)
}

// TestIntegration_FullMessageFlow tests the complete message flow:
// Wake-up → Supervisor → Actor → Handler → Ack
func TestIntegration_FullMessageFlow(t *testing.T) {
	ctx := context.Background()
	mailbox := newTestMailboxStore()

	// Set up event bus to track events
	eventBus := NewEventBus()
	var receivedEvents []EventType
	var eventsMu sync.Mutex

	// Subscribe to events via channel
	eventCh := eventBus.SubscribeAll()
	go func() {
		for e := range eventCh {
			eventsMu.Lock()
			receivedEvents = append(receivedEvents, e.Type)
			eventsMu.Unlock()
		}
	}()

	// Create supervisor
	supervisor := NewSupervisor(mailbox,
		WithEventBus(eventBus),
		WithLeaseTimeout(time.Minute),
	)

	// Create and register actor with handler
	var handlerCalled atomic.Bool
	var receivedMsg *Message

	cfg := DefaultConfig("test-agent")
	actor := NewBaseActor(cfg)
	actor.RegisterHandler("agent.ask", func(ctx context.Context, msg *Message) (*Message, error) {
		handlerCalled.Store(true)
		receivedMsg = msg
		return nil, nil
	})

	if err := supervisor.Register(ctx, actor); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	if err := supervisor.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer supervisor.Stop(ctx)

	// Send a message via mailbox
	testMsg := &Message{
		ID:        "msg-001",
		FromNS:    "sender",
		ToNS:      "test-agent",
		Subject:   "agent.ask",
		Body:      []byte(`{"question": "test"}`),
		CreatedAt: time.Now(),
	}
	mailbox.Send(ctx, testMsg)

	// Trigger wake-up (simulates watcher notification)
	supervisor.HandleWakeUp(ctx, WakeUp{
		Namespace: "test-agent",
		Timestamp: time.Now(),
	})

	// Verify handler was called
	if !handlerCalled.Load() {
		t.Error("handler was not called")
	}

	// Verify message was received correctly
	if receivedMsg == nil {
		t.Fatal("received message is nil")
	}
	if receivedMsg.ID != "msg-001" {
		t.Errorf("message ID = %q, want %q", receivedMsg.ID, "msg-001")
	}
	if receivedMsg.Subject != "agent.ask" {
		t.Errorf("message Subject = %q, want %q", receivedMsg.Subject, "agent.ask")
	}

	// Verify message was acked
	acked := mailbox.getAcked()
	if len(acked) != 1 || acked[0] != "msg-001" {
		t.Errorf("acked messages = %v, want [msg-001]", acked)
	}

	// Give events time to be delivered (event bus is async)
	time.Sleep(10 * time.Millisecond)

	// Verify events were emitted
	eventsMu.Lock()
	defer eventsMu.Unlock()

	expectedEvents := []EventType{EventAgentStarted, EventMailReceived, EventMailAcked}
	if len(receivedEvents) < len(expectedEvents) {
		t.Errorf("received %d events, want at least %d: got %v", len(receivedEvents), len(expectedEvents), receivedEvents)
	}
}

// TestIntegration_ActorRegistrationAndUnregistration tests actor lifecycle.
func TestIntegration_ActorRegistrationAndUnregistration(t *testing.T) {
	ctx := context.Background()
	mailbox := newTestMailboxStore()
	supervisor := NewSupervisor(mailbox)

	// Register actor
	cfg := DefaultConfig("test-agent")
	actor := NewBaseActor(cfg)

	if err := supervisor.Register(ctx, actor); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Verify actor is registered
	if a, ok := supervisor.GetActor("test-agent"); !ok {
		t.Error("actor not found after registration")
	} else if a.State() != StateIdle {
		t.Errorf("actor state = %s, want %s", a.State(), StateIdle)
	}

	// List actors
	actors := supervisor.ListActors()
	if len(actors) != 1 || actors[0] != "test-agent" {
		t.Errorf("ListActors() = %v, want [test-agent]", actors)
	}

	// Unregister actor
	if err := supervisor.Unregister(ctx, "test-agent"); err != nil {
		t.Fatalf("Unregister() error: %v", err)
	}

	// Verify actor is gone
	if _, ok := supervisor.GetActor("test-agent"); ok {
		t.Error("actor still found after unregistration")
	}
}

// TestIntegration_ErrorHandling tests error handling with directives.
func TestIntegration_ErrorHandling(t *testing.T) {
	ctx := context.Background()
	mailbox := newTestMailboxStore()
	supervisor := NewSupervisor(mailbox)

	// Create actor that returns error and requests resume
	cfg := DefaultConfig("error-agent")
	actor := NewBaseActor(cfg,
		WithOnError(func(ctx context.Context, err error) Directive {
			return DirectiveResume
		}),
	)
	actor.RegisterHandler("agent.fail", func(ctx context.Context, msg *Message) (*Message, error) {
		return nil, context.DeadlineExceeded // Simulate error
	})

	if err := supervisor.Register(ctx, actor); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Send message that will fail
	testMsg := &Message{
		ID:      "fail-msg",
		FromNS:  "sender",
		ToNS:    "error-agent",
		Subject: "agent.fail",
	}
	mailbox.Send(ctx, testMsg)

	// Process message
	supervisor.HandleWakeUp(ctx, WakeUp{
		Namespace: "error-agent",
		Timestamp: time.Now(),
	})

	// Verify message was nacked (for retry)
	nacked := mailbox.getNacked()
	if len(nacked) != 1 || nacked[0] != "fail-msg" {
		t.Errorf("nacked messages = %v, want [fail-msg]", nacked)
	}

	// Verify no ack
	acked := mailbox.getAcked()
	if len(acked) != 0 {
		t.Errorf("acked messages = %v, want []", acked)
	}
}

// TestIntegration_WatcherToSupervisor tests the Watcher → Supervisor flow.
func TestIntegration_WatcherToSupervisor(t *testing.T) {
	// Create in-memory SQLite database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// Create mailbox table (minimal for trigger to work)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS mailbox (
			id TEXT PRIMARY KEY,
			from_ns TEXT,
			to_ns TEXT NOT NULL,
			type TEXT,
			payload BLOB,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("create mailbox table: %v", err)
	}

	ctx := context.Background()

	// Create watcher
	watcher := NewWatcher(db, WithPollInterval(10*time.Millisecond))
	if err := watcher.Start(ctx); err != nil {
		t.Fatalf("start watcher: %v", err)
	}
	defer watcher.Stop()

	// Insert a message to trigger notification
	_, err = db.Exec(`INSERT INTO mailbox (id, from_ns, to_ns, type) VALUES (?, ?, ?, ?)`,
		"test-msg", "sender", "test-ns", "agent.ask")
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}

	// Wait for wake-up
	select {
	case wake := <-watcher.WakeUps():
		if wake.Namespace != "test-ns" {
			t.Errorf("wake namespace = %q, want %q", wake.Namespace, "test-ns")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for wake-up")
	}
}

// TestIntegration_MultipleActors tests concurrent actors.
func TestIntegration_MultipleActors(t *testing.T) {
	ctx := context.Background()
	mailbox := newTestMailboxStore()
	supervisor := NewSupervisor(mailbox)

	// Track which actors processed messages
	var processed sync.Map

	// Register multiple actors
	namespaces := []string{"agent-1", "agent-2", "agent-3"}
	for _, ns := range namespaces {
		cfg := DefaultConfig(ns)
		actor := NewBaseActor(cfg)
		ns := ns // capture for closure
		actor.RegisterHandler("agent.ping", func(ctx context.Context, msg *Message) (*Message, error) {
			processed.Store(ns, true)
			return nil, nil
		})

		if err := supervisor.Register(ctx, actor); err != nil {
			t.Fatalf("Register(%s) error: %v", ns, err)
		}
	}

	// Send message to each actor
	for i, ns := range namespaces {
		msg := &Message{
			ID:      "msg-" + ns,
			FromNS:  "sender",
			ToNS:    ns,
			Subject: "agent.ping",
		}
		mailbox.Send(ctx, msg)

		// Process wake-up
		supervisor.HandleWakeUp(ctx, WakeUp{
			Namespace: ns,
			Timestamp: time.Now(),
		})

		// Verify processed
		if _, ok := processed.Load(ns); !ok {
			t.Errorf("actor %s did not process message", ns)
		}

		// Verify stats
		stats, ok := supervisor.GetStats(ns)
		if !ok {
			t.Errorf("no stats for %s", ns)
		} else if stats.MessagesProcessed != 1 {
			t.Errorf("stats.MessagesProcessed = %d, want 1", stats.MessagesProcessed)
		}

		_ = i // suppress unused warning
	}

	// Verify all messages acked
	acked := mailbox.getAcked()
	if len(acked) != 3 {
		t.Errorf("acked %d messages, want 3", len(acked))
	}
}

// TestIntegration_ActorStateTransitions tests state management.
func TestIntegration_ActorStateTransitions(t *testing.T) {
	ctx := context.Background()
	mailbox := newTestMailboxStore()
	supervisor := NewSupervisor(mailbox)

	var statesDuringProcessing []State
	var statesMu sync.Mutex

	cfg := DefaultConfig("state-agent")
	actor := NewBaseActor(cfg)
	actor.RegisterHandler("agent.check", func(ctx context.Context, msg *Message) (*Message, error) {
		statesMu.Lock()
		statesDuringProcessing = append(statesDuringProcessing, actor.State())
		statesMu.Unlock()
		return nil, nil
	})

	if err := supervisor.Register(ctx, actor); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Initial state should be Idle
	if actor.State() != StateIdle {
		t.Errorf("initial state = %s, want %s", actor.State(), StateIdle)
	}

	// Send and process message
	mailbox.Send(ctx, &Message{
		ID:      "state-msg",
		ToNS:    "state-agent",
		Subject: "agent.check",
	})

	supervisor.HandleWakeUp(ctx, WakeUp{
		Namespace: "state-agent",
		Timestamp: time.Now(),
	})

	// State during handler should be Processing
	statesMu.Lock()
	defer statesMu.Unlock()
	if len(statesDuringProcessing) != 1 || statesDuringProcessing[0] != StateProcessing {
		t.Errorf("state during processing = %v, want [processing]", statesDuringProcessing)
	}

	// State after processing should be back to Idle
	if actor.State() != StateIdle {
		t.Errorf("final state = %s, want %s", actor.State(), StateIdle)
	}
}

// TestIntegration_SequentialProcessing verifies actors process one message at a time.
func TestIntegration_SequentialProcessing(t *testing.T) {
	ctx := context.Background()
	mailbox := newTestMailboxStore()
	supervisor := NewSupervisor(mailbox)

	processingCount := atomic.Int32{}
	maxConcurrent := atomic.Int32{}

	cfg := DefaultConfig("seq-agent")
	actor := NewBaseActor(cfg)
	actor.RegisterHandler("agent.slow", func(ctx context.Context, msg *Message) (*Message, error) {
		current := processingCount.Add(1)
		// Track max concurrent
		for {
			old := maxConcurrent.Load()
			if current <= old {
				break
			}
			if maxConcurrent.CompareAndSwap(old, current) {
				break
			}
		}

		time.Sleep(10 * time.Millisecond)
		processingCount.Add(-1)
		return nil, nil
	})

	if err := supervisor.Register(ctx, actor); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Queue multiple messages
	for i := 0; i < 3; i++ {
		mailbox.Send(ctx, &Message{
			ID:      "seq-msg-" + string(rune('0'+i)),
			ToNS:    "seq-agent",
			Subject: "agent.slow",
		})
	}

	// Process all in sequence
	for i := 0; i < 3; i++ {
		supervisor.HandleWakeUp(ctx, WakeUp{
			Namespace: "seq-agent",
			Timestamp: time.Now(),
		})
	}

	// Verify sequential processing (max 1 concurrent)
	if maxConcurrent.Load() > 1 {
		t.Errorf("max concurrent = %d, want 1 (sequential processing)", maxConcurrent.Load())
	}
}

// TestIntegration_ReplyHandling tests request-reply pattern.
func TestIntegration_ReplyHandling(t *testing.T) {
	ctx := context.Background()
	mailbox := newTestMailboxStore()
	eventBus := NewEventBus()
	supervisor := NewSupervisor(mailbox, WithEventBus(eventBus))

	cfg := DefaultConfig("reply-agent")
	actor := NewBaseActor(cfg)

	actor.RegisterHandler("agent.ask", func(ctx context.Context, msg *Message) (*Message, error) {
		return &Message{
			Subject: "agent.reply",
			Body:    []byte(`{"answer": "42"}`),
		}, nil
	})

	if err := supervisor.Register(ctx, actor); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Send request
	mailbox.Send(ctx, &Message{
		ID:      "ask-msg",
		FromNS:  "requester",
		ToNS:    "reply-agent",
		Subject: "agent.ask",
	})

	supervisor.HandleWakeUp(ctx, WakeUp{
		Namespace: "reply-agent",
		Timestamp: time.Now(),
	})

	// Check that reply was sent back to requester
	mailbox.mu.Lock()
	defer mailbox.mu.Unlock()
	replies := mailbox.messages["requester"]

	if len(replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(replies))
	}

	reply := replies[0]
	if reply.FromNS != "reply-agent" {
		t.Errorf("reply FromNS = %q, want %q", reply.FromNS, "reply-agent")
	}
	if reply.Subject != "agent.reply" {
		t.Errorf("reply Subject = %q, want %q", reply.Subject, "agent.reply")
	}

	// Check that mail.sent event was emitted
	time.Sleep(10 * time.Millisecond)
	events := drainEvents(eventBus.SubscribeAll())
	foundSent := false
	for _, e := range events {
		if e.Type == EventMailSent && e.Source == "reply-agent" && e.Target == "requester" {
			foundSent = true
			break
		}
	}
	if !foundSent {
		t.Errorf("expected EventMailSent from reply-agent to requester, got %v", events)
	}
}

func drainEvents(ch <-chan Event) []Event {
	var events []Event
	for {
		select {
		case e := <-ch:
			events = append(events, e)
		case <-time.After(5 * time.Millisecond):
			return events
		}
	}
}
