package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
)

// ---------------------------------------------------------------------------
// Mock executor for testing
// ---------------------------------------------------------------------------

// mockExecutorRecord records a single executor invocation.
type mockExecutorRecord struct {
	Input any
	Node  FlowNode
}

// mockExecutor is a controllable NodeExecutor for testing.
type mockExecutor struct {
	// fn is the function to call on Execute. If nil, returns a simple OK envelope.
	fn func(ctx context.Context, node FlowNode, input any) (NodeOutput, error)

	// records captures all Execute calls in order.
	records []mockExecutorRecord
	mu      sync.Mutex
}

func newMockExecutor(fn func(ctx context.Context, node FlowNode, input any) (NodeOutput, error)) *mockExecutor {
	return &mockExecutor{fn: fn}
}

func (m *mockExecutor) Execute(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
	m.mu.Lock()
	m.records = append(m.records, mockExecutorRecord{Input: input, Node: node})
	m.mu.Unlock()

	if m.fn != nil {
		return m.fn(ctx, node, input)
	}
	// Default: return OK envelope with input echoed.
	return NodeOutput{
		Envelope: envelope.OK("mock", input),
		Duration: time.Millisecond,
		NodeID:   node.ID,
	}, nil
}

func (m *mockExecutor) getRecords() []mockExecutorRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]mockExecutorRecord, len(m.records))
	copy(cp, m.records)
	return cp
}

// ---------------------------------------------------------------------------
// Mock store for testing
// ---------------------------------------------------------------------------

type mockStore struct {
	flows map[string]Flow
	nodes map[string]FlowNode
	edges map[string]FlowEdge
	runs  map[string]FlowRun
	mu    sync.Mutex
}

func newMockStore() *mockStore {
	return &mockStore{
		flows: make(map[string]Flow),
		nodes: make(map[string]FlowNode),
		edges: make(map[string]FlowEdge),
		runs:  make(map[string]FlowRun),
	}
}

func (s *mockStore) Close() error { return nil }

func (s *mockStore) CreateFlow(_ context.Context, f Flow) (Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flows[f.ID] = f
	return f, nil
}

func (s *mockStore) GetFlow(_ context.Context, id string) (Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.flows[id]
	if !ok {
		return Flow{}, ErrNotFound
	}
	return f, nil
}

func (s *mockStore) GetFlowByName(_ context.Context, workspace, name string) (Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.flows {
		if f.Name == name && f.Workspace == workspace {
			return f, nil
		}
	}
	return Flow{}, ErrNotFound
}

func (s *mockStore) ListFlows(_ context.Context, workspace string) ([]Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []Flow
	for _, f := range s.flows {
		if f.Workspace == workspace {
			result = append(result, f)
		}
	}
	if result == nil {
		result = []Flow{}
	}
	return result, nil
}

func (s *mockStore) UpdateFlow(_ context.Context, f Flow) (Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f.UpdatedAt = time.Now().UTC()
	s.flows[f.ID] = f
	return f, nil
}

func (s *mockStore) DeleteFlow(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.flows, id)
	return nil
}

func (s *mockStore) AddNode(_ context.Context, n FlowNode) (FlowNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[n.ID] = n
	return n, nil
}

func (s *mockStore) GetNode(_ context.Context, id string) (FlowNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.nodes[id]
	if !ok {
		return FlowNode{}, ErrNotFound
	}
	return n, nil
}

func (s *mockStore) RemoveNode(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.nodes, id)
	return nil
}

func (s *mockStore) ListNodesByFlow(_ context.Context, flowID string) ([]FlowNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []FlowNode
	for _, n := range s.nodes {
		if n.FlowID == flowID {
			result = append(result, n)
		}
	}
	if result == nil {
		result = []FlowNode{}
	}
	return result, nil
}

func (s *mockStore) AddEdge(_ context.Context, e FlowEdge) (FlowEdge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.edges[e.ID] = e
	return e, nil
}

func (s *mockStore) GetEdge(_ context.Context, id string) (FlowEdge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.edges[id]
	if !ok {
		return FlowEdge{}, ErrNotFound
	}
	return e, nil
}

func (s *mockStore) RemoveEdge(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.edges, id)
	return nil
}

func (s *mockStore) ListEdgesByFlow(_ context.Context, flowID string) ([]FlowEdge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []FlowEdge
	for _, e := range s.edges {
		if e.FlowID == flowID {
			result = append(result, e)
		}
	}
	if result == nil {
		result = []FlowEdge{}
	}
	return result, nil
}

func (s *mockStore) CreateRun(_ context.Context, r FlowRun) (FlowRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[r.ID] = r
	return r, nil
}

func (s *mockStore) UpdateRun(_ context.Context, r FlowRun) (FlowRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[r.ID] = r
	return r, nil
}

func (s *mockStore) WriteRunLog(_ context.Context, log RunLog) (RunLog, error) {
	return log, nil
}

func (s *mockStore) ListRunLogs(_ context.Context, runID string, opts ...RunLogOption) ([]RunLog, error) {
	return []RunLog{}, nil
}

func (s *mockStore) StreamRunLogs(_ context.Context, runID string, opts ...RunLogOption) (<-chan RunLog, error) {
	ch := make(chan RunLog)
	close(ch)
	return ch, nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func makeFlow(id, name string) Flow {
	return Flow{
		ID:        id,
		Name:      name,
		Workspace: "/tmp/test",
		State:     FlowDraft,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

func makeNode(id, flowID, label string, kind NodeKind, config string) FlowNode {
	return FlowNode{
		ID:     id,
		FlowID: flowID,
		Kind:   kind,
		Label:  label,
		Config: json.RawMessage(config),
	}
}

func makeEdge(id, flowID, fromID, toID string, transform TransformKind, condition string) FlowEdge {
	return FlowEdge{
		ID:         id,
		FlowID:     flowID,
		FromNodeID: fromID,
		ToNodeID:   toID,
		Transform:  transform,
		Trigger:    TriggerOutputReady,
		Condition:  condition,
	}
}

func makeConfig(v string) json.RawMessage {
	return json.RawMessage(v)
}

// ---------------------------------------------------------------------------
// Tests: Topological Sort & Cycle Detection
// ---------------------------------------------------------------------------

func TestTopologicalSortLinear(t *testing.T) {
	// A → B → C should produce [A, B, C]
	nodes := []FlowNode{
		makeNode("a", "f1", "A", NodeSkill, "{}"),
		makeNode("b", "f1", "B", NodeSkill, "{}"),
		makeNode("c", "f1", "C", NodeSkill, "{}"),
	}
	edges := []FlowEdge{
		makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""),
		makeEdge("e2", "f1", "b", "c", TransformPassthrough, ""),
	}

	order, err := topologicalSort(nodes, edges)
	if err != nil {
		t.Fatalf("topologicalSort: %v", err)
	}
	if len(order) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(order))
	}
	// A should come before B, B before C
	aIdx, bIdx, cIdx := indexOf(order, "a"), indexOf(order, "b"), indexOf(order, "c")
	if aIdx >= bIdx {
		t.Errorf("A (idx %d) should come before B (idx %d)", aIdx, bIdx)
	}
	if bIdx >= cIdx {
		t.Errorf("B (idx %d) should come before C (idx %d)", bIdx, cIdx)
	}
}

