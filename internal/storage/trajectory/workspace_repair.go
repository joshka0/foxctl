package trajectory

import (
	"context"
	"os"
	"strings"

	"github.com/joshka0/foxctl/internal/platform/logging"
	"github.com/joshka0/foxctl/internal/storage/workspacerepair"
)

var workspaceRepairColumns = []workspacerepair.WorkspaceColumn{
	{Table: "trajectories", Column: "workspace_id"},
	{Table: "trajectory_events", Column: "workspace_id"},
	{Table: "user_requests", Column: "workspace_id"},
}

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
//
//	Purpose: Auto-repair legacy path-keyed trajectory rows so exports/queries work across machines/users
//	Keywords: workspace_id, migration, repair, trajectories, trajectory_events, user_requests
//	Related: ws.CanonicalID, ws.ID, (*sqlStore).migrateWorkspace, trajectory.Open
//	Flow: detect path-like workspace IDs -> compute stable target IDs -> copy/move tables in a FK-safe transaction -> log results
//	Resources: trajectory.db, trajectories, trajectory_events, user_requests
//	Events: none
//	OutputFields: none
//
// [[invariant:workspace-migration-atomic]]
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
			logger.Warn().Err(err).Str("from", resolved.RawPath).Str("to", resolved.WorkspaceID).Msg("trajectory: workspace repair failed")
			continue
		}
		logger.Info().Str("from", resolved.RawPath).Str("to", resolved.WorkspaceID).Msg("trajectory: repaired workspace IDs")
	}
}

// migrateWorkspace rewrites trajectory tables from one workspace key to another.
//
// It copies parent tables (trajectories/user_requests) under the target workspace and then
// updates trajectory_events.workspace_id, keeping foreign key constraints satisfied.
//
// Index:
//
//	Purpose: Make legacy workspace IDs queryable under a stable workspace key
//	Keywords: migrate, transaction, foreign_keys, workspace_id
//	Related: (*sqlStore).repairWorkspaceIDs
//	Flow: begin tx -> copy parent rows -> update events -> delete old parents -> commit
//	Resources: trajectory.db
//	Events: none
//	OutputFields: none
//
// [[invariant:workspace-migration-atomic]]
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
