package runservice

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/observability"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/logging"
	"github.com/jkatigb/agentctl/internal/storage/jobs"
	"github.com/jkatigb/agentctl/internal/storage/jobs/executor"
	"github.com/jkatigb/agentctl/internal/storage/jobs/persist"
	"github.com/jkatigb/agentctl/internal/storage/jobs/types"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
	"github.com/jkatigb/agentctl/internal/trajectorycapture"
)

func (e *Executor) ensureJobStore() (err error) {
	if e.jobStore != nil {
		return nil
	}
	logger := logging.FromContext(e.ctx)
	p, err := persist.Open(e.ctx, e.cfg.Paths.Jobs)
	if err != nil {
		return err
	}
	defer errs.CloseOnErr(p, &err)
	exec := executor.New(e.cfg.Paths.Jobs, p,
		executor.WithLogger(logger),
		executor.WithCASPath(e.cfg.Paths.CAS),
	)
	store := jobs.New(e.cfg.Paths.Jobs, p, exec)
	recoveryStart := time.Now()
	if recovered, recErr := store.RecoverStaleJobs(e.ctx, types.DefaultMaxJobAge); recErr != nil {
		event := observability.NewEvent("job.recover").
			WithComponent(observability.ComponentJob).
			WithData("max_age_seconds", int64(types.DefaultMaxJobAge.Seconds()))
		if e.options.Workspace != "" {
			event.WithWorkspace(e.options.Workspace)
		}
		observability.Emit(e.ctx, event.Error(recErr, time.Since(recoveryStart)))
	} else if recovered > 0 {
		event := observability.NewEvent("job.recover").
			WithComponent(observability.ComponentJob).
			WithData("recovered", recovered).
			WithData("max_age_seconds", int64(types.DefaultMaxJobAge.Seconds()))
		if e.options.Workspace != "" {
			event.WithWorkspace(e.options.Workspace)
		}
		observability.Emit(e.ctx, event.Success(time.Since(recoveryStart)))
	}
	// NOTE: We avoid full orphan recovery here to prevent races when multiple
	// agentctl processes run concurrently (e.g., parallel hooks). RecoverStaleJobs
	// handles stale-job recovery using DefaultMaxJobAge, which is safe for
	// concurrent runs and avoids erroring active jobs.
	e.jobStore = store
	return nil
}

// PrepareJob finds or creates a job for the given input.
//
// Index:
// - Purpose: Find or create a job and initialize trajectory capture context
// - Flow: ensure job store -> prepare job -> persist workspace -> set correlation ID -> start capture -> capture hook call
// - SideEffects: writes job store; starts trajectory capture; may update workspace metadata
// - FailureModes: job store errors, workspace persistence errors (warned), capture start errors (ignored)
// - Related: jobs.Store.FindOrPrepareSkillJob, trajectorycapture.Start, Executor.ExecuteSync
// - Keywords: prepare_job, dedupe, workspace, correlation_id, trajectorycapture, job_store, hooks
func (e *Executor) PrepareJob(input []byte) (jobs.Job, bool, error) {
	if err := e.ensureJobStore(); err != nil {
		return jobs.Job{}, false, err
	}
	job, dup, err := e.jobStore.FindOrPrepareSkillJob(e.ctx, e.handle.Manifest.Metadata.Name, input, e.options.Dedupe)
	if err != nil {
		return job, dup, err
	}
	if e.options.Workspace != "" {
		if err := e.jobStore.SetWorkspace(e.ctx, job.ID, e.options.Workspace); err != nil {
			if _, warnErr := fmt.Fprintf(e.stderr, "warn: unable to persist workspace for job %s: %v\n", job.ID, err); warnErr != nil {
				errs.Ignore(warnErr, "runservice: warn workspace persist failure")
			}
		}
	}
	// Ensure correlation ID is set for every job, not just when initializing trajectory capture.
	corr := e.options.CorrelationID
	if corr == "" {
		corr = job.ID
		e.options.CorrelationID = corr
	}
	if e.trajCapture == nil {
		if e.cfg.Storage.Root != "" && e.options.Workspace != "" {
			capture, capErr := trajectorycapture.Start(e.ctx, trajectorycapture.StartOptions{
				StorageRoot:     e.cfg.Storage.Root,
				WorkspaceID:     e.options.Workspace,
				Actor:           "actor:human:cli",
				Source:          trajectory.SourceCLI,
				CLICommand:      e.options.CLICommand,
				ProtocolCommand: e.handle.Manifest.Metadata.Name,
				JobID:           job.ID,
				CorrelationID:   corr,
				Input:           input,
				SessionID:       e.options.SessionID,
			})
			if capErr == nil {
				e.trajCapture = capture
			} else {
				errs.Ignore(capErr, "trajectory capture start")
			}
		}
	}
	if e.trajCapture != nil && strings.HasPrefix(e.handle.Manifest.Metadata.Name, "hooks/") {
		capErr := e.trajCapture.CaptureHookCall(e.ctx, e.handle.Manifest.Metadata.Name, input, job.ID, e.options.CorrelationID)
		errs.Ignore(capErr, "trajectory capture hook call")
	}
	return job, dup, nil
}

