package repoindex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLadybugFreshnessScriptAcceptsCurrentProjection(t *testing.T) {
	repoRoot, head := newLadybugFreshnessGitRepo(t)
	metaPath := filepath.Join(t.TempDir(), "projection.lbug.meta.json")
	writeLadybugFreshnessMeta(t, metaPath, head, false, "")

	output, err := runLadybugFreshnessScript(t,
		"--db-path", strings.TrimSuffix(metaPath, ".meta.json"),
		"--projection-meta", metaPath,
		"--repo-root", repoRoot,
	)
	if err != nil {
		t.Fatalf("fresh projection rejected: %v\n%s", err, output)
	}
}

func TestLadybugFreshnessScriptRejectsStaleHead(t *testing.T) {
	repoRoot, _ := newLadybugFreshnessGitRepo(t)
	metaPath := filepath.Join(t.TempDir(), "projection.lbug.meta.json")
	writeLadybugFreshnessMeta(t, metaPath, strings.Repeat("0", 40), false, "")

	output, err := runLadybugFreshnessScript(t,
		"--db-path", strings.TrimSuffix(metaPath, ".meta.json"),
		"--projection-meta", metaPath,
		"--repo-root", repoRoot,
	)
	if err == nil {
		t.Fatalf("stale projection accepted:\n%s", output)
	}
	assertContainsAll(t, output, []string{"stale projection", "head_mismatch"})
}

func TestLadybugFreshnessScriptRejectsDirtyStateChange(t *testing.T) {
	repoRoot, head := newLadybugFreshnessGitRepo(t)
	metaPath := filepath.Join(t.TempDir(), "projection.lbug.meta.json")
	writeLadybugFreshnessMeta(t, metaPath, head, false, "")
	if err := os.WriteFile(filepath.Join(repoRoot, "file.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("dirty repo: %v", err)
	}

	output, err := runLadybugFreshnessScript(t,
		"--db-path", strings.TrimSuffix(metaPath, ".meta.json"),
		"--projection-meta", metaPath,
		"--repo-root", repoRoot,
	)
	if err == nil {
		t.Fatalf("dirty-state change accepted:\n%s", output)
	}
	assertContainsAll(t, output, []string{"stale projection", "dirty_state_changed"})
}

func TestLadybugFreshnessScriptAcceptsMatchingDirtyProjection(t *testing.T) {
	repoRoot, head := newLadybugFreshnessGitRepo(t)
	if err := os.WriteFile(filepath.Join(repoRoot, "file.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("dirty repo: %v", err)
	}
	snapshot := ResolveGitSnapshot(context.Background(), repoRoot)
	if !snapshot.WorktreeDirty || snapshot.DirtyStatusHash == "" {
		t.Fatalf("dirty snapshot not detected: %#v", snapshot)
	}
	metaPath := filepath.Join(t.TempDir(), "projection.lbug.meta.json")
	writeLadybugFreshnessMeta(t, metaPath, head, true, snapshot.DirtyStatusHash)

	output, err := runLadybugFreshnessScript(t,
		"--db-path", strings.TrimSuffix(metaPath, ".meta.json"),
		"--projection-meta", metaPath,
		"--repo-root", repoRoot,
	)
	if err != nil {
		t.Fatalf("matching dirty projection rejected: %v\n%s", err, output)
	}
}

func TestLadybugFreshnessScriptAllowsExplicitStaleBypass(t *testing.T) {
	repoRoot, _ := newLadybugFreshnessGitRepo(t)
	metaPath := filepath.Join(t.TempDir(), "projection.lbug.meta.json")
	writeLadybugFreshnessMeta(t, metaPath, strings.Repeat("0", 40), false, "")

	output, err := runLadybugFreshnessScript(t,
		"--db-path", strings.TrimSuffix(metaPath, ".meta.json"),
		"--projection-meta", metaPath,
		"--repo-root", repoRoot,
		"--allow-stale",
	)
	if err != nil {
		t.Fatalf("explicit stale bypass rejected: %v\n%s", err, output)
	}
}

func newLadybugFreshnessGitRepo(t *testing.T) (string, string) {
	t.Helper()
	requireLadybugFreshnessTool(t, "git")
	requireLadybugFreshnessTool(t, "jq")
	requireLadybugFreshnessTool(t, "perl")
	if _, err := exec.LookPath("shasum"); err != nil {
		requireLadybugFreshnessTool(t, "sha256sum")
	}

	repoRoot := t.TempDir()
	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "ladybug@example.invalid")
	runGit(t, repoRoot, "config", "user.name", "Ladybug Test")
	if err := os.WriteFile(filepath.Join(repoRoot, "file.txt"), []byte("original\n"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	runGit(t, repoRoot, "add", "file.txt")
	runGit(t, repoRoot, "commit", "-m", "initial")
	return repoRoot, runGit(t, repoRoot, "rev-parse", "HEAD")
}

func runLadybugFreshnessScript(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(ladybugScriptPath(t, "ladybug_freshness.sh"), args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func writeLadybugFreshnessMeta(t *testing.T, path, head string, dirty bool, dirtyHash string) {
	t.Helper()
	meta := struct {
		HeadSHA         string `json:"head_sha"`
		WorktreeDirty   bool   `json:"worktree_dirty"`
		DirtyStatusHash string `json:"dirty_status_hash"`
	}{
		HeadSHA:         head,
		WorktreeDirty:   dirty,
		DirtyStatusHash: dirtyHash,
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
}

func runGit(t *testing.T, repoRoot string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func requireLadybugFreshnessTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not found; skipping Ladybug freshness script test", name)
	}
}
