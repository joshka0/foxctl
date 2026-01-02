//go:build sqlite_mattn

package actor

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/mailbox"
)

func TestNewSystem(t *testing.T) {
	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "actor-system-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	// Open mailbox store
	store, err := mailbox.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("open mailbox: %v", err)
	}
	defer store.Close()

	// Create system
	sys, err := NewSystem(store)
	if err != nil {
		t.Fatalf("new system: %v", err)
	}

	// Verify components are wired
	if sys.Store == nil {
		t.Error("store is nil")
	}
	if sys.Adapter == nil {
		t.Error("adapter is nil")
	}
	if sys.Watcher == nil {
		t.Error("watcher is nil")
	}
	if sys.Supervisor == nil {
		t.Error("supervisor is nil")
	}
	if sys.EventBus == nil {
		t.Error("eventbus is nil")
	}
}

func TestSystem_StartStop(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "actor-system-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	store, err := mailbox.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("open mailbox: %v", err)
	}
	defer store.Close()

	sys, err := NewSystem(store,
		WithWatcherOptions(WithPollInterval(10*time.Millisecond)),
	)
	if err != nil {
		t.Fatalf("new system: %v", err)
	}

	// Start system
	if err := sys.Start(ctx); err != nil {
		t.Fatalf("start system: %v", err)
	}

	// Give it a moment to run
	time.Sleep(50 * time.Millisecond)

	// Stop system
	stopCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := sys.Stop(stopCtx); err != nil {
		t.Fatalf("stop system: %v", err)
	}
}

func TestSystem_NotifyTriggerExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "actor-system-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	store, err := mailbox.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("open mailbox: %v", err)
	}
	defer store.Close()

	sys, err := NewSystem(store)
	if err != nil {
		t.Fatalf("new system: %v", err)
	}

	// Start system (this creates the notify table and trigger)
	if err := sys.Start(ctx); err != nil {
		t.Fatalf("start system: %v", err)
	}
	defer sys.Stop(ctx)

	// Verify mailbox_notify table exists
	db := store.DB()
	var tableName string
	err = db.QueryRowContext(ctx, `
		SELECT name FROM sqlite_master
		WHERE type='table' AND name='mailbox_notify'
	`).Scan(&tableName)
	if err != nil {
		if err == sql.ErrNoRows {
			t.Error("mailbox_notify table was not created")
		} else {
			t.Errorf("query table: %v", err)
		}
	}

	// Verify trigger exists
	var triggerName string
	err = db.QueryRowContext(ctx, `
		SELECT name FROM sqlite_master
		WHERE type='trigger' AND name='mailbox_notify_trigger'
	`).Scan(&triggerName)
	if err != nil {
		if err == sql.ErrNoRows {
			t.Error("mailbox_notify_trigger was not created")
		} else {
			t.Errorf("query trigger: %v", err)
		}
	}
}

func TestSystem_WithOptions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "actor-system-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	store, err := mailbox.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("open mailbox: %v", err)
	}
	defer store.Close()

	customStrategy := SupervisionStrategy{
		MaxRestarts:       5,
		RestartWindow:     2 * time.Minute,
		BackoffInitial:    500 * time.Millisecond,
		BackoffMax:        30 * time.Second,
		BackoffMultiplier: 1.5,
	}

	sys, err := NewSystem(store,
		WithSystemSupervisionStrategy(customStrategy),
		WithWatcherOptions(WithPollInterval(100*time.Millisecond)),
		WithAdapterOptions(WithDefaultVisibilityTimeout(time.Minute)),
	)
	if err != nil {
		t.Fatalf("new system: %v", err)
	}

	// Verify system was created with custom options
	if sys.Supervisor.strategy.MaxRestarts != 5 {
		t.Errorf("expected MaxRestarts 5, got %d", sys.Supervisor.strategy.MaxRestarts)
	}
}

// mockActorForSystem is a minimal actor implementation for system tests.
type mockActorForSystem struct {
	id       string
	ns       string
	state    State
	messages []*Message
}

func (a *mockActorForSystem) ID() string        { return a.id }
func (a *mockActorForSystem) Namespace() string { return a.ns }
func (a *mockActorForSystem) State() State      { return a.state }
func (a *mockActorForSystem) SetState(s State)  { a.state = s }

func (a *mockActorForSystem) Start(_ context.Context) error {
	a.state = StateIdle
	return nil
}

func (a *mockActorForSystem) Stop(_ context.Context) error {
	a.state = StateStopped
	return nil
}

func (a *mockActorForSystem) OnMailReceived(_ context.Context, msg *Message) error {
	a.messages = append(a.messages, msg)
	return nil
}

func (a *mockActorForSystem) OnTimeout(_ context.Context, _ TimerEvent) error {
	return nil
}

func (a *mockActorForSystem) OnError(_ context.Context, _ error) Directive {
	return DirectiveResume
}

func TestSystem_RegisterActor(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "actor-system-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	store, err := mailbox.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("open mailbox: %v", err)
	}
	defer store.Close()

	sys, err := NewSystem(store)
	if err != nil {
		t.Fatalf("new system: %v", err)
	}

	if err := sys.Start(ctx); err != nil {
		t.Fatalf("start system: %v", err)
	}
	defer sys.Stop(ctx)

	// Register an actor
	actor := &mockActorForSystem{id: "test-actor-1", ns: "test-actor"}
	if err := sys.Register(ctx, actor); err != nil {
		t.Fatalf("register actor: %v", err)
	}

	// Verify actor is registered
	registered, ok := sys.Supervisor.GetActor("test-actor")
	if !ok {
		t.Fatal("actor not found after registration")
	}
	if registered.Namespace() != "test-actor" {
		t.Errorf("expected namespace test-actor, got %s", registered.Namespace())
	}

	// Unregister
	if err := sys.Unregister(ctx, "test-actor"); err != nil {
		t.Fatalf("unregister actor: %v", err)
	}

	// Verify actor is gone
	_, ok = sys.Supervisor.GetActor("test-actor")
	if ok {
		t.Error("actor still registered after unregister")
	}
}

// Verify system files are created in the expected location
func TestSystem_DatabaseLocation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "actor-system-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	store, err := mailbox.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("open mailbox: %v", err)
	}
	defer store.Close()

	// Verify mailbox.db was created
	dbPath := filepath.Join(tmpDir, "mailbox.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("mailbox.db was not created")
	}
}
