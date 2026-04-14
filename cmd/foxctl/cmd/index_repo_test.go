package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/repoquery"
)

func TestRepoOpenFallbackQueriesExtractRepoRelativePath(t *testing.T) {
	t.Parallel()

	got := repoOpenFallbackQueries("/tmp/foxctl", "foxctl-repoindex-abc::sym:go:github.com/joshka0/foxctl/internal/agent/types/types.go")
	found := false
	for _, item := range got {
		if item == "internal/agent/types/types.go" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("queries=%v missing repo-relative path", got)
	}
}

func TestResolveRepoOpenFallbackIDFindsNodeByPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	storageRoot := filepath.Join(root, "storage")
	workspace := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(workspace, "internal", "agent", "types"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "internal", "agent", "types", "types.go"), []byte("package types\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repoKey := store.RepoKey()
	node := repoindex.Node{
		ID:   repoindex.FileID(repoKey, "go:github.com/joshka0/foxctl/internal/agent/types", "internal/agent/types/types.go"),
		Kind: repoindex.NodeFile,
		Pkg:  "go:github.com/joshka0/foxctl/internal/agent/types",
		File: "internal/agent/types/types.go",
		Name: "types.go",
	}
	if err := store.ReplaceAll(ctx, []repoindex.Node{node}, nil); err != nil {
		t.Fatal(err)
	}

	service := repoquery.NewQueryService(repoindex.NewQueryEngine(store))
	got, err := resolveRepoOpenFallbackID(ctx, workspace, service, "foxctl-repoindex-abc::sym:go:github.com/joshka0/foxctl/internal/agent/types/types.go")
	if err != nil {
		t.Fatalf("resolveRepoOpenFallbackID() error = %v", err)
	}
	if got != node.ID {
		t.Fatalf("resolved=%q want %q", got, node.ID)
	}
}
