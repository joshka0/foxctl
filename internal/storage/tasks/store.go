package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/storage/dbutil"
	"github.com/oklog/ulid/v2"
)

// DefaultListAllLimit is the maximum number of tasks returned by ListAll when no limit is specified.
const DefaultListAllLimit = 1000

// Connection pool settings for SQLite file-based storage.
// Tasks are write-heavy with typically single-process access patterns,
// so we use conservative settings to minimize lock contention.
const (
	defaultMaxOpenConns    = 1                // Single writer reduces lock weirdness for SQLite
	defaultMaxIdleConns    = 1                // Keep one connection ready
	defaultConnMaxLifetime = 10 * time.Minute // Connection recycling interval
	defaultConnMaxIdleTime = 5 * time.Minute  // Idle connection timeout
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
	// ListWithOptions returns tasks scoped to a workspace with filtering options.
	ListWithOptions(ctx context.Context, workspaceID string, opts ListOptions) ([]Task, error)
	// ListByPlanFile returns tasks linked to a specific plan file.
	ListByPlanFile(ctx context.Context, planFile string) ([]Task, error)
	// ListAll returns all tasks (for embedding operations).
	ListAll(ctx context.Context, limit int) ([]Task, error)

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

	// SetEmbedding stores an embedding vector for a task.
	SetEmbedding(ctx context.Context, id string, embedding []byte, model string) error

	// SetPageRanks bulk updates PageRank scores for multiple tasks.
	// The map key is task ID, value is the PageRank score.
	SetPageRanks(ctx context.Context, ranks map[string]float64) error

	// Epic management
	// AddEpic creates a new epic.
	AddEpic(ctx context.Context, e Epic) (Epic, error)
	// GetEpic returns an epic by ID.
	GetEpic(ctx context.Context, id string) (Epic, error)
	// UpdateEpic updates an existing epic.
	UpdateEpic(ctx context.Context, e Epic) (Epic, error)
	// ListEpics returns epics for a workspace.
	ListEpics(ctx context.Context, workspaceID string) ([]Epic, error)
	// GetActiveEpic returns the active epic for a workspace and session, if any.
	GetActiveEpic(ctx context.Context, workspaceID, sessionID string) (Epic, bool, error)
	// SetActiveEpic sets the active epic for a workspace and session.
	SetActiveEpic(ctx context.Context, workspaceID, sessionID, epicID string) error
	// ClearActiveEpic removes the active epic for a workspace and session.
	ClearActiveEpic(ctx context.Context, workspaceID, sessionID string) error
	// ListTasksByEpic returns tasks linked to a specific epic.
	ListTasksByEpic(ctx context.Context, epicID string) ([]Task, error)
	// LinkTaskToEpic associates a task with an epic.
	LinkTaskToEpic(ctx context.Context, taskID, epicID string) error

	// UpdateAtomic stores atomic processing results for a task.
	// atomicDescription is the self-contained rewrite, entities are extracted identifiers,
	// keywords are BM25-optimized search terms.
	UpdateAtomic(ctx context.Context, id, atomicDescription string, entities, keywords []string) error
}

// Task represents a persisted task record.
type Task struct {
	ID              string
	WorkspaceID     string
	Title           string
	Description     string
	ScopePath       string
	ParentID        string
	Children        []string
	DependsOn       []string
	Status          string
	CreatedAt       time.Time
	CompletedAt     *time.Time
	AssignedActorID string
	AssignedAt      *time.Time
	OwnerActorID    string
	ClaimedAt       *time.Time
	HeartbeatAt     *time.Time
	BlockedReason   string
	BlockedAt       *time.Time
	Notes           string
	Gotchas         string

	// Review gate fields (review_gate.md)
	LastReviewStatus string     // "ok", "failed", "pending", or empty
	LastReviewAt     *time.Time // timestamp of last review
	LastReviewID     string     // ID of most recent review artifact

	// Plan integration fields (links task to ~/.claude/plans/)
	PlanFile    string // Path to the Claude Code plan file this task is linked to
	PlanSection string // Section path within the plan (e.g., "Phase 1 > Step 1.1")

	// Session tracking - links task to AI coding tool session
	SessionID string // AI coding tool session ID (Claude Code, OpenCode, Cursor, etc.)

	// Graph metrics - computed by tasksgraph analyzer
	PageRank float64 // PageRank score for priority ordering (higher = more important)

	// Epic linkage - groups tasks under a higher-level goal
	EpicID string // ID of the epic this task belongs to (if any)

	// Atomic processing fields (SimpleMem-style semantic lossless compression)
	// See: https://github.com/aiming-lab/SimpleMem
	AtomicDescription string   // Self-contained, disambiguated rewrite of description
	Entities          []string // Extracted entities (files, functions, people, concepts)
	Keywords          []string // BM25-optimized search terms
}

