package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	flow "github.com/joshka0/foxctl/internal/runtime/flow"
	"github.com/oklog/ulid/v2"
)

// newTestStore creates a temp-dir SQLite store for testing.
func newTestStore(t *testing.T) flow.Store {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	store, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func newFlow(name, workspace string) flow.Flow {
	now := time.Now().UTC()
	return flow.Flow{
		ID:          ulid.Make().String(),
		Name:        name,
		Workspace:   workspace,
		State:       flow.FlowDraft,
		Description: "",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func newFlowWithDesc(name, workspace, desc string) flow.Flow {
	f := newFlow(name, workspace)
	f.Description = desc
	return f
}

// TestMain sets up test invariants. sqliteutil enables PRAGMA foreign_keys=ON
// for all connections, but we assert it at runtime so cascade tests have a
// deterministic guarantee rather than relying on package-level defaults.
func TestMain(m *testing.M) {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "flow-test-*")
	if err != nil {
		panic(fmt.Sprintf("TestMain temp dir: %v", err))
	}
	defer os.RemoveAll(dir)
	store, err := Open(ctx, dir)
	if err != nil {
		panic(fmt.Sprintf("TestMain open: %v", err))
	}
	db := store.(*sqlStore).db
	var fk int
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		panic(fmt.Sprintf("TestMain pragma query: %v", err))
	}
	if fk != 1 {
		panic(fmt.Sprintf("TestMain: PRAGMA foreign_keys=%d, want 1", fk))
	}
	_ = store.Close()
	m.Run()
}

// ---------------------------------------------------------------------------
// Open & Migration
// ---------------------------------------------------------------------------

func TestOpenAndMigrate(t *testing.T) {
	store := newTestStore(t)
	if store == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestIdempotentMigration(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Open once — first migration.
	store1, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}

	// Create a flow so we can verify data survives re-migration.
	f := newFlow("persist-test", "/ws")
	_, err = store1.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}
	_ = store1.Close()

	// Open again — migration should be idempotent.
	store2, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("second open (idempotent migration): %v", err)
	}
	t.Cleanup(func() { _ = store2.Close() })

	// Verify data survived.
	got, err := store2.GetFlow(ctx, f.ID)
	if err != nil {
		t.Fatalf("get flow after re-migration: %v", err)
	}
	if got.Name != "persist-test" {
		t.Errorf("flow name after re-migration = %q, want %q", got.Name, "persist-test")
	}
}

func TestOpenCreatesDBFile(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	store, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = store.Close()

	dbPath := filepath.Join(dir, "flow.db")
	if _, err := filepath.Glob(dbPath); err != nil {
		t.Fatalf("glob flow.db: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Flow CRUD
// ---------------------------------------------------------------------------

func TestCreateFlow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	before := time.Now().UTC()
	f := newFlow("test-flow", "/workspace")
	got, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}
	after := time.Now().UTC()

	if got.ID != f.ID {
		t.Errorf("ID = %q, want %q", got.ID, f.ID)
	}
	if got.Name != "test-flow" {
		t.Errorf("Name = %q, want %q", got.Name, "test-flow")
	}
	if got.Workspace != "/workspace" {
		t.Errorf("Workspace = %q, want %q", got.Workspace, "/workspace")
	}
	if got.State != flow.FlowDraft {
		t.Errorf("State = %q, want %q", got.State, flow.FlowDraft)
	}
	if got.CreatedAt.Before(before) || got.CreatedAt.After(after) {
		t.Errorf("CreatedAt = %v, want between %v and %v", got.CreatedAt, before, after)
	}
	if got.UpdatedAt.Before(before) || got.UpdatedAt.After(after) {
		t.Errorf("UpdatedAt = %v, want between %v and %v", got.UpdatedAt, before, after)
	}
}

func TestCreateFlowWithDescription(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlowWithDesc("desc-flow", "/ws", "A test description")
	got, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}
	if got.Description != "A test description" {
		t.Errorf("Description = %q, want %q", got.Description, "A test description")
	}
}

func TestCreateFlowNoDescription(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("nodesc", "/ws")
	got, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}
	if got.Description != "" {
		t.Errorf("Description = %q, want empty", got.Description)
	}
}

func TestCreateFlowDuplicateNameRejected(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f1 := newFlow("same-name", "/ws")
	if _, err := store.CreateFlow(ctx, f1); err != nil {
		t.Fatalf("first create: %v", err)
	}

	f2 := newFlow("same-name", "/ws")
	_, err := store.CreateFlow(ctx, f2)
	if err == nil {
		t.Fatal("expected error for duplicate name, got nil")
	}
}

func TestCreateFlowSameNameDifferentWorkspace(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f1 := newFlow("same-name", "/ws-a")
	if _, err := store.CreateFlow(ctx, f1); err != nil {
		t.Fatalf("create ws-a: %v", err)
	}

	f2 := newFlow("same-name", "/ws-b")
	_, err := store.CreateFlow(ctx, f2)
	if err != nil {
		t.Fatalf("expected same name in different workspace to succeed, got: %v", err)
	}
}

