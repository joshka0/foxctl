package hot

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	refscope "github.com/joshka0/foxctl/internal/intelligence/refactor/scope"
	refsnapshotstore "github.com/joshka0/foxctl/internal/intelligence/refactor/snapshotstore"
)

func TestBuildRanksFrequentlyTouchedFilesHigher(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	storageRoot := filepath.Join(root, "storage")
	repo := filepath.Join(root, "repo")
	mustWriteHotFile(t, filepath.Join(repo, "internal", "a.go"), "package internal\n")
	mustWriteHotFile(t, filepath.Join(repo, "internal", "b.go"), "package internal\n")
	runHotGit(t, ctx, repo, "init")
	runHotGit(t, ctx, repo, "add", ".")
	runHotGit(t, ctx, repo, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "init")

	mustWriteHotFile(t, filepath.Join(repo, "internal", "a.go"), "package internal\n\nfunc A() {}\n")
	runHotGit(t, ctx, repo, "add", "internal/a.go")
	runHotGit(t, ctx, repo, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "touch a")

	mustWriteHotFile(t, filepath.Join(repo, "internal", "a.go"), "package internal\n\nfunc A() {}\nfunc A2() {}\n")
	mustWriteHotFile(t, filepath.Join(repo, "internal", "b.go"), "package internal\n\nfunc B() {}\n")
	runHotGit(t, ctx, repo, "add", "internal/a.go", "internal/b.go")
	runHotGit(t, ctx, repo, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "touch a and b")

	result, err := Build(ctx, storageRoot, Options{
		Scope: refscope.Scope{
			Workspace: repo,
			RepoRoot:  repo,
			Path:      "internal",
			Absolute:  filepath.Join(repo, "internal"),
			Mode:      "explicit",
			Language:  "go",
			Detected:  []string{"go"},
			IsDir:     true,
		},
		Since:        "HEAD~2",
		MaxResults:   10,
		HalfLifeDays: 90,
		Now:          time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got, want := len(result.Files), 2; got != want {
		t.Fatalf("file count=%d want %d", got, want)
	}
	if result.Files[0].Path != "internal/a.go" {
		t.Fatalf("top path=%q want internal/a.go", result.Files[0].Path)
	}
	if result.Files[0].TouchCount <= result.Files[1].TouchCount {
		t.Fatalf("touch counts not ranked: %#v", result.Files)
	}
}

func TestBuildResolvesSnapshotBaselineToGitHead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	storageRoot := filepath.Join(root, "storage")
	repo := filepath.Join(root, "repo")
	mustWriteHotFile(t, filepath.Join(repo, "internal", "a.go"), "package internal\n")
	runHotGit(t, ctx, repo, "init")
	runHotGit(t, ctx, repo, "add", ".")
	runHotGit(t, ctx, repo, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "init")
	head := hotGitHead(t, ctx, repo)

	store, err := refsnapshotstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Put(ctx, refsnapshotstore.Record{
		SnapshotID:     "refsnap-1",
		Workspace:      repo,
		RepoRoot:       repo,
		Path:           "internal",
		Language:       "go",
		Mode:           "parser_only",
		GitHeadSHA:     head,
		ArtifactDigest: "sha256:test",
		CreatedAt:      time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	result, err := Build(ctx, storageRoot, Options{
		Scope: refscope.Scope{
			Workspace: repo,
			RepoRoot:  repo,
			Path:      "internal",
			Absolute:  filepath.Join(repo, "internal"),
			Mode:      "explicit",
			Language:  "go",
			Detected:  []string{"go"},
			IsDir:     true,
		},
		Since:      "refsnap-1",
		MaxResults: 10,
		Now:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.Since.Kind != SinceKindSnapshot {
		t.Fatalf("kind=%q want %q", result.Since.Kind, SinceKindSnapshot)
	}
	if result.Since.GitHeadSHA != head {
		t.Fatalf("git head=%q want %q", result.Since.GitHeadSHA, head)
	}
}

func TestParseHotCommitTimestamp(t *testing.T) {
	t.Parallel()

	got, ok := parseHotCommitTimestamp("abc123\x1f1700000000")
	if !ok {
		t.Fatalf("expected commit line")
	}
	want := time.Unix(1700000000, 0).UTC()
	if !got.Equal(want) {
		t.Fatalf("timestamp=%s want %s", got, want)
	}

	got, ok = parseHotCommitTimestamp("abc123\x1fnot-a-timestamp")
	if !ok {
		t.Fatalf("invalid timestamp line should still be treated as commit line")
	}
	if !got.IsZero() {
		t.Fatalf("invalid timestamp should reset to zero, got %s", got)
	}

	if _, ok := parseHotCommitTimestamp("internal/a.go"); ok {
		t.Fatalf("path line must not be treated as commit line")
	}
}

func TestFilterHotPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		rawPath      string
		scopePath    string
		isDir        bool
		includeTests bool
		languageHint string
		wantPath     string
		wantLanguage string
		wantOK       bool
	}{
		{
			name:         "accepts matching file",
			rawPath:      "internal/a.go",
			scopePath:    "internal",
			isDir:        true,
			includeTests: false,
			languageHint: "go",
			wantPath:     "internal/a.go",
			wantLanguage: "go",
			wantOK:       true,
		},
		{
			name:         "rejects out of scope",
			rawPath:      "pkg/a.go",
			scopePath:    "internal",
			isDir:        true,
			includeTests: false,
			languageHint: "go",
			wantOK:       false,
		},
		{
			name:         "rejects tests when disabled",
			rawPath:      "internal/a_test.go",
			scopePath:    "internal",
			isDir:        true,
			includeTests: false,
			languageHint: "go",
			wantOK:       false,
		},
		{
			name:         "allows tests when enabled",
			rawPath:      "internal/a_test.go",
			scopePath:    "internal",
			isDir:        true,
			includeTests: true,
			languageHint: "go",
			wantPath:     "internal/a_test.go",
			wantLanguage: "go",
			wantOK:       true,
		},
		{
			name:         "rejects language mismatch",
			rawPath:      "internal/a.ts",
			scopePath:    "internal",
			isDir:        true,
			includeTests: true,
			languageHint: "go",
			wantOK:       false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotPath, gotLanguage, gotOK := filterHotPath(tc.rawPath, tc.scopePath, tc.isDir, tc.includeTests, tc.languageHint)
			if gotOK != tc.wantOK {
				t.Fatalf("ok=%v want %v", gotOK, tc.wantOK)
			}
			if gotPath != tc.wantPath {
				t.Fatalf("path=%q want %q", gotPath, tc.wantPath)
			}
			if gotLanguage != tc.wantLanguage {
				t.Fatalf("language=%q want %q", gotLanguage, tc.wantLanguage)
			}
		})
	}
}

func TestRankHotFileScoresDeterministicTies(t *testing.T) {
	t.Parallel()

	scores := map[string]*fileScore{
		"internal/z.go": {Path: "internal/z.go", Score: 3.0, TouchCount: 1},
		"internal/c.go": {Path: "internal/c.go", Score: 2.0, TouchCount: 2},
		"internal/a.go": {Path: "internal/a.go", Score: 2.0, TouchCount: 2},
		"internal/b.go": {Path: "internal/b.go", Score: 2.0, TouchCount: 1},
	}

	ranked := rankHotFileScores(scores)
	got := []string{ranked[0].Path, ranked[1].Path, ranked[2].Path, ranked[3].Path}
	want := []string{"internal/z.go", "internal/a.go", "internal/c.go", "internal/b.go"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rank[%d]=%q want %q (full=%v)", i, got[i], want[i], got)
		}
	}
}

func TestParseHotLogScoresAppliesFilters(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_800_000_000, 0).UTC()
	logOutput := "commit1\x1f1800000000\n" +
		"internal/a.go\n" +
		"internal/a_test.go\n" +
		"pkg/a.go\n" +
		"internal/readme.md\n" +
		"internal/b.go\n"
	scope := refscope.Scope{Path: "internal", IsDir: true, Language: "go"}

	files := parseHotLogScores(logOutput, scope, false, 90, now)
	if got, want := len(files), 2; got != want {
		t.Fatalf("len=%d want %d", got, want)
	}
	if files[0].Path != "internal/a.go" || files[1].Path != "internal/b.go" {
		t.Fatalf("unexpected files: %#v", files)
	}
}

