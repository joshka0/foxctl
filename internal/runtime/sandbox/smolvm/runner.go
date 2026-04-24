package smolvm

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"time"
)

var ErrEmptyCommandArgv = errors.New("smolvm: command argv is required")

// CommandResult captures one command execution attempt.
type CommandResult struct {
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
}

// CommandRunner executes one planned command.
type CommandRunner interface {
	Run(ctx context.Context, plan CommandPlan) (CommandResult, error)
}

// RunnerFunc adapts a function to the CommandRunner interface.
type RunnerFunc func(ctx context.Context, plan CommandPlan) (CommandResult, error)

// Run executes fn(ctx, plan).
func (fn RunnerFunc) Run(ctx context.Context, plan CommandPlan) (CommandResult, error) {
	return fn(ctx, plan)
}

// ExecCommandRunner executes commands with os/exec. Tests should prefer fakes.
type ExecCommandRunner struct{}

// Run executes one command plan and captures stdout/stderr/exit code/duration.
func (ExecCommandRunner) Run(ctx context.Context, plan CommandPlan) (CommandResult, error) {
	if len(plan.Argv) == 0 {
		return CommandResult{}, ErrEmptyCommandArgv
	}

	start := time.Now()

	cmd := exec.CommandContext(ctx, plan.Argv[0], plan.Argv[1:]...)
	if len(plan.Env) > 0 {
		env := os.Environ()
		for _, item := range plan.Env {
			env = append(env, item.Name+"="+item.Value)
		}
		cmd.Env = env
	}

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := CommandResult{
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMS: time.Since(start).Milliseconds(),
	}
	if err == nil {
		return result, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	return result, err
}
