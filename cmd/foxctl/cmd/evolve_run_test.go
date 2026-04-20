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
	"github.com/joshka0/foxctl/internal/tooling/evolve/store"
)

func TestEvolveRunPersistsAttemptAndScore(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	_, rootID := runEvolveInitWithBenchmarkForTest(t, ctx, workspacePath, "sh -lc 'echo score=42.25'")

	runCmd := newEvolveRunCommand()
	runCmd.SetContext(ctx)
	out := &bytes.Buffer{}
	runCmd.SetOut(out)
	runCmd.SetErr(&bytes.Buffer{})
	runCmd.SetArgs([]string{"--workspace", workspacePath})
	if err := runCmd.Execute(); err != nil {
		t.Fatalf("evolve run: %v", err)
	}

	env := decodeEnvelope(t, out.Bytes())
	data := mustMap(t, env["data"])
	if got := mustString(t, data["status"]); got != string(model.NodeStatusEvaluated) {
		t.Fatalf("status = %s, want %s", got, model.NodeStatusEvaluated)
	}
	if got := mustString(t, data["attempt_status"]); got != string(model.AttemptStatusCompleted) {
		t.Fatalf("attempt_status = %s, want %s", got, model.AttemptStatusCompleted)
	}
	if got := mustFloat64(t, data["score"]); got != 42.25 {
		t.Fatalf("score = %v, want 42.25", got)
	}
	artifacts := mustMap(t, data["artifacts"])
	if strings.TrimSpace(mustString(t, artifacts["benchmark"])) == "" {
		t.Fatalf("benchmark artifact is empty")
	}

	st := openEvolveStoreForTest(t, ctx, cfg.Storage.Root)
	defer func() { _ = st.Close() }()
	attempts, err := st.AttemptsByNode(ctx, rootID)
	if err != nil {
		t.Fatalf("attempts by node: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("attempt count = %d, want 1", len(attempts))
	}
	if attempts[0].Status != model.AttemptStatusCompleted {
		t.Fatalf("attempt status = %s, want %s", attempts[0].Status, model.AttemptStatusCompleted)
	}
	if attempts[0].Score == nil || *attempts[0].Score != 42.25 {
		t.Fatalf("attempt score = %#v, want 42.25", attempts[0].Score)
	}
	node, err := st.Node(ctx, rootID)
	if err != nil {
		t.Fatalf("load node: %v", err)
	}
	if node.Status != model.NodeStatusEvaluated {
		t.Fatalf("node status = %s, want %s", node.Status, model.NodeStatusEvaluated)
	}
	if node.CurrentAttempt != 1 || node.EvaluatedAttempts != 1 {
		t.Fatalf("node attempts current=%d evaluated=%d, want 1/1", node.CurrentAttempt, node.EvaluatedAttempts)
	}
}

func TestEvolveRunReturnsEParseWhenScoreMissing(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	_, rootID := runEvolveInitWithBenchmarkForTest(t, ctx, workspacePath, "sh -lc 'echo no-score-here'")

	runCmd := newEvolveRunCommand()
	runCmd.SetContext(ctx)
	out := &bytes.Buffer{}
	runCmd.SetOut(out)
	runCmd.SetErr(&bytes.Buffer{})
	runCmd.SetArgs([]string{"--workspace", workspacePath})
	err := runCmd.Execute()
	if err == nil {
		t.Fatalf("expected evolve run to fail when score is missing")
	}

	env := decodeEnvelope(t, bytes.SplitN(out.Bytes(), []byte("\n"), 2)[0])
	if status := mustString(t, env["status"]); status != "error" {
		t.Fatalf("status = %s, want error", status)
	}
	errMap := mustMap(t, env["error"])
	if code := mustString(t, errMap["code"]); code != "EPARSE" {
		t.Fatalf("error.code = %s, want EPARSE", code)
	}

	st := openEvolveStoreForTest(t, ctx, cfg.Storage.Root)
	defer func() { _ = st.Close() }()
	attempts, loadErr := st.AttemptsByNode(ctx, rootID)
	if loadErr != nil {
		t.Fatalf("attempts by node: %v", loadErr)
	}
	if len(attempts) != 1 || attempts[0].Status != model.AttemptStatusFailed {
		t.Fatalf("attempts = %#v, want one failed attempt", attempts)
	}
	node, loadErr := st.Node(ctx, rootID)
	if loadErr != nil {
		t.Fatalf("load node: %v", loadErr)
	}
	if node.Status != model.NodeStatusFailed {
		t.Fatalf("node status = %s, want %s", node.Status, model.NodeStatusFailed)
	}
}

func TestEvolveRunReturnsERuntimeWhenBenchmarkFails(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	_, rootID := runEvolveInitWithBenchmarkForTest(t, ctx, workspacePath, "sh -lc 'echo boom >&2; exit 7'")

	runCmd := newEvolveRunCommand()
	runCmd.SetContext(ctx)
	out := &bytes.Buffer{}
	runCmd.SetOut(out)
	runCmd.SetErr(&bytes.Buffer{})
	runCmd.SetArgs([]string{"--workspace", workspacePath})
	err := runCmd.Execute()
	if err == nil {
		t.Fatalf("expected evolve run to fail for benchmark non-zero exit")
	}

	env := decodeEnvelope(t, bytes.SplitN(out.Bytes(), []byte("\n"), 2)[0])
	if status := mustString(t, env["status"]); status != "error" {
		t.Fatalf("status = %s, want error", status)
	}
	errMap := mustMap(t, env["error"])
	if code := mustString(t, errMap["code"]); code != "ERUNTIME" {
		t.Fatalf("error.code = %s, want ERUNTIME", code)
	}

	st := openEvolveStoreForTest(t, ctx, cfg.Storage.Root)
	defer func() { _ = st.Close() }()
	attempts, loadErr := st.AttemptsByNode(ctx, rootID)
	if loadErr != nil {
		t.Fatalf("attempts by node: %v", loadErr)
	}
	if len(attempts) != 1 || attempts[0].Status != model.AttemptStatusFailed {
		t.Fatalf("attempts = %#v, want one failed attempt", attempts)
	}
	if !strings.Contains(attempts[0].Error, "exit code 7") {
		t.Fatalf("attempt error = %q, want exit code 7", attempts[0].Error)
	}
}

func TestInheritEvolveGatesRootFirstWithNearestOverride(t *testing.T) {
	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	nodes := []model.Node{
		{ID: "root", RunID: "run-1", Status: model.NodeStatusRoot, CreatedAt: now, UpdatedAt: now},
		{ID: "child", RunID: "run-1", ParentID: "root", Status: model.NodeStatusPending, CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute)},
		{ID: "leaf", RunID: "run-1", ParentID: "child", Status: model.NodeStatusPending, CreatedAt: now.Add(2 * time.Minute), UpdatedAt: now.Add(2 * time.Minute)},
	}
	gates := []model.Gate{
		{ID: "g1", RunID: "run-1", NodeID: "root", Name: "unit", Command: "root-unit", CreatedAt: now},
		{ID: "g2", RunID: "run-1", NodeID: "root", Name: "lint", Command: "root-lint", CreatedAt: now.Add(time.Second)},
		{ID: "g3", RunID: "run-1", NodeID: "child", Name: "unit", Command: "child-unit", CreatedAt: now.Add(2 * time.Second)},
		{ID: "g4", RunID: "run-1", NodeID: "leaf", Name: "fmt", Command: "leaf-fmt", CreatedAt: now.Add(3 * time.Second)},
	}

	merged, err := inheritEvolveGates(nodes[2], nodes, gates)
	if err != nil {
		t.Fatalf("inherit gates: %v", err)
	}
	if len(merged) != 3 {
		t.Fatalf("merged gates len = %d, want 3", len(merged))
	}
	if merged[0].Gate.Name != "unit" || merged[0].SourceNodeID != "child" || merged[0].Gate.Command != "child-unit" {
		t.Fatalf("merged[0] = %#v, want child unit override", merged[0])
	}
	if merged[1].Gate.Name != "lint" || merged[1].SourceNodeID != "root" {
		t.Fatalf("merged[1] = %#v, want root lint", merged[1])
	}
	if merged[2].Gate.Name != "fmt" || merged[2].SourceNodeID != "leaf" {
		t.Fatalf("merged[2] = %#v, want leaf fmt", merged[2])
	}
}

