package runservice

import (
	"fmt"

	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/jobs"
)

func (e *Executor) ensureJobStore() error {
	if e.jobStore != nil {
		return nil
	}
	store, err := jobs.Open(e.ctx, e.cfg.Paths.Jobs)
	if err != nil {
		return err
	}
	e.jobStore = store
	return nil
}

// PrepareJob finds or creates a job for the given input.
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
func (e *Executor) ExecuteSync(job jobs.Job) error {
	result, runErr := e.jobStore.ExecutePreparedSkill(e.ctx, job.ID, e.handle.ManifestPath, e.handle.ArtifactPath)
	latest, getErr := e.jobStore.Get(e.ctx, job.ID)
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
	if getErr == nil {
		if _, warnErr := fmt.Fprintf(e.stderr, "job %s state %s\n", latest.ID, latest.State); warnErr != nil {
			errs.Ignore(warnErr, "runservice: warn final state")
		}
	}
	return nil
}
