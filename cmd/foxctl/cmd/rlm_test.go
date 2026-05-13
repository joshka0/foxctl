package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/rlm"
	rlmenv "github.com/joshka0/foxctl/internal/rlm/env"
	"github.com/joshka0/foxctl/internal/rlm/optdata"
	rlmruntime "github.com/joshka0/foxctl/internal/rlm/runtime"
)

func TestRLMRunCommandBootstrapsEnvironment(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	var cfg config.Config
	cfg.Storage.Root = t.TempDir()

	cmd := newRLMRunCommand()
	cmd.SetArgs([]string{
		"--prompt", "inspect auth flow",
		"--workspace", workspace,
	})
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (stderr=%s)", err, stderr.String())
	}
	env, err := protocol.DecodeEnvelope(stdout.Bytes())
	if err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Status != "ok" {
		t.Fatalf("status=%q", env.Status)
	}
	raw, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var payload struct {
		Mode   string `json:"mode"`
		Result struct {
			Answer string `json:"answer"`
		} `json:"result"`
		Environment struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"environment"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Mode != "inspect" {
		t.Fatalf("mode=%q", payload.Mode)
	}
	if payload.Result.Answer == "" {
		t.Fatalf("expected non-empty answer")
	}
	if payload.Result.Answer == "bootstrap complete; recursive execution backend not implemented yet" {
		t.Fatalf("still returning placeholder answer")
	}
	if len(payload.Environment.Tools) == 0 {
		t.Fatalf("expected non-empty tool surface")
	}
}

func TestNormalizedRLMExecutorRepl(t *testing.T) {
	t.Parallel()

	if got := normalizedRLMExecutor("repl"); got != "repl" {
		t.Fatalf("mode=%q want repl", got)
	}
}

func TestChooseRLMRunnerReplWithEphemeralSkills(t *testing.T) {
	t.Parallel()

	runner := chooseRLMRunner(
		"repl",
		rlmenv.NewReadOnlyAdapter(config.Config{}, "", "", nil, rlm.Environment{}),
		rlm.Task{Prompt: "solve", MaxIterations: 1, MaxDepth: 1, MaxChildren: 3, MaxConcurrent: 2, MaxTotalNodes: 4},
		rlm.Environment{},
		"lmstudio",
		"model",
		"http://localhost:1234/v1",
		"",
		0,
		true,
		"",
		"",
		"",
		string(rlmruntime.SandboxKindPython),
		true,
		true,
	)
	replRunner, ok := runner.(*rlmruntime.REPLRunner)
	if !ok {
		t.Fatalf("runner type=%T want *REPLRunner", runner)
	}
	if replRunner.Config.EphemeralSkills {
		t.Fatal("EphemeralSkills=true")
	}
	if replRunner.Config.HelperFactory == nil {
		t.Fatal("HelperFactory=nil")
	}
	if replRunner.Config.RecursionPolicy != rlmruntime.RecursionPolicyOptional {
		t.Fatalf("RecursionPolicy=%q", replRunner.Config.RecursionPolicy)
	}
	if replRunner.Config.RLMQueryFactory == nil {
		t.Fatal("RLMQueryFactory=nil")
	}
	if !replRunner.Config.AsyncRecursion {
		t.Fatal("AsyncRecursion=false")
	}
	if replRunner.Config.Budget.MaxChildren != 3 || replRunner.Config.Budget.MaxConcurrent != 2 || replRunner.Config.Budget.MaxTotalNodes != 4 {
		t.Fatalf("budget=%+v, want explicit async tree budget", replRunner.Config.Budget)
	}
	if !replRunner.Config.ExtractSolutionLine {
		t.Fatal("ExtractSolutionLine=false")
	}
	if !strings.Contains(replRunner.Config.SystemPrompt, rlmruntime.EphemeralHelperSolveToolName) {
		t.Fatalf("system prompt missing helper-solve instructions:\n%s", replRunner.Config.SystemPrompt)
	}
	if strings.Contains(replRunner.Config.SystemPrompt, "ephemeral_skill_draft") {
		t.Fatalf("system prompt still references ephemeral skill draft surface:\n%s", replRunner.Config.SystemPrompt)
	}
}

