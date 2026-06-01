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

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

func TestIndexRepoBuildEmitsHumanProgressOnStderrByDefault(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "repo")
	writeIndexRepoProgressFile(t, workspace, "main.tf", `resource "local_file" "demo" {
  filename = "demo.txt"
  content  = "demo"
}
`)
	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	cmd := newIndexRepoBuildCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetContext(config.WithContext(ctx, cfg))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"--workspace", workspace,
		"--go=false",
		"--typescript=false",
		"--terraform",
		"--dry-run",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("index repo build: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("stdout is not a JSON envelope: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if env.Status != envelope.StatusOK || env.Command != "index.repo.build" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	progress := stderr.String()
	for _, want := range []string{
		"repoindex build:",
		"phase=init",
		"families=terraform",
		"include_tests=false",
		"semantic_anchors=false",
		"incremental=true",
		"phase=config",
		"phase=storage",
		"phase=build",
		"phase=start",
		"phase=terraform",
		"phase=done",
		"packages=",
		"files=",
		"nodes=",
		"edges=",
	} {
		if !strings.Contains(progress, want) {
			t.Fatalf("stderr progress missing %q:\n%s", want, progress)
		}
	}
}

func TestIndexRepoBuildProgressCanBeDisabled(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "repo")
	writeIndexRepoProgressFile(t, workspace, "main.tf", `resource "local_file" "demo" {
  filename = "demo.txt"
  content  = "demo"
}
`)
	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	cmd := newIndexRepoBuildCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetContext(config.WithContext(ctx, cfg))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"--workspace", workspace,
		"--go=false",
		"--typescript=false",
		"--terraform",
		"--dry-run",
		"--progress=false",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("index repo build: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), "repoindex build:") {
		t.Fatalf("progress should be disabled, got stderr:\n%s", stderr.String())
	}
}

