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

func TestBuilderBuildDeltaFallsBackSafely(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	store, err := Open(ctx, storageRoot, repoRoot)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	builder := NewBuilder(store, repoRoot)
	delta := WorkspaceDelta{Modified: []string{"main.go"}}
	result, err := builder.BuildDelta(ctx, BuildOptions{
		RepoRoot:  repoRoot,
		IncludeGo: true,
		Patterns:  []string{"./..."},
	}, delta)
	if err != nil {
		t.Fatalf("build delta: %v", err)
	}
	if result.Mode != DeltaBuildModeFullFallback {
		t.Fatalf("mode=%q want %q", result.Mode, DeltaBuildModeFullFallback)
	}
	if !result.FullFallback {
		t.Fatal("expected full fallback")
	}
	if result.Reason == "" {
		t.Fatal("expected fallback reason")
	}
	if result.Result.Files == 0 || result.Result.Nodes == 0 {
		t.Fatalf("expected rebuilt graph stats, got %+v", result.Result)
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
