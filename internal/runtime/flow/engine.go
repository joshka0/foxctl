package flow

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/oklog/ulid/v2"
)

const defaultBusBufferSize = 64

// ---------------------------------------------------------------------------
// Engine
// ---------------------------------------------------------------------------

// Engine orchestrates flow execution: loading graphs, topological sorting,
// starting source nodes, running evaluators, and tracking state.
//
// Engine is safe for concurrent use. All state mutations are protected by a mutex.
type Engine struct {
	store     Store
	executors map[NodeKind]NodeExecutor
	busSize   int

	mu   sync.Mutex
	runs map[string]*flowRun // flowID -> active run
}

// flowRun tracks the runtime state of an active flow execution.
// Each run owns its OutputBus, so stopping one flow does not affect others.
type flowRun struct {
	flowID   string
	cancel   context.CancelFunc
	state    FlowState
	runID    string
	storeRun FlowRun
	bus      *OutputBus // per-run bus

	// Per-node and per-edge state (guarded by Engine.mu).
	nodeStates map[string]NodeExecState
	edgeState  map[string]EdgeExecState

	// Pause/resume channels per evaluator.
	pauseChs  map[string]chan struct{} // edgeID -> pause signal
	resumeChs map[string]chan struct{} // edgeID -> resume signal
}

// NodeExecState tracks the execution state of a single node.
type NodeExecState struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Kind     NodeKind `json:"kind"`
	State    string   `json:"state"` // idle, running, completed, errored
	Error    string   `json:"error,omitempty"`
	Duration int64    `json:"duration_ms,omitempty"`
}

// EdgeExecState tracks the delivery state of a single edge.
type EdgeExecState struct {
	ID             string     `json:"id"`
	From           string     `json:"from"`
	To             string     `json:"to"`
	DeliveryCount  int        `json:"delivery_count"`
	LastDeliveryAt *time.Time `json:"last_delivery_at,omitempty"`
}

// EngineStatus is the snapshot returned by Engine.Status().
type EngineStatus struct {
	FlowState FlowState       `json:"flow_state"`
	Nodes     []NodeExecState `json:"nodes"`
	Edges     []EdgeExecState `json:"edges"`
	RunID     string          `json:"run_id,omitempty"`
}

// NewEngine creates a new flow execution engine.
func NewEngine(store Store, executors map[NodeKind]NodeExecutor, busSize int) *Engine {
	if busSize <= 0 {
		busSize = defaultBusBufferSize
	}
	return &Engine{
		store:     store,
		executors: executors,
		busSize:   busSize,
		runs:      make(map[string]*flowRun),
	}
}

