package cmd

import (
	"context"
	"io"
	"os"
	"os/exec"
)

// defaultAsyncRunner spawns a background worker process to execute a skill job.
// The worker runs independently and the parent doesn't wait for completion.
func defaultAsyncRunner(ctx context.Context, jobID, manifestPath, artifactPath string, stderr io.Writer) error {
	worker := exec.CommandContext(ctx, os.Args[0], "jobs", "exec-skill",
		"--job-id", jobID,
		"--manifest", manifestPath,
		"--artifact", artifactPath,
	)
	worker.Stdout = stderr
	worker.Stderr = stderr

	if err := worker.Start(); err != nil {
		return err
	}

	// Wait for the process in a goroutine to prevent zombie processes
	// The context cancellation will terminate the process if needed
	go func() {
		_ = worker.Wait()
	}()

	return nil
}
