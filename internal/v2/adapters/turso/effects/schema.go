package effects

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
)

// MigrateSchema creates durable effect journal tables.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("v2 effects migrate: nil db")
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS v2_model_effects (
			run_id TEXT NOT NULL,
			request_id TEXT NOT NULL,
			turn_id TEXT NOT NULL,
			iteration_index INTEGER NOT NULL,
			input_json TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'succeeded',
			response_json TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (run_id, request_id, turn_id, iteration_index),
			CHECK (status IN ('intent', 'succeeded', 'failed'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_model_effects_turn
			ON v2_model_effects(run_id, request_id, turn_id)`,
		`CREATE TABLE IF NOT EXISTS v2_tool_effects (
			run_id TEXT NOT NULL,
			request_id TEXT NOT NULL,
			turn_id TEXT NOT NULL,
			iteration_index INTEGER NOT NULL,
			tool_call_id TEXT NOT NULL,
			tool_name TEXT NOT NULL,
			args_json TEXT NOT NULL DEFAULT '',
			replay_policy TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			result_json TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (run_id, request_id, turn_id, iteration_index, tool_call_id),
			CHECK (status IN ('intent', 'succeeded', 'failed'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_tool_effects_turn
			ON v2_tool_effects(run_id, request_id, turn_id)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_tool_effects_status
			ON v2_tool_effects(status)`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("v2 effects migrate: %w", err)
		}
	}
	for _, migration := range []struct {
		table      string
		column     string
		columnType string
		defaultVal string
	}{
		{"v2_model_effects", "status", "TEXT NOT NULL", "'succeeded'"},
		{"v2_model_effects", "error_message", "TEXT NOT NULL", "''"},
		{"v2_tool_effects", "replay_policy", "TEXT NOT NULL", "''"},
	} {
		if err := dbutil.AddColumnIfNotExists(ctx, db, migration.table, migration.column, migration.columnType, migration.defaultVal); err != nil {
			return fmt.Errorf("v2 effects migrate: %w", err)
		}
	}
	return nil
}
