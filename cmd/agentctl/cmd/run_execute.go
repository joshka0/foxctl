package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/jkatigb/agentctl/internal/jobs"
)

// defaultAsyncRunner spawns a background worker process to execute a skill job.
func defaultAsyncRunner(ctx context.Context, jobID, manifestPath, artifactPath string, stderr io.Writer) error {
	worker := exec.CommandContext(ctx, os.Args[0], "jobs", "exec-skill",
		"--job-id", jobID,
		"--manifest", manifestPath,
		"--artifact", artifactPath,
	)
	worker.Stdout = stderr
	worker.Stderr = stderr
	return worker.Start()
}

// submitAsync schedules the job for asynchronous execution.
func (e *runExecutor) submitAsync(job jobs.Job) error {
	if err := e.asyncRunner(e.ctx, job.ID, e.handle.ManifestPath, e.handle.ArtifactPath, e.stderr); err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "job %s submitted\n", job.ID)
	return nil
}

// executeSync runs the skill synchronously and handles persistence side effects.
func (e *runExecutor) executeSync(job jobs.Job) error {
	// Execute the skill
	result, runErr := e.jobStore.ExecutePreparedSkill(e.ctx, job.ID, e.handle.ManifestPath, e.handle.ArtifactPath)

	// Fetch the latest job state
	latest, getErr := e.jobStore.Get(e.ctx, job.ID)
	if getErr != nil {
		fmt.Fprintf(e.stderr, "warning: failed to fetch job %s after execution: %v\n", job.ID, getErr)
		if runErr == nil {
			return fmt.Errorf("execution completed but failed to fetch final job state for %s: %w", job.ID, getErr)
		}
	}

	// Return execution error if any
	if runErr != nil {
		return runErr
	}

	// Handle artifacts
	if err := handleArtifacts(e.ctx, e.cfg, job.ID, result); err != nil {
		return err
	}

	// Annotate result with metadata
	annotated := annotateRunMeta(result, e.options.Workspace, e.handle.Manifest.Metadata.Version)

	// Save to cache
	if err := e.persistCache(annotated); err != nil {
		fmt.Fprintf(e.stderr, "cache put failed: %v\n", err)
	}

	// Save to memory if requested
	if err := e.remember(annotated); err != nil {
		fmt.Fprintf(e.stderr, "remember failed: %v\n", err)
	}

	// Write result to output
	if err := writeEnvelope(e.stdout, annotated); err != nil {
		return err
	}

	// Log final job state
	if getErr == nil {
		fmt.Fprintf(e.stderr, "job %s state %s\n", latest.ID, latest.State)
	}

	return nil
}

// remember saves the execution result to named memory if requested.
func (e *runExecutor) remember(result []byte) error {
	if strings.TrimSpace(e.options.RememberName) == "" {
		return nil
	}
	return rememberResult(e.ctx, e.cfg, RememberOptions{
		Name:      e.options.RememberName,
		Type:      e.options.RememberType,
		Summary:   e.options.RememberSummary,
		Workspace: e.options.Workspace,
		Result:    result,
	})
}
