package repoindex

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/semanticanchors"
)

func TestEdgeSetsKeepBeaconOutOfEmpiricalSet(t *testing.T) {
	assertNoEdgeType(t, EdgeSetEmpirical, EdgeBeaconFor)
	assertHasEdgeType(t, EdgeSetEmpirical, EdgeCoChangesWith)
}

func TestSemanticAnchorRepoindexHelpers(t *testing.T) {
	edgeType, ok := EdgeTypeForSemanticAnchorRelation(semanticanchors.SemanticAnchorRelationEnforces)
	if !ok || edgeType != EdgeEnforces {
		t.Fatalf("EdgeTypeForSemanticAnchorRelation(ENFORCES)=(%q,%v), want %q,true", edgeType, ok, EdgeEnforces)
	}
	if _, ok := EdgeTypeForSemanticAnchorRelation("BEACON_FOR"); ok {
		t.Fatal("reserved BEACON_FOR relation mapped as active semantic anchor relation")
	}

	node := Node{ID: NamespacedID("repo", "anchor:foxctl:invariant:no-send-without-read"), Kind: NodeConcept}
	if got := RawNodeID(node.ID); got != "anchor:foxctl:invariant:no-send-without-read" {
		t.Fatalf("RawNodeID=%q", got)
	}
	if !IsAnchorConceptNode(node) {
		t.Fatal("anchor concept node was not identified")
	}
	if IsAnchorConceptNode(Node{ID: NamespacedID("repo", ConceptKeyword+"read"), Kind: NodeConcept}) {
		t.Fatal("keyword concept identified as anchor concept")
	}
}

func TestDefaultExpandEdgeTypesAreStructuralOnly(t *testing.T) {
	got := DefaultExpandEdgeTypes()
	if !reflect.DeepEqual(got, EdgeSetStructural) {
		t.Fatalf("DefaultExpandEdgeTypes()=%v want %v", got, EdgeSetStructural)
	}
	assertNoEdgeType(t, got, EdgeEnforces)
	assertNoEdgeType(t, got, EdgeBeaconFor)
}

func TestEdgeSetsKeepSemanticAndEmpiricalOutOfStructuralAndDoc(t *testing.T) {
	for _, set := range [][]EdgeType{EdgeSetStructural, EdgeSetDoc} {
		assertNoEdgeType(t, set, EdgeEnforces)
		assertNoEdgeType(t, set, EdgeProtectsAgainst)
		assertNoEdgeType(t, set, EdgeBeaconFor)
		assertNoEdgeType(t, set, EdgeCoChangesWith)
	}
}

func TestAllEdgeTypesIncludesSemanticAndEmpiricalEdges(t *testing.T) {
	got := AllEdgeTypes()
	for _, edgeType := range []EdgeType{
		EdgeContains,
		EdgeDocRelated,
		EdgeEnforces,
		EdgeProtectsAgainst,
		EdgeVerifiedBy,
		EdgeDescribedBy,
		EdgeDecidedBy,
		EdgeImplementsProtocol,
		EdgeParticipatesIn,
		EdgeDeclaresAnchorTarget,
		EdgeCoChangesWith,
	} {
		assertHasEdgeType(t, got, edgeType)
	}
}

func TestNormalizeExpandOptionsDefaultsToStructuralOnly(t *testing.T) {
	got := NormalizeExpandOptions(ExpandOptions{})

	if got.Direction != DirOut {
		t.Fatalf("Direction=%q want %q", got.Direction, DirOut)
	}
	if got.Depth != 1 || got.Budget != 50 || got.PerNodeCap != 50 {
		t.Fatalf("defaults depth/budget/perNodeCap=%d/%d/%d", got.Depth, got.Budget, got.PerNodeCap)
	}
	if !reflect.DeepEqual(got.EdgeTypes, EdgeSetStructural) {
		t.Fatalf("EdgeTypes=%v want structural %v", got.EdgeTypes, EdgeSetStructural)
	}
}

func TestNormalizeExpandOptionsAppendsAndDedupesSemanticAnchors(t *testing.T) {
	got := NormalizeExpandOptions(ExpandOptions{
		EdgeTypes:              []EdgeType{EdgeCalls, EdgeEnforces},
		IncludeSemanticAnchors: true,
	})

	want := []EdgeType{
		EdgeCalls,
		EdgeEnforces,
		EdgeProtectsAgainst,
		EdgeVerifiedBy,
		EdgeDescribedBy,
		EdgeDecidedBy,
		EdgeImplementsProtocol,
		EdgeParticipatesIn,
		EdgeDeclaresAnchorTarget,
	}
	if !reflect.DeepEqual(got.EdgeTypes, want) {
		t.Fatalf("EdgeTypes=%v want %v", got.EdgeTypes, want)
	}
}

func TestNormalizeDAGGrepRequestLegacyIncludeAnchorsOnlyMapsOwnerContainers(t *testing.T) {
	got := NormalizeDAGGrepRequest(DAGGrepRequest{IncludeAnchors: true})

	if !got.IncludeOwnerContainers {
		t.Fatal("IncludeOwnerContainers=false want true")
	}
	if got.IncludeSemanticAnchors {
		t.Fatal("IncludeSemanticAnchors=true want false")
	}
	if !reflect.DeepEqual(got.EdgeTypes, EdgeSetStructural) {
		t.Fatalf("EdgeTypes=%v want structural %v", got.EdgeTypes, EdgeSetStructural)
	}
}

