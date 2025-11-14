// Package executor orchestrates skill execution with job preparation, state management, and result persistence.
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/jkatigb/agentctl/internal/execution"
	"github.com/jkatigb/agentctl/internal/jobs/types"
	"github.com/jkatigb/agentctl/internal/logging"
	"github.com/jkatigb/agentctl/internal/metrics"
	"github.com/jkatigb/agentctl/internal/runner"
	"github.com/jkatigb/agentctl/internal/skill"
	"github.com/jkatigb/agentctl/internal/workspace"
	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"
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
	root            string
	persist         Persistence
	skillExecutor   execution.SkillExecutor
	logger          zerolog.Logger
	progressFactory func(jobDir string) (*progressWriter, error)
}

// Option configures an Executor instance.
type Option func(*Executor)

// WithRunner overrides the runner invocation used by the executor.
// Deprecated: Use WithSkillExecutor for better testability.
func WithRunner(run RunnerFunc) Option {
	return func(e *Executor) {
		// Adapt the RunnerFunc to SkillExecutor interface
		e.skillExecutor = newRunnerFuncAdapter(run)
	}
}

// WithSkillExecutor sets a custom skill executor.
func WithSkillExecutor(executor execution.SkillExecutor) Option {
	return func(e *Executor) {
		e.skillExecutor = executor
	}
}

// WithLogger injects a logger used for best-effort warnings.
func WithLogger(logger zerolog.Logger) Option {
	return func(e *Executor) {
		e.logger = logger
	}
}

// withProgressWriterFactory swaps the progress writer constructor (test hook).
func withProgressWriterFactory(factory func(string) (*progressWriter, error)) Option {
	return func(e *Executor) {
		e.progressFactory = factory
	}
}