func TestGetFlow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("get-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := store.GetFlow(ctx, created.ID)
	if err != nil {
		t.Fatalf("get flow: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
	if got.Name != "get-test" {
		t.Errorf("Name = %q, want %q", got.Name, "get-test")
	}
}

func TestGetFlowNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.GetFlow(ctx, "nonexistent-id")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != flow.ErrNotFound.Error() {
		t.Errorf("error = %q, want %q", err.Error(), flow.ErrNotFound.Error())
	}
}

func TestGetFlowByName(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("by-name", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := store.GetFlowByName(ctx, "/ws", "by-name")
	if err != nil {
		t.Fatalf("get by name: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
}

func TestGetFlowByNameNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.GetFlowByName(ctx, "/ws", "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != flow.ErrNotFound.Error() {
		t.Errorf("error = %q, want %q", err.Error(), flow.ErrNotFound.Error())
	}
}

func TestListFlows(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Create flows in workspace A.
	for i := 0; i < 3; i++ {
		f := newFlow(fmt.Sprintf("flow-%d", i), "/ws-a")
		if _, err := store.CreateFlow(ctx, f); err != nil {
			t.Fatalf("create flow-%d: %v", i, err)
		}
	}

	// Create flow in workspace B.
	fb := newFlow("other-ws", "/ws-b")
	if _, err := store.CreateFlow(ctx, fb); err != nil {
		t.Fatalf("create other-ws: %v", err)
	}

	// List workspace A.
	flows, err := store.ListFlows(ctx, "/ws-a")
	if err != nil {
		t.Fatalf("list flows: %v", err)
	}
	if len(flows) != 3 {
		t.Errorf("len(flows) = %d, want 3", len(flows))
	}

	// List workspace B.
	flowsB, err := store.ListFlows(ctx, "/ws-b")
	if err != nil {
		t.Fatalf("list flows B: %v", err)
	}
	if len(flowsB) != 1 {
		t.Errorf("len(flowsB) = %d, want 1", len(flowsB))
	}
}

func TestListFlowsEmpty(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	flows, err := store.ListFlows(ctx, "/empty-ws")
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if flows == nil {
		t.Fatal("expected non-nil slice for empty list")
	}
	if len(flows) != 0 {
		t.Errorf("len(flows) = %d, want 0", len(flows))
	}
}

func TestUpdateFlow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("update-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Update state and description.
	time.Sleep(1 * time.Millisecond) // ensure updated_at changes
	created.State = flow.FlowRunning
	created.Description = "updated desc"

	updated, err := store.UpdateFlow(ctx, created)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.State != flow.FlowRunning {
		t.Errorf("State = %q, want %q", updated.State, flow.FlowRunning)
	}
	if updated.Description != "updated desc" {
		t.Errorf("Description = %q, want %q", updated.Description, "updated desc")
	}
	if !updated.UpdatedAt.After(created.CreatedAt) && updated.UpdatedAt != created.UpdatedAt {
		t.Errorf("UpdatedAt = %v, should be >= CreatedAt %v", updated.UpdatedAt, created.CreatedAt)
	}
}

func TestUpdateFlowNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := flow.Flow{ID: "nonexistent", State: flow.FlowRunning}
	_, err := store.UpdateFlow(ctx, f)
	if err == nil {
		t.Fatal("expected error for updating nonexistent flow")
	}
	if err.Error() != flow.ErrNotFound.Error() {
		t.Errorf("error = %q, want %q", err.Error(), flow.ErrNotFound.Error())
	}
}

func TestDeleteFlow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("delete-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := store.DeleteFlow(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = store.GetFlow(ctx, created.ID)
	if err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestDeleteFlowNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	err := store.DeleteFlow(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error deleting nonexistent flow")
	}
	if err.Error() != flow.ErrNotFound.Error() {
		t.Errorf("error = %q, want %q", err.Error(), flow.ErrNotFound.Error())
	}
}

func TestDeleteFlowCascadesNodesEdgesRuns(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Create flow.
	f := newFlow("cascade-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Add nodes.
	nodeA := flow.FlowNode{
		ID:     ulid.Make().String(),
		FlowID: created.ID,
		Kind:   flow.NodeSkill,
		Label:  "source",
		Config: json.RawMessage(`{"skill":"test"}`),
	}
	nodeA, err = store.AddNode(ctx, nodeA)
	if err != nil {
		t.Fatalf("add node a: %v", err)
	}

	nodeB := flow.FlowNode{
		ID:     ulid.Make().String(),
		FlowID: created.ID,
		Kind:   flow.NodeTransform,
		Label:  "transform",
		Config: json.RawMessage(`{"transform":"passthrough"}`),
	}
	nodeB, err = store.AddNode(ctx, nodeB)
	if err != nil {
		t.Fatalf("add node b: %v", err)
	}

	// Add edge.
	edge := flow.FlowEdge{
		ID:         ulid.Make().String(),
		FlowID:     created.ID,
		FromNodeID: nodeA.ID,
		ToNodeID:   nodeB.ID,
		Transform:  flow.TransformPassthrough,
		Trigger:    flow.TriggerOutputReady,
	}
	_, err = store.AddEdge(ctx, edge)
	if err != nil {
		t.Fatalf("add edge: %v", err)
	}

	// Add run.
	run := flow.FlowRun{
		ID:        ulid.Make().String(),
		FlowID:    created.ID,
		State:     flow.RunRunning,
		StartedAt: time.Now().UTC(),
	}
	_, err = store.CreateRun(ctx, run)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	// Delete flow — should cascade.
	if err := store.DeleteFlow(ctx, created.ID); err != nil {
		t.Fatalf("delete flow: %v", err)
	}

	// Verify nodes gone.
	nodes, err := store.ListNodesByFlow(ctx, created.ID)
	if err != nil {
		t.Fatalf("list nodes after delete: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("nodes after cascade delete = %d, want 0", len(nodes))
	}

	// Verify edges gone.
	edges, err := store.ListEdgesByFlow(ctx, created.ID)
	if err != nil {
		t.Fatalf("list edges after delete: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("edges after cascade delete = %d, want 0", len(edges))
	}
}

// ---------------------------------------------------------------------------
// Node CRUD
// ---------------------------------------------------------------------------

func TestAddNode(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("node-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	node := flow.FlowNode{
		ID:     ulid.Make().String(),
		FlowID: created.ID,
		Kind:   flow.NodeSkill,
		Label:  "search",
		Config: json.RawMessage(`{"skill":"code/semantic_search"}`),
	}
	got, err := store.AddNode(ctx, node)
	if err != nil {
		t.Fatalf("add node: %v", err)
	}
	if got.ID != node.ID {
		t.Errorf("ID = %q, want %q", got.ID, node.ID)
	}
	if got.Kind != flow.NodeSkill {
		t.Errorf("Kind = %q, want %q", got.Kind, flow.NodeSkill)
	}
	if got.Label != "search" {
		t.Errorf("Label = %q, want %q", got.Label, "search")
	}
}

func TestAddNodeAllKinds(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("kinds-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	kinds := []flow.NodeKind{
		flow.NodeSkill, flow.NodePTY, flow.NodeHTTP,
		flow.NodePlaywright, flow.NodeImage, flow.NodeTransform,
	}

	for _, kind := range kinds {
		node := flow.FlowNode{
			ID:     ulid.Make().String(),
			FlowID: created.ID,
			Kind:   kind,
			Label:  string(kind) + "-node",
			Config: json.RawMessage(`{}`),
		}
		got, err := store.AddNode(ctx, node)
		if err != nil {
			t.Fatalf("add node kind %s: %v", kind, err)
		}
		if got.Kind != kind {
			t.Errorf("kind %s: got.Kind = %q, want %q", kind, got.Kind, kind)
		}
	}

	nodes, err := store.ListNodesByFlow(ctx, created.ID)
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes) != len(kinds) {
		t.Errorf("len(nodes) = %d, want %d", len(nodes), len(kinds))
	}
}

func TestAddNodeWithPosition(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("pos-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	node := flow.FlowNode{
		ID:       ulid.Make().String(),
		FlowID:   created.ID,
		Kind:     flow.NodeSkill,
		Label:    "with-pos",
		Config:   json.RawMessage(`{}`),
		Position: &flow.Position{X: 100, Y: 200},
	}
	got, err := store.AddNode(ctx, node)
	if err != nil {
		t.Fatalf("add node: %v", err)
	}
	if got.Position == nil {
		t.Fatal("Position is nil, want non-nil")
	}
	if got.Position.X != 100 || got.Position.Y != 200 {
		t.Errorf("Position = {%v, %v}, want {100, 200}", got.Position.X, got.Position.Y)
	}
}

func TestAddNodeNullPosition(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("nopos-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	node := flow.FlowNode{
		ID:     ulid.Make().String(),
		FlowID: created.ID,
		Kind:   flow.NodeSkill,
		Label:  "no-pos",
		Config: json.RawMessage(`{}`),
	}
	got, err := store.AddNode(ctx, node)
	if err != nil {
		t.Fatalf("add node: %v", err)
	}
	if got.Position != nil {
		t.Errorf("Position = %+v, want nil", got.Position)
	}
}

func TestGetNode(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("get-node-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	node := flow.FlowNode{
		ID:     ulid.Make().String(),
		FlowID: created.ID,
		Kind:   flow.NodeSkill,
		Label:  "findme",
		Config: json.RawMessage(`{"skill":"test"}`),
	}
	added, err := store.AddNode(ctx, node)
	if err != nil {
		t.Fatalf("add node: %v", err)
	}

	got, err := store.GetNode(ctx, added.ID)
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if got.Label != "findme" {
		t.Errorf("Label = %q, want %q", got.Label, "findme")
	}
}

func TestGetNodeNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.GetNode(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != flow.ErrNotFound.Error() {
		t.Errorf("error = %q, want %q", err.Error(), flow.ErrNotFound.Error())
	}
}

func TestListNodesByFlow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("list-nodes-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	for i := 0; i < 3; i++ {
		node := flow.FlowNode{
			ID:     ulid.Make().String(),
			FlowID: created.ID,
			Kind:   flow.NodeSkill,
			Label:  fmt.Sprintf("node-%d", i),
			Config: json.RawMessage(`{}`),
		}
		if _, err := store.AddNode(ctx, node); err != nil {
			t.Fatalf("add node %d: %v", i, err)
		}
	}

	nodes, err := store.ListNodesByFlow(ctx, created.ID)
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes) != 3 {
		t.Errorf("len(nodes) = %d, want 3", len(nodes))
	}
}

func TestListNodesEmpty(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("empty-nodes", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	nodes, err := store.ListNodesByFlow(ctx, created.ID)
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if nodes == nil {
		t.Fatal("expected non-nil slice")
	}
	if len(nodes) != 0 {
		t.Errorf("len(nodes) = %d, want 0", len(nodes))
	}
}

func TestRemoveNode(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("remove-node-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	node := flow.FlowNode{
		ID:     ulid.Make().String(),
		FlowID: created.ID,
		Kind:   flow.NodeSkill,
		Label:  "remove-me",
		Config: json.RawMessage(`{}`),
	}
	added, err := store.AddNode(ctx, node)
	if err != nil {
		t.Fatalf("add node: %v", err)
	}

	if err := store.RemoveNode(ctx, added.ID); err != nil {
		t.Fatalf("remove node: %v", err)
	}

	_, err = store.GetNode(ctx, added.ID)
	if err == nil {
		t.Fatal("expected not found after removal")
	}
}

func TestRemoveNodeNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	err := store.RemoveNode(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error removing nonexistent node")
	}
	if err.Error() != flow.ErrNotFound.Error() {
		t.Errorf("error = %q, want %q", err.Error(), flow.ErrNotFound.Error())
	}
}

func TestRemoveNodeCascadesEdges(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("cascade-node-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	// Create 3 nodes: A -> B -> C.
	nodeA := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeSkill, Label: "a", Config: json.RawMessage(`{}`),
	}
	nodeB := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeTransform, Label: "b", Config: json.RawMessage(`{}`),
	}
	nodeC := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeSkill, Label: "c", Config: json.RawMessage(`{}`),
	}

	nodeA, _ = store.AddNode(ctx, nodeA)
	nodeB, _ = store.AddNode(ctx, nodeB)
	nodeC, _ = store.AddNode(ctx, nodeC)

	// Add edges A→B and B→C.
	edgeAB := flow.FlowEdge{
		ID: ulid.Make().String(), FlowID: created.ID,
		FromNodeID: nodeA.ID, ToNodeID: nodeB.ID,
		Transform: flow.TransformPassthrough, Trigger: flow.TriggerOutputReady,
	}
	edgeBC := flow.FlowEdge{
		ID: ulid.Make().String(), FlowID: created.ID,
		FromNodeID: nodeB.ID, ToNodeID: nodeC.ID,
		Transform: flow.TransformPassthrough, Trigger: flow.TriggerOutputReady,
	}
	if _, err := store.AddEdge(ctx, edgeAB); err != nil {
		t.Fatalf("add edge AB: %v", err)
	}
	if _, err := store.AddEdge(ctx, edgeBC); err != nil {
		t.Fatalf("add edge BC: %v", err)
	}

	// Remove node B.
	if err := store.RemoveNode(ctx, nodeB.ID); err != nil {
		t.Fatalf("remove node B: %v", err)
	}

	// Both edges should be gone.
	edges, err := store.ListEdgesByFlow(ctx, created.ID)
	if err != nil {
		t.Fatalf("list edges: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("edges after cascade = %d, want 0", len(edges))
	}

	// Nodes A and C should still exist.
	if _, err := store.GetNode(ctx, nodeA.ID); err != nil {
		t.Errorf("node A should still exist: %v", err)
	}
	if _, err := store.GetNode(ctx, nodeC.ID); err != nil {
		t.Errorf("node C should still exist: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Edge CRUD
// ---------------------------------------------------------------------------

func TestAddEdge(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("edge-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	nodeA := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeSkill, Label: "a", Config: json.RawMessage(`{}`),
	}
	nodeB := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeTransform, Label: "b", Config: json.RawMessage(`{}`),
	}
	nodeA, _ = store.AddNode(ctx, nodeA)
	nodeB, _ = store.AddNode(ctx, nodeB)

	edge := flow.FlowEdge{
		ID:         ulid.Make().String(),
		FlowID:     created.ID,
		FromNodeID: nodeA.ID,
		ToNodeID:   nodeB.ID,
		Transform:  flow.TransformPassthrough,
		Trigger:    flow.TriggerOutputReady,
	}
	got, err := store.AddEdge(ctx, edge)
	if err != nil {
		t.Fatalf("add edge: %v", err)
	}
	if got.ID != edge.ID {
		t.Errorf("ID = %q, want %q", got.ID, edge.ID)
	}
	if got.FromNodeID != nodeA.ID {
		t.Errorf("FromNodeID = %q, want %q", got.FromNodeID, nodeA.ID)
	}
}

func TestAddEdgeWithAllFields(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("edge-full-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	nodeA := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeSkill, Label: "a", Config: json.RawMessage(`{}`),
	}
	nodeB := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeTransform, Label: "b", Config: json.RawMessage(`{}`),
	}
	nodeA, _ = store.AddNode(ctx, nodeA)
	nodeB, _ = store.AddNode(ctx, nodeB)

	edge := flow.FlowEdge{
		ID:              ulid.Make().String(),
		FlowID:          created.ID,
		FromNodeID:      nodeA.ID,
		ToNodeID:        nodeB.ID,
		Transform:       flow.TransformRegex,
		TransformConfig: `{"pattern":"\\d+","group":1}`,
		Trigger:         flow.TriggerOutputReady,
		TriggerConfig:   `{"timeout_ms":5000}`,
		Condition:       "status == ok",
		RetryPolicy:     &flow.RetryPolicy{MaxAttempts: 3, DelayMS: 1000},
	}
	got, err := store.AddEdge(ctx, edge)
	if err != nil {
		t.Fatalf("add edge: %v", err)
	}
	if got.Transform != flow.TransformRegex {
		t.Errorf("Transform = %q, want %q", got.Transform, flow.TransformRegex)
	}
	if got.TransformConfig != `{"pattern":"\\d+","group":1}` {
		t.Errorf("TransformConfig = %q, unexpected", got.TransformConfig)
	}
	if got.Condition != "status == ok" {
		t.Errorf("Condition = %q, want %q", got.Condition, "status == ok")
	}
	if got.RetryPolicy == nil {
		t.Fatal("RetryPolicy is nil, want non-nil")
	}
	if got.RetryPolicy.MaxAttempts != 3 {
		t.Errorf("RetryPolicy.MaxAttempts = %d, want 3", got.RetryPolicy.MaxAttempts)
	}
	if got.RetryPolicy.DelayMS != 1000 {
		t.Errorf("RetryPolicy.DelayMS = %d, want 1000", got.RetryPolicy.DelayMS)
	}
}

func TestAddEdgeWithTransformConfig(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("edge-tcfg-test", "/ws")
	created, _ := store.CreateFlow(ctx, f)

	nodeA := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeSkill, Label: "a", Config: json.RawMessage(`{}`),
	}
	nodeB := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeTransform, Label: "b", Config: json.RawMessage(`{}`),
	}
	nodeA, _ = store.AddNode(ctx, nodeA)
	nodeB, _ = store.AddNode(ctx, nodeB)

	edge := flow.FlowEdge{
		ID:              ulid.Make().String(),
		FlowID:          created.ID,
		FromNodeID:      nodeA.ID,
		ToNodeID:        nodeB.ID,
		Transform:       flow.TransformJQ,
		TransformConfig: `{"filter":".name"}`,
		Trigger:         flow.TriggerOutputReady,
	}
	got, _ := store.AddEdge(ctx, edge)
	if got.TransformConfig != `{"filter":".name"}` {
		t.Errorf("TransformConfig = %q, want %q", got.TransformConfig, `{"filter":".name"}`)
	}
}

