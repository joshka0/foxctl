package trajectory

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/storage/dbutil"
	"github.com/jkatigb/agentctl/internal/storage/sqlutil"
	"github.com/oklog/ulid/v2"
)

// Store defines the persistence interface for trajectory capture.
type Store interface {
	Close() error

	// Trajectory operations

	// InsertTrajectory creates a new trajectory record.
	InsertTrajectory(ctx context.Context, t Trajectory) (Trajectory, error)

	// GetTrajectory returns a trajectory by ID.
	GetTrajectory(ctx context.Context, workspaceID, id string) (Trajectory, error)

	// UpdateTrajectory updates an existing trajectory.
	UpdateTrajectory(ctx context.Context, t Trajectory) error

	// ListTrajectories returns trajectories matching the filter.
	ListTrajectories(ctx context.Context, filter ListFilter) ([]Trajectory, error)

	// DeleteTrajectory removes a trajectory and its events.
	DeleteTrajectory(ctx context.Context, workspaceID, id string) error

	// Outcome operations (for optimization)

	// SetOutcome records the outcome for a trajectory.
	SetOutcome(ctx context.Context, workspaceID, id string, outcome Outcome) error

	// ListByOutcome returns trajectories filtered by outcome criteria.
	ListByOutcome(ctx context.Context, filter OutcomeFilter) ([]Trajectory, error)

	// UserRequestCapture operations

	// InsertUserRequest creates a new user request capture.
	InsertUserRequest(ctx context.Context, ur UserRequestCapture) (UserRequestCapture, error)

	// GetUserRequest returns a user request by ID.
	GetUserRequest(ctx context.Context, workspaceID, id string) (UserRequestCapture, error)

	// ListUserRequests returns user requests for a workspace.
	ListUserRequests(ctx context.Context, workspaceID string, limit int) ([]UserRequestCapture, error)

	// Event operations

	// InsertEvent creates a new trajectory event.
	InsertEvent(ctx context.Context, e Event) (Event, error)

	// InsertEvents creates multiple trajectory events in a batch.
	InsertEvents(ctx context.Context, events []Event) error

	// ListEvents returns events for a trajectory.
	ListEvents(ctx context.Context, filter EventFilter) ([]Event, error)

	// GetEventsByTraceID returns events matching a trace ID across trajectories.
	GetEventsByTraceID(ctx context.Context, workspaceID, traceID string) ([]Event, error)
}

type sqlStore struct {
	db    *sql.DB
	close func() error
}

// Open initializes a trajectory Store backed by a database file at root/trajectory.db.
// The database driver is selected via the dbdriver env var conventions (e.g., AGENTCTL_TRAJECTORY_DB_DRIVER).
// It returns the Store or an error if the database cannot be opened or migrated.
func Open(ctx context.Context, root string) (Store, error) {
	dbPath := filepath.Join(root, "trajectory.db")
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "TRAJECTORY", filepath.Base(dbPath), migrate)
	if err != nil {
		return nil, fmt.Errorf("trajectory: open db: %w", err)
	}
	store := &sqlStore{db: db, close: closeFn}
	store.repairWorkspaceIDs(ctx)
	return store, nil
}

// Close releases database resources.
func (s *sqlStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// migrate creates the trajectories, user_requests, and trajectory_events schema, associated indexes,
// and normalizes existing empty JSON/text fields to NULL for the trajectory store.
//
// It applies initial DDL (tables and indexes) and performs lightweight schema upgrades for older
// databases by attempting to add missing columns (`outcome_json`, `session_id`) and a session index;
// errors from those ALTER/CREATE attempts are intentionally ignored. Returns an error if executing the
// primary DDL statement fails.
// MigrateSchema runs the trajectory store DDL migrations against the given database.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	return migrate(ctx, db)
}

func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
-- Trajectories table stores index records for coherent runs/episodes.
CREATE TABLE IF NOT EXISTS trajectories (
	id               TEXT NOT NULL,
	workspace_id     TEXT NOT NULL,
	root_request_id  TEXT,
	task_ids_json    TEXT,
	epic_id          TEXT,
	agent_role       TEXT,
	job_id           TEXT,
	trace_id         TEXT,
	status           TEXT NOT NULL,
	summary          TEXT,
	artifact_digest  TEXT,
	outcome_json     TEXT,
	created_at       TEXT NOT NULL,
	updated_at       TEXT NOT NULL,
	PRIMARY KEY (workspace_id, id)
);
CREATE INDEX IF NOT EXISTS idx_trajectories_workspace ON trajectories(workspace_id);
CREATE INDEX IF NOT EXISTS idx_trajectories_trace_id ON trajectories(workspace_id, trace_id);
CREATE INDEX IF NOT EXISTS idx_trajectories_job_id ON trajectories(workspace_id, job_id);
CREATE INDEX IF NOT EXISTS idx_trajectories_created ON trajectories(workspace_id, created_at DESC);