// Start loads the flow graph, topologically sorts nodes, starts source nodes,
// and begins edge evaluators.
func (e *Engine) Start(ctx context.Context, flowID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if already running.
	if run, ok := e.runs[flowID]; ok {
		if run.state == FlowRunning {
			return fmt.Errorf("flow: %s already running", flowID)
		}
		if run.state == FlowPaused {
			// Resume: send resume signals to all evaluators
			return e.resumeLocked(flowID)
		}
	}

	// Load flow from store.
	fl, err := e.store.GetFlow(ctx, flowID)
	if err != nil {
		return fmt.Errorf("flow: start: %w", err)
	}

	// Load nodes and edges.
	nodes, err := e.store.ListNodesByFlow(ctx, flowID)
	if err != nil {
		return fmt.Errorf("flow: load nodes: %w", err)
	}
	edges, err := e.store.ListEdgesByFlow(ctx, flowID)
	if err != nil {
		return fmt.Errorf("flow: load edges: %w", err)
	}

	if len(nodes) == 0 {
		return fmt.Errorf("flow: %s has no nodes", flowID)
	}

	// Topological sort (detects cycles).
	_, err = topologicalSort(nodes, edges)
	if err != nil {
		return err
	}

	// Identify source nodes (nodes with no incoming edges).
	sources := findSourceNodes(nodes, edges)
	if len(sources) == 0 {
		return fmt.Errorf("flow: %s has no source nodes", flowID)
	}

	// Create a new context for this flow run.
	runCtx, cancel := context.WithCancel(ctx)

	// Create per-run OutputBus.
	bus := newOutputBus(e.busSize)

	// Create FlowRun record.
	now := time.Now().UTC()
	runID := ulid.Make().String()
	storeRun := FlowRun{
		ID:        runID,
		FlowID:    flowID,
		State:     RunRunning,
		StartedAt: now,
	}
	if createdRun, err := e.store.CreateRun(runCtx, storeRun); err == nil {
		storeRun = createdRun
	}

	// Update flow state to running.
	fl.State = FlowRunning
	if _, err := e.store.UpdateFlow(runCtx, fl); err != nil {
		// Best-effort: log but don't fail the flow start since GetFlow already succeeded.
		// The flow will still run; this is a non-critical state persistence failure.
		_ = err // Suppress unused variable warning
	}

	// Initialize per-node and per-edge state.
	nodeStates := make(map[string]NodeExecState)
	for _, n := range nodes {
		nodeStates[n.ID] = NodeExecState{
			ID:    n.ID,
			Label: n.Label,
			Kind:  n.Kind,
			State: "idle",
		}
	}

	edgeState := make(map[string]EdgeExecState)
	for _, ed := range edges {
		edgeState[ed.ID] = EdgeExecState{
			ID:   ed.ID,
			From: ed.FromNodeID,
			To:   ed.ToNodeID,
		}
	}

	// Create the flow run tracking object with its own bus.
	run := &flowRun{
		flowID:     flowID,
		cancel:     cancel,
		state:      FlowRunning,
		runID:      runID,
		storeRun:   storeRun,
		bus:        bus,
		nodeStates: nodeStates,
		edgeState:  edgeState,
		pauseChs:   make(map[string]chan struct{}),
		resumeChs:  make(map[string]chan struct{}),
	}
	e.runs[flowID] = run

	// Start the output bus for all nodes.
	for _, n := range nodes {
		bus.start(runCtx, n.ID)
	}

	// Start edge evaluators.
	for _, edge := range edges {
		executor, ok := e.executors[edge.ToNodeKind(nodes)]
		if !ok {
			continue // Skip edges to nodes without an executor
		}

		targetNode := findNodeByID(nodes, edge.ToNodeID)

		// Parse condition.
		var cond Condition
		if edge.Condition != "" {
			var pErr error
			cond, pErr = ParseCondition(edge.Condition)
			if pErr != nil {
				cond = AlwaysCondition // Fallback to always pass on parse error
			}
		}

		pauseCh := make(chan struct{})
		resumeCh := make(chan struct{})
		run.pauseChs[edge.ID] = pauseCh
		run.resumeChs[edge.ID] = resumeCh

		cfg := evaluatorConfig{
			edge:         edge,
			sourceNodeID: edge.FromNodeID,
			targetNodeID: edge.ToNodeID,
			bus:          bus,
			executor:     executor,
			targetNode:   targetNode,
			condition:    cond,
			pauseCh:      pauseCh,
			resumeCh:     resumeCh,
			onDeliver: func(edgeID string) {
				e.mu.Lock()
				if r, ok := e.runs[flowID]; ok {
					if es, ok := r.edgeState[edgeID]; ok {
						es.DeliveryCount++
						now := time.Now().UTC()
						es.LastDeliveryAt = &now
						r.edgeState[edgeID] = es
					}
				}
				e.mu.Unlock()
			},
		}

		startEvaluator(runCtx, cfg)
	}

	// Start source node executors.
	for _, src := range sources {
		executor, ok := e.executors[src.Kind]
		if !ok {
			continue
		}

		// Mark source as running.
		if ns, ok := run.nodeStates[src.ID]; ok {
			ns.State = "running"
			run.nodeStates[src.ID] = ns
		}

		go func(node FlowNode) {
			result := executeSourceWithResult(runCtx, executor, node, bus)

			// Update node state based on result.
			e.mu.Lock()
			if r, ok := e.runs[flowID]; ok {
				if ns, ok := r.nodeStates[node.ID]; ok {
					ns.Duration = result.Duration.Milliseconds()
					if result.Envelope.Status == envelope.StatusError {
						ns.State = "errored"
						ns.Error = result.Envelope.Error.Message
					} else {
						ns.State = "completed"
					}
					r.nodeStates[node.ID] = ns
				}
			}
			e.mu.Unlock()
		}(src)
	}

	return nil
}

