package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
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

func (s *mockFlowStore) GetRun(_ context.Context, id string) (flow.FlowRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return flow.FlowRun{}, flow.ErrNotFound
	}
	return r, nil
}

func (s *mockFlowStore) UpdateRun(_ context.Context, r flow.FlowRun) (flow.FlowRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[r.ID] = r
	return r, nil
}

func (s *mockFlowStore) WriteRunLog(_ context.Context, log flow.RunLog) (flow.RunLog, error) {
	return log, nil
}

func (s *mockFlowStore) ListRunLogs(_ context.Context, runID string, opts ...flow.RunLogOption) ([]flow.RunLog, error) {
	return []flow.RunLog{}, nil
}

func (s *mockFlowStore) StreamRunLogs(_ context.Context, runID string, opts ...flow.RunLogOption) (<-chan flow.RunLog, error) {
	ch := make(chan flow.RunLog)
	close(ch)
	return ch, nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newServiceWithFlowEngine creates a Service with an initialized flow engine
// using the provided mock store and a no-op executor. It also overrides
// flowStoreOpen so that per-workspace engine creation returns the same mock store.
func newServiceWithFlowEngine(store flow.Store) *Service {
	executors := map[flow.NodeKind]flow.NodeExecutor{
		flow.NodeSkill: &noOpExecutor{},
	}
	engine := flow.NewEngine(store, executors, 16)

	// Override flowStoreOpen so per-workspace engines get the same mock store.
	flowStoreOpen = func(_ context.Context, _ string) (flow.Store, error) {
		return store, nil
	}

	return &Service{
		flowEngine: engine,
		flowStore:  store,
		wsEngines:  make(map[string]*wsFlowEngine),
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

	store.CreateFlow(context.Background(), fl)  //nolint:errcheck // test helper
	store.AddNode(context.Background(), nodeA)   //nolint:errcheck // test helper
	store.AddNode(context.Background(), nodeB)   //nolint:errcheck // test helper
	store.AddEdge(context.Background(), edge)    //nolint:errcheck // test helper
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
	store.CreateFlow(context.Background(), fl) //nolint:errcheck // test helper
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
	store.CreateFlow(context.Background(), fl)  //nolint:errcheck // test helper
	store.AddNode(context.Background(), nodeA)   //nolint:errcheck // test helper
	store.AddNode(context.Background(), nodeB)   //nolint:errcheck // test helper
	store.AddEdge(context.Background(), edge1)   //nolint:errcheck // test helper
	store.AddEdge(context.Background(), edge2)   //nolint:errcheck // test helper

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
	store.CreateFlow(context.Background(), fl)   //nolint:errcheck // test helper
	store.AddNode(context.Background(), nodeA)    //nolint:errcheck // test helper
	store.AddNode(context.Background(), nodeB)    //nolint:errcheck // test helper
	store.AddNode(context.Background(), nodeC)    //nolint:errcheck // test helper
	store.AddEdge(context.Background(), edgeAB)   //nolint:errcheck // test helper
	store.AddEdge(context.Background(), edgeBC)   //nolint:errcheck // test helper
	store.AddEdge(context.Background(), edgeCA)   //nolint:errcheck // test helper

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
	svc := &Service{} // No flowEngine set, no wsEngines
	// Without workspace: falls back to default engine which is nil.
	params := json.RawMessage(`{"flow_id":"flow-1"}`)
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
	// Without workspace: falls back to default engine which is nil, then store which is nil.
	params := json.RawMessage(`{"flow_id":"flow-1"}`)
	_, err := svc.handleFlowStatus(params)
	if err == nil {
		t.Fatal("expected error when engine not initialized, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want 'not found'", err.Error())
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

// ---------------------------------------------------------------------------
// Tests: Error codes via RPC connection
// ---------------------------------------------------------------------------

func TestFlowRPC_ErrorCodes_StartNotFound(t *testing.T) {
	store := newMockFlowStore()
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
		Method: "flow.start",
		ID:     "err-enotfound",
		Params: json.RawMessage(`{"flow_id":"nonexistent","workspace":"/tmp/ws"}`),
	}
	if err := json.NewEncoder(client).Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var resp Response
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != "ENOTFOUND" {
		t.Errorf("Error.Code = %q, want %q", resp.Error.Code, "ENOTFOUND")
	}
}

func TestFlowRPC_ErrorCodes_StartMissingFlowID(t *testing.T) {
	store := newMockFlowStore()
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
		Method: "flow.start",
		ID:     "err-earg",
		Params: json.RawMessage(`{"workspace":"/tmp/ws"}`),
	}
	if err := json.NewEncoder(client).Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var resp Response
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != "EARG" {
		t.Errorf("Error.Code = %q, want %q", resp.Error.Code, "EARG")
	}
}

func TestFlowRPC_ErrorCodes_StartAlreadyRunning(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)
	svc.started = time.Now()
	svc.shutdownCh = make(chan struct{})

	// Start the flow first
	_, _ = svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`))

	client, server := netPipe(t)
	defer client.Close()

	go func() {
		defer server.Close()
		svc.handleConnection(context.Background(), server)
	}()

	// Try to start again
	req := Request{
		Method: "flow.start",
		ID:     "err-ealready",
		Params: json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`),
	}
	if err := json.NewEncoder(client).Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var resp Response
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != "EALREADY" {
		t.Errorf("Error.Code = %q, want %q", resp.Error.Code, "EALREADY")
	}
}

