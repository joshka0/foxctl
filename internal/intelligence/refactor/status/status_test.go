package status

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/intelligence/indexing/repoindex"
	refscope "github.com/jkatigb/agentctl/internal/intelligence/refactor/scope"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
)

func TestEvaluateParserOnlyWhenIndexMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root, storageRoot, workspace := setupGitWorkspace(t, ctx)
	_ = root

	scope := refscope.Scope{
		Workspace:    workspace,
		RepoRoot:     workspace,
		Path:         ".",
		Absolute:     workspace,
		Mode:         "explicit",
		Language:     "go",
		Detected:     []string{"go"},
		IsDir:        true,
		IncludeTests: false,
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
	}, {
		ID:        repoindex.FileID(store.RepoKey(), "go:example", "main.go"),
		Kind:      repoindex.NodeFile,
		Pkg:       "go:example",
		File:      "main.go",
		Name:      "main.go",
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
	if !got.RepoIndex.ScopeCovered {
		t.Fatal("expected scope_covered=true")
	}
	if got.RepoIndex.Coverage.DiscoveredFileCount != 1 || got.RepoIndex.Coverage.IndexedFileCount != 1 || got.RepoIndex.Coverage.MatchedFileCount != 1 {
		t.Fatalf("coverage=%+v want discovered=1 indexed=1 matched=1", got.RepoIndex.Coverage)
	}
	if got.RepoIndex.Coverage.MissingFileCount != 0 || got.RepoIndex.Coverage.ExtraIndexedFileCount != 0 {
		t.Fatalf("coverage=%+v want no missing or extra files", got.RepoIndex.Coverage)
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
	defer func() { _ = closeDB() }()
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

func TestEvaluateParserOnlyWhenScopePathNotFullyIndexed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, storageRoot, workspace := setupGitWorkspace(t, ctx)
	if err := os.MkdirAll(filepath.Join(workspace, "internal", "runtime", "actor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "internal", "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "internal", "runtime", "actor", "actor.go"), []byte("package actor\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "internal", "other", "other.go"), []byte("package other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, ctx, workspace, "add", "internal/runtime/actor/actor.go", "internal/other/other.go")
	runGit(t, ctx, workspace, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "add internal files")

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
	if err := store.ReplaceAll(ctx, []repoindex.Node{
		{
			ID:        repoindex.PackageID(store.RepoKey(), "go:example/internal/runtime/actor"),
			Kind:      repoindex.NodePackage,
			Pkg:       "go:example/internal/runtime/actor",
			Name:      "internal/runtime/actor",
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        repoindex.FileID(store.RepoKey(), "go:example/internal/runtime/actor", "internal/runtime/actor/actor.go"),
			Kind:      repoindex.NodeFile,
			Pkg:       "go:example/internal/runtime/actor",
			File:      "internal/runtime/actor/actor.go",
			Name:      "actor.go",
			UpdatedAt: time.Now().UTC(),
		},
	}, nil); err != nil {
		t.Fatal(err)
	}

	internalScope := refscope.Scope{
		Workspace:    workspace,
		RepoRoot:     workspace,
		Path:         "internal",
		Absolute:     filepath.Join(workspace, "internal"),
		Mode:         "explicit",
		Language:     "go",
		Detected:     []string{"go"},
		IsDir:        true,
		IncludeTests: false,
	}
	gotInternal := Evaluate(ctx, storageRoot, internalScope)
	if gotInternal.Mode != ModeParserOnly {
		t.Fatalf("internal mode=%q want %q", gotInternal.Mode, ModeParserOnly)
	}
	if gotInternal.RepoIndex.ScopeCovered {
		t.Fatal("expected internal scope_covered=false")
	}
	if !containsReason(gotInternal.Reasons, ReasonScopePathNotIndexed) {
		t.Fatalf("internal reasons=%v want %s", gotInternal.Reasons, ReasonScopePathNotIndexed)
	}
	if gotInternal.RepoIndex.Coverage.DiscoveredFileCount != 2 || gotInternal.RepoIndex.Coverage.IndexedFileCount != 1 || gotInternal.RepoIndex.Coverage.MatchedFileCount != 1 {
		t.Fatalf("internal coverage=%+v want discovered=2 indexed=1 matched=1", gotInternal.RepoIndex.Coverage)
	}
	if gotInternal.RepoIndex.Coverage.MissingFileCount != 1 {
		t.Fatalf("internal coverage=%+v want missing=1", gotInternal.RepoIndex.Coverage)
	}
	if len(gotInternal.RepoIndex.Coverage.MissingFilesSample) != 1 || gotInternal.RepoIndex.Coverage.MissingFilesSample[0] != "internal/other/other.go" {
		t.Fatalf("internal missing_files_sample=%v want [internal/other/other.go]", gotInternal.RepoIndex.Coverage.MissingFilesSample)
	}

	actorScope := refscope.Scope{
		Workspace:    workspace,
		RepoRoot:     workspace,
		Path:         "internal/runtime/actor",
		Absolute:     filepath.Join(workspace, "internal", "runtime", "actor"),
		Mode:         "explicit",
		Language:     "go",
		Detected:     []string{"go"},
		IsDir:        true,
		IncludeTests: false,
	}
	gotActor := Evaluate(ctx, storageRoot, actorScope)
	if gotActor.Mode != ModeIndexBacked {
		t.Fatalf("actor mode=%q want %q (reasons=%v)", gotActor.Mode, ModeIndexBacked, gotActor.Reasons)
	}
	if !gotActor.RepoIndex.ScopeCovered {
		t.Fatal("expected actor scope_covered=true")
	}
	if gotActor.RepoIndex.Coverage.DiscoveredFileCount != 1 || gotActor.RepoIndex.Coverage.IndexedFileCount != 1 || gotActor.RepoIndex.Coverage.MatchedFileCount != 1 {
		t.Fatalf("actor coverage=%+v want discovered=1 indexed=1 matched=1", gotActor.RepoIndex.Coverage)
	}
}

func TestEvaluateCoverageIncludesTestsWhenRequested(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, storageRoot, workspace := setupGitWorkspace(t, ctx)
	if err := os.MkdirAll(filepath.Join(workspace, "internal", "runtime", "actor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "internal", "runtime", "actor", "actor.go"), []byte("package actor\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "internal", "runtime", "actor", "actor_test.go"), []byte("package actor\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, ctx, workspace, "add", "internal/runtime/actor/actor.go", "internal/runtime/actor/actor_test.go")
	runGit(t, ctx, workspace, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "add actor files")

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
	if err := store.ReplaceAll(ctx, []repoindex.Node{
		{
			ID:        repoindex.PackageID(store.RepoKey(), "go:example/internal/runtime/actor"),
			Kind:      repoindex.NodePackage,
			Pkg:       "go:example/internal/runtime/actor",
			Name:      "internal/runtime/actor",
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        repoindex.FileID(store.RepoKey(), "go:example/internal/runtime/actor", "internal/runtime/actor/actor.go"),
			Kind:      repoindex.NodeFile,
			Pkg:       "go:example/internal/runtime/actor",
			File:      "internal/runtime/actor/actor.go",
			Name:      "actor.go",
			UpdatedAt: time.Now().UTC(),
		},
	}, nil); err != nil {
		t.Fatal(err)
	}

	scopeWithoutTests := refscope.Scope{
		Workspace:    workspace,
		RepoRoot:     workspace,
		Path:         "internal/runtime/actor",
		Absolute:     filepath.Join(workspace, "internal", "runtime", "actor"),
		Mode:         "explicit",
		Language:     "go",
		Detected:     []string{"go"},
		IsDir:        true,
		IncludeTests: false,
	}
	gotWithoutTests := Evaluate(ctx, storageRoot, scopeWithoutTests)
	if gotWithoutTests.Mode != ModeIndexBacked {
		t.Fatalf("without tests mode=%q want %q", gotWithoutTests.Mode, ModeIndexBacked)
	}
	if gotWithoutTests.RepoIndex.Coverage.DiscoveredFileCount != 1 {
		t.Fatalf("without tests coverage=%+v want discovered=1", gotWithoutTests.RepoIndex.Coverage)
	}

	scopeWithTests := refscope.Scope{
		Workspace:    workspace,
		RepoRoot:     workspace,
		Path:         "internal/runtime/actor",
		Absolute:     filepath.Join(workspace, "internal", "runtime", "actor"),
		Mode:         "explicit",
		Language:     "go",
		Detected:     []string{"go"},
		IsDir:        true,
		IncludeTests: true,
	}
	gotWithTests := Evaluate(ctx, storageRoot, scopeWithTests)
	if gotWithTests.Mode != ModeParserOnly {
		t.Fatalf("with tests mode=%q want %q", gotWithTests.Mode, ModeParserOnly)
	}
	if !containsReason(gotWithTests.Reasons, ReasonScopePathNotIndexed) {
		t.Fatalf("with tests reasons=%v want %s", gotWithTests.Reasons, ReasonScopePathNotIndexed)
	}
	if gotWithTests.RepoIndex.Coverage.DiscoveredFileCount != 2 || gotWithTests.RepoIndex.Coverage.IndexedFileCount != 1 {
		t.Fatalf("with tests coverage=%+v want discovered=2 indexed=1", gotWithTests.RepoIndex.Coverage)
	}
	if gotWithTests.RepoIndex.Coverage.MissingFileCount != 1 {
		t.Fatalf("with tests coverage=%+v want missing=1", gotWithTests.RepoIndex.Coverage)
	}
	if len(gotWithTests.RepoIndex.Coverage.MissingFilesSample) != 1 || gotWithTests.RepoIndex.Coverage.MissingFilesSample[0] != "internal/runtime/actor/actor_test.go" {
		t.Fatalf("with tests missing_files_sample=%v want [internal/runtime/actor/actor_test.go]", gotWithTests.RepoIndex.Coverage.MissingFilesSample)
	}
}

func TestEvaluateRebasesToDeepestIndexedWorkspace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, storageRoot, workspace := setupGitWorkspace(t, ctx)
	appRoot := filepath.Join(workspace, "apps", "praze-api")
	libDir := filepath.Join(appRoot, "lib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "accounts.ex"), []byte("defmodule Praze.Accounts do\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, ctx, workspace, "add", "apps/praze-api/lib/accounts.ex")
	runGit(t, ctx, workspace, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "add nested elixir app")

	store, err := repoindex.Open(ctx, storageRoot, appRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	head := gitHead(t, ctx, workspace)
	if err := store.SetMeta(ctx, repoindex.IndexMeta{
		RepoRoot:      appRoot,
		HeadSHA:       head,
		SchemaVersion: repoindex.SchemaVersion(),
		IndexedAt:     time.Now().UTC(),
		Languages:     []string{"elixir"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceAll(ctx, []repoindex.Node{
		{
			ID:        repoindex.PackageID(store.RepoKey(), "ex:lib"),
			Kind:      repoindex.NodePackage,
			Pkg:       "ex:lib",
			Name:      "lib",
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        repoindex.FileID(store.RepoKey(), "ex:lib", "lib/accounts.ex"),
			Kind:      repoindex.NodeFile,
			Pkg:       "ex:lib",
			File:      "lib/accounts.ex",
			Name:      "accounts.ex",
			UpdatedAt: time.Now().UTC(),
		},
	}, nil); err != nil {
		t.Fatal(err)
	}

	scope := refscope.Scope{
		Workspace:    workspace,
		RepoRoot:     workspace,
		Path:         "apps/praze-api/lib",
		Absolute:     filepath.Join(workspace, "apps", "praze-api", "lib"),
		Mode:         "explicit",
		Language:     "elixir",
		Detected:     []string{"elixir"},
		IsDir:        true,
		IncludeTests: false,
	}

	got := Evaluate(ctx, storageRoot, scope)
	if got.Mode != ModeIndexBacked {
		t.Fatalf("mode=%q want %q (reasons=%v)", got.Mode, ModeIndexBacked, got.Reasons)
	}
	if got.Scope.Workspace != appRoot {
		t.Fatalf("workspace=%q want %q", got.Scope.Workspace, appRoot)
	}
	if got.Scope.RepoRoot != appRoot {
		t.Fatalf("repo_root=%q want %q", got.Scope.RepoRoot, appRoot)
	}
	if got.Scope.Path != "lib" {
		t.Fatalf("path=%q want lib", got.Scope.Path)
	}
	if !got.RepoIndex.ScopeCovered {
		t.Fatal("expected scope_covered=true")
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
