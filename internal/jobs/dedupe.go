package jobs

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// FindOrPrepareSkillJob atomically finds a duplicate job or creates a new one.
// This prevents the TOCTOU race in deduplication, even across multiple processes.
// Uses a database transaction to ensure atomicity.
func (s *Store) FindOrPrepareSkillJob(ctx context.Context, name string, input []byte, dedupe bool) (Job, bool, error) {
	if !dedupe {
		job, err := s.prepareSkillJob(ctx, name, input)
		return job, false, err
	}

	argsBuf := marshalSkillArgs(name, input)
	argsHash := hashArgs(name, argsBuf)

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return Job{}, false, fmt.Errorf("jobs: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

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
		return job, true, nil
	}

	if !errorsIsNoRows(scanErr) {
		return Job{}, false, fmt.Errorf("jobs: find duplicate: %w", scanErr)
	}

	jobID := ulid.Make().String()
	jobDir := s.jobDir(jobID)
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

	_, err = tx.ExecContext(ctx, `
                INSERT INTO jobs (id, command, args_json, args_hash, state, result_path, error, created_at, updated_at)
                VALUES (?, ?, ?, ?, ?, '', '', ?, ?)`,
		job.ID, job.Command, job.ArgsJSON, job.ArgsHash, job.State,
		job.CreatedAt.Format(time.RFC3339Nano), job.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return Job{}, false, fmt.Errorf("jobs: insert: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Job{}, false, fmt.Errorf("jobs: commit transaction: %w", err)
	}

	return job, false, nil
}

// ComputeSkillArgsHash deterministically computes the hash for skill inputs.
func (s *Store) ComputeSkillArgsHash(name string, input []byte) string {
	argsBuf := marshalSkillArgs(name, input)
	return hashArgs(name, argsBuf)
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

func marshalSkillArgs(name string, input []byte) []byte {
	args := map[string]any{"skill": name}
	if len(input) > 0 {
		args["input_size_bytes"] = len(input)

		var structured any
		if err := json.Unmarshal(input, &structured); err == nil {
			args["input"] = structured
		} else {
			args["input_base64"] = base64.StdEncoding.EncodeToString(input)
		}
	}
	buf, err := json.Marshal(args)
	if err != nil {
		return []byte("{}")
	}
	return buf
}

func hashArgs(command string, argsJSON []byte) string {
	h := sha256Pool.Get().(hash.Hash)
	defer func() {
		h.Reset()
		sha256Pool.Put(h)
	}()

	h.Write([]byte(command))
	h.Write(argsJSON)
	return hex.EncodeToString(h.Sum(nil))
}

var sha256Pool = sync.Pool{New: func() any { return sha256.New() }}
