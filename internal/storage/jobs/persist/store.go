// Package persist provides SQLite-backed persistence for job metadata and lifecycle management.
package persist

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/jobs/types"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
	"github.com/jkatigb/agentctl/internal/storage/sqlutil"
	"github.com/oklog/ulid/v2"
)

// Store defines the persistence interface for job metadata.
type Store interface {
	Close() error
	List(ctx context.Context, limit int) ([]types.Job, error)
	Get(ctx context.Context, id string) (types.Job, error)
	InsertJob(ctx context.Context, job types.Job) error
	UpdateState(ctx context.Context, id string, newState types.State, errMsg, resultPath string) error
	Delete(ctx context.Context, id string) error
	RecoverOrphanedJobs(ctx context.Context) (int64, error)
	FindDuplicateJob(ctx context.Context, argsHash string) (types.Job, error)
	FindOrInsertJob(ctx context.Context, job types.Job) (types.Job, bool, error)
}

type sqlStore struct {
	db *sql.DB
	mu sync.Mutex
}

// Open initializes the persistent store rooted at the provided path.
func Open(ctx context.Context, root string) (Store, error) {
	dbPath := filepath.Join(root, "jobs.db")
	db, err := sqliteutil.OpenDB(ctx, dbPath, migrate)
	if err != nil {
		return nil, fmt.Errorf("jobs: open db: %w", err)
	}
	return &sqlStore{db: db}, nil
}

func (s *sqlStore) Close() error {
	return s.db.Close()
}

