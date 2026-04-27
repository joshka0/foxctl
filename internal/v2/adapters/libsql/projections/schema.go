package projections

import (
	"context"
	"database/sql"
	"fmt"
)

// MigrateSchema creates projection tables for run/agent state materialization.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("v2 projections migrate: nil db")
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS v2_run_state (
			run_id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			last_event_id TEXT NOT NULL,
			last_stream_version INTEGER NOT NULL,
			command TEXT,
			request_id TEXT,
			actor_id TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_run_state_status ON v2_run_state(status)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_run_state_request_id ON v2_run_state(request_id)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_run_state_command ON v2_run_state(command)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_run_state_actor_id ON v2_run_state(actor_id)`,
		`CREATE TABLE IF NOT EXISTS v2_agent_state (
			agent_id TEXT PRIMARY KEY,
			state TEXT NOT NULL,
			last_event_id TEXT NOT NULL,
			last_stream_version INTEGER NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_agent_state_state ON v2_agent_state(state)`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("v2 projections migrate: %w", err)
		}
	}
	return nil
}