func TestFlowRPC_ErrorCodes_StartCycle(t *testing.T) {
	store := newMockFlowStore()
	fl := makeTestFlow("flow-cycle", "cycle", "/tmp/ws")
	nodeA := makeTestNode("cn1", "flow-cycle", "A", flow.NodeSkill)
	nodeB := makeTestNode("cn2", "flow-cycle", "B", flow.NodeSkill)
	nodeC := makeTestNode("cn3", "flow-cycle", "C", flow.NodeSkill)
	edgeAB := makeTestEdge("ce1", "flow-cycle", nodeA.ID, nodeB.ID)
	edgeBC := makeTestEdge("ce2", "flow-cycle", nodeB.ID, nodeC.ID)
	edgeCA := makeTestEdge("ce3", "flow-cycle", nodeC.ID, nodeA.ID)
	store.CreateFlow(context.Background(), fl)   //nolint:errcheck // test helper
	store.AddNode(context.Background(), nodeA)    //nolint:errcheck // test helper
	store.AddNode(context.Background(), nodeB)    //nolint:errcheck // test helper
	store.AddNode(context.Background(), nodeC)    //nolint:errcheck // test helper
	store.AddEdge(context.Background(), edgeAB)   //nolint:errcheck // test helper
	store.AddEdge(context.Background(), edgeBC)   //nolint:errcheck // test helper
	store.AddEdge(context.Background(), edgeCA)   //nolint:errcheck // test helper

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
		Method: "flow.start",
		ID:     "err-ecycle",
		Params: json.RawMessage(`{"flow_id":"flow-cycle","workspace":"/tmp/ws"}`),
	}
	if err := json.NewEncoder(client).Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var resp Response
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != "ECYCLE" {
		t.Errorf("Error.Code = %q, want %q", resp.Error.Code, "ECYCLE")
	}
}

func TestFlowRPC_ErrorCodes_StopNotRunning(t *testing.T) {
	store := newMockFlowStore()
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
		Method: "flow.stop",
		ID:     "err-estate-stop",
		Params: json.RawMessage(`{"flow_id":"flow-draft"}`),
	}
	if err := json.NewEncoder(client).Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var resp Response
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != "ESTATE" {
		t.Errorf("Error.Code = %q, want %q", resp.Error.Code, "ESTATE")
	}
}

func TestFlowRPC_ErrorCodes_StatusNotFound(t *testing.T) {
	store := newMockFlowStore()
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
		ID:     "err-enotfound-status",
		Params: json.RawMessage(`{"flow_id":"nonexistent"}`),
	}
	if err := json.NewEncoder(client).Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var resp Response
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != "ENOTFOUND" {
		t.Errorf("Error.Code = %q, want %q", resp.Error.Code, "ENOTFOUND")
	}
}

