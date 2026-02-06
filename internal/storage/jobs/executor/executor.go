package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/execution"
	"github.com/jkatigb/agentctl/internal/execution/runner"
	"github.com/jkatigb/agentctl/internal/observability"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/logging"
	"github.com/jkatigb/agentctl/internal/platform/metrics"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/jkatigb/agentctl/internal/storage/jobs/types"
	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"
)

// RunnerFunc executes a skill manifest and returns stdout, stderr, and an error.
type RunnerFunc func(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, []byte, error)

// executeOptions captures the parameters used when running a skill.
type executeOptions struct {
	JobID        string
	Manifest     skill.Manifest
	ArtifactPath string
	Input        []byte
}

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
	casPath         string
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

// WithCASPath configures the CAS root path for stderr artifact capture.
func WithCASPath(path string) Option {
	return func(e *Executor) {
		e.casPath = strings.TrimSpace(path)
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
	result, err := e.executeSkill(ctx, executeOptions{
		JobID:        job.ID,
		Manifest:     manifest,
		ArtifactPath: artifactPath,
		Input:        input,
	})
	return job, result, err
}

// ExecutePrepared runs a previously prepared job.
func (e *Executor) ExecutePrepared(ctx context.Context, jobID string, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, error) {
	return e.executeSkill(ctx, executeOptions{
		JobID:        jobID,
		Manifest:     manifest,
		ArtifactPath: artifactPath,
		Input:        input,
	})
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
		ExpiresAt: now.Add(types.DefaultMaxJobAge),
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
		errs.Ignore(e.persist.Delete(ctx, job.ID), "delete job after mkdir failure")
		return types.Job{}, false, fmt.Errorf("jobs: job dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(jobDir, "input.json"), input, 0o644); err != nil {
		errs.Ignore(os.RemoveAll(jobDir), "cleanup job dir after write failure")
		errs.Ignore(e.persist.Delete(ctx, job.ID), "delete job after write failure")
		return types.Job{}, false, fmt.Errorf("jobs: write input: %w", err)
	}
	return job, false, nil
}

func (e *Executor) executeSkill(ctx context.Context, opts executeOptions) ([]byte, error) {
	ctx, pw, cleanup, start, err := e.startExecution(ctx, opts.JobID)
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}

	// Ensure trace ID for observability
	ctx, traceID := observability.EnsureTraceID(ctx)

	// Build wide event for skill execution in background to avoid blocking on
	// cloud metadata lookups (AWS/GCP/Azure) which can timeout in non-cloud environments.
	type enrichResult struct {
		builder *observability.EventBuilder
	}
	baseBuilder := observability.NewEvent(observability.OpSkillRun).
		WithTraceID(traceID).
		WithComponent(observability.ComponentSkill).
		WithCommand(opts.Manifest.Metadata.Name).
		WithJobID(opts.JobID).
		WithData("skill_version", opts.Manifest.Metadata.Version)
	enrichCh := make(chan enrichResult, 1)
	go func() {
		builder := observability.NewEvent(observability.OpSkillRun).
			WithTraceID(traceID).
			WithComponent(observability.ComponentSkill).
			WithCommand(opts.Manifest.Metadata.Name).
			WithJobID(opts.JobID).
			WithData("skill_version", opts.Manifest.Metadata.Version).
			EnrichFromEnv()
		// Add workspace if available
		if ws := e.jobWorkspace(opts.JobID); ws != "" {
			builder = builder.WithWorkspace(ws)
		}
		enrichCh <- enrichResult{builder: builder}
	}()

	stdout, stderr, runErr := e.runSkill(ctx, opts.Manifest, opts.ArtifactPath, opts.Input)
	duration := time.Since(start)
	metrics.Global().RecordExecutionTime(duration)
	e.writeStderrLog(opts.JobID, stderr)
	stderrDigest := e.persistStderrArtifact(opts.JobID, stderr)

	// Wait for enrichment to complete (runs in parallel with skill execution)
	eventBuilder := baseBuilder
	select {
	case enriched := <-enrichCh:
		if enriched.builder != nil {
			eventBuilder = enriched.builder
		}
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
	}
	if stderrDigest != "" {
		eventBuilder = eventBuilder.WithStderrArtifact(stderrDigest)
	}

	if runErr != nil {
		// Emit error event
		observability.Emit(ctx, eventBuilder.Error(runErr, duration))
		return e.handleFailure(ctx, opts.JobID, stdout, stderr, runErr, pw)
	}

	// Check if the envelope indicates an error status
	result, handleErr := e.handleSuccess(ctx, opts.JobID, stdout, pw)
	if handleErr != nil {
		// Emit error event for envelope validation/write failures
		observability.Emit(ctx, eventBuilder.Error(handleErr, duration))
		return result, handleErr
	}

	// Check envelope status for protocol-level errors
	var env envelope.Envelope
	if json.Unmarshal(stdout, &env) == nil && env.Status == "error" {
		// Skill returned an error envelope - still emit as error for observability
		eventBuilder = eventBuilder.WithData("envelope_error", true)
		if env.Error.Code != "" {
			eventBuilder = eventBuilder.
				WithData("error_code", env.Error.Code).
				WithData("error_message", env.Error.Message)
		}
		observability.Emit(ctx, eventBuilder.ErrorWithDetails(
			"skill_error",
			env.Error.Code,
			env.Error.Message,
			false,
			duration,
		))
	} else {
		// Success
		observability.Emit(ctx, eventBuilder.Success(duration))
	}

	return result, handleErr
}

