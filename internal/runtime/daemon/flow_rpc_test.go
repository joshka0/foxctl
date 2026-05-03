package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/runtime/flow"
)

// ---------------------------------------------------------------------------
// Mock flow store for daemon RPC tests
// ---------------------------------------------------------------------------

type mockFlowStore struct {
	flows map[string]flow.Flow
	nodes map[string]flow.FlowNode
	edges map[string]flow.FlowEdge
	runs  map[string]flow.FlowRun
	mu    sync.Mutex
}

func newMockFlowStore() *mockFlowStore {
	return &mockFlowStore{
		flows: make(map[string]flow.Flow),
		nodes: make(map[string]flow.FlowNode),
		edges: make(map[string]flow.FlowEdge),
		runs:  make(map[string]flow.FlowRun),
	}
}

func (s *mockFlowStore) Close() error { return nil }

func (s *mockFlowStore) CreateFlow(_ context.Context, f flow.Flow) (flow.Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flows[f.ID] = f
	return f, nil
}

func (s *mockFlowStore) GetFlow(_ context.Context, id string) (flow.Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.flows[id]
	if !ok {
		return flow.Flow{}, flow.ErrNotFound
	}
	return f, nil
}

func (s *mockFlowStore) GetFlowByName(_ context.Context, workspace, name string) (flow.Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.flows {
		if f.Name == name && f.Workspace == workspace {
			return f, nil
		}
	}
	return flow.Flow{}, flow.ErrNotFound
}

func (s *mockFlowStore) ListFlows(_ context.Context, workspace string) ([]flow.Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []flow.Flow
	for _, f := range s.flows {
		if f.Workspace == workspace {
			result = append(result, f)
		}
	}
	if result == nil {
		result = []flow.Flow{}
	}
	return result, nil
}

func (s *mockFlowStore) UpdateFlow(_ context.Context, f flow.Flow) (flow.Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f.UpdatedAt = time.Now().UTC()
	s.flows[f.ID] = f
	return f, nil
}

func (s *mockFlowStore) DeleteFlow(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.flows, id)
	return nil
}

func (s *mockFlowStore) AddNode(_ context.Context, n flow.FlowNode) (flow.FlowNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[n.ID] = n
	return n, nil
}

func (s *mockFlowStore) GetNode(_ context.Context, id string) (flow.FlowNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.nodes[id]
	if !ok {
		return flow.FlowNode{}, flow.ErrNotFound
	}
	return n, nil
}

func (s *mockFlowStore) RemoveNode(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.nodes, id)
	return nil
}

func (s *mockFlowStore) ListNodesByFlow(_ context.Context, flowID string) ([]flow.FlowNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []flow.FlowNode
	for _, n := range s.nodes {
		if n.FlowID == flowID {
			result = append(result, n)
		}
	}
	if result == nil {
		result = []flow.FlowNode{}
	}
	return result, nil
}

func (s *mockFlowStore) AddEdge(_ context.Context, e flow.FlowEdge) (flow.FlowEdge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.edges[e.ID] = e
	return e, nil
}

func (s *mockFlowStore) GetEdge(_ context.Context, id string) (flow.FlowEdge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.edges[id]
	if !ok {
		return flow.FlowEdge{}, flow.ErrNotFound
	}
	return e, nil
}

func (s *mockFlowStore) RemoveEdge(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.edges, id)
	return nil
}

func (s *mockFlowStore) ListEdgesByFlow(_ context.Context, flowID string) ([]flow.FlowEdge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []flow.FlowEdge
	for _, e := range s.edges {
		if e.FlowID == flowID {
			result = append(result, e)
		}
	}
	if result == nil {
		result = []flow.FlowEdge{}
	}
	return result, nil
}

func (s *mockFlowStore) CreateRun(_ context.Context, r flow.FlowRun) (flow.FlowRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[r.ID] = r
	return r, nil
}