func TestAddEdgeWithRetryPolicy(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("edge-retry-test", "/ws")
	created, _ := store.CreateFlow(ctx, f)

	nodeA := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeSkill, Label: "a", Config: json.RawMessage(`{}`),
	}
	nodeB := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeTransform, Label: "b", Config: json.RawMessage(`{}`),
	}
	nodeA, _ = store.AddNode(ctx, nodeA)
	nodeB, _ = store.AddNode(ctx, nodeB)

	edge := flow.FlowEdge{
		ID:          ulid.Make().String(),
		FlowID:      created.ID,
		FromNodeID:  nodeA.ID,
		ToNodeID:    nodeB.ID,
		Transform:   flow.TransformPassthrough,
		Trigger:     flow.TriggerOutputReady,
		RetryPolicy: &flow.RetryPolicy{MaxAttempts: 5, DelayMS: 2000},
	}
	got, _ := store.AddEdge(ctx, edge)
	if got.RetryPolicy == nil {
		t.Fatal("RetryPolicy is nil")
	}
	if got.RetryPolicy.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %d, want 5", got.RetryPolicy.MaxAttempts)
	}
	if got.RetryPolicy.DelayMS != 2000 {
		t.Errorf("DelayMS = %d, want 2000", got.RetryPolicy.DelayMS)
	}
}