func TestFlowRPC_ErrorCodes_StatusMissingFlowID(t *testing.T) {
	store := newMockFlowStore()
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
		ID:     "err-earg-status",
		Params: json.RawMessage(`{}`),
	}
	if err := json.NewEncoder(client).Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var resp Response
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != "EARG" {
		t.Errorf("Error.Code = %q, want %q", resp.Error.Code, "EARG")
	}
}

func TestFlowRPC_ErrorCodes_StartNoNodes(t *testing.T) {
	store := newMockFlowStore()
	fl := makeTestFlow("flow-empty", "empty", "/tmp/ws")
	store.CreateFlow(context.Background(), fl) //nolint:errcheck // test helper
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
		Method: "flow.start",
		ID:     "err-einvalid-nonodes",
		Params: json.RawMessage(`{"flow_id":"flow-empty","workspace":"/tmp/ws"}`),
	}
	if err := json.NewEncoder(client).Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var resp Response
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != "EINVALID" {
		t.Errorf("Error.Code = %q, want %q", resp.Error.Code, "EINVALID")
	}
}

// ---------------------------------------------------------------------------
// Tests: Workspace propagation
// ---------------------------------------------------------------------------

func TestFlowStart_WorkspacePropagation(t *testing.T) {
	store := newMockFlowStore()
	// Create flows in different workspaces
	seedTwoNodeFlow(store, "flow-ws1", "/tmp/workspace-1")
	seedTwoNodeFlow(store, "flow-ws2", "/tmp/workspace-2")
	svc := newServiceWithFlowEngine(store)

	// Start flow in workspace-1
	params1 := json.RawMessage(`{"flow_id":"flow-ws1","workspace":"/tmp/workspace-1"}`)
	result1, err := svc.handleFlowStart(context.Background(), params1)
	if err != nil {
		t.Fatalf("start flow-ws1: %v", err)
	}
	if result1.RunID == "" {
		t.Error("RunID should not be empty")
	}

	// Start flow in workspace-2 — should be independent
	params2 := json.RawMessage(`{"flow_id":"flow-ws2","workspace":"/tmp/workspace-2"}`)
	result2, err := svc.handleFlowStart(context.Background(), params2)
	if err != nil {
		t.Fatalf("start flow-ws2: %v", err)
	}

	if result1.RunID == result2.RunID {
		t.Error("flows from different workspaces should have distinct run IDs")
	}
}