func TestEvolveRunExecutesInheritedGatesWithOverride(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runID, rootID := runEvolveInitWithBenchmarkForTest(t, ctx, workspacePath, "sh -lc 'echo score=7.5'")
	st := openEvolveStoreForTest(t, ctx, cfg.Storage.Root)
	defer func() { _ = st.Close() }()

	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	child := model.Node{
		ID:           "child-node",
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
	gates := []model.Gate{
		{ID: "gate-root-unit", RunID: runID, NodeID: rootID, Name: "unit", Command: "sh -lc 'exit 1'", CreatedAt: now},
		{ID: "gate-root-lint", RunID: runID, NodeID: rootID, Name: "lint", Command: "sh -lc 'exit 0'", CreatedAt: now.Add(time.Second)},
		{ID: "gate-child-unit", RunID: runID, NodeID: child.ID, Name: "unit", Command: "sh -lc 'exit 0'", CreatedAt: now.Add(2 * time.Second)},
	}
	for _, gate := range gates {
		if err := st.SaveGate(ctx, gate); err != nil {
			t.Fatalf("save gate %s: %v", gate.ID, err)
		}
	}

	runCmd := newEvolveRunCommand()
	runCmd.SetContext(ctx)
	out := &bytes.Buffer{}
	runCmd.SetOut(out)
	runCmd.SetErr(&bytes.Buffer{})
	runCmd.SetArgs([]string{"--workspace", workspacePath, "--node", child.ID})
	if err := runCmd.Execute(); err != nil {
		t.Fatalf("evolve run with gates: %v", err)
	}

	env := decodeEnvelope(t, out.Bytes())
	data := mustMap(t, env["data"])
	if got := mustString(t, data["attempt_status"]); got != string(model.AttemptStatusCompleted) {
		t.Fatalf("attempt_status = %s, want %s", got, model.AttemptStatusCompleted)
	}
	gateSummary := mustMap(t, data["gate_result_summary"])
	if got := int(mustFloat64(t, gateSummary["total"])); got != 2 {
		t.Fatalf("gate total = %d, want 2", got)
	}
	if got := int(mustFloat64(t, gateSummary["passed"])); got != 2 {
		t.Fatalf("gate passed = %d, want 2", got)
	}

	attemptID := mustString(t, data["attempt_id"])
	results, err := st.GateResultsByAttempt(ctx, attemptID)
	if err != nil {
		t.Fatalf("gate results by attempt: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("gate results len = %d, want 2", len(results))
	}

	var (
		foundUnit bool
		foundLint bool
	)
	for _, result := range results {
		switch result.GateName {
		case "unit":
			foundUnit = true
			if result.SourceNodeID != child.ID || !result.Passed {
				t.Fatalf("unit gate result = %#v, want child override pass", result)
			}
		case "lint":
			foundLint = true
			if result.SourceNodeID != rootID || !result.Passed {
				t.Fatalf("lint gate result = %#v, want root pass", result)
			}
		}
	}
	if !foundUnit || !foundLint {
		t.Fatalf("gate names missing, unit=%v lint=%v", foundUnit, foundLint)
	}
}

func runEvolveInitWithBenchmarkForTest(t *testing.T, ctx context.Context, workspacePath, benchmark string) (string, string) {
	t.Helper()
	initCmd := newEvolveInitCommand()
	initCmd.SetContext(ctx)
	out := &bytes.Buffer{}
	initCmd.SetOut(out)
	initCmd.SetErr(&bytes.Buffer{})
	initCmd.SetArgs([]string{
		"--workspace", workspacePath,
		"--target", ".",
		"--benchmark", benchmark,
		"--metric", "max",
	})
	if err := initCmd.Execute(); err != nil {
		t.Fatalf("evolve init: %v", err)
	}
	data := mustMap(t, mustMap(t, decodeEnvelope(t, out.Bytes())["data"]))
	return mustString(t, data["run_id"]), mustString(t, data["root_node_id"])
}

func openEvolveStoreForTest(t *testing.T, ctx context.Context, storageRoot string) *store.SQLStore {
	t.Helper()
	st, err := store.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("open evolve store: %v", err)
	}
	return st
}