-- Index for outcome-based queries (optimization)
CREATE INDEX IF NOT EXISTS idx_trajectories_outcome_success ON trajectories(workspace_id, json_extract(outcome_json, '$.success'));
CREATE INDEX IF NOT EXISTS idx_trajectories_outcome_rating ON trajectories(workspace_id, json_extract(outcome_json, '$.human_rating'));

-- User request captures table stores normalized user intents.
CREATE TABLE IF NOT EXISTS user_requests (
	id                   TEXT NOT NULL,
	workspace_id         TEXT NOT NULL,
	actor                TEXT NOT NULL,
	source               TEXT NOT NULL,
	ts                   TEXT NOT NULL,
	text                 TEXT NOT NULL,
	command_context_json TEXT,
	task_hints_json      TEXT,
	PRIMARY KEY (workspace_id, id)
);
CREATE INDEX IF NOT EXISTS idx_user_requests_workspace ON user_requests(workspace_id);
CREATE INDEX IF NOT EXISTS idx_user_requests_ts ON user_requests(workspace_id, ts DESC);

-- Trajectory events table stores normalized views over envelopes/messages/artifacts.
CREATE TABLE IF NOT EXISTS trajectory_events (
	id               TEXT PRIMARY KEY,
	trajectory_id    TEXT NOT NULL,
	workspace_id     TEXT NOT NULL,
	ts               TEXT NOT NULL,
	kind             TEXT NOT NULL,
	actor            TEXT,
	command          TEXT,
	status           TEXT,
	data_inline_json TEXT,
	data_artifact    TEXT,
	meta_json        TEXT,
	FOREIGN KEY (workspace_id, trajectory_id) REFERENCES trajectories(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_events_trajectory ON trajectory_events(trajectory_id);
CREATE INDEX IF NOT EXISTS idx_events_workspace ON trajectory_events(workspace_id);
CREATE INDEX IF NOT EXISTS idx_events_ts ON trajectory_events(trajectory_id, ts);
CREATE INDEX IF NOT EXISTS idx_events_kind ON trajectory_events(trajectory_id, kind);

UPDATE trajectories SET task_ids_json = NULL WHERE task_ids_json = '';
UPDATE trajectories SET outcome_json = NULL WHERE outcome_json = '';
UPDATE user_requests SET command_context_json = NULL WHERE command_context_json = '';
UPDATE user_requests SET task_hints_json = NULL WHERE task_hints_json = '';
UPDATE trajectory_events SET data_inline_json = NULL WHERE data_inline_json = '';
UPDATE trajectory_events SET meta_json = NULL WHERE meta_json = '';

CREATE INDEX IF NOT EXISTS idx_events_trace_id ON trajectory_events(workspace_id, json_extract(meta_json, '$.trace_id'));
`
	_, err := db.ExecContext(ctx, ddl)
	if err != nil {
		return fmt.Errorf("trajectory: migrate: %w", err)
	}

	// Schema upgrade: add columns if missing (for existing databases)
	// SQLite doesn't support IF NOT EXISTS for ALTER TABLE, so we ignore the error.
	_, _ = db.ExecContext(ctx, `ALTER TABLE trajectories ADD COLUMN outcome_json TEXT`) //nolint:errcheck
	_, _ = db.ExecContext(ctx, `ALTER TABLE trajectories ADD COLUMN session_id TEXT`)   //nolint:errcheck
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_trajectories_session ON trajectories(workspace_id, session_id)`)
	return nil
}

// generateID creates a new ULID for records.
func generateID() string {
	return ulid.Make().String()
}

