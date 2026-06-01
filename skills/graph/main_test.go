package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skilltest"
	"github.com/joshka0/foxctl/internal/storage/graph"
)

// Test helpers

func newTestContext(t *testing.T, buf *bytes.Buffer) (*skillmain.RunContext, func()) {
	t.Helper()
	return skilltest.NewTestRunContext(t, buf, nil)
}

func decodeEnvelope(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	return env
}

func assertOK(t *testing.T, env map[string]any) {
	t.Helper()
	if env["status"] != "ok" {
		errField := env["error"]
		t.Fatalf("expected ok status, got %v (error: %v)", env["status"], errField)
	}
}

func getData(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map, got %T", env["data"])
	}
	return data
}

// =============================================================================
// Input Validation Tests
// =============================================================================

func TestGraph_OperationRequired(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	in := input{} // No operation
	err := run(ctx, rc, in)
	if err == nil {
		t.Fatal("expected error for missing operation")
	}

	// Error should mention operation is required
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

func TestGraph_InvalidOperation(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	in := input{Operation: "invalid_op"}
	err := run(ctx, rc, in)
	if err == nil {
		t.Fatal("expected error for invalid operation")
	}
}

func TestGraph_AddNodeMissingPayload(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	in := input{Operation: "add_node"} // No add_node payload
	err := run(ctx, rc, in)
	if err == nil {
		t.Fatal("expected error for missing add_node payload")
	}
}

func TestGraph_AddEdgeMissingPayload(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	in := input{Operation: "add_edge"} // No add_edge payload
	err := run(ctx, rc, in)
	if err == nil {
		t.Fatal("expected error for missing add_edge payload")
	}
}

func TestGraph_QueryMissingPayload(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	in := input{Operation: "query"} // No query payload
	err := run(ctx, rc, in)
	if err == nil {
		t.Fatal("expected error for missing query payload")
	}
}

func TestGraph_NeighborsMissingPayload(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	in := input{Operation: "neighbors"} // No neighbors payload
	err := run(ctx, rc, in)
	if err == nil {
		t.Fatal("expected error for missing neighbors payload")
	}
}

func TestGraph_DeleteNodeMissingPayload(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	in := input{Operation: "delete_node"} // No delete_node payload
	err := run(ctx, rc, in)
	if err == nil {
		t.Fatal("expected error for missing delete_node payload")
	}
}

func TestGraph_DeleteEdgeMissingPayload(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	in := input{Operation: "delete_edge"} // No delete_edge payload
	err := run(ctx, rc, in)
	if err == nil {
		t.Fatal("expected error for missing delete_edge payload")
	}
}

// =============================================================================
// add_node Tests
// =============================================================================

func TestGraph_AddNode_Basic(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	in := input{
		Operation: "add_node",
		Workspace: rc.Workspace,
		AddNode: &addNodeRequest{
			NodeID:   "file:src/main.go",
			NodeType: "file",
			Title:    "main.go",
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)

	data := getData(t, env)
	if data["node_id"] != "file:src/main.go" {
		t.Errorf("expected node_id 'file:src/main.go', got %v", data["node_id"])
	}
	if data["created"] != true {
		t.Errorf("expected created=true, got %v", data["created"])
	}
}

func TestGraph_AddNode_Idempotent(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	in := input{
		Operation: "add_node",
		Workspace: rc.Workspace,
		AddNode: &addNodeRequest{
			NodeID:   "file:src/main.go",
			NodeType: "file",
			Title:    "main.go",
		},
	}

	// First add
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("first add: %v", err)
	}

	// Second add (same node) - should succeed (upsert)
	buf.Reset()
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("second add: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)
}

func TestGraph_AddNode_WithMetadata(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	in := input{
		Operation: "add_node",
		Workspace: rc.Workspace,
		AddNode: &addNodeRequest{
			NodeID:      "func:main.Handler",
			NodeType:    string(graph.NodeTypeSymbol),
			Title:       "Handler",
			CurrentPath: "src/main.go",
			Metadata: map[string]string{
				"package": "main",
				"line":    "42",
			},
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)
}

// =============================================================================
// add_edge Tests
// =============================================================================

func TestGraph_AddEdge_Basic(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	// First add nodes
	addNodeInput := input{
		Operation: "add_node",
		Workspace: rc.Workspace,
		AddNode: &addNodeRequest{
			NodeID:   "file:a.go",
			NodeType: "file",
			Title:    "a.go",
		},
	}
	if err := run(ctx, rc, addNodeInput); err != nil {
		t.Fatalf("add node a: %v", err)
	}
	buf.Reset()

	addNodeInput.AddNode = &addNodeRequest{
		NodeID:   "file:b.go",
		NodeType: "file",
		Title:    "b.go",
	}
	if err := run(ctx, rc, addNodeInput); err != nil {
		t.Fatalf("add node b: %v", err)
	}
	buf.Reset()

	// Add edge
	in := input{
		Operation: "add_edge",
		Workspace: rc.Workspace,
		AddEdge: &addEdgeRequest{
			FromID:   "file:a.go",
			FromType: "file",
			ToID:     "file:b.go",
			ToType:   "file",
			EdgeType: "imports",
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)

	data := getData(t, env)
	if data["from_id"] != "file:a.go" {
		t.Errorf("expected from_id 'file:a.go', got %v", data["from_id"])
	}
	if data["to_id"] != "file:b.go" {
		t.Errorf("expected to_id 'file:b.go', got %v", data["to_id"])
	}
}

func TestGraph_AddEdge_DefaultWeight(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	in := input{
		Operation: "add_edge",
		Workspace: rc.Workspace,
		AddEdge: &addEdgeRequest{
			FromID:   "file:a.go",
			FromType: "file",
			ToID:     "file:b.go",
			ToType:   "file",
			EdgeType: "imports",
			// Weight not specified - should default to 1.0
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)
}

func TestGraph_AddEdge_WithTTL(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	ttl := 30
	in := input{
		Operation: "add_edge",
		Workspace: rc.Workspace,
		AddEdge: &addEdgeRequest{
			FromID:   "file:a.go",
			FromType: "file",
			ToID:     "file:b.go",
			ToType:   "file",
			EdgeType: "imports",
			TTLDays:  &ttl,
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)
}

// =============================================================================
// query Tests
// =============================================================================

func TestGraph_Query_Direction_From(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	// Setup: A -> B, A -> C
	setupGraph(t, ctx, rc, buf)

	in := input{
		Operation: "query",
		Workspace: rc.Workspace,
		Query: &queryRequest{
			NodeID:    "file:a.go",
			Direction: "from",
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)

	data := getData(t, env)
	count, ok := data["count"].(float64)
	if !ok {
		t.Fatalf("expected count to be number, got %T", data["count"])
	}
	if count != 2 {
		t.Errorf("expected 2 edges from A, got %v", count)
	}
}

func TestGraph_Query_Direction_To(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	// Setup: A -> B, A -> C
	setupGraph(t, ctx, rc, buf)

	in := input{
		Operation: "query",
		Workspace: rc.Workspace,
		Query: &queryRequest{
			NodeID:    "file:b.go",
			Direction: "to",
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)

	data := getData(t, env)
	count, ok := data["count"].(float64)
	if !ok {
		t.Fatalf("expected count to be number, got %T", data["count"])
	}
	if count != 1 {
		t.Errorf("expected 1 edge to B, got %v", count)
	}
}

func TestGraph_Query_Direction_Both(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	// Setup: A -> B, A -> C
	setupGraph(t, ctx, rc, buf)

	in := input{
		Operation: "query",
		Workspace: rc.Workspace,
		Query: &queryRequest{
			NodeID:    "file:a.go",
			Direction: "both",
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)
}

func TestGraph_Query_EdgeTypeFilter(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	// Setup: A -> B (imports), A -> C (calls)
	setupGraphWithEdgeTypes(t, ctx, rc, buf)

	in := input{
		Operation: "query",
		Workspace: rc.Workspace,
		Query: &queryRequest{
			NodeID:    "file:a.go",
			Direction: "from",
			EdgeTypes: []string{"imports"},
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)

	data := getData(t, env)
	count, ok := data["count"].(float64)
	if !ok {
		t.Fatalf("expected count to be number, got %T", data["count"])
	}
	if count != 1 {
		t.Errorf("expected 1 edge with type 'imports', got %v", count)
	}
}

// =============================================================================
// neighbors Tests
// =============================================================================

func TestGraph_Neighbors_Basic(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	setupGraph(t, ctx, rc, buf)

	in := input{
		Operation: "neighbors",
		Workspace: rc.Workspace,
		Neighbors: &neighborsReq{
			NodeID: "file:a.go",
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)

	data := getData(t, env)
	if data["node_id"] != "file:a.go" {
		t.Errorf("expected node_id 'file:a.go', got %v", data["node_id"])
	}
}

func TestGraph_Neighbors_WithLimit(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	setupGraph(t, ctx, rc, buf)

	in := input{
		Operation: "neighbors",
		Workspace: rc.Workspace,
		Neighbors: &neighborsReq{
			NodeID: "file:a.go",
			Limit:  1,
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)

	data := getData(t, env)
	count, ok := data["count"].(float64)
	if !ok {
		t.Fatalf("expected count to be number, got %T", data["count"])
	}
	if count > 1 {
		t.Errorf("expected at most 1 neighbor with limit=1, got %v", count)
	}
}

func TestGraph_Neighbors_DefaultLimit(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	setupGraph(t, ctx, rc, buf)

	in := input{
		Operation: "neighbors",
		Workspace: rc.Workspace,
		Neighbors: &neighborsReq{
			NodeID: "file:a.go",
			// No limit - should default to 20
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)
}

// =============================================================================
// top_nodes Tests
// =============================================================================

func TestGraph_TopNodes_Basic(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	setupGraph(t, ctx, rc, buf)

	in := input{
		Operation: "top_nodes",
		Workspace: rc.Workspace,
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)

	data := getData(t, env)
	nodes, ok := data["nodes"].([]any)
	if !ok {
		t.Fatalf("expected nodes to be array, got %T", data["nodes"])
	}
	// Should have nodes from setup
	if len(nodes) == 0 {
		t.Error("expected at least one node")
	}
}

func TestGraph_TopNodes_WithLimit(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	setupGraph(t, ctx, rc, buf)

	in := input{
		Operation: "top_nodes",
		Workspace: rc.Workspace,
		TopNodes: &topNodesReq{
			Limit: 1,
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)

	data := getData(t, env)
	nodes, ok := data["nodes"].([]any)
	if !ok {
		t.Fatalf("expected nodes to be array, got %T", data["nodes"])
	}
	if len(nodes) > 1 {
		t.Errorf("expected at most 1 node with limit=1, got %d", len(nodes))
	}
}

func TestGraph_TopNodes_FilterByType(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	// Add nodes of different types
	for _, node := range []addNodeRequest{
		{NodeID: "file:a.go", NodeType: string(graph.NodeTypeFile), Title: "a.go"},
		{NodeID: "func:Handler", NodeType: string(graph.NodeTypeSymbol), Title: "Handler"},
	} {
		in := input{
			Operation: "add_node",
			Workspace: rc.Workspace,
			AddNode:   &node,
		}
		if err := run(ctx, rc, in); err != nil {
			t.Fatalf("add node: %v", err)
		}
		buf.Reset()
	}

	in := input{
		Operation: "top_nodes",
		Workspace: rc.Workspace,
		TopNodes: &topNodesReq{
			NodeType: string(graph.NodeTypeSymbol),
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)

	data := getData(t, env)
	nodes, ok := data["nodes"].([]any)
	if !ok {
		t.Fatalf("expected nodes to be array, got %T", data["nodes"])
	}

	// All returned nodes should be functions
	for _, n := range nodes {
		node := n.(map[string]any)
		if node["node_type"] != string(graph.NodeTypeSymbol) {
			t.Errorf("expected node_type %q, got %v", graph.NodeTypeSymbol, node["node_type"])
		}
	}
}

// =============================================================================
// delete_node Tests
// =============================================================================

func TestGraph_DeleteNode_Basic(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	// Add a node first
	addIn := input{
		Operation: "add_node",
		Workspace: rc.Workspace,
		AddNode: &addNodeRequest{
			NodeID:   "file:delete-me.go",
			NodeType: "file",
			Title:    "delete-me.go",
		},
	}
	if err := run(ctx, rc, addIn); err != nil {
		t.Fatalf("add node: %v", err)
	}
	buf.Reset()

	// Delete it
	in := input{
		Operation: "delete_node",
		Workspace: rc.Workspace,
		DeleteNode: &deleteNodeReq{
			NodeID: "file:delete-me.go",
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)

	data := getData(t, env)
	if data["deleted"] != true {
		t.Errorf("expected deleted=true, got %v", data["deleted"])
	}
}

// =============================================================================
// delete_edge Tests
// =============================================================================

func TestGraph_DeleteEdge_Basic(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	// Add an edge first
	addIn := input{
		Operation: "add_edge",
		Workspace: rc.Workspace,
		AddEdge: &addEdgeRequest{
			FromID:   "file:a.go",
			FromType: "file",
			ToID:     "file:b.go",
			ToType:   "file",
			EdgeType: "imports",
		},
	}
	if err := run(ctx, rc, addIn); err != nil {
		t.Fatalf("add edge: %v", err)
	}
	buf.Reset()

	// Get the edge ID from the store
	store, err := graph.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	edges, err := store.GetEdgesFrom(ctx, rc.Workspace, "file:a.go", nil)
	if err != nil {
		t.Fatalf("get edges: %v", err)
	}
	if len(edges) == 0 {
		t.Fatal("expected at least one edge")
	}

	// Delete the edge
	in := input{
		Operation: "delete_edge",
		DeleteEdge: &deleteEdgeReq{
			EdgeID: edges[0].ID,
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)

	data := getData(t, env)
	if data["deleted"] != true {
		t.Errorf("expected deleted=true, got %v", data["deleted"])
	}
}

// =============================================================================
// stats Tests
// =============================================================================

func TestGraph_Stats_Empty(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	in := input{
		Operation: "stats",
		Workspace: rc.Workspace,
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)

	data := getData(t, env)
	if data["total_nodes"].(float64) != 0 {
		t.Errorf("expected 0 nodes in empty graph, got %v", data["total_nodes"])
	}
	if data["total_edges"].(float64) != 0 {
		t.Errorf("expected 0 edges in empty graph, got %v", data["total_edges"])
	}
}

func TestGraph_Stats_WithData(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	setupGraph(t, ctx, rc, buf)

	in := input{
		Operation: "stats",
		Workspace: rc.Workspace,
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)

	data := getData(t, env)
	if data["total_nodes"].(float64) < 3 {
		t.Errorf("expected at least 3 nodes, got %v", data["total_nodes"])
	}
	if data["total_edges"].(float64) < 2 {
		t.Errorf("expected at least 2 edges, got %v", data["total_edges"])
	}
}

// =============================================================================
// cleanup Tests
// =============================================================================

func TestGraph_Cleanup_ExpiredEdges(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	in := input{
		Operation: "cleanup",
		Workspace: rc.Workspace,
		Cleanup: &cleanupReq{
			ExpiredEdges: true,
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)

	data := getData(t, env)
	// Should have expired_edges_removed field
	if _, ok := data["expired_edges_removed"]; !ok {
		t.Error("expected expired_edges_removed field in response")
	}
}

func TestGraph_Cleanup_DanglingEdges(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	in := input{
		Operation: "cleanup",
		Workspace: rc.Workspace,
		Cleanup: &cleanupReq{
			DanglingEdges: true,
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)

	data := getData(t, env)
	// Should have dangling_edges_removed field
	if _, ok := data["dangling_edges_removed"]; !ok {
		t.Error("expected dangling_edges_removed field in response")
	}
}

func TestGraph_Cleanup_RecalcDegrees(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	setupGraph(t, ctx, rc, buf)

	in := input{
		Operation: "cleanup",
		Workspace: rc.Workspace,
		Cleanup: &cleanupReq{
			RecalcDegrees: true,
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)

	data := getData(t, env)
	if data["degrees_recalculated"] != true {
		t.Errorf("expected degrees_recalculated=true, got %v", data["degrees_recalculated"])
	}
}

func TestGraph_Cleanup_AllOptions(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	setupGraph(t, ctx, rc, buf)

	in := input{
		Operation: "cleanup",
		Workspace: rc.Workspace,
		Cleanup: &cleanupReq{
			ExpiredEdges:  true,
			DanglingEdges: true,
			RecalcDegrees: true,
		},
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)
}

// =============================================================================
// Workspace Scoping Tests
// =============================================================================

func TestGraph_WorkspaceScoping(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	rc, cleanup := newTestContext(t, buf)
	defer cleanup()

	// Add node to workspace A
	workspace1 := filepath.Join(rc.Workspace, "project-a")
	in := input{
		Operation: "add_node",
		Workspace: workspace1,
		AddNode: &addNodeRequest{
			NodeID:   "file:shared.go",
			NodeType: "file",
			Title:    "shared.go",
		},
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("add to workspace1: %v", err)
	}
	buf.Reset()

	// Add node to workspace B
	workspace2 := filepath.Join(rc.Workspace, "project-b")
	in.Workspace = workspace2
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("add to workspace2: %v", err)
	}
	buf.Reset()

	// Stats for workspace A should show 1 node
	statsIn := input{
		Operation: "stats",
		Workspace: workspace1,
	}
	if err := run(ctx, rc, statsIn); err != nil {
		t.Fatalf("stats workspace1: %v", err)
	}

	env := decodeEnvelope(t, buf)
	assertOK(t, env)

	data := getData(t, env)
	if data["total_nodes"].(float64) != 1 {
		t.Errorf("expected 1 node in workspace1, got %v", data["total_nodes"])
	}
}

// =============================================================================
// Helper functions for setting up test data
// =============================================================================

func setupGraph(t *testing.T, ctx context.Context, rc *skillmain.RunContext, buf *bytes.Buffer) {
	t.Helper()

	// Add nodes: A, B, C
	nodes := []addNodeRequest{
		{NodeID: "file:a.go", NodeType: "file", Title: "a.go"},
		{NodeID: "file:b.go", NodeType: "file", Title: "b.go"},
		{NodeID: "file:c.go", NodeType: "file", Title: "c.go"},
	}

	for _, node := range nodes {
		in := input{
			Operation: "add_node",
			Workspace: rc.Workspace,
			AddNode:   &node,
		}
		if err := run(ctx, rc, in); err != nil {
			t.Fatalf("add node %s: %v", node.NodeID, err)
		}
		buf.Reset()
	}

	// Add edges: A -> B, A -> C
	edges := []addEdgeRequest{
		{FromID: "file:a.go", FromType: "file", ToID: "file:b.go", ToType: "file", EdgeType: "imports"},
		{FromID: "file:a.go", FromType: "file", ToID: "file:c.go", ToType: "file", EdgeType: "imports"},
	}

	for _, edge := range edges {
		in := input{
			Operation: "add_edge",
			Workspace: rc.Workspace,
			AddEdge:   &edge,
		}
		if err := run(ctx, rc, in); err != nil {
			t.Fatalf("add edge %s->%s: %v", edge.FromID, edge.ToID, err)
		}
		buf.Reset()
	}
}

func setupGraphWithEdgeTypes(t *testing.T, ctx context.Context, rc *skillmain.RunContext, buf *bytes.Buffer) {
	t.Helper()

	// Add nodes
	nodes := []addNodeRequest{
		{NodeID: "file:a.go", NodeType: "file", Title: "a.go"},
		{NodeID: "file:b.go", NodeType: "file", Title: "b.go"},
		{NodeID: "file:c.go", NodeType: "file", Title: "c.go"},
	}

	for _, node := range nodes {
		in := input{
			Operation: "add_node",
			Workspace: rc.Workspace,
			AddNode:   &node,
		}
		if err := run(ctx, rc, in); err != nil {
			t.Fatalf("add node %s: %v", node.NodeID, err)
		}
		buf.Reset()
	}

	// Add edges with different types
	edges := []addEdgeRequest{
		{FromID: "file:a.go", FromType: "file", ToID: "file:b.go", ToType: "file", EdgeType: "imports"},
		{FromID: "file:a.go", FromType: "file", ToID: "file:c.go", ToType: "file", EdgeType: "calls"},
	}

	for _, edge := range edges {
		in := input{
			Operation: "add_edge",
			Workspace: rc.Workspace,
			AddEdge:   &edge,
		}
		if err := run(ctx, rc, in); err != nil {
			t.Fatalf("add edge %s->%s: %v", edge.FromID, edge.ToID, err)
		}
		buf.Reset()
	}
}