func (s *mockFlowStore) UpdateRun(_ context.Context, r flow.FlowRun) (flow.FlowRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[r.ID] = r
	return r, nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newServiceWithFlowEngine creates a Service with an initialized flow engine
// using the provided mock store and a no-op executor.
func newServiceWithFlowEngine(store flow.Store) *Service {
	executors := map[flow.NodeKind]flow.NodeExecutor{
		flow.NodeSkill: &noOpExecutor{},
	}
	engine := flow.NewEngine(store, executors, 16)
	return &Service{
		flowEngine: engine,
		flowStore:  store,
	}
}

// noOpExecutor is a simple NodeExecutor that returns a success envelope.
type noOpExecutor struct{}

func (e *noOpExecutor) Execute(_ context.Context, node flow.FlowNode, input any) (flow.NodeOutput, error) {
	return flow.NodeOutput{
		Envelope: envelope.OK("mock", input),
		Duration: time.Millisecond,
		NodeID:   node.ID,
	}, nil
}

// makeFlowTest helpers
func makeTestFlow(id, name, workspace string) flow.Flow {
	return flow.Flow{
		ID:        id,
		Name:      name,
		Workspace: workspace,
		State:     flow.FlowDraft,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

func makeTestNode(id, flowID, label string, kind flow.NodeKind) flow.FlowNode {
	return flow.FlowNode{
		ID:     id,
		FlowID: flowID,
		Kind:   kind,
		Label:  label,
		Config: json.RawMessage(`{}`),
	}
}

func makeTestEdge(id, flowID, from, to string) flow.FlowEdge {
	return flow.FlowEdge{
		ID:         id,
		FlowID:     flowID,
		FromNodeID: from,
		ToNodeID:   to,
		Transform:  flow.TransformPassthrough,
		Trigger:    flow.TriggerOutputReady,
	}
}

// seedTwoNodeFlow creates a flow with two source nodes (A→B) in the store.
func seedTwoNodeFlow(store *mockFlowStore, flowID, workspace string) {
	fl := makeTestFlow(flowID, "test-flow", workspace)
	nodeA := makeTestNode(flowID+"-n1", flowID, "A", flow.NodeSkill)
	nodeB := makeTestNode(flowID+"-n2", flowID, "B", flow.NodeSkill)
	edge := makeTestEdge(flowID+"-e1", flowID, nodeA.ID, nodeB.ID)

	store.CreateFlow(context.Background(), fl)
	store.AddNode(context.Background(), nodeA)
	store.AddNode(context.Background(), nodeB)
	store.AddEdge(context.Background(), edge)
}

// ---------------------------------------------------------------------------
// Tests: flow.start
// ---------------------------------------------------------------------------

func TestFlowStart_HappyPath(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	params := json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`)
	result, err := svc.handleFlowStart(context.Background(), params)
	if err != nil {
		t.Fatalf("handleFlowStart() error = %v", err)
	}

	if result.RunID == "" {
		t.Error("RunID is empty, expected non-empty")
	}
	if result.State != "running" {
		t.Errorf("State = %q, want %q", result.State, "running")
	}
	if result.FlowID != "flow-1" {
		t.Errorf("FlowID = %q, want %q", result.FlowID, "flow-1")
	}
}

func TestFlowStart_AlreadyRunning(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	params := json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`)
	_, err := svc.handleFlowStart(context.Background(), params)
	if err != nil {
		t.Fatalf("first start: %v", err)
	}

	// Try to start again — should fail with EALREADY
	result, err := svc.handleFlowStart(context.Background(), params)
	if err == nil {
		t.Fatalf("second start: expected error, got nil (result=%+v)", result)
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("error = %q, want 'already running'", err.Error())
	}
}

func TestFlowStart_ResumesPaused(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	params := json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`)
	startResult, err := svc.handleFlowStart(context.Background(), params)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	runID := startResult.RunID

	// Wait for source node to complete before pausing
	time.Sleep(100 * time.Millisecond)

	// Pause
	pauseParams := json.RawMessage(`{"flow_id":"flow-1"}`)
	_, err = svc.handleFlowPause(pauseParams)
	if err != nil {
		t.Fatalf("pause: %v", err)
	}

	// Resume by calling start again
	resumeResult, err := svc.handleFlowStart(context.Background(), params)
	if err != nil {
		t.Fatalf("resume (second start): %v", err)
	}
	if resumeResult.RunID != runID {
		t.Errorf("resume RunID = %q, want same as original %q", resumeResult.RunID, runID)
	}
	if resumeResult.State != "running" {
		t.Errorf("resume State = %q, want %q", resumeResult.State, "running")
	}
}

func TestFlowStart_FlowNotFound(t *testing.T) {
	store := newMockFlowStore()
	svc := newServiceWithFlowEngine(store)

	params := json.RawMessage(`{"flow_id":"nonexistent","workspace":"/tmp/ws"}`)
	_, err := svc.handleFlowStart(context.Background(), params)
	if err == nil {
		t.Fatal("expected ENOTFOUND error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want 'not found'", err.Error())
	}
}

func TestFlowStart_NoNodes(t *testing.T) {
	store := newMockFlowStore()
	fl := makeTestFlow("flow-empty", "empty", "/tmp/ws")
	store.CreateFlow(context.Background(), fl)
	svc := newServiceWithFlowEngine(store)

	params := json.RawMessage(`{"flow_id":"flow-empty","workspace":"/tmp/ws"}`)
	_, err := svc.handleFlowStart(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for flow with no nodes, got nil")
	}
	if !strings.Contains(err.Error(), "no nodes") {
		t.Errorf("error = %q, want 'no nodes'", err.Error())
	}
}

func TestFlowStart_NoSourceNodes(t *testing.T) {
	store := newMockFlowStore()
	fl := makeTestFlow("flow-no-src", "no-sources", "/tmp/ws")
	// Create two nodes with a cycle (A→B→A) so neither is a source
	nodeA := makeTestNode("n1", "flow-no-src", "A", flow.NodeSkill)
	nodeB := makeTestNode("n2", "flow-no-src", "B", flow.NodeSkill)
	edge1 := makeTestEdge("e1", "flow-no-src", nodeA.ID, nodeB.ID)
	edge2 := makeTestEdge("e2", "flow-no-src", nodeB.ID, nodeA.ID)
	store.CreateFlow(context.Background(), fl)
	store.AddNode(context.Background(), nodeA)
	store.AddNode(context.Background(), nodeB)
	store.AddEdge(context.Background(), edge1)
	store.AddEdge(context.Background(), edge2)

	svc := newServiceWithFlowEngine(store)
	params := json.RawMessage(`{"flow_id":"flow-no-src","workspace":"/tmp/ws"}`)
	_, err := svc.handleFlowStart(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for cyclic flow, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error = %q, want 'cycle'", err.Error())
	}
}

func TestFlowStart_CycleDetection(t *testing.T) {
	store := newMockFlowStore()
	fl := makeTestFlow("flow-cycle", "cycle", "/tmp/ws")
	nodeA := makeTestNode("cn1", "flow-cycle", "A", flow.NodeSkill)
	nodeB := makeTestNode("cn2", "flow-cycle", "B", flow.NodeSkill)
	nodeC := makeTestNode("cn3", "flow-cycle", "C", flow.NodeSkill)
	edgeAB := makeTestEdge("ce1", "flow-cycle", nodeA.ID, nodeB.ID)
	edgeBC := makeTestEdge("ce2", "flow-cycle", nodeB.ID, nodeC.ID)
	edgeCA := makeTestEdge("ce3", "flow-cycle", nodeC.ID, nodeA.ID)
	store.CreateFlow(context.Background(), fl)
	store.AddNode(context.Background(), nodeA)
	store.AddNode(context.Background(), nodeB)
	store.AddNode(context.Background(), nodeC)
	store.AddEdge(context.Background(), edgeAB)
	store.AddEdge(context.Background(), edgeBC)
	store.AddEdge(context.Background(), edgeCA)

	svc := newServiceWithFlowEngine(store)
	params := json.RawMessage(`{"flow_id":"flow-cycle","workspace":"/tmp/ws"}`)
	_, err := svc.handleFlowStart(context.Background(), params)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error = %q, want 'cycle detected'", err.Error())
	}
}

func TestFlowStart_MissingFlowID(t *testing.T) {
	store := newMockFlowStore()
	svc := newServiceWithFlowEngine(store)

	params := json.RawMessage(`{"workspace":"/tmp/ws"}`)
	_, err := svc.handleFlowStart(context.Background(), params)
	if err == nil {
		t.Fatal("expected EARG error for missing flow_id, got nil")
	}
	if !strings.Contains(err.Error(), "flow_id") {
		t.Errorf("error = %q, want 'flow_id'", err.Error())
	}
}

func TestFlowStart_NoEngine(t *testing.T) {
	svc := &Service{} // No flowEngine set
	params := json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`)
	_, err := svc.handleFlowStart(context.Background(), params)
	if err == nil {
		t.Fatal("expected error when engine not initialized, got nil")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("error = %q, want 'not initialized'", err.Error())
	}
}

func TestFlowStart_ConcurrentFlows(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-a", "/tmp/ws")
	seedTwoNodeFlow(store, "flow-b", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	paramsA := json.RawMessage(`{"flow_id":"flow-a","workspace":"/tmp/ws"}`)
	paramsB := json.RawMessage(`{"flow_id":"flow-b","workspace":"/tmp/ws"}`)

	resultA, errA := svc.handleFlowStart(context.Background(), paramsA)
	resultB, errB := svc.handleFlowStart(context.Background(), paramsB)

	if errA != nil {
		t.Fatalf("start flow-a: %v", errA)
	}
	if errB != nil {
		t.Fatalf("start flow-b: %v", errB)
	}

	if resultA.RunID == resultB.RunID {
		t.Error("concurrent flows should have distinct run IDs")
	}
}

// ---------------------------------------------------------------------------
// Tests: flow.stop
// ---------------------------------------------------------------------------

func TestFlowStop_HappyPath(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	startParams := json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`)
	_, err := svc.handleFlowStart(context.Background(), startParams)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// Wait for source node to complete before stopping to avoid race with bus goroutines
	time.Sleep(100 * time.Millisecond)

	stopParams := json.RawMessage(`{"flow_id":"flow-1"}`)
	result, err := svc.handleFlowStop(stopParams)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if result.State != "stopped" {
		t.Errorf("State = %q, want %q", result.State, "stopped")
	}
}

func TestFlowStop_PausedFlow(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	startParams := json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`)
	_, err := svc.handleFlowStart(context.Background(), startParams)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// Wait for source node to complete before pausing to avoid race
	time.Sleep(100 * time.Millisecond)

	pauseParams := json.RawMessage(`{"flow_id":"flow-1"}`)
	_, err = svc.handleFlowPause(pauseParams)
	if err != nil {
		t.Fatalf("pause: %v", err)
	}

	stopParams := json.RawMessage(`{"flow_id":"flow-1"}`)
	result, err := svc.handleFlowStop(stopParams)
	if err != nil {
		t.Fatalf("stop paused: %v", err)
	}
	if result.State != "stopped" {
		t.Errorf("State = %q, want %q", result.State, "stopped")
	}
}

func TestFlowStop_AlreadyStopped(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	startParams := json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`)
	_, err := svc.handleFlowStart(context.Background(), startParams)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// Wait for source node to complete
	time.Sleep(100 * time.Millisecond)

	stopParams := json.RawMessage(`{"flow_id":"flow-1"}`)
	_, err = svc.handleFlowStop(stopParams)
	if err != nil {
		t.Fatalf("first stop: %v", err)
	}

	// Second stop should fail
	_, err = svc.handleFlowStop(stopParams)
	if err == nil {
		t.Fatal("expected error for already stopped flow, got nil")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("error = %q, want 'not running'", err.Error())
	}
}

func TestFlowStop_DraftFlow(t *testing.T) {
	store := newMockFlowStore()
	svc := newServiceWithFlowEngine(store)

	stopParams := json.RawMessage(`{"flow_id":"flow-draft"}`)
	_, err := svc.handleFlowStop(stopParams)
	if err == nil {
		t.Fatal("expected error for draft flow, got nil")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("error = %q, want 'not running'", err.Error())
	}
}

func TestFlowStop_FlowNotFound(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	// Start the flow, then stop it — but call stop with wrong ID
	startParams := json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`)
	_, _ = svc.handleFlowStart(context.Background(), startParams)

	stopParams := json.RawMessage(`{"flow_id":"nonexistent"}`)
	_, err := svc.handleFlowStop(stopParams)
	if err == nil {
		t.Fatal("expected error for nonexistent flow, got nil")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("error = %q, want 'not running'", err.Error())
	}
}

