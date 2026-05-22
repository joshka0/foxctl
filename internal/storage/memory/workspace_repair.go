package memory

import (
	"context"
	"os"

	"github.com/joshka0/foxctl/internal/storage/workspacerepair"
)

var workspaceRepairColumns = []workspacerepair.WorkspaceColumn{
	{Table: "named_memory", Column: "workspace"},
	{Table: "embedding_metadata", Column: "workspace"},
	{Table: "indexer_state", Column: "workspace"},
}

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
//
//	Purpose: Keep historical named-memory entries reachable when workspace paths change across users/machines
//	Keywords: workspace, migration, repair, memory, embeddings, symbols
//	Related: ws.ID, ws.CanonicalID, (*Store).MigrateWorkspace
//	Flow: detect path-like workspace values -> compute stable target IDs -> migrate via MigrateWorkspace -> log when rows move
//	Resources: memory.db, named_memory, embedding_metadata, indexer_state
//	Events: none
//	OutputFields: none
//
// [[invariant:only-migrate-when-target-path-exists]]
// [[domain:workspace-stable-identity]]
func (s *Store) repairWorkspaceIDs(ctx context.Context) {
	if s == nil || s.db == nil {
		return
	}

	if !workspacerepair.AnyPathWorkspace(ctx, s.db, workspaceRepairColumns...) {
		return
	}

	userHome, _ := os.UserHomeDir()
	workspaces := workspacerepair.CollectPathWorkspaces(ctx, s.db, workspaceRepairColumns...)

	for raw := range workspaces {
		resolved, ok := workspacerepair.ResolvePathWorkspace(raw, userHome)
		if !ok {
			continue
		}

		summary, err := s.MigrateWorkspace(ctx, resolved.RawPath, resolved.WorkspaceID, false)
		if err != nil {
			logger.Warn().Err(err).Str("from", resolved.RawPath).Str("to", resolved.WorkspaceID).Msg("memory: workspace repair failed")
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
