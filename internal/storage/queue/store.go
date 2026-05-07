package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
	"github.com/oklog/ulid/v2"
)

// ErrNotFound indicates the requested job doesn't exist.
var ErrNotFound = errors.New("not found")

// Options configures a queue store.
type Options struct {
	Table string
}

// Store manages a queue table in SQLite.
type Store struct {
	db     *sql.DB
	table  string
	ownsDB bool
	close  func() error
}

// Open opens a SQLite-backed queue store and applies table migrations.
//
// Index:
//
//	Purpose: Initialize queue storage for a named table
//	Keywords: queue, migrate, table, dbutil.OpenSQLiteDBShared, Migrate
//	Related: dbutil.OpenSQLiteDBShared, Migrate
//	Flow: normalize table -> open shared db -> migrate -> return store
//	Resources: sqlite DB, queue table
//	Events: none
//	OutputFields: *Store
//
// [[invariant:table-name-alphanumeric-underscore-only]]
// [[risk:sql-injection-via-table-name]]
func Open(ctx context.Context, dbPath string, opts Options) (*Store, error) {
	table, err := normalizeTableName(opts.Table)
	if err != nil {
		return nil, err
	}
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, dbPath, func(ctx context.Context, db *sql.DB) error {
		return Migrate(ctx, db, Options{Table: table})
	})
	if err != nil {
		return nil, fmt.Errorf("queue: open db: %w", err)
	}
	return &Store{db: db, table: table, ownsDB: true, close: closeFn}, nil
}

// OpenStore opens a queue Store using the canonical per-store dbdriver configuration.
//
// This is preferred over Open/OpenInRoot when the queue corresponds to a named store in the
// foxctl store registry (e.g., SUMMARY_QUEUE).
//
// Index:
//
//	Purpose: Open a queue DB via standardized store configuration (sqlite/turso/postgres)
//	Keywords: queue, store_db, dbdriver, migrate, summary_queue
//	Related: dbutil.OpenStoreDB, Migrate
//	Flow: normalize table → open store DB via dbutil.OpenStoreDB → migrate table → return store
//	Resources: sqlite/turso/postgres DB
//	Events: none
//	OutputFields: *Store
//
// [[protocol:store-driver-abstraction]]
// [[invariant:table-name-alphanumeric-underscore-only]]
func OpenStore(ctx context.Context, storageRoot, storeName, defaultFile string, opts Options) (*Store, error) {
	table, err := normalizeTableName(opts.Table)
	if err != nil {
		return nil, err
	}
	db, closeFn, err := dbutil.OpenStoreDB(ctx, storageRoot, storeName, defaultFile, func(ctx context.Context, db *sql.DB) error {
		return Migrate(ctx, db, Options{Table: table})
	})
	if err != nil {
		return nil, fmt.Errorf("queue: open store db: %w", err)
	}
	return &Store{db: db, table: table, ownsDB: true, close: closeFn}, nil
}

// OpenInRoot opens a queue Store located at the filesystem path formed by joining
// the provided root directory and filename.
func OpenInRoot(ctx context.Context, root, filename string, opts Options) (*Store, error) {
	return Open(ctx, filepath.Join(root, filename), opts)
}

// NewStore wraps an existing database connection.
func NewStore(db *sql.DB, opts Options) (*Store, error) {
	table, err := normalizeTableName(opts.Table)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, table: table, ownsDB: false}, nil
}

