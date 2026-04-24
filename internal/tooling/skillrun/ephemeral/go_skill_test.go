package ephemeral

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestGoSkillRunnerRun(t *testing.T) {
	t.Parallel()

	runner, err := NewGoSkillRunner(GoSkillSpec{
		Name: "domain_solver",
		Source: `
func Solve(input map[string]any) map[string]any {
    prompt, _ := input["prompt"].(string)
    return map[string]any{
        "ok": true,
        "answer": "solution = " + prompt,
    }
}`,
	})
	if err != nil {
		t.Fatalf("NewGoSkillRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), map[string]any{"prompt": "42"})
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

func TestGoSkillToolExecutor(t *testing.T) {
	t.Parallel()

	runner, err := NewGoSkillRunner(GoSkillSpec{
		Name:        "short_lived_solver",
		Description: "test solver",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}}}`),
		Source: `
func Solve(input map[string]any) map[string]any {
    value, _ := input["value"].(string)
    return map[string]any{"ok": true, "answer": value}
}`,
	})
	if err != nil {
		t.Fatalf("NewGoSkillRunner() error = %v", err)
	}
	exec := GoSkillToolExecutor{Runner: runner}
	if defs := exec.List(); len(defs) != 1 || defs[0].Name != "short_lived_solver" {
		t.Fatalf("defs=%#v", defs)
	}
	raw, err := exec.Execute(context.Background(), "short_lived_solver", json.RawMessage(`{"value":"done"}`))
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

func TestValidateGoSkillSourceRejectsBadShape(t *testing.T) {
	t.Parallel()

	err := ValidateGoSkillSource(`func Solve(input string) string { return input }`)
	if err == nil || !strings.Contains(err.Error(), "map[string]any") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateGoSkillSourceRejectsMissingBody(t *testing.T) {
	t.Parallel()

	err := ValidateGoSkillSource(`func Solve(input map[string]any) map[string]any`)
	if err == nil || !strings.Contains(err.Error(), "function body") {
		t.Fatalf("err=%v", err)
	}
}

func TestGoSkillRunnerAcceptsOptionalPackageDecl(t *testing.T) {
	t.Parallel()

	runner, err := NewGoSkillRunner(GoSkillSpec{
		Source: `package main

func Solve(input map[string]any) map[string]any {
    return map[string]any{"ok": true, "answer": "solution = 42"}
}`,
	})
	if err != nil {
		t.Fatalf("NewGoSkillRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output["answer"] != "solution = 42" {
		t.Fatalf("answer=%v", result.Output["answer"])
	}
}

func TestGoSkillRunnerAddsMissingAllowedImports(t *testing.T) {
	t.Parallel()

	runner, err := NewGoSkillRunner(GoSkillSpec{
		Source: `func Solve(input map[string]any) map[string]any {
    return map[string]any{"ok": true, "answer": fmt.Sprintf("solution = %d", 42)}
}`,
	})
	if err != nil {
		t.Fatalf("NewGoSkillRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output["answer"] != "solution = 42" {
		t.Fatalf("answer=%v", result.Output["answer"])
	}
}

func TestGoSkillRunnerRepairsEscapedSynthesizedSource(t *testing.T) {
	t.Parallel()

	runner, err := NewGoSkillRunner(GoSkillSpec{
		Source: `func Solve(input map[string]any) map[string]any {\n    value := input[\"value\"].(string)\n    return map[string]any{\"ok\": true, \"answer\": \"solution = \" + value}\n}\",`,
	})
	if err != nil {
		t.Fatalf("NewGoSkillRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), map[string]any{"value": "42"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output["answer"] != "solution = 42" {
		t.Fatalf("answer=%v", result.Output["answer"])
	}
}

func TestGoSkillRunnerExtractsSolveFromTrailingJSONFragments(t *testing.T) {
	t.Parallel()

	runner, err := NewGoSkillRunner(GoSkillSpec{
		Source: `{"source":"func Solve(input map[string]any) map[string]any {\n return map[string]any{\"ok\": true, \"answer\": \"solution = repaired\"}\n}", "input":{}}`,
	})
	if err != nil {
		t.Fatalf("NewGoSkillRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output["answer"] != "solution = repaired" {
		t.Fatalf("answer=%v", result.Output["answer"])
	}
}

func TestGoSkillRunnerPreservesHelperDeclarations(t *testing.T) {
	t.Parallel()

	runner, err := NewGoSkillRunner(GoSkillSpec{
		Source: `func Solve(input map[string]any) map[string]any {
	return map[string]any{"ok": true, "answer": helperAnswer()}
}

func helperAnswer() string {
	return "solution = helper"
}`,
	})
	if err != nil {
		t.Fatalf("NewGoSkillRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output["answer"] != "solution = helper" {
		t.Fatalf("answer=%v", result.Output["answer"])
	}
}

func TestValidateGoSkillSourceRejectsUnsafeImports(t *testing.T) {
	t.Parallel()

	err := ValidateGoSkillSource(`
import "os"
func Solve(input map[string]any) map[string]any {
    _, _ = os.ReadFile("/etc/passwd")
    return map[string]any{"ok": false}
}`)
	if err == nil || (!strings.Contains(err.Error(), "disallowed import os") && !strings.Contains(err.Error(), "disallowed selector os.ReadFile")) {
		t.Fatalf("err=%v", err)
	}
}