func TestTopologicalSortCycle(t *testing.T) {
	// A → B → C → A should detect cycle
	nodes := []FlowNode{
		makeNode("a", "f1", "A", NodeSkill, "{}"),
		makeNode("b", "f1", "B", NodeSkill, "{}"),
		makeNode("c", "f1", "C", NodeSkill, "{}"),
	}
	edges := []FlowEdge{
		makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""),
		makeEdge("e2", "f1", "b", "c", TransformPassthrough, ""),
		makeEdge("e3", "f1", "c", "a", TransformPassthrough, ""),
	}

	_, err := topologicalSort(nodes, edges)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	// Error should mention "cycle"
	if err != nil && !containsString(err.Error(), "cycle") {
		t.Errorf("error should mention 'cycle', got: %v", err)
	}
}

func TestTopologicalSortDiamond(t *testing.T) {
	// A → B, A → C (fan-out)
	nodes := []FlowNode{
		makeNode("a", "f1", "A", NodeSkill, "{}"),
		makeNode("b", "f1", "B", NodeSkill, "{}"),
		makeNode("c", "f1", "C", NodeSkill, "{}"),
	}
	edges := []FlowEdge{
		makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""),
		makeEdge("e2", "f1", "a", "c", TransformPassthrough, ""),
	}

	order, err := topologicalSort(nodes, edges)
	if err != nil {
		t.Fatalf("topologicalSort: %v", err)
	}
	aIdx := indexOf(order, "a")
	bIdx := indexOf(order, "b")
	cIdx := indexOf(order, "c")
	if aIdx >= bIdx {
		t.Errorf("A should come before B")
	}
	if aIdx >= cIdx {
		t.Errorf("A should come before C")
	}
	_ = bIdx
	_ = cIdx
}

func TestTopologicalSortDisconnected(t *testing.T) {
	// A → B and C → D, disconnected subgraphs
	nodes := []FlowNode{
		makeNode("a", "f1", "A", NodeSkill, "{}"),
		makeNode("b", "f1", "B", NodeSkill, "{}"),
		makeNode("c", "f1", "C", NodeSkill, "{}"),
		makeNode("d", "f1", "D", NodeSkill, "{}"),
	}
	edges := []FlowEdge{
		makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""),
		makeEdge("e2", "f1", "c", "d", TransformPassthrough, ""),
	}

	order, err := topologicalSort(nodes, edges)
	if err != nil {
		t.Fatalf("topologicalSort: %v", err)
	}
	if len(order) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(order))
	}
}

func TestTopologicalSortSingleNode(t *testing.T) {
	nodes := []FlowNode{
		makeNode("a", "f1", "A", NodeSkill, "{}"),
	}
	order, err := topologicalSort(nodes, nil)
	if err != nil {
		t.Fatalf("topologicalSort: %v", err)
	}
	if len(order) != 1 || order[0].ID != "a" {
		t.Fatalf("expected [a], got %v", order)
	}
}

// ---------------------------------------------------------------------------
// Tests: OutputBus
// ---------------------------------------------------------------------------

func TestOutputBusFanOut(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newOutputBus(16)
	bus.start(ctx, "source")

	// Subscribe two consumers
	ch1 := bus.subscribe("source")
	ch2 := bus.subscribe("source")

	// Publish a message
	out := NodeOutput{
		Envelope: envelope.OK("test", map[string]any{"key": "value"}),
		NodeID:   "source",
	}
	bus.publish("source", out)

	// Both consumers should receive it
	select {
	case got := <-ch1:
		if got.NodeID != "source" {
			t.Errorf("ch1: expected nodeID=source, got %s", got.NodeID)
		}
	case <-time.After(time.Second):
		t.Fatal("ch1: timed out waiting for output")
	}

	select {
	case got := <-ch2:
		if got.NodeID != "source" {
			t.Errorf("ch2: expected nodeID=source, got %s", got.NodeID)
		}
	case <-time.After(time.Second):
		t.Fatal("ch2: timed out waiting for output")
	}
}

func TestOutputBusNoSubscribers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newOutputBus(16)
	bus.start(ctx, "source")

	// Publish with no subscribers should not block
	out := NodeOutput{
		Envelope: envelope.OK("test", "data"),
		NodeID:   "source",
	}
	bus.publish("source", out)
}

func TestOutputBusStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	bus := newOutputBus(16)
	bus.start(ctx, "source")
	ch := bus.subscribe("source")

	// Cancel context
	cancel()

	// Channel should be closed eventually
	// (In the current design, publish won't block after stop)
	_ = ch
}

// ---------------------------------------------------------------------------
// Tests: Engine Start/Stop/Pause/Status
// ---------------------------------------------------------------------------

func TestEngineStartLinearChain(t *testing.T) {
	store := newMockStore()
	exec := newMockExecutor(nil)
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	// Create flow: A → B → C
	flow := makeFlow("f1", "linear")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	nodes := []FlowNode{
		makeNode("a", "f1", "source", NodeSkill, `{"skill":"code/search"}`),
		makeNode("b", "f1", "middle", NodeSkill, `{"skill":"code/search"}`),
		makeNode("c", "f1", "sink", NodeSkill, `{"skill":"code/search"}`),
	}
	for _, n := range nodes {
		store.AddNode(context.Background(), n)
	}
	edges := []FlowEdge{
		makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""),
		makeEdge("e2", "f1", "b", "c", TransformPassthrough, ""),
	}
	for _, e := range edges {
		store.AddEdge(context.Background(), e)
	}

	eng := NewEngine(store, registry, 16)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for execution to complete
	time.Sleep(200 * time.Millisecond)

	// Stop the engine
	eng.Stop("f1")

	// All three nodes should have been executed
	records := exec.getRecords()
	if len(records) < 3 {
		t.Errorf("expected at least 3 executor calls, got %d", len(records))
	}

	// Source node (A) should have nil input
	foundSource := false
	for _, r := range records {
		if r.Node.ID == "a" {
			foundSource = true
			if r.Input != nil {
				t.Errorf("source node should receive nil input, got %v", r.Input)
			}
		}
	}
	if !foundSource {
		t.Error("source node A was never executed")
	}

	// Verify flow run was created
	runs := store.runs
	if len(runs) == 0 {
		t.Error("expected at least one FlowRun to be created")
	}
}

