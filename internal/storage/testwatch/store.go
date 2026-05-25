package testwatch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/platform/timeutil"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
)

// Status represents the state of a watcher.
type Status string

// Status constants representing the test run outcomes.
const (
	StatusUnknown Status = "unknown"
	StatusPass    Status = "pass"
	StatusFail    Status = "fail"
	StatusError   Status = "error"
	StatusRunning Status = "running"
)

// Failure represents a single test failure with location information.
type Failure struct {
	Name    string `json:"name"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message,omitempty"`
}

// TestStatus represents the latest status for a watcher in a workspace.
type TestStatus struct {
	WorkspaceID string
	WatcherID   string
	Status      Status
	Command     string
	StartedAt   *time.Time
	FinishedAt  *time.Time
	Summary     string
	Failures    []Failure
	RawTail     string
}

func validateStatus(status Status) error {
	switch status {
	case StatusUnknown, StatusPass, StatusFail, StatusError, StatusRunning:
		return nil
	default:
		return fmt.Errorf("testwatch: invalid status %q", status)
	}
}

// Store defines the persistence interface for test watcher status.
type Store interface {
	Close() error

	// Upsert creates or updates the test status for a (workspace, watcher) pair.
	Upsert(ctx context.Context, ts TestStatus) error

	// Get returns the test status for a (workspace, watcher) pair.
	Get(ctx context.Context, workspaceID, watcherID string) (TestStatus, bool, error)

	// ListByWorkspace returns all test statuses for a workspace.
	ListByWorkspace(ctx context.Context, workspaceID string) ([]TestStatus, error)

	// Delete removes the test status for a (workspace, watcher) pair.
	Delete(ctx context.Context, workspaceID, watcherID string) error

	// DeleteByWorkspace removes all test statuses for a workspace.
	DeleteByWorkspace(ctx context.Context, workspaceID string) error
}

type sqlStore struct {
	db    *sql.DB
	close func() error
}

// Open opens or creates the SQLite database at root/test_watch.db, applies the package migrations, and returns a Store backed by that database.
// The returned store uses a shared SQLite connection and will invoke the underlying close function when closed.
// Any error encountered while opening the database or running migrations is wrapped and returned.
func Open(ctx context.Context, root string) (Store, error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "TESTWATCH", "test_watch.db", migrate)
	if err != nil {
		return nil, fmt.Errorf("testwatch: open db: %w", err)
	}
	return &sqlStore{db: db, close: closeFn}, nil
}

// Close releases database resources.
func (s *sqlStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// migrate creates the database schema required by the package.
// It ensures the `test_status` table exists with columns for workspace_id, watcher_id,
// status, command, started_at, finished_at, summary, failures_json, and raw_tail,
// and creates an index on workspace_id. Returns an error if applying the DDL fails.
// MigrateSchema runs the testwatch store DDL migrations against the given database.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	return migrate(ctx, db)
}

func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS test_status (
	workspace_id TEXT NOT NULL,
	watcher_id   TEXT NOT NULL,
	status       TEXT NOT NULL,
	command      TEXT NOT NULL,
	started_at   TEXT,
	finished_at  TEXT,
	summary      TEXT,
	failures_json TEXT,
	raw_tail     TEXT,
	PRIMARY KEY (workspace_id, watcher_id)
);
CREATE INDEX IF NOT EXISTS idx_test_status_workspace ON test_status(workspace_id);
`
	_, err := db.ExecContext(ctx, ddl)
	if err != nil {
		return fmt.Errorf("testwatch: migrate: %w", err)
	}
	return nil
}

// Upsert creates or updates the test status for a (workspace, watcher) pair.
func (s *sqlStore) Upsert(ctx context.Context, ts TestStatus) error {
	if err := validateStatus(ts.Status); err != nil {
		return err
	}
	if err := validateFailures(ts.Failures); err != nil {
		return err
	}
	failures := ts.Failures
	if failures == nil {
		failures = []Failure{}
	}
	failuresJSON, err := json.Marshal(failures)
	if err != nil {
		return fmt.Errorf("testwatch: marshal failures: %w", err)
	}

	var startedAt, finishedAt sql.NullString
	if ts.StartedAt != nil {
		startedAt = sql.NullString{String: timeutil.FormatRFC3339Nano(*ts.StartedAt), Valid: true}
	}
	if ts.FinishedAt != nil {
		finishedAt = sql.NullString{String: timeutil.FormatRFC3339Nano(*ts.FinishedAt), Valid: true}
	}

	_, err = s.db.ExecContext(
		ctx, `
	INSERT INTO test_status (workspace_id, watcher_id, status, command, started_at, finished_at, summary, failures_json, raw_tail)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	ON CONFLICT (workspace_id, watcher_id) DO UPDATE SET
		status = excluded.status,
		command = excluded.command,
		started_at = excluded.started_at,
	finished_at = excluded.finished_at,
	summary = excluded.summary,
	failures_json = excluded.failures_json,
	raw_tail = excluded.raw_tail
