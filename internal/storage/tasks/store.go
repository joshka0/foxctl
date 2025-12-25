// Package tasks implements SQLite-backed persistence for task management.
package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/storage/dbutil"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
	"github.com/oklog/ulid/v2"
)

// Store defines the persistence interface for tasks and active-task state.
type Store interface {
	Close() error

	// Add inserts a new task.
	Add(ctx context.Context, t Task) (Task, error)
	// Update replaces mutable fields of an existing task.
	Update(ctx context.Context, t Task) (Task, error)
	// Get returns a task by ID.
	Get(ctx context.Context, id string) (Task, error)
	// ListByWorkspace returns tasks scoped to a workspace.
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Task, error)
	// ListByPlanFile returns tasks linked to a specific plan file.
	ListByPlanFile(ctx context.Context, planFile string) ([]Task, error)

	// GetActive returns the active task for a workspace, if any.
	GetActive(ctx context.Context, workspaceID string) (Task, bool, error)
	// SetActive marks the given task as active for the workspace.
	SetActive(ctx context.Context, workspaceID, taskID string) (Task, error)
	// ClearActive removes the active task for a workspace.
	ClearActive(ctx context.Context, workspaceID string) error
	// EnsureActive returns the active task or creates one with the given defaults.
	EnsureActive(ctx context.Context, workspaceID, defaultTitle, scopePath string) (Task, bool, error)

	// DirtyIfReviewed checks if the task is in ready_for_review or completed status.
	// If so, it demotes the status to in_progress and marks the review as stale.
	// Returns (task, dirtied, error) where dirtied is true if the task was modified.
	DirtyIfReviewed(ctx context.Context, taskID string) (Task, bool, error)
}

// Task represents a persisted task record.
type Task struct {
	ID          string
	WorkspaceID string
	Title       string
	Description string
	ScopePath   string
	ParentID    string
	Children    []string
	DependsOn   []string
	Status      string
	CreatedAt   time.Time
	CompletedAt *time.Time
	Notes       string
	Gotchas     string

	// Review gate fields (review_gate.md)
	LastReviewStatus string     // "ok", "failed", "pending", or empty
	LastReviewAt     *time.Time // timestamp of last review
	LastReviewID     string     // ID of most recent review artifact

	// Plan integration fields (links task to ~/.claude/plans/)
	PlanFile    string // Path to the Claude Code plan file this task is linked to
	PlanSection string // Section path within the plan (e.g., "Phase 1 > Step 1.1")
}

// Task status constants per review_gate.md
const (
	// StatusPending is the default status for new tasks.
	StatusPending = "pending"
	// StatusInProgress indicates work is actively happening.
	StatusInProgress = "in_progress"
	// StatusReadyForReview indicates implementation is believed complete, awaiting review.
	StatusReadyForReview = "ready_for_review"
	// StatusCompleted marks a task as done (has passed review).
	StatusCompleted = "completed"
	// StatusBlocked indicates the task is blocked on dependencies or other issues.
	StatusBlocked = "blocked"
	// StatusCanceled indicates the task has been abandoned or superseded.
	StatusCanceled = "canceled"
)

// Review status constants
const (
	// ReviewStatusOK indicates the last review passed.
	ReviewStatusOK = "ok"
	// ReviewStatusFailed indicates the last review failed.
	ReviewStatusFailed = "failed"
	// ReviewStatusPending indicates a review is in progress.
	ReviewStatusPending = "pending"
	// ReviewStatusStale indicates a review was invalidated by new writes.
	ReviewStatusStale = "stale"
)

type sqlStore struct {
	db *sql.DB
}

// Open initializes the task store rooted at the provided path.
func Open(ctx context.Context, root string) (Store, error) {
	dbPath := filepath.Join(root, "tasks.db")
	db, err := sqliteutil.OpenDB(ctx, dbPath, migrate)
	if err != nil {
		return nil, fmt.Errorf("tasks: open db: %w", err)
	}
	return &sqlStore{db: db}, nil
}

// Close releases database resources.
func (s *sqlStore) Close() error {
	return s.db.Close()
}