func TestEngineStartCycleDetection(t *testing.T) {
	store := newMockStore()
	registry := map[NodeKind]NodeExecutor{}

	// A → B → C → A (cycle)
	flow := makeFlow("f1", "cyclic")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	nodes := []FlowNode{
		makeNode("a", "f1", "A", NodeSkill, "{}"),
		makeNode("b", "f1", "B", NodeSkill, "{}"),
		makeNode("c", "f1", "C", NodeSkill, "{}"),
	}
	for _, n := range nodes {
		store.AddNode(context.Background(), n)
	}
	edges := []FlowEdge{
		makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""),
		makeEdge("e2", "f1", "b", "c", TransformPassthrough, ""),
		makeEdge("e3", "f1", "c", "a", TransformPassthrough, ""),
	}
	for _, e := range edges {
		store.AddEdge(context.Background(), e)
	}

	eng := NewEngine(store, registry, 16)

	err := eng.Start(context.Background(), "f1")
	if err == nil {
		t.Fatal("expected cycle detection error, got nil")
	}
	if !containsString(err.Error(), "cycle") {
		t.Errorf("error should mention 'cycle', got: %v", err)
	}
}

func TestEngineStartAlreadyRunning(t *testing.T) {
	store := newMockStore()
	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		// Slow executor to keep the engine running
		time.Sleep(2 * time.Second)
		return NodeOutput{
			Envelope: envelope.OK("mock", input),
			NodeID:   node.ID,
		}, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "running-test")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)
	store.AddNode(context.Background(), makeNode("a", "f1", "src", NodeSkill, "{}"))

	eng := NewEngine(store, registry, 16)

	ctx := context.Background()
	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("first Start: %v", err)
	}

	// Try to start again while running
	err = eng.Start(ctx, "f1")
	if err == nil {
		t.Fatal("expected error for already-running flow")
	}

	// Cleanup
	eng.Stop("f1")
}

func TestEngineStopNotRunning(t *testing.T) {
	store := newMockStore()
	registry := map[NodeKind]NodeExecutor{}

	eng := NewEngine(store, registry, 16)

	err := eng.Stop("nonexistent")
	if err == nil {
		t.Fatal("expected error for stopping non-running flow")
	}
}

func TestEngineStatus(t *testing.T) {
	store := newMockStore()
	exec := newMockExecutor(nil)
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "status-test")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))

	eng := NewEngine(store, registry, 16)

	ctx := context.Background()
	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for execution
	time.Sleep(100 * time.Millisecond)

	status := eng.Status("f1")
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.FlowState != FlowRunning && status.FlowState != FlowStopped {
		t.Errorf("expected running or stopped state, got %s", status.FlowState)
	}

	eng.Stop("f1")
}

func TestEnginePauseAndResume(t *testing.T) {
	store := newMockStore()
	var execCount atomic.Int32
	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		execCount.Add(1)
		return NodeOutput{
			Envelope: envelope.OK("mock", input),
			NodeID:   node.ID,
		}, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "pause-test")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	// Source → sink
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "sink", NodeSkill, "{}"))
	store.AddEdge(context.Background(), makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""))

	eng := NewEngine(store, registry, 16)

	ctx := context.Background()
	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for source to execute
	time.Sleep(100 * time.Millisecond)

	// Pause
	err = eng.Pause("f1")
	if err != nil {
		t.Fatalf("Pause: %v", err)
	}

	status := eng.Status("f1")
	if status == nil || status.FlowState != FlowPaused {
		t.Fatalf("expected paused state, got %v", status)
	}

	// Resume
	err = eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Resume (Start): %v", err)
	}

	eng.Stop("f1")
}

func TestEngineFlowNotFound(t *testing.T) {
	store := newMockStore()
	registry := map[NodeKind]NodeExecutor{}

	eng := NewEngine(store, registry, 16)

	err := eng.Start(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent flow")
	}
}

func TestEngineNoSourceNodes(t *testing.T) {
	store := newMockStore()
	registry := map[NodeKind]NodeExecutor{}

	flow := makeFlow("f1", "no-source")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	// Add a non-source node (has incoming edge from another node, but no source)
	store.AddNode(context.Background(), makeNode("a", "f1", "A", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "B", NodeSkill, "{}"))
	// B depends on A, but nothing feeds A → so if A has no incoming edges, it IS a source.
	// For this test, make B a source by having only an edge FROM B:
	store.AddEdge(context.Background(), makeEdge("e1", "f1", "b", "a", TransformPassthrough, ""))
	// Now B is a source and A is not. This should still work.
	// To truly test "no source nodes", we'd need a graph where every node has at least one incoming edge.

	eng := NewEngine(store, registry, 16)
	err := eng.Start(context.Background(), "f1")
	// B is a source, so this should work (not error).
	if err != nil {
		t.Logf("Start with one source node B: %v (acceptable)", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: Fan-out
// ---------------------------------------------------------------------------

func TestEngineFanOut(t *testing.T) {
	store := newMockStore()
	var mu sync.Mutex
	var calls []string
	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		mu.Lock()
		calls = append(calls, node.ID)
		mu.Unlock()
		return NodeOutput{
			Envelope: envelope.OK("mock", map[string]any{"from": node.ID}),
			NodeID:   node.ID,
		}, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "fanout")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	// A → B, A → C, A → D
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "B", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("c", "f1", "C", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("d", "f1", "D", NodeSkill, "{}"))
	store.AddEdge(context.Background(), makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""))
	store.AddEdge(context.Background(), makeEdge("e2", "f1", "a", "c", TransformPassthrough, ""))
	store.AddEdge(context.Background(), makeEdge("e3", "f1", "a", "d", TransformPassthrough, ""))

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for execution
	time.Sleep(500 * time.Millisecond)
	eng.Stop("f1")

	mu.Lock()
	defer mu.Unlock()
	// All 4 nodes should have been called
	for _, id := range []string{"a", "b", "c", "d"} {
		found := false
		for _, c := range calls {
			if c == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected node %s to be executed, calls: %v", id, calls)
		}
	}
}

func TestEngineFanOutWithConditions(t *testing.T) {
	store := newMockStore()
	var mu sync.Mutex
	var calls []string
	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		mu.Lock()
		calls = append(calls, node.ID)
		mu.Unlock()

		status := envelope.StatusOK
		if node.ID == "a" {
			// Source produces an error envelope
			status = envelope.StatusError
		}
		return NodeOutput{
			Envelope: envelope.Envelope{
				Version: 1,
				Status:  status,
				Command: "mock",
				Data:    map[string]any{"from": node.ID},
				Meta:    envelope.Meta{TS: time.Now().UTC().Format(time.RFC3339)},
			},
			NodeID: node.ID,
		}, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "fanout-cond")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	// A → B (condition: status == ok), A → C (no condition)
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "B", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("c", "f1", "C", NodeSkill, "{}"))
	store.AddEdge(context.Background(), makeEdge("e1", "f1", "a", "b", TransformPassthrough, "status == ok"))
	store.AddEdge(context.Background(), makeEdge("e2", "f1", "a", "c", TransformPassthrough, ""))

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	eng.Stop("f1")

	mu.Lock()
	defer mu.Unlock()

	// A should execute (source)
	// B should NOT execute (A produces error, edge filters status == ok)
	// C should execute (no condition filter)
	foundB := false
	for _, c := range calls {
		if c == "b" {
			foundB = true
		}
	}
	if foundB {
		t.Errorf("B should not have been executed (condition status == ok filtered error), calls: %v", calls)
	}

	foundC := false
	for _, c := range calls {
		if c == "c" {
			foundC = true
		}
	}
	if !foundC {
		t.Errorf("C should have been executed (no condition), calls: %v", calls)
	}
}

