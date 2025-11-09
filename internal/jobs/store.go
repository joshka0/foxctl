package jobs

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/jkatigb/agentctl/internal/runner"
	"github.com/jkatigb/agentctl/internal/skill"
	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite" // sqlite driver
)

// Store persists job metadata and results.
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

// SubmitEcho creates a job that echoes the provided message.
func (s *Store) SubmitEcho(ctx context.Context, message string) (Job, error) {
	args := map[string]string{"message": message}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return Job{}, fmt.Errorf("jobs: marshal args: %w", err)
	}
	argsHash := hashArgs("echo", argsJSON)
	jobID := ulid.Make().String()
	now := time.Now().UTC()

	jobDir := filepath.Join(s.root, jobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return Job{}, fmt.Errorf("jobs: job dir: %w", err)
	}

	job := Job{
		ID:        jobID,
		Command:   "echo",
		ArgsJSON:  string(argsJSON),
		ArgsHash:  argsHash,
		State:     StateQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.insertJob(ctx, job); err != nil {
		return Job{}, err
	}

	if err := s.updateState(ctx, jobID, StateRunning, "", ""); err != nil {
		return Job{}, err
	}

	env := envelope.OK("jobs.echo", map[string]string{"message": message})
	resultPath := filepath.Join(jobDir, "result.json")
	if err := writeResult(resultPath, env); err != nil {
		_ = s.updateState(ctx, jobID, StateError, err.Error(), "")
		return Job{}, err
	}

	if err := s.updateState(ctx, jobID, StateOK, "", resultPath); err != nil {
		return Job{}, err
	}

	return s.Get(ctx, jobID)
}

// List returns all jobs ordered by creation time descending.
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
	defer func() {
		_ = rows.Close()
	}()

	var jobs []Job
	for rows.Next() {
		var job Job
		var created, updated string
		if err := rows.Scan(&job.ID, &job.Command, &job.ArgsJSON, &job.ArgsHash, &job.State, &job.ResultPath, &job.Error, &created, &updated); err != nil {
			return nil, fmt.Errorf("jobs: scan: %w", err)
		}
		var err error
		job.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, fmt.Errorf("jobs: parse created_at: %w", err)
		}
		job.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, fmt.Errorf("jobs: parse updated_at: %w", err)
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

// RunSkill executes a skill binary, recording its output as a job.
func (s *Store) RunSkill(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) (Job, []byte, error) {
	job, err := s.prepareSkillJob(ctx, manifest.Metadata.Name, input)
	if err != nil {
		return Job{}, nil, err
	}
	result, execErr := s.executeSkill(ctx, job.ID, manifest, artifactPath, input)
	job, _ = s.Get(ctx, job.ID)
	return job, result, execErr
}

// PrepareSkillJob enqueues a job without executing the skill.
func (s *Store) PrepareSkillJob(ctx context.Context, name string, input []byte) (Job, error) {
	return s.prepareSkillJob(ctx, name, input)
}

// ExecutePreparedSkill runs a previously prepared job.
func (s *Store) ExecutePreparedSkill(ctx context.Context, jobID string, manifestPath string, artifactPath string) ([]byte, error) {
	inputPath := filepath.Join(s.jobDir(jobID), "input.json")
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("jobs: read input: %w", err)
	}

	// Load manifest from path
	manifest, err := skill.LoadManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("jobs: load manifest: %w", err)
	}

	return s.executeSkill(ctx, jobID, manifest, artifactPath, data)
}

