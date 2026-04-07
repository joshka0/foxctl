package changes

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing"
	refscope "github.com/jkatigb/agentctl/internal/refactor/scope"
	refsnapshot "github.com/jkatigb/agentctl/internal/refactor/snapshot"
	refsnapshotstore "github.com/jkatigb/agentctl/internal/refactor/snapshotstore"
	"github.com/jkatigb/agentctl/internal/storage/cas"
)

func TestDiffSnapshotsDetectsAddedModifiedDeletedFilesAndSymbols(t *testing.T) {
	t.Parallel()

	prev := refsnapshot.Payload{
		Files: []refsnapshot.FileSnapshot{
			{Path: "a.go", Language: "go", Hash: "sha256:a1"},
			{Path: "b.go", Language: "go", Hash: "sha256:b1"},
		},
		Symbols: []refsnapshot.SymbolSnapshot{
			{Path: "a.go", SymbolID: "a:Alpha", Name: "Alpha", Kind: "function", Hash: "sha256:s1"},
			{Path: "b.go", SymbolID: "b:Beta", Name: "Beta", Kind: "function", Hash: "sha256:s2"},
		},
	}
	curr := refsnapshot.Payload{
		Files: []refsnapshot.FileSnapshot{
			{Path: "a.go", Language: "go", Hash: "sha256:a2"},
			{Path: "c.go", Language: "go", Hash: "sha256:c1"},
		},
		Symbols: []refsnapshot.SymbolSnapshot{
			{Path: "a.go", SymbolID: "a:Alpha", Name: "Alpha", Kind: "function", Hash: "sha256:s3"},
			{Path: "c.go", SymbolID: "c:Gamma", Name: "Gamma", Kind: "function", Hash: "sha256:s4"},
		},
	}

	files, symbols, summary := diffSnapshots(prev, curr, 20, 20)
	if got, want := summary.FileCount, 3; got != want {
		t.Fatalf("file_count=%d want %d", got, want)
	}
	if got, want := summary.SymbolCount, 3; got != want {
		t.Fatalf("symbol_count=%d want %d", got, want)
	}
	wantFileKinds := map[string]indexing.ChangeKind{
		"a.go": indexing.ChangeKindModified,
		"b.go": indexing.ChangeKindDeleted,
		"c.go": indexing.ChangeKindAdded,
	}
	for _, file := range files {
		if wantFileKinds[file.Path] != file.ChangeKind {
			t.Fatalf("file %s kind=%q want %q", file.Path, file.ChangeKind, wantFileKinds[file.Path])
		}
	}
	wantSymbolKinds := map[string]indexing.ChangeKind{
		"a:Alpha": indexing.ChangeKindModified,
		"b:Beta":  indexing.ChangeKindDeleted,
		"c:Gamma": indexing.ChangeKindAdded,
	}
	for _, symbol := range symbols {
		if wantSymbolKinds[symbol.SymbolID] != symbol.ChangeKind {
			t.Fatalf("symbol %s kind=%q want %q", symbol.SymbolID, symbol.ChangeKind, wantSymbolKinds[symbol.SymbolID])
		}
	}
}

func TestCollectGitFileChangesFiltersScopeAndLanguage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := t.TempDir()
	mustWriteFile(t, filepath.Join(repo, "internal", "one.go"), "package internal\n")
	mustWriteFile(t, filepath.Join(repo, "web", "app.ts"), "export const app = 1;\n")
	runGit(t, ctx, repo, "init")
	runGit(t, ctx, repo, "add", ".")
	runGit(t, ctx, repo, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "init")

	mustWriteFile(t, filepath.Join(repo, "internal", "one.go"), "package internal\n\nfunc One() {}\n")
	mustWriteFile(t, filepath.Join(repo, "internal", "two.go"), "package internal\n\nfunc Two() {}\n")
	mustWriteFile(t, filepath.Join(repo, "web", "app.ts"), "export const app = 2;\n")

	changes, err := collectGitFileChanges(ctx, refscope.Scope{
		Workspace: repo,
		RepoRoot:  repo,
		Path:      "internal",
		Absolute:  filepath.Join(repo, "internal"),
		Mode:      "explicit",
		Language:  "go",
		Detected:  []string{"go"},
		IsDir:     true,
	}, false, "HEAD")
	if err != nil {
		t.Fatalf("collectGitFileChanges: %v", err)
	}
	if got, want := len(changes), 2; got != want {
		t.Fatalf("change count=%d want %d (%#v)", got, want, changes)
	}
	want := map[string]indexing.ChangeKind{
		"internal/one.go": indexing.ChangeKindModified,
		"internal/two.go": indexing.ChangeKindAdded,
	}
	for _, change := range changes {
		if want[change.Path] != change.ChangeKind {
			t.Fatalf("path=%q kind=%q want %q", change.Path, change.ChangeKind, want[change.Path])
		}
	}
}

func TestBuildSnapshotSinceReadsStoredArtifact(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	storageRoot := filepath.Join(root, "storage")
	casRoot := filepath.Join(root, "cas")
	workspace := filepath.Join(root, "repo")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\n\nfunc Next() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	previous := refsnapshot.Payload{
		SnapshotID: "refsnap-1",
		Files: []refsnapshot.FileSnapshot{
			{Path: "main.go", Language: "go", Hash: "sha256:old"},
		},
		Symbols: []refsnapshot.SymbolSnapshot{
			{Path: "main.go", SymbolID: "main.go:Old", Name: "Old", Kind: "function", Hash: "sha256:oldsym"},
		},
	}
	digest := persistSnapshotPayload(t, ctx, casRoot, previous)
	persistSnapshotRecord(t, ctx, storageRoot, digest)

	result, err := Build(ctx, storageRoot, casRoot, time.Date(2026, 4, 1, 13, 0, 0, 0, time.UTC), Options{
		Scope: refscope.Scope{
			Workspace: workspace,
			RepoRoot:  workspace,
			Path:      ".",
			Absolute:  workspace,
			Mode:      "explicit",
			Language:  "go",
			Detected:  []string{"go"},
			IsDir:     true,
		},
		Since:      "refsnap-1",
		MaxFiles:   20,
		MaxSymbols: 20,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.Since.Kind != SinceKindSnapshot {
		t.Fatalf("since kind=%q want %q", result.Since.Kind, SinceKindSnapshot)
	}
	if result.Summary.FileCount == 0 {
		t.Fatal("expected file changes")
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runGit(t *testing.T, ctx context.Context, repo string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
}

func persistSnapshotPayload(t *testing.T, ctx context.Context, casRoot string, payload refsnapshot.Payload) string {
	t.Helper()
	store, err := cas.NewStore(casRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	obj, err := store.Put(ctx, bytes.NewReader(append(body, '\n')), "application/json", []string{"test-refactor-snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	return obj.Digest
}

func persistSnapshotRecord(t *testing.T, ctx context.Context, storageRoot, digest string) {
	t.Helper()
	store, err := refsnapshotstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Put(ctx, refsnapshotstore.Record{
		SnapshotID:     "refsnap-1",
		Workspace:      "/repo",
		RepoRoot:       "/repo",
		Path:           ".",
		Language:       "go",
		IncludeTests:   false,
		Mode:           "parser_only",
		ArtifactDigest: digest,
		FileCount:      1,
		SymbolCount:    1,
		CreatedAt:      time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
}
