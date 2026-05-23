package tasks

import (
	"context"
	"os"
	"strings"

	"github.com/joshka0/foxctl/internal/platform/logging"
	"github.com/joshka0/foxctl/internal/storage/workspacerepair"
)

var workspaceRepairColumns = []workspacerepair.WorkspaceColumn{
	{Table: "tasks", Column: "workspace_id"},
	{Table: "active_tasks", Column: "workspace_id"},
	{Table: "epics", Column: "workspace_id"},
	{Table: "active_epics", Column: "workspace_id"},
}

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
//
//	Purpose: Auto-repair legacy path-keyed rows so workspace-scoped queries work across machines/users
//	Keywords: workspace_id, migration, repair, tasks, active_tasks, epics, active_epics
//	Related: ws.CanonicalID, ws.ID, (*sqlStore).migrateWorkspace, tasks.Open
//	Flow: detect path-like workspace IDs -> compute stable target IDs -> migrate tables in a transaction -> log results
//	Resources: tasks.db, tasks, active_tasks, epics, active_epics
//	Events: none
//	OutputFields: none
//
// [[invariant:only-migrate-when-target-path-exists]]
// [[domain:workspace-stable-identity]]
func (s *sqlStore) repairWorkspaceIDs(ctx context.Context) {
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
			logger.Warn().Err(err).Str("from", resolved.RawPath).Str("to", resolved.WorkspaceID).Msg("tasks: workspace repair failed")
			continue
		}
		logger.Info().Str("from", resolved.RawPath).Str("to", resolved.WorkspaceID).Msg("tasks: repaired workspace IDs")
	}
}

// migrateWorkspace rewrites all task-related tables from one workspace key to another.
//
// Index:
//
//	Purpose: Make legacy workspace IDs queryable under a stable workspace key
//	Keywords: migrate, transaction, workspace_id
//	Related: (*sqlStore).repairWorkspaceIDs
//	Flow: begin tx -> update tasks/epics -> update active_* with conflict-safe deletes -> commit
//	Resources: tasks.db
//	Events: none
//	OutputFields: none
//
// [[invariant:conflict-safe-delete-before-update-on-active-tables]]
// [[domain:workspace-stable-identity]]
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