// HandleDuplicate processes a duplicate job based on execution mode.
func (e *Executor) HandleDuplicate(job jobs.Job) error {
	if _, warnErr := fmt.Fprintf(e.stderr, "using existing job %s (deduplicated)\n", job.ID); warnErr != nil {
		errs.Ignore(warnErr, "runservice: warn duplicate job")
	}
	if e.options.Async {
		if _, warnErr := fmt.Fprintf(e.stdout, "job %s (existing)\n", job.ID); warnErr != nil {
			return fmt.Errorf("runservice: write existing job id: %w", warnErr)
		}
		return nil
	}
	if job.ResultPath != "" {
		result, err := e.jobStore.Result(e.ctx, job.ID)
		if err != nil {
			return err
		}
		return e.HandleResult(job.ID, result)
	}
	waited, err := e.jobStore.WaitForCompletion(e.ctx, job.ID, 0)
	if err != nil {
		return err
	}
	job = waited
	result, err := e.jobStore.Result(e.ctx, job.ID)
	if err != nil {
		return err
	}
	return e.HandleResult(job.ID, result)
}

// SubmitAsync schedules the job for asynchronous execution.
func (e *Executor) SubmitAsync(job jobs.Job) error {
	if e.asyncRunner == nil {
		return fmt.Errorf("runservice: async runner not configured")
	}
	if err := e.asyncRunner(e.ctx, job.ID, e.handle.ManifestPath, e.handle.ArtifactPath, e.stderr); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(e.stdout, "job %s submitted\n", job.ID); err != nil {
		return fmt.Errorf("runservice: write async status: %w", err)
	}
	return nil
}

// ExecuteSync runs the skill synchronously and applies persistence side effects.
//
// Index:
// - Purpose: Execute a job synchronously and persist results
// - Flow: ensure env → execute prepared skill → fetch job → handle result
// - SideEffects: executes skill; updates job store; may write envelopes
// - FailureModes: execution errors, job store errors, result handling errors
// - Related: Executor.HandleResult, jobs.Store.ExecutePreparedSkill
// - Keywords: execute_sync, jobs_store, result_path, correlation_id
func (e *Executor) ExecuteSync(job jobs.Job) error {
	// Set no-CAS mode in environment if requested (disables output truncation)
	//
	// KNOWN LIMITATION: This uses os.Setenv which affects the entire process.
	// In rare cases where multiple concurrent agentctl processes run with
	// different NoCAS settings, there could be a race condition. This is
	// acceptable because:
	//   1. CLI is typically used sequentially (one command at a time)
	//   2. Hooks run serially within a Claude Code session
	//   3. The race only affects concurrent invocations with different NoCAS settings
	//
	// A complete fix would require passing ExtraEnv through the jobs.Store and
	// executor.Executor chain, which is invasive. The ephemeral execution path
	// (used by hooks) does not have this limitation since it passes ExtraEnv
	// directly to the runner.
	//
	// TODO: Refactor executor chain to support ExtraEnv for full race-freedom.
	if e.options.NoCAS {
		os.Setenv("AGENTCTL_NO_CAS", "1")
		defer os.Unsetenv("AGENTCTL_NO_CAS")
	}

	result, runErr := e.jobStore.ExecutePreparedSkill(e.ctx, job.ID, e.handle.ManifestPath, e.handle.ArtifactPath)
	_, getErr := e.jobStore.Get(e.ctx, job.ID)
	if getErr != nil {
		if _, warnErr := fmt.Fprintf(e.stderr, "warning: failed to fetch job %s after execution: %v\n", job.ID, getErr); warnErr != nil {
			errs.Ignore(warnErr, "runservice: warn fetch job failure")
		}
		if runErr == nil {
			return fmt.Errorf("execution completed but failed to fetch final job state for %s: %w", job.ID, getErr)
		}
	}
	if runErr != nil {
		return runErr
	}
	if err := e.HandleResult(job.ID, result); err != nil {
		return err
	}
	// Job info is in envelope metadata (meta.job_id, status)
	return nil
}