func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS tasks (
	id TEXT PRIMARY KEY,
	workspace_id TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL,
	description TEXT,
	scope_path TEXT,
	parent_id TEXT,
	children TEXT NOT NULL DEFAULT '[]',
	depends_on TEXT NOT NULL DEFAULT '[]',
	status TEXT NOT NULL,
	created_at TEXT NOT NULL,
	completed_at TEXT,
	notes TEXT,
	gotchas TEXT,
	last_review_status TEXT,
	last_review_at TEXT,
	last_review_id TEXT,
	plan_file TEXT,
	plan_section TEXT
);
CREATE INDEX IF NOT EXISTS idx_tasks_workspace_created ON tasks(workspace_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_plan_file ON tasks(plan_file);

CREATE TABLE IF NOT EXISTS active_tasks (
	workspace_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("tasks: migrate: %w", err)
	}

	// Add columns to existing tables (idempotent migration)
	alterDDL := []string{
		`ALTER TABLE tasks ADD COLUMN last_review_status TEXT`,
		`ALTER TABLE tasks ADD COLUMN last_review_at TEXT`,
		`ALTER TABLE tasks ADD COLUMN last_review_id TEXT`,
		`ALTER TABLE tasks ADD COLUMN plan_file TEXT`,
		`ALTER TABLE tasks ADD COLUMN plan_section TEXT`,
	}
	for _, stmt := range alterDDL {
		// Ignore errors from "duplicate column" - columns may already exist.
		_, _ = db.ExecContext(ctx, stmt) //nolint:errcheck
	}

	// Create plan_file index if missing (idempotent)
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_tasks_plan_file ON tasks(plan_file)`) //nolint:errcheck

	return nil
}

// Add inserts a new task into the store.
func (s *sqlStore) Add(ctx context.Context, t Task) (Task, error) {
	if t.ID == "" {
		t.ID = ulid.Make().String()
	}
	if t.Status == "" {
		t.Status = StatusPending
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = timeutil.NowUTC()
	}
	if t.Children == nil {
		t.Children = []string{}
	}
	if t.DependsOn == nil {
		t.DependsOn = []string{}
	}

	childrenJSON, err := json.Marshal(t.Children)
	if err != nil {
		return Task{}, fmt.Errorf("tasks: marshal children: %w", err)
	}
	dependsOnJSON, err := json.Marshal(t.DependsOn)
	if err != nil {
		return Task{}, fmt.Errorf("tasks: marshal depends_on: %w", err)
	}

	var completedAt *string
	if t.CompletedAt != nil {
		s := timeutil.FormatRFC3339Nano(*t.CompletedAt)
		completedAt = &s
	}

	var lastReviewAt *string
	if t.LastReviewAt != nil {
		s := timeutil.FormatRFC3339Nano(*t.LastReviewAt)
		lastReviewAt = &s
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO tasks (id, workspace_id, title, description, scope_path, parent_id, children, depends_on, status, created_at, completed_at, notes, gotchas, last_review_status, last_review_at, last_review_id, plan_file, plan_section)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.WorkspaceID, t.Title, t.Description, t.ScopePath, t.ParentID,
		string(childrenJSON), string(dependsOnJSON), t.Status,
		timeutil.FormatRFC3339Nano(t.CreatedAt), completedAt, t.Notes, t.Gotchas,
		t.LastReviewStatus, lastReviewAt, t.LastReviewID, t.PlanFile, t.PlanSection)
	if err != nil {
		return Task{}, fmt.Errorf("tasks: insert: %w", err)
	}
	return t, nil
}

// Update replaces mutable fields of an existing task.
func (s *sqlStore) Update(ctx context.Context, t Task) (Task, error) {
	childrenJSON, err := json.Marshal(t.Children)
	if err != nil {
		return Task{}, fmt.Errorf("tasks: marshal children: %w", err)
	}
	dependsOnJSON, err := json.Marshal(t.DependsOn)
	if err != nil {
		return Task{}, fmt.Errorf("tasks: marshal depends_on: %w", err)
	}

	var completedAt *string
	if t.CompletedAt != nil {
		s := timeutil.FormatRFC3339Nano(*t.CompletedAt)
		completedAt = &s
	}

	var lastReviewAt *string
	if t.LastReviewAt != nil {
		s := timeutil.FormatRFC3339Nano(*t.LastReviewAt)
		lastReviewAt = &s
	}

	res, err := s.db.ExecContext(ctx, `
UPDATE tasks SET
	title = ?, description = ?, scope_path = ?, parent_id = ?,
	children = ?, depends_on = ?, status = ?, completed_at = ?,
	notes = ?, gotchas = ?, last_review_status = ?, last_review_at = ?, last_review_id = ?,
	plan_file = ?, plan_section = ?
WHERE id = ?`,
		t.Title, t.Description, t.ScopePath, t.ParentID,
		string(childrenJSON), string(dependsOnJSON), t.Status, completedAt,
		t.Notes, t.Gotchas, t.LastReviewStatus, lastReviewAt, t.LastReviewID,
		t.PlanFile, t.PlanSection, t.ID)
	if err != nil {
		return Task{}, fmt.Errorf("tasks: update: %w", err)
	}
	// RowsAffected error is nil for SQLite.
	n, _ := res.RowsAffected() //nolint:errcheck
	if n == 0 {
		return Task{}, fmt.Errorf("tasks: task %q not found", t.ID)
	}
	return s.Get(ctx, t.ID)
}

// Get returns a task by ID.
func (s *sqlStore) Get(ctx context.Context, id string) (Task, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, workspace_id, title, description, scope_path, parent_id, children, depends_on, status, created_at, completed_at, notes, gotchas, last_review_status, last_review_at, last_review_id, plan_file, plan_section
FROM tasks WHERE id = ?`, id)
	return scanTask(row)
}