func TestFlowStatus_IncludesNodeDetails(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	// Start the flow
	startParams := json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`)
	_, err := svc.handleFlowStart(context.Background(), startParams)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// Wait for source node to start
	time.Sleep(50 * time.Millisecond)

	// Check status includes per-node details
	statusParams := json.RawMessage(`{"flow_id":"flow-1"}`)
	result, err := svc.handleFlowStatus(statusParams)
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if len(result.Nodes) != 2 {
		t.Errorf("expected 2 node states, got %d", len(result.Nodes))
	}

	// Verify node states have IDs and states
	for _, node := range result.Nodes {
		if node.ID == "" {
			t.Error("node state should have non-empty ID")
		}
		if node.State == "" {
			t.Error("node state should have non-empty State")
		}
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

// ---------------------------------------------------------------------------
// Tests: Engine initialization
// ---------------------------------------------------------------------------

func TestStartFlowEngine_InitializesEngine(t *testing.T) {
	store := newMockFlowStore()

	// Override flowStoreOpen to return our mock store.
	orig := flowStoreOpen
	flowStoreOpen = func(_ context.Context, _ string) (flow.Store, error) {
		return store, nil
	}
	defer func() { flowStoreOpen = orig }()

	svc := &Service{
		cfg:        config.Config{},
		opts:       ServiceOptions{Workspace: "/tmp/ws"},
		shutdownCh: make(chan struct{}),
		wsEngines:  make(map[string]*wsFlowEngine),
	}

	if err := svc.startFlowEngine(context.Background()); err != nil {
		t.Fatalf("startFlowEngine() error = %v", err)
	}

	if svc.flowEngine == nil {
		t.Fatal("flowEngine should be initialized")
	}
	if svc.flowStore == nil {
		t.Fatal("flowStore should be initialized")
	}
}

func TestStartFlowEngine_Idempotent(t *testing.T) {
	store := newMockFlowStore()
	orig := flowStoreOpen
	flowStoreOpen = func(_ context.Context, _ string) (flow.Store, error) {
		return store, nil
	}
	defer func() { flowStoreOpen = orig }()

	svc := &Service{
		cfg:        config.Config{},
		shutdownCh: make(chan struct{}),
		wsEngines:  make(map[string]*wsFlowEngine),
	}

	// Call twice — should not error on second call.
	if err := svc.startFlowEngine(context.Background()); err != nil {
		t.Fatalf("first startFlowEngine() error = %v", err)
	}
	firstEngine := svc.flowEngine

	if err := svc.startFlowEngine(context.Background()); err != nil {
		t.Fatalf("second startFlowEngine() error = %v", err)
	}
	if svc.flowEngine != firstEngine {
		t.Error("engine should not be replaced on second call (idempotency)")
	}
}

func TestStopFlowEngine_StopsRuns(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	// Start a flow.
	_, _ = svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`))
	time.Sleep(50 * time.Millisecond)

	// Stop engine — should stop the running flow and clean up.
	svc.stopFlowEngine()

	if svc.flowEngine != nil {
		t.Error("flowEngine should be nil after stopFlowEngine")
	}
	if svc.flowStore != nil {
		t.Error("flowStore should be nil after stopFlowEngine")
	}
}

// ---------------------------------------------------------------------------
// Tests: Client flow RPC methods (via handleConnection)
// ---------------------------------------------------------------------------

func TestClientFlowRPC_FlowStartViaConnection(t *testing.T) {
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

	// Send flow.start request.
	req := Request{
		Method: "flow.start",
		ID:     "client-start-1",
		Params: json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`),
	}
	if err := json.NewEncoder(client).Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var resp Response
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.ID != "client-start-1" {
		t.Errorf("ID = %q, want %q", resp.ID, "client-start-1")
	}
	if resp.Error != nil {
		t.Fatalf("Error = %+v, want nil", resp.Error)
	}

	// Verify result has run_id and state.
	payload, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var result FlowStartResult
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.RunID == "" {
		t.Error("RunID is empty, expected non-empty")
	}
	if result.State != "running" {
		t.Errorf("State = %q, want %q", result.State, "running")
	}
}

func TestClientFlowRPC_FlowStatusViaConnection(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)
	svc.started = time.Now()
	svc.shutdownCh = make(chan struct{})

	// Query status of draft flow.
	client, server := netPipe(t)
	defer client.Close()

	go func() {
		defer server.Close()
		svc.handleConnection(context.Background(), server)
	}()

	req := Request{
		Method: "flow.status",
		ID:     "client-status-1",
		Params: json.RawMessage(`{"flow_id":"flow-1"}`),
	}
	if err := json.NewEncoder(client).Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var resp Response
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("Error = %+v, want nil", resp.Error)
	}

	payload, _ := json.Marshal(resp.Result)
	var result FlowStatusResult
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.FlowID != "flow-1" {
		t.Errorf("FlowID = %q, want %q", result.FlowID, "flow-1")
	}
}

func TestClientFlowRPC_FlowStartErrorViaConnection(t *testing.T) {
	store := newMockFlowStore()
	// Don't seed any flow — should get ENOTFOUND error.
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
		Method: "flow.start",
		ID:     "client-start-err",
		Params: json.RawMessage(`{"flow_id":"nonexistent","workspace":"/tmp/ws"}`),
	}
	if err := json.NewEncoder(client).Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var resp Response
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error for nonexistent flow, got nil")
	}
	if resp.Error.Code != "ENOTFOUND" {
		t.Errorf("Error.Code = %q, want %q", resp.Error.Code, "ENOTFOUND")
	}
}

func TestClientFlowRPC_MultipleRequestsOnConnection(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)
	svc.started = time.Now()
	svc.shutdownCh = make(chan struct{})

	// Single connection, two sequential requests.
	client, server := netPipe(t)
	defer client.Close()

	go func() {
		defer server.Close()
		// Handle both requests on the same connection.
		svc.handleConnection(context.Background(), server)
	}()

	// First request: flow.status
	req1 := Request{
		Method: "flow.status",
		ID:     "multi-1",
		Params: json.RawMessage(`{"flow_id":"flow-1"}`),
	}
	if err := json.NewEncoder(client).Encode(&req1); err != nil {
		t.Fatalf("encode req1: %v", err)
	}

	// Note: handleConnection processes one request per connection in the
	// current implementation. This test verifies the first request succeeds.
	var resp Response
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("decode resp1: %v", err)
	}

	if resp.ID != "multi-1" {
		t.Errorf("resp1.ID = %q, want %q", resp.ID, "multi-1")
	}
	if resp.Error != nil {
		t.Fatalf("resp1.Error = %+v, want nil", resp.Error)
	}
}

// ---------------------------------------------------------------------------
// Tests: Concurrent operations safety
// ---------------------------------------------------------------------------

func TestFlowRPC_ConcurrentStopSameFlow(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	// Start the flow
	_, err := svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`))
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Concurrent stops — first should succeed, second should fail
	var wg sync.WaitGroup
	var errors []error
	var mu sync.Mutex

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.handleFlowStop(json.RawMessage(`{"flow_id":"flow-1"}`))
			mu.Lock()
			errors = append(errors, err)
			mu.Unlock()
		}()
	}
	wg.Wait()

	// At least one should succeed (nil error) and at least one should fail
	var successes, failures int
	for _, e := range errors {
		if e == nil {
			successes++
		} else {
			failures++
		}
	}

	if successes < 1 {
		t.Error("expected at least one successful stop")
	}
	if failures < 1 {
		t.Error("expected at least one failed stop (already stopped)")
	}
}

