package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/jkatigb/agentctl/internal/runner"
	"github.com/jkatigb/agentctl/internal/skill"
)

type executorStore interface {
	jobDir(string) string
	prepareSkillJob(context.Context, string, []byte) (Job, error)
	updateState(context.Context, string, State, string, string) error
	Get(context.Context, string) (Job, error)
}

type skillRunnerFunc func(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) (stdout, stderr []byte, err error)

// Executor coordinates skill execution and progress reporting while persisting state via the Store.
type Executor struct {
	store executorStore
	run   skillRunnerFunc
}

// NewExecutor returns an Executor backed by the given store.
func NewExecutor(store executorStore) *Executor {
	return &Executor{store: store, run: runner.Run}
}

// RunSkill executes a skill binary, recording its output as a job.
func (e *Executor) RunSkill(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) (Job, []byte, error) {
	job, err := e.store.prepareSkillJob(ctx, manifest.Metadata.Name, input)
	if err != nil {
		return Job{}, nil, err
	}

	result, execErr := e.execute(ctx, job.ID, manifest, artifactPath, input)
	job, _ = e.store.Get(ctx, job.ID)
	return job, result, execErr
}

// PrepareSkillJob enqueues a job without executing the skill.
func (e *Executor) PrepareSkillJob(ctx context.Context, name string, input []byte) (Job, error) {
	return e.store.prepareSkillJob(ctx, name, input)
}

// ExecutePreparedSkill runs a previously prepared job.
func (e *Executor) ExecutePreparedSkill(ctx context.Context, jobID string, manifestPath string, artifactPath string) ([]byte, error) {
	inputPath := filepath.Join(e.store.jobDir(jobID), "input.json")
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("jobs: read input: %w", err)
	}

	manifest, err := skill.LoadManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("jobs: load manifest: %w", err)
	}

	return e.execute(ctx, jobID, manifest, artifactPath, data)
}

func (e *Executor) execute(ctx context.Context, jobID string, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, error) {
	if err := e.store.updateState(ctx, jobID, StateRunning, "", ""); err != nil {
		return nil, err
	}

	var pw *ProgressWriter
	if writer, err := NewProgressWriter(e.store.jobDir(jobID)); err == nil {
		pw = writer
		defer func() { _ = pw.Close() }()
		_ = pw.WriteMessage("skill execution started")
	}

	stdout, stderr, err := e.run(ctx, manifest, artifactPath, input)
	stderrPath := filepath.Join(e.store.jobDir(jobID), "stderr.log")
	if writeErr := os.WriteFile(stderrPath, append(stderr, '\n'), 0o644); writeErr != nil {
		_ = writeErr
	}
	if err != nil {
		if pw != nil {
			_ = pw.WriteMessage(fmt.Sprintf("skill failed: %s", err))
		}
		if stateErr := e.store.updateState(ctx, jobID, StateError, err.Error(), ""); stateErr != nil {
			return stdout, fmt.Errorf("skill run failed: %w (state update also failed: %v)", err, stateErr)
		}
		return stdout, fmt.Errorf("skill run failed: %w", err)
	}

	var resultEnv envelope.Envelope
	if err := json.Unmarshal(stdout, &resultEnv); err != nil {
		validationErr := fmt.Errorf("invalid result envelope: %w", err)
		if pw != nil {
			_ = pw.WriteMessage(fmt.Sprintf("skill failed: %s", validationErr))
		}
		if stateErr := e.store.updateState(ctx, jobID, StateError, validationErr.Error(), ""); stateErr != nil {
			return nil, fmt.Errorf("%w (state update also failed: %v)", validationErr, stateErr)
		}
		return nil, validationErr
	}

	if err := envelope.Validate(resultEnv); err != nil {
		validationErr := fmt.Errorf("envelope validation failed: %w", err)
		if pw != nil {
			_ = pw.WriteMessage(fmt.Sprintf("skill failed: %s", validationErr))
		}
		if stateErr := e.store.updateState(ctx, jobID, StateError, validationErr.Error(), ""); stateErr != nil {
			return nil, fmt.Errorf("%w (state update also failed: %v)", validationErr, stateErr)
		}
		return nil, validationErr
	}

	resultPath := filepath.Join(e.store.jobDir(jobID), "result.json")
	if err := os.WriteFile(resultPath, stdout, 0o644); err != nil {
		if pw != nil {
			_ = pw.WriteMessage(fmt.Sprintf("skill failed to write result: %s", err))
		}
		if stateErr := e.store.updateState(ctx, jobID, StateError, err.Error(), ""); stateErr != nil {
			return nil, fmt.Errorf("jobs: write result: %w (state update also failed: %v)", err, stateErr)
		}
		return nil, fmt.Errorf("jobs: write result: %w", err)
	}
	if err := e.store.updateState(ctx, jobID, StateOK, "", resultPath); err != nil {
		return nil, err
	}
	if pw != nil {
		_ = pw.Write(ProgressEvent{Percent: 100, Message: "skill completed"})
	}
	return stdout, nil
}
