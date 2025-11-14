package jobs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/logging"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/jobs/executor"
	"github.com/jkatigb/agentctl/internal/storage/jobs/fsutil"
	"github.com/jkatigb/agentctl/internal/storage/jobs/persist"
	"github.com/jkatigb/agentctl/internal/storage/jobs/types"
	"github.com/oklog/ulid/v2"
)

// Store composes persistence and execution primitives for job management.
type Store struct {
	root     string
	persist  Persistence
	executor SkillExecutor
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
	defer errs.CloseOnErr(p, &err)
	exec := executor.New(root, p, executor.WithLogger(logger))
	return New(root, p, exec), nil
}

// New constructs a Store instance from the provided dependencies.
func New(root string, p Persistence, exec SkillExecutor) *Store {
	return &Store{root: root, persist: p, executor: exec}
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

	jobDir, err := fsutil.EnsureJobDir(s.root, job.ID)
	if err != nil {
		errs.Ignore(s.persist.Delete(ctx, job.ID), "delete echo job after mkdir failure")
		return Job{}, err
	}

	if err := s.persist.UpdateState(ctx, job.ID, StateRunning, "", ""); err != nil {
		errs.Ignore(s.persist.Delete(ctx, job.ID), "delete echo job after state failure")
		return Job{}, err
	}

	env := envelope.OK("jobs.echo", map[string]string{"message": message})
	resultPath := filepath.Join(jobDir, "result.json")
	if err := writeResult(resultPath, env); err != nil {
		errs.Ignore(s.persist.UpdateState(ctx, job.ID, StateError, err.Error(), ""), "mark echo job error after result failure")
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
	inputPath := filepath.Join(fsutil.JobDir(s.root, jobID), "input.json")
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
	jobDir := fsutil.JobDir(s.root, jobID)
	return fsutil.RecordWorkspace(jobDir, workspacePath)
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
	if _, err := s.persist.Get(ctx, jobID); err != nil {
		return err
	}

	follower := newProgressFollower(ctx, s.persist, jobID, filepath.Join(fsutil.JobDir(s.root, jobID), "progress.ndjson"), follow)
	return follower.stream(w)
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

type progressFollower struct {
	ctx     context.Context
	persist Persistence
	jobID   string
	path    string
	follow  bool
	file    *os.File
}

func newProgressFollower(ctx context.Context, p Persistence, jobID, path string, follow bool) *progressFollower {
	return &progressFollower{ctx: ctx, persist: p, jobID: jobID, path: path, follow: follow}
}

func (pf *progressFollower) stream(w io.Writer) (retErr error) {
	file, err := pf.openFile()
	if err != nil {
		return err
	}
	pf.file = file
	defer func() {
		if closeErr := pf.file.Close(); closeErr != nil {
			if retErr == nil {
				retErr = fmt.Errorf("progress: close: %w", closeErr)
			} else {
				retErr = fmt.Errorf("%v; close error: %w", retErr, closeErr)
			}
		}
	}()

	scanner := bufio.NewScanner(pf.file)
	for {
		for scanner.Scan() {
			if _, err := fmt.Fprintln(w, scanner.Text()); err != nil {
				return fmt.Errorf("progress: write: %w", err)
			}
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("progress: scan: %w", err)
		}
		if !pf.follow {
			return nil
		}
		done, err := pf.jobComplete()
		if err != nil {
			return err
		}
		if done {
			scanner = bufio.NewScanner(pf.file)
			for scanner.Scan() {
				if _, err := fmt.Fprintln(w, scanner.Text()); err != nil {
					return fmt.Errorf("progress: write: %w", err)
				}
			}
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("progress: scan: %w", err)
			}
			return nil
		}
		if err := pf.waitForChange(); err != nil {
			return err
		}
		scanner = bufio.NewScanner(pf.file)
	}
}

func (pf *progressFollower) openFile() (*os.File, error) {
	for {
		f, err := os.Open(pf.path)
		if err == nil {
			return f, nil
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("progress: open: %w", err)
		}
		if !pf.follow {
			return nil, fmt.Errorf("progress: no progress file yet")
		}
		if err := pf.waitForChange(); err != nil {
			return nil, err
		}
	}
}

func (pf *progressFollower) waitForChange() error {
	select {
	case <-pf.ctx.Done():
		return pf.ctx.Err()
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

func (pf *progressFollower) jobComplete() (bool, error) {
	job, err := pf.persist.Get(pf.ctx, pf.jobID)
	if err != nil {
		return false, err
	}
	switch job.State {
	case StateOK, StateError, StateCanceled:
		return true, nil
	default:
		return false, nil
	}
}
