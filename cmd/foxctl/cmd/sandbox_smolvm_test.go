package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/joshka0/foxctl/internal/runtime/sandbox/smolvm"
)

func TestSandboxSmolVMRunAgentExecutesWithInjectedRunner(t *testing.T) {
	runner := smolvm.RunnerFunc(func(_ context.Context, plan smolvm.CommandPlan) (smolvm.CommandResult, error) {
		if plan.Summary.Mode != "pack_run_agent" {
			t.Fatalf("mode=%q", plan.Summary.Mode)
		}
		return smolvm.CommandResult{ExitCode: 0, Stdout: `{"status":"ok"}`}, nil
	})

	cmd := newSandboxSmolVMRunAgentPlanCommandWithRunner(runner)
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--dry-run=false",
		"--sidecar", "/tmp/foxctl-agent.smolmachine",
		"--repo", "/host/repo",
		"--out", "/host/out",
		"--role", "researcher",
		"--prompt", "Investigate runtime shape",
		"--run-id", "run-main",
		"--foxctl-binary", "/usr/local/bin/foxctl",
		"--agent-dry-run",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute sandbox smolvm run-agent: %v", err)
	}

	env := decodeSandboxEnvelopeMap(t, stdout.Bytes())
	if got := env["status"]; got != "ok" {
		t.Fatalf("status=%v payload=%s", got, stdout.String())
	}
	if got := env["command"]; got != "sandbox/smolvm/run-agent-plan" {
		t.Fatalf("command=%v", got)
	}
	data := mustSandboxMap(t, env["data"])
	execution := mustSandboxMap(t, data["execution"])
	if execution["wired"] != true || execution["success"] != true {
		t.Fatalf("execution=%v", execution)
	}
}

func TestSandboxSmolVMRunAgentExecutionFailureWritesErrorEnvelope(t *testing.T) {
	runner := smolvm.RunnerFunc(func(context.Context, smolvm.CommandPlan) (smolvm.CommandResult, error) {
		return smolvm.CommandResult{ExitCode: 3, Stderr: "pack run failed"}, nil
	})

	cmd := newSandboxSmolVMRunAgentPlanCommandWithRunner(runner)
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--dry-run=false",
		"--sidecar", "/tmp/foxctl-agent.smolmachine",
		"--repo", "/host/repo",
		"--out", "/host/out",
		"--role", "researcher",
		"--prompt", "Investigate runtime shape",
		"--run-id", "run-main",
	})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected non-nil error")
	}

	env := decodeSandboxEnvelopeMap(t, stdout.Bytes())
	if got := env["status"]; got != "error" {
		t.Fatalf("status=%v payload=%s", got, stdout.String())
	}
	if got := env["command"]; got != "sandbox/smolvm/run-agent-plan" {
		t.Fatalf("command=%v", got)
	}
	data := mustSandboxMap(t, env["data"])
	execution := mustSandboxMap(t, data["execution"])
	if execution["success"] != false {
		t.Fatalf("execution=%v", execution)
	}
}

func TestSandboxSmolVMFoxctlPackageExecutesWithInjectedRunner(t *testing.T) {
	var calls []string
	runner := smolvm.RunnerFunc(func(_ context.Context, plan smolvm.CommandPlan) (smolvm.CommandResult, error) {
		calls = append(calls, plan.Summary.Mode)
		return smolvm.CommandResult{ExitCode: 0}, nil
	})

	cmd := newSandboxSmolVMFoxctlPackageCommandWithRunner(runner)
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--dry-run=false",
		"--build-output", "/host/build/foxctl",
		"--machine-name", "foxctl-agent-stage",
		"--output", "/host/out/foxctl-agent",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute sandbox smolvm foxctl-package: %v", err)
	}

	env := decodeSandboxEnvelopeMap(t, stdout.Bytes())
	if got := env["status"]; got != "ok" {
		t.Fatalf("status=%v payload=%s", got, stdout.String())
	}
	if got := env["command"]; got != "sandbox/smolvm/foxctl-package" {
		t.Fatalf("command=%v", got)
	}
	data := mustSandboxMap(t, env["data"])
	execution := mustSandboxMap(t, data["execution"])
	if execution["wired"] != true || execution["success"] != true {
		t.Fatalf("execution=%v", execution)
	}
	if len(calls) != 10 {
		t.Fatalf("calls=%v", calls)
	}
}

func TestSandboxSmolVMFoxctlPackageFailureWritesErrorEnvelope(t *testing.T) {
	runner := smolvm.RunnerFunc(func(_ context.Context, plan smolvm.CommandPlan) (smolvm.CommandResult, error) {
		if plan.Summary.Mode == "machine_create" {
			return smolvm.CommandResult{ExitCode: 2, Stderr: "create failed"}, nil
		}
		return smolvm.CommandResult{ExitCode: 0}, nil
	})

	cmd := newSandboxSmolVMFoxctlPackageCommandWithRunner(runner)
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--dry-run=false",
		"--build-output", "/host/build/foxctl",
		"--machine-name", "foxctl-agent-stage",
		"--output", "/host/out/foxctl-agent",
		"--cleanup-machine",
	})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected execution error")
	}

	env := decodeSandboxEnvelopeMap(t, stdout.Bytes())
	if got := env["status"]; got != "error" {
		t.Fatalf("status=%v payload=%s", got, stdout.String())
	}
	data := mustSandboxMap(t, env["data"])
	execution := mustSandboxMap(t, data["execution"])
	if execution["success"] != false {
		t.Fatalf("execution=%v", execution)
	}
	results := mustListOfMap(t, execution["steps"])
	last := results[len(results)-1]
	if last["name"] != "machine_delete" {
		t.Fatalf("last step=%v", last)
	}
}

func decodeSandboxEnvelopeMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	line := bytes.TrimSpace(raw)
	if idx := bytes.IndexByte(line, '\n'); idx >= 0 {
		line = bytes.TrimSpace(line[:idx])
	}
	var env map[string]any
	if err := json.Unmarshal(line, &env); err != nil {
		t.Fatalf("decode envelope: %v\npayload=%s", err, string(raw))
	}
	return env
}

func mustSandboxMap(t *testing.T, value any) map[string]any {
	t.Helper()
	m, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value type=%T is not map[string]any", value)
	}
	return m
}

func mustListOfMap(t *testing.T, value any) []map[string]any {
	t.Helper()
	raw, ok := value.([]any)
	if !ok {
		t.Fatalf("value type=%T is not []any", value)
	}
	result := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("item type=%T is not map[string]any", item)
		}
		result = append(result, m)
	}
	return result
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
