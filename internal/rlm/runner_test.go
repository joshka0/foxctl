package rlm

import (
	"context"
	"errors"
	"testing"
)

func TestReadOnlyRunnerRequiresPrompt(t *testing.T) {
	t.Parallel()

	runner := ReadOnlyRunner{}
	_, err := runner.Run(context.Background(), Task{}, Environment{})
	if err == nil || err.Error() != "rlm: prompt is required" {
		t.Fatalf("err=%v", err)
	}
}

func TestReadOnlyRunnerRejectsWritableTools(t *testing.T) {
	t.Parallel()

	runner := ReadOnlyRunner{}
	_, err := runner.Run(context.Background(), Task{Prompt: "inspect"}, Environment{
		Tools: []Tool{{Name: "write_file", ReadOnly: false}},
	})
	if err == nil || err.Error() != "rlm: first-version runtime only allows read-only tools" {
		t.Fatalf("err=%v", err)
	}
}

func TestReadOnlyRunnerReturnsNotImplemented(t *testing.T) {
	t.Parallel()

	runner := ReadOnlyRunner{}
	_, err := runner.Run(context.Background(), Task{Prompt: "inspect"}, Environment{
		Tools: []Tool{{Name: "retrieve_mixed", ReadOnly: true}},
	})
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("err=%v want ErrNotImplemented", err)
	}
}

func TestReadOnlyRunnerExecutesRunFunc(t *testing.T) {
	t.Parallel()

	runner := ReadOnlyRunner{
		Execute: func(_ context.Context, task Task, env Environment) (Result, error) {
			return Result{
				Answer:       task.Prompt,
				Iterations:   1,
				EvidenceRefs: []string{env.Tools[0].Name},
			}, nil
		},
	}
	got, err := runner.Run(context.Background(), Task{Prompt: "inspect repo"}, Environment{
		Tools: []Tool{{Name: "retrieve_mixed", ReadOnly: true}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Answer != "inspect repo" {
		t.Fatalf("answer=%q", got.Answer)
	}
	if got.Iterations != 1 {
		t.Fatalf("iterations=%d", got.Iterations)
	}
	if len(got.EvidenceRefs) != 1 || got.EvidenceRefs[0] != "retrieve_mixed" {
		t.Fatalf("evidence=%v", got.EvidenceRefs)
	}
}
