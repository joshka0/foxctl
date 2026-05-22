package actor

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// WakeUp signals that an actor has pending work.
//
// Contract: WakeUp is signal-only; actual message consumption
// happens via mailbox.Poll() with lease semantics.
type WakeUp struct {
	Namespace string
	Timestamp time.Time
}

// Watcher monitors the mailbox for new messages and sends wake-up signals.
//
// Implementation uses SQLite triggers to detect INSERTs:
// 1. Trigger fires on mailbox INSERT → mailbox_notify row created
// 2. Watcher polls notify table every 50ms
// 3. Watcher sends WakeUp{namespace} to supervisor
// 4. Supervisor calls mailbox.Poll() to atomically claim message
//
// This provides ~50ms reactive notifications while preserving
// the existing lease/ack semantics for crash safety.
type Watcher struct {
	db       *sql.DB
	wakeUpCh chan WakeUp
	interval time.Duration
	log      zerolog.Logger

	ctx    context.Context
	cancel context.CancelFunc
}

// WatcherOption configures a Watcher.
type WatcherOption func(*Watcher)

// WithPollInterval sets the notify table poll interval.
func WithPollInterval(d time.Duration) WatcherOption {
	return func(w *Watcher) {
		w.interval = d
	}
}

// WithWakeUpBuffer sets the wake-up channel buffer size.
func WithWakeUpBuffer(size int) WatcherOption {
	return func(w *Watcher) {
		w.wakeUpCh = make(chan WakeUp, size)
	}
}

// WithWatcherLogger sets the watcher logger.
func WithWatcherLogger(log zerolog.Logger) WatcherOption {
	return func(w *Watcher) {
		w.log = log
	}
}

// NewWatcher creates a new Watcher.
func NewWatcher(db *sql.DB, opts ...WatcherOption) *Watcher {
	w := &Watcher{
		db:       db,
		wakeUpCh: make(chan WakeUp, 100),
		interval: 50 * time.Millisecond,
		log:      zerolog.Nop(),
	}

	for _, opt := range opts {
		opt(w)
	}

	return w
}

// WakeUps returns the channel for receiving wake-up signals.
func (w *Watcher) WakeUps() <-chan WakeUp {
	return w.wakeUpCh
}

// Start starts the watcher.
func (w *Watcher) Start(ctx context.Context) error {
	// Ensure schema exists
	if err := w.ensureSchema(ctx); err != nil {
		return fmt.Errorf("ensure schema: %w", err)
	}

	w.ctx, w.cancel = context.WithCancel(ctx)

	go w.run()

	return nil
}

// Stop stops the watcher.
func (w *Watcher) Stop() error {
	if w.cancel != nil {
		w.cancel()
	}
	return nil
}

// ensureSchema creates the notify table and trigger if they don't exist.
func (w *Watcher) ensureSchema(ctx context.Context) error {
	schema := `
		-- Notify table (minimal, just wake-up signal)
		CREATE TABLE IF NOT EXISTS mailbox_notify (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			to_ns TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		-- Index for efficient polling
		CREATE INDEX IF NOT EXISTS idx_mailbox_notify_created
		ON mailbox_notify(created_at);
	`

	if _, err := w.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	exists, err := w.sqliteObjectExists(ctx, "trigger", "mailbox_notify_trigger")
	if err != nil {
		return fmt.Errorf("check mailbox notify trigger: %w", err)
	}
	if exists {
		return nil
	}

	// Create trigger separately: older SQLite variants do not consistently
	// support CREATE TRIGGER IF NOT EXISTS.
	trigger := `
		CREATE TRIGGER mailbox_notify_trigger
		AFTER INSERT ON mailbox
		BEGIN
			INSERT INTO mailbox_notify (to_ns) VALUES (NEW.to_ns);
		END;
	`
	if _, err := w.db.ExecContext(ctx, trigger); err != nil {
		return fmt.Errorf("create mailbox notify trigger: %w", err)
	}

	return nil
}

func (w *Watcher) sqliteObjectExists(ctx context.Context, kind, name string) (bool, error) {
	var count int
	err := w.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = ? AND name = ?
	`, kind, name).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// run is the main watcher loop.
func (w *Watcher) run() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.checkNotifications()
		}
	}
}

// checkNotifications polls the notify table and sends wake-ups.
func (w *Watcher) checkNotifications() {
	ctx, cancel := context.WithTimeout(w.ctx, w.interval)
	defer cancel()

	// Get all notification IDs grouped by namespace
	rows, err := w.db.QueryContext(ctx, `
		SELECT id, to_ns
		FROM mailbox_notify
		ORDER BY id
	`)
	if err != nil {
		w.log.Error().Err(err).Msg("checkNotifications: query failed")
		return // retry next tick
	}
	defer rows.Close()

	signaled := make(map[string]bool)
	var toDelete []int64
	now := time.Now()

	for rows.Next() {
		var id int64
		var ns string
		if err := rows.Scan(&id, &ns); err != nil {
			w.log.Error().Err(err).Msg("checkNotifications: scan failed")
			continue
		}

		// Only signal once per namespace per poll
		if !signaled[ns] {
			select {
			case w.wakeUpCh <- WakeUp{Namespace: ns, Timestamp: now}:
				signaled[ns] = true
			default:
				// Channel full, skip this namespace for now
				w.log.Warn().Str("namespace", ns).Msg("checkNotifications: wake-up channel full")
				continue
			}
		}

		// Only delete if we successfully signaled this namespace
		if signaled[ns] {
			toDelete = append(toDelete, id)
		}
	}

	// Clean up processed notifications
	if len(toDelete) > 0 {
		w.cleanupNotifications(ctx, toDelete)
	}
}

// cleanupNotifications removes processed notifications.
func (w *Watcher) cleanupNotifications(ctx context.Context, ids []int64) {
	if len(ids) == 0 {
		return
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("DELETE FROM mailbox_notify WHERE id IN (%s)", strings.Join(placeholders, ","))
	if _, err := w.db.ExecContext(ctx, query, args...); err != nil {
		w.log.Error().Err(err).Msg("cleanupNotifications: delete failed")
	}
}

// Cleanup removes old notifications (can be called periodically).
func (w *Watcher) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	result, err := w.db.ExecContext(ctx, `
		DELETE FROM mailbox_notify
		WHERE created_at < ?
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