// InsertTrajectory creates a new trajectory record.
func (s *sqlStore) InsertTrajectory(ctx context.Context, t Trajectory) (Trajectory, error) {
	t.WorkspaceID = ws.CanonicalID(t.WorkspaceID)
	now := timeutil.NowUTC()
	if t.ID == "" {
		t.ID = generateID()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now

	taskIDsJSON, err := sqlutil.FormatJSON(t.TaskIDs)
	if err != nil {
		return Trajectory{}, fmt.Errorf("trajectory: format task_ids: %w", err)
	}

	taskIDsArg := any(taskIDsJSON)
	if taskIDsJSON == "" {
		taskIDsArg = nil
	}

	outcomeJSON, err := sqlutil.FormatJSON(t.Outcome)
	if err != nil {
		return Trajectory{}, fmt.Errorf("trajectory: format outcome: %w", err)
	}

	outcomeArg := any(outcomeJSON)
	if outcomeJSON == "" {
		outcomeArg = nil
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO trajectories (id, workspace_id, root_request_id, task_ids_json, epic_id, agent_role, job_id, trace_id, status, summary, artifact_digest, outcome_json, created_at, updated_at, session_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		t.ID, t.WorkspaceID, t.RootRequestID, taskIDsArg, t.EpicID, t.AgentRole,
		t.JobID, t.TraceID, string(t.Status), t.Summary, t.ArtifactDigest, outcomeArg,
		sqlutil.FormatTimestamp(t.CreatedAt), sqlutil.FormatTimestamp(t.UpdatedAt), t.SessionID,
	)
	if err != nil {
		return Trajectory{}, fmt.Errorf("trajectory: insert: %w", err)
	}
	return t, nil
}

// GetTrajectory returns a trajectory by ID.
func (s *sqlStore) GetTrajectory(ctx context.Context, workspaceID, id string) (Trajectory, error) {
	workspaceID = ws.CanonicalID(workspaceID)
	row := s.db.QueryRowContext(ctx, `
SELECT id, workspace_id, root_request_id, task_ids_json, epic_id, agent_role, job_id, trace_id, status, summary, artifact_digest, outcome_json, created_at, updated_at, session_id
FROM trajectories
WHERE workspace_id = ? AND id = ?
`, workspaceID, id)

	t, err := scanTrajectory(row)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return Trajectory{}, ErrNotFound
		}
		return Trajectory{}, fmt.Errorf("trajectory: get: %w", err)
	}
	return t, nil
}

// UpdateTrajectory updates an existing trajectory.
func (s *sqlStore) UpdateTrajectory(ctx context.Context, t Trajectory) error {
	t.WorkspaceID = ws.CanonicalID(t.WorkspaceID)
	t.UpdatedAt = timeutil.NowUTC()

	taskIDsJSON, err := sqlutil.FormatJSON(t.TaskIDs)
	if err != nil {
		return fmt.Errorf("trajectory: format task_ids: %w", err)
	}

	taskIDsArg := any(taskIDsJSON)
	if taskIDsJSON == "" {
		taskIDsArg = nil
	}

	outcomeJSON, err := sqlutil.FormatJSON(t.Outcome)
	if err != nil {
		return fmt.Errorf("trajectory: format outcome: %w", err)
	}

	outcomeArg := any(outcomeJSON)
	if outcomeJSON == "" {
		outcomeArg = nil
	}

	result, err := s.db.ExecContext(ctx, `
UPDATE trajectories SET
	root_request_id = ?, task_ids_json = ?, epic_id = ?, agent_role = ?, job_id = ?,
	trace_id = ?, status = ?, summary = ?, artifact_digest = ?, outcome_json = ?, updated_at = ?, session_id = ?
WHERE workspace_id = ? AND id = ?
`,
		t.RootRequestID, taskIDsArg, t.EpicID, t.AgentRole, t.JobID,
		t.TraceID, string(t.Status), t.Summary, t.ArtifactDigest, outcomeArg,
		sqlutil.FormatTimestamp(t.UpdatedAt), t.SessionID, t.WorkspaceID, t.ID,
	)
	if err != nil {
		return fmt.Errorf("trajectory: update: %w", err)
	}
	rowsAffected, raErr := result.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("trajectory: rows affected: %w", raErr)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ListTrajectories returns trajectories matching the filter.
func (s *sqlStore) ListTrajectories(ctx context.Context, filter ListFilter) ([]Trajectory, error) {
	filter.WorkspaceID = ws.CanonicalID(filter.WorkspaceID)
	if filter.WorkspaceID == "" {
		return nil, fmt.Errorf("trajectory: workspace_id required")
	}
	if filter.Limit <= 0 {
		filter.Limit = 100
	}

	query := `
SELECT id, workspace_id, root_request_id, task_ids_json, epic_id, agent_role, job_id, trace_id, status, summary, artifact_digest, outcome_json, created_at, updated_at, session_id
FROM trajectories
WHERE workspace_id = ?`
	args := []any{filter.WorkspaceID}

	if filter.TaskID != "" {
		query += ` AND EXISTS (SELECT 1 FROM json_each(task_ids_json) AS je WHERE je.value = ?)`
		args = append(args, filter.TaskID)
	}
	if filter.EpicID != "" {
		query += ` AND epic_id = ?`
		args = append(args, filter.EpicID)
	}
	if filter.AgentRole != "" {
		query += ` AND agent_role = ?`
		args = append(args, filter.AgentRole)
	}
	if filter.Status != "" {
		query += ` AND status = ?`
		args = append(args, string(filter.Status))
	}
	if filter.TraceID != "" {
		query += ` AND trace_id = ?`
		args = append(args, filter.TraceID)
	}
	if filter.SessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, filter.SessionID)
	}
	if !filter.Since.IsZero() {
		query += ` AND created_at >= ?`
		args = append(args, sqlutil.FormatTimestamp(filter.Since))
	}
	if !filter.Until.IsZero() {
		query += ` AND created_at <= ?`
		args = append(args, sqlutil.FormatTimestamp(filter.Until))
	}

	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, filter.Limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("trajectory: list: %w", err)
	}
	defer func() {
		// Rows cleanup in defer; error is not actionable after iteration.
		_ = rows.Close() //nolint:errcheck
	}()

	trajectories := make([]Trajectory, 0)
	for rows.Next() {
		t, err := scanTrajectoryRows(rows)
		if err != nil {
			return nil, fmt.Errorf("trajectory: scan: %w", err)
		}
		trajectories = append(trajectories, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("trajectory: rows: %w", err)
	}
	return trajectories, nil
}

