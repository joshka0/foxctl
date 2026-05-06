package turnrequests

import (
	"context"
	"database/sql"
	"fmt"
)

// MigrateSchema creates turn request registry tables.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("v2 turn requests migrate: nil db")
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS v2_turn_requests (
			run_id TEXT NOT NULL,
			request_id TEXT NOT NULL,
			turn_id TEXT NOT NULL,
			status TEXT NOT NULL,
			output_json TEXT NOT NULL DEFAULT '',
			error_json TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL,
			completed_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			PRIMARY KEY (run_id, request_id),
			CHECK (status IN ('running', 'succeeded', 'failed', 'canceled'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_turn_requests_turn
			ON v2_turn_requests(run_id, turn_id)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_turn_requests_status_updated_at
			ON v2_turn_requests(status, updated_at)`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("v2 turn requests migrate: %w", err)
		}
	}
	return nil
}
