package trajectory

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
// This primarily addresses cases like username changes on macOS:
//
//	/Users/olduser/... -> /Users/newuser/...
//
// It is intentionally conservative:
// - Only considers workspace_id values that look like filesystem paths.
// - Only migrates when it can compute an ID from a path that exists on disk.
// - Never fails Open(); errors are logged and ignored.
//
// Index:
// - Purpose: Auto-repair legacy path-keyed trajectory rows so exports/queries work across machines/users
// - Flow: detect path-like workspace IDs -> compute stable target IDs -> copy/move tables in a FK-safe transaction -> log results
// - SideEffects: updates trajectory.db workspace_id values; may drop conflicting rows via INSERT OR IGNORE
// - FailureModes: query failures, tx failures, FK constraint errors (logged; does not fail Open)
// - Related: ws.CanonicalID, ws.ID, (*sqlStore).migrateWorkspace, trajectory.Open
// - Keywords: workspace_id, migration, repair, trajectories, trajectory_events, user_requests
func (s *sqlStore) repairWorkspaceIDs(ctx context.Context) {
	if s == nil || s.db == nil {
		return
	}

	// Fast-path: if nothing looks like a filesystem path, there is nothing to repair.
	if !tableHasPathWorkspace(ctx, s.db, "trajectories", "workspace_id") &&
		!tableHasPathWorkspace(ctx, s.db, "trajectory_events", "workspace_id") &&
		!tableHasPathWorkspace(ctx, s.db, "user_requests", "workspace_id") {
		return
	}

	logger := logging.FromContext(ctx)
	userHome, _ := os.UserHomeDir()

	workspaces := make(map[string]struct{})
	collectPathWorkspaces(ctx, s.db, "trajectories", "workspace_id", workspaces)
	collectPathWorkspaces(ctx, s.db, "trajectory_events", "workspace_id", workspaces)
	collectPathWorkspaces(ctx, s.db, "user_requests", "workspace_id", workspaces)

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
			logger.Warn().Err(err).Str("from", raw).Str("to", targetID).Msg("trajectory: workspace repair failed")
			continue
		}
		logger.Info().Str("from", raw).Str("to", targetID).Msg("trajectory: repaired workspace IDs")
	}
}

// migrateWorkspace rewrites trajectory tables from one workspace key to another.
//
// It copies parent tables (trajectories/user_requests) under the target workspace and then
// updates trajectory_events.workspace_id, keeping foreign key constraints satisfied.
//
// Index:
// - Purpose: Make legacy workspace IDs queryable under a stable workspace key
// - Flow: begin tx -> copy parent rows -> update events -> delete old parents -> commit
// - SideEffects: database writes; may ignore duplicates if target already has rows
// - FailureModes: tx errors, exec errors, FK errors
// - Related: (*sqlStore).repairWorkspaceIDs
// - Keywords: migrate, transaction, foreign_keys, workspace_id
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

	// Copy trajectories into the target workspace (preserves IDs, avoids FK juggling).
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO trajectories (
			id, workspace_id, root_request_id, task_ids_json, epic_id, agent_role, job_id, trace_id,
			status, summary, artifact_digest, outcome_json, created_at, updated_at, session_id
		)
		SELECT
			id, ?, root_request_id, task_ids_json, epic_id, agent_role, job_id, trace_id,
			status, summary, artifact_digest, outcome_json, created_at, updated_at, session_id
		FROM trajectories
		WHERE workspace_id = ?
	`, to, from); err != nil {
		return err
	}

	// Copy user requests into the target workspace.
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO user_requests (
			id, workspace_id, actor, source, ts, text, command_context_json, task_hints_json
		)
		SELECT
			id, ?, actor, source, ts, text, command_context_json, task_hints_json
		FROM user_requests
		WHERE workspace_id = ?
	`, to, from); err != nil {
		return err
	}

	// Move events after trajectories exist in the target workspace (FK safe).
	if _, err := tx.ExecContext(ctx, `UPDATE trajectory_events SET workspace_id = ? WHERE workspace_id = ?`, to, from); err != nil {
		return err
	}

	// Remove old rows.
	if _, err := tx.ExecContext(ctx, `DELETE FROM trajectories WHERE workspace_id = ?`, from); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_requests WHERE workspace_id = ?`, from); err != nil {
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
