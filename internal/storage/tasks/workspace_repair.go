package tasks

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/platform/logging"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
)

// repairWorkspaceIDs best-effort migrates legacy absolute-path workspace keys to stable IDs.
//
// This addresses cases like username changes on macOS:
//
//	/Users/olduser/... -> /Users/newuser/...
//
// It is intentionally conservative:
// - Only considers workspace_id values that look like filesystem paths.
// - Only migrates when it can compute an ID from a path that exists on disk.
// - Never fails Open(); errors are logged and ignored.
//
// Index:
// - Purpose: Auto-repair legacy path-keyed rows so workspace-scoped queries work across machines/users
// - Flow: detect path-like workspace IDs -> compute stable target IDs -> migrate tables in a transaction -> log results
// - SideEffects: updates tasks.db workspace_id values; may delete conflicting active_* rows
// - FailureModes: query failures, tx failures, update conflicts (logged; does not fail Open)
// - Related: ws.CanonicalID, ws.ID, (*sqlStore).migrateWorkspace, tasks.Open
// - Keywords: workspace_id, migration, repair, tasks, active_tasks, epics, active_epics
func (s *sqlStore) repairWorkspaceIDs(ctx context.Context) {
	if s == nil || s.db == nil {
		return
	}

	// Fast-path: if nothing looks like a filesystem path, there is nothing to repair.
	if !tableHasPathWorkspace(ctx, s.db, "tasks", "workspace_id") &&
		!tableHasPathWorkspace(ctx, s.db, "active_tasks", "workspace_id") &&
		!tableHasPathWorkspace(ctx, s.db, "epics", "workspace_id") &&
		!tableHasPathWorkspace(ctx, s.db, "active_epics", "workspace_id") {
		return
	}

	logger := logging.FromContext(ctx)
	userHome, _ := os.UserHomeDir()

	workspaces := make(map[string]struct{})
	collectPathWorkspaces(ctx, s.db, "tasks", "workspace_id", workspaces)
	collectPathWorkspaces(ctx, s.db, "active_tasks", "workspace_id", workspaces)
	collectPathWorkspaces(ctx, s.db, "epics", "workspace_id", workspaces)
	collectPathWorkspaces(ctx, s.db, "active_epics", "workspace_id", workspaces)

	for raw := range workspaces {
		raw = strings.TrimSpace(raw)
		if raw == "" || ws.LooksLikeID(raw) {
			continue
		}

		// Prefer a canonical ID derived from a path that exists on disk:
		// - raw when it exists
		// - repaired (username change / "~" expansion) when it exists
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

		if err := s.migrateWorkspace(ctx, raw, targetID); err != nil {
			logger.Warn().Err(err).Str("from", raw).Str("to", targetID).Msg("tasks: workspace repair failed")
			continue
		}
		logger.Info().Str("from", raw).Str("to", targetID).Msg("tasks: repaired workspace IDs")
	}
}

// migrateWorkspace rewrites all task-related tables from one workspace key to another.
//
// Index:
// - Purpose: Make legacy workspace IDs queryable under a stable workspace key
// - Flow: begin tx -> update tasks/epics -> update active_* with conflict-safe deletes -> commit
// - SideEffects: database writes; may drop duplicate active rows
// - FailureModes: tx errors, exec errors
// - Related: (*sqlStore).repairWorkspaceIDs
// - Keywords: migrate, transaction, workspace_id
func (s *sqlStore) migrateWorkspace(ctx context.Context, from, to string) error {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || to == "" || from == to {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET workspace_id = ? WHERE workspace_id = ?`, to, from); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE epics SET workspace_id = ? WHERE workspace_id = ?`, to, from); err != nil {
		return err
	}

	// Avoid PK conflicts on active_tasks.workspace_id by dropping the source row when the target exists.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM active_tasks
		WHERE workspace_id = ?
		  AND EXISTS (SELECT 1 FROM active_tasks WHERE workspace_id = ?)
	`, from, to); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE active_tasks SET workspace_id = ? WHERE workspace_id = ?`, to, from); err != nil {
		return err
	}

	// Avoid PK conflicts on (workspace_id, session_id) by dropping duplicates in the source workspace.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM active_epics
		WHERE workspace_id = ?
		  AND session_id IN (SELECT session_id FROM active_epics WHERE workspace_id = ?)
	`, from, to); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE active_epics SET workspace_id = ? WHERE workspace_id = ?`, to, from); err != nil {
		return err
	}

	return tx.Commit()
}

func tableHasPathWorkspace(ctx context.Context, db *sql.DB, table, column string) bool {
	var one int
	err := db.QueryRowContext(ctx, "SELECT 1 FROM "+table+" WHERE "+column+" LIKE '%/%' OR "+column+" LIKE '~%' LIMIT 1").Scan(&one)
	return err == nil
}

func collectPathWorkspaces(ctx context.Context, db *sql.DB, table, column string, out map[string]struct{}) {
	rows, err := db.QueryContext(ctx, "SELECT DISTINCT "+column+" FROM "+table+" WHERE "+column+" LIKE '%/%' OR "+column+" LIKE '~%'")
	if err != nil {
		return
	}
	defer rows.Close() //nolint:errcheck

	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			continue
		}
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		out[w] = struct{}{}
	}
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func repairHomePath(raw, userHome string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if userHome == "" {
		return raw
	}

	// Expand "~" for legacy config values.
	if strings.HasPrefix(raw, "~") {
		trimmed := strings.TrimPrefix(raw, "~")
		trimmed = strings.TrimPrefix(trimmed, string(filepath.Separator))
		return filepath.Join(userHome, trimmed)
	}

	// Repair stale macOS home paths after a username change:
	//   raw:      /Users/olduser/...
	//   userHome: /Users/newuser
	if strings.HasPrefix(raw, "/Users/") && strings.HasPrefix(userHome, "/Users/") {
		rest := strings.TrimPrefix(raw, "/Users/")
		oldUser, remainder, _ := strings.Cut(rest, "/")
		if oldUser == "" {
			return raw
		}

		homeRest := strings.TrimPrefix(userHome, "/Users/")
		newUser, _, _ := strings.Cut(homeRest, "/")
		if newUser == "" || newUser == oldUser {
			return raw
		}

		// Only rewrite if the old user directory is gone.
		if _, err := os.Stat(filepath.Join("/Users", oldUser)); os.IsNotExist(err) {
			if remainder == "" {
				return filepath.Join("/Users", newUser)
			}
			return filepath.Join("/Users", newUser, remainder)
		}
	}

	return raw
}