func TestRLMRunCommandCarriesAsyncTreeBudgetFlags(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	var cfg config.Config
	cfg.Storage.Root = t.TempDir()

	cmd := newRLMRunCommand()
	cmd.SetArgs([]string{
		"--prompt", "inspect auth flow",
		"--workspace", workspace,
		"--max-depth", "3",
		"--max-subcalls", "5",
		"--max-children", "4",
		"--max-concurrent", "2",
		"--max-total-nodes", "6",
	})
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (stderr=%s)", err, stderr.String())
	}
	env, err := protocol.DecodeEnvelope(stdout.Bytes())
	if err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	raw, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var payload struct {
		Task struct {
			MaxDepth      int `json:"max_depth"`
			MaxSubcalls   int `json:"max_subcalls"`
			MaxChildren   int `json:"max_children"`
			MaxConcurrent int `json:"max_concurrent"`
			MaxTotalNodes int `json:"max_total_nodes"`
		} `json:"task"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Task.MaxDepth != 3 || payload.Task.MaxSubcalls != 5 || payload.Task.MaxChildren != 4 || payload.Task.MaxConcurrent != 2 || payload.Task.MaxTotalNodes != 6 {
		t.Fatalf("task budget=%+v, want CLI-provided async tree budget", payload.Task)
	}
}

func TestRLMRunCommandAppendsOptimizerTraceJSONL(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tracePath := filepath.Join(t.TempDir(), "optimizer-trace.jsonl")
	var cfg config.Config
	cfg.Storage.Root = t.TempDir()

	cmd := newRLMRunCommand()
	cmd.SetArgs([]string{
		"--prompt", "inspect auth flow",
		"--workspace", workspace,
		"--opt-trace-out", tracePath,
	})
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (stderr=%s)", err, stderr.String())
	}

	traceBody, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("read trace output: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(traceBody)), "\n")
	if len(lines) != 1 {
		t.Fatalf("trace line count=%d want 1", len(lines))
	}

	var record optdata.TrajectoryRecord
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("decode trace record: %v", err)
	}
	if record.RecordType != optdata.RecordTypeTrajectoryV1 {
		t.Fatalf("record_type=%q", record.RecordType)
	}
	if record.Execution.Runtime != "rlm" {
		t.Fatalf("runtime=%q", record.Execution.Runtime)
	}
	if record.Execution.Mode != "inspect" {
		t.Fatalf("mode=%q", record.Execution.Mode)
	}
	if record.Labels["tool_profile"] != rlmenv.ToolProfileDefault {
		t.Fatalf("tool profile=%q", record.Labels["tool_profile"])
	}
	if record.Prompt.User != "inspect auth flow" {
		t.Fatalf("prompt user=%q", record.Prompt.User)
	}
	if !record.Execution.Success {
		t.Fatalf("expected successful trace execution")
	}
	if len(record.Prompt.ContextBlocks) == 0 || strings.TrimSpace(record.Prompt.ContextBlocks[0].Content) != "inspect auth flow" {
		t.Fatalf("missing task prompt context: %+v", record.Prompt.ContextBlocks)
	}

	env, err := protocol.DecodeEnvelope(stdout.Bytes())
	if err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	raw, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	var payload struct {
		OptimizerTrace struct {
			Path string `json:"path"`
		} `json:"optimizer_trace"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.OptimizerTrace.Path == "" {
		t.Fatalf("optimizer_trace.path missing")
	}
}

func TestRLMRunCommandRejectsMismatchedTraceFlags(t *testing.T) {
	t.Parallel()

	cmd := newRLMRunCommand()
	cmd.SetArgs([]string{
		"--prompt", "inspect auth flow",
		"--opt-trace-out", filepath.Join(t.TempDir(), "a.jsonl"),
		"--trajectory-out", filepath.Join(t.TempDir(), "b.jsonl"),
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--opt-trace-out and --trajectory-out must match") {
		t.Fatalf("error=%v", err)
	}
}

func TestRLMRunCommandTrajectoryOutAlias(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tracePath := filepath.Join(t.TempDir(), "trajectory-trace.jsonl")
	var cfg config.Config
	cfg.Storage.Root = t.TempDir()

	cmd := newRLMRunCommand()
	cmd.SetArgs([]string{
		"--prompt", "inspect auth flow",
		"--workspace", workspace,
		"--trajectory-out", tracePath,
	})
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, err := os.Stat(tracePath); err != nil {
		t.Fatalf("trajectory-out file missing: %v", err)
	}
}