func TestExpandEmptyEdgeFiltersUseStructuralDefaults(t *testing.T) {
	_, qe, seedID := setupEdgeNormalizationStore(t)
	ctx := context.Background()

	result, err := qe.Expand(ctx, []string{seedID}, ExpandOptions{})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}

	assertHasExpandedNode(t, result.Nodes, "structural_target")
	assertNoExpandedNode(t, result.Nodes, "semantic_target")
	assertHasExpandedEdge(t, result.Edges, EdgeCalls)
	assertNoExpandedEdge(t, result.Edges, EdgeEnforces)
}

func TestExpandIncludeSemanticAnchorsAddsSemanticEdges(t *testing.T) {
	_, qe, seedID := setupEdgeNormalizationStore(t)
	ctx := context.Background()

	result, err := qe.Expand(ctx, []string{seedID}, ExpandOptions{IncludeSemanticAnchors: true})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}

	assertHasExpandedNode(t, result.Nodes, "structural_target")
	assertHasExpandedNode(t, result.Nodes, "semantic_target")
	assertHasExpandedEdge(t, result.Edges, EdgeCalls)
	assertHasExpandedEdge(t, result.Edges, EdgeEnforces)
}

func TestDAGGrepIncludeSemanticAnchorsSeparateFromOwnerContainers(t *testing.T) {
	_, qe, _ := setupEdgeNormalizationStore(t)
	ctx := context.Background()

	legacy, err := qe.DAGGrep(ctx, DAGGrepRequest{Query: "seed", IncludeAnchors: true})
	if err != nil {
		t.Fatalf("DAGGrep legacy include anchors: %v", err)
	}
	assertHasExpandedEdge(t, legacy.Graph.Edges, EdgeCalls)
	assertNoExpandedEdge(t, legacy.Graph.Edges, EdgeEnforces)

	withSemantic, err := qe.DAGGrep(ctx, DAGGrepRequest{Query: "seed", IncludeSemanticAnchors: true})
	if err != nil {
		t.Fatalf("DAGGrep include semantic anchors: %v", err)
	}
	assertHasExpandedEdge(t, withSemantic.Graph.Edges, EdgeCalls)
	assertHasExpandedEdge(t, withSemantic.Graph.Edges, EdgeEnforces)
}

func TestFetchEdgesEmptyFiltersUseStructuralDefaults(t *testing.T) {
	_, qe, seedID := setupEdgeNormalizationStore(t)
	ctx := context.Background()

	edges, err := qe.fetchEdges(ctx, seedID, ExpandOptions{})
	if err != nil {
		t.Fatalf("fetchEdges: %v", err)
	}

	assertHasExpandedEdge(t, edges, EdgeCalls)
	assertNoExpandedEdge(t, edges, EdgeEnforces)
}

func setupEdgeNormalizationStore(t *testing.T) (*Store, *QueryEngine, string) {
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
	t.Cleanup(func() { store.Close() })

	key := repoKey(repoRoot)
	pkg := "go:test/pkg"
	seedID := SymbolID(key, pkg, "seed")
	structuralID := SymbolID(key, pkg, "structural_target")
	semanticID := SymbolID(key, pkg, "semantic_target")
	now := time.Now().UTC()

	nodes := []Node{
		{ID: seedID, Kind: NodeSymbol, Pkg: pkg, File: "seed.go", Name: "seed", UpdatedAt: now},
		{ID: structuralID, Kind: NodeSymbol, Pkg: pkg, File: "structural.go", Name: "structural_target", UpdatedAt: now},
		{ID: semanticID, Kind: NodeSymbol, Pkg: pkg, File: "semantic.go", Name: "semantic_target", UpdatedAt: now},
	}
	edges := []Edge{
		{Src: seedID, Dst: structuralID, Type: EdgeCalls, Weight: 1.0},
		{Src: seedID, Dst: semanticID, Type: EdgeEnforces, Weight: 1.0},
	}
	if err := store.ReplaceAll(ctx, nodes, edges); err != nil {
		t.Fatalf("replace all: %v", err)
	}

	return store, NewQueryEngine(store), seedID
}

func assertHasEdgeType(t *testing.T, values []EdgeType, want EdgeType) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("missing edge type %s in %v", want, values)
}

func assertNoEdgeType(t *testing.T, values []EdgeType, unwanted EdgeType) {
	t.Helper()
	for _, value := range values {
		if value == unwanted {
			t.Fatalf("unexpected edge type %s in %v", unwanted, values)
		}
	}
}

func assertHasExpandedNode(t *testing.T, nodes []Node, wantName string) {
	t.Helper()
	for _, node := range nodes {
		if node.Name == wantName {
			return
		}
	}
	t.Fatalf("missing expanded node %q in %v", wantName, nodeNames(nodes))
}

func assertNoExpandedNode(t *testing.T, nodes []Node, unwantedName string) {
	t.Helper()
	for _, node := range nodes {
		if node.Name == unwantedName {
			t.Fatalf("unexpected expanded node %q in %v", unwantedName, nodeNames(nodes))
		}
	}
}

func assertHasExpandedEdge(t *testing.T, edges []Edge, want EdgeType) {
	t.Helper()
	for _, edge := range edges {
		if edge.Type == want {
			return
		}
	}
	t.Fatalf("missing expanded edge %s in %v", want, edgeTypes(edges))
}

func assertNoExpandedEdge(t *testing.T, edges []Edge, unwanted EdgeType) {
	t.Helper()
	for _, edge := range edges {
		if edge.Type == unwanted {
			t.Fatalf("unexpected expanded edge %s in %v", unwanted, edgeTypes(edges))
		}
	}
}

func nodeNames(nodes []Node) []string {
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node.Name)
	}
	return out
}

func edgeTypes(edges []Edge) []EdgeType {
	out := make([]EdgeType, 0, len(edges))
	for _, edge := range edges {
		out = append(out, edge.Type)
	}
	return out
}