func TestIndexRepoBuildSkipsUnchangedDiffByDefault(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "repo")
	writeIndexRepoProgressFile(t, workspace, "main.tf", `resource "local_file" "demo" {
  filename = "demo.txt"
  content  = "demo"
}
`)
	runIndexRepoProgressGit(t, workspace, "init")
	runIndexRepoProgressGit(t, workspace, "config", "user.email", "test@example.invalid")
	runIndexRepoProgressGit(t, workspace, "config", "user.name", "Test User")
	runIndexRepoProgressGit(t, workspace, "add", ".")
	runIndexRepoProgressGit(t, workspace, "commit", "-m", "initial")

	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	store, err := repoindex.Open(ctx, cfg.Storage.Root, workspace)
	if err != nil {
		t.Fatalf("open repoindex store: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(workspace, "main.tf"))
	if err != nil {
		t.Fatalf("read main.tf: %v", err)
	}
	info, err := os.Stat(filepath.Join(workspace, "main.tf"))
	if err != nil {
		t.Fatalf("stat main.tf: %v", err)
	}
	snapshot := repoindex.ResolveGitSnapshot(ctx, workspace)
	if snapshot.HeadSHA == "" {
		t.Fatal("git snapshot missing HEAD")
	}
	now := time.Unix(123, 0).UTC()
	if err := store.ReplaceFileStates(ctx, []repoindex.FileState{{
		Path:            "main.tf",
		ContentHash:     symbol.ComputeDigest(content),
		SizeBytes:       info.Size(),
		MTimeUnix:       info.ModTime().Unix(),
		Language:        "terraform",
		IndexedAt:       now,
		LastSeenHeadSHA: snapshot.HeadSHA,
	}}); err != nil {
		t.Fatalf("replace file states: %v", err)
	}
	meta := repoindex.IndexMetaFromGitSnapshot(repoindex.IndexMeta{
		RepoRoot:  workspace,
		IndexedAt: now,
		Languages: []string{"terraform"},
	}, snapshot)
	if err := store.SetMeta(ctx, meta); err != nil {
		t.Fatalf("set meta: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	cmd := newIndexRepoBuildCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetContext(config.WithContext(ctx, cfg))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"--workspace", workspace,
		"--go=false",
		"--typescript=false",
		"--terraform",
		"--progress=false",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("index repo build: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("decode stdout: %v\nstdout:\n%s", err, stdout.String())
	}
	data := raw["data"].(map[string]any)
	if data["skipped"] != true {
		t.Fatalf("skipped=%v want true; stdout=%s", data["skipped"], stdout.String())
	}
	if got := data["reason"]; got != "workspace_diff_unchanged" {
		t.Fatalf("reason=%v want workspace_diff_unchanged", got)
	}
	if got := data["incremental"]; got != true {
		t.Fatalf("incremental=%v want true", got)
	}
}

func TestIndexRepoEnrichSummariesAppliesStoredSummaries(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "repo")
	writeIndexRepoProgressFile(t, workspace, "main.go", "package main\n\nfunc Run() {}\n")
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatalf("abs workspace: %v", err)
	}
	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	store, err := repoindex.Open(ctx, cfg.Storage.Root, workspace)
	if err != nil {
		t.Fatalf("open repoindex store: %v", err)
	}
	key := store.RepoKey()
	fileNode := repoindex.Node{
		ID:   repoindex.FileID(key, "go:example", "main.go"),
		Kind: repoindex.NodeFile,
		Pkg:  "go:example",
		File: "main.go",
		Name: "main.go",
	}
	symbolNode := repoindex.Node{
		ID:   repoindex.SymbolID(key, "go:example", "Run"),
		Kind: repoindex.NodeSymbol,
		Pkg:  "go:example",
		File: "main.go",
		Name: "Run",
	}
	if err := store.ReplaceAll(ctx, []repoindex.Node{fileNode, symbolNode}, nil); err != nil {
		t.Fatalf("replace all: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close repoindex store: %v", err)
	}

	casDir := filepath.Join(cfg.Home, "cas")
	memStore, err := memory.Open(ctx, cfg.Storage.Root, casDir)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	if _, err := memStore.Save(ctx, memory.NamedEntry{
		Name:      symbol.FileSummaryEntryName(absWorkspace, "main.go"),
		Type:      symbol.FileSummaryType,
		Workspace: absWorkspace,
		Summary:   "file summary from memory",
		Result:    []byte(`{}`),
	}); err != nil {
		t.Fatalf("save file summary: %v", err)
	}
	if _, err := memStore.Save(ctx, memory.NamedEntry{
		Name:      symbol.SymbolSummaryKeyEntryName(absWorkspace, "go:root", "Run"),
		Type:      symbol.SymbolSummaryType,
		Workspace: absWorkspace,
		Summary:   "symbol summary from memory",
		Result:    []byte(`{}`),
	}); err != nil {
		t.Fatalf("save symbol summary: %v", err)
	}
	if err := memStore.Close(); err != nil {
		t.Fatalf("close memory store: %v", err)
	}

	cmd := newIndexRepoEnrichSummariesCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetContext(config.WithContext(ctx, cfg))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--workspace", workspace})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("enrich summaries: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	store, err = repoindex.Open(ctx, cfg.Storage.Root, workspace)
	if err != nil {
		t.Fatalf("reopen repoindex store: %v", err)
	}
	defer store.Close()
	gotFile, err := store.GetNode(ctx, fileNode.ID)
	if err != nil {
		t.Fatalf("get file node: %v", err)
	}
	if gotFile.Summary != "file summary from memory" {
		t.Fatalf("file summary=%q", gotFile.Summary)
	}
	gotSymbol, err := store.GetNode(ctx, symbolNode.ID)
	if err != nil {
		t.Fatalf("get symbol node: %v", err)
	}
	if gotSymbol.Summary != "symbol summary from memory" {
		t.Fatalf("symbol summary=%q", gotSymbol.Summary)
	}
}

func TestRepoIndexBuildCanSkipWhenDiffUnchangedBehindDefaultRef(t *testing.T) {
	delta := repoindex.WorkspaceDelta{Unchanged: 3, Untracked: []string{"notes.md"}}
	meta := repoindex.IndexMeta{HeadSHA: "abc", WorktreeDirty: true, DirtyStatusHash: "dirty-a"}
	current := repoindex.GitSnapshot{HeadSHA: "abc", WorktreeDirty: true, DirtyStatusHash: "dirty-a", CommitsBehind: 2}
	if !repoIndexBuildCanSkip(delta, meta, current) {
		t.Fatal("expected unchanged workspace diff to skip even when default ref is ahead and untracked diff is stable")
	}
}

func TestRepoIndexBuildCanSkipRejectsChangedFingerprint(t *testing.T) {
	delta := repoindex.WorkspaceDelta{Unchanged: 3}
	meta := repoindex.IndexMeta{HeadSHA: "abc", WorktreeDirty: true, DirtyStatusHash: "dirty-a"}
	current := repoindex.GitSnapshot{HeadSHA: "abc", WorktreeDirty: true, DirtyStatusHash: "dirty-b"}
	if repoIndexBuildCanSkip(delta, meta, current) {
		t.Fatal("expected dirty-status hash change to force rebuild")
	}
	current = repoindex.GitSnapshot{HeadSHA: "def", WorktreeDirty: true, DirtyStatusHash: "dirty-a"}
	if repoIndexBuildCanSkip(delta, meta, current) {
		t.Fatal("expected head change to force rebuild")
	}
	current = repoindex.GitSnapshot{HeadSHA: "abc", WorktreeDirty: true, DirtyStatusHash: "dirty-a"}
	delta.Modified = []string{"main.go"}
	if repoIndexBuildCanSkip(delta, meta, current) {
		t.Fatal("expected file delta to force rebuild")
	}
}

func writeIndexRepoProgressFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runIndexRepoProgressGit(t *testing.T, workspace string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}
	cmd := exec.Command("git", append([]string{"-C", workspace}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(output))
	}
}
