package repoindex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTracePathFindsShortestStoredPath(t *testing.T) {
	_, qe, ids := setupNavigationStore(t)

	got, err := qe.TracePath(context.Background(), TracePathOptions{
		SrcID:    ids.testWriter,
		DstID:    ids.writeExport,
		MaxDepth: 4,
	})
	if err != nil {
		t.Fatalf("trace path: %v", err)
	}
	if !got.Found || got.PathLen != 1 {
		t.Fatalf("TracePath found=%t path_len=%d want found path_len=1", got.Found, got.PathLen)
	}
	if len(got.Nodes) != 2 || got.Nodes[0].ID != ids.testWriter || got.Nodes[1].ID != ids.writeExport {
		t.Fatalf("TracePath nodes=%v want writer -> write export", nodeIDs(got.Nodes))
	}
	if len(got.Edges) != 1 || got.Edges[0].Type != EdgeCalls {
		t.Fatalf("TracePath edges=%+v want one CALLS edge", got.Edges)
	}
}

func TestSmartContextReturnsStableNamedSections(t *testing.T) {
	_, qe, ids := setupNavigationStore(t)

	got, err := qe.SmartContext(context.Background(), SmartContextOptions{
		NodeID: ids.buildExport,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("smart context: %v", err)
	}
	if got.Node.ID != ids.buildExport {
		t.Fatalf("SmartContext node=%s want %s", got.Node.ID, ids.buildExport)
	}
	sections := sectionsByName(got.Sections)
	assertSectionHasNode(t, sections["contains_in"], ids.exportFile)
	assertSectionHasEdge(t, sections["callees"], EdgeCalls, ids.buildExport, ids.buildCSV)
	assertSectionHasEdge(t, sections["callers"], EdgeCalls, ids.testBuilder, ids.buildExport)
	assertSectionHasEdge(t, sections["docs_concepts"], EdgeDescribedBy, ids.buildExport, ids.exportConcept)
	assertSectionHasEdge(t, sections["co_changes"], EdgeCoChangesWith, ids.buildExport, ids.writeExport)
}

func TestBlastRadiusReturnsBoundedForwardGraphAndIncomingCalls(t *testing.T) {
	_, qe, ids := setupNavigationStore(t)

	got, err := qe.BlastRadius(context.Background(), BlastRadiusOptions{
		NodeID:   ids.exportFile,
		MaxDepth: 2,
		Limit:    20,
	})
	if err != nil {
		t.Fatalf("blast radius: %v", err)
	}
	if got.Origin.ID != ids.exportFile {
		t.Fatalf("BlastRadius origin=%s want %s", got.Origin.ID, ids.exportFile)
	}
	assertNodeIDsContain(t, got.Graph.Nodes, ids.exportFile, ids.buildExport, ids.buildCSV)
	assertEdgesContain(t, got.Graph.Edges, EdgeContains, ids.exportFile, ids.buildExport)
	assertEdgesContain(t, got.Graph.Edges, EdgeCalls, ids.buildExport, ids.buildCSV)
	if got.Layers[ids.exportFile] != 0 || got.Layers[ids.buildExport] != 1 || got.Layers[ids.buildCSV] != 2 {
		t.Fatalf("layers=%v want file=0 build=1 csv=2", got.Layers)
	}
	sections := sectionsByName(got.Sections)
	assertSectionHasEdge(t, sections["incoming_call"], EdgeCalls, ids.testBuilder, ids.exportFile)
}

func TestBlastRadiusDoesNotReturnEdgesBeyondNodeLimit(t *testing.T) {
	_, qe, ids := setupNavigationStore(t)

	got, err := qe.BlastRadius(context.Background(), BlastRadiusOptions{
		NodeID:   ids.buildExport,
		MaxDepth: 1,
		Limit:    2,
	})
	if err != nil {
		t.Fatalf("blast radius: %v", err)
	}
	if len(got.Graph.Nodes) != 2 {
		t.Fatalf("nodes=%v want origin plus one reached node", nodeIDs(got.Graph.Nodes))
	}
	inGraph := make(map[string]struct{}, len(got.Graph.Nodes))
	for _, node := range got.Graph.Nodes {
		inGraph[node.ID] = struct{}{}
	}
	for _, edge := range got.Graph.Edges {
		if _, ok := inGraph[edge.Src]; !ok {
			t.Fatalf("edge %+v has source outside returned graph nodes %v", edge, nodeIDs(got.Graph.Nodes))
		}
		if _, ok := inGraph[edge.Dst]; !ok {
			t.Fatalf("edge %+v has destination outside returned graph nodes %v", edge, nodeIDs(got.Graph.Nodes))
		}
	}
}

type navigationIDs struct {
	exportFile    string
	buildExport   string
	buildCSV      string
	writeExport   string
	testBuilder   string
	testWriter    string
	exportConcept string
}

func setupNavigationStore(t *testing.T) (*Store, *QueryEngine, navigationIDs) {
	t.Helper()
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
	t.Cleanup(func() { _ = store.Close() })

	key := repoKey(repoRoot)
	pkg := "go:internal/intelligence/indexing/repoindex"
	ids := navigationIDs{
		exportFile:    FileID(key, pkg, "ladybug_export.go"),
		buildExport:   SymbolID(key, pkg, "ladybug_export.go/BuildLadybugExport"),
		buildCSV:      SymbolID(key, pkg, "ladybug_export.go/buildLadybugGraphNodeCSV"),
		writeExport:   SymbolID(key, pkg, "ladybug_export_writer.go/WriteLadybugExport"),
		testBuilder:   SymbolID(key, pkg, "ladybug_export_test.go/TestBuildLadybugExport"),
		testWriter:    SymbolID(key, pkg, "ladybug_export_test.go/TestWriteLadybugExportReplaceAll"),
		exportConcept: NamespacedID(key, "concept:doc:ladybug-export"),
	}
	now := time.Now().UTC()
	nodes := []Node{
		{ID: ids.exportFile, Kind: NodeFile, Pkg: pkg, File: "ladybug_export.go", Name: "ladybug_export.go", SpanStart: 1, SpanEnd: 200, UpdatedAt: now},
		{ID: ids.buildExport, Kind: NodeSymbol, Pkg: pkg, File: "ladybug_export.go", Name: "BuildLadybugExport", SpanStart: 30, SpanEnd: 90, UpdatedAt: now},
		{ID: ids.buildCSV, Kind: NodeSymbol, Pkg: pkg, File: "ladybug_export.go", Name: "buildLadybugGraphNodeCSV", SpanStart: 120, SpanEnd: 180, UpdatedAt: now},
		{ID: ids.writeExport, Kind: NodeSymbol, Pkg: pkg, File: "ladybug_export_writer.go", Name: "WriteLadybugExport", SpanStart: 10, SpanEnd: 70, UpdatedAt: now},
		{ID: ids.testBuilder, Kind: NodeSymbol, Pkg: pkg, File: "ladybug_export_test.go", Name: "TestBuildLadybugExport", SpanStart: 15, SpanEnd: 50, UpdatedAt: now},
		{ID: ids.testWriter, Kind: NodeSymbol, Pkg: pkg, File: "ladybug_export_test.go", Name: "TestWriteLadybugExportReplaceAll", SpanStart: 70, SpanEnd: 110, UpdatedAt: now},
		{ID: ids.exportConcept, Kind: NodeConcept, Name: "ladybug export", UpdatedAt: now},
	}
	edges := []Edge{
		{Src: ids.exportFile, Dst: ids.buildExport, Type: EdgeContains, Weight: 1},
		{Src: ids.buildExport, Dst: ids.buildCSV, Type: EdgeCalls, Weight: 1},
		{Src: ids.testBuilder, Dst: ids.buildExport, Type: EdgeCalls, Weight: 1},
		{Src: ids.testBuilder, Dst: ids.exportFile, Type: EdgeCalls, Weight: 1},
		{Src: ids.testWriter, Dst: ids.writeExport, Type: EdgeCalls, Weight: 1},
		{Src: ids.buildExport, Dst: ids.exportConcept, Type: EdgeDescribedBy, Weight: 0.8},
		{Src: ids.buildExport, Dst: ids.writeExport, Type: EdgeCoChangesWith, Weight: 0.5},
	}
	if err := store.ReplaceAll(ctx, nodes, edges); err != nil {
		t.Fatalf("replace all: %v", err)
	}

	return store, NewQueryEngine(store), ids
}

func sectionsByName(sections []ContextSection) map[string]ContextSection {
	out := make(map[string]ContextSection, len(sections))
	for _, section := range sections {
		out[section.Name] = section
	}
	return out
}

func assertSectionHasNode(t *testing.T, section ContextSection, id string) {
	t.Helper()
	assertNodeIDsContain(t, section.Nodes, id)
}

func assertSectionHasEdge(t *testing.T, section ContextSection, edgeType EdgeType, src, dst string) {
	t.Helper()
	assertEdgesContain(t, section.Edges, edgeType, src, dst)
}

func assertNodeIDsContain(t *testing.T, nodes []Node, wantIDs ...string) {
	t.Helper()
	got := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		got[node.ID] = struct{}{}
	}
	for _, want := range wantIDs {
		if _, ok := got[want]; !ok {
			t.Fatalf("nodes=%v missing %s", nodeIDs(nodes), want)
		}
	}
}

func assertEdgesContain(t *testing.T, edges []Edge, edgeType EdgeType, src, dst string) {
	t.Helper()
	for _, edge := range edges {
		if edge.Type == edgeType && edge.Src == src && edge.Dst == dst {
			return
		}
	}
	t.Fatalf("edges=%+v missing %s %s -> %s", edges, edgeType, src, dst)
}

func nodeIDs(nodes []Node) []string {
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node.ID)
	}
	return out
}
