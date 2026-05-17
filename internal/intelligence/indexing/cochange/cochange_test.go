package cochange

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestScoreCapsNeighborsAndPreservesSymmetry(t *testing.T) {
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	commits := []Commit{
		{SHA: "c1", Timestamp: now.AddDate(0, 0, -1), Files: []string{"a.go", "b.go"}},
		{SHA: "c2", Timestamp: now.AddDate(0, 0, -2), Files: []string{"a.go", "b.go", "c.go"}},
		{SHA: "c3", Timestamp: now.AddDate(0, 0, -3), Files: []string{"a.go", "d.go"}},
	}
	got := Score(commits, Config{
		Now:                  now,
		TopKPerFile:          1,
		GiantCommitSoftLimit: 10,
		GiantCommitHardLimit: 10,
	})
	if len(got["a.go"]) != 1 {
		t.Fatalf("a.go neighbor count=%d want 1", len(got["a.go"]))
	}
	if got["a.go"][0].Path != "b.go" {
		t.Fatalf("top a.go neighbor=%s want b.go", got["a.go"][0].Path)
	}
	if got["b.go"][0].Path != "a.go" {
		t.Fatalf("top b.go neighbor=%s want a.go", got["b.go"][0].Path)
	}
	if got["a.go"][0].LastSeenCommit != "c1" {
		t.Fatalf("LastSeenCommit=%s want c1", got["a.go"][0].LastSeenCommit)
	}
	if got["a.go"][0].Freshness <= 0 || got["a.go"][0].Volatility <= 0 {
		t.Fatalf("expected freshness and volatility metadata: %+v", got["a.go"][0])
	}
}

func TestScoreDownweightsGiantCommitsAndSkipsHardLimit(t *testing.T) {
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	normal := CommitWeight(now, 2, Config{
		Now:                  now,
		GiantCommitSoftLimit: 2,
		GiantCommitHardLimit: 4,
	})
	soft := CommitWeight(now, 4, Config{
		Now:                  now,
		GiantCommitSoftLimit: 2,
		GiantCommitHardLimit: 4,
	})
	skipped := CommitWeight(now, 5, Config{
		Now:                  now,
		GiantCommitSoftLimit: 2,
		GiantCommitHardLimit: 4,
	})
	if !(soft > 0 && soft < normal) {
		t.Fatalf("soft giant weight=%f want between 0 and normal %f", soft, normal)
	}
	if skipped != 0 {
		t.Fatalf("hard giant weight=%f want 0", skipped)
	}
}

func TestScoreSkipsCommitsAboveMaxFilesPerCommit(t *testing.T) {
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	got := Score([]Commit{
		{SHA: "wide", Timestamp: now, Files: []string{"a.go", "b.go", "c.go"}},
	}, Config{
		Now:                  now,
		MaxFilesPerCommit:    2,
		GiantCommitSoftLimit: 10,
		GiantCommitHardLimit: 10,
	})
	if len(got) != 0 {
		t.Fatalf("Score=%v want empty graph for commit over max files cap", got)
	}
}

func TestWeightedCommitWeightDownweightsFormattingOnlySignal(t *testing.T) {
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	cfg := Config{
		Now:                  now,
		GiantCommitSoftLimit: 10,
		GiantCommitHardLimit: 10,
		FormattingMultiplier: 0.1,
	}
	normal := WeightedCommitWeight(Commit{Timestamp: now}, 2, cfg)
	formatting := WeightedCommitWeight(Commit{Timestamp: now, FormattingOnly: true}, 2, cfg)
	if !(formatting > 0 && formatting < normal) {
		t.Fatalf("formatting weight=%f want positive and below normal %f", formatting, normal)
	}
}

func TestRecentCommitsOutweighOldCommits(t *testing.T) {
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	cfg := Config{
		Now:                  now,
		HalfLifeDays:         30,
		GiantCommitSoftLimit: 10,
		GiantCommitHardLimit: 10,
	}
	recent := CommitWeight(now.AddDate(0, 0, -1), 2, cfg)
	old := CommitWeight(now.AddDate(0, -6, 0), 2, cfg)
	if recent <= old {
		t.Fatalf("recent weight=%f want above old %f", recent, old)
	}
}

