package turns

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var schemaVectorDimsPattern = regexp.MustCompile(`(?i)f32_blob\s*\(\s*(\d+)\s*\)`)

// MigrateSchema creates turn lineage and artifact tables.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("v2 turns migrate: nil db")
	}

	vectorDims := defaultV2TurnsVectorDimensions()
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
		`CREATE TABLE IF NOT EXISTS v2_episodes (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL DEFAULT '',
			episode_version TEXT NOT NULL DEFAULT 'v1',
			boundary_key TEXT NOT NULL DEFAULT '',
			start_turn_id TEXT NOT NULL,
			end_turn_id TEXT NOT NULL,
			start_turn_index INTEGER NOT NULL DEFAULT 0,
			end_turn_index INTEGER NOT NULL DEFAULT 0,
			topic TEXT,
			summary TEXT,
			salience_score REAL NOT NULL DEFAULT 0,
			is_landmark INTEGER NOT NULL DEFAULT 0,
			anchor_refs_json TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(session_id, episode_version, boundary_key),
			FOREIGN KEY(start_turn_id) REFERENCES v2_turns(id) ON DELETE CASCADE,
			FOREIGN KEY(end_turn_id) REFERENCES v2_turns(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_episodes_session_time
			ON v2_episodes(session_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_episodes_session_landmark_time
			ON v2_episodes(session_id, is_landmark, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_episodes_start_turn
			ON v2_episodes(start_turn_id)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_episodes_end_turn
			ON v2_episodes(end_turn_id)`,
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
				CHECK (artifact_type IN ('embedding', 'annotation', 'classification', 'learning', 'narrative'))
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

	if err := ensureNarrativeArtifactTypeSupport(ctx, db, vectorDims); err != nil {
		return fmt.Errorf("v2 turns migrate narrative support: %w", err)
	}

	// Best-effort vector index for libsql deployments. SQLite fallback builds
	// do not have libsql_vector_idx and should continue without failing.
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s
		ON v2_turn_artifacts(libsql_vector_idx(embedding))
	`, artifactVectorIndexName))
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

func ensureNarrativeArtifactTypeSupport(ctx context.Context, db *sql.DB, fallbackVectorDims int) error {
	var tableSQL string
	err := db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='v2_turn_artifacts'`).Scan(&tableSQL)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if strings.Contains(strings.ToLower(tableSQL), "'narrative'") {
		return nil
	}

	vectorDims := fallbackVectorDims
	if matches := schemaVectorDimsPattern.FindStringSubmatch(tableSQL); len(matches) == 2 {
		if parsed, convErr := strconv.Atoi(matches[1]); convErr == nil && parsed > 0 {
			vectorDims = parsed
		}
	}
	if vectorDims <= 0 {
		vectorDims = defaultV2TurnsVectorDimensions()
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	createNew := fmt.Sprintf(`
		CREATE TABLE v2_turn_artifacts_new (
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
			CHECK (artifact_type IN ('embedding', 'annotation', 'classification', 'learning', 'narrative'))
		)
	`, vectorDims)
	if _, err := tx.ExecContext(ctx, createNew); err != nil {
		return fmt.Errorf("create new artifacts table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO v2_turn_artifacts_new (
			turn_id, artifact_type, artifact_version, ref, summary,
			content_json, metadata_json, embedding, embedding_json, embedding_model, created_at, updated_at
		)
		SELECT
			turn_id, artifact_type, artifact_version, ref, summary,
			content_json, metadata_json, embedding, embedding_json, embedding_model, created_at, updated_at
		FROM v2_turn_artifacts
	`); err != nil {
		return fmt.Errorf("copy artifacts rows: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE v2_turn_artifacts`); err != nil {
		return fmt.Errorf("drop legacy artifacts table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE v2_turn_artifacts_new RENAME TO v2_turn_artifacts`); err != nil {
		return fmt.Errorf("rename artifacts table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_v2_turn_artifacts_ref ON v2_turn_artifacts(ref)`); err != nil {
		return fmt.Errorf("create idx_v2_turn_artifacts_ref: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_v2_turn_artifacts_turn_type_time ON v2_turn_artifacts(turn_id, artifact_type, updated_at)`); err != nil {
		return fmt.Errorf("create idx_v2_turn_artifacts_turn_type_time: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_v2_turn_artifacts_type_time ON v2_turn_artifacts(artifact_type, updated_at)`); err != nil {
		return fmt.Errorf("create idx_v2_turn_artifacts_type_time: %w", err)
	}
	_, vecErr := tx.ExecContext(ctx, fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s
		ON v2_turn_artifacts(libsql_vector_idx(embedding))
	`, artifactVectorIndexName))
	if vecErr != nil && !isVectorIndexUnsupported(vecErr) {
		return fmt.Errorf("create %s: %w", artifactVectorIndexName, vecErr)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit artifacts table migration: %w", err)
	}
	return nil
}