`,
		ts.WorkspaceID, ts.WatcherID, ts.Status, ts.Command,
		startedAt, finishedAt, ts.Summary, string(failuresJSON), ts.RawTail,
	)
	if err != nil {
		return fmt.Errorf("testwatch: upsert: %w", err)
	}
	return nil
}

// Get returns the test status for a (workspace, watcher) pair.
func (s *sqlStore) Get(ctx context.Context, workspaceID, watcherID string) (TestStatus, bool, error) {
	row := s.db.QueryRowContext(ctx, `
	SELECT workspace_id, watcher_id, status, command, started_at, finished_at, summary, failures_json, raw_tail
	FROM test_status
	WHERE workspace_id = $1 AND watcher_id = $2
	`, workspaceID, watcherID)

	ts, err := scanTestStatus(row)
	if err == sql.ErrNoRows {
		return TestStatus{}, false, nil
	}
	if err != nil {
		return TestStatus{}, false, fmt.Errorf("testwatch: get: %w", err)
	}
	return ts, true, nil
}

// ListByWorkspace returns all test statuses for a workspace.
func (s *sqlStore) ListByWorkspace(ctx context.Context, workspaceID string) ([]TestStatus, error) {
	rows, err := s.db.QueryContext(ctx, `
	SELECT workspace_id, watcher_id, status, command, started_at, finished_at, summary, failures_json, raw_tail
	FROM test_status
	WHERE workspace_id = $1
	ORDER BY watcher_id
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("testwatch: list by workspace: %w", err)
	}
	defer func() {
		// Rows cleanup in defer; error is not actionable after iteration.
		_ = rows.Close() //nolint:errcheck
	}()

	var results []TestStatus
	for rows.Next() {
		ts, err := scanTestStatusRows(rows)
		if err != nil {
			return nil, fmt.Errorf("testwatch: scan: %w", err)
		}
		results = append(results, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("testwatch: rows: %w", err)
	}
	return results, nil
}

// Delete removes the test status for a (workspace, watcher) pair.
func (s *sqlStore) Delete(ctx context.Context, workspaceID, watcherID string) error {
	_, err := s.db.ExecContext(ctx, `
	DELETE FROM test_status WHERE workspace_id = $1 AND watcher_id = $2
	`, workspaceID, watcherID)
	if err != nil {
		return fmt.Errorf("testwatch: delete: %w", err)
	}
	return nil
}

// DeleteByWorkspace removes all test statuses for a workspace.
func (s *sqlStore) DeleteByWorkspace(ctx context.Context, workspaceID string) error {
	_, err := s.db.ExecContext(ctx, `
	DELETE FROM test_status WHERE workspace_id = $1
	`, workspaceID)
	if err != nil {
		return fmt.Errorf("testwatch: delete by workspace: %w", err)
	}
	return nil
}

// scanTestStatus scans a single row into a TestStatus.
func scanTestStatus(row *sql.Row) (TestStatus, error) {
	var ts TestStatus
	var startedAt, finishedAt, failuresJSON sql.NullString

	err := row.Scan(
		&ts.WorkspaceID, &ts.WatcherID, &ts.Status, &ts.Command,
		&startedAt, &finishedAt, &ts.Summary, &failuresJSON, &ts.RawTail,
	)
	if err != nil {
		return TestStatus{}, err
	}
	if err := validateStatus(ts.Status); err != nil {
		return TestStatus{}, fmt.Errorf("decode status: %w", err)
	}

	if startedAt.Valid {
		parsed, err := parseTimePtr(startedAt.String, "started_at")
		if err != nil {
			return TestStatus{}, err
		}
		ts.StartedAt = parsed
	}
	if finishedAt.Valid {
		parsed, err := parseTimePtr(finishedAt.String, "finished_at")
		if err != nil {
			return TestStatus{}, err
		}
		ts.FinishedAt = parsed
	}
	failures, err := decodeFailuresJSON(failuresJSON)
	if err != nil {
		return TestStatus{}, err
	}
	ts.Failures = failures

	return ts, nil
}

// scanTestStatusRows scans rows into a TestStatus.
func scanTestStatusRows(rows *sql.Rows) (TestStatus, error) {
	var ts TestStatus
	var startedAt, finishedAt, failuresJSON sql.NullString

	err := rows.Scan(
		&ts.WorkspaceID, &ts.WatcherID, &ts.Status, &ts.Command,
		&startedAt, &finishedAt, &ts.Summary, &failuresJSON, &ts.RawTail,
	)
	if err != nil {
		return TestStatus{}, err
	}
	if err := validateStatus(ts.Status); err != nil {
		return TestStatus{}, fmt.Errorf("decode status: %w", err)
	}

	if startedAt.Valid {
		parsed, err := parseTimePtr(startedAt.String, "started_at")
		if err != nil {
			return TestStatus{}, err
		}
		ts.StartedAt = parsed
	}
	if finishedAt.Valid {
		parsed, err := parseTimePtr(finishedAt.String, "finished_at")
		if err != nil {
			return TestStatus{}, err
		}
		ts.FinishedAt = parsed
	}
	failures, err := decodeFailuresJSON(failuresJSON)
	if err != nil {
		return TestStatus{}, err
	}
	ts.Failures = failures

	return ts, nil
}

func validateFailures(failures []Failure) error {
	for i, failure := range failures {
		if failure.Line < 0 {
			return fmt.Errorf("testwatch: invalid failure %d line %d", i, failure.Line)
		}
	}
	return nil
}

func decodeFailuresJSON(raw sql.NullString) ([]Failure, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var failures []Failure
	if err := json.Unmarshal([]byte(raw.String), &failures); err != nil {
		return nil, fmt.Errorf("decode failures_json: %w", err)
	}
	if failures == nil {
		return nil, fmt.Errorf("decode failures_json: expected array")
	}
	if err := validateFailures(failures); err != nil {
		return nil, fmt.Errorf("decode failures_json: %w", err)
	}
	return failures, nil
}

func parseTimePtr(s, field string) (*time.Time, error) {
	ts, err := sqlutil.ScanTimestamp(s)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", field, err)
	}
	if !ts.IsZero() {
		return &ts, nil
	}
	return nil, nil
}
