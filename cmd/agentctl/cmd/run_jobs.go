package cmd

import (
	"fmt"

	errs "github.com/jkatigb/agentctl/internal/errors"
	"github.com/jkatigb/agentctl/internal/jobs"
)

// ensureJobStore initializes the job store if it hasn't been initialized yet.
func (e *runExecutor) ensureJobStore() error {
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

// prepareJob creates or finds an existing job for the given input.
// It returns the job, a boolean indicating if it's a duplicate, and any error.
func (e *runExecutor) prepareJob(input []byte) (jobs.Job, bool, error) {
	if err := e.ensureJobStore(); err != nil {
		return jobs.Job{}, false, err
	}

	// Find or create the job
	job, dup, err := e.jobStore.FindOrPrepareSkillJob(
		e.ctx,
		e.handle.Manifest.Metadata.Name,
		input,
		e.options.Dedupe,
	)
	if err != nil {
		return job, dup, err
	}

	// Set workspace if provided
	if e.options.Workspace != "" {
		if err := e.jobStore.SetWorkspace(e.ctx, job.ID, e.options.Workspace); err != nil {
			if _, warnErr := fmt.Fprintf(e.stderr, "warn: unable to persist workspace for job %s: %v\n", job.ID, err); warnErr != nil {
				errs.Ignore(warnErr, "run: warn workspace persist failure")
			}
		}
	}

	return job, dup, nil
}

// handleDuplicate processes a duplicate job based on the execution mode.
// For async mode, it simply returns the job ID.
// For sync mode, it waits for completion if needed and returns the result.
func (e *runExecutor) handleDuplicate(job jobs.Job) error {
	if _, warnErr := fmt.Fprintf(e.stderr, "using existing job %s (deduplicated)\n", job.ID); warnErr != nil {
		errs.Ignore(warnErr, "run: warn duplicate job")
	}

	// For async mode, just return the job ID
	if e.options.Async {
		if _, warnErr := fmt.Fprintf(e.stdout, "job %s (existing)\n", job.ID); warnErr != nil {
			return fmt.Errorf("run: write existing job id: %w", warnErr)
		}
		return nil
	}

	// If result already exists, return it
	if job.ResultPath != "" {
		result, err := e.jobStore.Result(e.ctx, job.ID)
		if err != nil {
			return err
		}
		return e.handleResult(job.ID, result)
	}

	// Wait for job completion
	waited, err := e.jobStore.WaitForCompletion(e.ctx, job.ID, 0)
	if err != nil {
		return err
	}
	job = waited

	// Get the result
	result, err := e.jobStore.Result(e.ctx, job.ID)
	if err != nil {
		return err
	}

	return e.handleResult(job.ID, result)
}

// handleResult processes a job result by handling artifacts, caching, and memory.
func (e *runExecutor) handleResult(jobID string, result []byte) error {
	// Handle artifacts
	if err := handleArtifacts(e.ctx, e.cfg, jobID, result); err != nil {
		return err
	}

	// Annotate result with metadata
	annotated := annotateRunMeta(result, e.options.Workspace, e.handle.Manifest.Metadata.Version)

	// Save to cache
	if err := e.persistCache(annotated); err != nil {
		if _, warnErr := fmt.Fprintf(e.stderr, "cache put failed: %v\n", err); warnErr != nil {
			errs.Ignore(warnErr, "run: warn cache persist failure")
		}
	}

	// Save to memory if requested
	if err := e.remember(annotated); err != nil {
		if _, warnErr := fmt.Fprintf(e.stderr, "remember failed: %v\n", err); warnErr != nil {
			errs.Ignore(warnErr, "run: warn remember failure")
		}
	}

	// Write result
	return writeEnvelope(e.stdout, annotated)
}