func TestDuplicateEdgesAllowed(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("dup-edge-test", "/ws")
	created, _ := store.CreateFlow(ctx, f)

	nodeA := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeSkill, Label: "a", Config: json.RawMessage(`{}`),
	}
	nodeB := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeTransform, Label: "b", Config: json.RawMessage(`{}`),
	}
	nodeA, _ = store.AddNode(ctx, nodeA)
	nodeB, _ = store.AddNode(ctx, nodeB)

	edge1 := flow.FlowEdge{
		ID: ulid.Make().String(), FlowID: created.ID,
		FromNodeID: nodeA.ID, ToNodeID: nodeB.ID,
		Transform: flow.TransformPassthrough, Trigger: flow.TriggerOutputReady,
	}
	edge2 := flow.FlowEdge{
		ID: ulid.Make().String(), FlowID: created.ID,
		FromNodeID: nodeA.ID, ToNodeID: nodeB.ID,
		Transform: flow.TransformJQ, Trigger: flow.TriggerOutputReady,
	}

	_, err1 := store.AddEdge(ctx, edge1)
	_, err2 := store.AddEdge(ctx, edge2)
	if err1 != nil {
		t.Fatalf("add first edge: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("add duplicate edge: %v", err2)
	}

	edges, _ := store.ListEdgesByFlow(ctx, created.ID)
	if len(edges) != 2 {
		t.Errorf("edges = %d, want 2", len(edges))
	}
}

func TestGetEdge(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("get-edge-test", "/ws")
	created, _ := store.CreateFlow(ctx, f)

	nodeA := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeSkill, Label: "a", Config: json.RawMessage(`{}`),
	}
	nodeB := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeTransform, Label: "b", Config: json.RawMessage(`{}`),
	}
	nodeA, _ = store.AddNode(ctx, nodeA)
	nodeB, _ = store.AddNode(ctx, nodeB)

	edge := flow.FlowEdge{
		ID: ulid.Make().String(), FlowID: created.ID,
		FromNodeID: nodeA.ID, ToNodeID: nodeB.ID,
		Transform: flow.TransformPassthrough, Trigger: flow.TriggerOutputReady,
	}
	added, _ := store.AddEdge(ctx, edge)

	got, err := store.GetEdge(ctx, added.ID)
	if err != nil {
		t.Fatalf("get edge: %v", err)
	}
	if got.ID != added.ID {
		t.Errorf("ID = %q, want %q", got.ID, added.ID)
	}
}

func TestGetEdgeNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.GetEdge(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != flow.ErrNotFound.Error() {
		t.Errorf("error = %q, want %q", err.Error(), flow.ErrNotFound.Error())
	}
}

func TestRemoveEdge(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("rm-edge-test", "/ws")
	created, _ := store.CreateFlow(ctx, f)

	nodeA := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeSkill, Label: "a", Config: json.RawMessage(`{}`),
	}
	nodeB := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeTransform, Label: "b", Config: json.RawMessage(`{}`),
	}
	nodeA, _ = store.AddNode(ctx, nodeA)
	nodeB, _ = store.AddNode(ctx, nodeB)

	edge := flow.FlowEdge{
		ID: ulid.Make().String(), FlowID: created.ID,
		FromNodeID: nodeA.ID, ToNodeID: nodeB.ID,
		Transform: flow.TransformPassthrough, Trigger: flow.TriggerOutputReady,
	}
	added, _ := store.AddEdge(ctx, edge)

	if err := store.RemoveEdge(ctx, added.ID); err != nil {
		t.Fatalf("remove edge: %v", err)
	}

	_, err := store.GetEdge(ctx, added.ID)
	if err == nil {
		t.Fatal("expected not found after removal")
	}
}

func TestRemoveEdgeNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	err := store.RemoveEdge(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != flow.ErrNotFound.Error() {
		t.Errorf("error = %q, want %q", err.Error(), flow.ErrNotFound.Error())
	}
}

func TestListEdgesByFlow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("list-edges-test", "/ws")
	created, _ := store.CreateFlow(ctx, f)

	nodeA := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeSkill, Label: "a", Config: json.RawMessage(`{}`),
	}
	nodeB := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeTransform, Label: "b", Config: json.RawMessage(`{}`),
	}
	nodeC := flow.FlowNode{
		ID: ulid.Make().String(), FlowID: created.ID,
		Kind: flow.NodeSkill, Label: "c", Config: json.RawMessage(`{}`),
	}
	nodeA, _ = store.AddNode(ctx, nodeA)
	nodeB, _ = store.AddNode(ctx, nodeB)
	nodeC, _ = store.AddNode(ctx, nodeC)

	e1 := flow.FlowEdge{
		ID: ulid.Make().String(), FlowID: created.ID,
		FromNodeID: nodeA.ID, ToNodeID: nodeB.ID,
		Transform: flow.TransformPassthrough, Trigger: flow.TriggerOutputReady,
	}
	e2 := flow.FlowEdge{
		ID: ulid.Make().String(), FlowID: created.ID,
		FromNodeID: nodeB.ID, ToNodeID: nodeC.ID,
		Transform: flow.TransformPassthrough, Trigger: flow.TriggerOutputReady,
	}
	if _, err := store.AddEdge(ctx, e1); err != nil {
		t.Fatalf("add edge e1: %v", err)
	}
	if _, err := store.AddEdge(ctx, e2); err != nil {
		t.Fatalf("add edge e2: %v", err)
	}

	edges, err := store.ListEdgesByFlow(ctx, created.ID)
	if err != nil {
		t.Fatalf("list edges: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("len(edges) = %d, want 2", len(edges))
	}
}

