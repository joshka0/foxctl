package ephemeral

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
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

func TestPythonSkillRunnerPreservesEscapedNewlineInsideDecodedSource(t *testing.T) {
	t.Parallel()

	runner, err := NewPythonSkillRunner(context.Background(), PythonSkillSpec{
		Source: `def solve(input):
    prompt = input.get("prompt", "")
    lines = prompt.split('\n')
    return {"ok": True, "answer": "solution = " + str(len(lines))}
`,
	})
	if err != nil {
		t.Fatalf("NewPythonSkillRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), map[string]any{"prompt": "a\nb\nc"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output["answer"] != "solution = 3" {
		t.Fatalf("answer=%v", result.Output["answer"])
	}
}

func TestPythonSkillRunnerRepairsRawNewlineInsideStringLiteral(t *testing.T) {
	t.Parallel()

	runner, err := NewPythonSkillRunner(context.Background(), PythonSkillSpec{
		Source: "def solve(input):\n    sep = '\n'\n    return {\"ok\": True, \"answer\": \"solution = \" + str(len(input.get(\"text\", \"\").split(sep)))}\n",
	})
	if err != nil {
		t.Fatalf("NewPythonSkillRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), map[string]any{"text": "a\nb"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output["answer"] != "solution = 2" {
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

func TestPythonSkillRunnerAllowsRegexImport(t *testing.T) {
	t.Parallel()

	runner, err := NewPythonSkillRunner(context.Background(), PythonSkillSpec{
		Source: `
import re

def solve(input):
    return {"ok": True, "answer": "solution = " + str(len(re.findall(r"[a-z]+", input.get("text", ""))))}
`,
	})
	if err != nil {
		t.Fatalf("NewPythonSkillRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), map[string]any{"text": "a 1 bc"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output["answer"] != "solution = 2" {
		t.Fatalf("answer=%v", result.Output["answer"])
	}
}

func TestPythonSkillRunnerAllowsASTLiteralEval(t *testing.T) {
	t.Parallel()

	runner, err := NewPythonSkillRunner(context.Background(), PythonSkillSpec{
		Source: `
import ast

def solve(input):
    values = ast.literal_eval(input.get("values", "[]"))
    return {"ok": True, "answer": "solution = " + str(sum(values))}
`,
	})
	if err != nil {
		t.Fatalf("NewPythonSkillRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), map[string]any{"values": "[1, 2, 3]"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output["answer"] != "solution = 6" {
		t.Fatalf("answer=%v", result.Output["answer"])
	}
}

func TestPythonSkillRunnerAllowsCommonSafeBuiltins(t *testing.T) {
	t.Parallel()

	runner, err := NewPythonSkillRunner(context.Background(), PythonSkillSpec{
		Source: `
def solve(input):
    print("debug output is contained")
    first_even = next(x for x in input["values"] if x % 2 == 0)
    return {"ok": True, "answer": "solution = " + str(first_even)}
`,
	})
	if err != nil {
		t.Fatalf("NewPythonSkillRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), map[string]any{"values": []any{1, 3, 4, 6}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output["answer"] != "solution = 4" {
		t.Fatalf("answer=%v", result.Output["answer"])
	}
}

func TestPythonSkillRunnerRunTimeoutKillsRunawayHelper(t *testing.T) {
	t.Parallel()

	runner, err := NewPythonSkillRunner(context.Background(), PythonSkillSpec{
		Source: `
def solve(input):
    while True:
        pass
`,
		RunTimeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewPythonSkillRunner() error = %v", err)
	}
	start := time.Now()
	_, err = runner.Run(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Run() err=%v, want timeout", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Run() took %s, want fast timeout", elapsed)
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
