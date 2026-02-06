package memory

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"

	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
)

// repairWorkspaceIDs best-effort migrates legacy absolute-path workspace keys to stable repo-based IDs.
//
// This primarily addresses cases like username changes on macOS:
//
//	/Users/olduser/... -> /Users/newuser/...
//
// It is intentionally conservative:
// - Only considers workspace values that contain a path separator.
// - Only migrates when it can compute a repo identity (git remote URL).
// - Never fails Open(); errors are logged and ignored.
//
// Index:
// - Purpose: Keep historical named-memory entries reachable when workspace paths change across users/machines
// - Flow: detect path-like workspace values -> compute stable target IDs -> migrate via MigrateWorkspace -> log when rows move
// - SideEffects: updates memory.db workspace values; may move embedding metadata to the canonical workspace
// - FailureModes: SQLite errors (logged; does not fail Open)
// - Observability: logs warnings on repair failures; logs info when a migration moves rows
// - Related: ws.ID, ws.CanonicalID, (*Store).MigrateWorkspace
// - Keywords: workspace, migration, repair, memory, embeddings, symbols
func (s *Store) repairWorkspaceIDs(ctx context.Context) {
	if s == nil || s.db == nil {
		return
	}

	// Fast-path: if nothing looks like a filesystem path, there is nothing to repair.
	if !tableHasPathWorkspace(ctx, s.db, "named_memory") &&
		!tableHasPathWorkspace(ctx, s.db, "embedding_metadata") &&
		!tableHasPathWorkspace(ctx, s.db, "indexer_state") {
		return
	}

	userHome, _ := os.UserHomeDir()

	workspaces := make(map[string]struct{})
	collectPathWorkspaces(ctx, s.db, "named_memory", workspaces)
	collectPathWorkspaces(ctx, s.db, "embedding_metadata", workspaces)
	collectPathWorkspaces(ctx, s.db, "indexer_state", workspaces)

	for raw := range workspaces {
		raw = strings.TrimSpace(raw)
		if raw == "" || ws.LooksLikeID(raw) {
			continue
		}

		// Prefer a canonical ID derived from a path that exists on disk:
		// - raw when it exists
		// - repaired (username change / "~" expansion) when it exists
		//
		// This allows migrating legacy absolute paths even for repos without a git remote
		// (PathIdentity), while still preferring repo-derived IDs when remotes exist.
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

		summary, err := s.MigrateWorkspace(ctx, raw, targetID, false)
		if err != nil {
			logger.Warn().Err(err).Str("from", raw).Str("to", targetID).Msg("memory: workspace repair failed")
			continue
		}
		// Log only when we actually moved rows. Conflicts-only migrations are common
		// after re-indexing, and logging them on every open gets noisy.
		if summary.Migrated > 0 || summary.MetadataMoved {
			logger.Info().
				Str("from", summary.From).
				Str("to", summary.To).
				Int("total", summary.Total).
				Int("migrated", summary.Migrated).
				Int("conflicts", summary.Conflicts).
				Bool("metadata_moved", summary.MetadataMoved).
				Msg("memory: repaired workspace IDs")
		}
	}
}

func tableHasPathWorkspace(ctx context.Context, db *sql.DB, table string) bool {
	var one int
	err := db.QueryRowContext(ctx, "SELECT 1 FROM "+table+" WHERE workspace LIKE '%/%' OR workspace LIKE '~%' LIMIT 1").Scan(&one)
	return err == nil
}

func collectPathWorkspaces(ctx context.Context, db *sql.DB, table string, out map[string]struct{}) {
	rows, err := db.QueryContext(ctx, "SELECT DISTINCT workspace FROM "+table+" WHERE workspace LIKE '%/%' OR workspace LIKE '~%'")
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
