package events

import (
	"context"
	"database/sql"
	"fmt"
)

// MigrateSchema creates append-only v2 event tables and indexes.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("v2 events migrate: nil db")
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS v2_events (
			id TEXT PRIMARY KEY,
			stream_id TEXT NOT NULL,
			stream_type TEXT NOT NULL,
			stream_version INTEGER NOT NULL,
			sequence INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			occurred_at TEXT NOT NULL,
			correlation_id TEXT,
			causation_id TEXT,
			actor_id TEXT,
			request_id TEXT,
			command TEXT,
			payload TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_v2_events_stream_version
			ON v2_events(stream_id, stream_type, stream_version)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_v2_events_stream_sequence
			ON v2_events(stream_id, stream_type, sequence)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_events_type_time
			ON v2_events(event_type, occurred_at)`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("v2 events migrate: %w", err)
		}
	}
	return nil
}
