package jobs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/envelope"
	errs "github.com/jkatigb/agentctl/internal/errors"
	"github.com/jkatigb/agentctl/internal/jobs/executor"
	"github.com/jkatigb/agentctl/internal/jobs/persist"
	"github.com/jkatigb/agentctl/internal/jobs/types"
	"github.com/jkatigb/agentctl/internal/logging"
	"github.com/jkatigb/agentctl/internal/skill"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/oklog/ulid/v2"
)

// Store composes persistence and execution primitives for job management.
type Store struct {
	root     string
	persist  persist.Store
	executor *executor.Executor
}

// Ensure Store implements storage.JobStore.
var _ storage.JobStore = (*Store)(nil)

// Open initializes the job store rooted at the provided path.
func Open(ctx context.Context, root string) (store *Store, err error) {
	logger := logging.FromContext(ctx)
	p, err := persist.Open(ctx, root)
	if err != nil {
		return nil, err
	}
	defer errs.CloseOnErr(&err, p)
	exec := executor.New(root, p, executor.WithLogger(logger))
	store = &Store{root: root, persist: p, executor: exec}
	return store, nil
}

// Close releases database resources.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	return s.persist.Close()
}

// SubmitEcho creates a job that echoes the provided message.
func (s *Store) SubmitEcho(ctx context.Context, message string) (Job, error) {
	args := map[string]string{"message": message}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return Job{}, fmt.Errorf("jobs: marshal args: %w", err)
	}

	now := time.Now().UTC()
	job := Job{
		ID:        newJobID(),
		Command:   "echo",
		ArgsJSON:  string(argsJSON),
		ArgsHash:  types.HashArgs("echo", argsJSON),
		State:     StateQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.persist.InsertJob(ctx, job); err != nil {
		return Job{}, err
	}

	jobDir := s.jobDir(job.ID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		_ = s.persist.Delete(ctx, job.ID)
		return Job{}, fmt.Errorf("jobs: job dir: %w", err)
	}

	if err := s.persist.UpdateState(ctx, job.ID, StateRunning, "", ""); err != nil {
		_ = s.persist.Delete(ctx, job.ID)
		return Job{}, err
	}

	env := envelope.OK("jobs.echo", map[string]string{"message": message})
	resultPath := filepath.Join(jobDir, "result.json")
	if err := writeResult(resultPath, env); err != nil {
		_ = s.persist.UpdateState(ctx, job.ID, StateError, err.Error(), "")
		return Job{}, err
	}

	if err := s.persist.UpdateState(ctx, job.ID, StateOK, "", resultPath); err != nil {
		return Job{}, err
	}

	return s.persist.Get(ctx, job.ID)
}

// List returns all jobs ordered by creation time descending.
func (s *Store) List(ctx context.Context, limit int) ([]Job, error) {
	return s.persist.List(ctx, limit)
}

// Get returns a single job by id.
func (s *Store) Get(ctx context.Context, id string) (Job, error) {
	return s.persist.Get(ctx, id)
}

// Result loads the result envelope for a job.
func (s *Store) Result(ctx context.Context, id string) ([]byte, error) {
	job, err := s.persist.Get(ctx, id)
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
	job, err := s.persist.Get(ctx, id)
	if err != nil {
		return err
	}
	switch job.State {
	case StateQueued, StateRunning:
		return s.persist.UpdateState(ctx, id, StateCanceled, "", "")
	default:
		return fmt.Errorf("%w: job already %s", ErrInvalidState, job.State)
	}
}

// Delete removes the job record from the database.
func (s *Store) Delete(ctx context.Context, id string) error {
	return s.persist.Delete(ctx, id)
}

// RunSkill executes a skill binary, recording its output as a job.
func (s *Store) RunSkill(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) (Job, []byte, error) {
	job, result, err := s.executor.RunSkill(ctx, manifest, artifactPath, input)
	if err != nil {
		return job, result, err
	}
	updated, getErr := s.persist.Get(ctx, job.ID)
	if getErr != nil {
		return job, result, getErr
	}
	return updated, result, nil
}

// PrepareSkillJob enqueues a job without executing the skill.
func (s *Store) PrepareSkillJob(ctx context.Context, name string, input []byte) (Job, error) {
	job, _, err := s.executor.FindOrPrepareSkillJob(ctx, name, input, false)
	return job, err
}