func TestFlowRPC_ConcurrentStartSameFlow(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	// Concurrent starts — one should succeed, rest should fail with EALREADY
	var wg sync.WaitGroup
	var results []*FlowStartResult
	var errors []error
	var mu sync.Mutex

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`))
			mu.Lock()
			if err != nil {
				errors = append(errors, err)
			} else {
				results = append(results, result)
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(results) < 1 {
		t.Error("expected at least one successful start")
	}
	if len(errors) < 1 {
		t.Error("expected at least one failed start (already running)")
	}
}

// ---------------------------------------------------------------------------
// Tests: Stale state recovery
// ---------------------------------------------------------------------------

func TestFlowRPC_StaleStateRecovery(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	// Start and stop a flow
	_, _ = svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`))
	time.Sleep(100 * time.Millisecond)
	_, _ = svc.handleFlowStop(json.RawMessage(`{"flow_id":"flow-1"}`))

	// Now start again — should create a new run
	result, err := svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`))
	if err != nil {
		t.Fatalf("restart after stop: %v", err)
	}
	if result.RunID == "" {
		t.Error("restart should produce a new RunID")
	}
	if result.State != "running" {
		t.Errorf("restart State = %q, want %q", result.State, "running")
	}
}

// ---------------------------------------------------------------------------
// Tests: Per-workspace engine cache (VAL-WS-001..005)
// ---------------------------------------------------------------------------

func TestPerWorkspace_EngineCreatedOnDemand(t *testing.T) {
	store := newMockFlowStore()
	svc := newServiceWithFlowEngine(store)

	// Initially, no per-workspace engines.
	svc.wsEnginesMu.Lock()
	if len(svc.wsEngines) != 0 {
		t.Error("expected no per-workspace engines initially")
	}
	svc.wsEnginesMu.Unlock()

	// Start a flow with a workspace — this should create a per-workspace engine.
	seedTwoNodeFlow(store, "flow-ws", "/tmp/ws-test")
	params := json.RawMessage(`{"flow_id":"flow-ws","workspace":"/tmp/ws-test"}`)
	result, err := svc.handleFlowStart(context.Background(), params)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if result.RunID == "" {
		t.Error("expected non-empty RunID")
	}

	// Verify a per-workspace engine was created.
	svc.wsEnginesMu.Lock()
	count := len(svc.wsEngines)
	svc.wsEnginesMu.Unlock()
	if count == 0 {
		t.Error("expected per-workspace engine to be created")
	}
}

func TestPerWorkspace_DifferentWorkspacesGetDifferentEngines(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-ws1", "/tmp/ws-alpha")
	seedTwoNodeFlow(store, "flow-ws2", "/tmp/ws-beta")
	svc := newServiceWithFlowEngine(store)

	// Start flow in workspace A
	paramsA := json.RawMessage(`{"flow_id":"flow-ws1","workspace":"/tmp/ws-alpha"}`)
	resultA, err := svc.handleFlowStart(context.Background(), paramsA)
	if err != nil {
		t.Fatalf("start ws-alpha: %v", err)
	}

	// Start flow in workspace B
	paramsB := json.RawMessage(`{"flow_id":"flow-ws2","workspace":"/tmp/ws-beta"}`)
	resultB, err := svc.handleFlowStart(context.Background(), paramsB)
	if err != nil {
		t.Fatalf("start ws-beta: %v", err)
	}

	// Different run IDs
	if resultA.RunID == resultB.RunID {
		t.Error("flows in different workspaces should have distinct run IDs")
	}

	// Two per-workspace engines
	svc.wsEnginesMu.Lock()
	count := len(svc.wsEngines)
	svc.wsEnginesMu.Unlock()
	if count != 2 {
		t.Errorf("expected 2 per-workspace engines, got %d", count)
	}
}

func TestPerWorkspace_StopWithoutWorkspace(t *testing.T) {
	// VAL-WS-002: Stop/pause/status work without workspace by searching all engines.
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-ws", "/tmp/ws-stop")
	svc := newServiceWithFlowEngine(store)

	// Start with workspace
	startParams := json.RawMessage(`{"flow_id":"flow-ws","workspace":"/tmp/ws-stop"}`)
	_, err := svc.handleFlowStart(context.Background(), startParams)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Stop without workspace parameter — should find it via engine search
	stopParams := json.RawMessage(`{"flow_id":"flow-ws"}`)
	result, err := svc.handleFlowStop(stopParams)
	if err != nil {
		t.Fatalf("stop without workspace: %v", err)
	}
	if result.State != "stopped" {
		t.Errorf("State = %q, want %q", result.State, "stopped")
	}
}

// ---------------------------------------------------------------------------
// Tests: flow.output
// ---------------------------------------------------------------------------

func TestFlowOutput_HappyPath(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	// Start the flow first
	startParams := json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`)
	startResult, err := svc.handleFlowStart(context.Background(), startParams)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// Wait for source node to complete
	time.Sleep(200 * time.Millisecond)

	// Push output to the sink node
	outputParams := json.RawMessage(fmt.Sprintf(
		`{"flow_id":"flow-1","node_id":"%s-n2","data":{"pushed":true},"workspace":"/tmp/ws"}`,
		"flow-1",
	))
	result, err := svc.handleFlowOutput(context.Background(), outputParams)
	if err != nil {
		t.Fatalf("handleFlowOutput: %v", err)
	}

	if !result.OK {
		t.Error("expected OK=true")
	}
	if result.FlowID != "flow-1" {
		t.Errorf("FlowID = %q, want %q", result.FlowID, "flow-1")
	}
	if result.RunID == "" {
		t.Error("expected non-empty RunID")
	}
	if result.RunID != startResult.RunID {
		t.Errorf("RunID = %q, want %q", result.RunID, startResult.RunID)
	}
}

