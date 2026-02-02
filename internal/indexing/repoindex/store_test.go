package repoindex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreReplaceAllAndSearch(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storageRoot := filepath.Join(root, "storage")
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}

	store, err := Open(ctx, storageRoot, repoRoot)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	key := repoKey(repoRoot)
	pkg := "go:example"
	pkgNode := Node{
		ID:        PackageID(key, pkg),
		Kind:      NodePackage,
		Pkg:       pkg,
		Name:      "example",
		UpdatedAt: time.Now().UTC(),
	}
	fileNode := Node{
		ID:        FileID(key, pkg, "main.go"),
		Kind:      NodeFile,
		Pkg:       pkg,
		File:      "main.go",
		Name:      "main.go",
		SpanStart: 1,
		SpanEnd:   10,
		UpdatedAt: time.Now().UTC(),
	}

	edge := Edge{
		Src:    pkgNode.ID,
		Dst:    fileNode.ID,
		Type:   EdgeContains,
		Weight: 1.0,
	}

	if err := store.ReplaceAll(ctx, []Node{pkgNode, fileNode}, []Edge{edge}); err != nil {
		t.Fatalf("replace all: %v", err)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.NodesTotal != 2 {
		t.Fatalf("nodes total: expected 2, got %d", stats.NodesTotal)
	}
	if stats.EdgesTotal != 1 {
		t.Fatalf("edges total: expected 1, got %d", stats.EdgesTotal)
	}

	results, err := store.SearchFTS(ctx, "main", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected search results")
	}
	found := false
	for _, result := range results {
		if result.ID == fileNode.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected search result for node %s", fileNode.ID)
	}

	edges, err := store.GetOutgoingEdges(ctx, pkgNode.ID, []EdgeType{EdgeContains}, 5)
	if err != nil {
		t.Fatalf("outgoing edges: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
}
