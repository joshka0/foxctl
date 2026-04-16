package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
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

func TestEvolveNewCreatesChildNodeAndWorktree(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := initEvolveTestRepo(t)

	initCmd := newEvolveInitCommand()
	initCmd.SetContext(ctx)
	initOut := &bytes.Buffer{}
	initCmd.SetOut(initOut)
	initCmd.SetErr(&bytes.Buffer{})
	initCmd.SetArgs([]string{
		"--workspace", workspacePath,
		"--target", ".",
		"--benchmark", "go test ./...",
		"--metric", "max",
	})
	if err := initCmd.Execute(); err != nil {
		t.Fatalf("evolve init: %v", err)
	}
	initData := mustMap(t, mustMap(t, decodeEnvelope(t, initOut.Bytes())["data"]))
	runID := mustString(t, initData["run_id"])
	rootID := mustString(t, initData["root_node_id"])

	newCmd := newEvolveNewCommand()
	newCmd.SetContext(ctx)
	newCmd.SilenceUsage = true
	newOut := &bytes.Buffer{}
	newCmd.SetOut(newOut)
	newCmd.SetErr(&bytes.Buffer{})
	newCmd.SetArgs([]string{
		"--workspace", workspacePath,
		"--parent", rootID,
		"--hypothesis", "test child branch",
	})
	if err := newCmd.Execute(); err != nil {
		t.Fatalf("evolve new: %v", err)
	}
	newData := mustMap(t, mustMap(t, decodeEnvelope(t, newOut.Bytes())["data"]))
	nodeID := mustString(t, newData["node_id"])
	branch := mustString(t, newData["branch"])
	worktreePath := mustString(t, newData["worktree_path"])

	if got := mustString(t, newData["run_id"]); got != runID {
		t.Fatalf("run_id = %s, want %s", got, runID)
	}
	if got := mustString(t, newData["parent_node_id"]); got != rootID {
		t.Fatalf("parent_node_id = %s, want %s", got, rootID)
	}
	if !strings.HasPrefix(branch, "foxctl/evolve/") {
		t.Fatalf("branch = %q, want foxctl/evolve/*", branch)
	}
	if got := mustString(t, newData["status"]); got != string(model.NodeStatusPending) {
		t.Fatalf("status = %s, want %s", got, model.NodeStatusPending)
	}

	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("worktree path %q does not exist: %v", worktreePath, err)
	}

	st, err := store.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	node, err := st.Node(ctx, nodeID)
	if err != nil {
		t.Fatalf("load child node: %v", err)
	}
	if node.RunID != runID {
		t.Fatalf("node run id = %s, want %s", node.RunID, runID)
	}
	if node.ParentID != rootID {
		t.Fatalf("node parent id = %s, want %s", node.ParentID, rootID)
	}
	if node.Branch != branch {
		t.Fatalf("node branch = %s, want %s", node.Branch, branch)
	}
	if node.WorktreePath != worktreePath {
		t.Fatalf("node worktree path = %s, want %s", node.WorktreePath, worktreePath)
	}
	if strings.TrimSpace(node.CommitSHA) == "" {
		t.Fatalf("node commit_sha is empty")
	}
}

func TestEvolveNewUsesParentCommitAsBaseRef(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := initEvolveTestRepo(t)
	initialHead := gitHEAD(t, workspacePath)

	initCmd := newEvolveInitCommand()
	initCmd.SetContext(ctx)
	initOut := &bytes.Buffer{}
	initCmd.SetOut(initOut)
	initCmd.SetErr(&bytes.Buffer{})
	initCmd.SetArgs([]string{
		"--workspace", workspacePath,
		"--target", ".",
		"--benchmark", "go test ./...",
		"--metric", "max",
	})
	if err := initCmd.Execute(); err != nil {
		t.Fatalf("evolve init: %v", err)
	}
	initData := mustMap(t, mustMap(t, decodeEnvelope(t, initOut.Bytes())["data"]))
	rootID := mustString(t, initData["root_node_id"])

	st, err := store.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	root, err := st.Node(ctx, rootID)
	if err != nil {
		t.Fatalf("load root: %v", err)
	}
	root.CommitSHA = initialHead
	root.UpdatedAt = time.Now().UTC()
	if err := st.SaveNode(ctx, root); err != nil {
		t.Fatalf("save root with commit_sha: %v", err)
	}

	newFile := filepath.Join(workspacePath, "second.txt")
	if err := os.WriteFile(newFile, []byte("second\n"), 0o644); err != nil {
		t.Fatalf("write second file: %v", err)
	}
	runGit(t, workspacePath, "add", "second.txt")
	runGit(t, workspacePath, "commit", "-m", "second commit")
	if head := gitHEAD(t, workspacePath); head == initialHead {
		t.Fatalf("expected new HEAD to differ from initial HEAD")
	}

	newCmd := newEvolveNewCommand()
	newCmd.SetContext(ctx)
	newCmd.SilenceUsage = true
	newOut := &bytes.Buffer{}
	newCmd.SetOut(newOut)
	newCmd.SetErr(&bytes.Buffer{})
	newCmd.SetArgs([]string{
		"--workspace", workspacePath,
		"--parent", rootID,
	})
	if err := newCmd.Execute(); err != nil {
		t.Fatalf("evolve new: %v", err)
	}
	newData := mustMap(t, mustMap(t, decodeEnvelope(t, newOut.Bytes())["data"]))
	if got := mustString(t, newData["base_ref"]); got != initialHead {
		t.Fatalf("base_ref = %s, want %s", got, initialHead)
	}
	if got := mustString(t, newData["commit_sha"]); got != initialHead {
		t.Fatalf("commit_sha = %s, want %s", got, initialHead)
	}
}