func TestListEdgesEmpty(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("empty-edges", "/ws")
	created, _ := store.CreateFlow(ctx, f)

	edges, err := store.ListEdgesByFlow(ctx, created.ID)
	if err != nil {
		t.Fatalf("list edges: %v", err)
	}
	if edges == nil {
		t.Fatal("expected non-nil slice")
	}
	if len(edges) != 0 {
		t.Errorf("len(edges) = %d, want 0", len(edges))
	}
}

// ---------------------------------------------------------------------------
// Run CRUD
// ---------------------------------------------------------------------------

func TestCreateRun(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("run-test", "/ws")
	created, _ := store.CreateFlow(ctx, f)

	now := time.Now().UTC()
	run := flow.FlowRun{
		ID:        ulid.Make().String(),
		FlowID:    created.ID,
		State:     flow.RunRunning,
		StartedAt: now,
	}
	got, err := store.CreateRun(ctx, run)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if got.ID != run.ID {
		t.Errorf("ID = %q, want %q", got.ID, run.ID)
	}
	if got.State != flow.RunRunning {
		t.Errorf("State = %q, want %q", got.State, flow.RunRunning)
	}
}

func TestUpdateRun(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("run-update-test", "/ws")
	created, _ := store.CreateFlow(ctx, f)

	run := flow.FlowRun{
		ID:        ulid.Make().String(),
		FlowID:    created.ID,
		State:     flow.RunRunning,
		StartedAt: time.Now().UTC(),
	}
	createdRun, _ := store.CreateRun(ctx, run)

	completedAt := time.Now().UTC()
	createdRun.State = flow.RunCompleted
	createdRun.CompletedAt = &completedAt

	updated, err := store.UpdateRun(ctx, createdRun)
	if err != nil {
		t.Fatalf("update run: %v", err)
	}
	if updated.State != flow.RunCompleted {
		t.Errorf("State = %q, want %q", updated.State, flow.RunCompleted)
	}
	if updated.CompletedAt == nil {
		t.Fatal("CompletedAt is nil, want non-nil")
	}
}

func TestUpdateRunWithFailure(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("run-fail-test", "/ws")
	created, _ := store.CreateFlow(ctx, f)

	run := flow.FlowRun{
		ID:        ulid.Make().String(),
		FlowID:    created.ID,
		State:     flow.RunRunning,
		StartedAt: time.Now().UTC(),
	}
	createdRun, _ := store.CreateRun(ctx, run)

	completedAt := time.Now().UTC()
	createdRun.State = flow.RunFailed
	createdRun.CompletedAt = &completedAt
	createdRun.Error = "something went wrong"

	updated, err := store.UpdateRun(ctx, createdRun)
	if err != nil {
		t.Fatalf("update run: %v", err)
	}
	if updated.State != flow.RunFailed {
		t.Errorf("State = %q, want %q", updated.State, flow.RunFailed)
	}
	if updated.Error != "something went wrong" {
		t.Errorf("Error = %q, want %q", updated.Error, "something went wrong")
	}
}

// ---------------------------------------------------------------------------
// Timestamps
// ---------------------------------------------------------------------------

func TestTimestampsRFC3339(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	before := time.Now().UTC()
	f := newFlow("ts-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	after := time.Now().UTC()

	// Timestamps should be valid and within range.
	if created.CreatedAt.Before(before) {
		t.Errorf("CreatedAt %v is before start %v", created.CreatedAt, before)
	}
	if created.CreatedAt.After(after) {
		t.Errorf("CreatedAt %v is after end %v", created.CreatedAt, after)
	}
	if created.UpdatedAt.Before(before) {
		t.Errorf("UpdatedAt %v is before start %v", created.UpdatedAt, before)
	}
	if created.UpdatedAt.After(after) {
		t.Errorf("UpdatedAt %v is after end %v", created.UpdatedAt, after)
	}
}

func TestUpdatedAtChangesOnMutation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("ts-mut-test", "/ws")
	created, _ := store.CreateFlow(ctx, f)
	originalUpdatedAt := created.UpdatedAt

	time.Sleep(2 * time.Millisecond) // ensure time passes

	created.Description = "updated"
	updated, err := store.UpdateFlow(ctx, created)
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	if !updated.UpdatedAt.After(originalUpdatedAt) {
		t.Errorf("UpdatedAt %v should be after original %v", updated.UpdatedAt, originalUpdatedAt)
	}
}

// ---------------------------------------------------------------------------
// Run Log Tests
// ---------------------------------------------------------------------------

// helper: create a flow + run for log tests.
func setupFlowRun(t *testing.T, store flow.Store, ctx context.Context) (flow.Flow, flow.FlowRun) {
	t.Helper()
	f := newFlow("log-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	run := flow.FlowRun{
		ID:        ulid.Make().String(),
		FlowID:    created.ID,
		State:     flow.RunRunning,
		StartedAt: time.Now().UTC(),
	}
	createdRun, err := store.CreateRun(ctx, run)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	return created, createdRun
}

// helper: write a run log, failing the test on error.
func mustWriteLog(t *testing.T, store flow.Store, ctx context.Context, runID, nodeID string, envelope json.RawMessage) {
	t.Helper()
	if _, err := store.WriteRunLog(ctx, flow.RunLog{RunID: runID, NodeID: nodeID, Envelope: envelope}); err != nil {
		t.Fatalf("WriteRunLog %s: %v", nodeID, err)
	}
}

