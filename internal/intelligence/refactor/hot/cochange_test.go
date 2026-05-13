package hot

import (
	"context"
	"path/filepath"
	"strings"
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

func TestBuildCochangeGraphFromLogHandlesMalformedChunksAndFilters(t *testing.T) {
	t.Parallel()

	graph := buildCochangeGraphFromLog(strings.Join([]string{
		"commit-1\x1f1710000000",
		"internal/a.go",
		"internal/b.go",
		"internal/a_test.go",
		"README.md",
		"commit-2\x1fbroken-ts",
		"internal/a.go",
		"internal/c.go",
		"internal/notes.txt",
	}, "\n"), cochangeParseOptions{
		ScopePath:    "internal",
		IsDir:        true,
		IncludeTests: false,
		Language:     "go",
		HalfLifeDays: 0,
		Now:          time.Unix(1_710_000_100, 0).UTC(),
	})

	got := extractCochangeNeighbors(graph, 10)["internal/a.go"]
	if len(got) != 2 {
		t.Fatalf("neighbor count=%d want 2 (%#v)", len(got), got)
	}
	if got[0].Path != "internal/b.go" || got[1].Path != "internal/c.go" {
		t.Fatalf("neighbor order=%#v want [internal/b.go internal/c.go]", got)
	}
	if got[0].Count != 1 || got[1].Count != 1 {
		t.Fatalf("neighbor counts=%#v want each 1", got)
	}
	if got[0].LastTouched.IsZero() {
		t.Fatalf("internal/b.go last touched should keep valid commit timestamp")
	}
	if !got[1].LastTouched.IsZero() {
		t.Fatalf("internal/c.go last touched should remain zero for malformed timestamp chunk")
	}
}

func TestExtractCochangeNeighborsDeterministicOrderAndLimit(t *testing.T) {
	t.Parallel()

	graph := map[string]map[string]*cochangeStats{
		"internal/a.go": {
			"internal/d.go": {Count: 1, Score: 1},
			"internal/c.go": {Count: 1, Score: 1},
			"internal/b.go": {Count: 1, Score: 1},
		},
	}

	got := extractCochangeNeighbors(graph, 2)["internal/a.go"]
	if len(got) != 2 {
		t.Fatalf("neighbor count=%d want 2", len(got))
	}
	if got[0].Path != "internal/b.go" || got[1].Path != "internal/c.go" {
		t.Fatalf("neighbor order=%#v want [internal/b.go internal/c.go]", got)
	}
}

func TestFilterCochangeCommitPathIncludeTestsToggle(t *testing.T) {
	t.Parallel()

	opts := cochangeParseOptions{
		ScopePath: "internal",
		IsDir:     true,
		Language:  "go",
	}

	if _, ok := filterCochangeCommitPath("internal/a_test.go", opts); ok {
		t.Fatalf("expected test file to be excluded when IncludeTests=false")
	}

	opts.IncludeTests = true
	path, ok := filterCochangeCommitPath("internal/a_test.go", opts)
	if !ok || path != "internal/a_test.go" {
		t.Fatalf("expected test file when IncludeTests=true, got path=%q ok=%v", path, ok)
	}
}
