package postreview

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/oklog/ulid/v2"
)

// ErrDuplicateEvent is returned when attempting to store an event with a
// (workspace_id, task_id, review_id) tuple that already exists with different
// payload.
var ErrDuplicateEvent = errors.New("duplicate post-review event with conflicting payload")

// ErrEventNotFound is returned when an event cannot be found.
var ErrEventNotFound = errors.New("post-review event not found")

// Store provides idempotent storage for PostReviewEvent records.
// Events are keyed by (workspace_id, task_id, review_id) and are immutable
// once persisted.
type Store interface {
	// Put stores a new event. If an event with the same
	// (workspace_id, task_id, review_id) already exists:
	//   - If payloads match, returns the existing event (idempotent).
	//   - If payloads differ, returns ErrDuplicateEvent.
	Put(ctx context.Context, event indexing.PostReviewEvent) (indexing.PostReviewEvent, error)

	// Get retrieves an event by ID.
	Get(ctx context.Context, id string) (indexing.PostReviewEvent, error)

	// GetByReview retrieves an event by (workspace_id, task_id, review_id).
	GetByReview(ctx context.Context, workspaceID, taskID, reviewID string) (indexing.PostReviewEvent, error)

	// List returns events for a workspace, ordered by created_at desc.
	List(ctx context.Context, workspaceID string, limit int) ([]indexing.PostReviewEvent, error)

	// Close releases any resources held by the store.
	Close() error
}

// sqlStore implements Store using SQLite.
type sqlStore struct {
	db    *sql.DB
	close func() error
}

// Open opens or creates a SQLite-backed Store at root+"/post_review_events.db" and applies the package's schema migrations.
func Open(ctx context.Context, root string) (Store, error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "POST_REVIEW", "post_review_events.db", migrate)
	if err != nil {
		return nil, fmt.Errorf("postreview: open db: %w", err)
	}
	return &sqlStore{db: db, close: closeFn}, nil
}

// migrate creates the post_review_events table and its indexes if they do not already exist.
//
// The table stores post-review events with columns for id, workspace_id, task_id, review_id,
// review_kind, review_status, diff_applied_at, source, created_at, sequence, files_json and metadata_json.
// It enforces a UNIQUE constraint on (workspace_id, task_id, review_id) and creates indexes on
// workspace_id and created_at DESC.
func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS post_review_events (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    review_id TEXT NOT NULL,
    review_kind TEXT NOT NULL,
    review_status TEXT NOT NULL,
    diff_applied_at TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    sequence INTEGER NOT NULL DEFAULT 0,
    files_json TEXT NOT NULL,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    UNIQUE(workspace_id, task_id, review_id)
);
CREATE INDEX IF NOT EXISTS idx_pre_workspace ON post_review_events(workspace_id);
CREATE INDEX IF NOT EXISTS idx_pre_created ON post_review_events(created_at DESC);
`
	_, err := db.ExecContext(ctx, ddl)
	return err
}

func (s *sqlStore) Put(ctx context.Context, event indexing.PostReviewEvent) (indexing.PostReviewEvent, error) {
	// Generate ID if not set
	if event.ID == "" {
		event.ID = ulid.Make().String()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if event.ReviewStatus == "" {
		event.ReviewStatus = "ok"
	}

	filesJSON, err := marshalFiles(event.Files)
	if err != nil {
		return event, fmt.Errorf("postreview: marshal files: %w", err)
	}
	metaJSON, err := marshalMetadata(event.Metadata)
	if err != nil {
		return event, fmt.Errorf("postreview: marshal metadata: %w", err)
	}

	// Use INSERT OR IGNORE to avoid TOCTOU race. If a concurrent Put wins,
	// this insert is a no-op and we fetch the existing row below.
	_, err = s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO post_review_events (
    id, workspace_id, task_id, review_id, review_kind, review_status,
    diff_applied_at, source, created_at, sequence, files_json, metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID,
		event.WorkspaceID,
		event.TaskID,
		event.ReviewID,
		event.ReviewKind,
		event.ReviewStatus,
		event.DiffAppliedAt.Format(time.RFC3339),
		event.Source,
		event.CreatedAt.Format(time.RFC3339),
		event.Sequence,
		filesJSON,
		metaJSON,
	)
	if err != nil {
		return event, fmt.Errorf("postreview: insert: %w", err)
	}

	// Fetch the canonical row (ours or the winner's)
	existing, err := s.GetByReview(ctx, event.WorkspaceID, event.TaskID, event.ReviewID)
	if err != nil {
		return event, fmt.Errorf("postreview: fetch after insert: %w", err)
	}

	// If existing row was inserted by another caller, verify payload matches
	if existing.ID != event.ID {
		if !eventsMatch(existing, event) {
			return event, ErrDuplicateEvent
		}
	}

	return existing, nil
}

func (s *sqlStore) Get(ctx context.Context, id string) (indexing.PostReviewEvent, error) {
	return s.scanOne(ctx, `SELECT * FROM post_review_events WHERE id = ?`, id)
}

func (s *sqlStore) GetByReview(ctx context.Context, workspaceID, taskID, reviewID string) (indexing.PostReviewEvent, error) {
	return s.scanOne(ctx,
		`SELECT * FROM post_review_events WHERE workspace_id = ? AND task_id = ? AND review_id = ?`,
		workspaceID, taskID, reviewID,
	)
}

func (s *sqlStore) List(ctx context.Context, workspaceID string, limit int) ([]indexing.PostReviewEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT * FROM post_review_events WHERE workspace_id = ? ORDER BY created_at DESC LIMIT ?`,
		workspaceID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("postreview: list: %w", err)
	}
	defer func() {
		// Rows cleanup in defer; error is not actionable after iteration.
		_ = rows.Close() //nolint:errcheck
	}()

	// Initialize to empty slice so JSON serializes as [] not null
	events := make([]indexing.PostReviewEvent, 0)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *sqlStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

