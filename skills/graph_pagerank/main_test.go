package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skilltest"
	"github.com/jkatigb/agentctl/internal/storage/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// Tests for empty graph

func TestPageRank_EmptyGraph(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	err := run(context.Background(), rc, input{})
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	assert.Equal(t, float64(0), data["nodes_updated"])
	assert.Equal(t, "no nodes in graph", data["message"])
}

// Tests for basic PageRank computation

func TestPageRank_BasicComputation(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()
	ctx := context.Background()

	// Build a simple graph: A -> B -> C
	store, err := graph.Open(ctx, rc.Config.Storage.Root)
	require.NoError(t, err)

	now := time.Now()
	require.NoError(t, store.UpsertNode(ctx, graph.Node{
		Workspace: rc.Workspace,
		NodeID:    "A",
		NodeType:  "test",
		Title:     "Node A",
		LastSeen:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}))
	require.NoError(t, store.UpsertNode(ctx, graph.Node{
		Workspace: rc.Workspace,
		NodeID:    "B",
		NodeType:  "test",
		Title:     "Node B",
		LastSeen:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}))
	require.NoError(t, store.UpsertNode(ctx, graph.Node{
		Workspace: rc.Workspace,
		NodeID:    "C",
		NodeType:  "test",
		Title:     "Node C",
		LastSeen:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}))
	require.NoError(t, store.UpsertEdge(ctx, graph.Edge{
		ID:        "edge-ab",
		Workspace: rc.Workspace,
		FromID:    "A",
		ToID:      "B",
		EdgeType:  "link",
		Weight:    1.0,
		CreatedAt: now,
	}))
	require.NoError(t, store.UpsertEdge(ctx, graph.Edge{
		ID:        "edge-bc",
		Workspace: rc.Workspace,
		FromID:    "B",
		ToID:      "C",
		EdgeType:  "link",
		Weight:    1.0,
		CreatedAt: now,
	}))
	store.Close()

	err = run(ctx, rc, input{})
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	assert.Equal(t, float64(3), data["nodes_updated"])
	assert.Equal(t, float64(2), data["edges_processed"])
	assert.Equal(t, 0.85, data["damping_factor"])
	assert.Equal(t, 1e-6, data["tolerance"])

	// Verify top_nodes is present
	topNodes, ok := data["top_nodes"].([]any)
	require.True(t, ok, "top_nodes should be array")
	assert.LessOrEqual(t, len(topNodes), 10)
}

// Test custom parameters

func TestPageRank_CustomParameters(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()
	ctx := context.Background()

	now := time.Now()
	store, err := graph.Open(ctx, rc.Config.Storage.Root)
	require.NoError(t, err)
	require.NoError(t, store.UpsertNode(ctx, graph.Node{
		Workspace: rc.Workspace,
		NodeID:    "X",
		NodeType:  "test",
		Title:     "Node X",
		LastSeen:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}))
	require.NoError(t, store.UpsertNode(ctx, graph.Node{
		Workspace: rc.Workspace,
		NodeID:    "Y",
		NodeType:  "test",
		Title:     "Node Y",
		LastSeen:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}))
	require.NoError(t, store.UpsertEdge(ctx, graph.Edge{
		ID:        "edge-xy",
		Workspace: rc.Workspace,
		FromID:    "X",
		ToID:      "Y",
		EdgeType:  "link",
		Weight:    1.0,
		CreatedAt: now,
	}))
	store.Close()

	err = run(ctx, rc, input{
		DampingFactor: 0.9,
		Tolerance:     1e-4,
	})
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	assert.Equal(t, 0.9, data["damping_factor"])
	assert.Equal(t, 1e-4, data["tolerance"])
}

// Test workspace scoping

func TestPageRank_WorkspaceScoping(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()
	ctx := context.Background()

	// Add nodes to a different workspace
	now := time.Now()
	store, err := graph.Open(ctx, rc.Config.Storage.Root)
	require.NoError(t, err)

	otherWorkspace := "/other/workspace"
	require.NoError(t, store.UpsertNode(ctx, graph.Node{
		Workspace: otherWorkspace,
		NodeID:    "OtherNode",
		NodeType:  "test",
		Title:     "Other Node",
		LastSeen:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}))
	store.Close()

	// Run pagerank on test workspace (should be empty)
	err = run(ctx, rc, input{})
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	assert.Equal(t, float64(0), data["nodes_updated"])
	assert.Equal(t, "no nodes in graph", data["message"])
}

// Test explicit workspace parameter

func TestPageRank_ExplicitWorkspace(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()
	ctx := context.Background()

	// Add nodes to explicit workspace
	now := time.Now()
	store, err := graph.Open(ctx, rc.Config.Storage.Root)
	require.NoError(t, err)

	explicitWorkspace := "/explicit/ws"
	require.NoError(t, store.UpsertNode(ctx, graph.Node{
		Workspace: explicitWorkspace,
		NodeID:    "Node1",
		NodeType:  "test",
		Title:     "Node 1",
		LastSeen:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}))
	store.Close()

	err = run(ctx, rc, input{Workspace: explicitWorkspace})
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	assert.Equal(t, float64(1), data["nodes_updated"])
	assert.Equal(t, explicitWorkspace, data["workspace"])
}

// Test self-loops are skipped

