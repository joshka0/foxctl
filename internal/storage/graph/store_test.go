package graph

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpen(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	store, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	// Verify database file was created
	dbPath := filepath.Join(dir, "graph.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("expected database file to exist at %s", dbPath)
	}
}

func TestUpsertNode(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	node := Node{
		Workspace:   "/test/workspace",
		NodeID:      "task:123",
		NodeType:    NodeTypeTask,
		Title:       "Test Task",
		CurrentPath: "",
		LastSeen:    time.Now().UTC(),
	}

	if err := store.UpsertNode(ctx, node); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}

	// Retrieve and verify
	got, err := store.GetNode(ctx, node.Workspace, node.NodeID)
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}

	if got.NodeID != node.NodeID {
		t.Errorf("NodeID = %s, want %s", got.NodeID, node.NodeID)
	}
	if got.Title != node.Title {
		t.Errorf("Title = %s, want %s", got.Title, node.Title)
	}
	if got.NodeType != node.NodeType {
		t.Errorf("NodeType = %s, want %s", got.NodeType, node.NodeType)
	}
}

func TestUpsertNodeAcceptsKnownTypes(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	types := []NodeType{
		NodeTypeSession,
		NodeTypeTask,
		NodeTypeSymbol,
		NodeTypeMemory,
		NodeTypeFile,
	}

	for _, nodeType := range types {
		t.Run(string(nodeType), func(t *testing.T) {
			node := Node{
				Workspace: "/test/workspace",
				NodeID:    "node:" + string(nodeType),
				NodeType:  nodeType,
				Title:     string(nodeType),
				LastSeen:  time.Now().UTC(),
			}
			if err := store.UpsertNode(ctx, node); err != nil {
				t.Fatalf("UpsertNode(%q) error = %v", nodeType, err)
			}
		})
	}
}