func TestEvolveNewRejectsParentFromDifferentRun(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := initEvolveTestRepo(t)

	run1Root := runEvolveInitForTest(t, ctx, workspacePath)
	run2Root := runEvolveInitForTest(t, ctx, workspacePath)
	if run1Root == run2Root {
		t.Fatalf("expected different root ids for separate runs")
	}

	newCmd := newEvolveNewCommand()
	newCmd.SetContext(ctx)
	newOut := &bytes.Buffer{}
	newCmd.SetOut(newOut)
	newCmd.SetErr(&bytes.Buffer{})
	newCmd.SetArgs([]string{
		"--workspace", workspacePath,
		"--parent", run1Root,
	})
	err := newCmd.Execute()
	if err == nil {
		t.Fatalf("expected evolve new to fail for parent from different run")
	}
	if !strings.Contains(err.Error(), "does not belong to run") {
		t.Fatalf("error = %v, want run mismatch", err)
	}
	raw := newOut.Bytes()
	firstLine := bytes.SplitN(raw, []byte("\n"), 2)[0]
	env := decodeEnvelope(t, firstLine)
	if status := mustString(t, env["status"]); status != "error" {
		t.Fatalf("status = %s, want error", status)
	}
	errMap := mustMap(t, env["error"])
	if code := mustString(t, errMap["code"]); code != "EARG" {
		t.Fatalf("error.code = %s, want EARG", code)
	}
}

func TestEvolveNewMissingExplicitRunReturnsNotFound(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := initEvolveTestRepo(t)
	rootID := runEvolveInitForTest(t, ctx, workspacePath)

	newCmd := newEvolveNewCommand()
	newCmd.SetContext(ctx)
	newOut := &bytes.Buffer{}
	newCmd.SetOut(newOut)
	newCmd.SetErr(&bytes.Buffer{})
	newCmd.SetArgs([]string{
		"--workspace", workspacePath,
		"--run", "run-missing",
		"--parent", rootID,
	})
	err := newCmd.Execute()
	if err == nil {
		t.Fatalf("expected evolve new to fail for missing explicit run")
	}
	raw := newOut.Bytes()
	firstLine := bytes.SplitN(raw, []byte("\n"), 2)[0]
	env := decodeEnvelope(t, firstLine)
	if status := mustString(t, env["status"]); status != "error" {
		t.Fatalf("status = %s, want error", status)
	}
	errMap := mustMap(t, env["error"])
	if code := mustString(t, errMap["code"]); code != "ENOTFOUND" {
		t.Fatalf("error.code = %s, want ENOTFOUND", code)
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

func runEvolveInitForTest(t *testing.T, ctx context.Context, workspacePath string) string {
	t.Helper()
	cmd := newEvolveInitCommand()
	cmd.SetContext(ctx)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--workspace", workspacePath,
		"--target", ".",
		"--benchmark", "go test ./...",
		"--metric", "max",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("evolve init: %v", err)
	}
	data := mustMap(t, mustMap(t, decodeEnvelope(t, out.Bytes())["data"]))
	return mustString(t, data["root_node_id"])
}

func initEvolveTestRepo(t *testing.T) string {
	t.Helper()
	repoPath := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	runGit(t, repoPath, "init", "-b", "main")
	runGit(t, repoPath, "config", "user.email", "test@foxctl.dev")
	runGit(t, repoPath, "config", "user.name", "Foxctl Test")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Evolve Test\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repoPath, "add", ".")
	runGit(t, repoPath, "commit", "-m", "initial commit")
	return repoPath
}

func gitHEAD(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}
