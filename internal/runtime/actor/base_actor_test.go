package actor

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewBaseActor(t *testing.T) {
	cfg := DefaultConfig("test-ns")
	cfg.ID = "test-actor-1"

	actor := NewBaseActor(cfg)

	if actor.ID() != "test-actor-1" {
		t.Errorf("expected ID test-actor-1, got %s", actor.ID())
	}
	if actor.Namespace() != "test-ns" {
		t.Errorf("expected namespace test-ns, got %s", actor.Namespace())
	}
	if actor.State() != StateStopped {
		t.Errorf("expected initial state Stopped, got %s", actor.State())
	}
}

func TestBaseActor_StartStop(t *testing.T) {
	cfg := DefaultConfig("test-ns")
	actor := NewBaseActor(cfg)

	ctx := context.Background()

	// Start
	if err := actor.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if actor.State() != StateIdle {
		t.Errorf("expected state Idle after start, got %s", actor.State())
	}

	// Stop
	if err := actor.Stop(ctx); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
	if actor.State() != StateStopped {
		t.Errorf("expected state Stopped after stop, got %s", actor.State())
	}
}

func TestBaseActor_StartStopHooks(t *testing.T) {
	startCalled := false
	stopCalled := false

	cfg := DefaultConfig("test-ns")
	actor := NewBaseActor(
		cfg,
		WithOnStart(func(ctx context.Context) error {
			startCalled = true
			return nil
		}),
		WithOnStop(func(ctx context.Context) error {
			stopCalled = true
			return nil
		}),
	)

	ctx := context.Background()

	if err := actor.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if !startCalled {
		t.Error("onStart hook was not called")
	}

	if err := actor.Stop(ctx); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
	if !stopCalled {
		t.Error("onStop hook was not called")
	}
}

func TestBaseActor_StartHookError(t *testing.T) {
	expectedErr := errors.New("start failed")
	cfg := DefaultConfig("test-ns")
	actor := NewBaseActor(
		cfg,
		WithOnStart(func(ctx context.Context) error {
			return expectedErr
		}),
	)

	ctx := context.Background()
	err := actor.Start(ctx)
	if err == nil {
		t.Fatal("expected error from Start()")
	}
	if actor.State() != StateError {
		t.Errorf("expected state Error after failed start, got %s", actor.State())
	}
}

func TestBaseActor_RegisterHandler(t *testing.T) {
	cfg := DefaultConfig("test-ns")
	actor := NewBaseActor(cfg)

	handlerCalled := false
	actor.RegisterHandler("agent.ask", func(ctx context.Context, msg *Message) (*Message, error) {
		handlerCalled = true
		return nil, nil
	})

	ctx := context.Background()
	msg := &Message{
		ID:      "msg-1",
		Subject: "agent.ask",
		FromNS:  "sender",
		ToNS:    "test-ns",
	}

	if err := actor.OnMailReceived(ctx, msg); err != nil {
		t.Fatalf("OnMailReceived() error: %v", err)
	}

	if !handlerCalled {
		t.Error("handler was not called")
	}
}

func TestBaseActor_UnknownSubject(t *testing.T) {
	cfg := DefaultConfig("test-ns")
	actor := NewBaseActor(cfg)

	ctx := context.Background()
	msg := &Message{
		ID:      "msg-1",
		Subject: "unknown.subject",
		FromNS:  "sender",
		ToNS:    "test-ns",
	}

	err := actor.OnMailReceived(ctx, msg)
	if err == nil {
		t.Fatal("expected error for unknown subject")
	}
}

func TestBaseActor_HandlerWithReply(t *testing.T) {
	cfg := DefaultConfig("test-ns")

	var sentReply *Message
	actor := NewBaseActor(
		cfg,
		WithReplySender(func(ctx context.Context, msg *Message) error {
			sentReply = msg
			return nil
		}),
	)

	actor.RegisterHandler("agent.ask", func(ctx context.Context, msg *Message) (*Message, error) {
		return &Message{
			Subject: "agent.reply",
			Body:    []byte(`{"result": "done"}`),
		}, nil
	})

	ctx := context.Background()
	msg := &Message{
		ID:      "msg-1",
		Subject: "agent.ask",
		FromNS:  "sender",
		ToNS:    "test-ns",
	}

	if err := actor.OnMailReceived(ctx, msg); err != nil {
		t.Fatalf("OnMailReceived() error: %v", err)
	}

	if sentReply == nil {
		t.Fatal("reply was not sent")
	}
	if sentReply.FromNS != "test-ns" {
		t.Errorf("expected reply FromNS test-ns, got %s", sentReply.FromNS)
	}
	if sentReply.ToNS != "sender" {
		t.Errorf("expected reply ToNS sender, got %s", sentReply.ToNS)
	}
}

