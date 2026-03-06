package turns

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/dbutil"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

func TestMigrateSchema_UpgradesLegacyArtifactConstraintForNarrative(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "turns.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })

	// Pre-create the turns table + one row so copied legacy artifacts continue
	// to satisfy FK constraints during migration.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE v2_turns (
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
		)
	`); err != nil {
		t.Fatalf("create legacy v2_turns: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO v2_turns (id, session_id, turn_index, created_at, updated_at)
		VALUES ('legacy-turn-1', 'legacy-run-1', 1, '2026-02-27T11:00:00Z', '2026-02-27T11:00:00Z')
	`); err != nil {
		t.Fatalf("insert legacy turn row: %v", err)
	}

	// Legacy artifact schema from before narrative support.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE v2_turn_artifacts (
			turn_id TEXT NOT NULL,
			artifact_type TEXT NOT NULL,
			artifact_version TEXT NOT NULL,
			ref TEXT NOT NULL,
			summary TEXT,
			content_json TEXT NOT NULL DEFAULT '{}',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			embedding F32_BLOB(1024),
			embedding_json TEXT NOT NULL DEFAULT '[]',
			embedding_model TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(turn_id, artifact_type, artifact_version),
			FOREIGN KEY(turn_id) REFERENCES v2_turns(id) ON DELETE CASCADE,
			CHECK (artifact_type IN ('embedding', 'annotation', 'classification', 'learning'))
		)
	`); err != nil {
		t.Fatalf("create legacy v2_turn_artifacts: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO v2_turn_artifacts (
			turn_id, artifact_type, artifact_version, ref, summary,
			content_json, metadata_json, embedding_json, embedding_model, created_at, updated_at
		) VALUES (
			'legacy-turn-1', 'annotation', 'v1', 'turn/legacy-turn-1/artifact/annotation/v1', 'legacy summary',
			'{"text":"legacy annotation"}', '{"legacy":true}', '[]', 'legacy-model',
			'2026-02-27T11:00:00Z', '2026-02-27T11:00:00Z'
		)
	`); err != nil {
		t.Fatalf("insert legacy artifact row: %v", err)
	}

	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("MigrateSchema() error = %v", err)
	}

	var tableSQL string
	if err := db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='v2_turn_artifacts'`).Scan(&tableSQL); err != nil {
		t.Fatalf("query upgraded table sql: %v", err)
	}
	if !strings.Contains(strings.ToLower(tableSQL), "'narrative'") {
		t.Fatalf("upgraded artifact constraint missing narrative: %s", tableSQL)
	}
	var preserved int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM v2_turn_artifacts
		WHERE turn_id = 'legacy-turn-1'
		  AND artifact_type = 'annotation'
		  AND artifact_version = 'v1'
	`).Scan(&preserved); err != nil {
		t.Fatalf("count preserved legacy artifacts: %v", err)
	}
	if preserved != 1 {
		t.Fatalf("expected 1 preserved legacy artifact row, got %d", preserved)
	}

	store := NewStore(db, func() error { return nil })
	now := time.Date(2026, time.February, 27, 12, 0, 0, 0, time.UTC)
	store.SetNowForTest(func() time.Time { return now })

	if err := store.SaveTurn(ctx, run.TurnRecord{
		ID:        "turn-1",
		SessionID: "run-1",
		TurnIndex: 1,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SaveTurn() error = %v", err)
	}

	err = store.SaveNarrative(ctx, run.NarrativeRecord{
		SessionID:       "run-1",
		ArtifactVersion: "v1",
		Summary:         "narrative summary",
		Claims: []run.NarrativeClaim{
			{
				Text:       "turn one discussed migration safety",
				AnchorRefs: []string{"turn/turn-1"},
			},
		},
		AnchorRefs:      []string{"turn/turn-1"},
		SourceTurnID:    "turn-1",
		SourceTurnIndex: 1,
		SourceTurnCount: 1,
		UpdatedAt:       now,
	})
	if err != nil {
		t.Fatalf("SaveNarrative() error = %v", err)
	}
}