func makeEnvelopeJSON(t *testing.T, status, command string, data any) json.RawMessage {
	t.Helper()
	env := map[string]any{
		"version": 1,
		"status":  status,
		"command": command,
		"data":    data,
		"meta":    map[string]string{"ts": time.Now().UTC().Format(time.RFC3339)},
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return json.RawMessage(b)
}

func TestWriteRunLog(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	envJSON := makeEnvelopeJSON(t, "ok", "skill/run", map[string]any{"result": "hello"})
	log := flow.RunLog{
		RunID:   run.ID,
		NodeID:  "node-a",
		Envelope: envJSON,
	}

	got, err := store.WriteRunLog(ctx, log)
	if err != nil {
		t.Fatalf("WriteRunLog: %v", err)
	}

	if got.ID == "" {
		t.Error("expected auto-generated ID")
	}
	if got.Seq != 1 {
		t.Errorf("Seq = %d, want 1", got.Seq)
	}
	if got.RunID != run.ID {
		t.Errorf("RunID = %q, want %q", got.RunID, run.ID)
	}
	if got.NodeID != "node-a" {
		t.Errorf("NodeID = %q, want %q", got.NodeID, "node-a")
	}
	if got.CreatedAt.IsZero() {
		t.Error("expected auto-generated CreatedAt")
	}
}

func TestWriteRunLogSeqMonotonicallyIncreasing(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	for i := 0; i < 5; i++ {
		envJSON := makeEnvelopeJSON(t, "ok", "test", map[string]any{"i": i})
		got, err := store.WriteRunLog(ctx, flow.RunLog{
			RunID:    run.ID,
			NodeID:   fmt.Sprintf("node-%d", i),
			Envelope: envJSON,
		})
		if err != nil {
			t.Fatalf("WriteRunLog %d: %v", i, err)
		}
		if got.Seq != i+1 {
			t.Errorf("seq %d: got.Seq = %d, want %d", i, got.Seq, i+1)
		}
	}
}

func TestWriteRunLogSeqResetsPerRun(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run1 := setupFlowRun(t, store, ctx)

	envJSON := makeEnvelopeJSON(t, "ok", "test", nil)
	got, _ := store.WriteRunLog(ctx, flow.RunLog{
		RunID:    run1.ID,
		NodeID:   "a",
		Envelope: envJSON,
	})
	if got.Seq != 1 {
		t.Fatalf("first run seq = %d, want 1", got.Seq)
	}

	// Create a second run in the same flow.
	flow2, _ := store.GetFlow(ctx, run1.FlowID)
	run2 := flow.FlowRun{
		ID:        ulid.Make().String(),
		FlowID:    flow2.ID,
		State:     flow.RunRunning,
		StartedAt: time.Now().UTC(),
	}
	createdRun2, _ := store.CreateRun(ctx, run2)

	got2, err := store.WriteRunLog(ctx, flow.RunLog{
		RunID:    createdRun2.ID,
		NodeID:   "a",
		Envelope: envJSON,
	})
	if err != nil {
		t.Fatalf("WriteRunLog run2: %v", err)
	}
	if got2.Seq != 1 {
		t.Errorf("second run seq = %d, want 1 (resets per run)", got2.Seq)
	}
}

func TestListRunLogs(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	// Write 3 logs.
	for i := 0; i < 3; i++ {
		envJSON := makeEnvelopeJSON(t, "ok", "test", map[string]any{"i": i})
		_, err := store.WriteRunLog(ctx, flow.RunLog{
			RunID:    run.ID,
			NodeID:   fmt.Sprintf("node-%d", i),
			Envelope: envJSON,
		})
		if err != nil {
			t.Fatalf("WriteRunLog %d: %v", i, err)
		}
	}

	logs, err := store.ListRunLogs(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListRunLogs: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("len(logs) = %d, want 3", len(logs))
	}

	// Verify ordering by seq ascending.
	for i, l := range logs {
		if l.Seq != i+1 {
			t.Errorf("logs[%d].Seq = %d, want %d", i, l.Seq, i+1)
		}
	}

	// Verify all fields populated.
	for _, l := range logs {
		if l.ID == "" {
			t.Error("ID is empty")
		}
		if l.RunID != run.ID {
			t.Errorf("RunID = %q, want %q", l.RunID, run.ID)
		}
		if l.NodeID == "" {
			t.Error("NodeID is empty")
		}
		if l.Envelope == nil || string(l.Envelope) == "" {
			t.Error("Envelope is empty")
		}
		if l.CreatedAt.IsZero() {
			t.Error("CreatedAt is zero")
		}
	}
}

func TestListRunLogsWithNodeIDFilter(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	envJSON := makeEnvelopeJSON(t, "ok", "test", nil)

	// Write logs for multiple nodes.
	mustWriteLog(t, store, ctx, run.ID, "node-a", envJSON)
	mustWriteLog(t, store, ctx, run.ID, "node-b", envJSON)
	mustWriteLog(t, store, ctx, run.ID, "node-a", envJSON)
	mustWriteLog(t, store, ctx, run.ID, "node-c", envJSON)

	logs, err := store.ListRunLogs(ctx, run.ID, flow.WithNodeID("node-a"))
	if err != nil {
		t.Fatalf("ListRunLogs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("len(logs) = %d, want 2 (only node-a)", len(logs))
	}
	for _, l := range logs {
		if l.NodeID != "node-a" {
			t.Errorf("NodeID = %q, want %q", l.NodeID, "node-a")
		}
	}
}

func TestListRunLogsWithLimitOffset(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	envJSON := makeEnvelopeJSON(t, "ok", "test", nil)

	// Write 20 logs.
	for i := 0; i < 20; i++ {
		_, err := store.WriteRunLog(ctx, flow.RunLog{
			RunID:    run.ID,
			NodeID:   "node",
			Envelope: envJSON,
		})
		if err != nil {
			t.Fatalf("WriteRunLog %d: %v", i, err)
		}
	}

	// Pagination: limit=5, offset=10 → seq 11..15.
	logs, err := store.ListRunLogs(ctx, run.ID, flow.WithLimit(5), flow.WithOffset(10))
	if err != nil {
		t.Fatalf("ListRunLogs: %v", err)
	}
	if len(logs) != 5 {
		t.Fatalf("len(logs) = %d, want 5", len(logs))
	}
	for i, l := range logs {
		wantSeq := 11 + i
		if l.Seq != wantSeq {
			t.Errorf("logs[%d].Seq = %d, want %d", i, l.Seq, wantSeq)
		}
	}
}

func TestListRunLogsEmptyResult(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	// No logs written.
	logs, err := store.ListRunLogs(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListRunLogs: %v", err)
	}
	if logs == nil {
		t.Fatal("expected non-nil slice for empty result")
	}
	if len(logs) != 0 {
		t.Errorf("len(logs) = %d, want 0", len(logs))
	}
}

func TestListRunLogsNodeFilterEmptyResult(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	envJSON := makeEnvelopeJSON(t, "ok", "test", nil)
	if _, err := store.WriteRunLog(ctx, flow.RunLog{RunID: run.ID, NodeID: "node-a", Envelope: envJSON}); err != nil {
		t.Fatalf("WriteRunLog: %v", err)
	}

	// Filter for a different node.
	logs, err := store.ListRunLogs(ctx, run.ID, flow.WithNodeID("nonexistent"))
	if err != nil {
		t.Fatalf("ListRunLogs: %v", err)
	}
	if logs == nil {
		t.Fatal("expected non-nil slice")
	}
	if len(logs) != 0 {
		t.Errorf("len(logs) = %d, want 0", len(logs))
	}
}

func TestWriteRunLogErrorEnvelope(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	envJSON := makeEnvelopeJSON(t, "error", "skill/run", nil)
	// Add error fields.
	var env map[string]any
	if err := json.Unmarshal(envJSON, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	env["error"] = map[string]string{"code": "ERUNTIME", "message": "something failed"}
	envJSON, _ = json.Marshal(env)

	got, err := store.WriteRunLog(ctx, flow.RunLog{
		RunID:    run.ID,
		NodeID:   "node-err",
		Envelope: envJSON,
	})
	if err != nil {
		t.Fatalf("WriteRunLog: %v", err)
	}
	if got.Seq != 1 {
		t.Errorf("Seq = %d, want 1", got.Seq)
	}

	// Read back and verify the envelope.
	logs, _ := store.ListRunLogs(ctx, run.ID)
	if len(logs) != 1 {
		t.Fatalf("len(logs) = %d, want 1", len(logs))
	}
	var parsed map[string]any
	if err := json.Unmarshal(logs[0].Envelope, &parsed); err != nil {
		t.Fatalf("parse envelope: %v", err)
	}
	if parsed["status"] != "error" {
		t.Errorf("status = %v, want error", parsed["status"])
	}
}

func TestCascadeDeleteRemovesLogs(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	envJSON := makeEnvelopeJSON(t, "ok", "test", nil)
	if _, err := store.WriteRunLog(ctx, flow.RunLog{RunID: run.ID, NodeID: "a", Envelope: envJSON}); err != nil {
		t.Fatalf("WriteRunLog a: %v", err)
	}
	if _, err := store.WriteRunLog(ctx, flow.RunLog{RunID: run.ID, NodeID: "b", Envelope: envJSON}); err != nil {
		t.Fatalf("WriteRunLog b: %v", err)
	}

	// Verify logs exist.
	logs, _ := store.ListRunLogs(ctx, run.ID)
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs before delete, got %d", len(logs))
	}

	// Delete the flow (should cascade to run → logs).
	if err := store.DeleteFlow(ctx, run.FlowID); err != nil {
		t.Fatalf("DeleteFlow: %v", err)
	}

	// Verify logs are gone.
	logs, err := store.ListRunLogs(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListRunLogs after cascade: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("expected 0 logs after cascade delete, got %d", len(logs))
	}
}

func TestLargeEnvelopePersistedCorrectly(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	// Create a ~100KB payload.
	largeData := make(map[string]string)
	for i := 0; i < 1000; i++ {
		largeData[fmt.Sprintf("key_%04d", i)] = fmt.Sprintf("value_%050d", i)
	}
	envJSON := makeEnvelopeJSON(t, "ok", "test", largeData)
	originalSize := len(envJSON)
	if originalSize < 50*1024 {
		t.Logf("warning: large payload is only %d bytes, test may not be thorough", originalSize)
	}

	_, err := store.WriteRunLog(ctx, flow.RunLog{
		RunID:    run.ID,
		NodeID:   "big-node",
		Envelope: envJSON,
	})
	if err != nil {
		t.Fatalf("WriteRunLog: %v", err)
	}

	logs, _ := store.ListRunLogs(ctx, run.ID)
	if len(logs) != 1 {
		t.Fatalf("len(logs) = %d, want 1", len(logs))
	}

	// Byte-for-byte comparison.
	if string(logs[0].Envelope) != string(envJSON) {
		gotSize := len(logs[0].Envelope)
		t.Errorf("envelope truncated or modified: original=%d bytes, stored=%d bytes", originalSize, gotSize)
	}
}

func TestConcurrentWriteRunLogSeqSafety(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	envJSON := makeEnvelopeJSON(t, "ok", "test", nil)

	// Write 20 logs concurrently from multiple goroutines.
	var wg sync.WaitGroup
	count := 20
	results := make(chan flow.RunLog, count)

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			got, err := store.WriteRunLog(ctx, flow.RunLog{
				RunID:    run.ID,
				NodeID:   fmt.Sprintf("node-%d", idx%3),
				Envelope: envJSON,
			})
			if err != nil {
				t.Errorf("concurrent WriteRunLog %d: %v", idx, err)
				return
			}
			results <- got
		}(i)
	}
	wg.Wait()
	close(results)

	// Verify all writes succeeded.
	var allLogs []flow.RunLog
	for l := range results {
		allLogs = append(allLogs, l)
	}
	if len(allLogs) != count {
		t.Errorf("got %d logs, want %d", len(allLogs), count)
	}

	// Verify seq values are unique and contiguous 1..count.
	seqMap := make(map[int]bool)
	for _, l := range allLogs {
		if l.Seq < 1 || l.Seq > count {
			t.Errorf("Seq = %d out of range [1, %d]", l.Seq, count)
		}
		if seqMap[l.Seq] {
			t.Errorf("duplicate Seq = %d", l.Seq)
		}
		seqMap[l.Seq] = true
	}
	for i := 1; i <= count; i++ {
		if !seqMap[i] {
			t.Errorf("missing Seq = %d", i)
		}
	}

	// Verify total count in DB.
	logs, _ := store.ListRunLogs(ctx, run.ID)
	if len(logs) != count {
		t.Errorf("ListRunLogs count = %d, want %d", len(logs), count)
	}
}

