package persist

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/storage/jobs/types"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
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
	db    *sql.DB
	close func() error
	mu    sync.Mutex
}

// Open initializes the persistent store rooted at the provided path.
// Open opens a SQLite-backed job Store at root/jobs.db and applies migrations.
// If the on-disk database cannot be opened due to filesystem permissions, Open writes a brief warning to stderr and falls back to an in-memory store.
// On success it returns the Store; on failure it returns a non-nil error.
func Open(ctx context.Context, root string) (Store, error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "JOBS", "jobs.db", migrate)
	if err != nil {
		if isFilesystemAccessError(err) {
			fmt.Fprintf(os.Stderr, "warning: job store is not writable, using in-memory job store\n")
			fmt.Fprintf(os.Stderr, "hint: use `foxctl run --ephemeral` for transient skill runs, or set FOXCTL_STORAGE_ROOT/FOXCTL_JOBS_DB_PATH to a writable path\n")

			memDB, memErr := dbutil.OpenSQLiteInMemory(ctx, migrate)
			if memErr != nil {
				return nil, fmt.Errorf("jobs: open in-memory db: %w", memErr)
			}
			return &sqlStore{db: memDB, close: memDB.Close}, nil
		}
		return nil, fmt.Errorf("jobs: open db: %w", err)
	}
	return &sqlStore{db: db, close: closeFn}, nil
}

// isFilesystemAccessError reports whether err represents a local filesystem access
// failure that should degrade job persistence to an in-memory store. SQLite can
// surface sandboxed writes as generic "unable to open database file" errors
// while checking journal mode or creating WAL sidecars, so keep this broader than
// strictly read-only wording.
func isFilesystemAccessError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "readonly database") ||
		strings.Contains(errStr, "read-only file system") ||
		strings.Contains(errStr, "attempt to write a readonly database") ||
		strings.Contains(errStr, "operation not permitted") ||
		strings.Contains(errStr, "permission denied") ||
		strings.Contains(errStr, "unable to open database file")
}

