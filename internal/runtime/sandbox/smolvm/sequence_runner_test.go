package smolvm

import (
	"context"
	"errors"
	"testing"
)

func TestRunCommandSequenceSuccess(t *testing.T) {
	t.Parallel()

	calls := make([]string, 0, 2)
	runner := RunnerFunc(func(_ context.Context, plan CommandPlan) (CommandResult, error) {
		calls = append(calls, plan.Summary.Mode)
		return CommandResult{ExitCode: 0}, nil
	})

	result, err := RunCommandSequence(context.Background(), runner, []CommandStepPlan{
		{Name: "one", Command: commandStep("one", "true")},
		{Name: "two", Command: commandStep("two", "true")},
	})
	if err != nil {
		t.Fatalf("RunCommandSequence() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("result.Success=false")
	}
	if len(result.Steps) != 2 || len(calls) != 2 {
		t.Fatalf("result=%+v calls=%v", result, calls)
	}
}

func TestRunCommandSequenceStopsAndRunsOptionalCleanup(t *testing.T) {
	t.Parallel()

	calls := make([]string, 0, 3)
	runner := RunnerFunc(func(_ context.Context, plan CommandPlan) (CommandResult, error) {
		calls = append(calls, plan.Summary.Mode)
		if plan.Summary.Mode == "fail" {
			return CommandResult{ExitCode: 7}, nil
		}
		return CommandResult{ExitCode: 0}, nil
	})

	result, err := RunCommandSequence(context.Background(), runner, []CommandStepPlan{
		{Name: "one", Command: commandStep("one", "true")},
		{Name: "fail", Command: commandStep("fail", "false")},
		{Name: "skipped", Command: commandStep("skipped", "true")},
		{Name: "cleanup", Command: commandStep("cleanup", "cleanup"), Optional: true},
	})
	if !errors.Is(err, ErrCommandSequenceFailed) {
		t.Fatalf("expected ErrCommandSequenceFailed, got %v", err)
	}
	if result.Success {
		t.Fatalf("result.Success=true")
	}
	if got, want := calls, []string{"one", "fail", "cleanup"}; !equalStrings(got, want) {
		t.Fatalf("calls=%v want=%v", got, want)
	}
	if !result.Steps[2].Skipped {
		t.Fatalf("expected third step to be skipped: %+v", result.Steps[2])
	}
	if result.Steps[3].Error != "" {
		t.Fatalf("cleanup should succeed: %+v", result.Steps[3])
	}
}

func TestRunCommandSequenceOptionalFailureDoesNotFailSequence(t *testing.T) {
	t.Parallel()

	runner := RunnerFunc(func(_ context.Context, plan CommandPlan) (CommandResult, error) {
		if plan.Summary.Mode == "cleanup" {
			return CommandResult{}, errors.New("cleanup failed")
		}
		return CommandResult{ExitCode: 0}, nil
	})

	result, err := RunCommandSequence(context.Background(), runner, []CommandStepPlan{
		{Name: "one", Command: commandStep("one", "true")},
		{Name: "cleanup", Command: commandStep("cleanup", "cleanup"), Optional: true},
	})
	if err != nil {
		t.Fatalf("RunCommandSequence() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("result.Success=false")
	}
	if result.Steps[1].Error == "" {
		t.Fatalf("expected optional cleanup error to be captured")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
