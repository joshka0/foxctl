package graph

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/platform/logging"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
)

// repairWorkspaceIDs best-effort migrates legacy absolute-path workspace keys to stable IDs.
//
// This mirrors memory store behavior and primarily addresses cases like username changes on macOS:
//
//	/Users/olduser/... -> /Users/newuser/...
//
// Graph data is non-critical. Errors are logged and ignored.
//
// Index:
// - Purpose: Auto-repair legacy path-keyed graph rows so workspace-scoped graph queries remain stable across machines/users
// - Flow: detect path-like workspaces -> compute stable IDs -> migrate graph_nodes/graph_edges in a transaction -> log results
// - SideEffects: updates graph.db workspace values; may delete conflicting rows to preserve PK/UNIQUE constraints
// - FailureModes: query failures, tx failures, update conflicts (logged; does not fail Open)
// - Observability: logs warnings on failures; logs info when a migration succeeds
// - Related: ws.ID, ws.CanonicalID, (*SQLiteStore).migrateWorkspace
// - Keywords: workspace, migration, repair, graph, nodes, edges
func (s *SQLiteStore) repairWorkspaceIDs(ctx context.Context) {
	if s == nil || s.db == nil {
		return
	}

	// Fast-path: if nothing looks like a filesystem path, there is nothing to repair.
	if !tableHasPathWorkspace(ctx, s.db, "graph_nodes") && !tableHasPathWorkspace(ctx, s.db, "graph_edges") {
		return
	}

	logger := logging.FromContext(ctx)
	userHome, _ := os.UserHomeDir()

	workspaces := make(map[string]struct{})
	collectPathWorkspaces(ctx, s.db, "graph_nodes", workspaces)
	collectPathWorkspaces(ctx, s.db, "graph_edges", workspaces)

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
			logger.Warn().Err(err).Str("from", raw).Str("to", targetID).Msg("graph: workspace repair failed")
			continue
		}
		logger.Info().Str("from", raw).Str("to", targetID).Msg("graph: repaired workspace IDs")
	}
}

func (s *SQLiteStore) migrateWorkspace(ctx context.Context, from, to string) error {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || to == "" || from == to {
		return nil
	}

	// Index:
	// - Purpose: Rewrite graph workspace keys from a legacy path to a stable ID
	// - Flow: begin tx -> delete duplicates in target -> update nodes -> delete duplicate edges -> update edges -> commit
	// - SideEffects: database writes; may delete duplicate graph rows in the source workspace
	// - FailureModes: tx errors, exec errors
	// - Related: (*SQLiteStore).repairWorkspaceIDs
	// - Keywords: migrate, transaction, workspace
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Avoid PK conflicts on (workspace, node_id) by dropping duplicates in the source workspace.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM graph_nodes
		WHERE workspace = ?
		  AND node_id IN (SELECT node_id FROM graph_nodes WHERE workspace = ?)
	`, from, to); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE graph_nodes SET workspace = ? WHERE workspace = ?`, to, from); err != nil {
		return err
	}

	// Avoid UNIQUE conflicts on (workspace, from_id, to_id, edge_type) by dropping duplicates in the source workspace.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM graph_edges
		WHERE workspace = ?
		  AND (from_id, to_id, edge_type) IN (
			SELECT from_id, to_id, edge_type FROM graph_edges WHERE workspace = ?
		  )
	`, from, to); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE graph_edges SET workspace = ? WHERE workspace = ?`, to, from); err != nil {
		return err
	}

	return tx.Commit()
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
