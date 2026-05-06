package repoindex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestStoreUpdateNodeSummaryRefreshesSearch(t *testing.T) {
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
	node := Node{
		ID:        FileID(key, "go:example", "main.go"),
		Kind:      NodeFile,
		Pkg:       "go:example",
		File:      "main.go",
		Name:      "main.go",
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.ReplaceAll(ctx, []Node{node}, nil); err != nil {
		t.Fatalf("replace all: %v", err)
	}

	if err := store.UpdateNodeSummary(ctx, node.ID, "semantic anchor enrichment target"); err != nil {
		t.Fatalf("update node summary: %v", err)
	}
	got, err := store.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if got.Summary != "semantic anchor enrichment target" {
		t.Fatalf("summary=%q", got.Summary)
	}
	results, err := store.SearchFTS(ctx, "enrichment", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].ID != node.ID {
		t.Fatalf("search results=%#v want node %s", results, node.ID)
	}
}

func TestOpenContextExtendsShortDeadlines(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	time.Sleep(40 * time.Millisecond)

	derived, derivedCancel := openContext(parent, 500*time.Millisecond)
	defer derivedCancel()

	parentDeadline, ok := parent.Deadline()
	if !ok {
		t.Fatalf("expected parent deadline")
	}
	derivedDeadline, ok := derived.Deadline()
	if !ok {
		t.Fatalf("expected derived deadline")
	}
	if !derivedDeadline.After(parentDeadline) {
		t.Fatalf("expected derived deadline %v to extend beyond parent deadline %v", derivedDeadline, parentDeadline)
	}
	if err := derived.Err(); err != nil {
		t.Fatalf("expected derived context to remain usable, got %v", err)
	}
}

func TestStoreMetaPersistsGitBaselineState(t *testing.T) {
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

	want := IndexMeta{
		RepoRoot:        repoRoot,
		HeadSHA:         "abc123",
		WorktreeDirty:   true,
		DirtyStatusHash: "dirty-hash",
		DefaultRef:      "origin/main",
		DefaultRefSHA:   "def456",
		MergeBaseSHA:    "base789",
		CommitsAhead:    2,
		CommitsBehind:   3,
		IndexedAt:       time.Unix(123, 0).UTC(),
		Languages:       []string{"go", "typescript"},
	}
	if err := store.SetMeta(ctx, want); err != nil {
		t.Fatalf("set meta: %v", err)
	}
	got, err := store.GetMeta(ctx)
	if err != nil {
		t.Fatalf("get meta: %v", err)
	}
	if got.HeadSHA != want.HeadSHA {
		t.Fatalf("head sha=%q want %q", got.HeadSHA, want.HeadSHA)
	}
	if !got.WorktreeDirty {
		t.Fatalf("worktree dirty=false want true")
	}
	if got.DirtyStatusHash != want.DirtyStatusHash || got.DefaultRef != want.DefaultRef || got.DefaultRefSHA != want.DefaultRefSHA || got.MergeBaseSHA != want.MergeBaseSHA {
		t.Fatalf("extended meta=%#v want %#v", got, want)
	}
	if got.CommitsAhead != want.CommitsAhead || got.CommitsBehind != want.CommitsBehind {
		t.Fatalf("ahead/behind=%d/%d want %d/%d", got.CommitsAhead, got.CommitsBehind, want.CommitsAhead, want.CommitsBehind)
	}
	if got.IndexedAt.Unix() != want.IndexedAt.Unix() {
		t.Fatalf("indexed_at=%v want %v", got.IndexedAt, want.IndexedAt)
	}
	if strings.Join(got.Languages, ",") != "go,typescript" {
		t.Fatalf("languages=%v", got.Languages)
	}
}
