package cmd

import (
	"context"
	"io"
	"os"
	"os/exec"
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