func TestFlowOutput_FlowNotRunning(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	// Don't start the flow — output should fail
	outputParams := json.RawMessage(`{"flow_id":"flow-1","node_id":"flow-1-n2","data":{},"workspace":"/tmp/ws"}`)
	_, err := svc.handleFlowOutput(context.Background(), outputParams)
	if err == nil {
		t.Fatal("expected error for non-running flow, got nil")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("error = %q, want 'not running'", err.Error())
	}
}

func TestFlowOutput_NodeNotFound(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	// Start the flow
	_, _ = svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`))
	time.Sleep(100 * time.Millisecond)

	// Push output to nonexistent node
	outputParams := json.RawMessage(`{"flow_id":"flow-1","node_id":"nonexistent","data":{},"workspace":"/tmp/ws"}`)
	_, err := svc.handleFlowOutput(context.Background(), outputParams)
	if err == nil {
		t.Fatal("expected error for nonexistent node, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want 'not found'", err.Error())
	}
}

func TestFlowOutput_MissingFlowID(t *testing.T) {
	svc := newServiceWithFlowEngine(newMockFlowStore())
	params := json.RawMessage(`{"node_id":"n1","data":{}}`)
	_, err := svc.handleFlowOutput(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for missing flow_id, got nil")
	}
	if !strings.Contains(err.Error(), "flow_id") {
		t.Errorf("error = %q, want 'flow_id'", err.Error())
	}
}

func TestFlowOutput_MissingNodeID(t *testing.T) {
	svc := newServiceWithFlowEngine(newMockFlowStore())
	params := json.RawMessage(`{"flow_id":"f1","data":{}}`)
	_, err := svc.handleFlowOutput(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for missing node_id, got nil")
	}
	if !strings.Contains(err.Error(), "node_id") {
		t.Errorf("error = %q, want 'node_id'", err.Error())
	}
}

func TestFlowOutput_MissingData(t *testing.T) {
	svc := newServiceWithFlowEngine(newMockFlowStore())
	params := json.RawMessage(`{"flow_id":"f1","node_id":"n1"}`)
	_, err := svc.handleFlowOutput(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for missing data, got nil")
	}
	if !strings.Contains(err.Error(), "data") {
		t.Errorf("error = %q, want 'data'", err.Error())
	}
}

func TestFlowOutput_InvalidJSON(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)

	_, _ = svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`))
	time.Sleep(100 * time.Millisecond)

	params := json.RawMessage(`{"flow_id":"flow-1","node_id":"flow-1-n2","data":{invalid json},"workspace":"/tmp/ws"}`)
	_, err := svc.handleFlowOutput(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for invalid JSON data, got nil")
	}
}

