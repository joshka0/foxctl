package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/worktree"
	"github.com/joshka0/foxctl/internal/tooling/evolve/model"
)

func TestEvolveDiscardMarksNodeDiscarded(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runID, rootID := runEvolveInitWithBenchmarkForTest(t, ctx, workspacePath, "sh -lc 'echo score=1.0'")
	st := openEvolveStoreForTest(t, ctx, cfg.Storage.Root)
	defer func() { _ = st.Close() }()

	node, err := st.Node(ctx, rootID)
	if err != nil {
		t.Fatalf("load root node: %v", err)
	}
	node.Status = model.NodeStatusFailed
	node.UpdatedAt = time.Date(2026, 4, 16, 16, 0, 0, 0, time.UTC)
	if err := st.SaveNode(ctx, node); err != nil {
		t.Fatalf("save root node: %v", err)
	}

	cmd := newEvolveDiscardCommand()
	cmd.SetContext(ctx)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--workspace", workspacePath,
		"--run", runID,
		"--node", rootID,
		"--reason", "manual discard",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("evolve discard: %v", err)
	}

	env := decodeEnvelope(t, out.Bytes())
	data := mustMap(t, env["data"])
	if got := mustString(t, data["status"]); got != string(model.NodeStatusDiscarded) {
		t.Fatalf("status = %s, want %s", got, model.NodeStatusDiscarded)
	}
	if got := mustString(t, data["pruned_reason"]); got != "manual discard" {
		t.Fatalf("pruned_reason = %q, want %q", got, "manual discard")
	}

	updated, err := st.Node(ctx, rootID)
	if err != nil {
		t.Fatalf("reload node: %v", err)
	}
	if updated.Status != model.NodeStatusDiscarded {
		t.Fatalf("node status = %s, want %s", updated.Status, model.NodeStatusDiscarded)
	}
	if updated.PrunedReason != "manual discard" {
		t.Fatalf("node pruned_reason = %q, want %q", updated.PrunedReason, "manual discard")
	}
}

func TestEvolvePruneRejectsRootNode(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := initEvolveTestRepo(t)

	runID, rootID := runEvolveInitWithBenchmarkForTest(t, ctx, workspacePath, "sh -lc 'echo score=1.0'")

	cmd := newEvolvePruneCommand()
	cmd.SetContext(ctx)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--workspace", workspacePath, "--run", runID, "--node", rootID})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected prune root node to fail")
	}

	env := decodeFirstEnvelopeLine(t, out.Bytes())
	if status := mustString(t, env["status"]); status != "error" {
		t.Fatalf("status = %s, want error", status)
	}
	errMap := mustMap(t, env["error"])
	if code := mustString(t, errMap["code"]); code != "EARG" {
		t.Fatalf("error.code = %s, want EARG", code)
	}
}

func TestEvolvePruneWithoutWorktreeReturnsArgumentError(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runID, rootID := runEvolveInitWithBenchmarkForTest(t, ctx, workspacePath, "sh -lc 'echo score=1.0'")
	st := openEvolveStoreForTest(t, ctx, cfg.Storage.Root)
	defer func() { _ = st.Close() }()

	now := time.Date(2026, 4, 16, 16, 30, 0, 0, time.UTC)
	child := model.Node{
		ID:        "child-no-worktree",
		RunID:     runID,
		ParentID:  rootID,
		Status:    model.NodeStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := st.SaveNode(ctx, child); err != nil {
		t.Fatalf("save child node: %v", err)
	}

	cmd := newEvolvePruneCommand()
	cmd.SetContext(ctx)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--workspace", workspacePath, "--run", runID, "--node", child.ID})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected prune node without worktree path to fail")
	}

	env := decodeFirstEnvelopeLine(t, out.Bytes())
	errMap := mustMap(t, env["error"])
	if code := mustString(t, errMap["code"]); code != "EARG" {
		t.Fatalf("error.code = %s, want EARG", code)
	}
}

func TestEvolvePruneMissingNodeReturnsNotFound(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runID, _ := runEvolveInitWithBenchmarkForTest(t, ctx, workspacePath, "sh -lc 'echo score=1.0'")

	cmd := newEvolvePruneCommand()
	cmd.SetContext(ctx)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--workspace", workspacePath, "--run", runID, "--node", "node-missing"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected prune missing node to fail")
	}

	env := decodeFirstEnvelopeLine(t, out.Bytes())
	errMap := mustMap(t, env["error"])
	if code := mustString(t, errMap["code"]); code != "ENOTFOUND" {
		t.Fatalf("error.code = %s, want ENOTFOUND", code)
	}
}

