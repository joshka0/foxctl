package store

import (
	"context"
	"database/sql"
	"fmt"
)

// MigrateSchema creates the DB-authoritative evolve tables.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("evolve store migrate: nil db")
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS evolve_runs (
			id TEXT PRIMARY KEY,
			workspace_path TEXT NOT NULL,
			target_path TEXT NOT NULL,
			benchmark_command TEXT NOT NULL,
			metric TEXT NOT NULL CHECK (metric IN ('max','min')),
			status TEXT NOT NULL CHECK (status IN ('active','paused','completed','archived')),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_evolve_runs_workspace ON evolve_runs(workspace_path)`,
		`CREATE INDEX IF NOT EXISTS idx_evolve_runs_status ON evolve_runs(status)`,
		`CREATE TABLE IF NOT EXISTS evolve_active_runs (
			workspace_path TEXT PRIMARY KEY,
			run_id TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES evolve_runs(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_evolve_active_runs_run ON evolve_active_runs(run_id)`,
		`CREATE TABLE IF NOT EXISTS evolve_nodes (
			id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL,
			parent_id TEXT,
			status TEXT NOT NULL CHECK (status IN ('root','pending','active','committed','evaluated','failed','discarded','pruned')),
			hypothesis TEXT,
			score REAL,
			eval_epoch INTEGER NOT NULL DEFAULT 0,
			branch TEXT,
			worktree_path TEXT,
			commit_sha TEXT,
			pruned_reason TEXT,
			current_attempt INTEGER NOT NULL DEFAULT 0,
			evaluated_attempts INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES evolve_runs(id) ON DELETE CASCADE,
			FOREIGN KEY(parent_id) REFERENCES evolve_nodes(id) ON DELETE SET NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_evolve_nodes_run_parent ON evolve_nodes(run_id, parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_evolve_nodes_run_status ON evolve_nodes(run_id, status)`,
		`CREATE TABLE IF NOT EXISTS evolve_gates (
			id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL,
			node_id TEXT NOT NULL,
			name TEXT NOT NULL,
			command TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES evolve_runs(id) ON DELETE CASCADE,
			FOREIGN KEY(node_id) REFERENCES evolve_nodes(id) ON DELETE CASCADE,
			UNIQUE(node_id, name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_evolve_gates_run ON evolve_gates(run_id)`,
		`CREATE INDEX IF NOT EXISTS idx_evolve_gates_node ON evolve_gates(node_id)`,
		`CREATE TABLE IF NOT EXISTS evolve_attempts (
			id TEXT PRIMARY KEY,
			node_id TEXT NOT NULL,
			attempt_no INTEGER NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('active','completed','failed')),
			score REAL,
			benchmark_artifact TEXT,
			trace_artifact TEXT,
			diff_artifact TEXT,
			error TEXT,
			started_at TEXT NOT NULL,
			finished_at TEXT,
			FOREIGN KEY(node_id) REFERENCES evolve_nodes(id) ON DELETE CASCADE,
			UNIQUE(node_id, attempt_no)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_evolve_attempts_node_no ON evolve_attempts(node_id, attempt_no)`,
		`CREATE TABLE IF NOT EXISTS evolve_gate_results (
			attempt_id TEXT NOT NULL,
			gate_name TEXT NOT NULL,
			source_node_id TEXT NOT NULL,
			passed TEXT NOT NULL CHECK (passed IN ('true','false')),
			return_code INTEGER,
			log_artifact TEXT,
			PRIMARY KEY(attempt_id, gate_name),
			FOREIGN KEY(attempt_id) REFERENCES evolve_attempts(id) ON DELETE CASCADE,
			FOREIGN KEY(source_node_id) REFERENCES evolve_nodes(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_evolve_gate_results_source_node ON evolve_gate_results(source_node_id)`,
		`CREATE TABLE IF NOT EXISTS evolve_annotations (
			id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL,
			node_id TEXT,
			task_id TEXT,
			analysis TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES evolve_runs(id) ON DELETE CASCADE,
			FOREIGN KEY(node_id) REFERENCES evolve_nodes(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_evolve_annotations_run ON evolve_annotations(run_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_evolve_annotations_node ON evolve_annotations(node_id, created_at)`,
		`CREATE TABLE IF NOT EXISTS evolve_infra_events (
			id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL,
			message TEXT NOT NULL,
			breaking TEXT NOT NULL CHECK (breaking IN ('true','false')),
			created_at TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES evolve_runs(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_evolve_infra_events_run ON evolve_infra_events(run_id, created_at)`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("evolve store migrate: %w", err)
		}
	}
	return nil
}