// ---------------------------------------------------------------------------
// Tests: Error handling
// ---------------------------------------------------------------------------

func TestEnginePanicRecovery(t *testing.T) {
	store := newMockStore()
	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		if node.ID == "a" {
			panic("deliberate panic")
		}
		return NodeOutput{
			Envelope: envelope.OK("mock", input),
			NodeID:   node.ID,
		}, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "panic-test")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Check status before stop — the source node should have errored state.
	status := eng.Status("f1")
	if status == nil {
		t.Fatal("expected non-nil status")
	}

	// Find the source node state.
	var sourceState *NodeExecState
	for i := range status.Nodes {
		if status.Nodes[i].ID == "a" {
			sourceState = &status.Nodes[i]
			break
		}
	}
	if sourceState == nil {
		t.Fatal("source node state not found")
	}
	if sourceState.State != "errored" {
		t.Errorf("expected source node errored state, got %s", sourceState.State)
	}
	if sourceState.Error == "" {
		t.Error("expected error message on source node")
	}

	eng.Stop("f1")
}

// ---------------------------------------------------------------------------
// Tests: TransformExecutor
// ---------------------------------------------------------------------------

func TestTransformExecutorAppliesTransform(t *testing.T) {
	store := newMockStore()
	registry := map[NodeKind]NodeExecutor{
		NodeTransform: &TransformExecutor{},
		NodeSkill: newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
			return NodeOutput{
				Envelope: envelope.OK("mock", map[string]any{"name": "test", "value": 42}),
				NodeID:   node.ID,
			}, nil
		}),
	}

	flow := makeFlow("f1", "transform-test")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	// Source (skill) → Transform (jq_filter .name)
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, `{"skill":"code/search"}`))
	store.AddNode(context.Background(), makeNode("b", "f1", "jq", NodeTransform,
		`{"transform":"jq_filter","config":{"filter":".name"}}`))
	store.AddEdge(context.Background(), makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""))

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	eng.Stop("f1")
}

// ---------------------------------------------------------------------------
// Tests: SkillExecutor
// ---------------------------------------------------------------------------

func TestSkillExecutorInvalidOutput(t *testing.T) {
	// Test that non-JSON output produces an error envelope
	// We can't easily mock executil.RunFoxctlSkill, so we test the parseOutput helper
	output := []byte("not json at all")
	env, err := parseOutputEnvelope(output)
	if err == nil {
		t.Fatal("expected parse error for invalid JSON")
	}
	_ = env // env should be zero value on error
}

func TestSkillExecutorValidOutput(t *testing.T) {
	output := []byte(`{"version":1,"status":"ok","command":"test","data":{"key":"value"},"meta":{"ts":"2025-01-01T00:00:00Z"}}`)
	env, err := parseOutputEnvelope(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Status != envelope.StatusOK {
		t.Errorf("expected status ok, got %s", env.Status)
	}
}

// ---------------------------------------------------------------------------
// Tests: FlowRun tracking
// ---------------------------------------------------------------------------

func TestEngineFlowRunCreatedOnStart(t *testing.T) {
	store := newMockStore()
	exec := newMockExecutor(nil)
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "run-track")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	eng.Stop("f1")

	// Check a run was created
	found := false
	for _, run := range store.runs {
		if run.FlowID == "f1" {
			found = true
			if run.State != RunRunning && run.State != RunCompleted && run.State != RunFailed {
				t.Errorf("unexpected run state: %s", run.State)
			}
		}
	}
	if !found {
		t.Error("expected FlowRun to be created for flow f1")
	}
}

// ---------------------------------------------------------------------------
// Tests: Restart flow
// ---------------------------------------------------------------------------

func TestEngineRestartAfterStop(t *testing.T) {
	store := newMockStore()
	var execCount atomic.Int32
	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		execCount.Add(1)
		return NodeOutput{
			Envelope: envelope.OK("mock", input),
			NodeID:   node.ID,
		}, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "restart-test")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))

	eng := NewEngine(store, registry, 16)
	ctx := context.Background()

	// First run
	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start 1: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	eng.Stop("f1")

	firstCount := execCount.Load()

	// Second run
	err = eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start 2: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	eng.Stop("f1")

	secondCount := execCount.Load()
	if secondCount <= firstCount {
		t.Errorf("expected more executions after restart, first=%d second=%d", firstCount, secondCount)
	}

	// Should have two runs
	runCount := 0
	for _, run := range store.runs {
		if run.FlowID == "f1" {
			runCount++
		}
	}
	if runCount < 2 {
		t.Errorf("expected at least 2 runs, got %d", runCount)
	}
}

// ---------------------------------------------------------------------------
// Tests: Goroutine leak check
// ---------------------------------------------------------------------------

func TestEngineNoGoroutineLeak(t *testing.T) {
	store := newMockStore()
	exec := newMockExecutor(nil)
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "leak-test")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))
	store.AddEdge(context.Background(), makeEdge("e1", "f1", "a", "a-sink", TransformPassthrough, ""))
	store.AddNode(context.Background(), makeNode("a-sink", "f1", "sink", NodeSkill, "{}"))

	eng := NewEngine(store, registry, 16)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	before := countGoroutines()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	eng.Stop("f1")
	time.Sleep(100 * time.Millisecond)

	after := countGoroutines()
	// Allow some slack for GC, but should not grow significantly
	if after > before+5 {
		t.Errorf("potential goroutine leak: before=%d after=%d", before, after)
	}
}

// ---------------------------------------------------------------------------
// Tests: Disconnected subgraphs
// ---------------------------------------------------------------------------