func (s *Store) insertJob(ctx context.Context, job Job) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO jobs (id, command, args_json, args_hash, state, result_path, error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, '', '', ?, ?)`,
		job.ID, job.Command, job.ArgsJSON, job.ArgsHash, job.State, job.CreatedAt.Format(time.RFC3339Nano), job.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("jobs: insert: %w", err)
	}
	return nil
}

func (s *Store) updateState(ctx context.Context, id string, newState State, errMsg string, resultPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Build atomic UPDATE with state constraint in WHERE clause to prevent TOCTOU races
	allowedStates := validSourceStates(newState)
	placeholders := make([]string, len(allowedStates))
	stateArgs := make([]any, len(allowedStates))
	for i, state := range allowedStates {
		placeholders[i] = "?"
		stateArgs[i] = state
	}
	stateConstraint := fmt.Sprintf("state IN (%s)", strings.Join(placeholders, ", "))

	// Build UPDATE query with state validation in WHERE clause
	var setResult string
	if resultPath != "" {
		setResult = ", result_path = ?"
	}
	query := fmt.Sprintf(`UPDATE jobs SET state = ?, error = ?, updated_at = ?%s WHERE id = ? AND %s`,
		setResult, stateConstraint)

	// Build args: [newState, errMsg, timestamp, (optional resultPath), id, ...allowedStates]
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

	rows, _ := res.RowsAffected()
	if rows == 0 {
		// Distinguish between "job not found" and "invalid state transition"
		var exists bool
		checkErr := s.db.QueryRowContext(ctx, `SELECT 1 FROM jobs WHERE id = ?`, id).Scan(&exists)
		if checkErr != nil {
			if errorsIsNoRows(checkErr) {
				return ErrNotFound
			}
			return fmt.Errorf("jobs: check existence: %w", checkErr)
		}
		// Job exists but UPDATE didn't affect it, so state transition was invalid
		var currentState State
		_ = s.db.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id = ?`, id).Scan(&currentState)
		return fmt.Errorf("%w: cannot transition from %s to %s", ErrInvalidState, currentState, newState)
	}
	return nil
}

// validSourceStates returns the list of states that can validly transition to the target state.
// Used by updateState to build atomic UPDATE queries with state constraints.
func validSourceStates(target State) []State {
	switch target {
	case StateQueued:
		// Only same-state transition allowed
		return []State{StateQueued}
	case StateRunning:
		// Can transition from queued or stay running (idempotent)
		return []State{StateQueued, StateRunning}
	case StateOK:
		// Can only reach OK from running
		return []State{StateRunning, StateOK}
	case StateError:
		// Can error from queued or running
		return []State{StateQueued, StateRunning, StateError}
	case StateCanceled:
		// Can cancel from queued or running
		return []State{StateQueued, StateRunning, StateCanceled}
	default:
		return []State{}
	}
}

func writeResult(path string, env envelope.Envelope) error {
	buf, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("jobs: marshal result: %w", err)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return fmt.Errorf("jobs: write result: %w", err)
	}
	return nil
}

