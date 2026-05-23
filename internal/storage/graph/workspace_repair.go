package graph

import (
	"context"
	"os"
	"strings"

	"github.com/joshka0/foxctl/internal/platform/logging"
	"github.com/joshka0/foxctl/internal/storage/workspacerepair"
)

var workspaceRepairColumns = []workspacerepair.WorkspaceColumn{
	{Table: "graph_nodes", Column: "workspace"},
	{Table: "graph_edges", Column: "workspace"},
}

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

	if !workspacerepair.AnyPathWorkspace(ctx, s.db, workspaceRepairColumns...) {
		return
	}

	logger := logging.FromContext(ctx)
	userHome, _ := os.UserHomeDir()
	workspaces := workspacerepair.CollectPathWorkspaces(ctx, s.db, workspaceRepairColumns...)

	for raw := range workspaces {
		resolved, ok := workspacerepair.ResolvePathWorkspace(raw, userHome)
		if !ok {
			continue
		}

		if err := s.migrateWorkspace(ctx, resolved.RawPath, resolved.WorkspaceID); err != nil {
			logger.Warn().Err(err).Str("from", resolved.RawPath).Str("to", resolved.WorkspaceID).Msg("graph: workspace repair failed")
			continue
		}
		logger.Info().Str("from", resolved.RawPath).Str("to", resolved.WorkspaceID).Msg("graph: repaired workspace IDs")
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