func (s *sqlStore) List(ctx context.Context, limit int) ([]types.Job, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, command, args_json, args_hash, state, result_path, error, created_at, updated_at
        FROM jobs
        ORDER BY created_at DESC
        LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("jobs: list: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close jobs list rows")
	}()

	var jobs []types.Job
	for rows.Next() {
		var job types.Job
		var created, updated string
		if err := rows.Scan(&job.ID, &job.Command, &job.ArgsJSON, &job.ArgsHash, &job.State, &job.ResultPath, &job.Error, &created, &updated); err != nil {
			return nil, fmt.Errorf("jobs: scan: %w", err)
		}
		var parseErr error
		job.CreatedAt, parseErr = sqlutil.ScanTimestamp(created)
		if parseErr != nil {
			return nil, fmt.Errorf("jobs: scan created_at: %w", parseErr)
		}
		job.UpdatedAt, parseErr = sqlutil.ScanTimestamp(updated)
		if parseErr != nil {
			return nil, fmt.Errorf("jobs: scan updated_at: %w", parseErr)
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (s *sqlStore) Get(ctx context.Context, id string) (types.Job, error) {
	row := s.db.QueryRowContext(ctx, `
        SELECT id, command, args_json, args_hash, state, result_path, error, created_at, updated_at
        FROM jobs WHERE id = ?`, id)
	var job types.Job
	var created, updated string
	if err := row.Scan(&job.ID, &job.Command, &job.ArgsJSON, &job.ArgsHash, &job.State, &job.ResultPath, &job.Error, &created, &updated); err != nil {
		if errorsIsNoRows(err) {
			return types.Job{}, types.ErrNotFound
		}
		return types.Job{}, fmt.Errorf("jobs: get: %w", err)
	}
	var parseErr error
	job.CreatedAt, parseErr = sqlutil.ScanTimestamp(created)
	if parseErr != nil {
		return types.Job{}, fmt.Errorf("jobs: scan created_at: %w", parseErr)
	}
	job.UpdatedAt, parseErr = sqlutil.ScanTimestamp(updated)
	if parseErr != nil {
		return types.Job{}, fmt.Errorf("jobs: scan updated_at: %w", parseErr)
	}
	return job, nil
}

func (s *sqlStore) InsertJob(ctx context.Context, job types.Job) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO jobs (id, command, args_json, args_hash, state, result_path, error, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, '', '', ?, ?)`,
		job.ID, job.Command, job.ArgsJSON, job.ArgsHash, job.State, sqlutil.FormatTimestamp(job.CreatedAt), sqlutil.FormatTimestamp(job.UpdatedAt))
	if err != nil {
		return fmt.Errorf("jobs: insert: %w", err)
	}
	return nil
}

func (s *sqlStore) UpdateState(ctx context.Context, id string, newState types.State, errMsg string, resultPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Build dynamic query with state transition validation.
	// SQL injection safety: validSourceStates returns a hardcoded []State based on
	// the newState parameter. All values are typed State constants, not user input.
	// The dynamic portion only controls the number of '?' placeholders, not the
	// SQL structure. All actual values are passed as parameterized arguments.
	allowedStates := validSourceStates(newState)
	placeholders := make([]string, len(allowedStates))
	stateArgs := make([]any, len(allowedStates))
	for i, state := range allowedStates {
		placeholders[i] = "?"
		stateArgs[i] = state
	}
	stateConstraint := fmt.Sprintf("state IN (%s)", strings.Join(placeholders, ", "))

	var setResult string
	if resultPath != "" {
		setResult = ", result_path = ?"
	}
	query := fmt.Sprintf(`UPDATE jobs SET state = ?, error = ?, updated_at = ?%s WHERE id = ? AND %s`, setResult, stateConstraint)

	args := []any{newState, errMsg, sqlutil.FormatTimestamp(time.Now().UTC())}
	if resultPath != "" {
		args = append(args, resultPath)
	}
	args = append(args, id)
	args = append(args, stateArgs...)

	res, execErr := s.db.ExecContext(ctx, query, args...)
	if execErr != nil {
		return fmt.Errorf("jobs: update state: %w", execErr)
	}

	rows, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("jobs: update state rows affected: %w", rowsErr)
	}
	if rows == 0 {
		var exists bool
		checkErr := s.db.QueryRowContext(ctx, `SELECT 1 FROM jobs WHERE id = ?`, id).Scan(&exists)
		if checkErr != nil {
			if errorsIsNoRows(checkErr) {
				return types.ErrNotFound
			}
			return fmt.Errorf("jobs: check existence: %w", checkErr)
		}
		var currentState types.State
		if scanErr := s.db.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id = ?`, id).Scan(&currentState); scanErr != nil {
			return fmt.Errorf("jobs: fetch current state: %w", scanErr)
		}
		return fmt.Errorf("%w: cannot transition from %s to %s", types.ErrInvalidState, currentState, newState)
	}
	return nil
}

func (s *sqlStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM jobs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("jobs: delete: %w", err)
	}
	rows, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("jobs: delete rows affected: %w", rowsErr)
	}
	if rows == 0 {
		return types.ErrNotFound
	}
	return nil
}