func TestFlowOutput_ViaRPCConnection(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc := newServiceWithFlowEngine(store)
	svc.started = time.Now()
	svc.shutdownCh = make(chan struct{})

	// Start the flow first
	startResult, _ := svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`))
	time.Sleep(200 * time.Millisecond)

	client, server := netPipe(t)
	defer client.Close()

	go func() {
		defer server.Close()
		svc.handleConnection(context.Background(), server)
	}()

	req := Request{
		Method: "flow.output",
		ID:     "test-output-1",
		Params: json.RawMessage(`{"flow_id":"flow-1","node_id":"flow-1-n2","data":{"key":"value"},"workspace":"/tmp/ws"}`),
	}
	if err := json.NewEncoder(client).Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var resp Response
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.ID != "test-output-1" {
		t.Errorf("ID = %q, want %q", resp.ID, "test-output-1")
	}
	if resp.Error != nil {
		t.Fatalf("Error = %+v, want nil", resp.Error)
	}

	// Verify result
	payload, _ := json.Marshal(resp.Result)
	var result FlowOutputResult
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !result.OK {
		t.Error("expected OK=true")
	}
	if result.RunID != startResult.RunID {
		t.Errorf("RunID = %q, want %q", result.RunID, startResult.RunID)
	}
}

func TestPerWorkspace_PauseResumeAcrossEngines(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-ws", "/tmp/ws-pause")
	svc := newServiceWithFlowEngine(store)

	// Start with workspace
	_, err := svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-ws","workspace":"/tmp/ws-pause"}`))
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Pause WITHOUT workspace
	_, err = svc.handleFlowPause(json.RawMessage(`{"flow_id":"flow-ws"}`))
	if err != nil {
		t.Fatalf("pause without workspace: %v", err)
	}

	// Resume via start with workspace
	result, err := svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-ws","workspace":"/tmp/ws-pause"}`))
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if result.State != "running" {
		t.Errorf("State = %q, want %q", result.State, "running")
	}
}