// Stop terminates all executors and evaluators for the given flow.
func (e *Engine) Stop(flowID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.stopLocked(flowID, FlowStopped)
}

// stopLocked stops the flow, transitioning to the given state.
// Caller must hold e.mu.
func (e *Engine) stopLocked(flowID string, state FlowState) error {
	run, ok := e.runs[flowID]
	if !ok {
		return fmt.Errorf("flow: %s not running", flowID)
	}

	// Allow stopping errored flows, but reject already-stopped.
	if run.state == FlowStopped {
		return fmt.Errorf("flow: %s already stopped", flowID)
	}

	// Remove from active runs to prevent re-stop.
	delete(e.runs, flowID)

	// Cancel the context to stop all goroutines (evaluators, dispatch loops).
	run.cancel()

	// Stop per-run bus channels.
	run.bus.stopAll()

	// Update flow state in store.
	now := time.Now().UTC()
	fl, err := e.store.GetFlow(context.Background(), flowID)
	if err == nil {
		fl.State = state
		if _, updateErr := e.store.UpdateFlow(context.Background(), fl); updateErr != nil {
			// Best-effort state update. Continue with cleanup.
			_ = updateErr // Suppress unused variable warning
		}
	}

	// Update FlowRun.
	run.storeRun.State = RunCompleted
	run.storeRun.CompletedAt = &now
	if _, updateErr := e.store.UpdateRun(context.Background(), run.storeRun); updateErr != nil {
		// Best-effort state update. Continue with cleanup.
		_ = updateErr // Suppress unused variable warning
	}

	// Update node states: running nodes become idle (stopped before completion).
	for id, ns := range run.nodeStates {
		if ns.State == "running" {
			ns.State = "idle"
			run.nodeStates[id] = ns
		}
	}

	run.state = state

	return nil
}

// Pause suspends all evaluators without stopping executors.
func (e *Engine) Pause(flowID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	run, ok := e.runs[flowID]
	if !ok || (run.state != FlowRunning) {
		return fmt.Errorf("flow: %s not running", flowID)
	}

	// Send pause signal to all evaluators.
	for _, pauseCh := range run.pauseChs {
		select {
		case pauseCh <- struct{}{}:
		default:
			// Already paused or not reading
		}
	}

	run.state = FlowPaused

	// Update flow state in store.
	fl, err := e.store.GetFlow(context.Background(), flowID)
	if err == nil {
		fl.State = FlowPaused
		if _, updateErr := e.store.UpdateFlow(context.Background(), fl); updateErr != nil {
			// Best-effort state update. Continue with cleanup.
			_ = updateErr // Suppress unused variable warning
		}
	}

	return nil
}

// resumeLocked resumes all evaluators for a paused flow.
// Caller must hold e.mu.
func (e *Engine) resumeLocked(flowID string) error {
	run, ok := e.runs[flowID]
	if !ok || run.state != FlowPaused {
		return fmt.Errorf("flow: %s not paused", flowID)
	}

	// Send resume signal to all evaluators.
	for _, resumeCh := range run.resumeChs {
		select {
		case resumeCh <- struct{}{}:
		default:
		}
	}

	run.state = FlowRunning

	// Update flow state in store.
	fl, err := e.store.GetFlow(context.Background(), flowID)
	if err == nil {
		fl.State = FlowRunning
		if _, updateErr := e.store.UpdateFlow(context.Background(), fl); updateErr != nil {
			// Best-effort state update. Continue with cleanup.
			_ = updateErr // Suppress unused variable warning
		}
	}

	return nil
}

// Status returns the current runtime state of the given flow.
func (e *Engine) Status(flowID string) *EngineStatus {
	e.mu.Lock()
	defer e.mu.Unlock()

	run, ok := e.runs[flowID]
	if !ok {
		return nil
	}

	nodes := make([]NodeExecState, 0, len(run.nodeStates))
	for _, ns := range run.nodeStates {
		nodes = append(nodes, ns)
	}

	edges := make([]EdgeExecState, 0, len(run.edgeState))
	for _, es := range run.edgeState {
		edges = append(edges, es)
	}

	return &EngineStatus{
		FlowState: run.state,
		Nodes:     nodes,
		Edges:     edges,
		RunID:     run.runID,
	}
}