func TestParseHotLogScoresRecencyScoring(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_800_000_000, 0).UTC()
	newTS := now.Unix()
	oldTS := now.Add(-20 * 24 * time.Hour).Unix()

	logOutput := "commit-new\x1f" + strconv.FormatInt(newTS, 10) + "\n" +
		"internal/new.go\n" +
		"commit-old\x1f" + strconv.FormatInt(oldTS, 10) + "\n" +
		"internal/old.go\n"

	scope := refscope.Scope{Path: "internal", IsDir: true, Language: "go"}
	files := parseHotLogScores(logOutput, scope, true, 10, now)
	if got, want := len(files), 2; got != want {
		t.Fatalf("len=%d want %d", got, want)
	}
	if files[0].Path != "internal/new.go" {
		t.Fatalf("top path=%q want internal/new.go", files[0].Path)
	}

	oldScore := files[1].Score
	wantOldScore := 0.25
	if math.Abs(oldScore-wantOldScore) > 1e-9 {
		t.Fatalf("old score=%f want %f", oldScore, wantOldScore)
	}
	if files[0].Score <= files[1].Score {
		t.Fatalf("expected recent file to score higher: %#v", files)
	}
}

func mustWriteHotFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runHotGit(t *testing.T, ctx context.Context, repo string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
}

func hotGitHead(t *testing.T, ctx context.Context, repo string) string {
	t.Helper()
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git head: %v", err)
	}
	return string(bytesTrimHot(out))
}

func bytesTrimHot(data []byte) []byte {
	start := 0
	for start < len(data) && (data[start] == ' ' || data[start] == '\n' || data[start] == '\r' || data[start] == '\t') {
		start++
	}
	end := len(data)
	for end > start && (data[end-1] == ' ' || data[end-1] == '\n' || data[end-1] == '\r' || data[end-1] == '\t') {
		end--
	}
	return data[start:end]
}
