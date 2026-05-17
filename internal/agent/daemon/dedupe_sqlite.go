package daemon

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
)

// SQLiteDedupeStore implements DedupeStore using SQLite persistence.
type SQLiteDedupeStore struct {
	db    *sql.DB
	close func() error
}

// OpenSQLiteDedupeStore opens or creates the dedupe database at the provided storage root and returns a SQLiteDedupeStore.
// The returned store provides methods to query and update deduplication state and must be closed with Close when no longer needed.
// If opening or initializing the database fails, an error describing the failure is returned.
func OpenSQLiteDedupeStore(ctx context.Context, storageRoot string) (*SQLiteDedupeStore, error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, storageRoot, "DAEMON_DEDUPE", "daemon_dedupe.db", migrateDedupe)
	if err != nil {
		return nil, fmt.Errorf("open dedupe db: %w", err)
	}

	return &SQLiteDedupeStore{db: db, close: closeFn}, nil
}

// migrateDedupe creates the daemon_dedupe table and its processed_at index if they do not exist.
// It returns any error encountered while executing the migration SQL.
// MigrateDedupe runs the daemon dedupe store DDL migrations against the given database.
func MigrateDedupe(ctx context.Context, db *sql.DB) error {
	return migrateDedupe(ctx, db)
}

func migrateDedupe(ctx context.Context, db *sql.DB) error {
	schema := `
       CREATE TABLE IF NOT EXISTS daemon_dedupe (
           agent_id     TEXT NOT NULL,
           message_id   TEXT NOT NULL,
           processed_at INTEGER NOT NULL,
           PRIMARY KEY (agent_id, message_id)
       );
       CREATE INDEX IF NOT EXISTS idx_dedupe_processed_at ON daemon_dedupe(processed_at);
       `
	_, err := db.ExecContext(ctx, schema)
	return err
}

// IsProcessed checks if a message has already been processed.
func (s *SQLiteDedupeStore) IsProcessed(ctx context.Context, agentID, messageID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM daemon_dedupe WHERE agent_id = $1 AND message_id = $2`,
		agentID, messageID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check processed: %w", err)
	}
	return count > 0, nil
}

// MarkProcessed marks a message as processed.
func (s *SQLiteDedupeStore) MarkProcessed(ctx context.Context, agentID, messageID string) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO daemon_dedupe (agent_id, message_id, processed_at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		agentID, messageID, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("mark processed: %w", err)
	}
	return nil
}

// Cleanup removes dedupe records older than the given duration.
func (s *SQLiteDedupeStore) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan).Unix()
	result, err := s.db.ExecContext(
		ctx,
		`DELETE FROM daemon_dedupe WHERE processed_at < $1`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("cleanup dedupe: %w", err)
	}
	return result.RowsAffected()
}

// Close closes the database connection.
func (s *SQLiteDedupeStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}
