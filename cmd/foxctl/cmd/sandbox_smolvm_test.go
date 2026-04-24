package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/runtime/sandbox/smolvm"
)

func TestSandboxSmolVMProbeLMStudioWritesPlanEnvelope(t *testing.T) {
	t.Parallel()

	cmd := newSandboxCommand()
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"smolvm",
		"probe-lmstudio",
		"--image", "alpine:3.20",
		"--base-url", "http://127.0.0.1:1234/v1",
		"--outbound-localhost-only",
		"--allow-host", "lmstudio-proxy.local",
		"--allow-cidr", "10.10.0.0/16",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute sandbox smolvm probe-lmstudio: %v", err)
	}

	env := decodeSandboxEnvelopeMap(t, stdout.Bytes())
	if got := env["status"]; got != "ok" {
		t.Fatalf("status=%v payload=%s", got, stdout.String())
	}
	if got := env["command"]; got != "sandbox/smolvm/probe-lmstudio" {
		t.Fatalf("command=%v", got)
	}

	data := mustSandboxMap(t, env["data"])
	argv := mustStringSliceFromAnySlice(t, data["argv"])
	expectedTokens := []string{
		"smolvm", "machine", "run",
		"--image", "alpine:3.20",
		"--allow-host", "lmstudio-proxy.local",
		"--allow-cidr", "127.0.0.0/8",
		"--allow-cidr", "10.10.0.0/16",
		"--", "wget", "-qO-", "http://127.0.0.1:1234/v1/models",
	}
	if !containsSubsequence(argv, expectedTokens) {
		t.Fatalf("argv=%v missing expected subsequence=%v", argv, expectedTokens)
	}
}

func TestSandboxSmolVMPackagePlanWritesPackCreatePlan(t *testing.T) {
	t.Parallel()

	cmd := newSandboxCommand()
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"smolvm",
		"package-plan",
		"--from-vm", "foxctl-agent-stage",
		"--output", "/tmp/foxctl-agent",
		"--platform", "linux/arm64",
		"--cpus", "2",
		"--mem", "2048",
		"--no-sign",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute sandbox smolvm package-plan: %v", err)
	}

	env := decodeSandboxEnvelopeMap(t, stdout.Bytes())
	if got := env["status"]; got != "ok" {
		t.Fatalf("status=%v payload=%s", got, stdout.String())
	}
	if got := env["command"]; got != "sandbox/smolvm/package-plan" {
		t.Fatalf("command=%v", got)
	}

	data := mustSandboxMap(t, env["data"])
	argv := mustStringSliceFromAnySlice(t, data["argv"])
	expectedTokens := []string{
		"smolvm", "pack", "create",
		"--output", "/tmp/foxctl-agent",
		"--from-vm", "foxctl-agent-stage",
		"--oci-platform", "linux/arm64",
		"--cpus", "2",
		"--mem", "2048",
		"--no-sign",
	}
	if !containsSubsequence(argv, expectedTokens) {
		t.Fatalf("argv=%v missing expected subsequence=%v", argv, expectedTokens)
	}

	summary := mustSandboxMap(t, data["summary"])
	artifacts := mustSandboxMap(t, summary["pack_artifacts"])
	if artifacts["stub_path"] != "/tmp/foxctl-agent" {
		t.Fatalf("stub_path=%v", artifacts["stub_path"])
	}
	if artifacts["sidecar_path"] != "/tmp/foxctl-agent.smolmachine" {
		t.Fatalf("sidecar_path=%v", artifacts["sidecar_path"])
	}
}

