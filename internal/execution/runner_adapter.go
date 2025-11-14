package execution

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/jkatigb/agentctl/internal/runner"
	"github.com/jkatigb/agentctl/internal/skill"
)

// RunnerExecutor adapts the existing runner.Run to the SkillExecutor interface.
type RunnerExecutor struct{}

// NewRunnerExecutor creates a new executor using the default runner.
func NewRunnerExecutor() SkillExecutor {
	return &RunnerExecutor{}
}

// Execute implements SkillExecutor using runner.Run.
func (e *RunnerExecutor) Execute(ctx context.Context, opts ExecuteOptions) (*Result, error) {
	manifest, err := resolveManifest(opts)
	if err != nil {
		return nil, err
	}

	// Call existing runner
	stdout, stderr, err := runner.Run(ctx, manifest, opts.ArtifactPath, opts.Input)

	// Determine exit code
	exitCode := 0
	if err != nil {
		exitCode = determineExitCode(err)
	}

	return &Result{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
		Error:    err,
	}, nil
}

// determineExitCode extracts the exit code from an error.
func determineExitCode(err error) int {
	if err == nil {
		return 0
	}

	// Try to extract exit code from exec.ExitError
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}

	// Default to 1 for any other error
	return 1
}

func resolveManifest(opts ExecuteOptions) (skill.Manifest, error) {
	if opts.Manifest.Metadata.Name != "" || opts.Manifest.Distribution.Type != "" {
		return opts.Manifest, nil
	}
	if opts.ManifestPath == "" {
		return skill.Manifest{}, fmt.Errorf("manifest path required")
	}
	manifest, err := skill.LoadManifest(opts.ManifestPath)
	if err != nil {
		return skill.Manifest{}, fmt.Errorf("load manifest: %w", err)
	}
	return manifest, nil
}