// Close closes the store if it owns the database handle.
func (s *Store) Close() error {
	if s == nil || !s.ownsDB {
		return nil
	}
	if s.close != nil {
		return s.close()
	}
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Migrate creates the queue table and its supporting indexes in db if they do not exist.
// It validates and normalizes opts.Table and returns an error if the table name is invalid.
// The created table includes a UNIQUE constraint on (group_id, dedupe_key) and the function
// also creates indexes for (state, priority DESC, created_at) and (group_id, dedupe_key).
func Migrate(ctx context.Context, db *sql.DB, opts Options) error {
	table, err := normalizeTableName(opts.Table)
	if err != nil {
		return err
	}
	schema := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			group_id TEXT NOT NULL,
			payload BLOB NOT NULL,
			dedupe_key TEXT NOT NULL,
			state TEXT NOT NULL DEFAULT 'queued',
			priority INTEGER NOT NULL DEFAULT 50,
			attempts INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL DEFAULT 3,
			error TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			scheduled_at TEXT,
			completed_at TEXT,
			UNIQUE(group_id, dedupe_key)
		);

		CREATE INDEX IF NOT EXISTS %s_state_priority
			ON %s(state, priority DESC, created_at);

		CREATE INDEX IF NOT EXISTS %s_group_dedupe
			ON %s(group_id, dedupe_key);
	`, table, table, table, table, table)

	_, err = db.ExecContext(ctx, schema)
	return err
}

// Enqueue adds a single job to the queue.
func (s *Store) Enqueue(ctx context.Context, req EnqueueRequest) (*EnqueueResult, error) {
	result, err := s.EnqueueBatch(ctx, []EnqueueRequest{req})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// EnqueueBatch adds multiple jobs to the queue in one transaction.
//
// Index:
//
//	Purpose: Insert queued jobs with dedupe protection
//	Keywords: enqueue, group_id, dedupe_key, priority, max_attempts, ulid.Make
//	Related: sqlutil.FormatTimestamp, ulid.Make
//	Flow: begin tx -> prepare insert -> validate fields -> insert jobs -> commit
//	Resources: queue table
//	Events: none
//	OutputFields: EnqueueResult.Queued, EnqueueResult.Skipped, EnqueueResult.JobIDs
//
// [[invariant:dedupe-via-unique-group-dedupe-key]]
// [[test-contract:enqueue-batch-atomicity]]
func (s *Store) EnqueueBatch(ctx context.Context, reqs []EnqueueRequest) (*EnqueueResult, error) {
	result := &EnqueueResult{}
	if len(reqs) == 0 {
		return result, nil
	}

	now := time.Now().UTC()
	nowStr := sqlutil.FormatTimestamp(now)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(`
		INSERT INTO %s
			(id, group_id, payload, dedupe_key, state, priority, max_attempts, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'queued', ?, ?, ?, ?)
		ON CONFLICT(group_id, dedupe_key) DO NOTHING
	`, s.table))
	if err != nil {
		return nil, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, req := range reqs {
		if req.GroupID == "" {
			return nil, fmt.Errorf("group_id is required")
		}
		if req.DedupeKey == "" {
			return nil, fmt.Errorf("dedupe_key is required")
		}

		priority := req.Priority
		if priority == 0 {
			priority = PriorityNormal
		}
		maxAttempts := req.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = DefaultMaxAttempts
		}

		id := ulid.Make().String()
		res, err := stmt.ExecContext(ctx,
			id, req.GroupID, req.Payload, req.DedupeKey, priority, maxAttempts, nowStr, nowStr,
		)
		if err != nil {
			return nil, fmt.Errorf("insert job: %w", err)
		}
		affected, _ := res.RowsAffected()
		if affected > 0 {
			result.Queued++
			result.JobIDs = append(result.JobIDs, id)
		} else {
			result.Skipped++
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return result, nil
}

// ClaimNext claims the next available job for processing.
func (s *Store) ClaimNext(ctx context.Context, opts ClaimOptions) (*Job, error) {
	now := time.Now().UTC()
	nowStr := sqlutil.FormatTimestamp(now)

	groupClause := ""
	args := []any{StateRunning, nowStr, StateQueued, StateRetry, nowStr}
	if opts.GroupID != "" {
		groupClause = " AND group_id = ?"
		args = append(args, opts.GroupID)
	}
	kindClause := ""
	if predicate, predicateArgs := payloadKindPredicate(opts.PayloadKind); predicate != "" {
		kindClause = " AND " + predicate
		args = append(args, predicateArgs...)
	}

	query := fmt.Sprintf(`
		UPDATE %s
		SET state = ?, updated_at = ?, attempts = attempts + 1
		WHERE id = (
			SELECT id FROM %s
			WHERE state IN (?, ?)
			AND (scheduled_at IS NULL OR scheduled_at <= ?)%s%s
			ORDER BY priority DESC, created_at ASC
			LIMIT 1
		)
		RETURNING id, group_id, payload, dedupe_key, state, priority, attempts, max_attempts, error,
		          created_at, updated_at, scheduled_at, completed_at
	`, s.table, s.table, groupClause, kindClause)

	row := s.db.QueryRowContext(ctx, query, args...)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return job, nil
}

// Complete marks a job as completed successfully.
func (s *Store) Complete(ctx context.Context, jobID string) error {
	nowStr := sqlutil.FormatTimestamp(time.Now().UTC())
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s
		SET state = 'ok', updated_at = ?, completed_at = ?, error = NULL
		WHERE id = ?
	`, s.table), nowStr, nowStr, jobID)
	return err
}