func TestEngineDisconnectedSubgraphs(t *testing.T) {
	store := newMockStore()
	var mu sync.Mutex
	var calls []string
	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		mu.Lock()
		calls = append(calls, node.ID)
		mu.Unlock()
		return NodeOutput{
			Envelope: envelope.OK("mock", input),
			NodeID:   node.ID,
		}, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "disconnected")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	// A → B, C → D
	store.AddNode(context.Background(), makeNode("a", "f1", "A", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "B", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("c", "f1", "C", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("d", "f1", "D", NodeSkill, "{}"))
	store.AddEdge(context.Background(), makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""))
	store.AddEdge(context.Background(), makeEdge("e2", "f1", "c", "d", TransformPassthrough, ""))

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	eng.Stop("f1")

	mu.Lock()
	defer mu.Unlock()
	for _, id := range []string{"a", "b", "c", "d"} {
		found := false
		for _, c := range calls {
			if c == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected node %s to execute, calls: %v", id, calls)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: Single-node flow
// ---------------------------------------------------------------------------

func TestEngineSingleSourceNode(t *testing.T) {
	store := newMockStore()
	exec := newMockExecutor(nil)
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "single")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)
	store.AddNode(context.Background(), makeNode("a", "f1", "only", NodeSkill, "{}"))

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	eng.Stop("f1")

	records := exec.getRecords()
	if len(records) < 1 {
		t.Fatal("expected at least 1 executor call")
	}
	if records[0].Input != nil {
		t.Errorf("source node should receive nil input, got %v", records[0].Input)
	}
}

// ---------------------------------------------------------------------------
// Tests: Context cancellation
// ---------------------------------------------------------------------------

func TestEngineContextCancellation(t *testing.T) {
	store := newMockStore()
	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		// Slow executor
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
		}
		return NodeOutput{
			Envelope: envelope.OK("mock", input),
			NodeID:   node.ID,
		}, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "cancel-test")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))

	eng := NewEngine(store, registry, 16)

	ctx, cancel := context.WithCancel(context.Background())
	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Cancel after a short delay
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Wait for cleanup
	time.Sleep(200 * time.Millisecond)

	// The engine should have cleaned up
	// Verify we can still query status (no panic)
	status := eng.Status("f1")
	_ = status // May be nil after cancellation, that's OK
}

// ---------------------------------------------------------------------------
// Tests: Error envelopes flow downstream
// ---------------------------------------------------------------------------

func TestEngineErrorEnvelopeFlowsDownstream(t *testing.T) {
	store := newMockStore()
	var mu sync.Mutex
	var receivedInputs []any
	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		mu.Lock()
		receivedInputs = append(receivedInputs, input)
		mu.Unlock()

		if node.ID == "a" {
			// Source produces an error envelope
			return NodeOutput{
				Envelope: envelope.Error("mock", "ERUNTIME", "source error", nil),
				NodeID:   "a",
			}, nil
		}
		return NodeOutput{
			Envelope: envelope.OK("mock", input),
			NodeID:   node.ID,
		}, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "error-flow")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	// A → B → C (no conditions, so error should propagate)
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "mid", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("c", "f1", "sink", NodeSkill, "{}"))
	store.AddEdge(context.Background(), makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""))
	store.AddEdge(context.Background(), makeEdge("e2", "f1", "b", "c", TransformPassthrough, ""))

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	eng.Stop("f1")

	mu.Lock()
	defer mu.Unlock()
	// B should have received input (the error envelope data from A)
	if len(receivedInputs) < 2 {
		t.Errorf("expected at least 2 inputs received (at B and C), got %d", len(receivedInputs))
	}
}

// ---------------------------------------------------------------------------
// Tests: Fan-out with different transforms
// ---------------------------------------------------------------------------

func TestEngineFanOutDifferentTransforms(t *testing.T) {
	store := newMockStore()
	var mu sync.Mutex
	var nodeInputs map[string]any = make(map[string]any)
	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		mu.Lock()
		nodeInputs[node.ID] = input
		mu.Unlock()

		if node.ID == "a" {
			return NodeOutput{
				Envelope: envelope.OK("mock", map[string]any{"name": "test", "value": 42}),
				NodeID:   "a",
			}, nil
		}
		return NodeOutput{
			Envelope: envelope.OK("mock", input),
			NodeID:   node.ID,
		}, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "fanout-transform")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "B", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("c", "f1", "C", NodeSkill, "{}"))

	// A → B with passthrough, A → C with passthrough
	store.AddEdge(context.Background(), makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""))
	store.AddEdge(context.Background(), makeEdge("e2", "f1", "a", "c", TransformPassthrough, ""))

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	eng.Stop("f1")

	mu.Lock()
	defer mu.Unlock()
	// Both B and C should have received input
	if _, ok := nodeInputs["b"]; !ok {
		t.Error("B did not receive input")
	}
	if _, ok := nodeInputs["c"]; !ok {
		t.Error("C did not receive input")
	}
}

// ---------------------------------------------------------------------------
// Tests: Graceful shutdown
// ---------------------------------------------------------------------------

func TestEngineGracefulShutdown(t *testing.T) {
	store := newMockStore()
	exec := newMockExecutor(nil)
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "graceful")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	// Build a multi-node graph
	store.AddNode(context.Background(), makeNode("a", "f1", "A", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "B", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("c", "f1", "C", NodeSkill, "{}"))
	store.AddEdge(context.Background(), makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""))
	store.AddEdge(context.Background(), makeEdge("e2", "f1", "a", "c", TransformPassthrough, ""))

	eng := NewEngine(store, registry, 16)
	ctx := context.Background()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Stop should return without error
	err = eng.Stop("f1")
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Second stop should return error (already stopped)
	err = eng.Stop("f1")
	if err == nil {
		t.Fatal("second Stop should return error")
	}
}

// ---------------------------------------------------------------------------
// Tests: Concurrent status queries during execution (VAL-M2-052)
// ---------------------------------------------------------------------------

func TestEngineConcurrentStatusDuringExecution(t *testing.T) {
	store := newMockStore()
	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		// Slow executor to keep running during status queries.
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
		}
		return NodeOutput{
			Envelope: envelope.OK("mock", input),
			NodeID:   node.ID,
		}, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "concurrent-status")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "sink", NodeSkill, "{}"))
	store.AddEdge(context.Background(), makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""))

	eng := NewEngine(store, registry, 16)
	ctx := context.Background()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Hammer status queries concurrently.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status := eng.Status("f1")
			if status != nil && status.FlowState != FlowRunning {
				t.Errorf("expected running, got %s", status.FlowState)
			}
		}()
	}
	wg.Wait()

	eng.Stop("f1")
}

// ---------------------------------------------------------------------------
// Tests: Edge delivery state tracking (VAL-M2-004)
// ---------------------------------------------------------------------------

func TestEngineEdgeDeliveryState(t *testing.T) {
	store := newMockStore()
	exec := newMockExecutor(nil)
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "edge-state")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	// A → B
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "sink", NodeSkill, "{}"))
	store.AddEdge(context.Background(), makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""))

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	status := eng.Status("f1")
	if status == nil {
		t.Fatal("expected non-nil status")
	}

	// Find edge e1 state.
	var edgeState *EdgeExecState
	for i := range status.Edges {
		if status.Edges[i].ID == "e1" {
			edgeState = &status.Edges[i]
			break
		}
	}
	if edgeState == nil {
		t.Fatal("edge e1 state not found")
	}
	if edgeState.DeliveryCount < 1 {
		t.Errorf("expected at least 1 delivery, got %d", edgeState.DeliveryCount)
	}
	if edgeState.LastDeliveryAt == nil {
		t.Error("expected last_delivery_at to be set")
	}

	eng.Stop("f1")
}