func TestEvolvePruneRemovesWorktreeAndMarksNodePruned(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := initEvolveTestRepo(t)

	runID, rootID := runEvolveInitWithBenchmarkForTest(t, ctx, workspacePath, "sh -lc 'echo score=1.0'")

	newCmd := newEvolveNewCommand()
	newCmd.SetContext(ctx)
	newOut := &bytes.Buffer{}
	newCmd.SetOut(newOut)
	newCmd.SetErr(&bytes.Buffer{})
	newCmd.SetArgs([]string{
		"--workspace", workspacePath,
		"--run", runID,
		"--parent", rootID,
		"--hypothesis", "prune me",
	})
	if err := newCmd.Execute(); err != nil {
		t.Fatalf("evolve new: %v", err)
	}

	newEnv := decodeEnvelope(t, newOut.Bytes())
	newData := mustMap(t, newEnv["data"])
	childID := mustString(t, newData["node_id"])
	worktreePath := mustString(t, newData["worktree_path"])
	branchName := mustString(t, newData["branch"])

	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("expected child worktree to exist before prune: %v", err)
	}

	pruneCmd := newEvolvePruneCommand()
	pruneCmd.SetContext(ctx)
	pruneOut := &bytes.Buffer{}
	pruneCmd.SetOut(pruneOut)
	pruneCmd.SetErr(&bytes.Buffer{})
	pruneCmd.SetArgs([]string{
		"--workspace", workspacePath,
		"--run", runID,
		"--node", childID,
		"--reason", "superseded",
	})
	if err := pruneCmd.Execute(); err != nil {
		t.Fatalf("evolve prune: %v", err)
	}

	env := decodeEnvelope(t, pruneOut.Bytes())
	data := mustMap(t, env["data"])
	if got := mustString(t, data["status"]); got != string(model.NodeStatusPruned) {
		t.Fatalf("status = %s, want %s", got, model.NodeStatusPruned)
	}
	if got := mustString(t, data["removed_worktree_path"]); got != worktreePath {
		t.Fatalf("removed_worktree_path = %s, want %s", got, worktreePath)
	}
	if deleted, ok := data["branch_deleted"].(bool); !ok || !deleted {
		t.Fatalf("branch_deleted = %#v, want true", data["branch_deleted"])
	}
	if got := mustString(t, data["removed_branch"]); got != branchName {
		t.Fatalf("removed_branch = %s, want %s", got, branchName)
	}

	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("expected pruned worktree to be removed, stat err=%v", err)
	}

	st := openEvolveStoreForTest(t, ctx, cfg.Storage.Root)
	defer func() { _ = st.Close() }()
	node, err := st.Node(ctx, childID)
	if err != nil {
		t.Fatalf("reload child node: %v", err)
	}
	if node.Status != model.NodeStatusPruned {
		t.Fatalf("node status = %s, want %s", node.Status, model.NodeStatusPruned)
	}
	if node.WorktreePath != "" {
		t.Fatalf("node worktree_path = %q, want empty", node.WorktreePath)
	}
	if node.PrunedReason != "superseded" {
		t.Fatalf("node pruned_reason = %q, want %q", node.PrunedReason, "superseded")
	}

	branchCmd := exec.Command("git", "branch", "--list", branchName)
	branchCmd.Dir = workspacePath
	branchCmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	branchOut, err := branchCmd.Output()
	if err != nil {
		t.Fatalf("git branch --list: %v", err)
	}
	if strings.TrimSpace(string(branchOut)) != "" {
		t.Fatalf("expected branch %s to be deleted, got %q", branchName, strings.TrimSpace(string(branchOut)))
	}
}