// ListOptions configures task list queries.
type ListOptions struct {
	SessionID string   // Filter by session ID (empty = all sessions)
	Statuses  []string // Filter by status (empty = all statuses)
	Limit     int      // Max results (0 = no limit)
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

const (
	taskBusyRetryWindow = 2 * time.Second
	taskBusyRetryStep   = 50 * time.Millisecond
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

// Epic represents a higher-level goal that groups related tasks.
// Epics persist across sessions and provide continuity for multi-session work.
type Epic struct {
	ID          string
	WorkspaceID string
	Title       string
	Goal        string // Detailed description of what this epic aims to achieve
	Status      string // active, completed, archived
	CreatedAt   time.Time
	CompletedAt *time.Time
	SessionID   string // Session that created the epic
}

// Epic status constants
const (
	// EpicStatusActive indicates the epic is currently being worked on.
	EpicStatusActive = "active"
	// EpicStatusCompleted indicates all tasks in the epic are done.
	EpicStatusCompleted = "completed"
	// EpicStatusArchived indicates the epic is no longer relevant.
	EpicStatusArchived = "archived"
)

type sqlStore struct {
	db    *sql.DB
	close func() error
}

// Open initializes and returns a Store backed by a database at root/tasks.db.
// The database driver is selected via the dbdriver env var conventions (e.g., AGENTCTL_TASKS_DB_DRIVER).
// It runs the package migrations, configures the connection pool for single-writer semantics
// (primarily for SQLite), and returns a store whose Close will release the underlying resources.
func Open(ctx context.Context, root string) (Store, error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "TASKS", "tasks.db", migrate)
	if err != nil {
		return nil, fmt.Errorf("tasks: open db: %w", err)
	}

	// Configure connection pool for optimal SQLite performance.
	// Single writer pattern reduces lock contention for write-heavy task operations.
	db.SetMaxOpenConns(defaultMaxOpenConns)
	db.SetMaxIdleConns(defaultMaxIdleConns)
	db.SetConnMaxLifetime(defaultConnMaxLifetime)
	db.SetConnMaxIdleTime(defaultConnMaxIdleTime)

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

// migrate creates and migrates the tasks package database schema to the current version.
//
// It is safe to call multiple times: it creates missing tables and indexes, adds new
// columns if they do not exist, normalizes legacy JSON array columns (children, depends_on,
// entities, keywords) to the canonical "[]" representation, and ensures required indexes
// (including plan_file and session indexes) exist.
//
// As part of schema evolution, the function may drop and recreate the active_epics table
// to add session_id; this intentionally discards transient active-epic entries.
//
// The function returns an error if the primary schema creation step fails; individual
// idempotent alter/update statements and auxiliary cleanup steps intentionally ignore
// duplicate-column and similar minor errors.

// MigrateSchema runs the tasks store DDL migrations against the given database.
// This is exported so the CLI db migrate command can create PostgreSQL tables.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	return migrate(ctx, db)
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
	assigned_actor_id TEXT,
	assigned_at TEXT,
	owner_actor_id TEXT,
	claimed_at TEXT,
	heartbeat_at TEXT,
	blocked_reason TEXT,
	blocked_at TEXT,
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

CREATE TABLE IF NOT EXISTS epics (
	id TEXT PRIMARY KEY,
	workspace_id TEXT NOT NULL,
	title TEXT NOT NULL,
	goal TEXT,
	status TEXT NOT NULL DEFAULT 'active',
	created_at TEXT NOT NULL,
	completed_at TEXT,
	session_id TEXT
);
CREATE INDEX IF NOT EXISTS idx_epics_workspace ON epics(workspace_id);
CREATE INDEX IF NOT EXISTS idx_epics_status ON epics(workspace_id, status);

CREATE TABLE IF NOT EXISTS active_epics (
	workspace_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	epic_id TEXT NOT NULL,
	PRIMARY KEY (workspace_id, session_id),
	FOREIGN KEY(epic_id) REFERENCES epics(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_active_epics_session ON active_epics(session_id);
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
		`ALTER TABLE tasks ADD COLUMN session_id TEXT`,
		`ALTER TABLE tasks ADD COLUMN assigned_actor_id TEXT`,
		`ALTER TABLE tasks ADD COLUMN assigned_at TEXT`,
		`ALTER TABLE tasks ADD COLUMN owner_actor_id TEXT`,
		`ALTER TABLE tasks ADD COLUMN claimed_at TEXT`,
		`ALTER TABLE tasks ADD COLUMN heartbeat_at TEXT`,
		`ALTER TABLE tasks ADD COLUMN blocked_reason TEXT`,
		`ALTER TABLE tasks ADD COLUMN blocked_at TEXT`,
		`ALTER TABLE tasks ADD COLUMN embedding BLOB`,
		`ALTER TABLE tasks ADD COLUMN embedding_model TEXT`,
		`ALTER TABLE tasks ADD COLUMN pagerank REAL`,
		`ALTER TABLE tasks ADD COLUMN epic_id TEXT`,
		// Atomic processing columns for SimpleMem-style semantic lossless compression.
		// See: https://github.com/aiming-lab/SimpleMem
		`ALTER TABLE tasks ADD COLUMN atomic_description TEXT`, // Self-contained, disambiguated rewrite
		`ALTER TABLE tasks ADD COLUMN entities TEXT`,           // JSON array of extracted entities
		`ALTER TABLE tasks ADD COLUMN keywords TEXT`,           // JSON array of BM25 keywords
	}
	for _, stmt := range alterDDL {
		// Ignore errors from "duplicate column" - columns may already exist.
		_, _ = db.ExecContext(ctx, stmt) //nolint:errcheck
	}

	// Normalize JSON array columns to [] for legacy/partial rows.
	fixup := []string{
		"UPDATE tasks SET children = '[]' WHERE children IS NULL OR trim(children) = '' OR children = 'null'",
		"UPDATE tasks SET depends_on = '[]' WHERE depends_on IS NULL OR trim(depends_on) = '' OR depends_on = 'null'",
		"UPDATE tasks SET entities = '[]' WHERE entities IS NULL OR trim(entities) = '' OR entities = 'null'",
		"UPDATE tasks SET keywords = '[]' WHERE keywords IS NULL OR trim(keywords) = '' OR keywords = 'null'",
	}
	for _, stmt := range fixup {
		_, _ = db.ExecContext(ctx, stmt) //nolint:errcheck
	}

	// Create plan_file index if missing (idempotent)
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_tasks_plan_file ON tasks(plan_file)`) //nolint:errcheck

	// Create session index for cross-session queries (any AI coding tool)
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_tasks_session ON tasks(session_id)`) //nolint:errcheck

	// Migrate active_epics table to include session_id (if old schema exists)
	// Check if session_id column exists by querying table info
	var hasSessionID bool
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(active_epics)`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dfltValue any
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err == nil {
				if name == "session_id" {
					hasSessionID = true
					break
				}
			}
		}
	}
	if !hasSessionID {
		// Drop old table and let CREATE TABLE IF NOT EXISTS rebuild it.
		// NOTE: This migration intentionally drops existing active_epics data.
		// Active epics are transient session state that gets re-established via
		// /anchor commands, so data loss during schema evolution is acceptable.
		_, _ = db.ExecContext(ctx, `DROP TABLE IF EXISTS active_epics`) //nolint:errcheck
		_, _ = db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS active_epics (
	workspace_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	epic_id TEXT NOT NULL,
	PRIMARY KEY (workspace_id, session_id),
	FOREIGN KEY(epic_id) REFERENCES epics(id) ON DELETE CASCADE
)`) //nolint:errcheck
		_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_active_epics_session ON active_epics(session_id)`) //nolint:errcheck
	}

	return nil
}

// Add inserts a new task into the store.
func (s *sqlStore) Add(ctx context.Context, t Task) (Task, error) {
	t.WorkspaceID = ws.CanonicalID(t.WorkspaceID)
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
	var assignedAt *string
	if t.AssignedAt != nil {
		s := timeutil.FormatRFC3339Nano(*t.AssignedAt)
		assignedAt = &s
	}
	var claimedAt *string
	if t.ClaimedAt != nil {
		s := timeutil.FormatRFC3339Nano(*t.ClaimedAt)
		claimedAt = &s
	}
	var heartbeatAt *string
	if t.HeartbeatAt != nil {
		s := timeutil.FormatRFC3339Nano(*t.HeartbeatAt)
		heartbeatAt = &s
	}
	var blockedAt *string
	if t.BlockedAt != nil {
		s := timeutil.FormatRFC3339Nano(*t.BlockedAt)
		blockedAt = &s
	}

	var lastReviewAt *string
	if t.LastReviewAt != nil {
		s := timeutil.FormatRFC3339Nano(*t.LastReviewAt)
		lastReviewAt = &s
	}

	err = retryTaskBusy(ctx, func() error {
		_, execErr := s.db.ExecContext(ctx, `
INSERT INTO tasks (id, workspace_id, title, description, scope_path, parent_id, children, depends_on, status, created_at, completed_at, assigned_actor_id, assigned_at, owner_actor_id, claimed_at, heartbeat_at, blocked_reason, blocked_at, notes, gotchas, last_review_status, last_review_at, last_review_id, plan_file, plan_section, session_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26)`,
			t.ID, t.WorkspaceID, t.Title, t.Description, t.ScopePath, t.ParentID,
			string(childrenJSON), string(dependsOnJSON), t.Status,
			timeutil.FormatRFC3339Nano(t.CreatedAt), completedAt, t.AssignedActorID, assignedAt, t.OwnerActorID, claimedAt, heartbeatAt, t.BlockedReason, blockedAt, t.Notes, t.Gotchas,
			t.LastReviewStatus, lastReviewAt, t.LastReviewID, t.PlanFile, t.PlanSection, t.SessionID)
		return execErr
	})
	if err != nil {
		return Task{}, fmt.Errorf("tasks: insert: %w", err)
	}
	return t, nil
}

// Update replaces mutable fields of an existing task.
func (s *sqlStore) Update(ctx context.Context, t Task) (Task, error) {
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
	var assignedAt *string
	if t.AssignedAt != nil {
		s := timeutil.FormatRFC3339Nano(*t.AssignedAt)
		assignedAt = &s
	}
	var claimedAt *string
	if t.ClaimedAt != nil {
		s := timeutil.FormatRFC3339Nano(*t.ClaimedAt)
		claimedAt = &s
	}
	var heartbeatAt *string
	if t.HeartbeatAt != nil {
		s := timeutil.FormatRFC3339Nano(*t.HeartbeatAt)
		heartbeatAt = &s
	}
	var blockedAt *string
	if t.BlockedAt != nil {
		s := timeutil.FormatRFC3339Nano(*t.BlockedAt)
		blockedAt = &s
	}

	var lastReviewAt *string
	if t.LastReviewAt != nil {
		s := timeutil.FormatRFC3339Nano(*t.LastReviewAt)
		lastReviewAt = &s
	}

	var res sql.Result
	err = retryTaskBusy(ctx, func() error {
		var execErr error
		res, execErr = s.db.ExecContext(ctx, `
UPDATE tasks SET
	title = $1, description = $2, scope_path = $3, parent_id = $4,
	children = $5, depends_on = $6, status = $7, completed_at = $8,
	assigned_actor_id = $9, assigned_at = $10, owner_actor_id = $11, claimed_at = $12, heartbeat_at = $13, blocked_reason = $14, blocked_at = $15,
	notes = $16, gotchas = $17, last_review_status = $18, last_review_at = $19, last_review_id = $20,
	plan_file = $21, plan_section = $22, session_id = $23
WHERE id = $24`,
			t.Title, t.Description, t.ScopePath, t.ParentID,
			string(childrenJSON), string(dependsOnJSON), t.Status, completedAt,
			t.AssignedActorID, assignedAt, t.OwnerActorID, claimedAt, heartbeatAt, t.BlockedReason, blockedAt,
			t.Notes, t.Gotchas, t.LastReviewStatus, lastReviewAt, t.LastReviewID,
			t.PlanFile, t.PlanSection, t.SessionID, t.ID)
		return execErr
	})
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
SELECT id, workspace_id, title, description, scope_path, parent_id, children, depends_on, status, created_at, completed_at, assigned_actor_id, assigned_at, owner_actor_id, claimed_at, heartbeat_at, blocked_reason, blocked_at, notes, gotchas, last_review_status, last_review_at, last_review_id, plan_file, plan_section, session_id, pagerank, epic_id, atomic_description, entities, keywords
FROM tasks WHERE id = $1`, id)
	return scanTask(row)
}