func TestFlowStop_RunIsolation(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-a", "/tmp/ws")
	seedTwoNodeFlow(store, "flow-b", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	startA := json.RawMessage(`{"flow_id":"flow-a","workspace":"/tmp/ws"}`)
	startB := json.RawMessage(`{"flow_id":"flow-b","workspace":"/tmp/ws"}`)
	_, _ = svc.handleFlowStart(context.Background(), startA)
	_, _ = svc.handleFlowStart(context.Background(), startB)

	// Wait for source nodes to complete
	time.Sleep(100 * time.Millisecond)

	// Stop flow-a, flow-b should still be running
	stopA := json.RawMessage(`{"flow_id":"flow-a"}`)
	_, err := svc.handleFlowStop(stopA)
	if err != nil {
		t.Fatalf("stop flow-a: %v", err)
	}

	statusB := json.RawMessage(`{"flow_id":"flow-b"}`)
	result, err := svc.handleFlowStatus(statusB)
	if err != nil {
		t.Fatalf("status flow-b: %v", err)
	}
	if result.State != "running" {
		t.Errorf("flow-b state = %q, want 'running' (isolated from stop of flow-a)", result.State)
	}
}

func TestFlowStop_MissingFlowID(t *testing.T) {
	svc := newServiceWithFlowEngine(newMockFlowStore())
	params := json.RawMessage(`{}`)
	_, err := svc.handleFlowStop(params)
	if err == nil {
		t.Fatal("expected error for missing flow_id, got nil")
	}
	if !strings.Contains(err.Error(), "flow_id") {
		t.Errorf("error = %q, want 'flow_id'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Tests: flow.pause
// ---------------------------------------------------------------------------

func TestFlowPause_HappyPath(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	startParams := json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`)
	_, err := svc.handleFlowStart(context.Background(), startParams)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// Wait for source node to complete before pausing
	time.Sleep(100 * time.Millisecond)

	pauseParams := json.RawMessage(`{"flow_id":"flow-1"}`)
	result, err := svc.handleFlowPause(pauseParams)
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	if result.State != "paused" {
		t.Errorf("State = %q, want %q", result.State, "paused")
	}
}

func TestFlowPause_NotRunning(t *testing.T) {
	store := newMockFlowStore()
	svc := newServiceWithFlowEngine(store)

	pauseParams := json.RawMessage(`{"flow_id":"flow-1"}`)
	_, err := svc.handleFlowPause(pauseParams)
	if err == nil {
		t.Fatal("expected error for non-running flow, got nil")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("error = %q, want 'not running'", err.Error())
	}
}

func TestFlowPause_MissingFlowID(t *testing.T) {
	svc := newServiceWithFlowEngine(newMockFlowStore())
	params := json.RawMessage(`{}`)
	_, err := svc.handleFlowPause(params)
	if err == nil {
		t.Fatal("expected error for missing flow_id, got nil")
	}
	if !strings.Contains(err.Error(), "flow_id") {
		t.Errorf("error = %q, want 'flow_id'", err.Error())
	}
}

func TestFlowPause_NoEngine(t *testing.T) {
	svc := &Service{}
	params := json.RawMessage(`{"flow_id":"flow-1"}`)
	_, err := svc.handleFlowPause(params)
	if err == nil {
		t.Fatal("expected error when engine not initialized, got nil")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("error = %q, want 'not initialized'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Tests: flow.status
// ---------------------------------------------------------------------------

func TestFlowStatus_LiveEngineState(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	startParams := json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`)
	startResult, err := svc.handleFlowStart(context.Background(), startParams)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	runID := startResult.RunID

	// Give source node a moment to execute
	time.Sleep(50 * time.Millisecond)

	statusParams := json.RawMessage(`{"flow_id":"flow-1"}`)
	result, err := svc.handleFlowStatus(statusParams)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if result.State != "running" {
		t.Errorf("State = %q, want %q", result.State, "running")
	}
	if result.RunID != runID {
		t.Errorf("RunID = %q, want %q", result.RunID, runID)
	}
	if len(result.Nodes) == 0 {
		t.Error("expected non-empty node states")
	}
}

func TestFlowStatus_PersistedFallback(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	// Don't start the flow; status should fall back to store state
	statusParams := json.RawMessage(`{"flow_id":"flow-1"}`)
	result, err := svc.handleFlowStatus(statusParams)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if result.State != "draft" {
		t.Errorf("State = %q, want %q (from store)", result.State, "draft")
	}
}

func TestFlowStatus_FlowNotFound(t *testing.T) {
	store := newMockFlowStore()
	svc := newServiceWithFlowEngine(store)

	statusParams := json.RawMessage(`{"flow_id":"nonexistent"}`)
	_, err := svc.handleFlowStatus(statusParams)
	if err == nil {
		t.Fatal("expected ENOTFOUND error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want 'not found'", err.Error())
	}
}

func TestFlowStatus_MissingFlowID(t *testing.T) {
	svc := newServiceWithFlowEngine(newMockFlowStore())
	params := json.RawMessage(`{}`)
	_, err := svc.handleFlowStatus(params)
	if err == nil {
		t.Fatal("expected EARG error, got nil")
	}
	if !strings.Contains(err.Error(), "flow_id") {
		t.Errorf("error = %q, want 'flow_id'", err.Error())
	}
}

func TestFlowStatus_NoEngine(t *testing.T) {
	svc := &Service{}
	params := json.RawMessage(`{"flow_id":"flow-1"}`)
	_, err := svc.handleFlowStatus(params)
	if err == nil {
		t.Fatal("expected error when engine not initialized, got nil")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("error = %q, want 'not initialized'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Tests: Malformed JSON / Unknown Method
// ---------------------------------------------------------------------------

func TestFlowRPC_MalformedJSON(t *testing.T) {
	svc := newServiceWithFlowEngine(newMockFlowStore())

	// All handlers should handle malformed JSON gracefully
	_, err := svc.handleFlowStart(context.Background(), json.RawMessage(`{invalid json`))
	if err == nil {
		t.Fatal("expected parse error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error = %q, want 'parse'", err.Error())
	}

	_, err = svc.handleFlowStop(json.RawMessage(`{invalid json`))
	if err == nil {
		t.Fatal("expected parse error for malformed JSON, got nil")
	}

	_, err = svc.handleFlowPause(json.RawMessage(`{invalid json`))
	if err == nil {
		t.Fatal("expected parse error for malformed JSON, got nil")
	}

	_, err = svc.handleFlowStatus(json.RawMessage(`{invalid json`))
	if err == nil {
		t.Fatal("expected parse error for malformed JSON, got nil")
	}
}

func TestFlowRPC_UnknownMethod(t *testing.T) {
	// Test via handleConnection that an unknown flow method returns EMETHOD error.
	svc := &Service{
		started:    time.Now(),
		shutdownCh: make(chan struct{}),
	}

	client, server := netPipe(t)
	defer client.Close()

	go func() {
		defer server.Close()
		svc.handleConnection(context.Background(), server)
	}()

	req := Request{
		Method: "flow.nonexistent",
		ID:     "test-unknown-flow",
		Params: json.RawMessage(`{}`),
	}
	encoder := json.NewEncoder(client)
	if err := encoder.Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoder := json.NewDecoder(client)
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error for unknown flow method, got nil")
	}
	if resp.Error.Code != "EMETHOD" {
		t.Errorf("Error.Code = %q, want %q", resp.Error.Code, "EMETHOD")
	}
}

// ---------------------------------------------------------------------------
// Tests: handleConnection routing
// ---------------------------------------------------------------------------

func TestHandleConnection_FlowStart(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")

	svc := newServiceWithFlowEngine(store)
	svc.started = time.Now()
	svc.shutdownCh = make(chan struct{})

	// Create a pipe connection
	client, server := netPipe(t)
	defer client.Close()

	go func() {
		defer server.Close()
		svc.handleConnection(context.Background(), server)
	}()

	req := Request{
		Method: "flow.start",
		ID:     "test-start-1",
		Params: json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`),
	}
	encoder := json.NewEncoder(client)
	if err := encoder.Encode(&req); err != nil {
		t.Fatalf("encode request: %v", err)
	}

	decoder := json.NewDecoder(client)
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ID != "test-start-1" {
		t.Errorf("ID = %q, want %q", resp.ID, "test-start-1")
	}
	if resp.Error != nil {
		t.Fatalf("Error = %+v, want nil", resp.Error)
	}
}

func TestHandleConnection_FlowStop(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)
	svc.started = time.Now()
	svc.shutdownCh = make(chan struct{})

	// Start the flow first
	startParams := json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`)
	_, err := svc.handleFlowStart(context.Background(), startParams)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// Now stop via connection
	client, server := netPipe(t)
	defer client.Close()

	go func() {
		defer server.Close()
		svc.handleConnection(context.Background(), server)
	}()

	req := Request{
		Method: "flow.stop",
		ID:     "test-stop-1",
		Params: json.RawMessage(`{"flow_id":"flow-1"}`),
	}
	encoder := json.NewEncoder(client)
	if err := encoder.Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoder := json.NewDecoder(client)
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("Error = %+v, want nil", resp.Error)
	}
}

func TestHandleConnection_FlowPause(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)
	svc.started = time.Now()
	svc.shutdownCh = make(chan struct{})

	// Start first
	_, err := svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`))
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	client, server := netPipe(t)
	defer client.Close()

	go func() {
		defer server.Close()
		svc.handleConnection(context.Background(), server)
	}()

	req := Request{
		Method: "flow.pause",
		ID:     "test-pause-1",
		Params: json.RawMessage(`{"flow_id":"flow-1"}`),
	}
	encoder := json.NewEncoder(client)
	if err := encoder.Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoder := json.NewDecoder(client)
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("Error = %+v, want nil", resp.Error)
	}
}

func TestHandleConnection_FlowStatus(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)
	svc.started = time.Now()
	svc.shutdownCh = make(chan struct{})

	client, server := netPipe(t)
	defer client.Close()

	go func() {
		defer server.Close()
		svc.handleConnection(context.Background(), server)
	}()

	req := Request{
		Method: "flow.status",
		ID:     "test-status-1",
		Params: json.RawMessage(`{"flow_id":"flow-1"}`),
	}
	encoder := json.NewEncoder(client)
	if err := encoder.Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoder := json.NewDecoder(client)
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("Error = %+v, want nil", resp.Error)
	}
}

func TestHandleConnection_MalformedJSON(t *testing.T) {
	svc := &Service{
		started:    time.Now(),
		shutdownCh: make(chan struct{}),
	}

	client, server := netPipe(t)
	defer client.Close()

	go func() {
		defer server.Close()
		svc.handleConnection(context.Background(), server)
	}()

	// Send malformed JSON
	_, _ = client.Write([]byte("{bad json\n"))

	decoder := json.NewDecoder(client)
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if resp.Error.Code != "EPARSE" {
		t.Errorf("Error.Code = %q, want %q", resp.Error.Code, "EPARSE")
	}
}

// ---------------------------------------------------------------------------
// Tests: Engine lifecycle
// ---------------------------------------------------------------------------

func TestFlowEngine_StopAllRuns(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-a", "/tmp/ws")
	seedTwoNodeFlow(store, "flow-b", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	// Start two flows
	_, _ = svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-a","workspace":"/tmp/ws"}`))
	_, _ = svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-b","workspace":"/tmp/ws"}`))

	// Wait for source nodes to complete to avoid race conditions
	time.Sleep(100 * time.Millisecond)

	// Stop all runs via shutdown method
	svc.stopAllFlowRuns()

	// Verify both are stopped (status should fall back to store)
	statusA := svc.handleFlowStatusSafe(json.RawMessage(`{"flow_id":"flow-a"}`))
	statusB := svc.handleFlowStatusSafe(json.RawMessage(`{"flow_id":"flow-b"}`))

	if statusA == nil || statusB == nil {
		t.Fatal("status should not be nil after stopAllFlowRuns")
	}
}

func TestFlowEngine_GracefulShutdown(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	_, _ = svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`))

	// Wait for source node to complete to avoid race conditions
	time.Sleep(100 * time.Millisecond)

	// Verify engine tracks the run
	if svc.flowEngine == nil {
		t.Fatal("flowEngine should not be nil")
	}

	// Stop all flow runs (simulates graceful shutdown)
	svc.stopAllFlowRuns()

	// After stopAllFlowRuns, status should come from store (stopped state)
	result, err := svc.handleFlowStatus(json.RawMessage(`{"flow_id":"flow-1"}`))
	if err != nil {
		t.Fatalf("status after shutdown: %v", err)
	}
	if result.State == "running" {
		t.Error("flow should not be running after stopAllFlowRuns")
	}
}

// ---------------------------------------------------------------------------
// Test: Request ID echo
// ---------------------------------------------------------------------------

func TestHandleConnection_RequestIDEcho(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)
	svc.started = time.Now()
	svc.shutdownCh = make(chan struct{})

	client, server := netPipe(t)
	defer client.Close()

	go func() {
		defer server.Close()
		svc.handleConnection(context.Background(), server)
	}()

	req := Request{
		Method: "flow.status",
		ID:     "my-unique-id-123",
		Params: json.RawMessage(`{"flow_id":"flow-1"}`),
	}
	encoder := json.NewEncoder(client)
	if err := encoder.Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoder := json.NewDecoder(client)
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.ID != "my-unique-id-123" {
		t.Errorf("ID = %q, want %q", resp.ID, "my-unique-id-123")
	}
}

// netPipe creates a connected pair of net.Conn using two io.Pipe pairs
// for full-duplex communication.
func netPipe(t *testing.T) (client, server net.Conn) {
	t.Helper()
	c2sR, c2sW := io.Pipe() // client→server
	s2cR, s2cW := io.Pipe() // server→client
	client = &pipeConn{r: s2cR, w: c2sW}
	server = &pipeConn{r: c2sR, w: s2cW}
	return client, server
}

// pipeConn is a simple in-memory connection using io.Pipe.
type pipeConn struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (c *pipeConn) Read(b []byte) (int, error)  { return c.r.Read(b) }
func (c *pipeConn) Write(b []byte) (int, error) { return c.w.Write(b) }
func (c *pipeConn) Close() error {
	c.w.Close()
	c.r.Close()
	return nil
}
func (c *pipeConn) LocalAddr() net.Addr                { return addr{"pipe", "local"} }
func (c *pipeConn) RemoteAddr() net.Addr               { return addr{"pipe", "remote"} }
func (c *pipeConn) SetDeadline(t time.Time) error      { return nil }
func (c *pipeConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *pipeConn) SetWriteDeadline(t time.Time) error { return nil }

type addr struct{ network, str string }

func (a addr) Network() string { return a.network }
func (a addr) String() string  { return a.str }
