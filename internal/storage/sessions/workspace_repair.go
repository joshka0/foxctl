package sessions

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
)

// repairWorkspaceIDs best-effort backfills stable workspace IDs for legacy rows.
//
// Older agentctl versions stored only an absolute workspace path. When that path changes
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
// - Purpose: Backfill stable workspace IDs for legacy sessions so workspace-scoped session search works across machines/users
// - Flow: detect legacy rows -> compute stable ID from workspace_path -> update sessions + session_edges -> log results
// - SideEffects: updates sessions.db workspace_id/workspace_path; updates session_edges.workspace for matching legacy paths
// - FailureModes: query/update errors (logged; does not fail Open)
// - Observability: logs warnings on failures; logs info when a repair succeeds
// - Related: ws.ID, ws.CanonicalID, sessionkit.WorkspaceOrDefault
// - Keywords: sessions, workspace_id, workspace_path, migration, repair
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
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		// Only compute IDs from paths that exist locally; hashing stale absolute paths
		// would make future queries less likely to match.
		effective := raw
		repaired := repairHomePath(raw, userHome)
		if repaired != raw && pathExists(repaired) {
			effective = repaired
		}
		if !pathExists(effective) {
			continue
		}

		workspaceID := ws.ID(effective)
		if workspaceID == "" || workspaceID == raw {
			continue
		}

		// Best-effort updates: keep old paths intact unless we can safely repair them.
		if effective != raw {
			if _, err := s.db.ExecContext(ctx, `
				UPDATE sessions
				SET workspace_id = ?, workspace_path = ?
				WHERE workspace_id = '' AND workspace_path = ?`, workspaceID, effective, raw); err != nil {
				logger.Warn().Err(err).Str("from", raw).Str("to", workspaceID).Msg("sessions: workspace repair update failed")
				continue
			}
		} else {
			if _, err := s.db.ExecContext(ctx, `
				UPDATE sessions
				SET workspace_id = ?
				WHERE workspace_id = '' AND workspace_path = ?`, workspaceID, raw); err != nil {
				logger.Warn().Err(err).Str("from", raw).Str("to", workspaceID).Msg("sessions: workspace repair update failed")
				continue
			}
		}

		// Keep session_edges.workspace aligned when it stores a legacy workspace path.
		_, _ = s.db.ExecContext(ctx, `UPDATE session_edges SET workspace = ? WHERE workspace = ?`, workspaceID, raw)

		logger.Info().Str("from", raw).Str("to", workspaceID).Msg("sessions: repaired workspace IDs")
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