// ListByWorkspace returns tasks scoped to a workspace.
func (s *sqlStore) ListByWorkspace(ctx context.Context, workspaceID string) ([]Task, error) {
	workspaceID = ws.CanonicalID(workspaceID)
	rows, err := s.db.QueryContext(ctx, `
SELECT id, workspace_id, title, description, scope_path, parent_id, children, depends_on, status, created_at, completed_at, assigned_actor_id, assigned_at, owner_actor_id, claimed_at, heartbeat_at, blocked_reason, blocked_at, notes, gotchas, last_review_status, last_review_at, last_review_id, plan_file, plan_section, session_id, pagerank, epic_id, atomic_description, entities, keywords
FROM tasks WHERE workspace_id = $1 ORDER BY created_at DESC`, workspaceID)
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

// ListWithOptions returns tasks scoped to a workspace with filtering options.
func (s *sqlStore) ListWithOptions(ctx context.Context, workspaceID string, opts ListOptions) ([]Task, error) {
	workspaceID = ws.CanonicalID(workspaceID)
	// Build query with optional filters
	argIdx := 2
	query := `SELECT id, workspace_id, title, description, scope_path, parent_id, children, depends_on, status, created_at, completed_at, assigned_actor_id, assigned_at, owner_actor_id, claimed_at, heartbeat_at, blocked_reason, blocked_at, notes, gotchas, last_review_status, last_review_at, last_review_id, plan_file, plan_section, session_id, pagerank, epic_id, atomic_description, entities, keywords FROM tasks WHERE workspace_id = $1`
	args := []any{workspaceID}

	if opts.SessionID != "" {
		query += fmt.Sprintf(` AND session_id = $%d`, argIdx)
		argIdx++
		args = append(args, opts.SessionID)
	}

	if len(opts.Statuses) > 0 {
		placeholders := ""
		for i, status := range opts.Statuses {
			if i > 0 {
				placeholders += ", "
			}
			placeholders += fmt.Sprintf("$%d", argIdx)
			argIdx++
			args = append(args, status)
		}
		query += ` AND status IN (` + placeholders + `)`
	}

	query += ` ORDER BY created_at DESC`

	if opts.Limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, opts.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("tasks: list with options: %w", err)
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

// ListByPlanFile returns tasks linked to a specific plan file.
func (s *sqlStore) ListByPlanFile(ctx context.Context, planFile string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, workspace_id, title, description, scope_path, parent_id, children, depends_on, status, created_at, completed_at, assigned_actor_id, assigned_at, owner_actor_id, claimed_at, heartbeat_at, blocked_reason, blocked_at, notes, gotchas, last_review_status, last_review_at, last_review_id, plan_file, plan_section, session_id, pagerank, epic_id, atomic_description, entities, keywords
FROM tasks WHERE plan_file = $1 ORDER BY created_at ASC`, planFile)
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
	workspaceID = ws.CanonicalID(workspaceID)
	row := s.db.QueryRowContext(ctx, `
SELECT t.id, t.workspace_id, t.title, t.description, t.scope_path, t.parent_id, t.children, t.depends_on, t.status, t.created_at, t.completed_at, t.assigned_actor_id, t.assigned_at, t.owner_actor_id, t.claimed_at, t.heartbeat_at, t.blocked_reason, t.blocked_at, t.notes, t.gotchas, t.last_review_status, t.last_review_at, t.last_review_id, t.plan_file, t.plan_section, t.session_id, t.pagerank, t.epic_id, t.atomic_description, t.entities, t.keywords
FROM tasks t
JOIN active_tasks a ON t.id = a.task_id
WHERE a.workspace_id = $1`, workspaceID)

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
	workspaceID = ws.CanonicalID(workspaceID)
	// Verify task exists
	t, err := s.Get(ctx, taskID)
	if err != nil {
		return Task{}, err
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO active_tasks (workspace_id, task_id) VALUES ($1, $2)
ON CONFLICT(workspace_id) DO UPDATE SET task_id = excluded.task_id`, workspaceID, taskID)
	if err != nil {
		return Task{}, fmt.Errorf("tasks: set active: %w", err)
	}
	return t, nil
}

// ClearActive removes the active task for a workspace.
func (s *sqlStore) ClearActive(ctx context.Context, workspaceID string) error {
	workspaceID = ws.CanonicalID(workspaceID)
	_, err := s.db.ExecContext(ctx, `DELETE FROM active_tasks WHERE workspace_id = $1`, workspaceID)
	if err != nil {
		return fmt.Errorf("tasks: clear active: %w", err)
	}
	return nil
}

// EnsureActive returns the active task or creates one with the given defaults.
// Returns (task, created, error) where created is true if a new task was created.
func (s *sqlStore) EnsureActive(ctx context.Context, workspaceID, defaultTitle, scopePath string) (Task, bool, error) {
	workspaceID = ws.CanonicalID(workspaceID)
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

// ListAll returns all tasks up to the specified limit.
func (s *sqlStore) ListAll(ctx context.Context, limit int) ([]Task, error) {
	if limit <= 0 {
		limit = DefaultListAllLimit
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, workspace_id, title, description, scope_path, parent_id, children, depends_on, status, created_at, completed_at, assigned_actor_id, assigned_at, owner_actor_id, claimed_at, heartbeat_at, blocked_reason, blocked_at, notes, gotchas, last_review_status, last_review_at, last_review_id, plan_file, plan_section, session_id, pagerank, epic_id, atomic_description, entities, keywords
FROM tasks ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("tasks: list all: %w", err)
	}
	defer func() {
		_ = rows.Close() //nolint:errcheck
	}()

	tasks := make([]Task, 0)
	for rows.Next() {
		t, err := scanTaskRow(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// SetEmbedding stores an embedding vector for a task.
func (s *sqlStore) SetEmbedding(ctx context.Context, id string, embedding []byte, model string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE tasks SET
			embedding = $1,
			embedding_model = $2
		WHERE id = $3`,
		embedding, model, id)
	if err != nil {
		return fmt.Errorf("tasks: set embedding: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("tasks: rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("tasks: task %q not found", id)
	}
	return nil
}

// SetPageRanks bulk updates PageRank scores for multiple tasks.
func (s *sqlStore) SetPageRanks(ctx context.Context, ranks map[string]float64) error {
	if len(ranks) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("tasks: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback() //nolint:errcheck
	}()

	stmt, err := tx.PrepareContext(ctx, `UPDATE tasks SET pagerank = $1 WHERE id = $2`)
	if err != nil {
		return fmt.Errorf("tasks: prepare pagerank update: %w", err)
	}
	defer stmt.Close()

	for id, rank := range ranks {
		if _, err := stmt.ExecContext(ctx, rank, id); err != nil {
			return fmt.Errorf("tasks: set pagerank for %s: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("tasks: commit pagerank updates: %w", err)
	}
	return nil
}

// scanTask scans a single task from a row.
func scanTask(row *sql.Row) (Task, error) {
	var t Task
	var childrenJSON, dependsOnJSON string
	var createdAtStr string
	var completedAtStr sql.NullString
	var assignedAtStr, claimedAtStr, heartbeatAtStr, blockedAtStr sql.NullString
	var description, scopePath, parentID, assignedActorID, ownerActorID, blockedReason, notes, gotchas sql.NullString
	var lastReviewStatus, lastReviewAt, lastReviewID sql.NullString
	var planFile, planSection sql.NullString
	var sessionID sql.NullString
	var pagerank sql.NullFloat64
	var epicID sql.NullString
	var atomicDescription, entitiesJSON, keywordsJSON sql.NullString

	err := row.Scan(
		&t.ID, &t.WorkspaceID, &t.Title, &description, &scopePath, &parentID,
		&childrenJSON, &dependsOnJSON, &t.Status, &createdAtStr, &completedAtStr, &assignedActorID, &assignedAtStr, &ownerActorID, &claimedAtStr, &heartbeatAtStr, &blockedReason, &blockedAtStr,
		&notes, &gotchas, &lastReviewStatus, &lastReviewAt, &lastReviewID,
		&planFile, &planSection, &sessionID, &pagerank, &epicID,
		&atomicDescription, &entitiesJSON, &keywordsJSON)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return Task{}, err
		}
		return Task{}, fmt.Errorf("tasks: scan: %w", err)
	}

	t.Description = description.String
	t.ScopePath = scopePath.String
	t.ParentID = parentID.String
	t.AssignedActorID = assignedActorID.String
	t.OwnerActorID = ownerActorID.String
	t.BlockedReason = blockedReason.String
	t.Notes = notes.String
	t.Gotchas = gotchas.String
	t.LastReviewStatus = lastReviewStatus.String
	t.LastReviewID = lastReviewID.String
	t.PlanFile = planFile.String
	t.PlanSection = planSection.String
	t.SessionID = sessionID.String
	t.PageRank = pagerank.Float64
	t.EpicID = epicID.String
	t.AtomicDescription = atomicDescription.String
	t.Entities = dbutil.ScanJSONArrayMust(entitiesJSON.String)
	t.Keywords = dbutil.ScanJSONArrayMust(keywordsJSON.String)

	t.CreatedAt = timeutil.MustParseRFC3339Nano(createdAtStr)
	if completedAtStr.Valid {
		ct := timeutil.MustParseRFC3339Nano(completedAtStr.String)
		t.CompletedAt = &ct
	}
	if assignedAtStr.Valid {
		at := timeutil.MustParseRFC3339Nano(assignedAtStr.String)
		t.AssignedAt = &at
	}
	if claimedAtStr.Valid {
		ct := timeutil.MustParseRFC3339Nano(claimedAtStr.String)
		t.ClaimedAt = &ct
	}
	if heartbeatAtStr.Valid {
		ht := timeutil.MustParseRFC3339Nano(heartbeatAtStr.String)
		t.HeartbeatAt = &ht
	}
	if blockedAtStr.Valid {
		bt := timeutil.MustParseRFC3339Nano(blockedAtStr.String)
		t.BlockedAt = &bt
	}
	if lastReviewAt.Valid {
		rt := timeutil.MustParseRFC3339Nano(lastReviewAt.String)
		t.LastReviewAt = &rt
	}

	t.Children = dbutil.ScanJSONArrayMust(childrenJSON)
	t.DependsOn = dbutil.ScanJSONArrayMust(dependsOnJSON)

	return t, nil
}

// Epic management methods

// AddEpic creates a new epic.
func (s *sqlStore) AddEpic(ctx context.Context, e Epic) (Epic, error) {
	e.WorkspaceID = ws.CanonicalID(e.WorkspaceID)
	if e.ID == "" {
		e.ID = ulid.Make().String()
	}
	if e.Status == "" {
		e.Status = EpicStatusActive
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = timeutil.NowUTC()
	}

	var completedAt *string
	if e.CompletedAt != nil {
		s := timeutil.FormatRFC3339Nano(*e.CompletedAt)
		completedAt = &s
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO epics (id, workspace_id, title, goal, status, created_at, completed_at, session_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.ID, e.WorkspaceID, e.Title, e.Goal, e.Status,
		timeutil.FormatRFC3339Nano(e.CreatedAt), completedAt, e.SessionID)
	if err != nil {
		return Epic{}, fmt.Errorf("tasks: insert epic: %w", err)
	}
	return e, nil
}

// GetEpic returns an epic by ID.
func (s *sqlStore) GetEpic(ctx context.Context, id string) (Epic, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, workspace_id, title, goal, status, created_at, completed_at, session_id
FROM epics WHERE id = $1`, id)
	return scanEpic(row)
}

// UpdateEpic updates an existing epic.
func (s *sqlStore) UpdateEpic(ctx context.Context, e Epic) (Epic, error) {
	var completedAt *string
	if e.CompletedAt != nil {
		s := timeutil.FormatRFC3339Nano(*e.CompletedAt)
		completedAt = &s
	}

	_, err := s.db.ExecContext(ctx, `
UPDATE epics SET title = $1, goal = $2, status = $3, completed_at = $4, session_id = $5
WHERE id = $6`,
		e.Title, e.Goal, e.Status, completedAt, e.SessionID, e.ID)
	if err != nil {
		return Epic{}, fmt.Errorf("tasks: update epic: %w", err)
	}
	return e, nil
}

// ListEpics returns epics for a workspace.
func (s *sqlStore) ListEpics(ctx context.Context, workspaceID string) ([]Epic, error) {
	workspaceID = ws.CanonicalID(workspaceID)
	rows, err := s.db.QueryContext(ctx, `
SELECT id, workspace_id, title, goal, status, created_at, completed_at, session_id
FROM epics WHERE workspace_id = $1 ORDER BY created_at DESC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("tasks: list epics: %w", err)
	}
	defer rows.Close()

	var epics []Epic
	for rows.Next() {
		e, err := scanEpicRow(rows)
		if err != nil {
			return nil, err
		}
		epics = append(epics, e)
	}
	return epics, rows.Err()
}

// GetActiveEpic returns the active epic for a workspace, if any.
func (s *sqlStore) GetActiveEpic(ctx context.Context, workspaceID, sessionID string) (Epic, bool, error) {
	workspaceID = ws.CanonicalID(workspaceID)
	row := s.db.QueryRowContext(ctx, `
SELECT e.id, e.workspace_id, e.title, e.goal, e.status, e.created_at, e.completed_at, e.session_id
FROM epics e
JOIN active_epics a ON e.id = a.epic_id
WHERE a.workspace_id = $1 AND a.session_id = $2`, workspaceID, sessionID)

	e, err := scanEpic(row)
	if dbutil.IsNoRows(err) {
		return Epic{}, false, nil
	}
	if err != nil {
		return Epic{}, false, err
	}
	return e, true, nil
}

// SetActiveEpic sets the active epic for a workspace.
func (s *sqlStore) SetActiveEpic(ctx context.Context, workspaceID, sessionID, epicID string) error {
	workspaceID = ws.CanonicalID(workspaceID)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO active_epics (workspace_id, session_id, epic_id) VALUES ($1, $2, $3)
ON CONFLICT(workspace_id, session_id) DO UPDATE SET epic_id = excluded.epic_id`, workspaceID, sessionID, epicID)
	if err != nil {
		return fmt.Errorf("tasks: set active epic: %w", err)
	}
	return nil
}

// ClearActiveEpic removes the active epic for a workspace.
func (s *sqlStore) ClearActiveEpic(ctx context.Context, workspaceID, sessionID string) error {
	workspaceID = ws.CanonicalID(workspaceID)
	_, err := s.db.ExecContext(ctx, `DELETE FROM active_epics WHERE workspace_id = $1 AND session_id = $2`, workspaceID, sessionID)
	if err != nil {
		return fmt.Errorf("tasks: clear active epic: %w", err)
	}
	return nil
}

// ListTasksByEpic returns tasks linked to a specific epic.
func (s *sqlStore) ListTasksByEpic(ctx context.Context, epicID string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, workspace_id, title, description, scope_path, parent_id, children, depends_on, status, created_at, completed_at, assigned_actor_id, assigned_at, owner_actor_id, claimed_at, heartbeat_at, blocked_reason, blocked_at, notes, gotchas, last_review_status, last_review_at, last_review_id, plan_file, plan_section, session_id, pagerank, epic_id, atomic_description, entities, keywords
FROM tasks WHERE epic_id = $1 ORDER BY created_at ASC`, epicID)
	if err != nil {
		return nil, fmt.Errorf("tasks: list by epic: %w", err)
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

// LinkTaskToEpic associates a task with an epic.
func (s *sqlStore) LinkTaskToEpic(ctx context.Context, taskID, epicID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET epic_id = $1 WHERE id = $2`, epicID, taskID)
	if err != nil {
		return fmt.Errorf("tasks: link to epic: %w", err)
	}
	return nil
}

// UpdateAtomic stores atomic processing results for a task.
// atomicDescription is the self-contained rewrite, entities are extracted identifiers,
// keywords are BM25-optimized search terms.
func (s *sqlStore) UpdateAtomic(ctx context.Context, id, atomicDescription string, entities, keywords []string) error {
	if entities == nil {
		entities = []string{}
	}
	if keywords == nil {
		keywords = []string{}
	}

	entitiesJSON, err := json.Marshal(entities)
	if err != nil {
		return fmt.Errorf("tasks: marshal entities: %w", err)
	}
	keywordsJSON, err := json.Marshal(keywords)
	if err != nil {
		return fmt.Errorf("tasks: marshal keywords: %w", err)
	}

	result, execErr := s.db.ExecContext(ctx, `
		UPDATE tasks
		SET atomic_description = $1, entities = $2, keywords = $3
		WHERE id = $4
	`, atomicDescription, string(entitiesJSON), string(keywordsJSON), id)
	if execErr != nil {
		return fmt.Errorf("tasks: update atomic: %w", execErr)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("tasks: task not found: %s", id)
	}
	return nil
}

// scanEpic scans a single epic from a row.
func scanEpic(row *sql.Row) (Epic, error) {
	var e Epic
	var goal, sessionID sql.NullString
	var createdAtStr string
	var completedAtStr sql.NullString

	err := row.Scan(&e.ID, &e.WorkspaceID, &e.Title, &goal, &e.Status,
		&createdAtStr, &completedAtStr, &sessionID)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return Epic{}, err
		}
		return Epic{}, fmt.Errorf("tasks: scan epic: %w", err)
	}

	e.Goal = goal.String
	e.SessionID = sessionID.String
	e.CreatedAt = timeutil.MustParseRFC3339Nano(createdAtStr)
	if completedAtStr.Valid {
		ct := timeutil.MustParseRFC3339Nano(completedAtStr.String)
		e.CompletedAt = &ct
	}

	return e, nil
}

// scanEpicRow scans an epic from rows (for iteration).
func scanEpicRow(rows *sql.Rows) (Epic, error) {
	var e Epic
	var goal, sessionID sql.NullString
	var createdAtStr string
	var completedAtStr sql.NullString

	err := rows.Scan(&e.ID, &e.WorkspaceID, &e.Title, &goal, &e.Status,
		&createdAtStr, &completedAtStr, &sessionID)
	if err != nil {
		return Epic{}, fmt.Errorf("tasks: scan epic row: %w", err)
	}

	e.Goal = goal.String
	e.SessionID = sessionID.String
	e.CreatedAt = timeutil.MustParseRFC3339Nano(createdAtStr)
	if completedAtStr.Valid {
		ct := timeutil.MustParseRFC3339Nano(completedAtStr.String)
		e.CompletedAt = &ct
	}

	return e, nil
}

// scanTaskRow scans a task from rows (for iteration).
func scanTaskRow(rows *sql.Rows) (Task, error) {
	var t Task
	var childrenJSON, dependsOnJSON string
	var createdAtStr string
	var completedAtStr sql.NullString
	var assignedAtStr, claimedAtStr, heartbeatAtStr, blockedAtStr sql.NullString
	var description, scopePath, parentID, assignedActorID, ownerActorID, blockedReason, notes, gotchas sql.NullString
	var lastReviewStatus, lastReviewAt, lastReviewID sql.NullString
	var planFile, planSection sql.NullString
	var sessionID sql.NullString
	var pagerank sql.NullFloat64
	var epicID sql.NullString
	var atomicDescription, entitiesJSON, keywordsJSON sql.NullString

	err := rows.Scan(
		&t.ID, &t.WorkspaceID, &t.Title, &description, &scopePath, &parentID,
		&childrenJSON, &dependsOnJSON, &t.Status, &createdAtStr, &completedAtStr, &assignedActorID, &assignedAtStr, &ownerActorID, &claimedAtStr, &heartbeatAtStr, &blockedReason, &blockedAtStr,
		&notes, &gotchas, &lastReviewStatus, &lastReviewAt, &lastReviewID,
		&planFile, &planSection, &sessionID, &pagerank, &epicID,
		&atomicDescription, &entitiesJSON, &keywordsJSON)
	if err != nil {
		return Task{}, fmt.Errorf("tasks: scan row: %w", err)
	}

	t.Description = description.String
	t.ScopePath = scopePath.String
	t.ParentID = parentID.String
	t.AssignedActorID = assignedActorID.String
	t.OwnerActorID = ownerActorID.String
	t.BlockedReason = blockedReason.String
	t.Notes = notes.String
	t.Gotchas = gotchas.String
	t.LastReviewStatus = lastReviewStatus.String
	t.LastReviewID = lastReviewID.String
	t.PlanFile = planFile.String
	t.PlanSection = planSection.String
	t.SessionID = sessionID.String
	t.PageRank = pagerank.Float64
	t.EpicID = epicID.String
	t.AtomicDescription = atomicDescription.String
	t.Entities = dbutil.ScanJSONArrayMust(entitiesJSON.String)
	t.Keywords = dbutil.ScanJSONArrayMust(keywordsJSON.String)

	t.CreatedAt = timeutil.MustParseRFC3339Nano(createdAtStr)
	if completedAtStr.Valid {
		ct := timeutil.MustParseRFC3339Nano(completedAtStr.String)
		t.CompletedAt = &ct
	}
	if assignedAtStr.Valid {
		at := timeutil.MustParseRFC3339Nano(assignedAtStr.String)
		t.AssignedAt = &at
	}
	if claimedAtStr.Valid {
		ct := timeutil.MustParseRFC3339Nano(claimedAtStr.String)
		t.ClaimedAt = &ct
	}
	if heartbeatAtStr.Valid {
		ht := timeutil.MustParseRFC3339Nano(heartbeatAtStr.String)
		t.HeartbeatAt = &ht
	}
	if blockedAtStr.Valid {
		bt := timeutil.MustParseRFC3339Nano(blockedAtStr.String)
		t.BlockedAt = &bt
	}
	if lastReviewAt.Valid {
		rt := timeutil.MustParseRFC3339Nano(lastReviewAt.String)
		t.LastReviewAt = &rt
	}

	t.Children = dbutil.ScanJSONArrayMust(childrenJSON)
	t.DependsOn = dbutil.ScanJSONArrayMust(dependsOnJSON)

	return t, nil
}

func retryTaskBusy(ctx context.Context, fn func() error) error {
	deadline := time.Now().Add(taskBusyRetryWindow)
	var lastErr error
	for {
		err := fn()
		if err == nil || !isTaskBusyErr(err) {
			return err
		}
		lastErr = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return lastErr
		}
		timer := time.NewTimer(taskBusyRetryStep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func isTaskBusyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "sqlite_busy")
}
