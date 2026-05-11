package repoindex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestStoreListAllEdgesReturnsUncappedOrderedEdges(t *testing.T) {
	ctx := context.Background()
	store, _ := openExportSnapshotTestStore(t, ctx)
	defer store.Close()

	repoKey := store.RepoKey()
	pkg := "go:example"
	updatedAt := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	src := Node{
		ID:        FileID(repoKey, pkg, "source.go"),
		Kind:      NodeFile,
		Pkg:       pkg,
		File:      "source.go",
		Name:      "source.go",
		UpdatedAt: updatedAt,
	}

	nodes := []Node{src}
	edges := make([]Edge, 0, 105)
	for i := 104; i >= 0; i-- {
		name := fmt.Sprintf("dst-%03d.go", i)
		dst := Node{
			ID:        FileID(repoKey, pkg, name),
			Kind:      NodeFile,
			Pkg:       pkg,
			File:      name,
			Name:      name,
			UpdatedAt: updatedAt,
		}
		nodes = append(nodes, dst)
		edges = append(edges, Edge{
			Src:    src.ID,
			Dst:    dst.ID,
			Type:   EdgeCalls,
			Weight: float64(i),
		})
	}
	if err := store.ReplaceAll(ctx, nodes, edges); err != nil {
		t.Fatalf("replace all: %v", err)
	}

	got, err := store.ListAllEdges(ctx)
	if err != nil {
		t.Fatalf("list all edges: %v", err)
	}
	if len(got) != len(edges) {
		t.Fatalf("edges=%d want %d", len(got), len(edges))
	}
	for i, edge := range got {
		wantDst := FileID(repoKey, pkg, fmt.Sprintf("dst-%03d.go", i))
		if edge.Type != EdgeCalls || edge.Src != src.ID || edge.Dst != wantDst {
			t.Fatalf("edge[%d]=%+v want type=%s src=%s dst=%s", i, edge, EdgeCalls, src.ID, wantDst)
		}
	}
}

func TestStoreExportSnapshotIncludesRepoindexState(t *testing.T) {
	ctx := context.Background()
	store, repoRoot := openExportSnapshotTestStore(t, ctx)
	defer store.Close()

	repoKey := store.RepoKey()
	pkg := "go:example"
	indexedAt := time.Date(2026, 5, 11, 13, 30, 0, 0, time.UTC)
	meta := IndexMeta{
		RepoRoot:        repoRoot,
		HeadSHA:         "abc123",
		WorktreeDirty:   true,
		DirtyStatusHash: "dirty-hash",
		SchemaVersion:   SchemaVersion(),
		IndexedAt:       indexedAt,
		Languages:       []string{"go"},
	}
	if err := store.SetMeta(ctx, meta); err != nil {
		t.Fatalf("set meta: %v", err)
	}
	if err := store.ReplaceFileStates(ctx, []FileState{
		{
			Path:        "b.go",
			ContentHash: "hash-b",
			SizeBytes:   20,
			MTimeUnix:   200,
			Language:    "go",
			IndexedAt:   indexedAt,
		},
		{
			Path:        "a.go",
			ContentHash: "hash-a",
			SizeBytes:   10,
			MTimeUnix:   100,
			Language:    "go",
			IndexedAt:   indexedAt,
		},
	}); err != nil {
		t.Fatalf("replace file states: %v", err)
	}

	pkgNode := Node{ID: PackageID(repoKey, pkg), Kind: NodePackage, Pkg: pkg, Name: "example", UpdatedAt: indexedAt}
	fileNode := Node{ID: FileID(repoKey, pkg, "a.go"), Kind: NodeFile, Pkg: pkg, File: "a.go", Name: "a.go", UpdatedAt: indexedAt}
	symbolNode := Node{ID: SymbolID(repoKey, pkg, "A"), Kind: NodeSymbol, Pkg: pkg, File: "a.go", Name: "A", UpdatedAt: indexedAt}
	conceptNode := Node{ID: NamespacedID(repoKey, ConceptKeyword+"alpha"), Kind: NodeConcept, Name: "alpha", UpdatedAt: indexedAt}
	edges := []Edge{
		{Src: fileNode.ID, Dst: symbolNode.ID, Type: EdgeContains, Weight: 1},
		{Src: symbolNode.ID, Dst: conceptNode.ID, Type: EdgeHasKeyword, Weight: 0.5},
	}
	if err := store.ReplaceAll(ctx, []Node{conceptNode, symbolNode, fileNode, pkgNode}, edges); err != nil {
		t.Fatalf("replace all: %v", err)
	}

	snapshot, err := store.ExportSnapshot(ctx)
	if err != nil {
		t.Fatalf("export snapshot: %v", err)
	}
	if snapshot.RepoKey != repoKey {
		t.Fatalf("repo key=%q want %q", snapshot.RepoKey, repoKey)
	}
	if snapshot.Meta.HeadSHA != meta.HeadSHA || snapshot.Meta.DirtyStatusHash != meta.DirtyStatusHash {
		t.Fatalf("meta=%+v want head=%q dirty hash=%q", snapshot.Meta, meta.HeadSHA, meta.DirtyStatusHash)
	}
	if snapshot.Meta.IndexedAt.Unix() != indexedAt.Unix() {
		t.Fatalf("indexed at=%v want %v", snapshot.Meta.IndexedAt, indexedAt)
	}

	gotFilePaths := make([]string, 0, len(snapshot.FileStates))
	for _, state := range snapshot.FileStates {
		gotFilePaths = append(gotFilePaths, state.Path)
		if state.RepoKey != repoKey {
			t.Fatalf("file state repo key=%q want %q", state.RepoKey, repoKey)
		}
	}
	if want := []string{"a.go", "b.go"}; !reflect.DeepEqual(gotFilePaths, want) {
		t.Fatalf("file paths=%v want %v", gotFilePaths, want)
	}

	gotNodeIDs := make([]string, 0, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		gotNodeIDs = append(gotNodeIDs, node.ID)
	}
	wantNodeIDs := []string{pkgNode.ID, fileNode.ID, symbolNode.ID, conceptNode.ID}
	if !reflect.DeepEqual(gotNodeIDs, wantNodeIDs) {
		t.Fatalf("node ids=%v want %v", gotNodeIDs, wantNodeIDs)
	}

	gotEdgeTypes := make([]EdgeType, 0, len(snapshot.Edges))
	for _, edge := range snapshot.Edges {
		gotEdgeTypes = append(gotEdgeTypes, edge.Type)
	}
	if want := []EdgeType{EdgeContains, EdgeHasKeyword}; !reflect.DeepEqual(gotEdgeTypes, want) {
		t.Fatalf("edge types=%v want %v", gotEdgeTypes, want)
	}
}

func openExportSnapshotTestStore(t *testing.T, ctx context.Context) (*Store, string) {
	t.Helper()
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
	return store, repoRoot
}