// ListByWorkspace returns tasks scoped to a workspace.
func (s *sqlStore) ListByWorkspace(ctx context.Context, workspaceID string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, workspace_id, title, description, scope_path, parent_id, children, depends_on, status, created_at, completed_at, notes, gotchas, last_review_status, last_review_at, last_review_id, plan_file, plan_section
FROM tasks WHERE workspace_id = ? ORDER BY created_at DESC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("tasks: list: %w", err)
	}
	defer func() {
		// Rows cleanup in defer; error is not actionable after iteration.
		_ = rows.Close() //nolint:errcheck
	}()

	var tasks []Task
	for rows.Next() {
		t, err := scanTaskRow(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// ListByPlanFile returns tasks linked to a specific plan file.
func (s *sqlStore) ListByPlanFile(ctx context.Context, planFile string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, workspace_id, title, description, scope_path, parent_id, children, depends_on, status, created_at, completed_at, notes, gotchas, last_review_status, last_review_at, last_review_id, plan_file, plan_section
FROM tasks WHERE plan_file = ? ORDER BY created_at ASC`, planFile)
	if err != nil {
		return nil, fmt.Errorf("tasks: list by plan: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		t, err := scanTaskRow(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// GetActive returns the active task for a workspace, if any.
func (s *sqlStore) GetActive(ctx context.Context, workspaceID string) (Task, bool, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT t.id, t.workspace_id, t.title, t.description, t.scope_path, t.parent_id, t.children, t.depends_on, t.status, t.created_at, t.completed_at, t.notes, t.gotchas, t.last_review_status, t.last_review_at, t.last_review_id, t.plan_file, t.plan_section
FROM tasks t
JOIN active_tasks a ON t.id = a.task_id
WHERE a.workspace_id = ?`, workspaceID)

	t, err := scanTask(row)
	if dbutil.IsNoRows(err) {
		return Task{}, false, nil
	}
	if err != nil {
		return Task{}, false, err
	}
	return t, true, nil
}

// SetActive marks the given task as active for the workspace.
func (s *sqlStore) SetActive(ctx context.Context, workspaceID, taskID string) (Task, error) {
	// Verify task exists
	t, err := s.Get(ctx, taskID)
	if err != nil {
		return Task{}, err
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO active_tasks (workspace_id, task_id) VALUES (?, ?)
ON CONFLICT(workspace_id) DO UPDATE SET task_id = excluded.task_id`, workspaceID, taskID)
	if err != nil {
		return Task{}, fmt.Errorf("tasks: set active: %w", err)
	}
	return t, nil
}

// ClearActive removes the active task for a workspace.
func (s *sqlStore) ClearActive(ctx context.Context, workspaceID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM active_tasks WHERE workspace_id = ?`, workspaceID)
	if err != nil {
		return fmt.Errorf("tasks: clear active: %w", err)
	}
	return nil
}

// EnsureActive returns the active task or creates one with the given defaults.
// Returns (task, created, error) where created is true if a new task was created.
func (s *sqlStore) EnsureActive(ctx context.Context, workspaceID, defaultTitle, scopePath string) (Task, bool, error) {
	// Check for existing active task
	t, found, err := s.GetActive(ctx, workspaceID)
	if err != nil {
		return Task{}, false, err
	}
	if found {
		return t, false, nil
	}

	// Create new task and set as active
	newTask := Task{
		WorkspaceID: workspaceID,
		Title:       defaultTitle,
		ScopePath:   scopePath,
		Status:      StatusPending,
	}
	t, err = s.Add(ctx, newTask)
	if err != nil {
		return Task{}, false, err
	}

	_, err = s.SetActive(ctx, workspaceID, t.ID)
	if err != nil {
		return Task{}, false, err
	}
	return t, true, nil
}

// DirtyIfReviewed checks if the task is in ready_for_review or completed status.
// If so, it demotes the status to in_progress and marks the review as stale.
// Returns (task, dirtied, error) where dirtied is true if the task was modified.
func (s *sqlStore) DirtyIfReviewed(ctx context.Context, taskID string) (Task, bool, error) {
	t, err := s.Get(ctx, taskID)
	if err != nil {
		return Task{}, false, err
	}

	// Only dirty if task is in ready_for_review or completed status
	if t.Status != StatusReadyForReview && t.Status != StatusCompleted {
		return t, false, nil
	}

	// Demote status to in_progress
	t.Status = StatusInProgress

	// Mark any previous passing review as stale
	if t.LastReviewStatus == ReviewStatusOK {
		t.LastReviewStatus = ReviewStatusStale
	}

	updated, err := s.Update(ctx, t)
	if err != nil {
		return Task{}, false, fmt.Errorf("tasks: dirty: %w", err)
	}

	return updated, true, nil
}

// scanTask scans a single task from a row.
func scanTask(row *sql.Row) (Task, error) {
	var t Task
	var childrenJSON, dependsOnJSON string
	var createdAtStr string
	var completedAtStr sql.NullString
	var description, scopePath, parentID, notes, gotchas sql.NullString
	var lastReviewStatus, lastReviewAt, lastReviewID sql.NullString
	var planFile, planSection sql.NullString

	err := row.Scan(
		&t.ID, &t.WorkspaceID, &t.Title, &description, &scopePath, &parentID,
		&childrenJSON, &dependsOnJSON, &t.Status, &createdAtStr, &completedAtStr,
		&notes, &gotchas, &lastReviewStatus, &lastReviewAt, &lastReviewID,
		&planFile, &planSection)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return Task{}, err
		}
		return Task{}, fmt.Errorf("tasks: scan: %w", err)
	}

	t.Description = description.String
	t.ScopePath = scopePath.String
	t.ParentID = parentID.String
	t.Notes = notes.String
	t.Gotchas = gotchas.String
	t.LastReviewStatus = lastReviewStatus.String
	t.LastReviewID = lastReviewID.String
	t.PlanFile = planFile.String
	t.PlanSection = planSection.String

	t.CreatedAt = timeutil.MustParseRFC3339Nano(createdAtStr)
	if completedAtStr.Valid {
		ct := timeutil.MustParseRFC3339Nano(completedAtStr.String)
		t.CompletedAt = &ct
	}
	if lastReviewAt.Valid {
		rt := timeutil.MustParseRFC3339Nano(lastReviewAt.String)
		t.LastReviewAt = &rt
	}

	t.Children = dbutil.ScanJSONArrayMust(childrenJSON)
	t.DependsOn = dbutil.ScanJSONArrayMust(dependsOnJSON)

	return t, nil
}

