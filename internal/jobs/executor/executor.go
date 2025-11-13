// Package executor orchestrates skill execution with job preparation, state management, and result persistence.
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/jkatigb/agentctl/internal/jobs/types"
	"github.com/jkatigb/agentctl/internal/runner"
	"github.com/jkatigb/agentctl/internal/skill"
	"github.com/oklog/ulid/v2"
)

// RunnerFunc executes a skill manifest and returns stdout, stderr, and an error.
type RunnerFunc func(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, []byte, error)

// Persistence captures the persistence primitives required by the executor.
type Persistence interface {
	InsertJob(ctx context.Context, job types.Job) error
	FindOrInsertJob(ctx context.Context, job types.Job) (types.Job, bool, error)
	UpdateState(ctx context.Context, id string, newState types.State, errMsg, resultPath string) error
	Get(ctx context.Context, id string) (types.Job, error)
	Delete(ctx context.Context, id string) error
}

// Executor coordinates skill execution with persistent job state.
type Executor struct {
	root    string
	persist Persistence
	run     RunnerFunc
}

// Option configures an Executor instance.
type Option func(*Executor)

// WithRunner overrides the runner invocation used by the executor.
func WithRunner(run RunnerFunc) Option {
	return func(e *Executor) {
		e.run = run
	}
}

// New creates a new Executor instance.
func New(root string, persist Persistence, opts ...Option) *Executor {
	exec := &Executor{
		root:    root,
		persist: persist,
		run:     runner.Run,
	}
	for _, opt := range opts {
		opt(exec)
	}
	return exec
}

// RunSkill prepares and executes a skill job end-to-end.
func (e *Executor) RunSkill(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) (types.Job, []byte, error) {
	job, duplicate, err := e.prepareSkillJob(ctx, manifest.Metadata.Name, input, false)
	if err != nil {
		return types.Job{}, nil, err
	}
	if duplicate {
		// Should not happen when dedupe disabled, but guard regardless.
		return job, nil, fmt.Errorf("jobs: unexpected duplicate during run")
	}
	result, err := e.executeSkill(ctx, job.ID, manifest, artifactPath, input)
	return job, result, err
}

// ExecutePrepared runs a previously prepared job.
func (e *Executor) ExecutePrepared(ctx context.Context, jobID string, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, error) {
	return e.executeSkill(ctx, jobID, manifest, artifactPath, input)
}

// PrepareSkillJob creates a queued skill job.
func (e *Executor) PrepareSkillJob(ctx context.Context, name string, input []byte) (types.Job, error) {
	job, _, err := e.prepareSkillJob(ctx, name, input, false)
	return job, err
}

// FindOrPrepareSkillJob deduplicates skill jobs when requested.
func (e *Executor) FindOrPrepareSkillJob(ctx context.Context, name string, input []byte, dedupe bool) (types.Job, bool, error) {
	return e.prepareSkillJob(ctx, name, input, dedupe)
}

func (e *Executor) prepareSkillJob(ctx context.Context, name string, input []byte, dedupe bool) (types.Job, bool, error) {
	argsBuf := types.MarshalSkillArgs(name, input)
	now := time.Now().UTC()
	job := types.Job{
		ID:        ulid.Make().String(),
		Command:   "skill:" + name,
		ArgsJSON:  string(argsBuf),
		ArgsHash:  types.HashArgs(name, argsBuf),
		State:     types.StateQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}

	var duplicate bool
	var err error
	if dedupe {
		job, duplicate, err = e.persist.FindOrInsertJob(ctx, job)
		if err != nil {
			return types.Job{}, false, err
		}
		if duplicate {
			return job, true, nil
		}
	} else {
		if err := e.persist.InsertJob(ctx, job); err != nil {
			return types.Job{}, false, err
		}
	}

	jobDir := e.jobDir(job.ID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		_ = e.persist.Delete(ctx, job.ID)
		return types.Job{}, false, fmt.Errorf("jobs: job dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(jobDir, "input.json"), input, 0o644); err != nil {
		_ = os.RemoveAll(jobDir)
		_ = e.persist.Delete(ctx, job.ID)
		return types.Job{}, false, fmt.Errorf("jobs: write input: %w", err)
	}
	return job, false, nil
}

func (e *Executor) executeSkill(ctx context.Context, jobID string, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, error) {
	if err := e.persist.UpdateState(ctx, jobID, types.StateRunning, "", ""); err != nil {
		return nil, err
	}

	var pw *progressWriter
	if writer, err := newProgressWriter(e.jobDir(jobID)); err == nil {
		pw = writer
		defer func() { _ = pw.Close() }()
		_ = pw.WriteMessage("skill execution started")
	}

	stdout, stderr, err := e.run(ctx, manifest, artifactPath, input)
	stderrPath := filepath.Join(e.jobDir(jobID), "stderr.log")
	if writeErr := os.WriteFile(stderrPath, append(stderr, '\n'), 0o644); writeErr != nil {
		// best-effort logging
		_ = writeErr
	}

	if err != nil {
		if pw != nil {
			_ = pw.WriteMessage(fmt.Sprintf("skill failed: %s", err))
		}
		if stateErr := e.persist.UpdateState(ctx, jobID, types.StateError, err.Error(), ""); stateErr != nil {
			return stdout, fmt.Errorf("skill run failed: %w (state update also failed: %v)", err, stateErr)
		}
		return stdout, fmt.Errorf("skill run failed: %w", err)
	}

	var resultEnv envelope.Envelope
	if unmarshalErr := json.Unmarshal(stdout, &resultEnv); unmarshalErr != nil {
		validationErr := fmt.Errorf("invalid result envelope: %w", unmarshalErr)
		if pw != nil {
			_ = pw.WriteMessage(fmt.Sprintf("skill failed: %s", validationErr))
		}
		if stateErr := e.persist.UpdateState(ctx, jobID, types.StateError, validationErr.Error(), ""); stateErr != nil {
			return nil, fmt.Errorf("%w (state update also failed: %v)", validationErr, stateErr)
		}
		return nil, validationErr
	}

	if validationErr := envelope.Validate(resultEnv); validationErr != nil {
		wrapped := fmt.Errorf("envelope validation failed: %w", validationErr)
		if pw != nil {
			_ = pw.WriteMessage(fmt.Sprintf("skill failed: %s", wrapped))
		}
		if stateErr := e.persist.UpdateState(ctx, jobID, types.StateError, wrapped.Error(), ""); stateErr != nil {
			return nil, fmt.Errorf("%w (state update also failed: %v)", wrapped, stateErr)
		}
		return nil, wrapped
	}

	resultPath := filepath.Join(e.jobDir(jobID), "result.json")
	if err := os.WriteFile(resultPath, stdout, 0o644); err != nil {
		if pw != nil {
			_ = pw.WriteMessage(fmt.Sprintf("skill failed to write result: %s", err))
		}
		if stateErr := e.persist.UpdateState(ctx, jobID, types.StateError, err.Error(), ""); stateErr != nil {
			return nil, fmt.Errorf("jobs: write result: %w (state update also failed: %v)", err, stateErr)
		}
		return nil, fmt.Errorf("jobs: write result: %w", err)
	}

	if err := e.persist.UpdateState(ctx, jobID, types.StateOK, "", resultPath); err != nil {
		return nil, err
	}

	if pw != nil {
		_ = pw.Write(ProgressEvent{Percent: 100, Message: "skill completed"})
	}

	return stdout, nil
}

func (e *Executor) jobDir(id string) string {
	return filepath.Join(e.root, id)
}