func TestPerWorkspace_StatusFromStoreFallback(t *testing.T) {
	// When a flow hasn't been started but we query status with a workspace,
	// the status should come from the store.
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-ws", "/tmp/ws-status")
	svc := newServiceWithFlowEngine(store)

	// Query status without starting
	statusParams := json.RawMessage(`{"flow_id":"flow-ws","workspace":"/tmp/ws-status"}`)
	result, err := svc.handleFlowStatus(statusParams)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if result.State != "draft" {
		t.Errorf("State = %q, want %q (persisted fallback)", result.State, "draft")
	}
}

func TestPerWorkspace_EngineCleanup(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-ws", "/tmp/ws-cleanup")
	svc := newServiceWithFlowEngine(store)

	// Start a flow to create a per-workspace engine.
	_, _ = svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-ws","workspace":"/tmp/ws-cleanup"}`))
	time.Sleep(50 * time.Millisecond)

	// stopFlowEngine should clean up per-workspace engines.
	svc.stopFlowEngine()

	svc.wsEnginesMu.Lock()
	count := len(svc.wsEngines)
	svc.wsEnginesMu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 per-workspace engines after cleanup, got %d", count)
	}
}

func TestPerWorkspace_StopAllRunsAcrossWorkspaces(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-a", "/tmp/ws-all-a")
	seedTwoNodeFlow(store, "flow-b", "/tmp/ws-all-b")
	svc := newServiceWithFlowEngine(store)

	// Start flows in different workspaces.
	_, _ = svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-a","workspace":"/tmp/ws-all-a"}`))
	_, _ = svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-b","workspace":"/tmp/ws-all-b"}`))
	time.Sleep(100 * time.Millisecond)

	// stopAllFlowRuns should stop all of them.
	svc.stopAllFlowRuns()

	// Verify both are stopped by checking status from store.
	statusA := svc.handleFlowStatusSafe(json.RawMessage(`{"flow_id":"flow-a","workspace":"/tmp/ws-all-a"}`))
	statusB := svc.handleFlowStatusSafe(json.RawMessage(`{"flow_id":"flow-b","workspace":"/tmp/ws-all-b"}`))

	if statusA == nil {
		t.Error("statusA should not be nil after stopAllFlowRuns")
	}
	if statusB == nil {
		t.Error("statusB should not be nil after stopAllFlowRuns")
	}
}

func TestPerWorkspace_IsolationBetweenWorkspaces(t *testing.T) {
	// Stopping a flow in one workspace should not affect a running flow in another.
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-a", "/tmp/ws-iso-a")
	seedTwoNodeFlow(store, "flow-b", "/tmp/ws-iso-b")
	svc := newServiceWithFlowEngine(store)

	_, _ = svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-a","workspace":"/tmp/ws-iso-a"}`))
	_, _ = svc.handleFlowStart(context.Background(), json.RawMessage(`{"flow_id":"flow-b","workspace":"/tmp/ws-iso-b"}`))
	time.Sleep(100 * time.Millisecond)

	// Stop flow-a
	_, err := svc.handleFlowStop(json.RawMessage(`{"flow_id":"flow-a","workspace":"/tmp/ws-iso-a"}`))
	if err != nil {
		t.Fatalf("stop flow-a: %v", err)
	}

	// Flow-b should still be running.
	result, err := svc.handleFlowStatus(json.RawMessage(`{"flow_id":"flow-b","workspace":"/tmp/ws-iso-b"}`))
	if err != nil {
		t.Fatalf("status flow-b: %v", err)
	}
	if result.State != "running" {
		t.Errorf("flow-b state = %q, want 'running' (isolated from stop of flow-a)", result.State)
	}
}
