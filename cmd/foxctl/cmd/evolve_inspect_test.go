package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/tooling/evolve/model"
)

func TestEvolveFrontierReturnsDeterministicNodes(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runID := seedEvolveRunForStatusTree(t, ctx, cfg.Storage.Root, workspacePath)

	cmd := newEvolveFrontierCommand()
	cmd.SetContext(ctx)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--workspace", workspacePath, "--run", runID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("evolve frontier: %v", err)
	}

	env := decodeEnvelope(t, out.Bytes())
	data := mustMap(t, env["data"])
	if got := mustString(t, data["run_id"]); got != runID {
		t.Fatalf("run_id = %s, want %s", got, runID)
	}
	if got := int(mustFloat64(t, data["count"])); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}

	nodes := mustSlice(t, data["nodes"])
	if len(nodes) != 2 {
		t.Fatalf("nodes len = %d, want 2", len(nodes))
	}
	first := mustMap(t, nodes[0])
	second := mustMap(t, nodes[1])
	if mustString(t, first["id"]) != "child-early" || mustString(t, second["id"]) != "child-late" {
		t.Fatalf("frontier order = [%s, %s], want [child-early, child-late]", mustString(t, first["id"]), mustString(t, second["id"]))
	}
}

func TestEvolveGetReturnsRecentAttemptsAndGateResults(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runID, rootID := runEvolveInitWithBenchmarkForTest(t, ctx, workspacePath, "sh -lc 'echo score=1.0'")
	st := openEvolveStoreForTest(t, ctx, cfg.Storage.Root)
	defer func() { _ = st.Close() }()

	now := time.Date(2026, 4, 16, 13, 0, 0, 0, time.UTC)
	gate := model.Gate{
		ID:        "gate-lint",
		RunID:     runID,
		NodeID:    rootID,
		Name:      "lint",
		Command:   "sh -lc 'exit 0'",
		CreatedAt: now,
	}
	if err := st.SaveGate(ctx, gate); err != nil {
		t.Fatalf("save gate: %v", err)
	}

	score := 9.5
	attempt1 := model.Attempt{
		ID:                "attempt-1",
		NodeID:            rootID,
		AttemptNo:         1,
		Status:            model.AttemptStatusCompleted,
		Score:             &score,
		BenchmarkArtifact: "sha256:first",
		StartedAt:         now,
		FinishedAt:        now.Add(1 * time.Minute),
	}
	attempt2 := model.Attempt{
		ID:                "attempt-2",
		NodeID:            rootID,
		AttemptNo:         2,
		Status:            model.AttemptStatusFailed,
		BenchmarkArtifact: "sha256:second",
		Error:             "lint failed",
		StartedAt:         now.Add(2 * time.Minute),
		FinishedAt:        now.Add(3 * time.Minute),
	}
	if err := st.SaveAttempt(ctx, attempt1); err != nil {
		t.Fatalf("save attempt1: %v", err)
	}
	if err := st.SaveAttempt(ctx, attempt2); err != nil {
		t.Fatalf("save attempt2: %v", err)
	}
	if err := st.SaveGateResult(ctx, model.GateResult{
		AttemptID:    attempt2.ID,
		GateName:     "lint",
		SourceNodeID: rootID,
		Passed:       false,
	}); err != nil {
		t.Fatalf("save gate result: %v", err)
	}

	cmd := newEvolveGetCommand()
	cmd.SetContext(ctx)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--workspace", workspacePath, rootID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("evolve get: %v", err)
	}

	env := decodeEnvelope(t, out.Bytes())
	data := mustMap(t, env["data"])
	if got := mustString(t, data["run_id"]); got != runID {
		t.Fatalf("run_id = %s, want %s", got, runID)
	}
	if got := mustString(t, data["node_id"]); got != rootID {
		t.Fatalf("node_id = %s, want %s", got, rootID)
	}

	nodeGates := mustSlice(t, data["node_gates"])
	if len(nodeGates) != 1 || mustString(t, mustMap(t, nodeGates[0])["name"]) != "lint" {
		t.Fatalf("node_gates = %#v, want one lint gate", nodeGates)
	}

	recentAttempts := mustMap(t, data["recent_attempts"])
	attempts := mustSlice(t, recentAttempts["attempts"])
	if len(attempts) != 2 {
		t.Fatalf("attempts len = %d, want 2", len(attempts))
	}
	first := mustMap(t, attempts[0])
	second := mustMap(t, attempts[1])
	if mustString(t, first["id"]) != "attempt-2" || mustString(t, second["id"]) != "attempt-1" {
		t.Fatalf("attempt order = [%s, %s], want [attempt-2, attempt-1]", mustString(t, first["id"]), mustString(t, second["id"]))
	}

	firstGateResults := mustSlice(t, first["gate_results"])
	if len(firstGateResults) != 1 {
		t.Fatalf("first attempt gate_results len = %d, want 1", len(firstGateResults))
	}
	if got := mustString(t, mustMap(t, firstGateResults[0])["gate_name"]); got != "lint" {
		t.Fatalf("gate_name = %s, want lint", got)
	}
}