func TestEvolvePruneRejectsTamperedNodeBranchMismatch(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := initEvolveTestRepo(t)

	runID, rootID := runEvolveInitWithBenchmarkForTest(t, ctx, workspacePath, "sh -lc 'echo score=1.0'")
	st := openEvolveStoreForTest(t, ctx, cfg.Storage.Root)
	defer func() { _ = st.Close() }()

	mgr := worktree.NewManager()
	wt, err := mgr.Create(ctx, workspacePath, "feature/prune-mismatch", worktree.WithNewBranch(true))
	if err != nil {
		t.Fatalf("create non-foxctl worktree: %v", err)
	}

	now := time.Date(2026, 4, 16, 16, 45, 0, 0, time.UTC)
	node := model.Node{
		ID:           "node-branch-mismatch",
		RunID:        runID,
		ParentID:     rootID,
		Status:       model.NodeStatusPending,
		Branch:       "foxctl/evolve/fake/run",
		WorktreePath: wt.Path,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := st.SaveNode(ctx, node); err != nil {
		t.Fatalf("save mismatch node: %v", err)
	}

	cmd := newEvolvePruneCommand()
	cmd.SetContext(ctx)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--workspace", workspacePath,
		"--run", runID,
		"--node", node.ID,
	})
	err = cmd.Execute()
	if err == nil {
		t.Fatalf("expected prune to fail for mismatched node/worktree branch")
	}

	env := decodeFirstEnvelopeLine(t, out.Bytes())
	errMap := mustMap(t, env["error"])
	if code := mustString(t, errMap["code"]); code != "EARG" {
		t.Fatalf("error.code = %s, want EARG", code)
	}

	if _, statErr := os.Stat(wt.Path); statErr != nil {
		t.Fatalf("expected worktree path to remain after mismatch failure: %v", statErr)
	}

	branchCmd := exec.Command("git", "branch", "--list", wt.Branch)
	branchCmd.Dir = workspacePath
	branchCmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	branchOut, branchErr := branchCmd.Output()
	if branchErr != nil {
		t.Fatalf("git branch --list: %v", branchErr)
	}
	if strings.TrimSpace(string(branchOut)) == "" {
		t.Fatalf("expected actual branch %s to remain after mismatch failure", wt.Branch)
	}
}

func TestEvolveResetPreservesAttempts(t *testing.T) {
	cfg := testEvolveConfig(t)
	ctx := config.WithContext(context.Background(), cfg)
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runID, rootID := runEvolveInitWithBenchmarkForTest(t, ctx, workspacePath, "sh -lc 'echo score=1.0'")
	st := openEvolveStoreForTest(t, ctx, cfg.Storage.Root)
	defer func() { _ = st.Close() }()

	now := time.Date(2026, 4, 16, 17, 0, 0, 0, time.UTC)
	node, err := st.Node(ctx, rootID)
	if err != nil {
		t.Fatalf("load root node: %v", err)
	}
	score := 42.0
	node.Status = model.NodeStatusFailed
	node.Score = &score
	node.EvalEpoch = 3
	node.CurrentAttempt = 4
	node.EvaluatedAttempts = 2
	node.PrunedReason = "old reason"
	node.UpdatedAt = now
	if err := st.SaveNode(ctx, node); err != nil {
		t.Fatalf("save root node: %v", err)
	}

	finishedAt := now.Add(1 * time.Minute)
	attempt := model.Attempt{
		ID:                "attempt-reset-1",
		NodeID:            rootID,
		AttemptNo:         1,
		Status:            model.AttemptStatusFailed,
		BenchmarkArtifact: "sha256:bench",
		Error:             "benchmark failed",
		StartedAt:         now,
		FinishedAt:        finishedAt,
	}
	if err := st.SaveAttempt(ctx, attempt); err != nil {
		t.Fatalf("save attempt: %v", err)
	}

	cmd := newEvolveResetCommand()
	cmd.SetContext(ctx)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--workspace", workspacePath, "--run", runID, "--node", rootID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("evolve reset: %v", err)
	}

	env := decodeEnvelope(t, out.Bytes())
	data := mustMap(t, env["data"])
	if got := mustString(t, data["status"]); got != string(model.NodeStatusRoot) {
		t.Fatalf("status = %s, want %s", got, model.NodeStatusRoot)
	}
	if got := int(mustFloat64(t, data["attempts_preserved"])); got != 1 {
		t.Fatalf("attempts_preserved = %d, want 1", got)
	}

	resetNode, err := st.Node(ctx, rootID)
	if err != nil {
		t.Fatalf("reload root node: %v", err)
	}
	if resetNode.Status != model.NodeStatusRoot {
		t.Fatalf("node status = %s, want %s", resetNode.Status, model.NodeStatusRoot)
	}
	if resetNode.Score != nil {
		t.Fatalf("node score = %v, want nil", *resetNode.Score)
	}
	if resetNode.CurrentAttempt != 0 || resetNode.EvaluatedAttempts != 0 || resetNode.EvalEpoch != 0 {
		t.Fatalf(
			"node counters = current:%d evaluated:%d epoch:%d, want all zero",
			resetNode.CurrentAttempt,
			resetNode.EvaluatedAttempts,
			resetNode.EvalEpoch,
		)
	}
	if resetNode.PrunedReason != "" {
		t.Fatalf("node pruned_reason = %q, want empty", resetNode.PrunedReason)
	}

	attempts, err := st.AttemptsByNode(ctx, rootID)
	if err != nil {
		t.Fatalf("load attempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("attempt count = %d, want 1", len(attempts))
	}
}

func decodeFirstEnvelopeLine(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	line := bytes.SplitN(raw, []byte("\n"), 2)[0]
	return decodeEnvelope(t, line)
}
