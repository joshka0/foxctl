package orchestration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// MigrateSchema creates orchestration projection tables.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("v2 orchestration migrate: nil db")
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS v2_orchestration_cards (
			issue_id TEXT PRIMARY KEY,
			workspace_id TEXT,
			issue_identifier TEXT,
			title TEXT,
			state TEXT NOT NULL,
			lane TEXT NOT NULL,
			tracker_state TEXT,
			policy_status TEXT,
			last_outcome TEXT,
			eligibility TEXT,
			denial_reason TEXT,
			suggestion TEXT,
			run_id TEXT,
			agent_id TEXT,
			actor_id TEXT,
			attempt INTEGER,
			retry_due_at TEXT,
			last_event_type TEXT,
			last_event_at TEXT,
			last_request_id TEXT,
			last_event_id TEXT NOT NULL,
			last_stream_version INTEGER NOT NULL,
			updated_at TEXT NOT NULL,
			archived_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_orchestration_cards_workspace ON v2_orchestration_cards(workspace_id)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_orchestration_cards_lane ON v2_orchestration_cards(lane)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_orchestration_cards_tracker_state ON v2_orchestration_cards(tracker_state)`,
		`CREATE TABLE IF NOT EXISTS v2_orchestration_applied_events (
			event_id TEXT PRIMARY KEY,
			command TEXT,
			scope_id TEXT,
			request_id TEXT,
			applied_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_v2_orchestration_applied_request
			ON v2_orchestration_applied_events(command, scope_id, request_id)
			WHERE request_id IS NOT NULL AND request_id <> ''`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("v2 orchestration migrate: %w", err)
		}
	}
	for _, stmt := range []string{
		`ALTER TABLE v2_orchestration_cards ADD COLUMN agent_id TEXT`,
		`ALTER TABLE v2_orchestration_cards ADD COLUMN archived_at TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil && !isDuplicateColumnError(err) {
			return fmt.Errorf("v2 orchestration migrate: %w", err)
		}
	}
	return nil
}

func isDuplicateColumnError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "duplicate column") || strings.Contains(msg, "already exists")
}
