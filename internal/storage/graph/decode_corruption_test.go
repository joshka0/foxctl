package graph

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"testing/quick"
	"time"
)

func TestDecodeRejectsCorruptGraphTypes(t *testing.T) {
	ctx := context.Background()

	t.Run("node type", func(t *testing.T) {
		store := setupTestStore(t)
		defer store.Close()

		node := Node{
			Workspace: "/test/workspace",
			NodeID:    "task:1",
			NodeType:  NodeTypeTask,
			Title:     "Task 1",
			LastSeen:  time.Now().UTC(),
		}
		if err := store.UpsertNode(ctx, node); err != nil {
			t.Fatalf("UpsertNode() error = %v", err)
		}
		mustExecDecodeTest(t, store, `UPDATE graph_nodes SET node_type = ? WHERE node_id = ?`, "unknown-node-type", "task:1")

		_, err := store.GetNode(ctx, "/test/workspace", "task:1")
		requireDecodeError(t, err, "node_type")
	})

	t.Run("edge from type", func(t *testing.T) {
		store := setupGraphDecodeEdge(t)
		defer store.Close()

		mustExecDecodeTest(t, store, `UPDATE graph_edges SET from_type = ? WHERE id = ?`, "unknown-node-type", "edge:1")

		_, err := store.GetEdge(ctx, "edge:1")
		requireDecodeError(t, err, "from_type")
	})

	t.Run("edge to type", func(t *testing.T) {
		store := setupGraphDecodeEdge(t)
		defer store.Close()

		mustExecDecodeTest(t, store, `UPDATE graph_edges SET to_type = ? WHERE id = ?`, "unknown-node-type", "edge:1")

		_, err := store.GetEdge(ctx, "edge:1")
		requireDecodeError(t, err, "to_type")
	})

	t.Run("edge type", func(t *testing.T) {
		store := setupGraphDecodeEdge(t)
		defer store.Close()

		mustExecDecodeTest(t, store, `UPDATE graph_edges SET edge_type = ? WHERE id = ?`, "unknown-edge-type", "edge:1")

		_, err := store.GetEdge(ctx, "edge:1")
		requireDecodeError(t, err, "edge_type")
	})
}

func TestDecodeRejectsCorruptGraphMetadata(t *testing.T) {
	ctx := context.Background()

	for _, badMetadata := range []struct {
		name  string
		value string
	}{
		{name: "malformed", value: `{"kind":`},
		{name: "null", value: `null`},
		{name: "array", value: `["task"]`},
	} {
		t.Run("node metadata "+badMetadata.name, func(t *testing.T) {
			store := setupTestStore(t)
			defer store.Close()

			node := Node{
				Workspace: "/test/workspace",
				NodeID:    "task:metadata",
				NodeType:  NodeTypeTask,
				Title:     "Task Metadata",
				PageRank:  0.5,
				LastSeen:  time.Now().UTC(),
				Metadata:  map[string]string{"kind": "task"},
			}
			if err := store.UpsertNode(ctx, node); err != nil {
				t.Fatalf("UpsertNode() error = %v", err)
			}
			mustExecDecodeTest(t, store, `UPDATE graph_nodes SET metadata = ? WHERE node_id = ?`, badMetadata.value, node.NodeID)

			_, err := store.GetNode(ctx, node.Workspace, node.NodeID)
			requireDecodeError(t, err, "metadata")
			_, err = store.GetAllNodes(ctx, node.Workspace)
			requireDecodeError(t, err, "metadata")
			_, err = store.TopNodes(ctx, TopNodesOptions{Workspace: node.Workspace, MinRank: 0})
			requireDecodeError(t, err, "metadata")
			_, err = store.SearchNodes(ctx, node.Workspace, "metadata", 10)
			requireDecodeError(t, err, "metadata")
		})

		t.Run("edge metadata "+badMetadata.name, func(t *testing.T) {
			store := setupGraphDecodeEdge(t)
			defer store.Close()

			mustExecDecodeTest(t, store, `UPDATE graph_edges SET metadata = ? WHERE id = ?`, badMetadata.value, "edge:1")

			_, err := store.GetEdge(ctx, "edge:1")
			requireDecodeError(t, err, "metadata")
			_, err = store.GetAllEdges(ctx, "/test/workspace")
			requireDecodeError(t, err, "metadata")
			_, err = store.GetEdgesFrom(ctx, "/test/workspace", "task:1", nil)
			requireDecodeError(t, err, "metadata")
			_, err = store.GetEdgesTo(ctx, "/test/workspace", "task:2", nil)
			requireDecodeError(t, err, "metadata")
			_, err = store.GetEdgesBetween(ctx, "/test/workspace", []string{"task:1", "task:2"})
			requireDecodeError(t, err, "metadata")
		})
	}
}

func TestGraphMetadataJSONProperty(t *testing.T) {
	roundTripsObjectMetadata := func(input map[string]string) bool {
		if input == nil {
			input = map[string]string{}
		}
		encoded, err := encodeGraphMetadata(input)
		if err != nil {
			return false
		}
		if len(input) == 0 {
			return encoded == ""
		}

		var expected map[string]string
		if err := json.Unmarshal([]byte(encoded), &expected); err != nil {
			return false
		}
		got, err := decodeGraphMetadata("property metadata", encoded)
		return err == nil && reflect.DeepEqual(got, expected)
	}

	if err := quick.Check(roundTripsObjectMetadata, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("graph metadata JSON property failed: %v", err)
	}
}

func TestGetNeighborsRejectsCorruptNeighborNode(t *testing.T) {
	ctx := context.Background()
	store := setupGraphDecodeEdge(t)
	defer store.Close()

	mustExecDecodeTest(t, store, `UPDATE graph_nodes SET metadata = ? WHERE node_id = ?`, `null`, "task:2")

	_, err := store.GetNeighbors(ctx, "/test/workspace", "task:1", NeighborOptions{Direction: "out"})
	requireDecodeError(t, err, "metadata")
}

func setupGraphDecodeEdge(t *testing.T) *SQLiteStore {
	t.Helper()
	ctx := context.Background()
	store := setupTestStore(t)
	workspace := "/test/workspace"

	nodes := []Node{
		{Workspace: workspace, NodeID: "task:1", NodeType: NodeTypeTask, Title: "Task 1", LastSeen: time.Now().UTC()},
		{Workspace: workspace, NodeID: "task:2", NodeType: NodeTypeTask, Title: "Task 2", LastSeen: time.Now().UTC()},
	}
	if err := store.UpsertNodes(ctx, nodes); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	if err := store.UpsertEdge(ctx, Edge{
		ID:        "edge:1",
		Workspace: workspace,
		FromID:    "task:1",
		FromType:  NodeTypeTask,
		ToID:      "task:2",
		ToType:    NodeTypeTask,
		EdgeType:  EdgeTypeDependsOn,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertEdge() error = %v", err)
	}
	return store
}

func mustExecDecodeTest(t *testing.T, store *SQLiteStore, query string, args ...any) {
	t.Helper()
	if _, err := store.db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec corrupt fixture: %v", err)
	}
}

func requireDecodeError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected decode error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want substring %q", err.Error(), want)
	}
}