// scanTaskRow scans a task from rows (for iteration).
func scanTaskRow(rows *sql.Rows) (Task, error) {
	var t Task
	var childrenJSON, dependsOnJSON string
	var createdAtStr string
	var completedAtStr sql.NullString
	var description, scopePath, parentID, notes, gotchas sql.NullString
	var lastReviewStatus, lastReviewAt, lastReviewID sql.NullString
	var planFile, planSection sql.NullString

	err := rows.Scan(
		&t.ID, &t.WorkspaceID, &t.Title, &description, &scopePath, &parentID,
		&childrenJSON, &dependsOnJSON, &t.Status, &createdAtStr, &completedAtStr,
		&notes, &gotchas, &lastReviewStatus, &lastReviewAt, &lastReviewID,
		&planFile, &planSection)
	if err != nil {
		return Task{}, fmt.Errorf("tasks: scan row: %w", err)
	}

	t.Description = description.String
	t.ScopePath = scopePath.String
	t.ParentID = parentID.String
	t.Notes = notes.String
	t.Gotchas = gotchas.String
	t.LastReviewStatus = lastReviewStatus.String
	t.LastReviewID = lastReviewID.String
	t.PlanFile = planFile.String
	t.PlanSection = planSection.String

	t.CreatedAt = timeutil.MustParseRFC3339Nano(createdAtStr)
	if completedAtStr.Valid {
		ct := timeutil.MustParseRFC3339Nano(completedAtStr.String)
		t.CompletedAt = &ct
	}
	if lastReviewAt.Valid {
		rt := timeutil.MustParseRFC3339Nano(lastReviewAt.String)
		t.LastReviewAt = &rt
	}

	t.Children = dbutil.ScanJSONArrayMust(childrenJSON)
	t.DependsOn = dbutil.ScanJSONArrayMust(dependsOnJSON)

	return t, nil
}
