package hot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	refscope "github.com/jkatigb/agentctl/internal/refactor/scope"
	refsnapshotstore "github.com/jkatigb/agentctl/internal/refactor/snapshotstore"
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