func (s *sqlStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

type jobScanner interface {
	Scan(dest ...any) error
}

func scanJob(scanner jobScanner) (types.Job, error) {
	var job types.Job
	var created, updated, expires string
	if err := scanner.Scan(&job.ID, &job.Command, &job.ArgsJSON, &job.ArgsHash, &job.State, &job.ResultPath, &job.Error, &created, &updated, &expires); err != nil {
		return types.Job{}, fmt.Errorf("jobs: scan: %w", err)
	}
	if err := types.ValidateState(job.State); err != nil {
		return types.Job{}, err
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
	job.ExpiresAt, parseErr = sqlutil.ScanTimestamp(expires)
	if parseErr != nil {
		return types.Job{}, fmt.Errorf("jobs: scan expires_at: %w", parseErr)
	}
	return job, nil
}

func (s *sqlStore) List(ctx context.Context, limit int) ([]types.Job, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
	        SELECT id, command, args_json, args_hash, state, result_path, error, created_at, updated_at, expires_at
	        FROM jobs
	        ORDER BY created_at DESC
	        LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("jobs: list: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close jobs list rows")
	}()

	var jobs []types.Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (s *sqlStore) Get(ctx context.Context, id string) (types.Job, error) {
	row := s.db.QueryRowContext(ctx, `
	        SELECT id, command, args_json, args_hash, state, result_path, error, created_at, updated_at, expires_at
	        FROM jobs WHERE id = $1`, id)
	job, err := scanJob(row)
	if err != nil {
		if errorsIsNoRows(err) {
			return types.Job{}, types.ErrNotFound
		}
		return types.Job{}, fmt.Errorf("jobs: get: %w", err)
	}
	return job, nil
}

func (s *sqlStore) InsertJob(ctx context.Context, job types.Job) error {
	// Use INSERT ... ON CONFLICT DO NOTHING to handle rare ULID collisions from concurrent processes.
	// Each process has its own entropy source, so if multiple processes call ulid.Make()
	// at the same millisecond, they could theoretically produce the same ID.
	// When this happens (rows == 0), we treat the existing job as valid since
	// the caller will proceed with their job ID which already exists in the DB.
	if err := types.ValidateState(job.State); err != nil {
		return err
	}
	if job.ExpiresAt.IsZero() && !job.CreatedAt.IsZero() {
		job.ExpiresAt = job.CreatedAt.Add(types.DefaultMaxJobAge)
	}
	_, err := s.db.ExecContext(ctx, `
	        INSERT INTO jobs (id, command, args_json, args_hash, state, result_path, error, created_at, updated_at, expires_at)
	        VALUES ($1, $2, $3, $4, $5, '', '', $6, $7, $8)
	        ON CONFLICT DO NOTHING`,
		job.ID, job.Command, job.ArgsJSON, job.ArgsHash, job.State, sqlutil.FormatTimestamp(job.CreatedAt), sqlutil.FormatTimestamp(job.UpdatedAt), sqlutil.FormatTimestamp(job.ExpiresAt))
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
	// The dynamic portion only controls the number of positional placeholders, not the
	// SQL structure. All actual values are passed as parameterized arguments.
	allowedStates := validSourceStates(newState)
	if len(allowedStates) == 0 {
		return fmt.Errorf("%w: unknown target state %q", types.ErrInvalidState, newState)
	}

	args := []any{newState, errMsg, sqlutil.FormatTimestamp(time.Now().UTC())}
	query := `UPDATE jobs SET state = $1, error = $2, updated_at = $3`
	if resultPath != "" {
		query += fmt.Sprintf(", result_path = $%d", len(args)+1)
		args = append(args, resultPath)
	}

	query += fmt.Sprintf(" WHERE id = $%d AND state IN (", len(args)+1)
	args = append(args, id)

	placeholders := make([]string, len(allowedStates))
	for i, state := range allowedStates {
		placeholders[i] = fmt.Sprintf("$%d", len(args)+1)
		args = append(args, state)
	}
	query += strings.Join(placeholders, ", ") + ")"

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
		checkErr := s.db.QueryRowContext(ctx, `SELECT 1 FROM jobs WHERE id = $1`, id).Scan(&exists)
		if checkErr != nil {
			if errorsIsNoRows(checkErr) {
				return types.ErrNotFound
			}
			return fmt.Errorf("jobs: check existence: %w", checkErr)
		}
		var currentState types.State
		if scanErr := s.db.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id = $1`, id).Scan(&currentState); scanErr != nil {
			return fmt.Errorf("jobs: fetch current state: %w", scanErr)
		}
		return fmt.Errorf("%w: cannot transition from %s to %s", types.ErrInvalidState, currentState, newState)
	}
	return nil
}

func (s *sqlStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM jobs WHERE id = $1`, id)
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
	        SET state = $1, error = $2, updated_at = $3
	        WHERE state = $4`,
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

func (s *sqlStore) RecoverOrphanedJobsBefore(ctx context.Context, before time.Time) (int64, error) {
	cutoff := sqlutil.FormatTimestamp(before.UTC())
	now := time.Now().UTC()
	nowStamp := sqlutil.FormatTimestamp(now)
	result, err := s.db.ExecContext(ctx, `
	        UPDATE jobs
	        SET state = $1, error = $2, updated_at = $3
	        WHERE state = $4 AND ((expires_at != '' AND expires_at < $5) OR (expires_at = '' AND updated_at < $6))`,
		types.StateError,
		fmt.Sprintf("%s: stale running job", protocol.ErrorCodeERuntimeRestart),
		nowStamp,
		types.StateRunning,
		nowStamp,
		cutoff)
	if err != nil {
		return 0, fmt.Errorf("recover stale orphans: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("recover stale orphans: get rows affected: %w", err)
	}
	return rows, nil
}

func (s *sqlStore) FindDuplicateJob(ctx context.Context, argsHash string) (types.Job, error) {
	row := s.db.QueryRowContext(ctx, `
	        SELECT id, command, args_json, args_hash, state, result_path, error, created_at, updated_at, expires_at
	        FROM jobs
	        WHERE args_hash = $1
	        ORDER BY created_at DESC
	        LIMIT 1`, argsHash)

	job, err := scanJob(row)
	if err != nil {
		if errorsIsNoRows(err) {
			return types.Job{}, types.ErrNotFound
		}
		return types.Job{}, fmt.Errorf("jobs: find duplicate: %w", err)
	}
	return job, nil
}

// FindOrInsertJob returns an existing job by args hash or inserts a new job.
//
// Index:
//
//	Purpose: Deduplicate jobs by args hash while inserting atomically
//	Keywords: jobs_find_or_insert, args_hash, transaction, dedupe
//	Related: FindDuplicateJob, types.Job
//	Flow: begin tx → lookup by args_hash → return existing or insert new → commit
//	Resources: jobs table, sqlutil, ulid
//	Events: none
//	OutputFields: types.Job, bool (wasFound)
//
// [[invariant:args-hash-uniqueness-dedupes-identical-jobs]]
// [[protocol:job-state-machine]]
func (s *sqlStore) FindOrInsertJob(ctx context.Context, job types.Job) (types.Job, bool, error) {
	if err := types.ValidateState(job.State); err != nil {
		return types.Job{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return types.Job{}, false, fmt.Errorf("jobs: begin transaction: %w", err)
	}
	defer func() {
		errs.Ignore(tx.Rollback(), "rollback job find-or-insert txn")
	}()

	row := tx.QueryRowContext(ctx, `
	        SELECT id, command, args_json, args_hash, state, result_path, error, created_at, updated_at, expires_at
	        FROM jobs
	        WHERE args_hash = $1
	        ORDER BY created_at DESC
	        LIMIT 1`, job.ArgsHash)

	existing, scanErr := scanJob(row)
	if scanErr == nil {
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
	if job.ExpiresAt.IsZero() {
		job.ExpiresAt = job.CreatedAt.Add(types.DefaultMaxJobAge)
	}

	_, err = tx.ExecContext(ctx, `
	        INSERT INTO jobs (id, command, args_json, args_hash, state, result_path, error, created_at, updated_at, expires_at)
	        VALUES ($1, $2, $3, $4, $5, '', '', $6, $7, $8)`,
		job.ID, job.Command, job.ArgsJSON, job.ArgsHash, job.State, sqlutil.FormatTimestamp(job.CreatedAt), sqlutil.FormatTimestamp(job.UpdatedAt), sqlutil.FormatTimestamp(job.ExpiresAt))
	if err != nil {
		return types.Job{}, false, fmt.Errorf("jobs: insert: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return types.Job{}, false, fmt.Errorf("jobs: commit transaction: %w", err)
	}
	return job, false, nil
}

// MigrateSchema runs the jobs store DDL migrations against the given database.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	return migrate(ctx, db)
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
    updated_at TEXT NOT NULL,
    expires_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_jobs_state ON jobs(state);
CREATE INDEX IF NOT EXISTS idx_jobs_created ON jobs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_jobs_args_hash ON jobs(args_hash);
CREATE INDEX IF NOT EXISTS idx_jobs_expires ON jobs(expires_at);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("jobs: migrate: %w", err)
	}

	alterDDL := []string{
		`ALTER TABLE jobs ADD COLUMN expires_at TEXT NOT NULL DEFAULT ''`,
	}
	for _, stmt := range alterDDL {
		_, _ = db.ExecContext(ctx, stmt) //nolint:errcheck
	}
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_jobs_expires ON jobs(expires_at)`) //nolint:errcheck

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
		return []types.State{types.StateRunning}
	case types.StateError:
		return []types.State{types.StateQueued, types.StateRunning}
	case types.StateCanceled:
		return []types.State{types.StateQueued, types.StateRunning}
	default:
		return []types.State{}
	}
}