func (e *Executor) startExecution(ctx context.Context, jobID string) (context.Context, *progressWriter, func(), time.Time, error) {
	if err := e.persist.UpdateState(ctx, jobID, types.StateRunning, "", ""); err != nil {
		return nil, nil, nil, time.Time{}, err
	}
	if ws := e.jobWorkspace(jobID); ws != "" {
		ctx = workspace.WithContext(ctx, ws)
	}
	metrics.Global().RecordSkillExecution()
	start := time.Now()

	writer, err := e.progressFactory(e.jobDir(jobID))
	if err != nil {
		e.logger.Warn().
			Str("job_id", jobID).
			Err(err).
			Msg("progress writer init failed")
		return ctx, nil, nil, start, nil
	}

	cleanup := func() {
		if closeErr := writer.Close(); closeErr != nil {
			e.logger.Warn().
				Str("job_id", jobID).
				Err(closeErr).
				Msg("progress close failed")
		}
	}
	if err := writer.WriteMessage("skill execution started"); err != nil {
		e.warnProgress(jobID, "execution_start", err)
	}
	return ctx, writer, cleanup, start, nil
}

func (e *Executor) runSkill(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, []byte, error) {
	if e.skillExecutor != nil {
		result, execErr := e.skillExecutor.Execute(ctx, execution.ExecuteOptions{
			Manifest:     manifest,
			ArtifactPath: artifactPath,
			Input:        input,
		})
		if result == nil {
			if execErr == nil {
				execErr = fmt.Errorf("skill executor returned nil result without error")
			}
			return nil, nil, execErr
		}
		stdout := result.Stdout
		stderr := result.Stderr
		if execErr != nil {
			return stdout, stderr, execErr
		}
		return stdout, stderr, result.Error
	}
	return runner.RunWithOptions(ctx, runner.RunOptions{
		Manifest:     manifest,
		ArtifactPath: artifactPath,
		Input:        input,
	})
}

func (e *Executor) handleFailure(_ context.Context, jobID string, stdout, stderr []byte, runErr error, pw *progressWriter) ([]byte, error) {
	err := augmentError(runErr, stdout, stderr)
	if pw != nil {
		if pwErr := pw.WriteMessage(fmt.Sprintf("skill failed: %s", err)); pwErr != nil {
			e.warnProgress(jobID, "execution_error", pwErr)
		}
	}
	// Use background context for final state update to ensure it completes
	// even if the CLI's context was cancelled (e.g., timeout).
	stateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if stateErr := e.persist.UpdateState(stateCtx, jobID, types.StateError, err.Error(), ""); stateErr != nil {
		return stdout, fmt.Errorf("skill run failed: %w (state update also failed: %v)", err, stateErr)
	}
	return stdout, fmt.Errorf("skill run failed: %w", err)
}

