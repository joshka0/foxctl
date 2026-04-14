package hot

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	refscope "github.com/joshka0/foxctl/internal/intelligence/refactor/scope"
)

func TestBuildCochangeIndexRanksRepeatedPairs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")

	mustWriteHotFile(t, filepath.Join(repo, "internal", "a.go"), "package internal\n")
	mustWriteHotFile(t, filepath.Join(repo, "internal", "b.go"), "package internal\n")
	mustWriteHotFile(t, filepath.Join(repo, "internal", "c.go"), "package internal\n")
	runHotGit(t, ctx, repo, "init")
	runHotGit(t, ctx, repo, "add", ".")
	runHotGit(t, ctx, repo, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "init")

	mustWriteHotFile(t, filepath.Join(repo, "internal", "a.go"), "package internal\n\nfunc A() {}\n")
	mustWriteHotFile(t, filepath.Join(repo, "internal", "b.go"), "package internal\n\nfunc B() {}\n")
	runHotGit(t, ctx, repo, "add", "internal/a.go", "internal/b.go")
	runHotGit(t, ctx, repo, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "touch a+b")

	mustWriteHotFile(t, filepath.Join(repo, "internal", "a.go"), "package internal\n\nfunc A() {}\nfunc A2() {}\n")
	mustWriteHotFile(t, filepath.Join(repo, "internal", "b.go"), "package internal\n\nfunc B() {}\nfunc B2() {}\n")
	runHotGit(t, ctx, repo, "add", "internal/a.go", "internal/b.go")
	runHotGit(t, ctx, repo, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "touch a+b again")

	mustWriteHotFile(t, filepath.Join(repo, "internal", "c.go"), "package internal\n\nfunc C() {}\n")
	runHotGit(t, ctx, repo, "add", "internal/c.go")
	runHotGit(t, ctx, repo, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "touch c")

	scope := refscope.Scope{
		Workspace: repo,
		RepoRoot:  repo,
		Path:      "internal",
		Absolute:  filepath.Join(repo, "internal"),
		Mode:      "explicit",
		Language:  "go",
		Detected:  []string{"go"},
		IsDir:     true,
	}
	got, err := BuildCochangeIndex(ctx, scope, false, "HEAD~3", 90, time.Now().UTC(), 5)
	if err != nil {
		t.Fatalf("BuildCochangeIndex: %v", err)
	}

	neighbors := got["internal/a.go"]
	if len(neighbors) == 0 {
		t.Fatalf("expected cochange neighbors for a.go: %#v", got)
	}
	if neighbors[0].Path != "internal/b.go" {
		t.Fatalf("top neighbor=%q want internal/b.go", neighbors[0].Path)
	}
	if neighbors[0].Count != 2 {
		t.Fatalf("cochange count=%d want 2", neighbors[0].Count)
	}
	if _, ok := got["internal/c.go"]; ok && len(got["internal/c.go"]) != 0 {
		t.Fatalf("expected c.go to have no cochange neighbors: %#v", got["internal/c.go"])
	}
}
