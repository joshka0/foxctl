package turns

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
)

// MigrateSchema creates turn lineage and artifact tables.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("v2 turns migrate: nil db")
	}

	vectorDims := dbdriver.GetDefaultVectorDimensions()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS v2_turns (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL DEFAULT '',
			turn_index INTEGER NOT NULL DEFAULT 0,
			trace_id TEXT,
			root_span_id TEXT,
			correlation_id TEXT,
			causation_id TEXT,
			request_id TEXT,
			actor_id TEXT,
			command TEXT,
			prompt TEXT,
			final_output_id TEXT,
			final_output_role TEXT,
			final_output_text TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_turns_session_turn_index
			ON v2_turns(session_id, turn_index)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_turns_trace
			ON v2_turns(trace_id)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_turns_session_created_at
			ON v2_turns(session_id, created_at)`,
		`CREATE TABLE IF NOT EXISTS v2_turn_iterations (
			turn_id TEXT NOT NULL,
			iteration_index INTEGER NOT NULL,
			trace_id TEXT,
			span_id TEXT,
			parent_span_id TEXT,
			message_id TEXT,
			message_role TEXT,
			message_text TEXT,
			created_at TEXT NOT NULL,
			PRIMARY KEY(turn_id, iteration_index),
			FOREIGN KEY(turn_id) REFERENCES v2_turns(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_turn_iterations_turn_index
			ON v2_turn_iterations(turn_id, iteration_index)`,
		`CREATE TABLE IF NOT EXISTS v2_turn_tool_calls (
			turn_id TEXT NOT NULL,
			iteration_index INTEGER NOT NULL,
			call_id TEXT NOT NULL,
			trace_id TEXT,
			span_id TEXT,
			parent_span_id TEXT,
			name TEXT NOT NULL,
			args_json TEXT NOT NULL DEFAULT '{}',
			status TEXT,
			result_ref_id TEXT,
			result_ref_kind TEXT,
			result_ref_text TEXT,
			created_at TEXT NOT NULL,
			PRIMARY KEY(turn_id, iteration_index, call_id),
			FOREIGN KEY(turn_id, iteration_index) REFERENCES v2_turn_iterations(turn_id, iteration_index) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_turn_tool_calls_turn_iteration
			ON v2_turn_tool_calls(turn_id, iteration_index)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_turn_tool_calls_turn_call
			ON v2_turn_tool_calls(turn_id, call_id)`,
		fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS v2_turn_artifacts (
				turn_id TEXT NOT NULL,
				artifact_type TEXT NOT NULL,
				artifact_version TEXT NOT NULL,
				ref TEXT NOT NULL,
				summary TEXT,
				content_json TEXT NOT NULL DEFAULT '{}',
				metadata_json TEXT NOT NULL DEFAULT '{}',
				embedding F32_BLOB(%d),
				embedding_json TEXT NOT NULL DEFAULT '[]',
				embedding_model TEXT,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				PRIMARY KEY(turn_id, artifact_type, artifact_version),
				FOREIGN KEY(turn_id) REFERENCES v2_turns(id) ON DELETE CASCADE,
				CHECK (artifact_type IN ('embedding', 'annotation', 'classification', 'learning'))
			)
		`, vectorDims),
		`CREATE INDEX IF NOT EXISTS idx_v2_turn_artifacts_ref
			ON v2_turn_artifacts(ref)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_turn_artifacts_turn_type_time
			ON v2_turn_artifacts(turn_id, artifact_type, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_turn_artifacts_type_time
			ON v2_turn_artifacts(artifact_type, updated_at)`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("v2 turns migrate: %w", err)
		}
	}

	// Best-effort vector index for libsql deployments. SQLite fallback builds
	// do not have libsql_vector_idx and should continue without failing.
	_, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_v2_turn_artifacts_embedding_vec
		ON v2_turn_artifacts(libsql_vector_idx(embedding))
	`)
	if err != nil && !isVectorIndexUnsupported(err) {
		return fmt.Errorf("v2 turns migrate vector index: %w", err)
	}

	return nil
}

func isVectorIndexUnsupported(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such function") ||
		strings.Contains(msg, "unknown function") ||
		strings.Contains(msg, "syntax error")
}