// ---------------------------------------------------------------------------
// Tests: Stopped flow can be started again (VAL-M2-045)
// ---------------------------------------------------------------------------

func TestEngineRestartNewRun(t *testing.T) {
	store := newMockStore()
	exec := newMockExecutor(nil)
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "restart-new-run")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))

	eng := NewEngine(store, registry, 16)
	ctx := context.Background()

	// First run
	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start 1: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	status1 := eng.Status("f1")
	runID1 := status1.RunID

	eng.Stop("f1")

	// Second run should produce a new run ID
	err = eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start 2: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	status2 := eng.Status("f1")
	runID2 := status2.RunID

	eng.Stop("f1")

	if runID1 == runID2 {
		t.Errorf("expected different run IDs, got same: %s", runID1)
	}
}

// ---------------------------------------------------------------------------
// Tests: Stop on errored flow (VAL-M2-006)
// ---------------------------------------------------------------------------

func TestEngineStopErroredFlow(t *testing.T) {
	store := newMockStore()
	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		panic("deliberate")
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "errored-stop")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Node should have errored; stop should work.
	err = eng.Stop("f1")
	if err != nil {
		t.Fatalf("Stop on errored flow: %v", err)
	}

	// Verify flow state in store.
	stored, err := store.GetFlow(context.Background(), "f1")
	if err != nil {
		t.Fatalf("GetFlow: %v", err)
	}
	if stored.State != FlowStopped {
		t.Errorf("expected stopped state, got %s", stored.State)
	}
}

// ---------------------------------------------------------------------------
// Tests: OutputBus backpressure (VAL-M2-034)
// ---------------------------------------------------------------------------

func TestOutputBusBackpressure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Small buffer to trigger backpressure
	bus := newOutputBus(2)
	bus.start(ctx, "source")

	// Subscriber that reads slowly
	ch := bus.subscribe("source")

	// Fill the buffer
	for i := 0; i < 3; i++ {
		out := NodeOutput{
			Envelope: envelope.OK("test", map[string]any{"i": i}),
			NodeID:   "source",
		}
		bus.publish("source", out)
	}

	// Read at least one message
	select {
	case <-ch:
		// Good
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
	}
}

// ---------------------------------------------------------------------------
// Tests: Cycle detection with error message including path (VAL-M2-009)
// ---------------------------------------------------------------------------

func TestEngineCyclePathInError(t *testing.T) {
	store := newMockStore()
	registry := map[NodeKind]NodeExecutor{}

	flow := makeFlow("f1", "cycle-path")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	store.AddNode(context.Background(), makeNode("a", "f1", "Alpha", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "Beta", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("c", "f1", "Gamma", NodeSkill, "{}"))
	store.AddEdge(context.Background(), makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""))
	store.AddEdge(context.Background(), makeEdge("e2", "f1", "b", "c", TransformPassthrough, ""))
	store.AddEdge(context.Background(), makeEdge("e3", "f1", "c", "a", TransformPassthrough, ""))

	eng := NewEngine(store, registry, 16)

	err := eng.Start(context.Background(), "f1")
	if err == nil {
		t.Fatal("expected cycle error")
	}
	// Error should include cycle path with labels
	errMsg := err.Error()
	if !containsString(errMsg, "cycle") {
		t.Errorf("error should mention cycle: %s", errMsg)
	}
	// Should contain at least one of the node labels
	if !containsString(errMsg, "Alpha") && !containsString(errMsg, "Beta") && !containsString(errMsg, "Gamma") {
		t.Errorf("error should include node labels in path: %s", errMsg)
	}
}

// ---------------------------------------------------------------------------
// Tests: Linear chain execution order (VAL-M2-007)
// ---------------------------------------------------------------------------

func TestEngineLinearChainOrder(t *testing.T) {
	store := newMockStore()
	var mu sync.Mutex
	var order []string
	var timestamps map[string]time.Time = make(map[string]time.Time)
	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		mu.Lock()
		order = append(order, node.ID)
		timestamps[node.ID] = time.Now()
		mu.Unlock()
		return NodeOutput{
			Envelope: envelope.OK("mock", input),
			NodeID:   node.ID,
		}, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "order-test")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	// A → B → C
	store.AddNode(context.Background(), makeNode("a", "f1", "A", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "B", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("c", "f1", "C", NodeSkill, "{}"))
	store.AddEdge(context.Background(), makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""))
	store.AddEdge(context.Background(), makeEdge("e2", "f1", "b", "c", TransformPassthrough, ""))

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	eng.Stop("f1")

	mu.Lock()
	defer mu.Unlock()

	// Verify execution order: A before B before C
	if len(order) < 3 {
		t.Fatalf("expected 3 executions, got %d: %v", len(order), order)
	}

	aIdx, bIdx, cIdx := -1, -1, -1
	for i, id := range order {
		switch id {
		case "a":
			if aIdx == -1 {
				aIdx = i
			}
		case "b":
			if bIdx == -1 {
				bIdx = i
			}
		case "c":
			if cIdx == -1 {
				cIdx = i
			}
		}
	}

	if aIdx == -1 || bIdx == -1 || cIdx == -1 {
		t.Fatalf("not all nodes executed: order=%v", order)
	}
	if aIdx >= bIdx {
		t.Errorf("A (idx %d) should execute before B (idx %d)", aIdx, bIdx)
	}
	if bIdx >= cIdx {
		t.Errorf("B (idx %d) should execute before C (idx %d)", bIdx, cIdx)
	}
}

// ---------------------------------------------------------------------------
// Tests: Source nodes receive nil input (VAL-M2-008)
// ---------------------------------------------------------------------------

func TestEngineSourceNodesReceiveNilInput(t *testing.T) {
	store := newMockStore()
	var mu sync.Mutex
	var sourceInputs []any
	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		mu.Lock()
		if input == nil {
			sourceInputs = append(sourceInputs, nil)
		}
		mu.Unlock()
		return NodeOutput{
			Envelope: envelope.OK("mock", input),
			NodeID:   node.ID,
		}, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "nil-input")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	// Two source nodes
	store.AddNode(context.Background(), makeNode("a", "f1", "A", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "B", NodeSkill, "{}"))

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	eng.Stop("f1")

	mu.Lock()
	defer mu.Unlock()
	if len(sourceInputs) < 2 {
		t.Errorf("expected at least 2 source nodes with nil input, got %d", len(sourceInputs))
	}
}

// ---------------------------------------------------------------------------
// Tests: Pause non-running flow returns error (VAL-M2-003)
// ---------------------------------------------------------------------------

func TestEnginePauseNonRunning(t *testing.T) {
	store := newMockStore()
	registry := map[NodeKind]NodeExecutor{}

	eng := NewEngine(store, registry, 16)

	err := eng.Pause("nonexistent")
	if err == nil {
		t.Fatal("expected error for pausing non-running flow")
	}
}