// ExecutePreparedSkill runs a previously prepared job.
func (s *Store) ExecutePreparedSkill(ctx context.Context, jobID string, manifestPath string, artifactPath string) ([]byte, error) {
	inputPath := filepath.Join(s.jobDir(jobID), "input.json")
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("jobs: read input: %w", err)
	}
	manifest, err := skill.LoadManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("jobs: load manifest: %w", err)
	}
	return s.executor.ExecutePrepared(ctx, jobID, manifest, artifactPath, data)
}

// SetWorkspace records the workspace path for a job so runners can enforce policy.
func (s *Store) SetWorkspace(_ context.Context, jobID, workspacePath string) error {
	if workspacePath == "" {
		return nil
	}
	jobDir := s.jobDir(jobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return fmt.Errorf("jobs: ensure job dir: %w", err)
	}
	path := filepath.Join(jobDir, "workspace")
	if existing, err := os.ReadFile(path); err == nil {
		if strings.TrimSpace(string(existing)) != "" {
			return nil
		}
	}
	if err := os.WriteFile(path, []byte(workspacePath), 0o644); err != nil {
		return fmt.Errorf("jobs: write workspace: %w", err)
	}
	return nil
}

// FindOrPrepareSkillJob atomically finds a duplicate job or creates a new one.
func (s *Store) FindOrPrepareSkillJob(ctx context.Context, name string, input []byte, dedupe bool) (Job, bool, error) {
	job, isDup, err := s.executor.FindOrPrepareSkillJob(ctx, name, input, dedupe)
	return job, isDup, err
}

// RecoverOrphanedJobs marks any running jobs as error (crash recovery).
func (s *Store) RecoverOrphanedJobs(ctx context.Context) (int64, error) {
	return s.persist.RecoverOrphanedJobs(ctx)
}

// ComputeSkillArgsHash deterministically computes the hash for skill inputs.
func (s *Store) ComputeSkillArgsHash(name string, input []byte) string {
	return types.ComputeSkillArgsHash(name, input)
}

// TailProgress streams progress events from a job, following new writes.
func (s *Store) TailProgress(ctx context.Context, jobID string, follow bool, w io.Writer) (retErr error) {
	if w == nil {
		return fmt.Errorf("progress: writer cannot be nil")
	}
	jobDir := s.jobDir(jobID)
	progressPath := filepath.Join(jobDir, "progress.ndjson")

	if _, err := s.persist.Get(ctx, jobID); err != nil {
		return err
	}

	var f *os.File
	var err error
	for {
		f, err = os.Open(progressPath)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("progress: open: %w", err)
		}
		if !follow {
			return fmt.Errorf("progress: no progress file yet")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			if retErr == nil {
				retErr = fmt.Errorf("progress: close: %w", closeErr)
			} else {
				retErr = fmt.Errorf("%v; close error: %w", retErr, closeErr)
			}
		}
	}()

	scanner := bufio.NewScanner(f)
	for {
		for scanner.Scan() {
			if _, err := fmt.Fprintln(w, scanner.Text()); err != nil {
				return fmt.Errorf("progress: write: %w", err)
			}
		}

		if err := scanner.Err(); err != nil {
			return fmt.Errorf("progress: scan: %w", err)
		}

		if !follow {
			return nil
		}

		job, err := s.persist.Get(ctx, jobID)
		if err != nil {
			return err
		}
		if job.State == StateOK || job.State == StateError || job.State == StateCanceled {
			scanner = bufio.NewScanner(f)
			for scanner.Scan() {
				if _, err := fmt.Fprintln(w, scanner.Text()); err != nil {
					return fmt.Errorf("progress: write: %w", err)
				}
			}
			return scanner.Err()
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}

		scanner = bufio.NewScanner(f)
	}
}

// WaitForCompletion blocks until the job reaches a terminal state.
func (s *Store) WaitForCompletion(ctx context.Context, jobID string, pollInterval time.Duration) (Job, error) {
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		job, err := s.persist.Get(ctx, jobID)
		if err != nil {
			return Job{}, err
		}

		switch job.State {
		case StateOK, StateError, StateCanceled:
			return job, nil
		}

		select {
		case <-ctx.Done():
			return Job{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Store) jobDir(id string) string {
	return filepath.Join(s.root, id)
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

func newJobID() string {
	return ulid.Make().String()
}