func (e *Executor) handleSuccess(_ context.Context, jobID string, stdout []byte, pw *progressWriter) ([]byte, error) {
	// Use background context for final state updates to ensure they complete
	// even if the CLI's context was cancelled (e.g., timeout).
	stateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var resultEnv envelope.Envelope
	if unmarshalErr := json.Unmarshal(stdout, &resultEnv); unmarshalErr != nil {
		validationErr := fmt.Errorf("invalid result envelope: %w", unmarshalErr)
		e.recordProgressFailure(jobID, pw, "decode_error", validationErr)
		if stateErr := e.persist.UpdateState(stateCtx, jobID, types.StateError, validationErr.Error(), ""); stateErr != nil {
			return nil, fmt.Errorf("%w (state update also failed: %v)", validationErr, stateErr)
		}
		return nil, validationErr
	}

	if validationErr := envelope.Validate(resultEnv); validationErr != nil {
		wrapped := fmt.Errorf("envelope validation failed: %w", validationErr)
		e.recordProgressFailure(jobID, pw, "validation_error", wrapped)
		if stateErr := e.persist.UpdateState(stateCtx, jobID, types.StateError, wrapped.Error(), ""); stateErr != nil {
			return nil, fmt.Errorf("%w (state update also failed: %v)", wrapped, stateErr)
		}
		return nil, wrapped
	}

	resultPath := filepath.Join(e.jobDir(jobID), "result.json")
	if err := os.WriteFile(resultPath, stdout, 0o644); err != nil {
		e.recordProgressFailure(jobID, pw, "result_error", fmt.Errorf("skill failed to write result: %w", err))
		if stateErr := e.persist.UpdateState(stateCtx, jobID, types.StateError, err.Error(), ""); stateErr != nil {
			return nil, fmt.Errorf("jobs: write result: %w (state update also failed: %v)", err, stateErr)
		}
		return nil, fmt.Errorf("jobs: write result: %w", err)
	}

	if err := e.persist.UpdateState(stateCtx, jobID, types.StateOK, "", resultPath); err != nil {
		return nil, err
	}

	if pw != nil {
		if err := pw.Write(ProgressEvent{Percent: 100, Message: "skill completed"}); err != nil {
			e.warnProgress(jobID, "execution_complete", err)
		}
	}

	return stdout, nil
}

func (e *Executor) recordProgressFailure(jobID string, pw *progressWriter, phase string, err error) {
	if pw == nil {
		return
	}
	if pwErr := pw.WriteMessage(fmt.Sprintf("skill failed: %s", err)); pwErr != nil {
		e.warnProgress(jobID, phase, pwErr)
	}
}

func augmentError(runErr error, stdout, stderr []byte) error {
	if detail := strings.TrimSpace(string(stderr)); detail != "" {
		runErr = fmt.Errorf("%w: %s", runErr, detail)
	}
	if len(stdout) > 0 {
		snippet := strings.TrimSpace(string(stdout))
		const maxSnippet = 512
		if len(snippet) > maxSnippet {
			snippet = snippet[:maxSnippet] + "..."
		}
		if snippet != "" {
			runErr = fmt.Errorf("%w; stdout: %s", runErr, snippet)
		}
	}
	return runErr
}

func (e *Executor) writeStderrLog(jobID string, stderr []byte) {
	stderrPath := filepath.Join(e.jobDir(jobID), "stderr.log")
	if writeErr := os.WriteFile(stderrPath, append(stderr, '\n'), 0o644); writeErr != nil {
		e.logger.Warn().
			Str("job_id", jobID).
			Str("path", stderrPath).
			Err(writeErr).
			Msg("stderr log write failed")
	}
}

func (e *Executor) persistStderrArtifact(jobID string, stderr []byte) string {
	if len(stderr) == 0 {
		return ""
	}
	if strings.TrimSpace(e.casPath) == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	store, err := cas.NewStore(e.casPath)
	if err != nil {
		e.logger.Warn().Err(err).Str("job_id", jobID).Msg("stderr cas open failed")
		return ""
	}
	defer func() { errs.Ignore(store.Close(), "close cas store") }()

	obj, err := store.Put(ctx, bytes.NewReader(stderr), "text/plain", []string{"stderr", "job:" + jobID})
	if err != nil {
		e.logger.Warn().Err(err).Str("job_id", jobID).Msg("stderr cas put failed")
		return ""
	}
	return obj.Digest
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