// New creates a new Executor instance.
func New(root string, persist Persistence, opts ...Option) *Executor {
	exec := &Executor{
		root:            root,
		persist:         persist,
		skillExecutor:   execution.NewRunnerExecutor(),
		logger:          logging.Default(),
		progressFactory: newProgressWriter,
	}
	for _, opt := range opts {
		opt(exec)
	}
	if exec.progressFactory == nil {
		exec.progressFactory = newProgressWriter
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
	if ws := e.jobWorkspace(jobID); ws != "" {
		ctx = workspace.WithContext(ctx, ws)
	}
	metrics.Global().RecordSkillExecution()
	start := time.Now()

	var pw *progressWriter
	if writer, err := e.progressFactory(e.jobDir(jobID)); err == nil {
		pw = writer
		defer func() {
			if closeErr := pw.Close(); closeErr != nil {
				e.logger.Warn().
					Str("job_id", jobID).
					Err(closeErr).
					Msg("progress close failed")
			}
		}()
		if err := pw.WriteMessage("skill execution started"); err != nil {
			e.warnProgress(jobID, "execution_start", err)
		}
	} else if err != nil {
		e.logger.Warn().
			Str("job_id", jobID).
			Err(err).
			Msg("progress writer init failed")
	}

	var stdout, stderr []byte
	var err error
	if e.skillExecutor != nil {
		result, execErr := e.skillExecutor.Execute(ctx, execution.ExecuteOptions{
			Manifest:     manifest,
			ArtifactPath: artifactPath,
			Input:        input,
		})
		if result != nil {
			stdout = result.Stdout
			stderr = result.Stderr
			if execErr != nil {
				err = execErr
			} else {
				err = result.Error
			}
		} else {
			err = execErr
		}
		if result == nil && execErr == nil {
			err = fmt.Errorf("skill executor returned nil result without error")
		}
	} else {
		stdout, stderr, err = runner.RunWithOptions(ctx, runner.RunOptions{
			Manifest:     manifest,
			ArtifactPath: artifactPath,
			Input:        input,
		})
	}
	metrics.Global().RecordExecutionTime(time.Since(start))
	stderrPath := filepath.Join(e.jobDir(jobID), "stderr.log")
	if writeErr := os.WriteFile(stderrPath, append(stderr, '\n'), 0o644); writeErr != nil {
		e.logger.Warn().
			Str("job_id", jobID).
			Str("path", stderrPath).
			Err(writeErr).
			Msg("stderr log write failed")
	}

	if err != nil {
		if detail := strings.TrimSpace(string(stderr)); detail != "" {
			err = fmt.Errorf("%w: %s", err, detail)
		}
		if len(stdout) > 0 {
			snippet := strings.TrimSpace(string(stdout))
			const maxSnippet = 512
			if len(snippet) > maxSnippet {
				snippet = snippet[:maxSnippet] + "..."
			}
			if snippet != "" {
				err = fmt.Errorf("%w; stdout: %s", err, snippet)
			}
		}
		if pw != nil {
			if pwErr := pw.WriteMessage(fmt.Sprintf("skill failed: %s", err)); pwErr != nil {
				e.warnProgress(jobID, "execution_error", pwErr)
			}
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
			if pwErr := pw.WriteMessage(fmt.Sprintf("skill failed: %s", validationErr)); pwErr != nil {
				e.warnProgress(jobID, "decode_error", pwErr)
			}
		}
		if stateErr := e.persist.UpdateState(ctx, jobID, types.StateError, validationErr.Error(), ""); stateErr != nil {
			return nil, fmt.Errorf("%w (state update also failed: %v)", validationErr, stateErr)
		}
		return nil, validationErr
	}

	if validationErr := envelope.Validate(resultEnv); validationErr != nil {
		wrapped := fmt.Errorf("envelope validation failed: %w", validationErr)
		if pw != nil {
			if pwErr := pw.WriteMessage(fmt.Sprintf("skill failed: %s", wrapped)); pwErr != nil {
				e.warnProgress(jobID, "validation_error", pwErr)
			}
		}
		if stateErr := e.persist.UpdateState(ctx, jobID, types.StateError, wrapped.Error(), ""); stateErr != nil {
			return nil, fmt.Errorf("%w (state update also failed: %v)", wrapped, stateErr)
		}
		return nil, wrapped
	}

	resultPath := filepath.Join(e.jobDir(jobID), "result.json")
	if err := os.WriteFile(resultPath, stdout, 0o644); err != nil {
		if pw != nil {
			if pwErr := pw.WriteMessage(fmt.Sprintf("skill failed to write result: %s", err)); pwErr != nil {
				e.warnProgress(jobID, "result_error", pwErr)
			}
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
		if err := pw.Write(ProgressEvent{Percent: 100, Message: "skill completed"}); err != nil {
			e.warnProgress(jobID, "execution_complete", err)
		}
	}

	return stdout, nil
}

func (e *Executor) warnProgress(jobID, phase string, err error) {
	if err == nil {
		return
	}
	e.logger.Warn().
		Str("job_id", jobID).
		Str("phase", phase).
		Err(err).
		Msg("progress write failed")
}

func (e *Executor) jobDir(id string) string {
	return filepath.Join(e.root, id)
}

func (e *Executor) jobWorkspace(id string) string {
	data, err := os.ReadFile(filepath.Join(e.jobDir(id), "workspace"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// newRunnerFuncAdapter adapts a RunnerFunc to the SkillExecutor interface.
func newRunnerFuncAdapter(run RunnerFunc) execution.SkillExecutor {
	return execution.ExecutorFunc(func(ctx context.Context, opts execution.ExecuteOptions) (*execution.Result, error) {
		manifest := opts.Manifest
		if manifest.Metadata.Name == "" && opts.ManifestPath != "" {
			var err error
			manifest, err = skill.LoadManifest(opts.ManifestPath)
			if err != nil {
				return nil, fmt.Errorf("load manifest: %w", err)
			}
		}

		// Call the runner function
		stdout, stderr, runErr := run(ctx, manifest, opts.ArtifactPath, opts.Input)

		// Determine exit code
		exitCode := 0
		if runErr != nil {
			exitCode = 1
		}

		return &execution.Result{
			Stdout:   stdout,
			Stderr:   stderr,
			ExitCode: exitCode,
			Error:    runErr,
		}, nil
	})
}