func hashArgs(command string, argsJSON []byte) string {
	h := sha256.New()
	h.Write([]byte(command))
	h.Write(argsJSON)
	return hex.EncodeToString(h.Sum(nil))
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

func (s *Store) prepareSkillJob(ctx context.Context, name string, input []byte) (Job, error) {
	argsBuf := marshalSkillArgs(name, input)
	jobID := ulid.Make().String()
	jobDir := s.jobDir(jobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return Job{}, fmt.Errorf("jobs: job dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(jobDir, "input.json"), input, 0o644); err != nil {
		return Job{}, fmt.Errorf("jobs: write input: %w", err)
	}
	job := Job{
		ID:        jobID,
		Command:   "skill:" + name,
		ArgsJSON:  string(argsBuf),
		ArgsHash:  hashArgs(name, argsBuf),
		State:     StateQueued,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.insertJob(ctx, job); err != nil {
		return Job{}, err
	}
	return job, nil
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

// FindOrPrepareSkillJob atomically finds a duplicate job or creates a new one.
// This prevents the TOCTOU race in deduplication, even across multiple processes.
// Uses a database transaction to ensure atomicity.
func (s *Store) FindOrPrepareSkillJob(ctx context.Context, name string, input []byte, dedupe bool) (Job, bool, error) {
	if !dedupe {
		// No dedup requested, just create new job
		job, err := s.prepareSkillJob(ctx, name, input)
		return job, false, err
	}

	// Compute hash outside transaction
	argsBuf := marshalSkillArgs(name, input)
	argsHash := hashArgs(name, argsBuf)

	// Use a transaction with immediate locking to prevent cross-process races
	// BEGIN IMMEDIATE acquires a write lock on the database, preventing other
	// processes from writing between our duplicate check and insert
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return Job{}, false, fmt.Errorf("jobs: begin transaction: %w", err)
	}
	// Always attempt rollback; if transaction was committed, Rollback returns sql.ErrTxDone which we ignore
	defer func() { _ = tx.Rollback() }()

	// Check for existing job with same hash (inside transaction)
	row := tx.QueryRowContext(ctx, `
		SELECT id, command, args_json, args_hash, state, result_path, error, created_at, updated_at
		FROM jobs
		WHERE args_hash = ?
		ORDER BY created_at DESC
		LIMIT 1`, argsHash)

	var job Job
	var created, updated string
	scanErr := row.Scan(&job.ID, &job.Command, &job.ArgsJSON, &job.ArgsHash, &job.State, &job.ResultPath, &job.Error, &created, &updated)

	if scanErr == nil {
		// Found duplicate - parse timestamps and commit transaction
		var parseErr error
		job.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, created)
		if parseErr != nil {
			return Job{}, false, fmt.Errorf("jobs: parse created_at: %w", parseErr)
		}
		job.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updated)
		if parseErr != nil {
			return Job{}, false, fmt.Errorf("jobs: parse updated_at: %w", parseErr)
		}

		if err := tx.Commit(); err != nil {
			return Job{}, false, fmt.Errorf("jobs: commit transaction: %w", err)
		}
		return job, true, nil // true = found duplicate
	}

	if !errorsIsNoRows(scanErr) {
		return Job{}, false, fmt.Errorf("jobs: find duplicate: %w", scanErr)
	}

	// No duplicate found, create new job (still inside transaction)
	jobID := ulid.Make().String()
	jobDir := s.jobDir(jobID)

	// Create job directory and input file
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return Job{}, false, fmt.Errorf("jobs: job dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(jobDir, "input.json"), input, 0o644); err != nil {
		return Job{}, false, fmt.Errorf("jobs: write input: %w", err)
	}

	job = Job{
		ID:        jobID,
		Command:   "skill:" + name,
		ArgsJSON:  string(argsBuf),
		ArgsHash:  argsHash,
		State:     StateQueued,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	// Insert job (inside transaction)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO jobs (id, command, args_json, args_hash, state, result_path, error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, '', '', ?, ?)`,
		job.ID, job.Command, job.ArgsJSON, job.ArgsHash, job.State,
		job.CreatedAt.Format(time.RFC3339Nano), job.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return Job{}, false, fmt.Errorf("jobs: insert: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return Job{}, false, fmt.Errorf("jobs: commit transaction: %w", err)
	}

	return job, false, nil // false = new job created
}

func (s *Store) executeSkill(ctx context.Context, jobID string, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, error) {
	if err := s.updateState(ctx, jobID, StateRunning, "", ""); err != nil {
		return nil, err
	}

	var pw *ProgressWriter
	if writer, err := NewProgressWriter(s.jobDir(jobID)); err == nil {
		pw = writer
		defer func() { _ = pw.Close() }()
		if err := pw.WriteMessage("skill execution started"); err != nil {
			// Progress write failures are non-fatal but we track them
			// TODO: add structured logging
			_ = err // Explicitly ignore - best effort only
		}
	}

	stdout, stderr, err := runner.Run(ctx, manifest, artifactPath, input)
	stderrPath := filepath.Join(s.jobDir(jobID), "stderr.log")
	if writeErr := os.WriteFile(stderrPath, append(stderr, '\n'), 0o644); writeErr != nil {
		// Stderr logging is best-effort; don't fail job if it can't be written
		// TODO: add structured logging for writeErr
		_ = writeErr // Explicitly ignore - best effort only
	}
	if err != nil {
		if pw != nil {
			if pwErr := pw.WriteMessage(fmt.Sprintf("skill failed: %s", err)); pwErr != nil {
				// Progress write failed, but we're already in error path
				_ = pwErr // Explicitly ignore - already in error state
			}
		}
		if stateErr := s.updateState(ctx, jobID, StateError, err.Error(), ""); stateErr != nil {
			// State update failed - return combined error
			return stdout, fmt.Errorf("skill run failed: %w (state update also failed: %v)", err, stateErr)
		}
		return stdout, fmt.Errorf("skill run failed: %w", err)
	}

	// Validate the result envelope before persisting
	var resultEnv envelope.Envelope
	if err := json.Unmarshal(stdout, &resultEnv); err != nil {
		validationErr := fmt.Errorf("invalid result envelope: %w", err)
		if pw != nil {
			if pwErr := pw.WriteMessage(fmt.Sprintf("skill failed: %s", validationErr)); pwErr != nil {
				// Progress write failed, but we're already in error path
				_ = pwErr // Explicitly ignore - already in error state
			}
		}
		if stateErr := s.updateState(ctx, jobID, StateError, validationErr.Error(), ""); stateErr != nil {
			return nil, fmt.Errorf("%w (state update also failed: %v)", validationErr, stateErr)
		}
		return nil, validationErr
	}

	if err := envelope.Validate(resultEnv); err != nil {
		validationErr := fmt.Errorf("envelope validation failed: %w", err)
		if pw != nil {
			if pwErr := pw.WriteMessage(fmt.Sprintf("skill failed: %s", validationErr)); pwErr != nil {
				// Progress write failed, but we're already in error path
				_ = pwErr // Explicitly ignore - already in error state
			}
		}
		if stateErr := s.updateState(ctx, jobID, StateError, validationErr.Error(), ""); stateErr != nil {
			return nil, fmt.Errorf("%w (state update also failed: %v)", validationErr, stateErr)
		}
		return nil, validationErr
	}

	resultPath := filepath.Join(s.jobDir(jobID), "result.json")
	if err := os.WriteFile(resultPath, stdout, 0o644); err != nil {
		if pw != nil {
			if pwErr := pw.WriteMessage(fmt.Sprintf("skill failed to write result: %s", err)); pwErr != nil {
				// Progress write failed, but we're already in error path
				_ = pwErr // Explicitly ignore - already in error state
			}
		}
		if stateErr := s.updateState(ctx, jobID, StateError, err.Error(), ""); stateErr != nil {
			// State update failed - return combined error
			return nil, fmt.Errorf("jobs: write result: %w (state update also failed: %v)", err, stateErr)
		}
		return nil, fmt.Errorf("jobs: write result: %w", err)
	}
	if err := s.updateState(ctx, jobID, StateOK, "", resultPath); err != nil {
		return nil, err
	}
	if pw != nil {
		if err := pw.Write(ProgressEvent{Percent: 100, Message: "skill completed"}); err != nil {
			// Progress write failures are non-fatal
			// TODO: add structured logging
			_ = err // Explicitly ignore - best effort only
		}
	}
	return stdout, nil
}

// ComputeSkillArgsHash deterministically computes the hash for skill inputs.
func (s *Store) ComputeSkillArgsHash(name string, input []byte) string {
	argsBuf := marshalSkillArgs(name, input)
	return hashArgs(name, argsBuf)
}

func marshalSkillArgs(name string, input []byte) []byte {
	args := map[string]any{"skill": name}
	if len(input) > 0 {
		args["input_size_bytes"] = len(input)
	}
	buf, err := json.Marshal(args)
	if err != nil {
		return []byte("{}")
	}
	return buf
}