// DeleteTrajectory removes a trajectory and its events (via CASCADE).
func (s *sqlStore) DeleteTrajectory(ctx context.Context, workspaceID, id string) error {
	workspaceID = ws.CanonicalID(workspaceID)
	result, err := s.db.ExecContext(ctx, `
DELETE FROM trajectories WHERE workspace_id = ? AND id = ?
`, workspaceID, id)
	if err != nil {
		return fmt.Errorf("trajectory: delete: %w", err)
	}
	rowsAffected, raErr := result.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("trajectory: rows affected: %w", raErr)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SetOutcome records the outcome for a trajectory.
func (s *sqlStore) SetOutcome(ctx context.Context, workspaceID, id string, outcome Outcome) error {
	workspaceID = ws.CanonicalID(workspaceID)
	// Set recorded time if not already set
	if outcome.RecordedAt.IsZero() {
		outcome.RecordedAt = timeutil.NowUTC()
	}

	outcomeJSON, err := sqlutil.FormatJSON(outcome)
	if err != nil {
		return fmt.Errorf("trajectory: format outcome: %w", err)
	}

	result, err := s.db.ExecContext(ctx, `
UPDATE trajectories SET outcome_json = ?, updated_at = ?
WHERE workspace_id = ? AND id = ?
`, outcomeJSON, sqlutil.FormatTimestamp(timeutil.NowUTC()), workspaceID, id)
	if err != nil {
		return fmt.Errorf("trajectory: set outcome: %w", err)
	}
	rowsAffected, raErr := result.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("trajectory: rows affected: %w", raErr)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ListByOutcome returns trajectories filtered by outcome criteria.
func (s *sqlStore) ListByOutcome(ctx context.Context, filter OutcomeFilter) ([]Trajectory, error) {
	filter.WorkspaceID = ws.CanonicalID(filter.WorkspaceID)
	if filter.WorkspaceID == "" {
		return nil, fmt.Errorf("trajectory: workspace_id required")
	}
	if filter.Limit <= 0 {
		filter.Limit = 100
	}

	query := `
SELECT id, workspace_id, root_request_id, task_ids_json, epic_id, agent_role, job_id, trace_id, status, summary, artifact_digest, outcome_json, created_at, updated_at, session_id
FROM trajectories
WHERE workspace_id = ? AND outcome_json IS NOT NULL`
	args := []any{filter.WorkspaceID}

	if filter.AgentRole != "" {
		query += ` AND agent_role = ?`
		args = append(args, filter.AgentRole)
	}
	if filter.Success != nil {
		query += ` AND json_extract(outcome_json, '$.success') = ?`
		args = append(args, *filter.Success)
	}
	if filter.MinRating != nil {
		query += ` AND json_extract(outcome_json, '$.human_rating') >= ?`
		args = append(args, *filter.MinRating)
	}
	if filter.MaxRating != nil {
		query += ` AND json_extract(outcome_json, '$.human_rating') <= ?`
		args = append(args, *filter.MaxRating)
	}
	if filter.HasFeedback != nil && *filter.HasFeedback {
		query += ` AND json_extract(outcome_json, '$.feedback') IS NOT NULL AND json_extract(outcome_json, '$.feedback') != ''`
	}
	if !filter.Since.IsZero() {
		query += ` AND json_extract(outcome_json, '$.recorded_at') >= ?`
		args = append(args, sqlutil.FormatTimestamp(filter.Since))
	}
	if !filter.Until.IsZero() {
		query += ` AND json_extract(outcome_json, '$.recorded_at') <= ?`
		args = append(args, sqlutil.FormatTimestamp(filter.Until))
	}

	query += ` ORDER BY json_extract(outcome_json, '$.recorded_at') DESC LIMIT ?`
	args = append(args, filter.Limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("trajectory: list by outcome: %w", err)
	}
	defer rows.Close()

	trajectories := make([]Trajectory, 0)
	for rows.Next() {
		t, err := scanTrajectoryRows(rows)
		if err != nil {
			return nil, fmt.Errorf("trajectory: scan: %w", err)
		}
		trajectories = append(trajectories, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("trajectory: rows: %w", err)
	}
	return trajectories, nil
}

// InsertUserRequest creates a new user request capture.
func (s *sqlStore) InsertUserRequest(ctx context.Context, ur UserRequestCapture) (UserRequestCapture, error) {
	ur.WorkspaceID = ws.CanonicalID(ur.WorkspaceID)
	if ur.ID == "" {
		ur.ID = generateID()
	}
	if ur.TS.IsZero() {
		ur.TS = timeutil.NowUTC()
	}

	cmdCtxJSON, err := sqlutil.FormatJSON(ur.CommandContext)
	if err != nil {
		return UserRequestCapture{}, fmt.Errorf("trajectory: format command_context: %w", err)
	}
	taskHintsJSON, err := sqlutil.FormatJSON(ur.TaskHints)
	if err != nil {
		return UserRequestCapture{}, fmt.Errorf("trajectory: format task_hints: %w", err)
	}

	cmdCtxArg := any(cmdCtxJSON)
	if cmdCtxJSON == "" {
		cmdCtxArg = nil
	}
	taskHintsArg := any(taskHintsJSON)
	if taskHintsJSON == "" {
		taskHintsArg = nil
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO user_requests (id, workspace_id, actor, source, ts, text, command_context_json, task_hints_json)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`,
		ur.ID, ur.WorkspaceID, ur.Actor, string(ur.Source), sqlutil.FormatTimestamp(ur.TS),
		ur.Text, cmdCtxArg, taskHintsArg,
	)
	if err != nil {
		return UserRequestCapture{}, fmt.Errorf("trajectory: insert user_request: %w", err)
	}
	return ur, nil
}

// GetUserRequest returns a user request by ID.
func (s *sqlStore) GetUserRequest(ctx context.Context, workspaceID, id string) (UserRequestCapture, error) {
	workspaceID = ws.CanonicalID(workspaceID)
	row := s.db.QueryRowContext(ctx, `
SELECT id, workspace_id, actor, source, ts, text, command_context_json, task_hints_json
FROM user_requests
WHERE workspace_id = ? AND id = ?
`, workspaceID, id)

	ur, err := scanUserRequest(row)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return UserRequestCapture{}, ErrNotFound
		}
		return UserRequestCapture{}, fmt.Errorf("trajectory: get user_request: %w", err)
	}
	return ur, nil
}

// ListUserRequests returns user requests for a workspace.
func (s *sqlStore) ListUserRequests(ctx context.Context, workspaceID string, limit int) ([]UserRequestCapture, error) {
	workspaceID = ws.CanonicalID(workspaceID)
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, workspace_id, actor, source, ts, text, command_context_json, task_hints_json
FROM user_requests
WHERE workspace_id = ?
ORDER BY ts DESC
LIMIT ?
`, workspaceID, limit)
	if err != nil {
		return nil, fmt.Errorf("trajectory: list user_requests: %w", err)
	}
	defer func() {
		// Rows cleanup in defer; error is not actionable after iteration.
		_ = rows.Close() //nolint:errcheck
	}()

	requests := make([]UserRequestCapture, 0)
	for rows.Next() {
		ur, err := scanUserRequestRows(rows)
		if err != nil {
			return nil, fmt.Errorf("trajectory: scan user_request: %w", err)
		}
		requests = append(requests, ur)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("trajectory: rows: %w", err)
	}
	return requests, nil
}

// InsertEvent creates a new trajectory event.
func (s *sqlStore) InsertEvent(ctx context.Context, e Event) (Event, error) {
	if e.ID == "" {
		e.ID = generateID()
	}
	if e.TS.IsZero() {
		e.TS = timeutil.NowUTC()
	}

	// Look up workspace_id from trajectory for the foreign key.
	var workspaceID string
	err := s.db.QueryRowContext(ctx, `SELECT workspace_id FROM trajectories WHERE id = ?`, e.TrajectoryID).Scan(&workspaceID)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return Event{}, fmt.Errorf("trajectory: trajectory not found: %s", e.TrajectoryID)
		}
		return Event{}, fmt.Errorf("trajectory: lookup workspace: %w", err)
	}

	dataInlineJSON, err := sqlutil.FormatJSON(e.DataInline)
	if err != nil {
		return Event{}, fmt.Errorf("trajectory: format data_inline: %w", err)
	}
	metaJSON, err := sqlutil.FormatJSON(e.Meta)
	if err != nil {
		return Event{}, fmt.Errorf("trajectory: format meta: %w", err)
	}

	dataInlineArg := any(dataInlineJSON)
	if dataInlineJSON == "" {
		dataInlineArg = nil
	}
	metaArg := any(metaJSON)
	if metaJSON == "" {
		metaArg = nil
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO trajectory_events (id, trajectory_id, workspace_id, ts, kind, actor, command, status, data_inline_json, data_artifact, meta_json)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		e.ID, e.TrajectoryID, workspaceID, sqlutil.FormatTimestamp(e.TS), string(e.Kind),
		e.Actor, e.Command, e.Status, dataInlineArg, e.DataArtifact, metaArg,
	)
	if err != nil {
		return Event{}, fmt.Errorf("trajectory: insert event: %w", err)
	}
	return e, nil
}

// InsertEvents creates multiple trajectory events in a batch.
func (s *sqlStore) InsertEvents(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("trajectory: begin tx: %w", err)
	}
	defer func() {
		// Rollback is a no-op after successful commit; error is intentionally ignored.
		_ = tx.Rollback() //nolint:errcheck
	}()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO trajectory_events (id, trajectory_id, workspace_id, ts, kind, actor, command, status, data_inline_json, data_artifact, meta_json)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`)
	if err != nil {
		return fmt.Errorf("trajectory: prepare: %w", err)
	}
	defer func() {
		// Statement cleanup in defer; error is not actionable.
		_ = stmt.Close() //nolint:errcheck
	}()

	now := timeutil.NowUTC()

	// Cache workspace lookups.
	workspaceCache := make(map[string]string)
	for i := range events {
		e := &events[i]
		if e.ID == "" {
			e.ID = generateID()
		}
		if e.TS.IsZero() {
			e.TS = now
		}

		workspaceID, ok := workspaceCache[e.TrajectoryID]
		if !ok {
			err := tx.QueryRowContext(ctx, `SELECT workspace_id FROM trajectories WHERE id = ?`, e.TrajectoryID).Scan(&workspaceID)
			if err != nil {
				return fmt.Errorf("trajectory: lookup workspace for trajectory %s: %w", e.TrajectoryID, err)
			}
			workspaceCache[e.TrajectoryID] = workspaceID
		}

		dataInlineJSON, err := sqlutil.FormatJSON(e.DataInline)
		if err != nil {
			return fmt.Errorf("trajectory: format data_inline: %w", err)
		}
		metaJSON, err := sqlutil.FormatJSON(e.Meta)
		if err != nil {
			return fmt.Errorf("trajectory: format meta: %w", err)
		}

		dataInlineArg := any(dataInlineJSON)
		if dataInlineJSON == "" {
			dataInlineArg = nil
		}
		metaArg := any(metaJSON)
		if metaJSON == "" {
			metaArg = nil
		}

		_, err = stmt.ExecContext(ctx,
			e.ID, e.TrajectoryID, workspaceID, sqlutil.FormatTimestamp(e.TS), string(e.Kind),
			e.Actor, e.Command, e.Status, dataInlineArg, e.DataArtifact, metaArg,
		)
		if err != nil {
			return fmt.Errorf("trajectory: insert event %s: %w", e.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("trajectory: commit: %w", err)
	}
	return nil
}

// ListEvents returns events for a trajectory.
func (s *sqlStore) ListEvents(ctx context.Context, filter EventFilter) ([]Event, error) {
	if filter.TrajectoryID == "" {
		return nil, fmt.Errorf("trajectory: trajectory_id required")
	}
	if filter.Limit <= 0 {
		filter.Limit = 1000
	}

	query := `
SELECT id, trajectory_id, ts, kind, actor, command, status, data_inline_json, data_artifact, meta_json
FROM trajectory_events
WHERE trajectory_id = ?`
	args := []any{filter.TrajectoryID}

	if filter.Kind != "" {
		query += ` AND kind = ?`
		args = append(args, string(filter.Kind))
	}
	if !filter.Since.IsZero() {
		query += ` AND ts >= ?`
		args = append(args, sqlutil.FormatTimestamp(filter.Since))
	}
	if !filter.Until.IsZero() {
		query += ` AND ts <= ?`
		args = append(args, sqlutil.FormatTimestamp(filter.Until))
	}

	query += ` ORDER BY ts LIMIT ?`
	args = append(args, filter.Limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("trajectory: list events: %w", err)
	}
	defer func() {
		// Rows cleanup in defer; error is not actionable after iteration.
		_ = rows.Close() //nolint:errcheck
	}()

	events := make([]Event, 0)
	for rows.Next() {
		e, err := scanEventRows(rows)
		if err != nil {
			return nil, fmt.Errorf("trajectory: scan event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("trajectory: rows: %w", err)
	}
	return events, nil
}

// GetEventsByTraceID returns events matching a trace ID across trajectories.
func (s *sqlStore) GetEventsByTraceID(ctx context.Context, workspaceID, traceID string) ([]Event, error) {
	workspaceID = ws.CanonicalID(workspaceID)
	rows, err := s.db.QueryContext(ctx, `
SELECT id, trajectory_id, ts, kind, actor, command, status, data_inline_json, data_artifact, meta_json
FROM trajectory_events
WHERE workspace_id = ? AND json_extract(meta_json, '$.trace_id') = ?
ORDER BY ts
LIMIT 1000
`, workspaceID, traceID)
	if err != nil {
		return nil, fmt.Errorf("trajectory: get events by trace_id: %w", err)
	}
	defer func() {
		// Rows cleanup in defer; error is not actionable after iteration.
		_ = rows.Close() //nolint:errcheck
	}()

	events := make([]Event, 0)
	for rows.Next() {
		e, err := scanEventRows(rows)
		if err != nil {
			return nil, fmt.Errorf("trajectory: scan event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("trajectory: rows: %w", err)
	}
	return events, nil
}

// ErrNotFound indicates a missing record.
var ErrNotFound = fmt.Errorf("trajectory: not found")

// scanTrajectory scans a single row into a Trajectory.
func scanTrajectory(row *sql.Row) (Trajectory, error) {
	var t Trajectory
	var taskIDsJSON, rootRequestID, epicID, agentRole, jobID, traceID, summary, artifactDigest, outcomeJSON, sessionID sql.NullString
	var createdAt, updatedAt string
	var status string

	err := row.Scan(
		&t.ID, &t.WorkspaceID, &rootRequestID, &taskIDsJSON, &epicID, &agentRole,
		&jobID, &traceID, &status, &summary, &artifactDigest, &outcomeJSON, &createdAt, &updatedAt, &sessionID,
	)
	if err != nil {
		return Trajectory{}, err
	}

	t.RootRequestID = rootRequestID.String
	t.EpicID = epicID.String
	t.AgentRole = agentRole.String
	t.JobID = jobID.String
	t.TraceID = traceID.String
	t.Status = Status(status)
	t.Summary = summary.String
	t.ArtifactDigest = artifactDigest.String
	t.SessionID = sessionID.String

	if taskIDsJSON.Valid && taskIDsJSON.String != "" {
		// Optional JSON field; parse errors leave default empty slice.
		_ = sqlutil.ScanJSON(taskIDsJSON.String, &t.TaskIDs) //nolint:errcheck
	}

	if outcomeJSON.Valid && outcomeJSON.String != "" {
		t.Outcome = &Outcome{}
		// Optional JSON field; parse errors leave default empty struct.
		_ = sqlutil.ScanJSON(outcomeJSON.String, t.Outcome) //nolint:errcheck
	}

	// Timestamp parsing for required fields; format is controlled by our writes.
	t.CreatedAt, _ = sqlutil.ScanTimestamp(createdAt) //nolint:errcheck
	t.UpdatedAt, _ = sqlutil.ScanTimestamp(updatedAt) //nolint:errcheck

	return t, nil
}

// scanTrajectoryRows scans rows into a Trajectory.
func scanTrajectoryRows(rows *sql.Rows) (Trajectory, error) {
	var t Trajectory
	var taskIDsJSON, rootRequestID, epicID, agentRole, jobID, traceID, summary, artifactDigest, outcomeJSON, sessionID sql.NullString
	var createdAt, updatedAt string
	var status string

	err := rows.Scan(
		&t.ID, &t.WorkspaceID, &rootRequestID, &taskIDsJSON, &epicID, &agentRole,
		&jobID, &traceID, &status, &summary, &artifactDigest, &outcomeJSON, &createdAt, &updatedAt, &sessionID,
	)
	if err != nil {
		return Trajectory{}, err
	}

	t.RootRequestID = rootRequestID.String
	t.EpicID = epicID.String
	t.AgentRole = agentRole.String
	t.JobID = jobID.String
	t.TraceID = traceID.String
	t.Status = Status(status)
	t.Summary = summary.String
	t.ArtifactDigest = artifactDigest.String
	t.SessionID = sessionID.String

	if taskIDsJSON.Valid && taskIDsJSON.String != "" {
		// Optional JSON field; parse errors leave default empty slice.
		_ = sqlutil.ScanJSON(taskIDsJSON.String, &t.TaskIDs) //nolint:errcheck
	}

	if outcomeJSON.Valid && outcomeJSON.String != "" {
		t.Outcome = &Outcome{}
		// Optional JSON field; parse errors leave default empty struct.
		_ = sqlutil.ScanJSON(outcomeJSON.String, t.Outcome) //nolint:errcheck
	}

	// Timestamp parsing for required fields; format is controlled by our writes.
	t.CreatedAt, _ = sqlutil.ScanTimestamp(createdAt) //nolint:errcheck
	t.UpdatedAt, _ = sqlutil.ScanTimestamp(updatedAt) //nolint:errcheck

	return t, nil
}

// scanUserRequest scans a single row into a UserRequestCapture.
func scanUserRequest(row *sql.Row) (UserRequestCapture, error) {
	var ur UserRequestCapture
	var cmdCtxJSON, taskHintsJSON sql.NullString
	var ts, source string

	err := row.Scan(
		&ur.ID, &ur.WorkspaceID, &ur.Actor, &source, &ts, &ur.Text,
		&cmdCtxJSON, &taskHintsJSON,
	)
	if err != nil {
		return UserRequestCapture{}, err
	}

	ur.Source = Source(source)
	// Timestamp parsing; format is controlled by our writes.
	ur.TS, _ = sqlutil.ScanTimestamp(ts) //nolint:errcheck

	if cmdCtxJSON.Valid && cmdCtxJSON.String != "" {
		ur.CommandContext = &CommandContext{}
		// Optional JSON field; parse errors leave default empty struct.
		_ = sqlutil.ScanJSON(cmdCtxJSON.String, ur.CommandContext) //nolint:errcheck
	}
	if taskHintsJSON.Valid && taskHintsJSON.String != "" {
		ur.TaskHints = &TaskHints{}
		// Optional JSON field; parse errors leave default empty struct.
		_ = sqlutil.ScanJSON(taskHintsJSON.String, ur.TaskHints) //nolint:errcheck
	}

	return ur, nil
}

// scanUserRequestRows scans rows into a UserRequestCapture.
func scanUserRequestRows(rows *sql.Rows) (UserRequestCapture, error) {
	var ur UserRequestCapture
	var cmdCtxJSON, taskHintsJSON sql.NullString
	var ts, source string

	err := rows.Scan(
		&ur.ID, &ur.WorkspaceID, &ur.Actor, &source, &ts, &ur.Text,
		&cmdCtxJSON, &taskHintsJSON,
	)
	if err != nil {
		return UserRequestCapture{}, err
	}

	ur.Source = Source(source)
	// Timestamp parsing; format is controlled by our writes.
	ur.TS, _ = sqlutil.ScanTimestamp(ts) //nolint:errcheck

	if cmdCtxJSON.Valid && cmdCtxJSON.String != "" {
		ur.CommandContext = &CommandContext{}
		// Optional JSON field; parse errors leave default empty struct.
		_ = sqlutil.ScanJSON(cmdCtxJSON.String, ur.CommandContext) //nolint:errcheck
	}
	if taskHintsJSON.Valid && taskHintsJSON.String != "" {
		ur.TaskHints = &TaskHints{}
		// Optional JSON field; parse errors leave default empty struct.
		_ = sqlutil.ScanJSON(taskHintsJSON.String, ur.TaskHints) //nolint:errcheck
	}

	return ur, nil
}

// scanEventRows scans rows into a Event.
func scanEventRows(rows *sql.Rows) (Event, error) {
	var e Event
	var dataInlineJSON, metaJSON, actor, command, status, dataArtifact sql.NullString
	var ts, kind string

	err := rows.Scan(
		&e.ID, &e.TrajectoryID, &ts, &kind, &actor, &command, &status,
		&dataInlineJSON, &dataArtifact, &metaJSON,
	)
	if err != nil {
		return Event{}, err
	}

	e.Kind = EventKind(kind)
	e.Actor = actor.String
	e.Command = command.String
	e.Status = status.String
	e.DataArtifact = dataArtifact.String
	// Timestamp parsing; format is controlled by our writes.
	e.TS, _ = sqlutil.ScanTimestamp(ts) //nolint:errcheck

	if dataInlineJSON.Valid && dataInlineJSON.String != "" {
		e.DataInline = make(map[string]any)
		// Optional JSON field; parse errors leave default empty map.
		_ = sqlutil.ScanJSON(dataInlineJSON.String, &e.DataInline) //nolint:errcheck
	}
	if metaJSON.Valid && metaJSON.String != "" {
		e.Meta = &EventMeta{}
		// Optional JSON field; parse errors leave default empty struct.
		_ = sqlutil.ScanJSON(metaJSON.String, e.Meta) //nolint:errcheck
	}

	return e, nil
}

// Helper to avoid unused import warning when timeutil is used only for NowUTC.
var _ = time.Now