// Fail records a job failure, scheduling retries when allowed.
func (s *Store) Fail(ctx context.Context, jobID string, errMsg string) error {
	now := time.Now().UTC()
	nowStr := sqlutil.FormatTimestamp(now)

	var attempts, maxAttempts int
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT attempts, max_attempts FROM %s WHERE id = ?
	`, s.table), jobID).Scan(&attempts, &maxAttempts)
	if err != nil {
		return fmt.Errorf("get job: %w", err)
	}

	if attempts < maxAttempts {
		backoff := time.Duration(1<<uint(max(attempts, 1)-1)) * time.Minute
		scheduledAt := sqlutil.FormatTimestamp(now.Add(backoff))
		_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
			UPDATE %s
			SET state = 'retry', updated_at = ?, scheduled_at = ?, error = ?
			WHERE id = ?
		`, s.table), nowStr, scheduledAt, errMsg, jobID)
		return err
	}

	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s
		SET state = 'error', updated_at = ?, completed_at = ?, error = ?
		WHERE id = ?
	`, s.table), nowStr, nowStr, errMsg, jobID)
	return err
}

// RequeueStaleRunning moves running jobs older than olderThan back to retry.
//
// This is an explicit crash-recovery operation for workers that may be killed
// while a job is in progress. It is intentionally opt-in so a second worker
// does not steal a legitimately long-running job by default.
func (s *Store) RequeueStaleRunning(ctx context.Context, olderThan time.Duration) (int64, error) {
	return s.requeueStaleRunning(ctx, olderThan, "", "")
}

// RequeueStaleRunningForGroup moves stale running jobs for one group back to retry.
func (s *Store) RequeueStaleRunningForGroup(ctx context.Context, olderThan time.Duration, groupID string) (int64, error) {
	if groupID == "" {
		return s.RequeueStaleRunning(ctx, olderThan)
	}
	return s.requeueStaleRunning(ctx, olderThan, groupID, "")
}

// RequeueStaleRunningForKind moves stale running jobs for one JSON payload kind back to retry.
func (s *Store) RequeueStaleRunningForKind(ctx context.Context, olderThan time.Duration, payloadKind string) (int64, error) {
	return s.requeueStaleRunning(ctx, olderThan, "", strings.TrimSpace(payloadKind))
}

// RequeueStaleRunningForGroupKind moves stale running jobs for one group and JSON payload kind back to retry.
func (s *Store) RequeueStaleRunningForGroupKind(ctx context.Context, olderThan time.Duration, groupID, payloadKind string) (int64, error) {
	if groupID == "" {
		return s.RequeueStaleRunningForKind(ctx, olderThan, payloadKind)
	}
	return s.requeueStaleRunning(ctx, olderThan, groupID, strings.TrimSpace(payloadKind))
}

func (s *Store) requeueStaleRunning(ctx context.Context, olderThan time.Duration, groupID, payloadKind string) (int64, error) {
	if olderThan <= 0 {
		return 0, nil
	}
	now := time.Now().UTC()
	nowStr := sqlutil.FormatTimestamp(now)
	cutoff := sqlutil.FormatTimestamp(now.Add(-olderThan))
	clauses := []string{"state = 'running'", "updated_at < ?"}
	args := []any{nowStr, "recovered stale running job", cutoff}
	if groupID != "" {
		clauses = append(clauses, "group_id = ?")
		args = append(args, groupID)
	}
	if predicate, predicateArgs := payloadKindPredicate(payloadKind); predicate != "" {
		clauses = append(clauses, predicate)
		args = append(args, predicateArgs...)
	}
	result, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s
		SET state = 'retry', updated_at = ?, scheduled_at = NULL, error = ?
		WHERE %s
	`, s.table, strings.Join(clauses, " AND ")), args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// GetJob fetches a job by ID.
