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
		job.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		job.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
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
	job.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	job.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
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
func (s *Store) ExecutePreparedSkill(ctx context.Context, jobID string, manifest skill.Manifest, artifactPath string) ([]byte, error) {
	inputPath := filepath.Join(s.jobDir(jobID), "input.json")
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("jobs: read input: %w", err)
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

func (s *Store) updateState(ctx context.Context, id string, state State, errMsg string, resultPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var setResult string
	if resultPath != "" {
		setResult = ", result_path = ?"
	}
	query := fmt.Sprintf(`UPDATE jobs SET state = ?, error = ?, updated_at = ?%s WHERE id = ?`, setResult)
	args := []any{state, errMsg, time.Now().UTC().Format(time.RFC3339Nano)}
	if resultPath != "" {
		args = append(args, resultPath)
	}
	args = append(args, id)
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("jobs: update state: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
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
func (s *Store) RecoverOrphanedJobs(ctx context.Context) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET state = ?, error = ?, updated_at = ?
		WHERE state = ?`,
		StateError, "ERUNTIME_RESTART: process restarted", time.Now().UTC().Format(time.RFC3339Nano), StateRunning)
	if err != nil {
		return fmt.Errorf("recover orphans: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows > 0 {
		// Could log this if we had logging
		_ = rows
	}
	return nil
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
	job.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	job.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return job, nil
}

func (s *Store) executeSkill(ctx context.Context, jobID string, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, error) {
	if err := s.updateState(ctx, jobID, StateRunning, "", ""); err != nil {
		return nil, err
	}

	var pw *ProgressWriter
	if writer, err := NewProgressWriter(s.jobDir(jobID)); err == nil {
		pw = writer
		defer func() { _ = pw.Close() }()
		_ = pw.WriteMessage("skill execution started")
	}

	stdout, stderr, err := runner.Run(ctx, manifest, artifactPath, input)
	stderrPath := filepath.Join(s.jobDir(jobID), "stderr.log")
	_ = os.WriteFile(stderrPath, append(stderr, '\n'), 0o644)
	if err != nil {
		if pw != nil {
			_ = pw.WriteMessage(fmt.Sprintf("skill failed: %s", err))
		}
		_ = s.updateState(ctx, jobID, StateError, err.Error(), "")
		return stdout, fmt.Errorf("skill run failed: %w", err)
	}

	resultPath := filepath.Join(s.jobDir(jobID), "result.json")
	if err := os.WriteFile(resultPath, stdout, 0o644); err != nil {
		if pw != nil {
			_ = pw.WriteMessage(fmt.Sprintf("skill failed to write result: %s", err))
		}
		_ = s.updateState(ctx, jobID, StateError, err.Error(), "")
		return nil, fmt.Errorf("jobs: write result: %w", err)
	}
	if err := s.updateState(ctx, jobID, StateOK, "", resultPath); err != nil {
		return nil, err
	}
	if pw != nil {
		_ = pw.Write(ProgressEvent{Percent: 100, Message: "skill completed"})
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
