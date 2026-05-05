package actor

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test db: %v", err)
	}

	// Create mailbox table (watcher depends on it for trigger)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS mailbox (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			to_ns TEXT NOT NULL,
			from_ns TEXT,
			subject TEXT,
			body TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create mailbox table: %v", err)
	}

	return db
}

func TestNewWatcher(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	w := NewWatcher(db)

	if w == nil {
		t.Fatal("NewWatcher() returned nil")
	}
	if w.interval != 50*time.Millisecond {
		t.Errorf("interval = %v, want 50ms", w.interval)
	}
}

func TestWatcher_WithPollInterval(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	w := NewWatcher(db, WithPollInterval(100*time.Millisecond))

	if w.interval != 100*time.Millisecond {
		t.Errorf("interval = %v, want 100ms", w.interval)
	}
}

func TestWatcher_WithWakeUpBuffer(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	w := NewWatcher(db, WithWakeUpBuffer(50))

	// Test by filling the buffer
	for i := 0; i < 50; i++ {
		select {
		case w.wakeUpCh <- WakeUp{Namespace: "test"}:
		default:
			t.Errorf("Buffer full at %d, expected 50", i)
			return
		}
	}
}

func TestWatcher_Start_Stop(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	w := NewWatcher(db)

	ctx := context.Background()
	err := w.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Verify schema was created
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='mailbox_notify'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check schema: %v", err)
	}
	if count != 1 {
		t.Error("mailbox_notify table not created")
	}

	err = w.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}

func TestWatcher_WakeUps(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	w := NewWatcher(db)
	ch := w.WakeUps()

	if ch == nil {
		t.Fatal("WakeUps() returned nil channel")
	}
}

func TestWatcher_ensureSchema(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	w := NewWatcher(db)

	ctx := context.Background()
	err := w.ensureSchema(ctx)
	if err != nil {
		t.Fatalf("ensureSchema() error = %v", err)
	}

	// Verify table exists
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='mailbox_notify'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check table: %v", err)
	}
	if count != 1 {
		t.Error("mailbox_notify table not created")
	}

	// Verify trigger exists
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name='mailbox_notify_trigger'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check trigger: %v", err)
	}
	if count != 1 {
		t.Error("mailbox_notify_trigger not created")
	}
}

func TestWatcher_TriggerFiresOnMailboxInsert(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	w := NewWatcher(db)
	ctx := context.Background()

	err := w.ensureSchema(ctx)
	if err != nil {
		t.Fatalf("ensureSchema() error = %v", err)
	}

	// Insert into mailbox
	_, err = db.Exec("INSERT INTO mailbox (to_ns, subject) VALUES ('actor-1', 'test')")
	if err != nil {
		t.Fatalf("Failed to insert into mailbox: %v", err)
	}

	// Check notify table has entry
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM mailbox_notify WHERE to_ns = 'actor-1'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check notify table: %v", err)
	}
	if count != 1 {
		t.Errorf("notify table count = %d, want 1", count)
	}
}

func TestWatcher_Cleanup(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	w := NewWatcher(db)
	ctx := context.Background()

	err := w.ensureSchema(ctx)
	if err != nil {
		t.Fatalf("ensureSchema() error = %v", err)
	}

	// Use Go time format for consistent comparison with Cleanup function
	oldTime := time.Now().Add(-2 * time.Hour).Format("2006-01-02 15:04:05")
	futureTime := time.Now().Add(time.Hour).Format("2006-01-02 15:04:05")

	// Insert old notification
	_, err = db.Exec("INSERT INTO mailbox_notify (to_ns, created_at) VALUES ('actor-1', ?)", oldTime)
	if err != nil {
		t.Fatalf("Failed to insert old notification: %v", err)
	}

	// Insert future notification
	_, err = db.Exec("INSERT INTO mailbox_notify (to_ns, created_at) VALUES ('actor-2', ?)", futureTime)
	if err != nil {
		t.Fatalf("Failed to insert future notification: %v", err)
	}

	// Cleanup notifications older than 1 hour
	deleted, err := w.Cleanup(ctx, time.Hour)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if deleted != 1 {
		t.Errorf("Cleanup() deleted = %d, want 1", deleted)
	}

	// Verify only future notification remains
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM mailbox_notify").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count: %v", err)
	}
	if count != 1 {
		t.Errorf("remaining count = %d, want 1", count)
	}
}

func TestWakeUp(t *testing.T) {
	now := time.Now()
	wake := WakeUp{
		Namespace: "actor-1",
		Timestamp: now,
	}

	if wake.Namespace != "actor-1" {
		t.Errorf("Namespace = %q, want actor-1", wake.Namespace)
	}
	if wake.Timestamp != now {
		t.Errorf("Timestamp = %v, want %v", wake.Timestamp, now)
	}
}

func TestWatcher_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := setupTestDB(t)
	defer db.Close()

	w := NewWatcher(db, WithPollInterval(10*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := w.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		if err := w.Stop(); err != nil {
			t.Errorf("stop watcher: %v", err)
		}
	}()

	// Insert into mailbox (trigger fires)
	_, err = db.Exec("INSERT INTO mailbox (to_ns, subject) VALUES ('actor-1', 'test')")
	if err != nil {
		t.Fatalf("Failed to insert: %v", err)
	}

	// Wait for wake-up
	select {
	case wake := <-w.WakeUps():
		if wake.Namespace != "actor-1" {
			t.Errorf("WakeUp namespace = %q, want actor-1", wake.Namespace)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("Timed out waiting for wake-up")
	}
}
