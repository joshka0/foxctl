package ephemeral

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestPythonSkillRunnerRun(t *testing.T) {
	t.Parallel()

	runner, err := NewPythonSkillRunner(context.Background(), PythonSkillSpec{
		Name: "python_solver",
		Source: `
def solve(input):
    return {"ok": True, "answer": "solution = " + str(input["value"])}
`,
	})
	if err != nil {
		t.Fatalf("NewPythonSkillRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), map[string]any{"value": 42})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.OK {
		t.Fatalf("OK=false: %#v", result)
	}
	if got := result.Output["answer"]; got != "solution = 42" {
		t.Fatalf("answer=%v", got)
	}
}

func TestPythonSkillRunnerAllowsAlgorithmImports(t *testing.T) {
	t.Parallel()

	runner, err := NewPythonSkillRunner(context.Background(), PythonSkillSpec{
		Source: `
from bisect import bisect_left

def solve(input):
    values = sorted(input["values"])
    idx = bisect_left(values, input["target"])
    return {"ok": True, "answer": f"solution = {values[idx]}"}
`,
	})
	if err != nil {
		t.Fatalf("NewPythonSkillRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), map[string]any{
		"values": []any{5, 1, 9},
		"target": 4,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output["answer"] != "solution = 5" {
		t.Fatalf("answer=%v", result.Output["answer"])
	}
}

func TestPythonSkillRunnerAllowsTypeChecks(t *testing.T) {
	t.Parallel()

	runner, err := NewPythonSkillRunner(context.Background(), PythonSkillSpec{
		Source: `
def solve(input):
    kind = "dict" if isinstance(input, dict) else str(type(input))
    return {"ok": True, "answer": "solution = " + kind}
`,
	})
	if err != nil {
		t.Fatalf("NewPythonSkillRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), map[string]any{"value": 1})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output["answer"] != "solution = dict" {
		t.Fatalf("answer=%v", result.Output["answer"])
	}
}

func TestPythonSkillRunnerRepairsEscapedSynthesizedSource(t *testing.T) {
	t.Parallel()

	runner, err := NewPythonSkillRunner(context.Background(), PythonSkillSpec{
		Source: `def solve(input):\n    values = sorted(input["values"])\n    return {"ok": True, "answer": "solution = " + str(values[0])}\", \"input\": {}}`,
	})
	if err != nil {
		t.Fatalf("NewPythonSkillRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), map[string]any{"values": []any{3, 1, 2}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output["answer"] != "solution = 1" {
		t.Fatalf("answer=%v", result.Output["answer"])
	}
}

func TestPythonSkillRunnerRejectsUnsafeImport(t *testing.T) {
	t.Parallel()

	_, err := NewPythonSkillRunner(context.Background(), PythonSkillSpec{
		Source: `
import os

def solve(input):
    return {"ok": True, "answer": os.getcwd()}
`,
	})
	if err == nil || (!strings.Contains(err.Error(), "disallowed import os") && !strings.Contains(err.Error(), "disallowed selector os.getcwd")) {
		t.Fatalf("err=%v", err)
	}
}

func TestPythonSkillToolExecutor(t *testing.T) {
	t.Parallel()

	runner, err := NewPythonSkillRunner(context.Background(), PythonSkillSpec{
		Name:        "short_lived_python_solver",
		Description: "test python solver",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}}}`),
		Source: `
def Solve(input):
    return {"ok": True, "answer": input["value"]}
`,
	})
	if err != nil {
		t.Fatalf("NewPythonSkillRunner() error = %v", err)
	}
	exec := PythonSkillToolExecutor{Runner: runner}
	if defs := exec.List(); len(defs) != 1 || defs[0].Name != "short_lived_python_solver" {
		t.Fatalf("defs=%#v", defs)
	}
	raw, err := exec.Execute(context.Background(), "short_lived_python_solver", json.RawMessage(`{"value":"done"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var result GoSkillResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if got := result.Output["answer"]; got != "done" {
		t.Fatalf("answer=%v", got)
	}
}