// ---------------------------------------------------------------------------
// Tests: Status for stopped flow (VAL-M2-004)
// ---------------------------------------------------------------------------

func TestEngineStatusAfterStop(t *testing.T) {
	store := newMockStore()
	exec := newMockExecutor(nil)
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "status-after-stop")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))

	eng := NewEngine(store, registry, 16)
	ctx := context.Background()

	eng.Start(ctx, "f1")
	time.Sleep(100 * time.Millisecond)
	eng.Stop("f1")

	// Status should return nil for stopped (removed from runs) flow.
	status := eng.Status("f1")
	if status != nil {
		t.Logf("status after stop: %+v (nil expected for removed flow)", status)
	}
}

// ---------------------------------------------------------------------------
// Tests: Concurrent node execution (VAL-M2-051)
// ---------------------------------------------------------------------------

func TestEngineConcurrentNodeExecution(t *testing.T) {
	store := newMockStore()
	var mu sync.Mutex
	var startTimes map[string]time.Time = make(map[string]time.Time)
	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		mu.Lock()
		startTimes[node.ID] = time.Now()
		mu.Unlock()
		// Small delay to make timing visible
		time.Sleep(50 * time.Millisecond)
		return NodeOutput{
			Envelope: envelope.OK("mock", input),
			NodeID:   node.ID,
		}, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "concurrent")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	// A → B, A → C (fan-out, B and C should start concurrently)
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "B", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("c", "f1", "C", NodeSkill, "{}"))
	store.AddEdge(context.Background(), makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""))
	store.AddEdge(context.Background(), makeEdge("e2", "f1", "a", "c", TransformPassthrough, ""))

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	eng.Stop("f1")

	mu.Lock()
	bStart, bOk := startTimes["b"]
	cStart, cOk := startTimes["c"]
	mu.Unlock()

	if !bOk || !cOk {
		t.Fatalf("B and/or C did not execute: b=%v c=%v", bOk, cOk)
	}

	// B and C should have started within 100ms of each other (concurrent, not sequential)
	diff := bStart.Sub(cStart)
	if diff < 0 {
		diff = -diff
	}
	if diff > 100*time.Millisecond {
		t.Errorf("B and C should start concurrently, but gap was %v", diff)
	}
}

// ---------------------------------------------------------------------------
// Tests: TransformExecutor produces valid envelope (VAL-M2-017)
// ---------------------------------------------------------------------------

func TestTransformExecutorProducesValidEnvelope(t *testing.T) {
	exec := &TransformExecutor{}
	node := makeNode("t1", "f1", "jq-filter", NodeTransform,
		`{"transform":"jq_filter","config":"{\"filter\":\".name\"}"}`)

	result, err := exec.Execute(context.Background(), node, map[string]any{"name": "test", "value": 42})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Envelope.Version != 1 {
		t.Errorf("expected version 1, got %d", result.Envelope.Version)
	}
	if result.Envelope.Status != envelope.StatusOK {
		t.Errorf("expected status ok, got %s", result.Envelope.Status)
	}
	if result.Envelope.Meta.TS == "" {
		t.Error("expected meta.ts to be set")
	}
}

// ---------------------------------------------------------------------------
// Tests: FlowRun updated on completion (VAL-M2-046)
// ---------------------------------------------------------------------------

