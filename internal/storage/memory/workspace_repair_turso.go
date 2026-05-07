package memory

import (
	"context"
	"os"
	"strings"

	ws "github.com/joshka0/foxctl/internal/platform/workspace"
)

// repairWorkspaceIDs best-effort migrates legacy absolute-path workspace keys to stable IDs.
//
// This is primarily meant to keep historical Turso-backed memory reachable after
// username/home changes on macOS or after older clients stored raw paths.
// Errors are logged and ignored.
//
// Index:
//
//	Purpose: Keep historical Turso-backed named-memory entries reachable when workspace paths change across machines/users
//	Keywords: workspace, migration, repair, memory, turso
//	Related: ws.ID, ws.CanonicalID, (*TursoStore).migrateWorkspace
//	Flow: detect path-like workspace values -> compute stable target IDs -> migrate -> log when rows move
//	Resources: Turso DB, named_memory, embedding_metadata, indexer_state
//	Events: none
//	OutputFields: none
//
// [[invariant:only-migrate-when-target-path-exists]]
// [[domain:workspace-stable-identity]]
func (s *TursoStore) repairWorkspaceIDs(ctx context.Context) {
	if s == nil || s.db == nil {
		return
	}

	db, ok := s.db.GetUnderlyingDB()
	if !ok || db == nil {
		return
	}

	// Fast-path: if nothing looks like a filesystem path, there is nothing to repair.
	if !tableHasPathWorkspace(ctx, db, "named_memory") &&
		!tableHasPathWorkspace(ctx, db, "embedding_metadata") &&
		!tableHasPathWorkspace(ctx, db, "indexer_state") {
		return
	}

	userHome, _ := os.UserHomeDir()

	workspaces := make(map[string]struct{})
	collectPathWorkspaces(ctx, db, "named_memory", workspaces)
	collectPathWorkspaces(ctx, db, "embedding_metadata", workspaces)
	collectPathWorkspaces(ctx, db, "indexer_state", workspaces)

	for raw := range workspaces {
		raw = strings.TrimSpace(raw)
		if raw == "" || ws.LooksLikeID(raw) {
			continue
		}

		targetID := ""
		if pathExists(raw) {
			targetID = ws.ID(raw)
		}
		repaired := repairHomePath(raw, userHome)
		if repaired != raw && pathExists(repaired) {
			targetID = ws.ID(repaired)
		}
		if targetID == "" || targetID == raw {
			continue
		}

		moved, err := s.migrateWorkspace(ctx, raw, targetID)
		if err != nil {
			logger.Warn().Err(err).Str("from", raw).Str("to", targetID).Msg("memory(turso): workspace repair failed")
			continue
		}
		if moved {
			logger.Info().Str("from", raw).Str("to", targetID).Msg("memory(turso): repaired workspace IDs")
		}
	}
}

// migrateWorkspace rewrites named memory tables from one workspace key to another.
//
// Index:
//
//	Purpose: Make legacy workspace IDs queryable under a stable workspace key in Turso
//	Keywords: migrate, transaction, workspace, turso
//	Related: (*TursoStore).repairWorkspaceIDs
//	Flow: begin tx -> update named_memory (conflict-safe) -> update embedding_metadata (conflict-safe) -> commit
//	Resources: Turso DB
//	Events: none
//	OutputFields: bool (moved)
//
// [[invariant:skip-conflicting-rows-on-migration]]
// [[domain:workspace-stable-identity]]
func (s *TursoStore) migrateWorkspace(ctx context.Context, from, to string) (bool, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || to == "" || from == to {
		return false, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	moved := false

	// Avoid UNIQUE(name, workspace) conflicts by skipping rows that already exist in the target workspace.
	res, err := tx.ExecContext(ctx, `
		UPDATE named_memory AS src
		SET workspace = ?
		WHERE src.workspace = ?
		  AND NOT EXISTS (
			SELECT 1 FROM named_memory dst
			WHERE dst.workspace = ? AND dst.name = src.name
		  )`, to, from, to)
	if err != nil {
		return false, err
	}
	if rows, err := res.RowsAffected(); err == nil && rows > 0 {
		moved = true
	}

	// Move embedding metadata only when the target workspace doesn't already have metadata.
	res, err = tx.ExecContext(ctx, `
		UPDATE embedding_metadata
		SET workspace = ?
		WHERE workspace = ?
		  AND NOT EXISTS (SELECT 1 FROM embedding_metadata WHERE workspace = ?)
	`, to, from, to)
	if err != nil {
		return false, err
	}
	if rows, err := res.RowsAffected(); err == nil && rows > 0 {
		moved = true
	}

	res, err = tx.ExecContext(ctx, `
		UPDATE indexer_state AS src
		SET workspace = ?
		WHERE src.workspace = ?
		  AND NOT EXISTS (
			SELECT 1 FROM indexer_state dst
			WHERE dst.workspace = ? AND dst.indexer_id = src.indexer_id
		  )`, to, from, to)
	if err != nil {
		return false, err
	}
	if rows, err := res.RowsAffected(); err == nil && rows > 0 {
		moved = true
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return moved, nil
}
