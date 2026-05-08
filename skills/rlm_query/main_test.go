package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
)

func TestExecuteRLMQueryNormalizesEnvelope(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-openrouter-key")
	t.Setenv("FOXCTL_RLM_LLM_API_KEY", "")

	var gotDir string
	var gotArgs []string
	var gotEnv []string
	runner := func(_ context.Context, dir, _ string, env []string, args ...string) executil.CmdResult {
		gotDir = dir
		gotEnv = append([]string(nil), env...)
		gotArgs = append([]string(nil), args...)
		return executil.CmdResult{
			Stdout: []byte(`[CONTEXT] iter=1 finish=tool_calls
{"version":1,"status":"ok","command":"rlm/run","data":{"mode":"llm","result":{"answer":"Use internal/interfaces/web/api/skill_runner.go.","iterations":2,"metadata":{"retrieved_paths":["internal/interfaces/web/api/skill_runner.go"],"evidence_refs":["path:internal/interfaces/web/api/skill_runner.go"],"tool_names":["retrieve_code","load_evidence_ref"],"parent_total_tokens":24296,"parent_tool_usage":{"target_tool_invocations":1}}},"run_spec":{"route_profile":"code_retrieval","plan_mode":"free","tool_policy":{"profile":"code-intel"}}},"meta":{"ts":"2026-05-07T00:00:00Z"},"error":{}}
`),
		}
	}

	out, err := executeRLMQuery(context.Background(), "/repo", Input{Prompt: "find symbol", Workspace: "."}, runner)
	if err != nil {
		t.Fatalf("executeRLMQuery returned error: %v", err)
	}
	if gotDir != "/repo" {
		t.Fatalf("runner dir = %q, want /repo", gotDir)
	}
	wantPrefix := []string{"rlm", "run", "--workspace", "/repo", "--executor", "llm"}
	if !reflect.DeepEqual(gotArgs[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("args prefix = %v, want %v", gotArgs[:len(wantPrefix)], wantPrefix)
	}
	if !containsArgPair(gotArgs, "--tool-profile", "code-intel") {
		t.Fatalf("args missing code-intel tool profile: %v", gotArgs)
	}
	if !contains(gotArgs, "--require-tool-use=true") {
		t.Fatalf("args missing require-tool-use=true: %v", gotArgs)
	}
	if !contains(gotEnv, "FOXCTL_RLM_LLM_API_KEY=test-openrouter-key") {
		t.Fatalf("child env did not map OPENROUTER_API_KEY: %v", gotEnv)
	}
	if out.Answer != "Use internal/interfaces/web/api/skill_runner.go." {
		t.Fatalf("answer = %q", out.Answer)
	}
	if !reflect.DeepEqual(out.RetrievedPaths, []string{"internal/interfaces/web/api/skill_runner.go"}) {
		t.Fatalf("retrieved paths = %v", out.RetrievedPaths)
	}
	if !reflect.DeepEqual(out.ToolNames, []string{"retrieve_code", "load_evidence_ref"}) {
		t.Fatalf("tool names = %v", out.ToolNames)
	}
	if out.ParentTotalTokens != 24296 {
		t.Fatalf("parent tokens = %d", out.ParentTotalTokens)
	}
	if len(out.StdoutLogs) != 1 || !strings.Contains(out.StdoutLogs[0], "[CONTEXT]") {
		t.Fatalf("stdout logs = %v", out.StdoutLogs)
	}
}

func TestExecuteRLMQueryRequiresPrompt(t *testing.T) {
	_, err := executeRLMQuery(context.Background(), "/repo", Input{Workspace: "."}, nil)
	if err == nil {
		t.Fatal("expected error for missing prompt")
	}
	if !strings.Contains(err.Error(), "prompt is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestExecuteRLMQueryRequiresExplicitWorkspace(t *testing.T) {
	called := false
	runner := func(_ context.Context, _, _ string, _ []string, _ ...string) executil.CmdResult {
		called = true
		return executil.CmdResult{}
	}

	_, err := executeRLMQuery(context.Background(), "/repo", Input{Prompt: "find symbol"}, runner)
	if err == nil {
		t.Fatal("expected error for missing workspace")
	}
	if !strings.Contains(err.Error(), "workspace is required") {
		t.Fatalf("error = %v", err)
	}
	if called {
		t.Fatal("runner was called despite missing workspace")
	}
}

func TestExecuteRLMQueryAcceptsWorkspaceRootAlias(t *testing.T) {
	var gotDir string
	runner := func(_ context.Context, dir, _ string, _ []string, _ ...string) executil.CmdResult {
		gotDir = dir
		return executil.CmdResult{
			Stdout: []byte(`{"version":1,"status":"ok","command":"rlm/run","data":{"mode":"inspect","result":{"answer":"ok"}},"meta":{"ts":"2026-05-07T00:00:00Z"},"error":{}}`),
		}
	}

	out, err := executeRLMQuery(context.Background(), "/repo", Input{Prompt: "find symbol", WorkspaceRoot: "."}, runner)
	if err != nil {
		t.Fatalf("executeRLMQuery returned error: %v", err)
	}
	if gotDir != "/repo" {
		t.Fatalf("runner dir = %q, want /repo", gotDir)
	}
	if out.Answer != "ok" {
		t.Fatalf("answer = %q, want ok", out.Answer)
	}
}

func TestExecuteRLMQueryReportsSubprocessFailureWithoutEnvelope(t *testing.T) {
	runner := func(_ context.Context, _, _ string, _ []string, _ ...string) executil.CmdResult {
		return executil.CmdResult{
			ExitCode: 1,
			Err:      errors.New("exit status 1"),
			Stderr:   []byte("auth mode requires an API key"),
		}
	}

	_, err := executeRLMQuery(context.Background(), "/repo", Input{Prompt: "find symbol", Workspace: "."}, runner)
	if err == nil {
		t.Fatal("expected subprocess error")
	}
	if !strings.Contains(err.Error(), "run rlm query") {
		t.Fatalf("error = %v", err)
	}
}

func TestNormalizeInputAllowsRequireToolUseFalse(t *testing.T) {
	requireToolUse := false
	in := Input{
		Prompt:         "find symbol",
		Workspace:      ".",
		RequireToolUse: &requireToolUse,
	}
	workspace, err := normalizeInput("/repo", &in)
	if err != nil {
		t.Fatalf("normalizeInput returned error: %v", err)
	}
	args := buildRLMArgs(workspace, in)
	if !contains(args, "--require-tool-use=false") {
		t.Fatalf("args missing require-tool-use=false: %v", args)
	}
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func containsArgPair(args []string, key, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}