func TestBaseActor_OnError(t *testing.T) {
	cfg := DefaultConfig("test-ns")

	// Default behavior: resume
	actor := NewBaseActor(cfg)
	ctx := context.Background()

	directive := actor.OnError(ctx, errors.New("some error"))
	if directive != DirectiveResume {
		t.Errorf("expected default directive Resume, got %s", directive)
	}

	// Custom error handler
	actor = NewBaseActor(
		cfg,
		WithOnError(func(ctx context.Context, err error) Directive {
			return DirectiveRestart
		}),
	)

	directive = actor.OnError(ctx, errors.New("some error"))
	if directive != DirectiveRestart {
		t.Errorf("expected custom directive Restart, got %s", directive)
	}
}

func TestBaseActor_OnTimeout(t *testing.T) {
	cfg := DefaultConfig("test-ns")

	timeoutHandled := false
	actor := NewBaseActor(
		cfg,
		WithOnTimeout(func(ctx context.Context, event TimerEvent) error {
			timeoutHandled = true
			if event.Name != "test-timer" {
				t.Errorf("expected timer name test-timer, got %s", event.Name)
			}
			return nil
		}),
	)

	ctx := context.Background()
	event := TimerEvent{
		Name:      "test-timer",
		Deadline:  time.Now(),
		Namespace: "test-ns",
	}

	if err := actor.OnTimeout(ctx, event); err != nil {
		t.Fatalf("OnTimeout() error: %v", err)
	}

	if !timeoutHandled {
		t.Error("timeout handler was not called")
	}
}

func TestBaseActor_SetTimer(t *testing.T) {
	cfg := DefaultConfig("test-ns")
	actor := NewBaseActor(cfg)

	// Set a timer
	actor.SetTimer("test-timer", 100*time.Millisecond, nil)

	// Verify timer exists
	actor.mu.RLock()
	_, exists := actor.timers["test-timer"]
	actor.mu.RUnlock()

	if !exists {
		t.Error("timer was not set")
	}

	// Cancel timer
	if !actor.CancelTimer("test-timer") {
		t.Error("CancelTimer returned false")
	}

	// Verify timer is gone
	actor.mu.RLock()
	_, exists = actor.timers["test-timer"]
	actor.mu.RUnlock()

	if exists {
		t.Error("timer was not cancelled")
	}
}

func TestBaseActor_CancelNonexistentTimer(t *testing.T) {
	cfg := DefaultConfig("test-ns")
	actor := NewBaseActor(cfg)

	if actor.CancelTimer("nonexistent") {
		t.Error("CancelTimer should return false for nonexistent timer")
	}
}

func TestBaseActor_UnregisterHandler(t *testing.T) {
	cfg := DefaultConfig("test-ns")
	actor := NewBaseActor(cfg)

	actor.RegisterHandler("agent.ask", func(ctx context.Context, msg *Message) (*Message, error) {
		return nil, nil
	})

	actor.UnregisterHandler("agent.ask")

	ctx := context.Background()
	msg := &Message{
		ID:      "msg-1",
		Subject: "agent.ask",
	}

	err := actor.OnMailReceived(ctx, msg)
	if err == nil {
		t.Error("expected error after unregistering handler")
	}
}

func TestBaseActor_ReplyWithoutSender(t *testing.T) {
	cfg := DefaultConfig("test-ns")
	actor := NewBaseActor(cfg) // No reply sender configured

	ctx := context.Background()
	original := &Message{FromNS: "sender"}
	reply := &Message{Subject: "reply"}

	err := actor.Reply(ctx, original, reply)
	if err == nil {
		t.Error("expected error when no reply sender configured")
	}
}

func TestBaseActor_SetState(t *testing.T) {
	cfg := DefaultConfig("test-ns")
	actor := NewBaseActor(cfg)

	states := []State{StateIdle, StateProcessing, StateError, StateStopped}
	for _, s := range states {
		actor.SetState(s)
		if actor.State() != s {
			t.Errorf("expected state %s, got %s", s, actor.State())
		}
	}
}

func TestBaseActor_Config(t *testing.T) {
	cfg := Config{
		ID:           "test-id",
		Namespace:    "test-ns",
		Role:         "coder",
		LeaseTimeout: 10 * time.Minute,
		MaxRetries:   5,
		Metadata:     map[string]any{"key": "value"},
	}
	actor := NewBaseActor(cfg)

	got := actor.Config()
	if got.ID != cfg.ID {
		t.Errorf("expected ID %s, got %s", cfg.ID, got.ID)
	}
	if got.Namespace != cfg.Namespace {
		t.Errorf("expected Namespace %s, got %s", cfg.Namespace, got.Namespace)
	}
	if got.Role != cfg.Role {
		t.Errorf("expected Role %s, got %s", cfg.Role, got.Role)
	}
}