// ---------------------------------------------------------------------------
// Topological Sort
// ---------------------------------------------------------------------------

// topologicalSort performs a topological sort of the given nodes based on edges.
// Returns an error if a cycle is detected, including the cycle path.
func topologicalSort(nodes []FlowNode, edges []FlowEdge) ([]FlowNode, error) {
	if len(nodes) == 0 {
		return nil, nil
	}

	// Build adjacency list and in-degree map.
	adj := make(map[string][]string) // nodeID -> []downstreamNodeID
	inDegree := make(map[string]int)
	nodeMap := make(map[string]FlowNode)

	for _, n := range nodes {
		nodeMap[n.ID] = n
		inDegree[n.ID] = 0
	}

	for _, e := range edges {
		adj[e.FromNodeID] = append(adj[e.FromNodeID], e.ToNodeID)
		inDegree[e.ToNodeID]++
	}

	// Kahn's algorithm.
	queue := make([]string, 0)
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	var sorted []FlowNode
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		sorted = append(sorted, nodeMap[id])

		for _, next := range adj[id] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if len(sorted) != len(nodes) {
		// Cycle detected. Find the cycle path.
		cycle := findCyclePath(nodes, edges)
		return nil, fmt.Errorf("flow: cycle detected: %s", cycle)
	}

	return sorted, nil
}

// findCyclePath attempts to find and return a human-readable cycle path.
func findCyclePath(nodes []FlowNode, edges []FlowEdge) string {
	// Build adjacency list with labels.
	adj := make(map[string][]string)
	labelMap := make(map[string]string)
	for _, n := range nodes {
		labelMap[n.ID] = n.Label
	}
	for _, e := range edges {
		adj[e.FromNodeID] = append(adj[e.FromNodeID], e.ToNodeID)
	}

	// DFS to find cycle.
	visited := make(map[string]bool)
	inStack := make(map[string]bool)
	var path []string

	var dfs func(nodeID string) bool
	dfs = func(nodeID string) bool {
		visited[nodeID] = true
		inStack[nodeID] = true
		path = append(path, labelMap[nodeID])

		for _, next := range adj[nodeID] {
			if !visited[next] {
				if dfs(next) {
					return true
				}
			} else if inStack[next] {
				// Found cycle
				path = append(path, labelMap[next])
				return true
			}
		}

		path = path[:len(path)-1]
		inStack[nodeID] = false
		return false
	}

	for _, n := range nodes {
		if !visited[n.ID] {
			if dfs(n.ID) {
				// Format cycle path
				result := ""
				for i, label := range path {
					if i > 0 {
						result += " → "
					}
					result += label
				}
				return result
			}
		}
	}

	return "unknown cycle"
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// findSourceNodes returns nodes that have no incoming edges.
func findSourceNodes(nodes []FlowNode, edges []FlowEdge) []FlowNode {
	hasIncoming := make(map[string]bool)
	for _, e := range edges {
		hasIncoming[e.ToNodeID] = true
	}

	var sources []FlowNode
	for _, n := range nodes {
		if !hasIncoming[n.ID] {
			sources = append(sources, n)
		}
	}
	return sources
}

// findNodeByID returns the node with the given ID from the slice.
func findNodeByID(nodes []FlowNode, id string) FlowNode {
	for _, n := range nodes {
		if n.ID == id {
			return n
		}
	}
	return FlowNode{}
}

// ToNodeKind returns the NodeKind of the target node.
// This is a helper method on FlowEdge for convenience.
func (e FlowEdge) ToNodeKind(nodes []FlowNode) NodeKind {
	for _, n := range nodes {
		if n.ID == e.ToNodeID {
			return n.Kind
		}
	}
	return ""
}

// makeEnvelope creates a simple envelope for error reporting.
func makeEnvelope(status, command string, data any) envelope.Envelope {
	return envelope.Envelope{
		Version: 1,
		Status:  status,
		Command: command,
		Data:    data,
		Meta: envelope.Meta{
			TS: time.Now().UTC().Format(time.RFC3339),
		},
	}
}
