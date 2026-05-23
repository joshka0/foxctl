package rlm

import (
	"context"
	"testing"
)

func TestValidateRunRequestRequiresPrompt(t *testing.T) {
	t.Parallel()

	err := ValidateRunRequest(Task{}, Environment{})
	if err == nil || err.Error() != "rlm: prompt is required" {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateRunRequestRejectsWritableTools(t *testing.T) {
	t.Parallel()

	err := ValidateRunRequest(Task{Prompt: "inspect"}, Environment{
		Tools: []Tool{{Name: "write_file", ReadOnly: false}},
	})
	if err == nil || err.Error() != "rlm: first-version runtime only allows read-only tools" {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateRunRequestAllowsReadOnlyTools(t *testing.T) {
	t.Parallel()

	err := ValidateRunRequest(Task{Prompt: "inspect"}, Environment{
		Tools: []Tool{{Name: "retrieve_mixed", ReadOnly: true}},
	})
	if err != nil {
		t.Fatalf("ValidateRunRequest: %v", err)
	}
}

func TestRunFuncExecutes(t *testing.T) {
	t.Parallel()

	runner := RunFunc(func(_ context.Context, task Task, env Environment) (Result, error) {
		return Result{
			Answer:       task.Prompt,
			Iterations:   1,
			EvidenceRefs: []string{env.Tools[0].Name},
		}, nil
	})
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