func TestStreamRunLogsHistoricalReplay(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	envJSON := makeEnvelopeJSON(t, "ok", "test", nil)

	// Write 3 logs.
	for _, node := range []string{"a", "b", "c"} {
		if _, err := store.WriteRunLog(ctx, flow.RunLog{RunID: run.ID, NodeID: node, Envelope: envJSON}); err != nil {
			t.Fatalf("WriteRunLog %s: %v", node, err)
		}
	}

	// Complete the run.
	run.State = flow.RunCompleted
	completedAt := time.Now().UTC()
	run.CompletedAt = &completedAt
	if _, err := store.UpdateRun(ctx, run); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}

	// Stream: should replay all logs then close.
	streamCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	ch, err := store.StreamRunLogs(streamCtx, run.ID)
	if err != nil {
		t.Fatalf("StreamRunLogs: %v", err)
	}

	var received []flow.RunLog
	for l := range ch {
		received = append(received, l)
	}

	if len(received) != 3 {
		t.Errorf("received %d logs, want 3", len(received))
	}

	// Verify order: seq ascending.
	for i, l := range received {
		if l.Seq != i+1 {
			t.Errorf("received[%d].Seq = %d, want %d", i, l.Seq, i+1)
		}
	}
}

func TestStreamRunLogsCompletedRunClosesAfterReplay(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	envJSON := makeEnvelopeJSON(t, "ok", "test", nil)
	mustWriteLog(t, store, ctx, run.ID, "a", envJSON)

	// Complete the run.
	run.State = flow.RunCompleted
	completedAt := time.Now().UTC()
	run.CompletedAt = &completedAt
	if _, err := store.UpdateRun(ctx, run); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}

	streamCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	ch, err := store.StreamRunLogs(streamCtx, run.ID)
	if err != nil {
		t.Fatalf("StreamRunLogs: %v", err)
	}

	// Drain and verify channel closes.
	count := 0
	for range ch {
		count++
	}
	if count != 1 {
		t.Errorf("expected 1 replayed log, got %d", count)
	}
}

func TestStreamRunLogsContextCancellation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	envJSON := makeEnvelopeJSON(t, "ok", "test", nil)
	mustWriteLog(t, store, ctx, run.ID, "a", envJSON)

	streamCtx, cancel := context.WithCancel(ctx)

	ch, err := store.StreamRunLogs(streamCtx, run.ID)
	if err != nil {
		t.Fatalf("StreamRunLogs: %v", err)
	}

	// Read the first historical entry.
	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatal("channel closed unexpectedly")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for historical log")
	}

	// Cancel context.
	cancel()

	// Channel should close promptly.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed after cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel did not close after context cancellation")
	}
}

func TestStreamRunLogsNodeFilter(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	envJSON := makeEnvelopeJSON(t, "ok", "test", nil)

	mustWriteLog(t, store, ctx, run.ID, "a", envJSON)
	mustWriteLog(t, store, ctx, run.ID, "b", envJSON)
	mustWriteLog(t, store, ctx, run.ID, "a", envJSON)

	// Complete the run.
	run.State = flow.RunCompleted
	completedAt := time.Now().UTC()
	run.CompletedAt = &completedAt
	if _, err := store.UpdateRun(ctx, run); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}

	streamCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	ch, err := store.StreamRunLogs(streamCtx, run.ID, flow.WithNodeID("a"))
	if err != nil {
		t.Fatalf("StreamRunLogs: %v", err)
	}

	var received []flow.RunLog
	for l := range ch {
		received = append(received, l)
	}

	if len(received) != 2 {
		t.Errorf("received %d logs, want 2 (only node-a)", len(received))
	}
	for _, l := range received {
		if l.NodeID != "a" {
			t.Errorf("NodeID = %q, want %q", l.NodeID, "a")
		}
	}
}

func TestStreamRunLogsNonexistentRun(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.StreamRunLogs(ctx, "nonexistent-run")
	if err == nil {
		t.Fatal("expected error for nonexistent run")
	}
}

