package smolvm

import (
	"context"
	"errors"
	"testing"
)

func TestRunnerFuncSuccess(t *testing.T) {
	t.Parallel()

	runner := RunnerFunc(func(ctx context.Context, plan CommandPlan) (CommandResult, error) {
		if len(plan.Argv) == 0 {
			t.Fatalf("plan argv should be passed to runner")
		}
		return CommandResult{
			Stdout:     "ok",
			Stderr:     "",
			ExitCode:   0,
			DurationMS: 15,
		}, nil
	})

	result, err := runner.Run(context.Background(), CommandPlan{
		Argv: []string{"smolvm", "pack", "create"},
	})
	if err != nil {
		t.Fatalf("RunnerFunc.Run() error = %v", err)
	}
	if result.Stdout != "ok" || result.ExitCode != 0 {
		t.Fatalf("result=%+v", result)
	}
}

func TestRunnerFuncFailure(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("runner failure")
	runner := RunnerFunc(func(context.Context, CommandPlan) (CommandResult, error) {
		return CommandResult{
			Stdout:     "",
			Stderr:     "boom",
			ExitCode:   1,
			DurationMS: 1,
		}, sentinel
	})

	result, err := runner.Run(context.Background(), CommandPlan{
		Argv: []string{"smolvm", "pack", "run"},
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if result.ExitCode != 1 || result.Stderr != "boom" {
		t.Fatalf("result=%+v", result)
	}
}

func TestExecCommandRunnerRequiresArgv(t *testing.T) {
	t.Parallel()

	runner := ExecCommandRunner{}
	_, err := runner.Run(context.Background(), CommandPlan{})
	if !errors.Is(err, ErrEmptyCommandArgv) {
		t.Fatalf("expected ErrEmptyCommandArgv, got %v", err)
	}
}