func (s *sqlStore) RecoverOrphanedJobs(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
        UPDATE jobs
        SET state = ?, error = ?, updated_at = ?
        WHERE state = ?`,
		types.StateError,
		fmt.Sprintf("%s: process restarted", protocol.ErrorCodeERuntimeRestart),
		sqlutil.FormatTimestamp(time.Now().UTC()),
		types.StateRunning)
	if err != nil {
		return 0, fmt.Errorf("recover orphans: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("recover orphans: get rows affected: %w", err)
	}
	return rows, nil
}

func (s *sqlStore) FindDuplicateJob(ctx context.Context, argsHash string) (types.Job, error) {
	row := s.db.QueryRowContext(ctx, `
        SELECT id, command, args_json, args_hash, state, result_path, error, created_at, updated_at
        FROM jobs
        WHERE args_hash = ?
        ORDER BY created_at DESC
        LIMIT 1`, argsHash)

	var job types.Job
	var created, updated string
	if err := row.Scan(&job.ID, &job.Command, &job.ArgsJSON, &job.ArgsHash, &job.State, &job.ResultPath, &job.Error, &created, &updated); err != nil {
		if errorsIsNoRows(err) {
			return types.Job{}, types.ErrNotFound
		}
		return types.Job{}, fmt.Errorf("jobs: find duplicate: %w", err)
	}
	var parseErr error
	job.CreatedAt, parseErr = sqlutil.ScanTimestamp(created)
	if parseErr != nil {
		return types.Job{}, fmt.Errorf("jobs: scan created_at: %w", parseErr)
	}
	job.UpdatedAt, parseErr = sqlutil.ScanTimestamp(updated)
	if parseErr != nil {
		return types.Job{}, fmt.Errorf("jobs: scan updated_at: %w", parseErr)
	}
	return job, nil
}

func (s *sqlStore) FindOrInsertJob(ctx context.Context, job types.Job) (types.Job, bool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return types.Job{}, false, fmt.Errorf("jobs: begin transaction: %w", err)
	}
	defer func() {
		errs.Ignore(tx.Rollback(), "rollback job find-or-insert txn")
	}()

	row := tx.QueryRowContext(ctx, `
        SELECT id, command, args_json, args_hash, state, result_path, error, created_at, updated_at
        FROM jobs
        WHERE args_hash = ?
        ORDER BY created_at DESC
        LIMIT 1`, job.ArgsHash)

	var existing types.Job
	var created, updated string
	scanErr := row.Scan(&existing.ID, &existing.Command, &existing.ArgsJSON, &existing.ArgsHash, &existing.State, &existing.ResultPath, &existing.Error, &created, &updated)
	if scanErr == nil {
		var parseErr error
		existing.CreatedAt, parseErr = sqlutil.ScanTimestamp(created)
		if parseErr != nil {
			return types.Job{}, false, fmt.Errorf("jobs: scan created_at: %w", parseErr)
		}
		existing.UpdatedAt, parseErr = sqlutil.ScanTimestamp(updated)
		if parseErr != nil {
			return types.Job{}, false, fmt.Errorf("jobs: scan updated_at: %w", parseErr)
		}
		if err := tx.Commit(); err != nil {
			return types.Job{}, false, fmt.Errorf("jobs: commit transaction: %w", err)
		}
		return existing, true, nil
	}
	if !errorsIsNoRows(scanErr) {
		return types.Job{}, false, fmt.Errorf("jobs: find duplicate: %w", scanErr)
	}

	if job.ID == "" {
		job.ID = ulid.Make().String()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = job.CreatedAt
	}

	_, err = tx.ExecContext(ctx, `
        INSERT INTO jobs (id, command, args_json, args_hash, state, result_path, error, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, '', '', ?, ?)`,
		job.ID, job.Command, job.ArgsJSON, job.ArgsHash, job.State, sqlutil.FormatTimestamp(job.CreatedAt), sqlutil.FormatTimestamp(job.UpdatedAt))
	if err != nil {
		return types.Job{}, false, fmt.Errorf("jobs: insert: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return types.Job{}, false, fmt.Errorf("jobs: commit transaction: %w", err)
	}
	return job, false, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    command TEXT NOT NULL,
    args_json TEXT NOT NULL,
    args_hash TEXT NOT NULL,
    state TEXT NOT NULL,
    result_path TEXT,
    error TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_jobs_state ON jobs(state);
CREATE INDEX IF NOT EXISTS idx_jobs_created ON jobs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_jobs_args_hash ON jobs(args_hash);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("jobs: migrate: %w", err)
	}
	return nil
}

func errorsIsNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func validSourceStates(target types.State) []types.State {
	switch target {
	case types.StateQueued:
		return []types.State{types.StateQueued}
	case types.StateRunning:
		return []types.State{types.StateQueued, types.StateRunning}
	case types.StateOK:
		return []types.State{types.StateRunning, types.StateOK}
	case types.StateError:
		return []types.State{types.StateQueued, types.StateRunning, types.StateError}
	case types.StateCanceled:
		return []types.State{types.StateQueued, types.StateRunning, types.StateCanceled}
	default:
		return []types.State{}
	}
}