func TestStreamRunLogsEmptyCompletedRun(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	// Complete the run without writing any logs.
	run.State = flow.RunCompleted
	completedAt := time.Now().UTC()
	run.CompletedAt = &completedAt
	if _, err := store.UpdateRun(ctx, run); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}

	streamCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	ch, err := store.StreamRunLogs(streamCtx, run.ID)
	if err != nil {
		t.Fatalf("StreamRunLogs: %v", err)
	}

	count := 0
	for range ch {
		count++
	}
	if count != 0 {
		t.Errorf("expected 0 logs for empty completed run, got %d", count)
	}
}

func TestRunLogSchemaAndIndexes(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_ = ctx // just need the store to trigger migration

	// Access the underlying DB to check schema.
	db := store.(*sqlStore).db

	// Check table exists.
	var name string
	err := db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name='flow_run_logs'`).Scan(&name)
	if err != nil {
		t.Fatalf("flow_run_logs table not found: %v", err)
	}

	// Check columns via PRAGMA.
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(flow_run_logs)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()

	columns := map[string]string{}
	for rows.Next() {
		var cid int
		var colName, colType string
		var notNull int
		var dfltValue any
		var pk int
		if err := rows.Scan(&cid, &colName, &colType, &notNull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		columns[colName] = colType
	}

	expectedCols := []string{"id", "run_id", "node_id", "seq", "envelope", "created_at"}
	for _, col := range expectedCols {
		if _, ok := columns[col]; !ok {
			t.Errorf("missing column %q in flow_run_logs", col)
		}
	}

	// Check indexes.
	idxRows, err := db.QueryContext(ctx, `PRAGMA index_list(flow_run_logs)`)
	if err != nil {
		t.Fatalf("PRAGMA index_list: %v", err)
	}
	defer idxRows.Close()

	idxNames := map[string]bool{}
	for idxRows.Next() {
		var seq int
		var idxName string
		var unique int
		var origin string
		var partial int
		if err := idxRows.Scan(&seq, &idxName, &unique, &origin, &partial); err != nil {
			t.Fatalf("scan index: %v", err)
		}
		idxNames[idxName] = true
	}

	expectedIdx := []string{"idx_flow_run_logs_run_seq", "idx_flow_run_logs_run_node"}
	for _, idx := range expectedIdx {
		if !idxNames[idx] {
			t.Errorf("missing index %q", idx)
		}
	}
}

func TestRunLogLinearChainAllNodesLogged(t *testing.T) {
	// Simulate a linear chain A→B→C: each node produces one log.
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	envA := makeEnvelopeJSON(t, "ok", "skill/run", map[string]any{"from": "A"})
	envB := makeEnvelopeJSON(t, "ok", "skill/run", map[string]any{"from": "B"})
	envC := makeEnvelopeJSON(t, "ok", "skill/run", map[string]any{"from": "C"})

	mustWriteLog(t, store, ctx, run.ID, "node-a", envA)
	mustWriteLog(t, store, ctx, run.ID, "node-b", envB)
	mustWriteLog(t, store, ctx, run.ID, "node-c", envC)

	logs, _ := store.ListRunLogs(ctx, run.ID)
	if len(logs) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(logs))
	}

	nodeIDs := make(map[string]bool)
	for _, l := range logs {
		nodeIDs[l.NodeID] = true
	}
	for _, expected := range []string{"node-a", "node-b", "node-c"} {
		if !nodeIDs[expected] {
			t.Errorf("missing log for node %q", expected)
		}
	}
}

func TestRunLogEnvelopePreservesMetaSource(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	envJSON := makeEnvelopeJSON(t, "ok", "test", nil)
	// Add meta.source.
	var env map[string]any
	if err := json.Unmarshal(envJSON, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	meta := env["meta"].(map[string]any)
	meta["source"] = "flow/engine"
	envJSON, _ = json.Marshal(env)

	mustWriteLog(t, store, ctx, run.ID, "a", envJSON)

	logs, _ := store.ListRunLogs(ctx, run.ID)
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	var parsed map[string]any
	if err := json.Unmarshal(logs[0].Envelope, &parsed); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	parsedMeta, ok := parsed["meta"].(map[string]any)
	if !ok {
		t.Fatal("meta field not found or wrong type")
	}
	if parsedMeta["source"] != "flow/engine" {
		t.Errorf("meta.source = %v, want flow/engine", parsedMeta["source"])
	}
}

func TestRunLogCreatedAtBetweenRunBounds(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	envJSON := makeEnvelopeJSON(t, "ok", "test", nil)
	time.Sleep(1 * time.Millisecond)
	got, err := store.WriteRunLog(ctx, flow.RunLog{RunID: run.ID, NodeID: "a", Envelope: envJSON})
	if err != nil {
		t.Fatalf("WriteRunLog: %v", err)
	}
	time.Sleep(1 * time.Millisecond)

	if got.CreatedAt.Before(run.StartedAt) {
		t.Errorf("log CreatedAt %v before run StartedAt %v", got.CreatedAt, run.StartedAt)
	}
}

func TestStreamRunLogsLiveDelivery(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	envJSON := makeEnvelopeJSON(t, "ok", "test", nil)

	// Write one historical log.
	mustWriteLog(t, store, ctx, run.ID, "a", envJSON)

	streamCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	ch, err := store.StreamRunLogs(streamCtx, run.ID)
	if err != nil {
		t.Fatalf("StreamRunLogs: %v", err)
	}

	// Read the historical log.
	select {
	case l, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before receiving historical log")
		}
		if l.Seq != 1 {
			t.Errorf("historical log Seq = %d, want 1", l.Seq)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for historical log")
	}

	// Write a new log (live delivery via broadcast).
	go func() {
		time.Sleep(100 * time.Millisecond)
		mustWriteLog(t, store, ctx, run.ID, "b", envJSON)
	}()

	// Should receive the live log.
	select {
	case l, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before receiving live log")
		}
		if l.Seq != 2 {
			t.Errorf("live log Seq = %d, want 2", l.Seq)
		}
		if l.NodeID != "b" {
			t.Errorf("live log NodeID = %q, want %q", l.NodeID, "b")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for live log")
	}

	cancel()
}

func TestLogPersistenceSurvivesEngineRestart(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Open store, write logs.
	store1, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("open store1: %v", err)
	}

	f := newFlow("persist-test", "/ws")
	created, _ := store1.CreateFlow(ctx, f)
	run := flow.FlowRun{ID: ulid.Make().String(), FlowID: created.ID, State: flow.RunRunning, StartedAt: time.Now().UTC()}
	if _, err := store1.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	envJSON := makeEnvelopeJSON(t, "ok", "test", map[string]any{"data": "important"})
	if _, err := store1.WriteRunLog(ctx, flow.RunLog{RunID: run.ID, NodeID: "a", Envelope: envJSON}); err != nil {
		t.Fatalf("WriteRunLog a: %v", err)
	}
	if _, err := store1.WriteRunLog(ctx, flow.RunLog{RunID: run.ID, NodeID: "b", Envelope: envJSON}); err != nil {
		t.Fatalf("WriteRunLog b: %v", err)
	}
	store1.Close()

	// Reopen store.
	store2, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("open store2: %v", err)
	}
	defer store2.Close()

	logs, err := store2.ListRunLogs(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListRunLogs after restart: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("expected 2 logs after restart, got %d", len(logs))
	}
}
