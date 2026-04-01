package status

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	refscope "github.com/jkatigb/agentctl/internal/refactor/scope"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
)

func TestEvaluateParserOnlyWhenIndexMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root, storageRoot, workspace := setupGitWorkspace(t, ctx)
	_ = root

	scope := refscope.Scope{
		Workspace: workspace,
		RepoRoot:  workspace,
		Path:      ".",
		Absolute:  workspace,
		Mode:      "explicit",
		Language:  "go",
		Detected:  []string{"go"},
		IsDir:     true,
	}

	got := Evaluate(ctx, storageRoot, scope)
	if got.Mode != ModeParserOnly {
		t.Fatalf("mode=%q want %q", got.Mode, ModeParserOnly)
	}
	if !containsReason(got.Reasons, ReasonRepoIndexMissing) {
		t.Fatalf("reasons=%v want %s", got.Reasons, ReasonRepoIndexMissing)
	}
}

func TestEvaluateIndexBackedWhenHeadAndLanguageMatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root, storageRoot, workspace := setupGitWorkspace(t, ctx)

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	head := gitHead(t, ctx, workspace)
	if err := store.SetMeta(ctx, repoindex.IndexMeta{
		RepoRoot:      workspace,
		HeadSHA:       head,
		SchemaVersion: 3,
		IndexedAt:     time.Now().UTC(),
		Languages:     []string{"go"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceAll(ctx, []repoindex.Node{{
		ID:        repoindex.PackageID(store.RepoKey(), "go:example"),
		Kind:      repoindex.NodePackage,
		Pkg:       "go:example",
		Name:      "example",
		UpdatedAt: time.Now().UTC(),
	}}, nil); err != nil {
		t.Fatal(err)
	}

	scope := refscope.Scope{
		Workspace: workspace,
		RepoRoot:  workspace,
		Path:      ".",
		Absolute:  workspace,
		Mode:      "explicit",
		Language:  "go",
		Detected:  []string{"go"},
		IsDir:     true,
	}

	got := Evaluate(ctx, storageRoot, scope)
	if got.Mode != ModeIndexBacked {
		t.Fatalf("mode=%q want %q (reasons=%v)", got.Mode, ModeIndexBacked, got.Reasons)
	}
	if !got.RepoIndex.Available {
		t.Fatalf("expected repo index available")
	}
	if got.Git.HeadSHA != head {
		t.Fatalf("git head=%q want %q", got.Git.HeadSHA, head)
	}
	if len(got.Reasons) != 0 {
		t.Fatalf("reasons=%v want none", got.Reasons)
	}
	_ = root
}

func TestEvaluateParserOnlyWhenHeadMismatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, storageRoot, workspace := setupGitWorkspace(t, ctx)

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetMeta(ctx, repoindex.IndexMeta{
		RepoRoot:      workspace,
		HeadSHA:       "different-head",
		SchemaVersion: 3,
		IndexedAt:     time.Now().UTC(),
		Languages:     []string{"go"},
	}); err != nil {
		t.Fatal(err)
	}

	scope := refscope.Scope{
		Workspace: workspace,
		RepoRoot:  workspace,
		Path:      ".",
		Absolute:  workspace,
		Mode:      "explicit",
		Language:  "go",
		Detected:  []string{"go"},
		IsDir:     true,
	}

	got := Evaluate(ctx, storageRoot, scope)
	if got.Mode != ModeParserOnly {
		t.Fatalf("mode=%q want %q", got.Mode, ModeParserOnly)
	}
	if !containsReason(got.Reasons, ReasonRepoIndexHeadMismatch) {
		t.Fatalf("reasons=%v want %s", got.Reasons, ReasonRepoIndexHeadMismatch)
	}
}

func TestEvaluateParserOnlyWhenSchemaMismatches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, storageRoot, workspace := setupGitWorkspace(t, ctx)

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	head := gitHead(t, ctx, workspace)
	if err := store.SetMeta(ctx, repoindex.IndexMeta{
		RepoRoot:      workspace,
		HeadSHA:       head,
		SchemaVersion: repoindex.SchemaVersion() - 1,
		IndexedAt:     time.Now().UTC(),
		Languages:     []string{"go"},
	}); err != nil {
		t.Fatal(err)
	}
	db, closeDB, err := sqliteutil.OpenDBShared(ctx, store.Path(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	if _, err := db.ExecContext(ctx, `UPDATE index_meta SET value = ? WHERE key = 'schema_version'`, fmt.Sprintf("%d", repoindex.SchemaVersion()-1)); err != nil {
		t.Fatal(err)
	}

	scope := refscope.Scope{
		Workspace: workspace,
		RepoRoot:  workspace,
		Path:      ".",
		Absolute:  workspace,
		Mode:      "explicit",
		Language:  "go",
		Detected:  []string{"go"},
		IsDir:     true,
	}

	got := Evaluate(ctx, storageRoot, scope)
	if got.Mode != ModeParserOnly {
		t.Fatalf("mode=%q want %q", got.Mode, ModeParserOnly)
	}
	if !containsReason(got.Reasons, ReasonRepoIndexSchemaMismatch) {
		t.Fatalf("reasons=%v want %s", got.Reasons, ReasonRepoIndexSchemaMismatch)
	}
}

func TestEvaluateParserOnlyWhenLanguageNotIndexed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, storageRoot, workspace := setupGitWorkspace(t, ctx)

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	head := gitHead(t, ctx, workspace)
	if err := store.SetMeta(ctx, repoindex.IndexMeta{
		RepoRoot:      workspace,
		HeadSHA:       head,
		SchemaVersion: 3,
		IndexedAt:     time.Now().UTC(),
		Languages:     []string{"typescript"},
	}); err != nil {
		t.Fatal(err)
	}

	scope := refscope.Scope{
		Workspace: workspace,
		RepoRoot:  workspace,
		Path:      ".",
		Absolute:  workspace,
		Mode:      "explicit",
		Language:  "go",
		Detected:  []string{"go"},
		IsDir:     true,
	}

	got := Evaluate(ctx, storageRoot, scope)
	if got.Mode != ModeParserOnly {
		t.Fatalf("mode=%q want %q", got.Mode, ModeParserOnly)
	}
	if !containsReason(got.Reasons, ReasonScopeLanguageNotIndexed) {
		t.Fatalf("reasons=%v want %s", got.Reasons, ReasonScopeLanguageNotIndexed)
	}
}

func setupGitWorkspace(t *testing.T, ctx context.Context) (string, string, string) {
	t.Helper()

	root := t.TempDir()
	storageRoot := filepath.Join(root, "storage")
	workspace := filepath.Join(root, "repo")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, ctx, workspace, "init")
	runGit(t, ctx, workspace, "add", "main.go")
	runGit(t, ctx, workspace, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "init")
	return root, storageRoot, workspace
}

func gitHead(t *testing.T, ctx context.Context, workspace string) string {
	t.Helper()
	cmd := exec.CommandContext(ctx, "git", "-C", workspace, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return string(bytesTrimSpace(out))
}

func runGit(t *testing.T, ctx context.Context, workspace string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", workspace}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
}

func containsReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

func bytesTrimSpace(data []byte) []byte {
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
