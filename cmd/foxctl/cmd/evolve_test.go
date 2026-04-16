package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/tooling/evolve/model"
	"github.com/joshka0/foxctl/internal/tooling/evolve/store"
)

func TestEvolveInitCreatesRunAndRootNode(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	cmd := newEvolveInitCommand()
	cmd.SetContext(ctx)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--workspace", workspacePath,
		"--target", "./pkg",
		"--benchmark", "go test ./...",
		"--metric", "max",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("evolve init: %v", err)
	}

	env := decodeEnvelope(t, out.Bytes())
	data := mustMap(t, env["data"])
	runID := mustString(t, data["run_id"])
	rootID := mustString(t, data["root_node_id"])

	st, err := store.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	resolvedWorkspace, err := filepath.Abs(workspacePath)
	if err != nil {
		t.Fatalf("abs workspace: %v", err)
	}
	activeRun, ok, err := st.ActiveRun(ctx, resolvedWorkspace)
	if err != nil {
		t.Fatalf("active run: %v", err)
	}
	if !ok {
		t.Fatalf("expected active run")
	}
	if activeRun.ID != runID {
		t.Fatalf("run id = %s, want %s", activeRun.ID, runID)
	}
	if activeRun.TargetPath != filepath.Join(resolvedWorkspace, "pkg") {
		t.Fatalf("target path = %s", activeRun.TargetPath)
	}

	root, err := st.Node(ctx, rootID)
	if err != nil {
		t.Fatalf("root node: %v", err)
	}
	if root.RunID != runID {
		t.Fatalf("root run id = %s, want %s", root.RunID, runID)
	}
	if root.Status != model.NodeStatusRoot {
		t.Fatalf("root status = %s, want %s", root.Status, model.NodeStatusRoot)
	}
}

func TestEvolveStatusReportsNoActiveRun(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	cmd := newEvolveStatusCommand()
	cmd.SetContext(ctx)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--workspace", workspacePath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("evolve status: %v", err)
	}

	env := decodeEnvelope(t, out.Bytes())
	data := mustMap(t, env["data"])
	if active, ok := data["active_run"].(bool); !ok || active {
		t.Fatalf("active_run = %#v, want false", data["active_run"])
	}
}

func TestEvolveStatusAndTreeReadModel(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	runID := seedEvolveRunForStatusTree(t, ctx, cfg.Storage.Root, workspacePath)

	statusCmd := newEvolveStatusCommand()
	statusCmd.SetContext(ctx)
	statusOut := &bytes.Buffer{}
	statusCmd.SetOut(statusOut)
	statusCmd.SetErr(&bytes.Buffer{})
	statusCmd.SetArgs([]string{"--workspace", workspacePath})
	if err := statusCmd.Execute(); err != nil {
		t.Fatalf("evolve status: %v", err)
	}
	statusEnv := decodeEnvelope(t, statusOut.Bytes())
	statusData := mustMap(t, statusEnv["data"])
	summary := mustMap(t, statusData["summary"])
	if got := int(mustFloat64(t, summary["total_nodes"])); got != 4 {
		t.Fatalf("total_nodes = %d, want 4", got)
	}
	if got := int(mustFloat64(t, summary["frontier_count"])); got != 2 {
		t.Fatalf("frontier_count = %d, want 2", got)
	}
	frontier := mustSlice(t, summary["frontier_node_ids"])
	if len(frontier) != 2 || mustString(t, frontier[0]) != "child-early" || mustString(t, frontier[1]) != "child-late" {
		t.Fatalf("frontier ids = %#v", frontier)
	}
	counts := mustSlice(t, summary["node_counts"])
	gotCounts := map[string]int{}
	for _, item := range counts {
		bucket := mustMap(t, item)
		gotCounts[mustString(t, bucket["status"])] = int(mustFloat64(t, bucket["count"]))
	}
	if gotCounts[string(model.NodeStatusRoot)] != 1 || gotCounts[string(model.NodeStatusPending)] != 1 ||
		gotCounts[string(model.NodeStatusFailed)] != 1 || gotCounts[string(model.NodeStatusCommitted)] != 1 {
		t.Fatalf("node counts = %#v", gotCounts)
	}

	treeCmd := newEvolveTreeCommand()
	treeCmd.SetContext(ctx)
	treeOut := &bytes.Buffer{}
	treeCmd.SetOut(treeOut)
	treeCmd.SetErr(&bytes.Buffer{})
	treeCmd.SetArgs([]string{"--workspace", workspacePath, "--run", runID})
	if err := treeCmd.Execute(); err != nil {
		t.Fatalf("evolve tree: %v", err)
	}
	treeEnv := decodeEnvelope(t, treeOut.Bytes())
	treeData := mustMap(t, treeEnv["data"])
	tree := mustMap(t, treeData["tree"])
	rendered := mustString(t, tree["rendered"])
	wantRendered := "root [root]\n  child-early [pending]\n  child-late [failed]\n  child-done [committed] score=12.5000"
	if rendered != wantRendered {
		t.Fatalf("rendered tree:\n%s\nwant:\n%s", rendered, wantRendered)
	}
}