func TestFilterPathsSkipsLockfilesGeneratedAndVendor(t *testing.T) {
	got := FilterPaths([]string{
		"src/a.go",
		"pnpm-lock.yaml",
		"vendor/mod/a.go",
		"internal/service.pb.go",
		"node_modules/pkg/index.js",
	}, Config{SkipGenerated: true, SkipLockfiles: true})
	if len(got) != 1 || got[0] != "src/a.go" {
		t.Fatalf("FilterPaths=%v want [src/a.go]", got)
	}
}

func TestCollectGitCommitsMarksWhitespaceOnlyAndIgnoresWorktree(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	runGit(t, repo, "init")

	writeFile(t, repo, "a.go", "package main\nfunc a(){return}\n")
	writeFile(t, repo, "b.go", "package main\nfunc b(){return}\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "-c", "user.name=Foxctl Test", "-c", "user.email=foxctl@example.invalid", "commit", "-m", "initial")

	writeFile(t, repo, "a.go", "package main\n\nfunc a() { return }\n")
	writeFile(t, repo, "b.go", "package main\n\nfunc b() { return }\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "-c", "user.name=Foxctl Test", "-c", "user.email=foxctl@example.invalid", "commit", "-m", "format")

	writeFile(t, repo, "uncommitted.go", "package main\n")

	commits, err := CollectGitCommits(ctx, repo, []string{"a.go"}, Config{CommitLimit: 10})
	if err != nil {
		t.Fatalf("CollectGitCommits: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("commits=%d want 2: %+v", len(commits), commits)
	}
	if !commits[0].FormattingOnly {
		t.Fatalf("latest commit FormattingOnly=false want true: %+v", commits[0])
	}
	for _, commit := range commits {
		for _, file := range commit.Files {
			if file == "uncommitted.go" {
				t.Fatalf("uncommitted worktree file leaked into committed history: %+v", commit)
			}
		}
	}
}

func TestCollectGitCommitsScoresRepeatedCoChangeFromTempGitHistory(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	repo := t.TempDir()
	runGit(t, repo, "init")

	writeFile(t, repo, "README.md", "fixture\n")
	runGit(t, repo, "add", ".")
	commitGitAt(t, repo, now.AddDate(0, 0, -30), "initial")

	writeFile(t, repo, "a.go", "package fixture\nfunc A() int { return 1 }\n")
	writeFile(t, repo, "b.go", "package fixture\nfunc B() int { return A() }\n")
	runGit(t, repo, "add", ".")
	commitGitAt(t, repo, now.AddDate(0, 0, -10), "pair one")

	writeFile(t, repo, "a.go", "package fixture\nfunc A() int { return 2 }\n")
	writeFile(t, repo, "b.go", "package fixture\nfunc B() int { return A() + 1 }\n")
	runGit(t, repo, "add", ".")
	commitGitAt(t, repo, now.AddDate(0, 0, -5), "pair two")

	writeFile(t, repo, "a.go", "package fixture\nfunc A() int { return 3 }\n")
	writeFile(t, repo, "c.go", "package fixture\nfunc C() int { return A() }\n")
	runGit(t, repo, "add", ".")
	commitGitAt(t, repo, now.AddDate(0, 0, -4), "competing pair")

	writeFile(t, repo, "a.go", "package fixture\nfunc A() int { return 4 }\n")
	writeFile(t, repo, "d.go", "package fixture\nfunc D() {}\n")
	writeFile(t, repo, "e.go", "package fixture\nfunc E() {}\n")
	writeFile(t, repo, "f.go", "package fixture\nfunc F() {}\n")
	runGit(t, repo, "add", ".")
	commitGitAt(t, repo, now.AddDate(0, 0, -3), "wide generated noise")

	writeFile(t, repo, "a.go", "package fixture\nfunc A() int { return 5 }\n")
	writeFile(t, repo, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	writeFile(t, repo, "service.pb.go", "package fixture\nfunc Generated() {}\n")
	runGit(t, repo, "add", ".")
	commitGitAt(t, repo, now.AddDate(0, 0, -2), "lock and generated noise")

	writeFile(t, repo, "a.go", "package fixture\n\nfunc A() int { return 5 }\n")
	writeFile(t, repo, "b.go", "package fixture\n\nfunc B() int { return A() + 1 }\n")
	runGit(t, repo, "add", ".")
	commitGitAt(t, repo, now.AddDate(0, 0, -1), "format pair")

	commits, err := CollectGitCommits(ctx, repo, nil, Config{CommitLimit: 20})
	if err != nil {
		t.Fatalf("CollectGitCommits: %v", err)
	}
	if !hasFormattingOnlyCommit(commits) {
		t.Fatalf("expected collected history to mark the whitespace-only commit: %+v", commits)
	}

	got := Score(commits, Config{
		Now:                  now,
		MaxFilesPerCommit:    3,
		HalfLifeDays:         30,
		TopKPerFile:          2,
		SkipGenerated:        true,
		SkipLockfiles:        true,
		GiantCommitSoftLimit: 3,
		GiantCommitHardLimit: 3,
		FormattingMultiplier: 0.1,
	})
	aToB := findNeighbor(t, got["a.go"], "b.go")
	aToC := findNeighbor(t, got["a.go"], "c.go")
	bToA := findNeighbor(t, got["b.go"], "a.go")
	if aToB.Count != 3 {
		t.Fatalf("a.go->b.go count=%d want 3 from repeated empirical co-change", aToB.Count)
	}
	if aToC.Count != 1 {
		t.Fatalf("a.go->c.go count=%d want 1", aToC.Count)
	}
	if aToB.WeightedCount <= aToC.WeightedCount {
		t.Fatalf("repeated co-change weight=%f want above competing pair %f", aToB.WeightedCount, aToC.WeightedCount)
	}
	if bToA.Count != aToB.Count || bToA.WeightedCount != aToB.WeightedCount {
		t.Fatalf("symmetry mismatch a->b=%+v b->a=%+v", aToB, bToA)
	}
	if aToB.LastSeenAt != now.AddDate(0, 0, -1) {
		t.Fatalf("last seen=%s want latest formatting commit time", aToB.LastSeenAt)
	}
	if aToB.Freshness <= aToC.Freshness {
		t.Fatalf("freshness a->b=%f want above older a->c=%f", aToB.Freshness, aToC.Freshness)
	}
	for _, noisy := range []string{"pnpm-lock.yaml", "service.pb.go", "d.go", "e.go", "f.go"} {
		if _, ok := got[noisy]; ok {
			t.Fatalf("noisy path %s should not emit co-change neighbors: %+v", noisy, got[noisy])
		}
		if hasNeighbor(got["a.go"], noisy) {
			t.Fatalf("a.go should not include noisy neighbor %s: %+v", noisy, got["a.go"])
		}
	}
}

func TestParseGitLogNameOnlyIgnoresUncommittedStateShape(t *testing.T) {
	output := "abc\x1f1778068800\nb.go\na.go\n\nxyz\x1f1777982400\na.go\nc.go\n"
	got := ParseGitLogNameOnly(output)
	if len(got) != 2 {
		t.Fatalf("commits=%d want 2", len(got))
	}
	if got[0].SHA != "abc" || got[0].Files[0] != "a.go" || got[0].Files[1] != "b.go" {
		t.Fatalf("first commit=%+v", got[0])
	}
}

func hasFormattingOnlyCommit(commits []Commit) bool {
	for _, commit := range commits {
		if commit.FormattingOnly {
			return true
		}
	}
	return false
}

func findNeighbor(t *testing.T, neighbors []Neighbor, path string) Neighbor {
	t.Helper()
	for _, neighbor := range neighbors {
		if neighbor.Path == path {
			return neighbor
		}
	}
	t.Fatalf("neighbor %s not found in %+v", path, neighbors)
	return Neighbor{}
}

func hasNeighbor(neighbors []Neighbor, path string) bool {
	for _, neighbor := range neighbors {
		if neighbor.Path == path {
			return true
		}
	}
	return false
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func commitGitAt(t *testing.T, root string, at time.Time, message string) {
	t.Helper()
	cmd := exec.Command("git", "-C", root, "-c", "user.name=Foxctl Test", "-c", "user.email=foxctl@example.invalid", "commit", "-m", message)
	cmd.Env = append(
		os.Environ(),
		"GIT_AUTHOR_DATE="+at.Format(time.RFC3339),
		"GIT_COMMITTER_DATE="+at.Format(time.RFC3339),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git commit %q: %v\n%s", message, err, string(out))
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
}