func TestEvolvePathResolvesRootToWorkspace(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	_, rootID := runEvolveInitWithBenchmarkForTest(t, ctx, workspacePath, "sh -lc 'echo score=1.0'")

	cmd := newEvolvePathCommand()
	cmd.SetContext(ctx)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--workspace", workspacePath, "--node", rootID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("evolve path: %v", err)
	}

	env := decodeEnvelope(t, out.Bytes())
	data := mustMap(t, env["data"])
	resolvedWorkspace, err := filepath.Abs(workspacePath)
	if err != nil {
		t.Fatalf("abs workspace: %v", err)
	}
	if got := mustString(t, data["execution_path"]); got != resolvedWorkspace {
		t.Fatalf("execution_path = %s, want %s", got, resolvedWorkspace)
	}
	if got := mustString(t, data["path_source"]); got != "workspace" {
		t.Fatalf("path_source = %s, want workspace", got)
	}
}

func TestEvolvePathMissingChildWorktreeReturnsNotFound(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runID, rootID := runEvolveInitWithBenchmarkForTest(t, ctx, workspacePath, "sh -lc 'echo score=1.0'")
	st := openEvolveStoreForTest(t, ctx, cfg.Storage.Root)
	defer func() { _ = st.Close() }()

	now := time.Date(2026, 4, 16, 14, 0, 0, 0, time.UTC)
	child := model.Node{
		ID:        "child-missing-path",
		RunID:     runID,
		ParentID:  rootID,
		Status:    model.NodeStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := st.SaveNode(ctx, child); err != nil {
		t.Fatalf("save child node: %v", err)
	}

	cmd := newEvolvePathCommand()
	cmd.SetContext(ctx)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--workspace", workspacePath, "--node", child.ID})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected evolve path to fail for child without worktree path")
	}

	env := decodeEnvelope(t, bytes.SplitN(out.Bytes(), []byte("\n"), 2)[0])
	if status := mustString(t, env["status"]); status != "error" {
		t.Fatalf("status = %s, want error", status)
	}
	errMap := mustMap(t, env["error"])
	if code := mustString(t, errMap["code"]); code != "ENOTFOUND" {
		t.Fatalf("error.code = %s, want ENOTFOUND", code)
	}
}

func TestEvolveDiffReturnsBoundedDiffAndArtifact(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := initEvolveTestRepo(t)

	runID, rootID := runEvolveInitWithBenchmarkForTest(t, ctx, workspacePath, "sh -lc 'echo score=1.0'")
	st := openEvolveStoreForTest(t, ctx, cfg.Storage.Root)
	defer func() { _ = st.Close() }()

	now := time.Date(2026, 4, 16, 15, 0, 0, 0, time.UTC)
	child := model.Node{
		ID:           "child-diff",
		RunID:        runID,
		ParentID:     rootID,
		Status:       model.NodeStatusPending,
		WorktreePath: workspacePath,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := st.SaveNode(ctx, child); err != nil {
		t.Fatalf("save child node: %v", err)
	}

	readmePath := filepath.Join(workspacePath, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Evolve Test\n\nchanged line\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}

	cmd := newEvolveDiffCommand()
	cmd.SetContext(ctx)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--workspace", workspacePath, "--node", child.ID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("evolve diff: %v", err)
	}

	env := decodeEnvelope(t, out.Bytes())
	data := mustMap(t, env["data"])
	if got := mustString(t, data["run_id"]); got != runID {
		t.Fatalf("run_id = %s, want %s", got, runID)
	}
	if got := mustString(t, data["node_id"]); got != child.ID {
		t.Fatalf("node_id = %s, want %s", got, child.ID)
	}
	if got := mustString(t, data["parent_node_id"]); got != rootID {
		t.Fatalf("parent_node_id = %s, want %s", got, rootID)
	}
	if hasChanges, ok := data["has_changes"].(bool); !ok || !hasChanges {
		t.Fatalf("has_changes = %#v, want true", data["has_changes"])
	}
	diffText := mustString(t, data["diff"])
	if !strings.Contains(diffText, "changed line") {
		t.Fatalf("diff does not contain expected changed line:\n%s", diffText)
	}
	if got := mustString(t, data["artifact"]); strings.TrimSpace(got) == "" {
		t.Fatalf("artifact is empty")
	}
}
