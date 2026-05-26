package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/quick"
	"time"

	envelopepkg "github.com/joshka0/foxctl/internal/domain/envelope"
	flow "github.com/joshka0/foxctl/internal/runtime/flow"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
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

func TestCreateFlowRejectsInvalidState(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("invalid-state-create", "/ws")
	f.State = flow.FlowState("not-a-flow-state")

	if _, err := store.CreateFlow(ctx, f); err == nil {
		t.Fatal("CreateFlow accepted invalid flow state")
	}

	flows, err := store.ListFlows(ctx, "/ws")
	if err != nil {
		t.Fatalf("list flows after rejected create: %v", err)
	}
	if len(flows) != 0 {
		t.Fatalf("rejected invalid flow state persisted %d flows", len(flows))
	}
}

func TestFlowReadsRejectCorruptPersistedState(t *testing.T) {
	store := newTestStore(t)
	sqlStore := store.(*sqlStore)
	ctx := context.Background()

	f := newFlow("corrupt-flow-state", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	if _, err := sqlStore.db.ExecContext(ctx, `
		UPDATE flows SET state = $1 WHERE id = $2
	`, "not-a-flow-state", created.ID); err != nil {
		t.Fatalf("corrupt state: %v", err)
	}

	if _, err := store.GetFlow(ctx, created.ID); !flowReadErrorNamesColumn(err, "state") {
		t.Fatalf("GetFlow() error=%v, want it to name corrupt state", err)
	}
	if _, err := store.GetFlowByName(ctx, created.Workspace, created.Name); !flowReadErrorNamesColumn(err, "state") {
		t.Fatalf("GetFlowByName() error=%v, want it to name corrupt state", err)
	}
	if _, err := store.ListFlows(ctx, created.Workspace); !flowReadErrorNamesColumn(err, "state") {
		t.Fatalf("ListFlows() error=%v, want it to name corrupt state", err)
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

func TestFlowReadsRejectCorruptPersistedTimestamps(t *testing.T) {
	ctx := context.Background()

	for _, column := range []string{"created_at", "updated_at"} {
		t.Run(column, func(t *testing.T) {
			store := newTestStore(t)
			sqlStore := store.(*sqlStore)

			f := newFlow("corrupt-flow-"+column, "/ws")
			created, err := store.CreateFlow(ctx, f)
			if err != nil {
				t.Fatalf("create flow: %v", err)
			}

			if _, err := sqlStore.db.ExecContext(ctx, fmt.Sprintf(`
				UPDATE flows SET %s = $1 WHERE id = $2
			`, column), "not-a-timestamp", created.ID); err != nil {
				t.Fatalf("corrupt %s: %v", column, err)
			}

			if _, err := store.GetFlow(ctx, created.ID); !flowReadErrorNamesColumn(err, column) {
				t.Fatalf("GetFlow() error=%v, want it to name corrupt column %s", err, column)
			}
			if _, err := store.GetFlowByName(ctx, created.Workspace, created.Name); !flowReadErrorNamesColumn(err, column) {
				t.Fatalf("GetFlowByName() error=%v, want it to name corrupt column %s", err, column)
			}
			if _, err := store.ListFlows(ctx, created.Workspace); !flowReadErrorNamesColumn(err, column) {
				t.Fatalf("ListFlows() error=%v, want it to name corrupt column %s", err, column)
			}
		})
	}
}

func flowReadErrorNamesColumn(err error, column string) bool {
	return err != nil && strings.Contains(err.Error(), column)
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

func TestUpdateFlowRejectsInvalidStateAndPreservesExistingFlow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("invalid-state-update", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	invalid := created
	invalid.State = flow.FlowState("not-a-flow-state")
	invalid.Description = "should not persist"
	if _, err := store.UpdateFlow(ctx, invalid); err == nil {
		t.Fatal("UpdateFlow accepted invalid flow state")
	}

	persisted, err := store.GetFlow(ctx, created.ID)
	if err != nil {
		t.Fatalf("get flow after rejected update: %v", err)
	}
	if persisted.State != flow.FlowDraft {
		t.Fatalf("persisted state=%q want %q", persisted.State, flow.FlowDraft)
	}
	if persisted.Description != "" {
		t.Fatalf("persisted description=%q want empty", persisted.Description)
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

func TestAddNodeRejectsInvalidKind(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("invalid-node-kind-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	node := flow.FlowNode{
		ID:     ulid.Make().String(),
		FlowID: created.ID,
		Kind:   flow.NodeKind("not-a-node-kind"),
		Label:  "invalid",
		Config: json.RawMessage(`{}`),
	}
	if _, err := store.AddNode(ctx, node); err == nil {
		t.Fatal("AddNode accepted invalid node kind")
	}

	nodes, err := store.ListNodesByFlow(ctx, created.ID)
	if err != nil {
		t.Fatalf("list nodes after rejected add: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("rejected invalid node kind persisted %d nodes", len(nodes))
	}
}

func TestNodeReadsRejectCorruptPersistedKind(t *testing.T) {
	store := newTestStore(t)
	sqlStore := store.(*sqlStore)
	ctx := context.Background()

	f := newFlow("node-corrupt-kind-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	node, err := store.AddNode(ctx, flow.FlowNode{
		ID:     ulid.Make().String(),
		FlowID: created.ID,
		Kind:   flow.NodeSkill,
		Label:  "valid-kind",
		Config: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("add node: %v", err)
	}

	if _, err := sqlStore.db.ExecContext(ctx, `
		UPDATE flow_nodes SET kind = $1 WHERE id = $2
	`, "not-a-node-kind", node.ID); err != nil {
		t.Fatalf("corrupt kind: %v", err)
	}

	if _, err := store.GetNode(ctx, node.ID); !flowReadErrorNamesColumn(err, "kind") {
		t.Fatalf("GetNode() error=%v, want it to name corrupt kind", err)
	}
	if _, err := store.ListNodesByFlow(ctx, created.ID); !flowReadErrorNamesColumn(err, "kind") {
		t.Fatalf("ListNodesByFlow() error=%v, want it to name corrupt kind", err)
	}
}

func TestAddNodeRejectsInvalidConfigJSON(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("invalid-node-config-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	node := flow.FlowNode{
		ID:     ulid.Make().String(),
		FlowID: created.ID,
		Kind:   flow.NodeSkill,
		Label:  "invalid-config",
		Config: json.RawMessage(`{"skill":`),
	}
	if _, err := store.AddNode(ctx, node); err == nil {
		t.Fatal("AddNode accepted invalid JSON config")
	}

	nodes, err := store.ListNodesByFlow(ctx, created.ID)
	if err != nil {
		t.Fatalf("list nodes after rejected add: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("rejected invalid config persisted %d nodes", len(nodes))
	}
}

func TestAddNodeRejectsNonObjectConfig(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("non-object-node-config-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	node := flow.FlowNode{
		ID:     ulid.Make().String(),
		FlowID: created.ID,
		Kind:   flow.NodeSkill,
		Label:  "array-config",
		Config: json.RawMessage(`[]`),
	}
	if _, err := store.AddNode(ctx, node); err == nil {
		t.Fatal("AddNode accepted non-object JSON config")
	}
}

func TestAddNodeDefaultsEmptyConfigToObject(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("empty-node-config-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	node := flow.FlowNode{
		ID:     ulid.Make().String(),
		FlowID: created.ID,
		Kind:   flow.NodeTransform,
		Label:  "empty-config",
	}
	got, err := store.AddNode(ctx, node)
	if err != nil {
		t.Fatalf("add node with empty config: %v", err)
	}
	if string(got.Config) != "{}" {
		t.Fatalf("empty node config stored as %q want {}", got.Config)
	}
}

func TestNodeReadsRejectCorruptPersistedConfig(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		config string
	}{
		{name: "malformed json", config: `{"skill":`},
		{name: "array shape", config: `[]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t)
			sqlStore := store.(*sqlStore)

			f := newFlow("node-corrupt-config-"+tt.name, "/ws")
			created, err := store.CreateFlow(ctx, f)
			if err != nil {
				t.Fatalf("create flow: %v", err)
			}

			node, err := store.AddNode(ctx, flow.FlowNode{
				ID:     ulid.Make().String(),
				FlowID: created.ID,
				Kind:   flow.NodeSkill,
				Label:  "with-config",
				Config: json.RawMessage(`{"skill":"test"}`),
			})
			if err != nil {
				t.Fatalf("add node: %v", err)
			}

			if _, err := sqlStore.db.ExecContext(ctx, `
				UPDATE flow_nodes SET config = $1 WHERE id = $2
			`, tt.config, node.ID); err != nil {
				t.Fatalf("corrupt config: %v", err)
			}

			if _, err := store.GetNode(ctx, node.ID); !flowReadErrorNamesColumn(err, "config") {
				t.Fatalf("GetNode() error=%v, want it to name corrupt config", err)
			}
			if _, err := store.ListNodesByFlow(ctx, created.ID); !flowReadErrorNamesColumn(err, "config") {
				t.Fatalf("ListNodesByFlow() error=%v, want it to name corrupt config", err)
			}
		})
	}
}

func TestAddNodeRejectsNonFinitePosition(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("invalid-node-position-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	node := flow.FlowNode{
		ID:       ulid.Make().String(),
		FlowID:   created.ID,
		Kind:     flow.NodeSkill,
		Label:    "invalid-position",
		Config:   json.RawMessage(`{}`),
		Position: &flow.Position{X: math.Inf(1), Y: 10},
	}
	if _, err := store.AddNode(ctx, node); err == nil {
		t.Fatal("AddNode accepted non-finite position")
	}

	nodes, err := store.ListNodesByFlow(ctx, created.ID)
	if err != nil {
		t.Fatalf("list nodes after rejected add: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("rejected invalid position persisted %d nodes", len(nodes))
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

func TestNodeReadsRejectCorruptPersistedPosition(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		position string
	}{
		{name: "malformed json", position: `{"x":`},
		{name: "array shape", position: `[1,2]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t)
			sqlStore := store.(*sqlStore)

			f := newFlow("node-corrupt-position-"+tt.name, "/ws")
			created, err := store.CreateFlow(ctx, f)
			if err != nil {
				t.Fatalf("create flow: %v", err)
			}

			node, err := store.AddNode(ctx, flow.FlowNode{
				ID:       ulid.Make().String(),
				FlowID:   created.ID,
				Kind:     flow.NodeSkill,
				Label:    "with-position",
				Config:   json.RawMessage(`{}`),
				Position: &flow.Position{X: 100, Y: 200},
			})
			if err != nil {
				t.Fatalf("add node: %v", err)
			}

			if _, err := sqlStore.db.ExecContext(ctx, `
				UPDATE flow_nodes SET position = $1 WHERE id = $2
			`, tt.position, node.ID); err != nil {
				t.Fatalf("corrupt position: %v", err)
			}

			if _, err := store.GetNode(ctx, node.ID); !flowReadErrorNamesColumn(err, "position") {
				t.Fatalf("GetNode() error=%v, want it to name corrupt position", err)
			}
			if _, err := store.ListNodesByFlow(ctx, created.ID); !flowReadErrorNamesColumn(err, "position") {
				t.Fatalf("ListNodesByFlow() error=%v, want it to name corrupt position", err)
			}
		})
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

func TestAddEdgeRejectsInvalidEnums(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("invalid-edge-enums-test", "/ws")
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
	nodeA, err = store.AddNode(ctx, nodeA)
	if err != nil {
		t.Fatalf("add node A: %v", err)
	}
	nodeB, err = store.AddNode(ctx, nodeB)
	if err != nil {
		t.Fatalf("add node B: %v", err)
	}

	tests := []struct {
		name      string
		transform flow.TransformKind
		trigger   flow.TriggerKind
	}{
		{
			name:      "invalid transform",
			transform: flow.TransformKind("not-a-transform"),
			trigger:   flow.TriggerOutputReady,
		},
		{
			name:      "invalid trigger",
			transform: flow.TransformPassthrough,
			trigger:   flow.TriggerKind("not-a-trigger"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			edge := flow.FlowEdge{
				ID:         ulid.Make().String(),
				FlowID:     created.ID,
				FromNodeID: nodeA.ID,
				ToNodeID:   nodeB.ID,
				Transform:  tc.transform,
				Trigger:    tc.trigger,
			}
			if _, err := store.AddEdge(ctx, edge); err == nil {
				t.Fatalf("AddEdge accepted %s", tc.name)
			}
		})
	}

	edges, err := store.ListEdgesByFlow(ctx, created.ID)
	if err != nil {
		t.Fatalf("list edges after rejected adds: %v", err)
	}
	if len(edges) != 0 {
		t.Fatalf("rejected invalid edge enum persisted %d edges", len(edges))
	}
}

func TestEdgeReadsRejectCorruptPersistedEnums(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		column string
		value  string
	}{
		{name: "invalid transform", column: "transform", value: "not-a-transform"},
		{name: "invalid trigger", column: "trigger", value: "not-a-trigger"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t)
			sqlStore := store.(*sqlStore)

			f := newFlow("edge-corrupt-"+tt.column, "/ws")
			created, err := store.CreateFlow(ctx, f)
			if err != nil {
				t.Fatalf("create flow: %v", err)
			}
			nodeA, err := store.AddNode(ctx, flow.FlowNode{
				ID: ulid.Make().String(), FlowID: created.ID,
				Kind: flow.NodeSkill, Label: "a", Config: json.RawMessage(`{}`),
			})
			if err != nil {
				t.Fatalf("add node A: %v", err)
			}
			nodeB, err := store.AddNode(ctx, flow.FlowNode{
				ID: ulid.Make().String(), FlowID: created.ID,
				Kind: flow.NodeTransform, Label: "b", Config: json.RawMessage(`{}`),
			})
			if err != nil {
				t.Fatalf("add node B: %v", err)
			}
			edge, err := store.AddEdge(ctx, flow.FlowEdge{
				ID:         ulid.Make().String(),
				FlowID:     created.ID,
				FromNodeID: nodeA.ID,
				ToNodeID:   nodeB.ID,
				Transform:  flow.TransformPassthrough,
				Trigger:    flow.TriggerOutputReady,
			})
			if err != nil {
				t.Fatalf("add edge: %v", err)
			}

			if _, err := sqlStore.db.ExecContext(ctx, fmt.Sprintf(`
				UPDATE flow_edges SET %s = $1 WHERE id = $2
			`, tt.column), tt.value, edge.ID); err != nil {
				t.Fatalf("corrupt %s: %v", tt.column, err)
			}

			if _, err := store.GetEdge(ctx, edge.ID); !flowReadErrorNamesColumn(err, tt.column) {
				t.Fatalf("GetEdge() error=%v, want it to name corrupt %s", err, tt.column)
			}
			if _, err := store.ListEdgesByFlow(ctx, created.ID); !flowReadErrorNamesColumn(err, tt.column) {
				t.Fatalf("ListEdgesByFlow() error=%v, want it to name corrupt %s", err, tt.column)
			}
		})
	}
}

func TestAddEdgeEndpointFlowInvariantProperty(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	prop := func(fromInEdgeFlow, toInEdgeFlow bool) bool {
		flowA, err := store.CreateFlow(ctx, newFlow("edge-owner-"+ulid.Make().String(), "/ws"))
		if err != nil {
			t.Logf("create edge-owner flow: %v", err)
			return false
		}
		flowB, err := store.CreateFlow(ctx, newFlow("other-flow-"+ulid.Make().String(), "/ws"))
		if err != nil {
			t.Logf("create other flow: %v", err)
			return false
		}

		fromA, err := store.AddNode(ctx, flow.FlowNode{
			ID: ulid.Make().String(), FlowID: flowA.ID,
			Kind: flow.NodeSkill, Label: "from-a", Config: json.RawMessage(`{}`),
		})
		if err != nil {
			t.Logf("add fromA: %v", err)
			return false
		}
		toA, err := store.AddNode(ctx, flow.FlowNode{
			ID: ulid.Make().String(), FlowID: flowA.ID,
			Kind: flow.NodeSkill, Label: "to-a", Config: json.RawMessage(`{}`),
		})
		if err != nil {
			t.Logf("add toA: %v", err)
			return false
		}
		fromB, err := store.AddNode(ctx, flow.FlowNode{
			ID: ulid.Make().String(), FlowID: flowB.ID,
			Kind: flow.NodeSkill, Label: "from-b", Config: json.RawMessage(`{}`),
		})
		if err != nil {
			t.Logf("add fromB: %v", err)
			return false
		}
		toB, err := store.AddNode(ctx, flow.FlowNode{
			ID: ulid.Make().String(), FlowID: flowB.ID,
			Kind: flow.NodeSkill, Label: "to-b", Config: json.RawMessage(`{}`),
		})
		if err != nil {
			t.Logf("add toB: %v", err)
			return false
		}

		fromNode := fromB
		if fromInEdgeFlow {
			fromNode = fromA
		}
		toNode := toB
		if toInEdgeFlow {
			toNode = toA
		}

		before, err := store.ListEdgesByFlow(ctx, flowA.ID)
		if err != nil {
			t.Logf("list edges before: %v", err)
			return false
		}

		_, err = store.AddEdge(ctx, flow.FlowEdge{
			ID:         ulid.Make().String(),
			FlowID:     flowA.ID,
			FromNodeID: fromNode.ID,
			ToNodeID:   toNode.ID,
			Transform:  flow.TransformPassthrough,
			Trigger:    flow.TriggerOutputReady,
		})

		after, listErr := store.ListEdgesByFlow(ctx, flowA.ID)
		if listErr != nil {
			t.Logf("list edges after: %v", listErr)
			return false
		}

		valid := fromInEdgeFlow && toInEdgeFlow
		if valid {
			return err == nil && len(after) == len(before)+1
		}
		return err != nil && len(after) == len(before)
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 50}); err != nil {
		t.Fatalf("edge endpoint flow invariant failed: %v", err)
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

func TestAddEdgeRejectsNegativeRetryPolicy(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("edge-negative-retry-test", "/ws")
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
	nodeA, err = store.AddNode(ctx, nodeA)
	if err != nil {
		t.Fatalf("add node A: %v", err)
	}
	nodeB, err = store.AddNode(ctx, nodeB)
	if err != nil {
		t.Fatalf("add node B: %v", err)
	}

	tests := []struct {
		name  string
		retry flow.RetryPolicy
	}{
		{name: "negative attempts", retry: flow.RetryPolicy{MaxAttempts: -1, DelayMS: 0}},
		{name: "negative delay", retry: flow.RetryPolicy{MaxAttempts: 1, DelayMS: -1}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.AddEdge(ctx, flow.FlowEdge{
				ID:          ulid.Make().String(),
				FlowID:      created.ID,
				FromNodeID:  nodeA.ID,
				ToNodeID:    nodeB.ID,
				Transform:   flow.TransformPassthrough,
				Trigger:     flow.TriggerOutputReady,
				RetryPolicy: &tc.retry,
			})
			if err == nil {
				t.Fatal("AddEdge accepted negative retry policy")
			}
		})
	}

	edges, err := store.ListEdgesByFlow(ctx, created.ID)
	if err != nil {
		t.Fatalf("list edges after rejected retry policies: %v", err)
	}
	if len(edges) != 0 {
		t.Fatalf("rejected negative retry policy persisted %d edges", len(edges))
	}
}

func TestEdgeReadsRejectCorruptPersistedRetryPolicy(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		retryPolicy string
	}{
		{name: "malformed json", retryPolicy: `{"max_attempts":`},
		{name: "negative attempts", retryPolicy: `{"max_attempts":-1,"delay_ms":0}`},
		{name: "negative delay", retryPolicy: `{"max_attempts":1,"delay_ms":-1}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t)
			sqlStore := store.(*sqlStore)

			f := newFlow("edge-corrupt-retry-"+tt.name, "/ws")
			created, err := store.CreateFlow(ctx, f)
			if err != nil {
				t.Fatalf("create flow: %v", err)
			}
			nodeA, err := store.AddNode(ctx, flow.FlowNode{
				ID: ulid.Make().String(), FlowID: created.ID,
				Kind: flow.NodeSkill, Label: "a", Config: json.RawMessage(`{}`),
			})
			if err != nil {
				t.Fatalf("add node A: %v", err)
			}
			nodeB, err := store.AddNode(ctx, flow.FlowNode{
				ID: ulid.Make().String(), FlowID: created.ID,
				Kind: flow.NodeTransform, Label: "b", Config: json.RawMessage(`{}`),
			})
			if err != nil {
				t.Fatalf("add node B: %v", err)
			}
			edge, err := store.AddEdge(ctx, flow.FlowEdge{
				ID:          ulid.Make().String(),
				FlowID:      created.ID,
				FromNodeID:  nodeA.ID,
				ToNodeID:    nodeB.ID,
				Transform:   flow.TransformPassthrough,
				Trigger:     flow.TriggerOutputReady,
				RetryPolicy: &flow.RetryPolicy{MaxAttempts: 2, DelayMS: 10},
			})
			if err != nil {
				t.Fatalf("add edge: %v", err)
			}

			if _, err := sqlStore.db.ExecContext(ctx, `
				UPDATE flow_edges SET retry_policy = $1 WHERE id = $2
			`, tt.retryPolicy, edge.ID); err != nil {
				t.Fatalf("corrupt retry_policy: %v", err)
			}

			if _, err := store.GetEdge(ctx, edge.ID); !flowReadErrorNamesColumn(err, "retry_policy") {
				t.Fatalf("GetEdge() error=%v, want it to name corrupt retry_policy", err)
			}
			if _, err := store.ListEdgesByFlow(ctx, created.ID); !flowReadErrorNamesColumn(err, "retry_policy") {
				t.Fatalf("ListEdgesByFlow() error=%v, want it to name corrupt retry_policy", err)
			}
		})
	}
}

func TestAddEdgeRetryPolicyNonNegativeProperty(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("edge-retry-policy-property-test", "/ws")
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
	nodeA, err = store.AddNode(ctx, nodeA)
	if err != nil {
		t.Fatalf("add node A: %v", err)
	}
	nodeB, err = store.AddNode(ctx, nodeB)
	if err != nil {
		t.Fatalf("add node B: %v", err)
	}

	prop := func(maxAttempts int8, delayMS int8) bool {
		before, err := store.ListEdgesByFlow(ctx, created.ID)
		if err != nil {
			t.Logf("list edges before: %v", err)
			return false
		}

		_, addErr := store.AddEdge(ctx, flow.FlowEdge{
			ID:         ulid.Make().String(),
			FlowID:     created.ID,
			FromNodeID: nodeA.ID,
			ToNodeID:   nodeB.ID,
			Transform:  flow.TransformPassthrough,
			Trigger:    flow.TriggerOutputReady,
			RetryPolicy: &flow.RetryPolicy{
				MaxAttempts: int(maxAttempts),
				DelayMS:     int64(delayMS),
			},
		})

		after, err := store.ListEdgesByFlow(ctx, created.ID)
		if err != nil {
			t.Logf("list edges after: %v", err)
			return false
		}

		valid := maxAttempts >= 0 && delayMS >= 0
		if valid {
			return addErr == nil && len(after) == len(before)+1
		}
		return addErr != nil && len(after) == len(before)
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("edge retry policy property failed: %v", err)
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

func TestCreateRunRejectsInvalidState(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("run-invalid-state-create-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	_, err = store.CreateRun(ctx, flow.FlowRun{
		ID:        ulid.Make().String(),
		FlowID:    created.ID,
		State:     flow.RunState("not-a-run-state"),
		StartedAt: time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("CreateRun accepted invalid run state")
	}
}

func TestCreateRunRejectsCompletedAtBeforeStartedAt(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("run-invalid-completion-create-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	startedAt := time.Now().UTC()
	completedAt := startedAt.Add(-time.Second)
	if _, err := store.CreateRun(ctx, flow.FlowRun{
		ID:          ulid.Make().String(),
		FlowID:      created.ID,
		State:       flow.RunCompleted,
		StartedAt:   startedAt,
		CompletedAt: &completedAt,
	}); err == nil {
		t.Fatal("CreateRun accepted completed_at before started_at")
	}
}

func TestCreateRunRejectsRunningWithCompletedAt(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("run-running-completed-create-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	startedAt := time.Now().UTC()
	runID := ulid.Make().String()
	if _, err := store.CreateRun(ctx, flow.FlowRun{
		ID:          runID,
		FlowID:      created.ID,
		State:       flow.RunRunning,
		StartedAt:   startedAt,
		CompletedAt: ptrTime(startedAt.Add(time.Second)),
	}); err == nil {
		t.Fatal("CreateRun accepted running run with completed_at")
	}

	if _, err := store.GetRun(ctx, runID); err == nil {
		t.Fatal("rejected running run with completed_at was persisted")
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

func TestUpdateRunRejectsInvalidStateAndPreservesExistingRun(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("run-invalid-state-update-test", "/ws")
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

	invalid := createdRun
	invalid.State = flow.RunState("not-a-run-state")
	invalid.Error = "should not persist"
	if _, err := store.UpdateRun(ctx, invalid); err == nil {
		t.Fatal("UpdateRun accepted invalid run state")
	}

	persisted, err := store.GetRun(ctx, createdRun.ID)
	if err != nil {
		t.Fatalf("get run after rejected update: %v", err)
	}
	if persisted.State != flow.RunRunning {
		t.Fatalf("persisted state=%q want %q", persisted.State, flow.RunRunning)
	}
	if persisted.Error != "" {
		t.Fatalf("persisted error=%q want empty", persisted.Error)
	}
}

func TestUpdateRunRejectsCompletedAtBeforeStartedAtAndPreservesExistingRun(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("run-invalid-completion-update-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	startedAt := time.Now().UTC()
	createdRun, err := store.CreateRun(ctx, flow.FlowRun{
		ID:        ulid.Make().String(),
		FlowID:    created.ID,
		State:     flow.RunRunning,
		StartedAt: startedAt,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	invalid := createdRun
	invalid.State = flow.RunCompleted
	invalid.CompletedAt = ptrTime(startedAt.Add(-time.Second))
	if _, err := store.UpdateRun(ctx, invalid); err == nil {
		t.Fatal("UpdateRun accepted completed_at before started_at")
	}

	persisted, err := store.GetRun(ctx, createdRun.ID)
	if err != nil {
		t.Fatalf("get run after rejected update: %v", err)
	}
	if persisted.State != flow.RunRunning {
		t.Fatalf("persisted state=%q want %q", persisted.State, flow.RunRunning)
	}
	if persisted.CompletedAt != nil {
		t.Fatalf("persisted completed_at=%v want nil", persisted.CompletedAt)
	}
}

func TestUpdateRunRejectsRunningWithCompletedAtAndPreservesExistingRun(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("run-running-completed-update-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	startedAt := time.Now().UTC()
	createdRun, err := store.CreateRun(ctx, flow.FlowRun{
		ID:        ulid.Make().String(),
		FlowID:    created.ID,
		State:     flow.RunRunning,
		StartedAt: startedAt,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	invalid := createdRun
	invalid.CompletedAt = ptrTime(startedAt.Add(time.Second))
	if _, err := store.UpdateRun(ctx, invalid); err == nil {
		t.Fatal("UpdateRun accepted running run with completed_at")
	}

	persisted, err := store.GetRun(ctx, createdRun.ID)
	if err != nil {
		t.Fatalf("get run after rejected update: %v", err)
	}
	if persisted.State != flow.RunRunning {
		t.Fatalf("persisted state=%q want %q", persisted.State, flow.RunRunning)
	}
	if persisted.CompletedAt != nil {
		t.Fatalf("persisted completed_at=%v want nil", persisted.CompletedAt)
	}
}

func TestRunningRunHasNoCompletedAtProperty(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("run-running-completed-property-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	prop := func(offsetSeconds uint8) bool {
		startedAt := time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC)
		completedAt := startedAt.Add(time.Duration(offsetSeconds+1) * time.Second)

		createID := ulid.Make().String()
		_, createErr := store.CreateRun(ctx, flow.FlowRun{
			ID:          createID,
			FlowID:      created.ID,
			State:       flow.RunRunning,
			StartedAt:   startedAt,
			CompletedAt: &completedAt,
		})
		if createErr == nil {
			t.Logf("CreateRun accepted running completed_at offset=%d", offsetSeconds)
			return false
		}

		updateID := ulid.Make().String()
		running, err := store.CreateRun(ctx, flow.FlowRun{
			ID:        updateID,
			FlowID:    created.ID,
			State:     flow.RunRunning,
			StartedAt: startedAt,
		})
		if err != nil {
			t.Logf("CreateRun running offset=%d err=%v", offsetSeconds, err)
			return false
		}
		running.CompletedAt = &completedAt
		if _, err := store.UpdateRun(ctx, running); err == nil {
			t.Logf("UpdateRun accepted running completed_at offset=%d", offsetSeconds)
			return false
		}

		return true
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("running run completed_at property failed: %v", err)
	}
}

func TestRunCompletionTimestampOrderingProperty(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("run-completion-order-property-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	prop := func(offsetSeconds int8) bool {
		startedAt := time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC)
		completedAt := startedAt.Add(time.Duration(offsetSeconds) * time.Second)
		valid := !completedAt.Before(startedAt)

		createID := ulid.Make().String()
		_, createErr := store.CreateRun(ctx, flow.FlowRun{
			ID:          createID,
			FlowID:      created.ID,
			State:       flow.RunCompleted,
			StartedAt:   startedAt,
			CompletedAt: &completedAt,
		})
		if valid != (createErr == nil) {
			t.Logf("CreateRun offset=%d err=%v", offsetSeconds, createErr)
			return false
		}

		updateID := ulid.Make().String()
		running, err := store.CreateRun(ctx, flow.FlowRun{
			ID:        updateID,
			FlowID:    created.ID,
			State:     flow.RunRunning,
			StartedAt: startedAt,
		})
		if err != nil {
			t.Logf("CreateRun running offset=%d err=%v", offsetSeconds, err)
			return false
		}
		running.State = flow.RunCompleted
		running.CompletedAt = &completedAt
		_, updateErr := store.UpdateRun(ctx, running)
		if valid != (updateErr == nil) {
			t.Logf("UpdateRun offset=%d err=%v", offsetSeconds, updateErr)
			return false
		}

		return true
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("run completion timestamp ordering property failed: %v", err)
	}
}

func TestGetRunRejectsCorruptPersistedTimestamps(t *testing.T) {
	ctx := context.Background()

	for _, column := range []string{"started_at", "completed_at"} {
		t.Run(column, func(t *testing.T) {
			store := newTestStore(t)
			sqlStore := store.(*sqlStore)

			f := newFlow("run-corrupt-"+column, "/ws")
			created, err := store.CreateFlow(ctx, f)
			if err != nil {
				t.Fatalf("create flow: %v", err)
			}

			completedAt := time.Now().UTC()
			run, err := store.CreateRun(ctx, flow.FlowRun{
				ID:          ulid.Make().String(),
				FlowID:      created.ID,
				State:       flow.RunCompleted,
				StartedAt:   completedAt.Add(-time.Second),
				CompletedAt: &completedAt,
			})
			if err != nil {
				t.Fatalf("create run: %v", err)
			}

			if _, err := sqlStore.db.ExecContext(ctx, fmt.Sprintf(`
				UPDATE flow_runs SET %s = $1 WHERE id = $2
			`, column), "not-a-timestamp", run.ID); err != nil {
				t.Fatalf("corrupt %s: %v", column, err)
			}

			_, err = store.GetRun(ctx, run.ID)
			if err == nil {
				t.Fatalf("GetRun accepted corrupt %s", column)
			}
			if !strings.Contains(err.Error(), column) {
				t.Fatalf("GetRun error=%v, want it to name corrupt column %s", err, column)
			}
		})
	}
}

func TestGetRunRejectsCorruptPersistedState(t *testing.T) {
	store := newTestStore(t)
	sqlStore := store.(*sqlStore)
	ctx := context.Background()

	f := newFlow("run-corrupt-state", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	run, err := store.CreateRun(ctx, flow.FlowRun{
		ID:        ulid.Make().String(),
		FlowID:    created.ID,
		State:     flow.RunRunning,
		StartedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	if _, err := sqlStore.db.ExecContext(ctx, `
		UPDATE flow_runs SET state = $1 WHERE id = $2
	`, "not-a-run-state", run.ID); err != nil {
		t.Fatalf("corrupt state: %v", err)
	}

	if _, err := store.GetRun(ctx, run.ID); !flowReadErrorNamesColumn(err, "state") {
		t.Fatalf("GetRun() error=%v, want it to name corrupt state", err)
	}
}

func TestGetRunRejectsCorruptPersistedCompletionInvariant(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		initial   flow.RunState
		corruptAt func(startedAt time.Time) time.Time
	}{
		{
			name:    "running with completed_at",
			initial: flow.RunRunning,
			corruptAt: func(startedAt time.Time) time.Time {
				return startedAt.Add(time.Second)
			},
		},
		{
			name:    "completed before started_at",
			initial: flow.RunCompleted,
			corruptAt: func(startedAt time.Time) time.Time {
				return startedAt.Add(-time.Second)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t)
			sqlStore := store.(*sqlStore)

			f := newFlow("run-corrupt-completion-"+tt.name, "/ws")
			created, err := store.CreateFlow(ctx, f)
			if err != nil {
				t.Fatalf("create flow: %v", err)
			}

			startedAt := time.Now().UTC()
			completedAt := startedAt.Add(time.Second)
			run := flow.FlowRun{
				ID:        ulid.Make().String(),
				FlowID:    created.ID,
				State:     tt.initial,
				StartedAt: startedAt,
			}
			if tt.initial != flow.RunRunning {
				run.CompletedAt = &completedAt
			}
			createdRun, err := store.CreateRun(ctx, run)
			if err != nil {
				t.Fatalf("create run: %v", err)
			}

			corruptCompletedAt := tt.corruptAt(startedAt)
			if _, err := sqlStore.db.ExecContext(ctx, `
				UPDATE flow_runs SET completed_at = $1 WHERE id = $2
			`, sqlutil.FormatTimestamp(corruptCompletedAt), createdRun.ID); err != nil {
				t.Fatalf("corrupt completed_at: %v", err)
			}

			if _, err := store.GetRun(ctx, createdRun.ID); !flowReadErrorNamesColumn(err, "completed_at") {
				t.Fatalf("GetRun() error=%v, want it to name corrupt completed_at", err)
			}
		})
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

func TestUpdateRunTerminalStateCannotBeReopenedOrOverwritten(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("run-terminal-immutable-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	for _, terminal := range []flow.RunState{flow.RunCompleted, flow.RunFailed} {
		t.Run(string(terminal), func(t *testing.T) {
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

			completedAt := time.Now().UTC()
			createdRun.State = terminal
			createdRun.CompletedAt = &completedAt
			if terminal == flow.RunFailed {
				createdRun.Error = "original failure"
			}
			terminalRun, err := store.UpdateRun(ctx, createdRun)
			if err != nil {
				t.Fatalf("mark terminal: %v", err)
			}

			reopen := terminalRun
			reopen.State = flow.RunRunning
			reopen.CompletedAt = nil
			reopen.Error = "should not persist"
			if _, err := store.UpdateRun(ctx, reopen); err == nil {
				t.Fatal("UpdateRun reopened terminal run")
			}

			persisted, err := store.GetRun(ctx, terminalRun.ID)
			if err != nil {
				t.Fatalf("get run after rejected reopen: %v", err)
			}
			if persisted.State != terminal {
				t.Fatalf("persisted state=%q want %q", persisted.State, terminal)
			}
			if persisted.CompletedAt == nil || !persisted.CompletedAt.Equal(completedAt) {
				t.Fatalf("persisted completed_at=%v want %v", persisted.CompletedAt, completedAt)
			}
			if persisted.Error != terminalRun.Error {
				t.Fatalf("persisted error=%q want %q", persisted.Error, terminalRun.Error)
			}

			overwrite := persisted
			overwrite.CompletedAt = ptrTime(completedAt.Add(time.Second))
			overwrite.Error = "overwritten"
			if _, err := store.UpdateRun(ctx, overwrite); err == nil {
				t.Fatal("UpdateRun overwrote finalized terminal run")
			}
		})
	}
}

func TestUpdateRunCanFinalizeIncompleteTerminalRunOnce(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("run-terminal-finalize-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	run, err := store.CreateRun(ctx, flow.FlowRun{
		ID:        ulid.Make().String(),
		FlowID:    created.ID,
		State:     flow.RunCompleted,
		StartedAt: time.Now().Add(-time.Second).UTC(),
	})
	if err != nil {
		t.Fatalf("create incomplete terminal run: %v", err)
	}

	completedAt := time.Now().UTC()
	run.CompletedAt = &completedAt
	finalized, err := store.UpdateRun(ctx, run)
	if err != nil {
		t.Fatalf("finalize incomplete terminal run: %v", err)
	}
	if finalized.State != flow.RunCompleted {
		t.Fatalf("finalized state=%q want %q", finalized.State, flow.RunCompleted)
	}
	if finalized.CompletedAt == nil || !finalized.CompletedAt.Equal(completedAt) {
		t.Fatalf("finalized completed_at=%v want %v", finalized.CompletedAt, completedAt)
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
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
		RunID:    run.ID,
		NodeID:   "node-a",
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

func TestListRunLogsRejectsCorruptPersistedCreatedAt(t *testing.T) {
	store := newTestStore(t)
	sqlStore := store.(*sqlStore)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	envJSON := makeEnvelopeJSON(t, "ok", "test", nil)
	written, err := store.WriteRunLog(ctx, flow.RunLog{
		RunID:    run.ID,
		NodeID:   "node-a",
		Envelope: envJSON,
	})
	if err != nil {
		t.Fatalf("WriteRunLog: %v", err)
	}

	if _, err := sqlStore.db.ExecContext(ctx, `
		UPDATE flow_run_logs SET created_at = $1 WHERE id = $2
	`, "not-a-timestamp", written.ID); err != nil {
		t.Fatalf("corrupt run log created_at: %v", err)
	}

	_, err = store.ListRunLogs(ctx, run.ID)
	if err == nil {
		t.Fatal("ListRunLogs accepted corrupt created_at")
	}
	if !strings.Contains(err.Error(), "created_at") {
		t.Fatalf("ListRunLogs error=%v, want it to name corrupt created_at", err)
	}
}

func TestListRunLogsRejectsCorruptPersistedCreatedAtBeforeRunStart(t *testing.T) {
	store := newTestStore(t)
	sqlStore := store.(*sqlStore)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	written, err := store.WriteRunLog(ctx, flow.RunLog{
		RunID:    run.ID,
		NodeID:   "node-a",
		Envelope: makeEnvelopeJSON(t, "ok", "test", nil),
	})
	if err != nil {
		t.Fatalf("WriteRunLog: %v", err)
	}

	tooEarly := run.StartedAt.Add(-time.Nanosecond)
	if _, err := sqlStore.db.ExecContext(ctx, `
		UPDATE flow_run_logs SET created_at = $1 WHERE id = $2
	`, sqlutil.FormatTimestamp(tooEarly), written.ID); err != nil {
		t.Fatalf("corrupt created_at: %v", err)
	}

	if _, err := store.ListRunLogs(ctx, run.ID); !flowReadErrorNamesColumn(err, "created_at") {
		t.Fatalf("ListRunLogs() error=%v, want it to name corrupt created_at", err)
	}
}

func TestListRunLogsRejectsCorruptPersistedEnvelope(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		envelope string
	}{
		{name: "malformed json", envelope: `{"version":`},
		{name: "missing command", envelope: `{"version":1,"status":"ok","meta":{"ts":"2026-05-25T12:00:00Z"}}`},
		{name: "ok with error fields", envelope: `{"version":1,"status":"ok","command":"test","meta":{"ts":"2026-05-25T12:00:00Z"},"error":{"code":"EFAIL"}}`},
		{name: "error missing code", envelope: `{"version":1,"status":"error","command":"test","meta":{"ts":"2026-05-25T12:00:00Z"},"error":{"message":"failed"}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t)
			sqlStore := store.(*sqlStore)
			_, run := setupFlowRun(t, store, ctx)

			written, err := store.WriteRunLog(ctx, flow.RunLog{
				RunID:    run.ID,
				NodeID:   "node-a",
				Envelope: makeEnvelopeJSON(t, "ok", "test", nil),
			})
			if err != nil {
				t.Fatalf("WriteRunLog: %v", err)
			}

			if _, err := sqlStore.db.ExecContext(ctx, `
				UPDATE flow_run_logs SET envelope = $1 WHERE id = $2
			`, tt.envelope, written.ID); err != nil {
				t.Fatalf("corrupt envelope: %v", err)
			}

			if _, err := store.ListRunLogs(ctx, run.ID); !flowReadErrorNamesColumn(err, "envelope") {
				t.Fatalf("ListRunLogs() error=%v, want it to name corrupt envelope", err)
			}
		})
	}
}

func TestListRunLogsRejectsCorruptPersistedSeq(t *testing.T) {
	ctx := context.Background()

	for _, seq := range []int{0, -1} {
		t.Run(fmt.Sprintf("seq_%d", seq), func(t *testing.T) {
			store := newTestStore(t)
			sqlStore := store.(*sqlStore)
			_, run := setupFlowRun(t, store, ctx)

			written, err := store.WriteRunLog(ctx, flow.RunLog{
				RunID:    run.ID,
				NodeID:   "node-a",
				Envelope: makeEnvelopeJSON(t, "ok", "test", nil),
			})
			if err != nil {
				t.Fatalf("WriteRunLog: %v", err)
			}

			if _, err := sqlStore.db.ExecContext(ctx, `
				UPDATE flow_run_logs SET seq = $1 WHERE id = $2
			`, seq, written.ID); err != nil {
				t.Fatalf("corrupt seq: %v", err)
			}

			if _, err := store.ListRunLogs(ctx, run.ID); !flowReadErrorNamesColumn(err, "seq") {
				t.Fatalf("ListRunLogs() error=%v, want it to name corrupt seq", err)
			}
		})
	}
}

func TestListRunLogsRejectsDuplicatePersistedSeq(t *testing.T) {
	store := newTestStore(t)
	sqlStore := store.(*sqlStore)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	first, err := store.WriteRunLog(ctx, flow.RunLog{
		RunID:    run.ID,
		NodeID:   "node-a",
		Envelope: makeEnvelopeJSON(t, "ok", "test", nil),
	})
	if err != nil {
		t.Fatalf("write first log: %v", err)
	}
	second, err := store.WriteRunLog(ctx, flow.RunLog{
		RunID:    run.ID,
		NodeID:   "node-b",
		Envelope: makeEnvelopeJSON(t, "ok", "test", nil),
	})
	if err != nil {
		t.Fatalf("write second log: %v", err)
	}

	if _, err := sqlStore.db.ExecContext(ctx, `
		UPDATE flow_run_logs SET seq = $1 WHERE id = $2
	`, first.Seq, second.ID); err != nil {
		t.Fatalf("corrupt duplicate seq: %v", err)
	}

	if _, err := store.ListRunLogs(ctx, run.ID); !flowReadErrorNamesColumn(err, "seq") {
		t.Fatalf("ListRunLogs() error=%v, want it to name duplicate seq", err)
	}
}

func TestWriteRunLogRejectsInvalidEnvelopeAndPreservesSequence(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	validEnvelope := makeEnvelopeJSON(t, "ok", "test", map[string]any{"i": 1})
	first, err := store.WriteRunLog(ctx, flow.RunLog{
		RunID:    run.ID,
		NodeID:   "node-a",
		Envelope: validEnvelope,
	})
	if err != nil {
		t.Fatalf("write first valid log: %v", err)
	}
	if first.Seq != 1 {
		t.Fatalf("first seq=%d want 1", first.Seq)
	}

	tests := []struct {
		name     string
		envelope json.RawMessage
	}{
		{name: "malformed json", envelope: json.RawMessage(`{"version":`)},
		{name: "missing command", envelope: json.RawMessage(`{"version":1,"status":"ok","meta":{"ts":"2026-05-25T12:00:00Z"}}`)},
		{name: "ok with error fields", envelope: json.RawMessage(`{"version":1,"status":"ok","command":"test","meta":{"ts":"2026-05-25T12:00:00Z"},"error":{"code":"EFAIL"}}`)},
		{name: "error missing code", envelope: json.RawMessage(`{"version":1,"status":"error","command":"test","meta":{"ts":"2026-05-25T12:00:00Z"},"error":{"message":"failed"}}`)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.WriteRunLog(ctx, flow.RunLog{
				RunID:    run.ID,
				NodeID:   "node-invalid",
				Envelope: tc.envelope,
			}); err == nil {
				t.Fatal("WriteRunLog accepted invalid envelope")
			}
		})
	}

	second, err := store.WriteRunLog(ctx, flow.RunLog{
		RunID:    run.ID,
		NodeID:   "node-b",
		Envelope: validEnvelope,
	})
	if err != nil {
		t.Fatalf("write second valid log: %v", err)
	}
	if second.Seq != 2 {
		t.Fatalf("second seq=%d want 2 after rejected invalid logs", second.Seq)
	}
}

func TestWriteRunLogEnvelopeValidationProperty(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("run-log-envelope-property-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	prop := func(statusSelector uint8, badVersion, emptyCommand, missingTS, includeErrorCode, includeErrorMessage bool) bool {
		env := envelopepkg.Envelope{
			Version: envelopepkg.Version,
			Status:  envelopepkg.StatusOK,
			Command: "flow/log-test",
			Meta: envelopepkg.Meta{
				TS: "2026-05-25T12:00:00Z",
			},
		}

		switch statusSelector % 3 {
		case 0:
			env.Status = envelopepkg.StatusOK
		case 1:
			env.Status = envelopepkg.StatusError
		default:
			env.Status = "unknown"
		}
		if badVersion {
			env.Version = envelopepkg.Version + 1
		}
		if emptyCommand {
			env.Command = ""
		}
		if missingTS {
			env.Meta.TS = ""
		}
		if includeErrorCode {
			env.Error.Code = "ETEST"
		}
		if includeErrorMessage {
			env.Error.Message = "failed"
		}

		raw, err := json.Marshal(env)
		if err != nil {
			t.Logf("marshal envelope: %v", err)
			return false
		}
		wantValid := envelopepkg.Validate(env) == nil

		run, err := store.CreateRun(ctx, flow.FlowRun{
			ID:        ulid.Make().String(),
			FlowID:    created.ID,
			State:     flow.RunRunning,
			StartedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Logf("create run: %v", err)
			return false
		}

		_, writeErr := store.WriteRunLog(ctx, flow.RunLog{
			RunID:    run.ID,
			NodeID:   "node",
			Envelope: raw,
		})
		if wantValid != (writeErr == nil) {
			t.Logf("WriteRunLog validity mismatch wantValid=%v err=%v raw=%s", wantValid, writeErr, raw)
			return false
		}

		logs, err := store.ListRunLogs(ctx, run.ID)
		if err != nil {
			t.Logf("list logs: %v", err)
			return false
		}
		if wantValid {
			return len(logs) == 1
		}
		return len(logs) == 0
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("run log envelope validation property failed: %v", err)
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

func TestWriteRunLogRejectsCreatedAtBeforeRunStartedAtAndPreservesSequence(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, run := setupFlowRun(t, store, ctx)

	envJSON := makeEnvelopeJSON(t, "ok", "test", nil)
	first, err := store.WriteRunLog(ctx, flow.RunLog{
		RunID:    run.ID,
		NodeID:   "a",
		Envelope: envJSON,
	})
	if err != nil {
		t.Fatalf("write first log: %v", err)
	}
	if first.Seq != 1 {
		t.Fatalf("first seq=%d want 1", first.Seq)
	}

	if _, err := store.WriteRunLog(ctx, flow.RunLog{
		RunID:     run.ID,
		NodeID:    "too-early",
		Envelope:  envJSON,
		CreatedAt: run.StartedAt.Add(-time.Nanosecond),
	}); err == nil {
		t.Fatal("WriteRunLog accepted created_at before run started_at")
	}

	second, err := store.WriteRunLog(ctx, flow.RunLog{
		RunID:    run.ID,
		NodeID:   "b",
		Envelope: envJSON,
	})
	if err != nil {
		t.Fatalf("write second log: %v", err)
	}
	if second.Seq != 2 {
		t.Fatalf("second seq=%d want 2 after rejected early log", second.Seq)
	}
}

func TestRunLogCreatedAtOrderingProperty(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	f := newFlow("run-log-created-at-property-test", "/ws")
	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}
	envJSON := makeEnvelopeJSON(t, "ok", "test", nil)

	prop := func(offsetSeconds int8) bool {
		startedAt := time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC)
		run, err := store.CreateRun(ctx, flow.FlowRun{
			ID:        ulid.Make().String(),
			FlowID:    created.ID,
			State:     flow.RunRunning,
			StartedAt: startedAt,
		})
		if err != nil {
			t.Logf("create run: %v", err)
			return false
		}

		logCreatedAt := startedAt.Add(time.Duration(offsetSeconds) * time.Second)
		valid := !logCreatedAt.Before(startedAt)
		_, writeErr := store.WriteRunLog(ctx, flow.RunLog{
			RunID:     run.ID,
			NodeID:    "node",
			Envelope:  envJSON,
			CreatedAt: logCreatedAt,
		})
		if valid != (writeErr == nil) {
			t.Logf("WriteRunLog offset=%d valid=%v err=%v", offsetSeconds, valid, writeErr)
			return false
		}

		logs, err := store.ListRunLogs(ctx, run.ID)
		if err != nil {
			t.Logf("list run logs: %v", err)
			return false
		}
		if valid {
			return len(logs) == 1
		}
		return len(logs) == 0
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("run log created_at ordering property failed: %v", err)
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
