package rlm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type recordingToolExecutor struct {
	calls []string
}

func (e *recordingToolExecutor) Execute(_ context.Context, name string, _ json.RawMessage) (map[string]any, error) {
	e.calls = append(e.calls, name)
	return map[string]any{"ok": true}, nil
}

func TestLLMToolExecutorDeniesUndeclaredTool(t *testing.T) {
	t.Parallel()

	base := &recordingToolExecutor{}
	exec := NewLLMToolExecutor(base, []Tool{{Name: "retrieve_code", ReadOnly: true}})

	_, err := exec.Execute(context.Background(), "undeclared_tool", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), `tool "undeclared_tool" is not declared`) {
		t.Fatalf("err=%v", err)
	}
	if len(base.calls) != 0 {
		t.Fatalf("delegate calls=%v want none", base.calls)
	}
}

func TestLLMToolExecutorDeniesWritableTool(t *testing.T) {
	t.Parallel()

	base := &recordingToolExecutor{}
	exec := NewLLMToolExecutor(base, []Tool{{Name: "write_file", ReadOnly: false}})

	_, err := exec.Execute(context.Background(), "write_file", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), `tool "write_file" is not read-only`) {
		t.Fatalf("err=%v", err)
	}
	if len(base.calls) != 0 {
		t.Fatalf("delegate calls=%v want none", base.calls)
	}
}

func TestInspectRunnerDeniesUndeclaredToolBeforeDelegating(t *testing.T) {
	t.Parallel()

	base := &recordingToolExecutor{}
	result, err := (InspectRunner{Tools: base}).Run(context.Background(), Task{
		Prompt: "inspect code",
	}, Environment{
		Tools: []Tool{{Name: "load_evidence_ref", ReadOnly: true}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(base.calls) != 0 {
		t.Fatalf("delegate calls=%v want none", base.calls)
	}
	if result.Answer == "" {
		t.Fatalf("expected fallback answer")
	}
}

func TestLambdaRunnerDeniesUndeclaredToolBeforeDelegating(t *testing.T) {
	t.Parallel()

	base := &recordingToolExecutor{}
	_, err := (LambdaRunner{Tools: base}).Run(context.Background(), Task{
		Prompt:        "inspect code",
		MaxIterations: 1,
		MaxSubcalls:   1,
	}, Environment{
		Tools: []Tool{{Name: "load_evidence_ref", ReadOnly: true}},
	})
	if err == nil || !strings.Contains(err.Error(), "lambda leaf search") {
		t.Fatalf("err=%v", err)
	}
	if len(base.calls) != 0 {
		t.Fatalf("delegate calls=%v want none", base.calls)
	}
}
