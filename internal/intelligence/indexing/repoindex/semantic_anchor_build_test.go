package repoindex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuilderEmitsSemanticAnchorEdgesBehindFlag(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	writeSemanticAnchorFixture(t, workspace)

	store, err := Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	builder := NewBuilder(store, workspace)
	if _, err := builder.Build(ctx, BuildOptions{
		RepoRoot:               workspace,
		IncludeGo:              true,
		IncludeSemanticAnchors: true,
	}); err != nil {
		t.Fatalf("build: %v", err)
	}

	guard := findNodeByName(t, store, NodeSymbol, "Guard")
	semantic, err := store.GetOutgoingEdges(ctx, guard.ID, EdgeSetSemanticAnchors, 20)
	if err != nil {
		t.Fatalf("get semantic edges: %v", err)
	}
	assertEdgeTypePresent(t, semantic, EdgeEnforces)
	assertEdgeTypePresent(t, semantic, EdgeDescribedBy)
	assertEdgeTypePresent(t, semantic, EdgeVerifiedBy)
	assertNoExpandedEdge(t, semantic, EdgeBeaconFor)

	expanded, err := NewQueryEngine(store).Expand(ctx, []string{guard.ID}, ExpandOptions{})
	if err != nil {
		t.Fatalf("expand default: %v", err)
	}
	assertNoExpandedEdge(t, expanded.Edges, EdgeEnforces)

	expanded, err = NewQueryEngine(store).Expand(ctx, []string{guard.ID}, ExpandOptions{IncludeSemanticAnchors: true})
	if err != nil {
		t.Fatalf("expand with semantic anchors: %v", err)
	}
	assertHasExpandedEdge(t, expanded.Edges, EdgeEnforces)

	hits, err := NewQueryEngine(store).Search(ctx, "no-send-without-read", 10)
	if err != nil {
		t.Fatalf("search anchor target: %v", err)
	}
	anchorNode := findNodeInList(t, hits, NodeConcept, "no-send-without-read")
	incoming, err := NewQueryEngine(store).Expand(ctx, []string{anchorNode.ID}, ExpandOptions{
		Direction: DirIn,
		EdgeTypes: EdgeSetSemanticAnchors,
		Depth:     1,
		Budget:    20,
	})
	if err != nil {
		t.Fatalf("expand anchor incoming: %v", err)
	}
	assertHasExpandedEdge(t, incoming.Edges, EdgeEnforces)
	if !expandedNodesContain(incoming.Nodes, guard.ID) {
		t.Fatalf("incoming semantic traversal did not reach owner %s: %+v", guard.ID, incoming.Nodes)
	}
}

func TestBuilderSkipsSemanticAnchorEdgesByDefault(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	writeSemanticAnchorFixture(t, workspace)

	store, err := Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	builder := NewBuilder(store, workspace)
	if _, err := builder.Build(ctx, BuildOptions{
		RepoRoot:  workspace,
		IncludeGo: true,
	}); err != nil {
		t.Fatalf("build: %v", err)
	}

	guard := findNodeByName(t, store, NodeSymbol, "Guard")
	semantic, err := store.GetOutgoingEdges(ctx, guard.ID, EdgeSetSemanticAnchors, 20)
	if err != nil {
		t.Fatalf("get semantic edges: %v", err)
	}
	if len(semantic) != 0 {
		t.Fatalf("semantic edges emitted by default: %+v", semantic)
	}
}

func TestBuilderSkipsSemanticAnchorEdgesOnFileLevelLintError(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	writeDuplicateSemanticAnchorFixture(t, workspace)

	store, err := Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	builder := NewBuilder(store, workspace)
	if _, err := builder.Build(ctx, BuildOptions{
		RepoRoot:               workspace,
		IncludeGo:              true,
		IncludeSemanticAnchors: true,
	}); err != nil {
		t.Fatalf("build: %v", err)
	}

	guard := findNodeByName(t, store, NodeSymbol, "Guard")
	semantic, err := store.GetOutgoingEdges(ctx, guard.ID, EdgeSetSemanticAnchors, 20)
	if err != nil {
		t.Fatalf("get semantic edges: %v", err)
	}
	if len(semantic) != 0 {
		t.Fatalf("semantic edges emitted despite file-level lint error: %+v", semantic)
	}
}

func writeSemanticAnchorFixture(t *testing.T, workspace string) {
	t.Helper()
	mustWriteFile(t, filepath.Join(workspace, "go.mod"), "module example.com/anchors\n\ngo 1.22\n")
	mustWriteFile(t, filepath.Join(workspace, "docs", "anchors.md"), "# Overview\n\nAnchor docs.\n")
	mustWriteFile(t, filepath.Join(workspace, "internal", "demo", "demo_test.go"), `package demo

import "testing"

func TestGuard(t *testing.T) {}
`)
	mustWriteFile(t, filepath.Join(workspace, "internal", "demo", "demo.go"), `package demo

// [[foxctl:invariant/no-send-without-read]]
// [[doc:docs/anchors.md#Overview]]
// [[test:internal/demo/demo_test.go#TestGuard]]
// [[foxctl:beacon/agent-terminal-safety]]
func Guard() {}
`)
}

func writeDuplicateSemanticAnchorFixture(t *testing.T, workspace string) {
	t.Helper()
	mustWriteFile(t, filepath.Join(workspace, "go.mod"), "module example.com/anchors\n\ngo 1.22\n")
	mustWriteFile(t, filepath.Join(workspace, "internal", "demo", "demo.go"), `package demo

// [[foxctl:invariant/no-send-without-read]]
// [[foxctl:invariant/no-send-without-read]]
func Guard() {}
`)
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func findNodeByName(t *testing.T, store *Store, kind NodeKind, name string) Node {
	t.Helper()
	nodes, err := store.ListNodesByKind(context.Background(), kind, 200)
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	for _, node := range nodes {
		if node.Name == name {
			return node
		}
	}
	t.Fatalf("missing %s node named %q in %+v", kind, name, nodes)
	return Node{}
}

func findNodeInList(t *testing.T, nodes []Node, kind NodeKind, name string) Node {
	t.Helper()
	for _, node := range nodes {
		if node.Kind == kind && node.Name == name {
			return node
		}
	}
	t.Fatalf("missing %s node named %q in %+v", kind, name, nodes)
	return Node{}
}

func expandedNodesContain(nodes []Node, id string) bool {
	for _, node := range nodes {
		if node.ID == id {
			return true
		}
	}
	return false
}

func assertEdgeTypePresent(t *testing.T, edges []Edge, want EdgeType) {
	t.Helper()
	for _, edge := range edges {
		if edge.Type == want {
			if _, present, err := DecodeAndValidateSemanticAnchorEdge(edge); err != nil || !present {
				t.Fatalf("edge %s failed semantic validation: present=%v err=%v", want, present, err)
			}
			return
		}
	}
	t.Fatalf("missing edge type %s in %+v", want, edges)
}
