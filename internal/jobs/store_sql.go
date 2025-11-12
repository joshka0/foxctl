package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // sqlite driver
)

// Store persists job metadata and results using a SQLite database.
type Store struct {
	db   *sql.DB
	root string
	mu   sync.Mutex
}

// Open initializes the store rooted at the provided path.
func Open(ctx context.Context, root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("jobs: ensure root: %w", err)
	}

	dbPath := filepath.Join(root, "jobs.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("jobs: open db: %w", err)
	}

	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("jobs: pragma: %w", err)
	}

	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{db: db, root: root}, nil
}

// Close releases database resources.
func (s *Store) Close() error {
	return s.db.Close()
}

// List returns jobs ordered by creation time descending.
func (s *Store) List(ctx context.Context, limit int) ([]Job, error) {
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
	defer func() { _ = rows.Close() }()

	var jobs []Job
	for rows.Next() {
		var job Job
		var created, updated string
		if err := rows.Scan(&job.ID, &job.Command, &job.ArgsJSON, &job.ArgsHash, &job.State, &job.ResultPath, &job.Error, &created, &updated); err != nil {
			return nil, fmt.Errorf("jobs: scan: %w", err)
		}

		var parseErr error
		job.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, created)
		if parseErr != nil {
			return nil, fmt.Errorf("jobs: parse created_at: %w", parseErr)
		}
		job.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updated)
		if parseErr != nil {
			return nil, fmt.Errorf("jobs: parse updated_at: %w", parseErr)
		}

		jobs = append(jobs, job)
	}

	return jobs, nil
}

// Get returns a single job by id.
func (s *Store) Get(ctx context.Context, id string) (Job, error) {
	row := s.db.QueryRowContext(ctx, `
                SELECT id, command, args_json, args_hash, state, result_path, error, created_at, updated_at
                FROM jobs WHERE id = ?`, id)

	var job Job
	var created, updated string
	if err := row.Scan(&job.ID, &job.Command, &job.ArgsJSON, &job.ArgsHash, &job.State, &job.ResultPath, &job.Error, &created, &updated); err != nil {
		if errorsIsNoRows(err) {
			return Job{}, ErrNotFound
		}
		return Job{}, fmt.Errorf("jobs: get: %w", err)
	}

	var parseErr error
	job.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, created)
	if parseErr != nil {
		return Job{}, fmt.Errorf("jobs: parse created_at: %w", parseErr)
	}
	job.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updated)
	if parseErr != nil {
		return Job{}, fmt.Errorf("jobs: parse updated_at: %w", parseErr)
	}

	return job, nil
}

// Result loads the result envelope for a job.
func (s *Store) Result(ctx context.Context, id string) ([]byte, error) {
	job, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if job.ResultPath == "" {
		return nil, fmt.Errorf("jobs: result not available")
	}

	data, err := os.ReadFile(job.ResultPath)
	if err != nil {
		return nil, fmt.Errorf("jobs: read result: %w", err)
	}

	return data, nil
}

// Cancel attempts to mark a job as canceled if it is still pending.
func (s *Store) Cancel(ctx context.Context, id string) error {
	job, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	switch job.State {
	case StateQueued, StateRunning:
		return s.updateState(ctx, id, StateCanceled, "", "")
	default:
		return fmt.Errorf("%w: job already %s", ErrInvalidState, job.State)
	}
}

// Delete removes the job record from the database.
func (s *Store) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM jobs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("jobs: delete: %w", err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}

	return nil
}

// RecoverOrphanedJobs marks any running jobs as error (crash recovery).
// This should be called explicitly during worker startup, NOT on every Open().
// Returns the number of jobs recovered.
func (s *Store) RecoverOrphanedJobs(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
                UPDATE jobs
                SET state = ?, error = ?, updated_at = ?
                WHERE state = ?`,
		StateError, "ERUNTIME_RESTART: process restarted", time.Now().UTC().Format(time.RFC3339Nano), StateRunning)
	if err != nil {
		return 0, fmt.Errorf("recover orphans: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("recover orphans: get rows affected: %w", err)
	}

	return rows, nil
}

// FindDuplicateJob searches for an existing job with the same args_hash.
func (s *Store) FindDuplicateJob(ctx context.Context, argsHash string) (Job, error) {
	row := s.db.QueryRowContext(ctx, `
                SELECT id, command, args_json, args_hash, state, result_path, error, created_at, updated_at
                FROM jobs
                WHERE args_hash = ?
                ORDER BY created_at DESC
                LIMIT 1`, argsHash)

	var job Job
	var created, updated string
	if err := row.Scan(&job.ID, &job.Command, &job.ArgsJSON, &job.ArgsHash, &job.State, &job.ResultPath, &job.Error, &created, &updated); err != nil {
		if errorsIsNoRows(err) {
			return Job{}, ErrNotFound
		}
		return Job{}, fmt.Errorf("jobs: find duplicate: %w", err)
	}

	var parseErr error
	job.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, created)
	if parseErr != nil {
		return Job{}, fmt.Errorf("jobs: parse created_at: %w", parseErr)
	}
	job.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updated)
	if parseErr != nil {
		return Job{}, fmt.Errorf("jobs: parse updated_at: %w", parseErr)
	}

	return job, nil
}

func (s *Store) insertJob(ctx context.Context, job Job) error {
	_, err := s.db.ExecContext(ctx, `
                INSERT INTO jobs (id, command, args_json, args_hash, state, result_path, error, created_at, updated_at)
                VALUES (?, ?, ?, ?, ?, '', '', ?, ?)`,
		job.ID, job.Command, job.ArgsJSON, job.ArgsHash, job.State,
		job.CreatedAt.Format(time.RFC3339Nano), job.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("jobs: insert: %w", err)
	}

	return nil
}

func (s *Store) updateState(ctx context.Context, id string, newState State, errMsg string, resultPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

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

	args := []any{newState, errMsg, time.Now().UTC().Format(time.RFC3339Nano)}
	if resultPath != "" {
		args = append(args, resultPath)
	}
	args = append(args, id)
	args = append(args, stateArgs...)

	res, execErr := s.db.ExecContext(ctx, query, args...)
	if execErr != nil {
		return fmt.Errorf("jobs: update state: %w", execErr)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("jobs: update state rows affected: %w", err)
	}
	if rows == 0 {
		var exists bool
		checkErr := s.db.QueryRowContext(ctx, `SELECT 1 FROM jobs WHERE id = ?`, id).Scan(&exists)
		if checkErr != nil {
			if errorsIsNoRows(checkErr) {
				return ErrNotFound
			}
			return fmt.Errorf("jobs: check existence: %w", checkErr)
		}

		var currentState State
		if err := s.db.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id = ?`, id).Scan(&currentState); err != nil {
			return fmt.Errorf("jobs: get current state: %w", err)
		}
		return fmt.Errorf("%w: cannot transition from %s to %s", ErrInvalidState, currentState, newState)
	}

	return nil
}

func validSourceStates(target State) []State {
	switch target {
	case StateQueued:
		return []State{StateQueued}
	case StateRunning:
		return []State{StateQueued, StateRunning}
	case StateOK:
		return []State{StateRunning, StateOK}
	case StateError:
		return []State{StateQueued, StateRunning, StateError}
	case StateCanceled:
		return []State{StateQueued, StateRunning, StateCanceled}
	default:
		return []State{}
	}
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
	return err == sql.ErrNoRows
}

func (s *Store) jobDir(id string) string {
	return filepath.Join(s.root, id)
}
