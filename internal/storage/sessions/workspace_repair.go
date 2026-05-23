package sessions

import (
	"context"
	"os"

	"github.com/joshka0/foxctl/internal/storage/workspacerepair"
)

// repairWorkspaceIDs best-effort backfills stable workspace IDs for legacy rows.
//
// Older foxctl versions stored only an absolute workspace path. When that path changes
// (e.g., username change on macOS or cloning the repo elsewhere), workspace-scoped queries
// won't match historical sessions.
//
// This repair pass:
// - Finds sessions with empty workspace_id.
// - Computes a stable ID from workspace_path (preferring a repaired/expanded path that exists on disk).
// - Updates sessions.workspace_id (and sessions.workspace_path when a safe macOS home repair is applicable).
// - Updates session_edges.workspace when it matches the legacy workspace_path.
//
// Errors are logged and ignored.
//
// Index:
//
//	Purpose: Backfill stable workspace IDs for legacy sessions so workspace-scoped session search works across machines/users
//	Keywords: sessions, workspace_id, workspace_path, migration, repair
//	Related: ws.ID, ws.CanonicalID, sessionkit.WorkspaceOrDefault
//	Flow: detect legacy rows -> compute stable ID from workspace_path -> update sessions + session_edges -> log results
//	Resources: sessions.db, sessions table, session_edges table
//	Events: none
//	OutputFields: none
//
// [[invariant:only-backfill-when-path-exists-on-disk]]
// [[domain:workspace-stable-identity]]
func (s *Store) repairWorkspaceIDs(ctx context.Context) {
	if s == nil || s.db == nil {
		return
	}

	// Fast-path: only run when we have legacy rows.
	var one int
	if err := s.db.QueryRowContext(ctx, `
		SELECT 1
		FROM sessions
		WHERE workspace_id = ''
		  AND (workspace_path LIKE '%/%' OR workspace_path LIKE '~%')
		LIMIT 1`).Scan(&one); err != nil {
		return
	}

	userHome, _ := os.UserHomeDir()

	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT workspace_path
		FROM sessions
		WHERE workspace_id = ''
		  AND (workspace_path LIKE '%/%' OR workspace_path LIKE '~%')`)
	if err != nil {
		logger.Warn().Err(err).Msg("sessions: workspace repair query failed")
		return
	}
	defer rows.Close() //nolint:errcheck

	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		resolved, ok := workspacerepair.ResolvePathWorkspace(raw, userHome)
		if !ok {
			continue
		}

		// Best-effort updates: keep old paths intact unless we can safely repair them.
		if resolved.EffectivePath != resolved.RawPath {
			if _, err := s.db.ExecContext(ctx, `
				UPDATE sessions
				SET workspace_id = ?, workspace_path = ?
				WHERE workspace_id = '' AND workspace_path = ?`, resolved.WorkspaceID, resolved.EffectivePath, resolved.RawPath); err != nil {
				logger.Warn().Err(err).Str("from", resolved.RawPath).Str("to", resolved.WorkspaceID).Msg("sessions: workspace repair update failed")
				continue
			}
		} else {
			if _, err := s.db.ExecContext(ctx, `
				UPDATE sessions
				SET workspace_id = ?
				WHERE workspace_id = '' AND workspace_path = ?`, resolved.WorkspaceID, resolved.RawPath); err != nil {
				logger.Warn().Err(err).Str("from", resolved.RawPath).Str("to", resolved.WorkspaceID).Msg("sessions: workspace repair update failed")
				continue
			}
		}

		// Keep session_edges.workspace aligned when it stores a legacy workspace path.
		_, _ = s.db.ExecContext(ctx, `UPDATE session_edges SET workspace = ? WHERE workspace = ?`, resolved.WorkspaceID, resolved.RawPath)

		logger.Info().Str("from", resolved.RawPath).Str("to", resolved.WorkspaceID).Msg("sessions: repaired workspace IDs")
	}
}
