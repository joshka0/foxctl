package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/storage/dbutil"
	"github.com/jkatigb/agentctl/internal/storage/sqlutil"
)

// IndexerState represents per-workspace indexer metadata stored alongside named memory.
//
// This is intentionally small and does not attempt to represent full indexing history.
// It is used as a "high-water mark" to drive incremental indexing strategies such as
// indexing the git diff since the last successful run.
type IndexerState struct {
	WorkspaceID        string    `json:"workspace_id"`
	IndexerID          string    `json:"indexer_id"`
	LastIndexedHeadSHA string    `json:"last_indexed_head_sha"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// GetIndexerState returns the persisted per-workspace state for an indexer.
//
// Index:
// - Purpose: Provide a stable "last indexed" anchor for incremental indexing workflows (e.g., git-diff since last run)
// - Flow: canonicalize workspace -> query indexer_state -> parse timestamps -> return (state, ok)
// - SideEffects: reads sqlite rows
// - FailureModes: db errors, timestamp parse errors
// - Related: SetLastIndexedHeadSHA, (*Store).MigrateWorkspace, (*Store).repairWorkspaceIDs
// - Keywords: indexer_state, last_indexed_head_sha, workspace, incremental_index, git-diff
func (s *Store) GetIndexerState(ctx context.Context, workspaceID, indexerID string) (IndexerState, bool, error) {
	if s == nil || s.db == nil {
		return IndexerState{}, false, fmt.Errorf("memory: get indexer state: store not initialized")
	}

	workspaceID = ws.CanonicalID(workspaceID)
	indexerID = strings.TrimSpace(indexerID)
	if workspaceID == "" || indexerID == "" {
		return IndexerState{}, false, fmt.Errorf("memory: get indexer state: workspace and indexer_id are required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT workspace, indexer_id, last_indexed_head_sha, created_at, updated_at
		FROM indexer_state
		WHERE workspace = ? AND indexer_id = ?`, workspaceID, indexerID)

	var state IndexerState
	var createdAt, updatedAt string
	if err := row.Scan(&state.WorkspaceID, &state.IndexerID, &state.LastIndexedHeadSHA, &createdAt, &updatedAt); err != nil {
		if dbutil.IsNoRows(err) {
			return IndexerState{}, false, nil
		}
		return IndexerState{}, false, fmt.Errorf("memory: get indexer state: %w", err)
	}

	var err error
	state.CreatedAt, err = sqlutil.ScanTimestamp(createdAt)
	if err != nil {
		return IndexerState{}, false, fmt.Errorf("memory: parse indexer state created_at: %w", err)
	}
	state.UpdatedAt, err = sqlutil.ScanTimestamp(updatedAt)
	if err != nil {
		return IndexerState{}, false, fmt.Errorf("memory: parse indexer state updated_at: %w", err)
	}

	return state, true, nil
}

// SetLastIndexedHeadSHA upserts the last indexed HEAD commit SHA for a workspace+indexer.
//
// Index:
// - Purpose: Record an indexer's "high-water mark" so future runs can diff from it
// - Flow: canonicalize workspace -> upsert row -> fetch row -> return state
// - SideEffects: writes sqlite rows
// - FailureModes: db errors, timestamp parse errors
// - Related: GetIndexerState, cmd/index.runIndexGitDiff
// - Keywords: indexer_state, upsert, last_indexed_head_sha, workspace, head_sha
func (s *Store) SetLastIndexedHeadSHA(ctx context.Context, workspaceID, indexerID, headSHA string) (IndexerState, error) {
	if s == nil || s.db == nil {
		return IndexerState{}, fmt.Errorf("memory: set indexer state: store not initialized")
	}

	workspaceID = ws.CanonicalID(workspaceID)
	indexerID = strings.TrimSpace(indexerID)
	headSHA = strings.TrimSpace(headSHA)
	if workspaceID == "" || indexerID == "" || headSHA == "" {
		return IndexerState{}, fmt.Errorf("memory: set indexer state: workspace, indexer_id, and head_sha are required")
	}

	now := timeutil.NowUTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO indexer_state (workspace, indexer_id, last_indexed_head_sha, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(workspace, indexer_id) DO UPDATE SET
			last_indexed_head_sha = excluded.last_indexed_head_sha,
			updated_at = excluded.updated_at
	`, workspaceID, indexerID, headSHA, sqlutil.FormatTimestamp(now), sqlutil.FormatTimestamp(now))
	if err != nil {
		return IndexerState{}, fmt.Errorf("memory: set indexer state: %w", err)
	}

	state, ok, err := s.GetIndexerState(ctx, workspaceID, indexerID)
	if err != nil {
		return IndexerState{}, err
	}
	if !ok {
		return IndexerState{
			WorkspaceID:        workspaceID,
			IndexerID:          indexerID,
			LastIndexedHeadSHA: headSHA,
			CreatedAt:          now,
			UpdatedAt:          now,
		}, nil
	}
	return state, nil
}