func TestUpsertNodeRejectsInvalidType(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	err := store.UpsertNode(ctx, Node{
		Workspace: "/test/workspace",
		NodeID:    "node:invalid",
		NodeType:  NodeType("unknown-node-type"),
		Title:     "Invalid",
		LastSeen:  time.Now().UTC(),
	})
	if err == nil {
		t.Fatalf("expected invalid node type to be rejected")
	}

	nodes, err := store.GetAllNodes(ctx, "/test/workspace")
	if err != nil {
		t.Fatalf("GetAllNodes() error = %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("invalid node was persisted: %+v", nodes)
	}
}

func TestUpsertNodesRejectsInvalidTypeWithoutPersistingBatch(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	err := store.UpsertNodes(ctx, []Node{
		{
			Workspace: "/test/workspace",
			NodeID:    "node:valid",
			NodeType:  NodeTypeTask,
			Title:     "Valid",
			LastSeen:  time.Now().UTC(),
		},
		{
			Workspace: "/test/workspace",
			NodeID:    "node:invalid",
			NodeType:  NodeType("unknown-node-type"),
			Title:     "Invalid",
			LastSeen:  time.Now().UTC(),
		},
	})
	if err == nil {
		t.Fatalf("expected invalid node type batch to be rejected")
	}

	nodes, err := store.GetAllNodes(ctx, "/test/workspace")
	if err != nil {
		t.Fatalf("GetAllNodes() error = %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("invalid batch persisted nodes: %+v", nodes)
	}
}

func TestUpsertNodeRejectsNegativeMetrics(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	tests := []struct {
		name string
		node Node
	}{
		{
			name: "pagerank",
			node: Node{
				Workspace: "/test/workspace", NodeID: "node:pagerank",
				NodeType: NodeTypeTask, PageRank: -0.1, LastSeen: time.Now().UTC(),
			},
		},
		{
			name: "in degree",
			node: Node{
				Workspace: "/test/workspace", NodeID: "node:in-degree",
				NodeType: NodeTypeTask, InDegree: -1, LastSeen: time.Now().UTC(),
			},
		},
		{
			name: "out degree",
			node: Node{
				Workspace: "/test/workspace", NodeID: "node:out-degree",
				NodeType: NodeTypeTask, OutDegree: -1, LastSeen: time.Now().UTC(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.UpsertNode(ctx, tt.node); err == nil {
				t.Fatalf("expected negative %s to be rejected", tt.name)
			}

			nodes, err := store.GetAllNodes(ctx, "/test/workspace")
			if err != nil {
				t.Fatalf("GetAllNodes() error = %v", err)
			}
			if len(nodes) != 0 {
				t.Fatalf("invalid node was persisted: %+v", nodes)
			}
		})
	}
}

func TestUpsertNodeRejectsNonFinitePageRank(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		pageRank float64
	}{
		{name: "nan", pageRank: math.NaN()},
		{name: "positive infinity", pageRank: math.Inf(1)},
		{name: "negative infinity", pageRank: math.Inf(-1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := setupTestStore(t)
			defer store.Close()

			err := store.UpsertNode(ctx, Node{
				Workspace: "/test/workspace",
				NodeID:    "node:" + tt.name,
				NodeType:  NodeTypeTask,
				PageRank:  tt.pageRank,
				LastSeen:  time.Now().UTC(),
			})
			if err == nil {
				t.Fatalf("expected non-finite pagerank %s to be rejected", tt.name)
			}

			nodes, err := store.GetAllNodes(ctx, "/test/workspace")
			if err != nil {
				t.Fatalf("GetAllNodes() error = %v", err)
			}
			if len(nodes) != 0 {
				t.Fatalf("invalid node was persisted: %+v", nodes)
			}
		})
	}
}

func TestUpsertEdge(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	// Create nodes first
	workspace := "/test/workspace"
	node1 := Node{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()}
	node2 := Node{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, Title: "Task 2", LastSeen: time.Now().UTC()}
	if err := store.UpsertNode(ctx, node1); err != nil {
		t.Fatalf("UpsertNode(node1) error = %v", err)
	}
	if err := store.UpsertNode(ctx, node2); err != nil {
		t.Fatalf("UpsertNode(node2) error = %v", err)
	}

	edge := Edge{
		Workspace: workspace,
		FromID:    "task:1",
		FromType:  NodeTypeTask,
		ToID:      "task:2",
		ToType:    NodeTypeTask,
		EdgeType:  EdgeTypeDependsOn,
		Weight:    1.0,
		CreatedAt: time.Now().UTC(),
	}

	if err := store.UpsertEdge(ctx, edge); err != nil {
		t.Fatalf("UpsertEdge() error = %v", err)
	}

	// Retrieve and verify
	edges, err := store.GetEdgesFrom(ctx, workspace, "task:1", nil)
	if err != nil {
		t.Fatalf("GetEdgesFrom() error = %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	if edges[0].ToID != "task:2" {
		t.Errorf("ToID = %s, want task:2", edges[0].ToID)
	}
}

func TestUpsertEdgeRejectsNegativeMetrics(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"
	node1 := Node{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()}
	node2 := Node{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, Title: "Task 2", LastSeen: time.Now().UTC()}
	if err := store.UpsertNodes(ctx, []Node{node1, node2}); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	negativeTTL := -1
	tests := []struct {
		name string
		edge Edge
	}{
		{
			name: "weight",
			edge: Edge{
				Workspace: workspace, FromID: "task:1", FromType: NodeTypeTask,
				ToID: "task:2", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, Weight: -0.1, CreatedAt: time.Now().UTC(),
			},
		},
		{
			name: "ttl days",
			edge: Edge{
				Workspace: workspace, FromID: "task:1", FromType: NodeTypeTask,
				ToID: "task:2", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, TTLDays: &negativeTTL, CreatedAt: time.Now().UTC(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.UpsertEdge(ctx, tt.edge); err == nil {
				t.Fatalf("expected negative %s to be rejected", tt.name)
			}

			edges, err := store.GetAllEdges(ctx, workspace)
			if err != nil {
				t.Fatalf("GetAllEdges() error = %v", err)
			}
			if len(edges) != 0 {
				t.Fatalf("invalid edge was persisted: %+v", edges)
			}
		})
	}
}

func TestUpsertEdgeRejectsNonFiniteWeight(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		weight float64
	}{
		{name: "nan", weight: math.NaN()},
		{name: "positive infinity", weight: math.Inf(1)},
		{name: "negative infinity", weight: math.Inf(-1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := setupTestStore(t)
			defer store.Close()

			workspace := "/test/workspace"
			node1 := Node{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()}
			node2 := Node{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, Title: "Task 2", LastSeen: time.Now().UTC()}
			if err := store.UpsertNodes(ctx, []Node{node1, node2}); err != nil {
				t.Fatalf("UpsertNodes() error = %v", err)
			}

			err := store.UpsertEdge(ctx, Edge{
				Workspace: workspace,
				FromID:    "task:1",
				FromType:  NodeTypeTask,
				ToID:      "task:2",
				ToType:    NodeTypeTask,
				EdgeType:  EdgeTypeDependsOn,
				Weight:    tt.weight,
				CreatedAt: time.Now().UTC(),
			})
			if err == nil {
				t.Fatalf("expected non-finite weight %s to be rejected", tt.name)
			}

			edges, err := store.GetAllEdges(ctx, workspace)
			if err != nil {
				t.Fatalf("GetAllEdges() error = %v", err)
			}
			if len(edges) != 0 {
				t.Fatalf("invalid edge was persisted: %+v", edges)
			}
		})
	}
}

func TestUpsertEdgeAcceptsKnownTypes(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"
	node1 := Node{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()}
	node2 := Node{Workspace: workspace, NodeID: "file:1", NodeType: NodeTypeFile, Title: "File 1", LastSeen: time.Now().UTC()}
	if err := store.UpsertNodes(ctx, []Node{node1, node2}); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	edgeTypes := []EdgeType{
		EdgeTypeTouched,
		EdgeTypeModified,
		EdgeTypeWorkedOn,
		EdgeTypeCalls,
		EdgeTypeImports,
		EdgeTypeDependsOn,
		EdgeTypeParentOf,
		EdgeTypeAbout,
		EdgeTypeRelatesTo,
	}

	for _, edgeType := range edgeTypes {
		t.Run(string(edgeType), func(t *testing.T) {
			if err := store.UpsertEdge(ctx, Edge{
				Workspace: workspace,
				FromID:    "task:1",
				FromType:  NodeTypeTask,
				ToID:      "file:1",
				ToType:    NodeTypeFile,
				EdgeType:  edgeType,
				CreatedAt: time.Now().UTC(),
			}); err != nil {
				t.Fatalf("UpsertEdge(%q) error = %v", edgeType, err)
			}
		})
	}
}

func TestUpsertEdgeRejectsInvalidTypes(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"
	node1 := Node{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()}
	node2 := Node{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, Title: "Task 2", LastSeen: time.Now().UTC()}
	if err := store.UpsertNodes(ctx, []Node{node1, node2}); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	tests := []struct {
		name string
		edge Edge
	}{
		{
			name: "from type",
			edge: Edge{
				Workspace: workspace, FromID: "task:1", FromType: NodeType("unknown-node-type"),
				ToID: "task:2", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC(),
			},
		},
		{
			name: "to type",
			edge: Edge{
				Workspace: workspace, FromID: "task:1", FromType: NodeTypeTask,
				ToID: "task:2", ToType: NodeType("unknown-node-type"), EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC(),
			},
		},
		{
			name: "edge type",
			edge: Edge{
				Workspace: workspace, FromID: "task:1", FromType: NodeTypeTask,
				ToID: "task:2", ToType: NodeTypeTask, EdgeType: EdgeType("unknown-edge-type"), CreatedAt: time.Now().UTC(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.UpsertEdge(ctx, tt.edge); err == nil {
				t.Fatalf("expected invalid graph edge %s to be rejected", tt.name)
			}

			edges, err := store.GetAllEdges(ctx, workspace)
			if err != nil {
				t.Fatalf("GetAllEdges() error = %v", err)
			}
			if len(edges) != 0 {
				t.Fatalf("invalid edge was persisted: %+v", edges)
			}
		})
	}
}

func TestUpsertEdgesRejectsInvalidTypesWithoutPersistingBatch(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"
	node1 := Node{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()}
	node2 := Node{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, Title: "Task 2", LastSeen: time.Now().UTC()}
	if err := store.UpsertNodes(ctx, []Node{node1, node2}); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	err := store.UpsertEdges(ctx, []Edge{
		{Workspace: workspace, FromID: "task:1", FromType: NodeTypeTask, ToID: "task:2", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC()},
		{Workspace: workspace, FromID: "task:2", FromType: NodeTypeTask, ToID: "task:1", ToType: NodeTypeTask, EdgeType: EdgeType("unknown-edge-type"), CreatedAt: time.Now().UTC()},
	})
	if err == nil {
		t.Fatalf("expected invalid edge type batch to be rejected")
	}

	edges, err := store.GetAllEdges(ctx, workspace)
	if err != nil {
		t.Fatalf("GetAllEdges() error = %v", err)
	}
	if len(edges) != 0 {
		t.Fatalf("invalid edge batch persisted edges: %+v", edges)
	}
}

func TestUpsertEdgesRejectsNegativeMetricsWithoutPersistingBatch(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"
	node1 := Node{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()}
	node2 := Node{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, Title: "Task 2", LastSeen: time.Now().UTC()}
	if err := store.UpsertNodes(ctx, []Node{node1, node2}); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	err := store.UpsertEdges(ctx, []Edge{
		{Workspace: workspace, FromID: "task:1", FromType: NodeTypeTask, ToID: "task:2", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC()},
		{Workspace: workspace, FromID: "task:2", FromType: NodeTypeTask, ToID: "task:1", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, Weight: -0.1, CreatedAt: time.Now().UTC()},
	})
	if err == nil {
		t.Fatalf("expected negative edge weight batch to be rejected")
	}

	edges, err := store.GetAllEdges(ctx, workspace)
	if err != nil {
		t.Fatalf("GetAllEdges() error = %v", err)
	}
	if len(edges) != 0 {
		t.Fatalf("invalid edge batch persisted edges: %+v", edges)
	}
}

func TestCleanupExpiredEdges(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"

	// Create nodes
	node1 := Node{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()}
	node2 := Node{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, Title: "Task 2", LastSeen: time.Now().UTC()}
	if err := store.UpsertNode(ctx, node1); err != nil {
		t.Fatalf("UpsertNode(node1) error = %v", err)
	}
	if err := store.UpsertNode(ctx, node2); err != nil {
		t.Fatalf("UpsertNode(node2) error = %v", err)
	}

	// Create an expired edge (TTL of 1 day, created 2 days ago)
	ttl := 1
	expiredEdge := Edge{
		Workspace: workspace,
		FromID:    "task:1",
		FromType:  NodeTypeTask,
		ToID:      "task:2",
		ToType:    NodeTypeTask,
		EdgeType:  EdgeTypeWorkedOn,
		Weight:    1.0,
		TTLDays:   &ttl,
		CreatedAt: time.Now().UTC().AddDate(0, 0, -2), // 2 days ago
	}
	if err := store.UpsertEdge(ctx, expiredEdge); err != nil {
		t.Fatalf("UpsertEdge() error = %v", err)
	}

	// Verify edge exists
	edges, _ := store.GetAllEdges(ctx, workspace)
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge before cleanup, got %d", len(edges))
	}

	// Cleanup expired edges
	count, err := store.CleanupExpiredEdges(ctx)
	if err != nil {
		t.Fatalf("CleanupExpiredEdges() error = %v", err)
	}
	if count != 1 {
		t.Errorf("CleanupExpiredEdges() removed %d edges, want 1", count)
	}

	// Verify edge was removed
	edges, _ = store.GetAllEdges(ctx, workspace)
	if len(edges) != 0 {
		t.Errorf("expected 0 edges after cleanup, got %d", len(edges))
	}
}

func TestCleanupExpiredEdges_KeepsNonExpired(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"

	// Create nodes
	node1 := Node{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()}
	node2 := Node{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, Title: "Task 2", LastSeen: time.Now().UTC()}
	if err := store.UpsertNode(ctx, node1); err != nil {
		t.Fatalf("UpsertNode(node1) error = %v", err)
	}
	if err := store.UpsertNode(ctx, node2); err != nil {
		t.Fatalf("UpsertNode(node2) error = %v", err)
	}

	// Create an edge with 90 day TTL (not expired)
	ttl := 90
	validEdge := Edge{
		Workspace: workspace,
		FromID:    "task:1",
		FromType:  NodeTypeTask,
		ToID:      "task:2",
		ToType:    NodeTypeTask,
		EdgeType:  EdgeTypeWorkedOn,
		Weight:    1.0,
		TTLDays:   &ttl,
		CreatedAt: time.Now().UTC(), // Just created
	}
	if err := store.UpsertEdge(ctx, validEdge); err != nil {
		t.Fatalf("UpsertEdge() error = %v", err)
	}

	// Cleanup expired edges
	count, err := store.CleanupExpiredEdges(ctx)
	if err != nil {
		t.Fatalf("CleanupExpiredEdges() error = %v", err)
	}
	if count != 0 {
		t.Errorf("CleanupExpiredEdges() removed %d edges, want 0", count)
	}

	// Verify edge still exists
	edges, _ := store.GetAllEdges(ctx, workspace)
	if len(edges) != 1 {
		t.Errorf("expected 1 edge after cleanup, got %d", len(edges))
	}
}

func TestUpsertEdgesRefreshesCreatedAtForSlidingTTL(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"
	node1 := Node{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()}
	node2 := Node{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, Title: "Task 2", LastSeen: time.Now().UTC()}
	if err := store.UpsertNodes(ctx, []Node{node1, node2}); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	ttl := 1
	oldCreatedAt := time.Now().UTC().AddDate(0, 0, -2)
	edge := Edge{
		Workspace: workspace,
		FromID:    "task:1",
		FromType:  NodeTypeTask,
		ToID:      "task:2",
		ToType:    NodeTypeTask,
		EdgeType:  EdgeTypeDependsOn,
		Weight:    1.0,
		TTLDays:   &ttl,
		CreatedAt: oldCreatedAt,
	}
	if err := store.UpsertEdges(ctx, []Edge{edge}); err != nil {
		t.Fatalf("UpsertEdges(old) error = %v", err)
	}

	refreshedAt := time.Now().UTC()
	edge.CreatedAt = refreshedAt
	if err := store.UpsertEdges(ctx, []Edge{edge}); err != nil {
		t.Fatalf("UpsertEdges(refresh) error = %v", err)
	}

	removed, err := store.CleanupExpiredEdges(ctx)
	if err != nil {
		t.Fatalf("CleanupExpiredEdges() error = %v", err)
	}
	if removed != 0 {
		t.Fatalf("CleanupExpiredEdges() removed %d refreshed edge(s), want 0", removed)
	}

	edges, err := store.GetAllEdges(ctx, workspace)
	if err != nil {
		t.Fatalf("GetAllEdges() error = %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("refreshed edge count=%d want 1", len(edges))
	}
	if edges[0].CreatedAt.Before(refreshedAt.Add(-time.Second)) {
		t.Fatalf("edge CreatedAt=%v was not refreshed near %v", edges[0].CreatedAt, refreshedAt)
	}
}

func TestCleanupDanglingEdges(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"

	// Create only one node (edge will be dangling)
	node1 := Node{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()}
	if err := store.UpsertNode(ctx, node1); err != nil {
		t.Fatalf("UpsertNode(node1) error = %v", err)
	}

	// Create an edge to a non-existent node
	danglingEdge := Edge{
		Workspace: workspace,
		FromID:    "task:1",
		FromType:  NodeTypeTask,
		ToID:      "task:nonexistent",
		ToType:    NodeTypeTask,
		EdgeType:  EdgeTypeDependsOn,
		Weight:    1.0,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.UpsertEdge(ctx, danglingEdge); err != nil {
		t.Fatalf("UpsertEdge() error = %v", err)
	}

	// Verify edge exists
	edges, _ := store.GetAllEdges(ctx, workspace)
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge before cleanup, got %d", len(edges))
	}

	// Cleanup dangling edges
	count, err := store.CleanupDanglingEdges(ctx, workspace)
	if err != nil {
		t.Fatalf("CleanupDanglingEdges() error = %v", err)
	}
	if count != 1 {
		t.Errorf("CleanupDanglingEdges() removed %d edges, want 1", count)
	}

	// Verify edge was removed
	edges, _ = store.GetAllEdges(ctx, workspace)
	if len(edges) != 0 {
		t.Errorf("expected 0 edges after cleanup, got %d", len(edges))
	}
}

func TestCleanupDanglingEdges_KeepsValidEdges(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"

	// Create both nodes
	node1 := Node{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()}
	node2 := Node{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, Title: "Task 2", LastSeen: time.Now().UTC()}
	if err := store.UpsertNode(ctx, node1); err != nil {
		t.Fatalf("UpsertNode(node1) error = %v", err)
	}
	if err := store.UpsertNode(ctx, node2); err != nil {
		t.Fatalf("UpsertNode(node2) error = %v", err)
	}

	// Create a valid edge
	validEdge := Edge{
		Workspace: workspace,
		FromID:    "task:1",
		FromType:  NodeTypeTask,
		ToID:      "task:2",
		ToType:    NodeTypeTask,
		EdgeType:  EdgeTypeDependsOn,
		Weight:    1.0,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.UpsertEdge(ctx, validEdge); err != nil {
		t.Fatalf("UpsertEdge() error = %v", err)
	}

	// Cleanup dangling edges
	count, err := store.CleanupDanglingEdges(ctx, workspace)
	if err != nil {
		t.Fatalf("CleanupDanglingEdges() error = %v", err)
	}
	if count != 0 {
		t.Errorf("CleanupDanglingEdges() removed %d edges, want 0", count)
	}

	// Verify edge still exists
	edges, _ := store.GetAllEdges(ctx, workspace)
	if len(edges) != 1 {
		t.Errorf("expected 1 edge after cleanup, got %d", len(edges))
	}
}

func TestRecalculateDegrees(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"

	// Create nodes
	node1 := Node{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()}
	node2 := Node{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, Title: "Task 2", LastSeen: time.Now().UTC()}
	node3 := Node{Workspace: workspace, NodeID: "task:3", NodeType: NodeTypeTask, Title: "Task 3", LastSeen: time.Now().UTC()}
	for _, n := range []Node{node1, node2, node3} {
		if err := store.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode(%s) error = %v", n.NodeID, err)
		}
	}

	// Create edges: 1 -> 2, 1 -> 3, 2 -> 3
	edges := []Edge{
		{Workspace: workspace, FromID: "task:1", FromType: NodeTypeTask, ToID: "task:2", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC()},
		{Workspace: workspace, FromID: "task:1", FromType: NodeTypeTask, ToID: "task:3", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC()},
		{Workspace: workspace, FromID: "task:2", FromType: NodeTypeTask, ToID: "task:3", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC()},
	}
	for _, e := range edges {
		if err := store.UpsertEdge(ctx, e); err != nil {
			t.Fatalf("UpsertEdge(%s->%s) error = %v", e.FromID, e.ToID, err)
		}
	}

	// Recalculate degrees
	if err := store.RecalculateDegrees(ctx, workspace); err != nil {
		t.Fatalf("RecalculateDegrees() error = %v", err)
	}

	// Verify degrees
	got1, _ := store.GetNode(ctx, workspace, "task:1")
	got2, _ := store.GetNode(ctx, workspace, "task:2")
	got3, _ := store.GetNode(ctx, workspace, "task:3")

	// task:1 has 2 outgoing, 0 incoming
	if got1.OutDegree != 2 {
		t.Errorf("task:1 OutDegree = %d, want 2", got1.OutDegree)
	}
	if got1.InDegree != 0 {
		t.Errorf("task:1 InDegree = %d, want 0", got1.InDegree)
	}

	// task:2 has 1 outgoing, 1 incoming
	if got2.OutDegree != 1 {
		t.Errorf("task:2 OutDegree = %d, want 1", got2.OutDegree)
	}
	if got2.InDegree != 1 {
		t.Errorf("task:2 InDegree = %d, want 1", got2.InDegree)
	}

	// task:3 has 0 outgoing, 2 incoming
	if got3.OutDegree != 0 {
		t.Errorf("task:3 OutDegree = %d, want 0", got3.OutDegree)
	}
	if got3.InDegree != 2 {
		t.Errorf("task:3 InDegree = %d, want 2", got3.InDegree)
	}
}

func TestStats(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"

	// Create nodes of different types
	nodes := []Node{
		{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()},
		{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, Title: "Task 2", LastSeen: time.Now().UTC()},
		{Workspace: workspace, NodeID: "file:1", NodeType: NodeTypeFile, Title: "file.go", LastSeen: time.Now().UTC()},
		{Workspace: workspace, NodeID: "session:1", NodeType: NodeTypeSession, Title: "Session 1", LastSeen: time.Now().UTC()},
	}
	for _, n := range nodes {
		if err := store.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode(%s) error = %v", n.NodeID, err)
		}
	}

	// Create edges of different types
	edges := []Edge{
		{Workspace: workspace, FromID: "task:1", FromType: NodeTypeTask, ToID: "task:2", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC()},
		{Workspace: workspace, FromID: "task:1", FromType: NodeTypeTask, ToID: "file:1", ToType: NodeTypeFile, EdgeType: EdgeTypeModified, CreatedAt: time.Now().UTC()},
		{Workspace: workspace, FromID: "session:1", FromType: NodeTypeSession, ToID: "task:1", ToType: NodeTypeTask, EdgeType: EdgeTypeWorkedOn, CreatedAt: time.Now().UTC()},
	}
	for _, e := range edges {
		if err := store.UpsertEdge(ctx, e); err != nil {
			t.Fatalf("UpsertEdge(%s->%s) error = %v", e.FromID, e.ToID, err)
		}
	}

	stats, err := store.Stats(ctx, workspace)
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}

	// Verify node stats
	if stats.Nodes.TotalNodes != 4 {
		t.Errorf("TotalNodes = %d, want 4", stats.Nodes.TotalNodes)
	}
	if stats.Nodes.ByType[NodeTypeTask] != 2 {
		t.Errorf("Task nodes = %d, want 2", stats.Nodes.ByType[NodeTypeTask])
	}
	if stats.Nodes.ByType[NodeTypeFile] != 1 {
		t.Errorf("File nodes = %d, want 1", stats.Nodes.ByType[NodeTypeFile])
	}
	if stats.Nodes.ByType[NodeTypeSession] != 1 {
		t.Errorf("Session nodes = %d, want 1", stats.Nodes.ByType[NodeTypeSession])
	}

	// Verify edge stats
	if stats.Edges.TotalEdges != 3 {
		t.Errorf("TotalEdges = %d, want 3", stats.Edges.TotalEdges)
	}
	if stats.Edges.ByType[EdgeTypeDependsOn] != 1 {
		t.Errorf("DependsOn edges = %d, want 1", stats.Edges.ByType[EdgeTypeDependsOn])
	}
	if stats.Edges.ByType[EdgeTypeModified] != 1 {
		t.Errorf("Modified edges = %d, want 1", stats.Edges.ByType[EdgeTypeModified])
	}
	if stats.Edges.ByType[EdgeTypeWorkedOn] != 1 {
		t.Errorf("WorkedOn edges = %d, want 1", stats.Edges.ByType[EdgeTypeWorkedOn])
	}
}

func TestTopNodes(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"

	// Create nodes with different PageRank values
	setupNodes := []Node{
		{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Low PR Task", PageRank: 0.1, LastSeen: time.Now().UTC()},
		{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, Title: "High PR Task", PageRank: 0.9, LastSeen: time.Now().UTC()},
		{Workspace: workspace, NodeID: "file:1", NodeType: NodeTypeFile, Title: "file.go", PageRank: 0.5, LastSeen: time.Now().UTC()},
	}
	for _, n := range setupNodes {
		if err := store.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode(%s) error = %v", n.NodeID, err)
		}
	}

	// Get top nodes
	nodes, err := store.TopNodes(ctx, TopNodesOptions{
		Workspace: workspace,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("TopNodes() error = %v", err)
	}

	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}

	// Verify order (highest PageRank first)
	if nodes[0].NodeID != "task:2" {
		t.Errorf("expected task:2 first, got %s", nodes[0].NodeID)
	}
	if nodes[1].NodeID != "file:1" {
		t.Errorf("expected file:1 second, got %s", nodes[1].NodeID)
	}
	if nodes[2].NodeID != "task:1" {
		t.Errorf("expected task:1 third, got %s", nodes[2].NodeID)
	}
}

func TestTopNodes_FilterByType(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"

	// Create nodes of different types
	if err := store.UpsertNode(ctx, Node{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task", PageRank: 0.5, LastSeen: time.Now().UTC()}); err != nil {
		t.Fatalf("UpsertNode(task:1) error = %v", err)
	}
	if err := store.UpsertNode(ctx, Node{Workspace: workspace, NodeID: "file:1", NodeType: NodeTypeFile, Title: "file.go", PageRank: 0.5, LastSeen: time.Now().UTC()}); err != nil {
		t.Fatalf("UpsertNode(file:1) error = %v", err)
	}

	// Filter by type
	taskType := NodeTypeTask
	nodes, err := store.TopNodes(ctx, TopNodesOptions{
		Workspace: workspace,
		NodeType:  &taskType,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("TopNodes() error = %v", err)
	}

	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].NodeType != NodeTypeTask {
		t.Errorf("expected task node, got %s", nodes[0].NodeType)
	}
}

func TestBulkUpdatePageRank(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"

	// Create nodes
	if err := store.UpsertNode(ctx, Node{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()}); err != nil {
		t.Fatalf("UpsertNode(task:1) error = %v", err)
	}
	if err := store.UpsertNode(ctx, Node{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, Title: "Task 2", LastSeen: time.Now().UTC()}); err != nil {
		t.Fatalf("UpsertNode(task:2) error = %v", err)
	}

	// Bulk update PageRank
	ranks := map[string]float64{
		"task:1": 0.75,
		"task:2": 0.25,
	}
	if err := store.BulkUpdatePageRank(ctx, workspace, ranks); err != nil {
		t.Fatalf("BulkUpdatePageRank() error = %v", err)
	}

	// Verify updates
	got1, _ := store.GetNode(ctx, workspace, "task:1")
	got2, _ := store.GetNode(ctx, workspace, "task:2")

	if got1.PageRank != 0.75 {
		t.Errorf("task:1 PageRank = %f, want 0.75", got1.PageRank)
	}
	if got2.PageRank != 0.25 {
		t.Errorf("task:2 PageRank = %f, want 0.25", got2.PageRank)
	}
}

func TestUpdatePageRankRejectsNegativeWithoutMutatingExisting(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"
	if err := store.UpsertNode(ctx, Node{
		Workspace: workspace,
		NodeID:    "task:1",
		NodeType:  NodeTypeTask,
		Title:     "Task 1",
		PageRank:  0.5,
		LastSeen:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}

	if err := store.UpdatePageRank(ctx, workspace, "task:1", -0.1); err == nil {
		t.Fatalf("expected negative pagerank update to be rejected")
	}

	got, err := store.GetNode(ctx, workspace, "task:1")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if got.PageRank != 0.5 {
		t.Fatalf("PageRank = %f, want 0.5", got.PageRank)
	}
}

func TestUpdatePageRankRejectsNonFiniteWithoutMutatingExisting(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"
	if err := store.UpsertNode(ctx, Node{
		Workspace: workspace,
		NodeID:    "task:1",
		NodeType:  NodeTypeTask,
		Title:     "Task 1",
		PageRank:  0.5,
		LastSeen:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}

	tests := []struct {
		name     string
		pageRank float64
	}{
		{name: "nan", pageRank: math.NaN()},
		{name: "positive infinity", pageRank: math.Inf(1)},
		{name: "negative infinity", pageRank: math.Inf(-1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.UpdatePageRank(ctx, workspace, "task:1", tt.pageRank); err == nil {
				t.Fatalf("expected non-finite pagerank %s to be rejected", tt.name)
			}

			got, err := store.GetNode(ctx, workspace, "task:1")
			if err != nil {
				t.Fatalf("GetNode() error = %v", err)
			}
			if got.PageRank != 0.5 {
				t.Fatalf("PageRank = %f, want 0.5", got.PageRank)
			}
		})
	}
}

func TestUpdateDegreesRejectsNegativeWithoutMutatingExisting(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"
	if err := store.UpsertNode(ctx, Node{
		Workspace: workspace,
		NodeID:    "task:1",
		NodeType:  NodeTypeTask,
		Title:     "Task 1",
		InDegree:  2,
		OutDegree: 3,
		LastSeen:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}

	if err := store.UpdateDegrees(ctx, workspace, "task:1", -1, 3); err == nil {
		t.Fatalf("expected negative in-degree update to be rejected")
	}
	if err := store.UpdateDegrees(ctx, workspace, "task:1", 2, -1); err == nil {
		t.Fatalf("expected negative out-degree update to be rejected")
	}

	got, err := store.GetNode(ctx, workspace, "task:1")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if got.InDegree != 2 || got.OutDegree != 3 {
		t.Fatalf("degrees = (%d, %d), want (2, 3)", got.InDegree, got.OutDegree)
	}
}

func TestBulkUpdatePageRankRejectsNegativeWithoutPartialMutation(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"
	nodes := []Node{
		{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, PageRank: 0.2, LastSeen: time.Now().UTC()},
		{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, PageRank: 0.3, LastSeen: time.Now().UTC()},
	}
	if err := store.UpsertNodes(ctx, nodes); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	err := store.BulkUpdatePageRank(ctx, workspace, map[string]float64{
		"task:1": 0.9,
		"task:2": -0.1,
	})
	if err == nil {
		t.Fatalf("expected negative pagerank batch to be rejected")
	}

	got1, err := store.GetNode(ctx, workspace, "task:1")
	if err != nil {
		t.Fatalf("GetNode(task:1) error = %v", err)
	}
	got2, err := store.GetNode(ctx, workspace, "task:2")
	if err != nil {
		t.Fatalf("GetNode(task:2) error = %v", err)
	}
	if got1.PageRank != 0.2 || got2.PageRank != 0.3 {
		t.Fatalf("pageranks = (%f, %f), want (0.2, 0.3)", got1.PageRank, got2.PageRank)
	}
}

func TestBulkUpdatePageRankRejectsNonFiniteWithoutPartialMutation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		pageRank float64
	}{
		{name: "nan", pageRank: math.NaN()},
		{name: "positive infinity", pageRank: math.Inf(1)},
		{name: "negative infinity", pageRank: math.Inf(-1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := setupTestStore(t)
			defer store.Close()

			workspace := "/test/workspace"
			nodes := []Node{
				{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, PageRank: 0.2, LastSeen: time.Now().UTC()},
				{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, PageRank: 0.3, LastSeen: time.Now().UTC()},
			}
			if err := store.UpsertNodes(ctx, nodes); err != nil {
				t.Fatalf("UpsertNodes() error = %v", err)
			}

			err := store.BulkUpdatePageRank(ctx, workspace, map[string]float64{
				"task:1": 0.9,
				"task:2": tt.pageRank,
			})
			if err == nil {
				t.Fatalf("expected non-finite pagerank %s batch to be rejected", tt.name)
			}

			got1, err := store.GetNode(ctx, workspace, "task:1")
			if err != nil {
				t.Fatalf("GetNode(task:1) error = %v", err)
			}
			got2, err := store.GetNode(ctx, workspace, "task:2")
			if err != nil {
				t.Fatalf("GetNode(task:2) error = %v", err)
			}
			if got1.PageRank != 0.2 || got2.PageRank != 0.3 {
				t.Fatalf("pageranks = (%f, %f), want (0.2, 0.3)", got1.PageRank, got2.PageRank)
			}
		})
	}
}

func TestDeleteNode(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"

	// Create nodes and edges
	node1 := Node{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()}
	node2 := Node{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, Title: "Task 2", LastSeen: time.Now().UTC()}
	if err := store.UpsertNode(ctx, node1); err != nil {
		t.Fatalf("UpsertNode(node1) error = %v", err)
	}
	if err := store.UpsertNode(ctx, node2); err != nil {
		t.Fatalf("UpsertNode(node2) error = %v", err)
	}
	if err := store.UpsertEdge(ctx, Edge{Workspace: workspace, FromID: "task:1", FromType: NodeTypeTask, ToID: "task:2", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("UpsertEdge() error = %v", err)
	}

	// Delete node1
	if err := store.DeleteNode(ctx, workspace, "task:1"); err != nil {
		t.Fatalf("DeleteNode() error = %v", err)
	}

	// Verify node was deleted
	_, err := store.GetNode(ctx, workspace, "task:1")
	if err == nil {
		t.Error("expected error getting deleted node")
	}

	// Verify edges were also deleted
	edges, _ := store.GetAllEdges(ctx, workspace)
	if len(edges) != 0 {
		t.Errorf("expected 0 edges after deleting node, got %d", len(edges))
	}
}

func TestGetNeighbors(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"

	// Create nodes
	nodes := []Node{
		{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()},
		{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, Title: "Task 2", LastSeen: time.Now().UTC()},
		{Workspace: workspace, NodeID: "task:3", NodeType: NodeTypeTask, Title: "Task 3", LastSeen: time.Now().UTC()},
	}
	for _, n := range nodes {
		if err := store.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode(%s) error = %v", n.NodeID, err)
		}
	}

	// Create edges: 1 -> 2, 3 -> 1
	edges := []Edge{
		{Workspace: workspace, FromID: "task:1", FromType: NodeTypeTask, ToID: "task:2", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC()},
		{Workspace: workspace, FromID: "task:3", FromType: NodeTypeTask, ToID: "task:1", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC()},
	}
	for _, e := range edges {
		if err := store.UpsertEdge(ctx, e); err != nil {
			t.Fatalf("UpsertEdge(%s->%s) error = %v", e.FromID, e.ToID, err)
		}
	}

	// Get all neighbors of task:1
	neighbors, err := store.GetNeighbors(ctx, workspace, "task:1", NeighborOptions{Direction: "both"})
	if err != nil {
		t.Fatalf("GetNeighbors() error = %v", err)
	}

	if len(neighbors) != 2 {
		t.Errorf("expected 2 neighbors, got %d", len(neighbors))
	}

	// Get outgoing neighbors only
	outNeighbors, err := store.GetNeighbors(ctx, workspace, "task:1", NeighborOptions{Direction: "out"})
	if err != nil {
		t.Fatalf("GetNeighbors(out) error = %v", err)
	}
	if len(outNeighbors) != 1 {
		t.Errorf("expected 1 outgoing neighbor, got %d", len(outNeighbors))
	}

	// Get incoming neighbors only
	inNeighbors, err := store.GetNeighbors(ctx, workspace, "task:1", NeighborOptions{Direction: "in"})
	if err != nil {
		t.Fatalf("GetNeighbors(in) error = %v", err)
	}
	if len(inNeighbors) != 1 {
		t.Errorf("expected 1 incoming neighbor, got %d", len(inNeighbors))
	}
}

func TestNodeIDs(t *testing.T) {
	// Test single-arg functions
	singleArgTests := []struct {
		name     string
		fn       func(string) string
		input    string
		expected string
	}{
		{"TaskNodeID", TaskNodeID, "abc123", "task:abc123"},
		{"SessionNodeID", SessionNodeID, "xyz789", "session:xyz789"},
		{"FileNodeID", FileNodeID, "/path/to/file.go", "file:/path/to/file.go"},
		{"MemoryNodeID", MemoryNodeID, "mem001", "memory:mem001"},
	}

	for _, tt := range singleArgTests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn(tt.input)
			if got != tt.expected {
				t.Errorf("%s(%q) = %q, want %q", tt.name, tt.input, got, tt.expected)
			}
		})
	}

	// Test SymbolNodeID separately (takes two args)
	t.Run("SymbolNodeID", func(t *testing.T) {
		got := SymbolNodeID("abcdef123456", "main.Handler")
		expected := "symbol:abcdef123456:main.Handler"
		if got != expected {
			t.Errorf("SymbolNodeID = %q, want %q", got, expected)
		}
	})

	// Test SymbolNodeID hash truncation
	t.Run("SymbolNodeID_truncates_long_hash", func(t *testing.T) {
		longHash := "abcdef1234567890abcdef"
		got := SymbolNodeID(longHash, "pkg.Func")
		expected := "symbol:abcdef123456:pkg.Func"
		if got != expected {
			t.Errorf("SymbolNodeID = %q, want %q", got, expected)
		}
	})
}

func TestSearchNodes(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"

	// Create nodes with various titles and paths
	nodes := []Node{
		{Workspace: workspace, NodeID: "file:src/auth/handler.go", NodeType: NodeTypeFile, Title: "Auth Handler", CurrentPath: "src/auth/handler.go", PageRank: 0.8, LastSeen: time.Now().UTC()},
		{Workspace: workspace, NodeID: "file:src/auth/service.go", NodeType: NodeTypeFile, Title: "Auth Service", CurrentPath: "src/auth/service.go", PageRank: 0.6, LastSeen: time.Now().UTC()},
		{Workspace: workspace, NodeID: "file:src/db/pool.go", NodeType: NodeTypeFile, Title: "Database Pool", CurrentPath: "src/db/pool.go", PageRank: 0.9, LastSeen: time.Now().UTC()},
		{Workspace: workspace, NodeID: "task:123", NodeType: NodeTypeTask, Title: "Fix authentication bug", PageRank: 0.5, LastSeen: time.Now().UTC()},
	}
	for _, n := range nodes {
		if err := store.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode(%s) error = %v", n.NodeID, err)
		}
	}

	// Search for "auth" - should find handler, service, and task
	results, err := store.SearchNodes(ctx, workspace, "auth", 10)
	if err != nil {
		t.Fatalf("SearchNodes(auth) error = %v", err)
	}
	if len(results) != 3 {
		t.Errorf("SearchNodes(auth) returned %d results, want 3", len(results))
	}

	// Verify ordered by PageRank (handler: 0.8, service: 0.6, task: 0.5)
	if len(results) >= 1 && results[0].NodeID != "file:src/auth/handler.go" {
		t.Errorf("expected highest PageRank first, got %s", results[0].NodeID)
	}

	// Search for "database" - should find db/pool.go via title
	results, err = store.SearchNodes(ctx, workspace, "database", 10)
	if err != nil {
		t.Fatalf("SearchNodes(database) error = %v", err)
	}
	if len(results) != 1 {
		t.Errorf("SearchNodes(database) returned %d results, want 1", len(results))
	}

	// Search for ".go" in path - should find all 3 files
	results, err = store.SearchNodes(ctx, workspace, ".go", 10)
	if err != nil {
		t.Fatalf("SearchNodes(.go) error = %v", err)
	}
	if len(results) != 3 {
		t.Errorf("SearchNodes(.go) returned %d results, want 3", len(results))
	}

	// Search for non-existent term
	results, err = store.SearchNodes(ctx, workspace, "xyz123", 10)
	if err != nil {
		t.Fatalf("SearchNodes(xyz123) error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("SearchNodes(xyz123) returned %d results, want 0", len(results))
	}
}

func TestSearchNodes_CaseInsensitive(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"

	if err := store.UpsertNode(ctx, Node{
		Workspace:   workspace,
		NodeID:      "file:Auth.go",
		NodeType:    NodeTypeFile,
		Title:       "Authentication Module",
		CurrentPath: "Auth.go",
		PageRank:    0.5,
		LastSeen:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}

	// Search with different cases
	for _, term := range []string{"auth", "AUTH", "Auth", "AuTh"} {
		results, err := store.SearchNodes(ctx, workspace, term, 10)
		if err != nil {
			t.Fatalf("SearchNodes(%s) error = %v", term, err)
		}
		if len(results) != 1 {
			t.Errorf("SearchNodes(%s) returned %d results, want 1", term, len(results))
		}
	}
}

func TestGetEdgesBetween(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"

	// Create nodes
	nodes := []Node{
		{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()},
		{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, Title: "Task 2", LastSeen: time.Now().UTC()},
		{Workspace: workspace, NodeID: "task:3", NodeType: NodeTypeTask, Title: "Task 3", LastSeen: time.Now().UTC()},
		{Workspace: workspace, NodeID: "task:4", NodeType: NodeTypeTask, Title: "Task 4", LastSeen: time.Now().UTC()},
	}
	for _, n := range nodes {
		if err := store.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode(%s) error = %v", n.NodeID, err)
		}
	}

	// Create edges: 1->2, 2->3, 3->4, 1->4
	edges := []Edge{
		{Workspace: workspace, FromID: "task:1", FromType: NodeTypeTask, ToID: "task:2", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC()},
		{Workspace: workspace, FromID: "task:2", FromType: NodeTypeTask, ToID: "task:3", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC()},
		{Workspace: workspace, FromID: "task:3", FromType: NodeTypeTask, ToID: "task:4", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC()},
		{Workspace: workspace, FromID: "task:1", FromType: NodeTypeTask, ToID: "task:4", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC()},
	}
	for _, e := range edges {
		if err := store.UpsertEdge(ctx, e); err != nil {
			t.Fatalf("UpsertEdge(%s->%s) error = %v", e.FromID, e.ToID, err)
		}
	}

	// Get edges between {1, 2} - should find 1->2
	result, err := store.GetEdgesBetween(ctx, workspace, []string{"task:1", "task:2"})
	if err != nil {
		t.Fatalf("GetEdgesBetween({1,2}) error = %v", err)
	}
	if len(result) != 1 {
		t.Errorf("GetEdgesBetween({1,2}) returned %d edges, want 1", len(result))
	}

	// Get edges between {1, 2, 3} - should find 1->2, 2->3
	result, err = store.GetEdgesBetween(ctx, workspace, []string{"task:1", "task:2", "task:3"})
	if err != nil {
		t.Fatalf("GetEdgesBetween({1,2,3}) error = %v", err)
	}
	if len(result) != 2 {
		t.Errorf("GetEdgesBetween({1,2,3}) returned %d edges, want 2", len(result))
	}

	// Get edges between {1, 4} - should find 1->4
	result, err = store.GetEdgesBetween(ctx, workspace, []string{"task:1", "task:4"})
	if err != nil {
		t.Fatalf("GetEdgesBetween({1,4}) error = %v", err)
	}
	if len(result) != 1 {
		t.Errorf("GetEdgesBetween({1,4}) returned %d edges, want 1", len(result))
	}

	// Get edges between all - should find all 4
	result, err = store.GetEdgesBetween(ctx, workspace, []string{"task:1", "task:2", "task:3", "task:4"})
	if err != nil {
		t.Fatalf("GetEdgesBetween(all) error = %v", err)
	}
	if len(result) != 4 {
		t.Errorf("GetEdgesBetween(all) returned %d edges, want 4", len(result))
	}

	// Get edges between empty set
	result, err = store.GetEdgesBetween(ctx, workspace, []string{})
	if err != nil {
		t.Fatalf("GetEdgesBetween([]) error = %v", err)
	}
	if len(result) != 0 {
		t.Errorf("GetEdgesBetween([]) returned %d edges, want 0", len(result))
	}
}

func TestFindShortestPath(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"

	// Create a graph:
	//   1 -> 2 -> 3
	//   |         ^
	//   +-> 4 ----+
	nodes := []Node{
		{Workspace: workspace, NodeID: "n:1", NodeType: NodeTypeTask, Title: "Node 1", LastSeen: time.Now().UTC()},
		{Workspace: workspace, NodeID: "n:2", NodeType: NodeTypeTask, Title: "Node 2", LastSeen: time.Now().UTC()},
		{Workspace: workspace, NodeID: "n:3", NodeType: NodeTypeTask, Title: "Node 3", LastSeen: time.Now().UTC()},
		{Workspace: workspace, NodeID: "n:4", NodeType: NodeTypeTask, Title: "Node 4", LastSeen: time.Now().UTC()},
	}
	for _, n := range nodes {
		if err := store.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode(%s) error = %v", n.NodeID, err)
		}
	}

	edges := []Edge{
		{Workspace: workspace, FromID: "n:1", FromType: NodeTypeTask, ToID: "n:2", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC()},
		{Workspace: workspace, FromID: "n:2", FromType: NodeTypeTask, ToID: "n:3", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC()},
		{Workspace: workspace, FromID: "n:1", FromType: NodeTypeTask, ToID: "n:4", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC()},
		{Workspace: workspace, FromID: "n:4", FromType: NodeTypeTask, ToID: "n:3", ToType: NodeTypeTask, EdgeType: EdgeTypeDependsOn, CreatedAt: time.Now().UTC()},
	}
	for _, e := range edges {
		if err := store.UpsertEdge(ctx, e); err != nil {
			t.Fatalf("UpsertEdge(%s->%s) error = %v", e.FromID, e.ToID, err)
		}
	}

	// Find path from 1 to 3 - should find two paths of length 3
	paths, err := store.FindShortestPath(ctx, workspace, "n:1", "n:3", 5)
	if err != nil {
		t.Fatalf("FindShortestPath(1->3) error = %v", err)
	}
	if len(paths) != 2 {
		t.Errorf("FindShortestPath(1->3) returned %d paths, want 2", len(paths))
	}
	for _, path := range paths {
		if len(path) != 3 {
			t.Errorf("expected path length 3, got %d: %v", len(path), path)
		}
		if path[0] != "n:1" || path[len(path)-1] != "n:3" {
			t.Errorf("path should start with n:1 and end with n:3, got %v", path)
		}
	}

	// Find path to self - should return single-element path
	paths, err = store.FindShortestPath(ctx, workspace, "n:1", "n:1", 5)
	if err != nil {
		t.Fatalf("FindShortestPath(1->1) error = %v", err)
	}
	if len(paths) != 1 || len(paths[0]) != 1 {
		t.Errorf("FindShortestPath(1->1) should return [[n:1]], got %v", paths)
	}

	// Find path with maxDepth=1 - should find direct neighbors only
	paths, err = store.FindShortestPath(ctx, workspace, "n:1", "n:2", 1)
	if err != nil {
		t.Fatalf("FindShortestPath(1->2, depth=1) error = %v", err)
	}
	if len(paths) != 1 {
		t.Errorf("FindShortestPath(1->2, depth=1) returned %d paths, want 1", len(paths))
	}

	// Find path to unreachable node (simulate by using non-existent node)
	paths, err = store.FindShortestPath(ctx, workspace, "n:1", "n:999", 5)
	if err != nil {
		t.Fatalf("FindShortestPath(1->999) error = %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("FindShortestPath(1->999) should return empty, got %v", paths)
	}
}

func TestFindShortestPath_MaxDepthLimit(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()
	ctx := context.Background()

	workspace := "/test/workspace"

	// Create a linear chain: 1 -> 2 -> 3 -> 4 -> 5 -> 6
	for i := 1; i <= 6; i++ {
		nodeID := "n:" + string(rune('0'+i))
		if err := store.UpsertNode(ctx, Node{
			Workspace: workspace,
			NodeID:    nodeID,
			NodeType:  NodeTypeTask,
			Title:     "Node " + string(rune('0'+i)),
			LastSeen:  time.Now().UTC(),
		}); err != nil {
			t.Fatalf("UpsertNode(%s) error = %v", nodeID, err)
		}
	}

	for i := 1; i < 6; i++ {
		fromID := "n:" + string(rune('0'+i))
		toID := "n:" + string(rune('0'+i+1))
		if err := store.UpsertEdge(ctx, Edge{
			Workspace: workspace,
			FromID:    fromID,
			FromType:  NodeTypeTask,
			ToID:      toID,
			ToType:    NodeTypeTask,
			EdgeType:  EdgeTypeDependsOn,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("UpsertEdge(%s->%s) error = %v", fromID, toID, err)
		}
	}

	// Path from 1 to 6 requires 5 hops - should work with maxDepth=5
	paths, err := store.FindShortestPath(ctx, workspace, "n:1", "n:6", 5)
	if err != nil {
		t.Fatalf("FindShortestPath(1->6, depth=5) error = %v", err)
	}
	if len(paths) != 1 {
		t.Errorf("FindShortestPath(1->6, depth=5) returned %d paths, want 1", len(paths))
	}

	// Path from 1 to 6 with maxDepth=3 - should fail to find
	paths, err = store.FindShortestPath(ctx, workspace, "n:1", "n:6", 3)
	if err != nil {
		t.Fatalf("FindShortestPath(1->6, depth=3) error = %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("FindShortestPath(1->6, depth=3) should return empty, got %d paths", len(paths))
	}
}

// Helper function to set up a test store
func setupTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	store, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return store
}