func TestResolveEvolveTargetPathRejectsRelativeEscape(t *testing.T) {
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	_, err := resolveEvolveTargetPath(workspacePath, "../outside")
	if err == nil {
		t.Fatalf("expected error for relative escape target")
	}
	if !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("error = %v, want escape error", err)
	}
}

func TestResolveEvolveTargetPathRejectsAbsoluteEscape(t *testing.T) {
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	outsidePath := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outsidePath, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}

	_, err := resolveEvolveTargetPath(workspacePath, outsidePath)
	if err == nil {
		t.Fatalf("expected error for absolute escape target")
	}
	if !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("error = %v, want escape error", err)
	}
}

func seedEvolveRunForStatusTree(t *testing.T, ctx context.Context, storageRoot, workspacePath string) string {
	t.Helper()
	st, err := store.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	resolvedWorkspace, err := filepath.Abs(workspacePath)
	if err != nil {
		t.Fatalf("abs workspace: %v", err)
	}
	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	run := model.Run{
		ID:               "run-status-tree",
		WorkspacePath:    resolvedWorkspace,
		TargetPath:       filepath.Join(resolvedWorkspace, "pkg"),
		BenchmarkCommand: "go test ./...",
		Metric:           model.MetricMax,
		Status:           model.RunStatusActive,
		Active:           true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := st.SaveRun(ctx, run); err != nil {
		t.Fatalf("save run: %v", err)
	}

	score := 12.5
	nodes := []model.Node{
		{
			ID:           "root",
			RunID:        run.ID,
			Status:       model.NodeStatusRoot,
			WorktreePath: resolvedWorkspace,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			ID:        "child-late",
			RunID:     run.ID,
			ParentID:  "root",
			Status:    model.NodeStatusFailed,
			CreatedAt: now.Add(3 * time.Minute),
			UpdatedAt: now.Add(3 * time.Minute),
		},
		{
			ID:        "child-early",
			RunID:     run.ID,
			ParentID:  "root",
			Status:    model.NodeStatusPending,
			CreatedAt: now.Add(1 * time.Minute),
			UpdatedAt: now.Add(1 * time.Minute),
		},
		{
			ID:        "child-done",
			RunID:     run.ID,
			ParentID:  "root",
			Status:    model.NodeStatusCommitted,
			Score:     &score,
			CreatedAt: now.Add(4 * time.Minute),
			UpdatedAt: now.Add(4 * time.Minute),
		},
	}
	for _, node := range nodes {
		if err := st.SaveNode(ctx, node); err != nil {
			t.Fatalf("save node %s: %v", node.ID, err)
		}
	}
	return run.ID
}

func testEvolveConfig(t *testing.T) config.Config {
	t.Helper()
	home := t.TempDir()
	return config.Config{
		Home: home,
		Paths: config.Paths{
			CAS:           filepath.Join(home, "cas"),
			Jobs:          filepath.Join(home, "jobs"),
			Cache:         filepath.Join(home, "cache"),
			Observability: filepath.Join(home, "observability"),
		},
		Storage: config.StorageSettings{
			Root: filepath.Join(home, "storage"),
		},
		Memory: config.MemorySettings{
			AutoLoadWorkspace: true,
		},
	}
}

func decodeEnvelope(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope: %v\nraw=%s", err, raw)
	}
	return env
}

func mustMap(t *testing.T, v any) map[string]any {
	t.Helper()
	out, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", v)
	}
	return out
}

func mustSlice(t *testing.T, v any) []any {
	t.Helper()
	out, ok := v.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", v)
	}
	return out
}

func mustString(t *testing.T, v any) string {
	t.Helper()
	out, ok := v.(string)
	if !ok {
		t.Fatalf("expected string, got %T", v)
	}
	return out
}

func mustFloat64(t *testing.T, v any) float64 {
	t.Helper()
	out, ok := v.(float64)
	if !ok {
		t.Fatalf("expected float64, got %T", v)
	}
	return out
}
