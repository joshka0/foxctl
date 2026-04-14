package execution

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/runtime/execution/runner"
)

// RunnerExecutor adapts the existing runner.Run to the SkillExecutor interface.
type RunnerExecutor struct{}

// NewRunnerExecutor creates a new executor using the default runner.
func NewRunnerExecutor() SkillExecutor {
	return &RunnerExecutor{}
}

// Execute implements SkillExecutor using runner.RunWithOptions.
//
// Index:
// - Purpose: Execute a skill via the runner adapter
// - Flow: resolve manifest → run with options → map exit code → return result
// - SideEffects: launches skill subprocess
// - FailureModes: manifest errors, runner errors
// - Related: runner.RunWithOptions, resolveManifest
// - Keywords: skill_execute, runner, manifest, exit_code
func (e *RunnerExecutor) Execute(ctx context.Context, opts ExecuteOptions) (*Result, error) {
	manifest, err := resolveManifest(opts)
	if err != nil {
		return nil, err
	}

	// Call existing runner with options
	stdout, stderr, err := runner.RunWithOptions(ctx, runner.RunOptions{
		Manifest:     manifest,
		ArtifactPath: opts.ArtifactPath,
		Input:        opts.Input,
		ExtraEnv:     opts.ExtraEnv,
	})

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
	// If a pre-parsed manifest is provided, validate it has required fields
	if opts.Manifest.Metadata.Name != "" || opts.Manifest.Distribution.Type != "" {
		// Require both Name and Distribution.Type to prevent partial manifests
		if opts.Manifest.Metadata.Name == "" {
			return skill.Manifest{}, fmt.Errorf("manifest metadata.name is required")
		}
		if opts.Manifest.Distribution.Type == "" {
			return skill.Manifest{}, fmt.Errorf("manifest distribution.type is required")
		}
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
