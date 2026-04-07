package workers

import (
	"context"
	"database/sql"
	"fmt"
)

// MigrateSchema creates runtime worker registry tables.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("v2 workers migrate: nil db")
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS v2_runtime_workers (
			worker_id TEXT PRIMARY KEY,
			backend_kind TEXT NOT NULL,
			backend_worker_ref TEXT,
			agent_id TEXT,
			run_id TEXT,
			session_id TEXT,
			parent_agent_id TEXT,
			parent_worker_id TEXT,
			workspace_id TEXT,
			role TEXT,
			status TEXT NOT NULL,
			tag TEXT,
			pid TEXT,
			started_at TEXT,
			updated_at TEXT NOT NULL,
			heartbeat_at TEXT,
			stop_reason TEXT,
			exit_code INTEGER NOT NULL DEFAULT 0,
			metadata_json TEXT,
			raw_state BLOB
		)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_runtime_workers_agent_id ON v2_runtime_workers(agent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_runtime_workers_parent_agent_id ON v2_runtime_workers(parent_agent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_runtime_workers_parent_worker_id ON v2_runtime_workers(parent_worker_id)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_runtime_workers_run_id ON v2_runtime_workers(run_id)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_runtime_workers_status ON v2_runtime_workers(status)`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("v2 workers migrate: %w", err)
		}
	}
	return nil
}