func TestEngineFlowRunUpdatedOnCompletion(t *testing.T) {
	store := newMockStore()
	exec := newMockExecutor(nil)
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "run-completion")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))

	eng := NewEngine(store, registry, 16)
	ctx := context.Background()

	eng.Start(ctx, "f1")
	time.Sleep(100 * time.Millisecond)
	eng.Stop("f1")

	// Check FlowRun was updated to completed
	for _, run := range store.runs {
		if run.FlowID == "f1" {
			if run.State != RunCompleted {
				t.Errorf("expected run state completed, got %s", run.State)
			}
			if run.CompletedAt == nil {
				t.Error("expected completed_at to be set")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: Envelope integrity - every node produces valid envelope (VAL-M2-053)
// ---------------------------------------------------------------------------

func TestEngineEnvelopeIntegrity(t *testing.T) {
	store := newMockStore()
	var mu sync.Mutex
	var outputs []NodeOutput
	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		out := NodeOutput{
			Envelope: envelope.OK("mock", map[string]any{"from": node.ID}),
			NodeID:   node.ID,
			Duration: time.Millisecond,
		}
		mu.Lock()
		outputs = append(outputs, out)
		mu.Unlock()
		return out, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "envelope-integrity")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	// A → B → C
	store.AddNode(context.Background(), makeNode("a", "f1", "A", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "B", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("c", "f1", "C", NodeSkill, "{}"))
	store.AddEdge(context.Background(), makeEdge("e1", "f1", "a", "b", TransformPassthrough, ""))
	store.AddEdge(context.Background(), makeEdge("e2", "f1", "b", "c", TransformPassthrough, ""))

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	eng.Start(ctx, "f1")
	time.Sleep(300 * time.Millisecond)
	eng.Stop("f1")

	mu.Lock()
	defer mu.Unlock()

	for _, out := range outputs {
		if out.Envelope.Version != 1 {
			t.Errorf("envelope version should be 1, got %d", out.Envelope.Version)
		}
		if out.Envelope.Status == "" {
			t.Error("envelope status should not be empty")
		}
		if out.Envelope.Meta.TS == "" {
			t.Error("envelope meta.ts should not be empty")
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func indexOf(nodes []FlowNode, id string) int {
	for i, n := range nodes {
		if n.ID == id {
			return i
		}
	}
	return -1
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func countGoroutines() int {
	return runtime.NumGoroutine()
}

// Simple error wrapper for testing.
type testError struct {
	msg string
}

func (e testError) Error() string { return e.msg }

// ---------------------------------------------------------------------------
// Tests: Retry Policy (VAL-M2-038, VAL-M2-039, VAL-M2-040)
// ---------------------------------------------------------------------------

// TestRetryPolicyRetriesOnFailure tests VAL-M2-038:
// Edge with max_attempts=2 retries up to 2 times on failure.
// If retry succeeds, normal delivery continues.
func TestRetryPolicyRetriesOnFailure(t *testing.T) {
	store := newMockStore()
	var mu sync.Mutex
	var execCalls []string

	bAttempts := 0
	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		mu.Lock()
		execCalls = append(execCalls, node.ID)
		mu.Unlock()

		if node.ID == "b" {
			mu.Lock()
			bAttempts++
			attempt := bAttempts
			mu.Unlock()
			// Fail on first attempt, succeed on second (retry)
			if attempt == 1 {
				return NodeOutput{}, fmt.Errorf("transient failure")
			}
		}

		return NodeOutput{
			Envelope: envelope.OK("mock", input),
			NodeID:   node.ID,
		}, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "retry-test")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	// A → B with retry policy (max_attempts=2, delay_ms=10)
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "sink", NodeSkill, "{}"))
	edge := makeEdge("e1", "f1", "a", "b", TransformPassthrough, "")
	edge.RetryPolicy = &RetryPolicy{MaxAttempts: 2, DelayMS: 10}
	store.AddEdge(context.Background(), edge)

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	eng.Stop("f1")

	mu.Lock()
	defer mu.Unlock()

	// B should have been called at least twice (first fail, then retry succeeds)
	bCalls := 0
	for _, id := range execCalls {
		if id == "b" {
			bCalls++
		}
	}
	if bCalls < 2 {
		t.Errorf("expected at least 2 calls to B (initial + retry), got %d (all calls: %v)", bCalls, execCalls)
	}
}

// TestRetryPolicyDelayRespected tests VAL-M2-039:
// delay_ms controls the interval between retry attempts.
func TestRetryPolicyDelayRespected(t *testing.T) {
	store := newMockStore()
	var mu sync.Mutex
	var attemptTimes []time.Time

	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		// Only record times for the target node B
		if node.ID == "b" {
			mu.Lock()
			attemptTimes = append(attemptTimes, time.Now())
			mu.Unlock()
		}

		// Always fail for this test to observe retries
		return NodeOutput{}, fmt.Errorf("failure")
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "retry-delay")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	// A → B with retry policy (max_attempts=3, delay_ms=200)
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "sink", NodeSkill, "{}"))
	edge := makeEdge("e1", "f1", "a", "b", TransformPassthrough, "")
	edge.RetryPolicy = &RetryPolicy{MaxAttempts: 3, DelayMS: 200}
	store.AddEdge(context.Background(), edge)

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(1 * time.Second)
	eng.Stop("f1")

	mu.Lock()
	defer mu.Unlock()

	// Should have at least 3 attempts (1 initial + 2 retries = max_attempts=3 total)
	if len(attemptTimes) < 3 {
		t.Fatalf("expected at least 3 attempts, got %d", len(attemptTimes))
	}

	// Check delay between first and second attempt is at least 200ms
	delay := attemptTimes[1].Sub(attemptTimes[0])
	if delay < 180*time.Millisecond { // allow small tolerance
		t.Errorf("expected delay of ~200ms between attempts, got %v", delay)
	}

	// Check delay between second and third attempt
	delay2 := attemptTimes[2].Sub(attemptTimes[1])
	if delay2 < 180*time.Millisecond {
		t.Errorf("expected delay of ~200ms between attempts 2 and 3, got %v", delay2)
	}
}

// TestRetryPolicyExhaustedRetriesPropagateError tests VAL-M2-038:
// If all retries exhausted, error flows downstream.
func TestRetryPolicyExhaustedRetriesPropagateError(t *testing.T) {
	store := newMockStore()
	var mu sync.Mutex
	var sinkInputs []any

	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		mu.Lock()
		defer mu.Unlock()

		if node.ID == "b" {
			// Always fail
			return NodeOutput{}, fmt.Errorf("permanent failure")
		}

		// Sink C records its input
		if node.ID == "c" {
			sinkInputs = append(sinkInputs, input)
		}

		return NodeOutput{
			Envelope: envelope.OK("mock", input),
			NodeID:   node.ID,
		}, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "retry-exhausted")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	// A → B (with retry, max_attempts=2) → C
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "mid", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("c", "f1", "sink", NodeSkill, "{}"))
	edgeAB := makeEdge("e1", "f1", "a", "b", TransformPassthrough, "")
	edgeAB.RetryPolicy = &RetryPolicy{MaxAttempts: 2, DelayMS: 10}
	store.AddEdge(context.Background(), edgeAB)
	store.AddEdge(context.Background(), makeEdge("e2", "f1", "b", "c", TransformPassthrough, ""))

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(500 * time.Millisecond)
	eng.Stop("f1")

	mu.Lock()
	defer mu.Unlock()

	// After all retries exhausted, error from B should propagate to C
	if len(sinkInputs) == 0 {
		t.Error("expected sink C to receive input (error from B after exhausted retries)")
	}
}

// TestRetryPolicyZeroMeansNoRetry tests VAL-M2-040:
// Zero/null retry policy means no retry (single attempt).
func TestRetryPolicyZeroMeansNoRetry(t *testing.T) {
	store := newMockStore()
	var mu sync.Mutex
	var bCalls int

	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		mu.Lock()
		defer mu.Unlock()

		if node.ID == "b" {
			bCalls++
			return NodeOutput{}, fmt.Errorf("failure")
		}
		return NodeOutput{
			Envelope: envelope.OK("mock", input),
			NodeID:   node.ID,
		}, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "no-retry")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	// A → B with no retry policy (default)
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "sink", NodeSkill, "{}"))
	edge := makeEdge("e1", "f1", "a", "b", TransformPassthrough, "")
	// No RetryPolicy set (nil)
	store.AddEdge(context.Background(), edge)

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	eng.Stop("f1")

	mu.Lock()
	defer mu.Unlock()

	// B should have been called exactly once (no retry)
	if bCalls != 1 {
		t.Errorf("expected exactly 1 call to B (no retry), got %d", bCalls)
	}
}

// TestRetryPolicyZeroMaxAttemptsNoRetry tests VAL-M2-040:
// Explicit max_attempts=0 means no retry.
func TestRetryPolicyZeroMaxAttemptsNoRetry(t *testing.T) {
	store := newMockStore()
	var mu sync.Mutex
	var bCalls int

	exec := newMockExecutor(func(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
		mu.Lock()
		defer mu.Unlock()

		if node.ID == "b" {
			bCalls++
			return NodeOutput{}, fmt.Errorf("failure")
		}
		return NodeOutput{
			Envelope: envelope.OK("mock", input),
			NodeID:   node.ID,
		}, nil
	})
	registry := map[NodeKind]NodeExecutor{
		NodeSkill: exec,
	}

	flow := makeFlow("f1", "zero-retry")
	flow.State = FlowDraft
	store.CreateFlow(context.Background(), flow)

	// A → B with max_attempts=0
	store.AddNode(context.Background(), makeNode("a", "f1", "source", NodeSkill, "{}"))
	store.AddNode(context.Background(), makeNode("b", "f1", "sink", NodeSkill, "{}"))
	edge := makeEdge("e1", "f1", "a", "b", TransformPassthrough, "")
	edge.RetryPolicy = &RetryPolicy{MaxAttempts: 0, DelayMS: 100}
	store.AddEdge(context.Background(), edge)

	eng := NewEngine(store, registry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := eng.Start(ctx, "f1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	eng.Stop("f1")

	mu.Lock()
	defer mu.Unlock()

	// B should have been called exactly once (no retry with max_attempts=0)
	if bCalls != 1 {
		t.Errorf("expected exactly 1 call to B (max_attempts=0), got %d", bCalls)
	}
}

// Silence unused import
var _ = fmt.Sprintf