func (s *Store) GetJob(ctx context.Context, id string) (*Job, error) {
	row := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT id, group_id, payload, dedupe_key, state, priority, attempts, max_attempts, error,
		       created_at, updated_at, scheduled_at, completed_at
		FROM %s WHERE id = ?
	`, s.table), id)

	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return job, nil
}

// Stats returns queue state counts, optionally scoped to a group.
func (s *Store) Stats(ctx context.Context, groupID string) (*Stats, error) {
	return s.stats(ctx, groupID, "")
}

// StatsForKind returns queue state counts scoped to a group and JSON payload kind.
func (s *Store) StatsForKind(ctx context.Context, groupID, payloadKind string) (*Stats, error) {
	return s.stats(ctx, groupID, strings.TrimSpace(payloadKind))
}

func (s *Store) stats(ctx context.Context, groupID, payloadKind string) (*Stats, error) {
	stats := &Stats{}

	query := fmt.Sprintf("SELECT state, COUNT(*) FROM %s", s.table)
	args := []any{}
	clauses := []string{}
	if groupID != "" {
		clauses = append(clauses, "group_id = ?")
		args = append(args, groupID)
	}
	if payloadKind != "" {
		if predicate, predicateArgs := payloadKindPredicate(payloadKind); predicate != "" {
			clauses = append(clauses, predicate)
			args = append(args, predicateArgs...)
		}
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " GROUP BY state"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("count jobs: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close queue stats rows")
	}()

	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		switch JobState(state) {
		case StateQueued, StateRetry:
			stats.QueuedCount += count
		case StateRunning:
			stats.RunningCount = count
		case StateOK:
			stats.CompletedCount = count
		case StateError:
			stats.FailedCount = count
		}
	}

	oldestQuery := fmt.Sprintf("SELECT MIN(created_at) FROM %s WHERE state IN ('queued','retry')", s.table)
	oldestArgs := []any{}
	if groupID != "" {
		oldestQuery += " AND group_id = ?"
		oldestArgs = append(oldestArgs, groupID)
	}
	if payloadKind != "" {
		if predicate, predicateArgs := payloadKindPredicate(payloadKind); predicate != "" {
			oldestQuery += " AND " + predicate
			oldestArgs = append(oldestArgs, predicateArgs...)
		}
	}

	var oldestStr sql.NullString
	if err := s.db.QueryRowContext(ctx, oldestQuery, oldestArgs...).Scan(&oldestStr); err == nil && oldestStr.Valid {
		if t, parseErr := sqlutil.ScanTimestamp(oldestStr.String); parseErr == nil {
			stats.OldestQueuedAt = &t
		}
	}

	return stats, nil
}

// Cleanup deletes completed jobs older than the provided duration.
func (s *Store) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := sqlutil.FormatTimestamp(time.Now().UTC().Add(-olderThan))
	result, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM %s
		WHERE state IN ('ok', 'error') AND completed_at < ?
	`, s.table), cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func scanJob(row *sql.Row) (*Job, error) {
	var job Job
	var createdStr, updatedStr string
	var scheduledStr, completedStr, errStr sql.NullString

	err := row.Scan(
		&job.ID, &job.GroupID, &job.Payload, &job.DedupeKey, &job.State, &job.Priority,
		&job.Attempts, &job.MaxAttempts, &errStr, &createdStr, &updatedStr, &scheduledStr, &completedStr,
	)
	if err != nil {
		return nil, err
	}

	job.CreatedAt, _ = sqlutil.ScanTimestamp(createdStr)
	job.UpdatedAt, _ = sqlutil.ScanTimestamp(updatedStr)
	if scheduledStr.Valid {
		job.ScheduledAt, _ = sqlutil.ScanTimestamp(scheduledStr.String)
	}
	if completedStr.Valid {
		t, _ := sqlutil.ScanTimestamp(completedStr.String)
		job.CompletedAt = &t
	}
	if errStr.Valid {
		job.Error = errStr.String
	}

	return &job, nil
}

var tableNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func normalizeTableName(table string) (string, error) {
	if table == "" {
		return "", fmt.Errorf("table name is required")
	}
	if !tableNamePattern.MatchString(table) {
		return "", fmt.Errorf("invalid table name: %q", table)
	}
	return table, nil
}

func payloadKindPredicate(payloadKind string) (string, []any) {
	kind := strings.TrimSpace(payloadKind)
	if kind == "" {
		return "", nil
	}
	if kind == "symbol" {
		return "CASE WHEN json_valid(payload) THEN (json_extract(payload, '$.kind') = ? OR json_type(payload, '$.kind') IS NULL) ELSE 0 END", []any{kind}
	}
	return "CASE WHEN json_valid(payload) THEN json_extract(payload, '$.kind') = ? ELSE 0 END", []any{kind}
}