func TestPageRank_SelfLoopsSkipped(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()
	ctx := context.Background()

	now := time.Now()
	store, err := graph.Open(ctx, rc.Config.Storage.Root)
	require.NoError(t, err)

	require.NoError(t, store.UpsertNode(ctx, graph.Node{
		Workspace: rc.Workspace,
		NodeID:    "SelfRef",
		NodeType:  "test",
		Title:     "Self Reference",
		LastSeen:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}))
	// Add self-loop edge
	require.NoError(t, store.UpsertEdge(ctx, graph.Edge{
		ID:        "self-edge",
		Workspace: rc.Workspace,
		FromID:    "SelfRef",
		ToID:      "SelfRef",
		EdgeType:  "self",
		Weight:    1.0,
		CreatedAt: now,
	}))
	store.Close()

	err = run(ctx, rc, input{})
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	assert.Equal(t, float64(1), data["nodes_updated"])
	// Self-loop is still counted as processed edge
	assert.Equal(t, float64(1), data["edges_processed"])
}

// Test more complex graph with multiple connections

func TestPageRank_ComplexGraph(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()
	ctx := context.Background()

	now := time.Now()
	store, err := graph.Open(ctx, rc.Config.Storage.Root)
	require.NoError(t, err)

	// Create a hub-and-spoke structure
	// Hub connects to A, B, C
	// A, B, C all link back to Hub
	nodes := []string{"Hub", "A", "B", "C"}
	for _, n := range nodes {
		require.NoError(t, store.UpsertNode(ctx, graph.Node{
			Workspace: rc.Workspace,
			NodeID:    n,
			NodeType:  "test",
			Title:     "Node " + n,
			LastSeen:  now,
			CreatedAt: now,
			UpdatedAt: now,
		}))
	}

	// Hub -> A, B, C
	for i, target := range []string{"A", "B", "C"} {
		require.NoError(t, store.UpsertEdge(ctx, graph.Edge{
			ID:        "edge-hub-" + string(rune('a'+i)),
			Workspace: rc.Workspace,
			FromID:    "Hub",
			ToID:      target,
			EdgeType:  "link",
			Weight:    1.0,
			CreatedAt: now,
		}))
	}

	// A, B, C -> Hub
	for _, source := range []string{"A", "B", "C"} {
		require.NoError(t, store.UpsertEdge(ctx, graph.Edge{
			ID:        "edge-" + source + "-hub",
			Workspace: rc.Workspace,
			FromID:    source,
			ToID:      "Hub",
			EdgeType:  "backlink",
			Weight:    1.0,
			CreatedAt: now,
		}))
	}
	store.Close()

	err = run(ctx, rc, input{})
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	assert.Equal(t, float64(4), data["nodes_updated"])
	assert.Equal(t, float64(6), data["edges_processed"])

	// Top nodes should include Hub at or near top (highest in-degree)
	topNodes, ok := data["top_nodes"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, topNodes)
}

// Test PageRank values are persisted

func TestPageRank_ValuesPersisted(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()
	ctx := context.Background()

	now := time.Now()
	store, err := graph.Open(ctx, rc.Config.Storage.Root)
	require.NoError(t, err)

	require.NoError(t, store.UpsertNode(ctx, graph.Node{
		Workspace: rc.Workspace,
		NodeID:    "Node1",
		NodeType:  "test",
		Title:     "Node 1",
		LastSeen:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}))
	require.NoError(t, store.UpsertNode(ctx, graph.Node{
		Workspace: rc.Workspace,
		NodeID:    "Node2",
		NodeType:  "test",
		Title:     "Node 2",
		LastSeen:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}))
	require.NoError(t, store.UpsertEdge(ctx, graph.Edge{
		ID:        "edge-12",
		Workspace: rc.Workspace,
		FromID:    "Node1",
		ToID:      "Node2",
		EdgeType:  "link",
		Weight:    1.0,
		CreatedAt: now,
	}))
	store.Close()

	err = run(ctx, rc, input{})
	require.NoError(t, err)

	// Reopen store and verify PageRank values were saved
	store2, err := graph.Open(ctx, rc.Config.Storage.Root)
	require.NoError(t, err)
	defer store2.Close()

	topNodes, err := store2.TopNodes(ctx, graph.TopNodesOptions{
		Workspace: rc.Workspace,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, topNodes, 2)

	// Verify PageRank values are non-zero
	for _, n := range topNodes {
		assert.Greater(t, n.PageRank, 0.0, "PageRank should be > 0 for node %s", n.NodeID)
	}
}

// Test compute_time_ms is returned

func TestPageRank_ComputeTimeReturned(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()
	ctx := context.Background()

	now := time.Now()
	store, err := graph.Open(ctx, rc.Config.Storage.Root)
	require.NoError(t, err)
	require.NoError(t, store.UpsertNode(ctx, graph.Node{
		Workspace: rc.Workspace,
		NodeID:    "Node1",
		NodeType:  "test",
		Title:     "Node 1",
		LastSeen:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}))
	store.Close()

	err = run(ctx, rc, input{})
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	computeTime, ok := data["compute_time_ms"].(float64)
	assert.True(t, ok, "compute_time_ms should be a number")
	assert.GreaterOrEqual(t, computeTime, 0.0, "compute_time_ms should be >= 0")
}
