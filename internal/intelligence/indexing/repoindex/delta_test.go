package repoindex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
)

func TestStoreComputeDeltaFromFileState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storageRoot := filepath.Join(root, "storage")
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "same.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write same: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "changed.go"), []byte("package main\nconst Changed = true\n"), 0o644); err != nil {
		t.Fatalf("write changed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "new.go"), []byte("package main\nconst New = true\n"), 0o644); err != nil {
		t.Fatalf("write new: %v", err)
	}

	store, err := Open(ctx, storageRoot, repoRoot)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	sameState, ok := fileStateForPath(repoRoot, "same.go", "head-a")
	if !ok {
		t.Fatal("same state missing")
	}
	if err := store.ReplaceFileStates(ctx, []FileState{
		sameState,
		{
			Path:            "changed.go",
			ContentHash:     symbol.ComputeDigest([]byte("old content")),
			SizeBytes:       11,
			MTimeUnix:       time.Now().Unix(),
			Language:        "go",
			IndexedAt:       time.Now().UTC(),
			LastSeenHeadSHA: "head-a",
		},
		{
			Path:            "deleted.go",
			ContentHash:     symbol.ComputeDigest([]byte("deleted")),
			SizeBytes:       7,
			MTimeUnix:       time.Now().Unix(),
			Language:        "go",
			IndexedAt:       time.Now().UTC(),
			LastSeenHeadSHA: "head-a",
		},
	}); err != nil {
		t.Fatalf("replace file states: %v", err)
	}

	delta, err := store.ComputeDelta(ctx)
	if err != nil {
		t.Fatalf("compute delta: %v", err)
	}
	if !containsDeltaPath(delta.Modified, "changed.go") {
		t.Fatalf("modified=%v missing changed.go", delta.Modified)
	}
	if !containsDeltaPath(delta.Deleted, "deleted.go") {
		t.Fatalf("deleted=%v missing deleted.go", delta.Deleted)
	}
	if delta.Unchanged != 1 {
		t.Fatalf("unchanged=%d want 1", delta.Unchanged)
	}
	if containsDeltaPath(delta.Modified, "same.go") {
		t.Fatalf("modified=%v should not include same.go", delta.Modified)
	}
}

func TestBuilderBuildDeltaPatchesModifiedFile(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storageRoot := filepath.Join(root, "storage")
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte("module example.com/delta\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n\nfunc main() { OnlyOld() }\n\nfunc OnlyOld() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	store, err := Open(ctx, storageRoot, repoRoot)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	builder := NewBuilder(store, repoRoot)
	if _, err := builder.Build(ctx, BuildOptions{
		RepoRoot:  repoRoot,
		IncludeGo: true,
		Patterns:  []string{"./..."},
	}); err != nil {
		t.Fatalf("initial build: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n\nfunc main() { Extra() }\n\nfunc Extra() {}\n"), 0o644); err != nil {
		t.Fatalf("rewrite main.go: %v", err)
	}
	delta, err := store.ComputeDelta(ctx)
	if err != nil {
		t.Fatalf("compute delta: %v", err)
	}
	if !containsDeltaPath(delta.Modified, "main.go") {
		t.Fatalf("modified=%v missing main.go", delta.Modified)
	}
	result, err := builder.BuildDelta(ctx, BuildOptions{
		RepoRoot:  repoRoot,
		IncludeGo: true,
		Patterns:  []string{"./..."},
	}, delta)
	if err != nil {
		t.Fatalf("build delta: %v", err)
	}
	if result.Mode != DeltaBuildModeIncremental {
		t.Fatalf("mode=%q want %q", result.Mode, DeltaBuildModeIncremental)
	}
	if result.FullFallback {
		t.Fatal("did not expect full fallback")
	}
	if result.Result.Files == 0 || result.Result.Nodes == 0 {
		t.Fatalf("expected rebuilt graph stats, got %+v", result.Result)
	}
	hits, err := store.SearchFTS(ctx, "Extra", 10)
	if err != nil {
		t.Fatalf("search Extra: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected Extra after incremental patch")
	}
	oldHits, err := store.SearchFTS(ctx, "OnlyOld", 10)
	if err != nil {
		t.Fatalf("search OnlyOld: %v", err)
	}
	if len(oldHits) != 0 {
		t.Fatalf("old symbol survived incremental patch: %+v", oldHits)
	}
	nextDelta, err := store.ComputeDelta(ctx)
	if err != nil {
		t.Fatalf("compute next delta: %v", err)
	}
	if !nextDelta.Empty() {
		t.Fatalf("next delta=%+v want empty", nextDelta)
	}
}

func TestBuilderBuildDeltaRemovesDeletedFile(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storageRoot := filepath.Join(root, "storage")
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte("module example.com/delete_delta\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "removed.go"), []byte("package main\n\nfunc Removed() {}\n"), 0o644); err != nil {
		t.Fatalf("write removed.go: %v", err)
	}

	store, err := Open(ctx, storageRoot, repoRoot)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	builder := NewBuilder(store, repoRoot)
	if _, err := builder.Build(ctx, BuildOptions{
		RepoRoot:  repoRoot,
		IncludeGo: true,
		Patterns:  []string{"./..."},
	}); err != nil {
		t.Fatalf("initial build: %v", err)
	}
	if err := os.Remove(filepath.Join(repoRoot, "removed.go")); err != nil {
		t.Fatalf("remove file: %v", err)
	}
	delta, err := store.ComputeDelta(ctx)
	if err != nil {
		t.Fatalf("compute delta: %v", err)
	}
	if !containsDeltaPath(delta.Deleted, "removed.go") {
		t.Fatalf("deleted=%v missing removed.go", delta.Deleted)
	}
	result, err := builder.BuildDelta(ctx, BuildOptions{
		RepoRoot:  repoRoot,
		IncludeGo: true,
		Patterns:  []string{"./..."},
	}, delta)
	if err != nil {
		t.Fatalf("build delta: %v", err)
	}
	if result.Mode != DeltaBuildModeIncremental {
		t.Fatalf("mode=%q want %q", result.Mode, DeltaBuildModeIncremental)
	}
	hits, err := store.SearchFTS(ctx, "Removed", 10)
	if err != nil {
		t.Fatalf("search Removed: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("deleted file symbols survived incremental patch: %+v", hits)
	}
}

func TestBuilderBuildDeltaFallsBackWithoutIndexedFileState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storageRoot := filepath.Join(root, "storage")
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte("module example.com/bootstrap_delta\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	store, err := Open(ctx, storageRoot, repoRoot)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	result, err := NewBuilder(store, repoRoot).BuildDelta(ctx, BuildOptions{
		RepoRoot:  repoRoot,
		IncludeGo: true,
		Patterns:  []string{"./..."},
	}, WorkspaceDelta{Modified: []string{"main.go"}})
	if err != nil {
		t.Fatalf("build delta: %v", err)
	}
	if result.Mode != DeltaBuildModeFullFallback || !result.FullFallback || result.Reason != deltaNoIndexedFileStateReason {
		t.Fatalf("result=%+v want no-state full fallback", result)
	}
}

func containsDeltaPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