func (s *sqlStore) scanOne(ctx context.Context, query string, args ...any) (indexing.PostReviewEvent, error) {
	row := s.db.QueryRowContext(ctx, query, args...)
	return scanEventRow(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanEvent(s scanner) (indexing.PostReviewEvent, error) {
	var (
		event        indexing.PostReviewEvent
		diffApplied  string
		createdAt    string
		filesJSON    string
		metadataJSON string
	)
	err := s.Scan(
		&event.ID,
		&event.WorkspaceID,
		&event.TaskID,
		&event.ReviewID,
		&event.ReviewKind,
		&event.ReviewStatus,
		&diffApplied,
		&event.Source,
		&createdAt,
		&event.Sequence,
		&filesJSON,
		&metadataJSON,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return event, ErrEventNotFound
		}
		return event, fmt.Errorf("postreview: scan: %w", err)
	}

	event.DiffAppliedAt, err = time.Parse(time.RFC3339, diffApplied)
	if err != nil {
		return event, fmt.Errorf("postreview: parse diff_applied_at %q: %w", diffApplied, err)
	}
	event.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return event, fmt.Errorf("postreview: parse created_at %q: %w", createdAt, err)
	}
	event.Files, err = unmarshalFiles(filesJSON)
	if err != nil {
		return event, fmt.Errorf("postreview: unmarshal files_json: %w", err)
	}
	event.Metadata, err = unmarshalMetadata(metadataJSON)
	if err != nil {
		return event, fmt.Errorf("postreview: unmarshal metadata_json: %w", err)
	}

	return event, nil
}

func scanEventRow(row *sql.Row) (indexing.PostReviewEvent, error) {
	return scanEvent(row)
}

// eventsMatch checks if two events have the same logical payload.
// This is used for idempotence: if the same (workspace, task, review)
// is submitted with identical data, we return the existing event.
func eventsMatch(a, b indexing.PostReviewEvent) bool {
	if a.ReviewKind != b.ReviewKind {
		return false
	}
	if len(a.Files) != len(b.Files) {
		return false
	}
	for i := range a.Files {
		if a.Files[i].Path != b.Files[i].Path ||
			a.Files[i].Digest != b.Files[i].Digest ||
			a.Files[i].ChangeKind != b.Files[i].ChangeKind {
			return false
		}
	}
	return true
}