func TestSandboxSmolVMRunAgentPlanWritesLimitations(t *testing.T) {
	t.Parallel()

	cmd := newSandboxCommand()
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"smolvm",
		"run-agent-plan",
		"--sidecar", "/tmp/foxctl-agent.smolmachine",
		"--repo", "/host/repo",
		"--repo-mode", "readonly",
		"--out", "/host/out",
		"--role", "researcher",
		"--prompt", "Investigate runtime shape",
		"--skills-allow", "fs_read,code_symbols",
		"--run-id", "run-main",
		"--agent-id", "researcher/child-1",
		"--llm-base-url", "http://127.0.0.1:1234/v1",
		"--local-llm-only",
		"--agent-dry-run",
		"--exec-mode", "autonomous",
		"--max-auto-turns", "1",
		"--max-iterations", "2",
		"--agent-timeout", "2m",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute sandbox smolvm run-agent-plan: %v", err)
	}

	env := decodeSandboxEnvelopeMap(t, stdout.Bytes())
	if got := env["status"]; got != "ok" {
		t.Fatalf("status=%v payload=%s", got, stdout.String())
	}
	if got := env["command"]; got != "sandbox/smolvm/run-agent-plan" {
		t.Fatalf("command=%v", got)
	}

	data := mustSandboxMap(t, env["data"])
	argv := mustStringSliceFromAnySlice(t, data["argv"])
	if !containsString(argv, "--net") {
		t.Fatalf("expected --net in argv: %v", argv)
	}
	for _, forbidden := range []string{"--outbound-localhost-only", "--allow-host", "--allow-cidr"} {
		if containsString(argv, forbidden) {
			t.Fatalf("pack run argv should not include %s: %v", forbidden, argv)
		}
	}

	limitations := mustStringSliceFromAnySlice(t, data["limitations"])
	if !containsAnySubstring(limitations, "does not support --outbound-localhost-only") {
		t.Fatalf("limitations=%v", limitations)
	}

	envList := mustListOfMap(t, data["env"])
	if !hasEnvVar(envList, "FOXCTL_LLM_PROVIDER", "openai_compat") {
		t.Fatalf("expected FOXCTL_LLM_PROVIDER=openai_compat in env: %#v", envList)
	}
	for _, token := range []string{"--dry-run", "--exec-mode", "autonomous", "--max-auto-turns", "1", "--max-iterations", "2", "--timeout", "2m", "--llm-provider", "openai_compat", "--llm-base-url", "http://127.0.0.1:1234/v1"} {
		if !containsString(argv, token) {
			t.Fatalf("argv missing %q: %v", token, argv)
		}
	}
}

func TestSandboxSmolVMRunAgentExecutesWithInjectedRunner(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

func TestSandboxSmolVMFoxctlPackagePlanWritesOrderedSteps(t *testing.T) {
	t.Parallel()

	cmd := newSandboxCommand()
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"smolvm",
		"foxctl-package-plan",
		"--build-output", "/host/build/foxctl",
		"--machine-name", "foxctl-agent-stage",
		"--output", "/host/out/foxctl-agent",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute sandbox smolvm foxctl-package-plan: %v", err)
	}

	env := decodeSandboxEnvelopeMap(t, stdout.Bytes())
	if got := env["status"]; got != "ok" {
		t.Fatalf("status=%v payload=%s", got, stdout.String())
	}
	if got := env["command"]; got != "sandbox/smolvm/foxctl-package-plan" {
		t.Fatalf("command=%v", got)
	}

	data := mustSandboxMap(t, env["data"])
	steps := mustListOfMap(t, data["steps"])
	if len(steps) != 10 {
		t.Fatalf("step count=%d want=10", len(steps))
	}

	stepNames := make([]string, 0, len(steps))
	for _, step := range steps {
		name, _ := step["name"].(string)
		stepNames = append(stepNames, name)
	}
	wantStepNames := []string{
		"host_prepare_dirs",
		"host_go_build",
		"machine_create",
		"machine_start",
		"machine_copy_foxctl",
		"machine_chmod_foxctl",
		"machine_verify_foxctl",
		"machine_stop",
		"pack_create",
		"packed_verify_foxctl",
	}
	if !reflect.DeepEqual(stepNames, wantStepNames) {
		t.Fatalf("step names=%v\nwant=%v", stepNames, wantStepNames)
	}

	createCommand := mustSandboxMap(t, steps[2]["command"])
	createArgv := mustStringSliceFromAnySlice(t, createCommand["argv"])
	if !containsSubsequence(createArgv, []string{"--allow-cidr", "0.0.0.0/0", "foxctl-agent-stage"}) {
		t.Fatalf("machine create argv missing CIDR workaround + name ordering: %v", createArgv)
	}

	if got := mustStringSliceFromAnySlice(t, data["packed_run_argv"]); !reflect.DeepEqual(got, []string{"/host/out/foxctl-agent", "run", "--", "/usr/local/bin/foxctl", "--help"}) {
		t.Fatalf("packed_run_argv=%v", got)
	}
}

func TestSandboxSmolVMFoxctlPackageExecutesWithInjectedRunner(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

func mustStringSliceFromAnySlice(t *testing.T, value any) []string {
	t.Helper()
	raw, ok := value.([]any)
	if !ok {
		t.Fatalf("value type=%T is not []any", value)
	}
	items := make([]string, 0, len(raw))
	for _, item := range raw {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("slice item type=%T is not string", item)
		}
		items = append(items, s)
	}
	return items
}

func containsSubsequence(haystack, needle []string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func containsAnySubstring(items []string, substr string) bool {
	for _, item := range items {
		if strings.Contains(item, substr) {
			return true
		}
	}
	return false
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

func hasEnvVar(env []map[string]any, name, wantValue string) bool {
	for _, item := range env {
		gotName, _ := item["name"].(string)
		gotValue, _ := item["value"].(string)
		if gotName == name && gotValue == wantValue {
			return true
		}
	}
	return false
}
