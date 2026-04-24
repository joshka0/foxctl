package runtime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// ErrRunAlreadyExists indicates run creation conflicts.
	ErrRunAlreadyExists = errors.New("rlm runtime: run already exists")
	// ErrRunNotFound indicates a missing run record.
	ErrRunNotFound = errors.New("rlm runtime: run not found")
	// ErrNodeAlreadyExists indicates node creation conflicts.
	ErrNodeAlreadyExists = errors.New("rlm runtime: node already exists")
	// ErrNodeNotFound indicates a missing node record.
	ErrNodeNotFound = errors.New("rlm runtime: node not found")
	// ErrInvalidRun indicates malformed run input.
	ErrInvalidRun = errors.New("rlm runtime: invalid run")
	// ErrInvalidNode indicates malformed node input.
	ErrInvalidNode = errors.New("rlm runtime: invalid node")
)

// NodeStore is the persistence boundary for recursive RLM run and node state.
type NodeStore interface {
	CreateRun(ctx context.Context, run Run) (Run, error)
	GetRun(ctx context.Context, runID string) (Run, error)
	CreateNode(ctx context.Context, node Node) (Node, error)
	GetNode(ctx context.Context, runID, nodeID string) (Node, error)
	ListNodes(ctx context.Context, runID string) ([]Node, error)
	ListChildren(ctx context.Context, runID, parentNodeID string) ([]Node, error)
	UpdateNodeStatus(ctx context.Context, runID, nodeID string, status NodeStatus) (Node, error)
	SetNodeResult(ctx context.Context, runID, nodeID string, result NodeResult) (Node, error)
}

// MemoryNodeStoreOption configures an in-memory node store.
type MemoryNodeStoreOption func(*MemoryNodeStore)

// WithMemoryNodeStoreNow injects a deterministic clock for tests.
func WithMemoryNodeStoreNow(now func() time.Time) MemoryNodeStoreOption {
	return func(store *MemoryNodeStore) {
		if now != nil {
			store.now = now
		}
	}
}

// MemoryNodeStore is an in-memory NodeStore with immutable snapshot semantics.
type MemoryNodeStore struct {
	mu    sync.RWMutex
	now   func() time.Time
	runs  map[string]Run
	nodes map[nodeStoreKey]Node
}

type nodeStoreKey struct {
	runID  string
	nodeID string
}

var _ NodeStore = (*MemoryNodeStore)(nil)

// NewMemoryNodeStore creates a new in-memory node store.
func NewMemoryNodeStore(options ...MemoryNodeStoreOption) *MemoryNodeStore {
	store := &MemoryNodeStore{
		now:   time.Now,
		runs:  make(map[string]Run),
		nodes: make(map[nodeStoreKey]Node),
	}
	for _, option := range options {
		option(store)
	}
	return store
}

// CreateRun stores one run record.
func (store *MemoryNodeStore) CreateRun(ctx context.Context, run Run) (Run, error) {
	if err := checkStoreContext(ctx); err != nil {
		return Run{}, err
	}

	runID := strings.TrimSpace(run.ID)
	if runID == "" {
		return Run{}, fmt.Errorf("%w: empty id", ErrInvalidRun)
	}

	stored := cloneRun(run)
	stored.ID = runID
	if stored.Status == "" {
		stored.Status = NodeStatusQueued
	}
	if !stored.Status.IsValid() {
		return Run{}, fmt.Errorf("%w: status=%q", ErrInvalidNodeStatus, stored.Status)
	}

	now := store.now().UTC()
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = now
	}
	if stored.UpdatedAt.IsZero() {
		stored.UpdatedAt = stored.CreatedAt
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.runs[runID]; exists {
		return Run{}, fmt.Errorf("%w: %q", ErrRunAlreadyExists, runID)
	}
	store.runs[runID] = stored
	return cloneRun(stored), nil
}

// GetRun returns one run snapshot by ID.
func (store *MemoryNodeStore) GetRun(ctx context.Context, runID string) (Run, error) {
	if err := checkStoreContext(ctx); err != nil {
		return Run{}, err
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return Run{}, fmt.Errorf("%w: empty id", ErrInvalidRun)
	}

	store.mu.RLock()
	defer store.mu.RUnlock()
	run, exists := store.runs[runID]
	if !exists {
		return Run{}, fmt.Errorf("%w: %q", ErrRunNotFound, runID)
	}
	return cloneRun(run), nil
}

// CreateNode stores one node record.
func (store *MemoryNodeStore) CreateNode(ctx context.Context, node Node) (Node, error) {
	if err := checkStoreContext(ctx); err != nil {
		return Node{}, err
	}
	runID := strings.TrimSpace(node.RunID)
	if runID == "" {
		return Node{}, fmt.Errorf("%w: empty run id", ErrInvalidNode)
	}
	nodeID := strings.TrimSpace(node.ID)
	if nodeID == "" {
		return Node{}, fmt.Errorf("%w: empty node id", ErrInvalidNode)
	}

	stored := cloneNode(node)
	stored.RunID = runID
	stored.ID = nodeID
	if stored.ParentNodeID == stored.ID {
		return Node{}, fmt.Errorf("%w: node cannot parent itself", ErrInvalidNode)
	}
	if stored.Status == "" {
		stored.Status = NodeStatusQueued
	}
	if !stored.Status.IsValid() {
		return Node{}, fmt.Errorf("%w: status=%q", ErrInvalidNodeStatus, stored.Status)
	}
	if stored.Result != nil {
		if stored.Result.Status == "" {
			stored.Result.Status = stored.Status
		}
		if !stored.Result.Status.IsValid() {
			return Node{}, fmt.Errorf("%w: result status=%q", ErrInvalidNodeStatus, stored.Result.Status)
		}
		if stored.Result.Status != stored.Status {
			return Node{}, fmt.Errorf("%w: result status=%q does not match node status=%q", ErrInvalidNode, stored.Result.Status, stored.Status)
		}
	}

	now := store.now().UTC()
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = now
	}
	if stored.UpdatedAt.IsZero() {
		stored.UpdatedAt = stored.CreatedAt
	}
	if stored.Status == NodeStatusRunning && stored.StartedAt.IsZero() {
		stored.StartedAt = stored.UpdatedAt
	}
	if stored.Status.IsTerminal() && stored.FinishedAt.IsZero() {
		stored.FinishedAt = stored.UpdatedAt
	}

	key := nodeStoreKey{runID: runID, nodeID: nodeID}

	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.runs[runID]; !exists {
		return Node{}, fmt.Errorf("%w: %q", ErrRunNotFound, runID)
	}
	if _, exists := store.nodes[key]; exists {
		return Node{}, fmt.Errorf("%w: run=%q node=%q", ErrNodeAlreadyExists, runID, nodeID)
	}
	store.nodes[key] = stored
	return cloneNode(stored), nil
}

// GetNode returns one node snapshot by run and node ID.
func (store *MemoryNodeStore) GetNode(ctx context.Context, runID, nodeID string) (Node, error) {
	if err := checkStoreContext(ctx); err != nil {
		return Node{}, err
	}
	key, err := buildNodeStoreKey(runID, nodeID)
	if err != nil {
		return Node{}, err
	}

	store.mu.RLock()
	defer store.mu.RUnlock()
	node, exists := store.nodes[key]
	if !exists {
		return Node{}, fmt.Errorf("%w: run=%q node=%q", ErrNodeNotFound, key.runID, key.nodeID)
	}
	return cloneNode(node), nil
}

// ListNodes returns all node snapshots for one run, ordered by node ID.
func (store *MemoryNodeStore) ListNodes(ctx context.Context, runID string) ([]Node, error) {
	if err := checkStoreContext(ctx); err != nil {
		return nil, err
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("%w: empty id", ErrInvalidRun)
	}

	store.mu.RLock()
	defer store.mu.RUnlock()
	if _, exists := store.runs[runID]; !exists {
		return nil, fmt.Errorf("%w: %q", ErrRunNotFound, runID)
	}

	out := make([]Node, 0)
	for key, node := range store.nodes {
		if key.runID == runID {
			out = append(out, cloneNode(node))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// ListChildren returns all direct child node snapshots for one parent node.
func (store *MemoryNodeStore) ListChildren(ctx context.Context, runID, parentNodeID string) ([]Node, error) {
	if err := checkStoreContext(ctx); err != nil {
		return nil, err
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("%w: empty id", ErrInvalidRun)
	}
	parentNodeID = strings.TrimSpace(parentNodeID)

	store.mu.RLock()
	defer store.mu.RUnlock()
	if _, exists := store.runs[runID]; !exists {
		return nil, fmt.Errorf("%w: %q", ErrRunNotFound, runID)
	}

	out := make([]Node, 0)
	for key, node := range store.nodes {
		if key.runID == runID && node.ParentNodeID == parentNodeID {
			out = append(out, cloneNode(node))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// UpdateNodeStatus updates one node status after transition validation.
func (store *MemoryNodeStore) UpdateNodeStatus(ctx context.Context, runID, nodeID string, status NodeStatus) (Node, error) {
	if err := checkStoreContext(ctx); err != nil {
		return Node{}, err
	}
	key, err := buildNodeStoreKey(runID, nodeID)
	if err != nil {
		return Node{}, err
	}
	if !status.IsValid() {
		return Node{}, fmt.Errorf("%w: status=%q", ErrInvalidNodeStatus, status)
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	stored, exists := store.nodes[key]
	if !exists {
		return Node{}, fmt.Errorf("%w: run=%q node=%q", ErrNodeNotFound, key.runID, key.nodeID)
	}

	transitioned, err := ApplyNodeStatusTransition(stored, status, store.now().UTC())
	if err != nil {
		return Node{}, err
	}
	if transitioned.Result != nil {
		result := cloneNodeResult(*transitioned.Result)
		result.Status = transitioned.Status
		if result.StartedAt.IsZero() && !transitioned.StartedAt.IsZero() {
			result.StartedAt = transitioned.StartedAt
		}
		if transitioned.Status.IsTerminal() && result.CompletedAt.IsZero() && !transitioned.FinishedAt.IsZero() {
			result.CompletedAt = transitioned.FinishedAt
		}
		transitioned.Result = &result
	}
	store.nodes[key] = transitioned
	return cloneNode(transitioned), nil
}

// SetNodeResult stores one node result snapshot and reconciles status.
func (store *MemoryNodeStore) SetNodeResult(ctx context.Context, runID, nodeID string, result NodeResult) (Node, error) {
	if err := checkStoreContext(ctx); err != nil {
		return Node{}, err
	}
	key, err := buildNodeStoreKey(runID, nodeID)
	if err != nil {
		return Node{}, err
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	stored, exists := store.nodes[key]
	if !exists {
		return Node{}, fmt.Errorf("%w: run=%q node=%q", ErrNodeNotFound, key.runID, key.nodeID)
	}

	storedResult := cloneNodeResult(result)
	if storedResult.Status == "" {
		storedResult.Status = stored.Status
	}
	if !storedResult.Status.IsValid() {
		return Node{}, fmt.Errorf("%w: status=%q", ErrInvalidNodeStatus, storedResult.Status)
	}

	now := store.now().UTC()
	updated, err := ApplyNodeStatusTransition(stored, storedResult.Status, now)
	if err != nil {
		return Node{}, err
	}
	if storedResult.StartedAt.IsZero() && !updated.StartedAt.IsZero() {
		storedResult.StartedAt = updated.StartedAt
	}
	if updated.Status.IsTerminal() {
		if updated.FinishedAt.IsZero() {
			updated.FinishedAt = now
		}
		if storedResult.CompletedAt.IsZero() {
			storedResult.CompletedAt = updated.FinishedAt
		}
	}
	updated.Result = &storedResult
	updated.UpdatedAt = now

	store.nodes[key] = updated
	return cloneNode(updated), nil
}

func buildNodeStoreKey(runID, nodeID string) (nodeStoreKey, error) {
	runID = strings.TrimSpace(runID)
	nodeID = strings.TrimSpace(nodeID)
	if runID == "" {
		return nodeStoreKey{}, fmt.Errorf("%w: empty run id", ErrInvalidNode)
	}
	if nodeID == "" {
		return nodeStoreKey{}, fmt.Errorf("%w: empty node id", ErrInvalidNode)
	}
	return nodeStoreKey{runID: runID, nodeID: nodeID}, nil
}

func checkStoreContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func cloneRun(in Run) Run {
	out := in
	out.Metadata = cloneMapAny(in.Metadata)
	return out
}

func cloneNode(in Node) Node {
	out := in
	if in.Result != nil {
		result := cloneNodeResult(*in.Result)
		out.Result = &result
	}
	out.Metadata = cloneMapAny(in.Metadata)
	return out
}

func cloneNodeResult(in NodeResult) NodeResult {
	out := in
	if len(in.Findings) > 0 {
		out.Findings = make([]Finding, len(in.Findings))
		for i := range in.Findings {
			out.Findings[i] = cloneFinding(in.Findings[i])
		}
	}
	out.EvidenceRefs = cloneEvidenceRefs(in.EvidenceRefs)
	out.ArtifactRefs = cloneArtifactRefs(in.ArtifactRefs)
	out.Metadata = cloneMapAny(in.Metadata)
	return out
}

func cloneFinding(in Finding) Finding {
	out := in
	out.EvidenceRefs = cloneEvidenceRefs(in.EvidenceRefs)
	out.ArtifactRefs = cloneArtifactRefs(in.ArtifactRefs)
	out.Metadata = cloneMapAny(in.Metadata)
	return out
}

func cloneEvidenceRefs(in []EvidenceRef) []EvidenceRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]EvidenceRef, len(in))
	copy(out, in)
	return out
}

func cloneArtifactRefs(in []ArtifactRef) []ArtifactRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]ArtifactRef, len(in))
	copy(out, in)
	return out
}

func cloneMapAny(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneAny(value)
	}
	return out
}

func cloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMapAny(typed)
	case map[string]string:
		out := make(map[string]string, len(typed))
		for key, item := range typed {
			out[key] = item
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = cloneAny(typed[i])
		}
		return out
	case []string:
		out := make([]string, len(typed))
		copy(out, typed)
		return out
	case []int:
		out := make([]int, len(typed))
		copy(out, typed)
		return out
	case []int64:
		out := make([]int64, len(typed))
		copy(out, typed)
		return out
	case []float64:
		out := make([]float64, len(typed))
		copy(out, typed)
		return out
	case []bool:
		out := make([]bool, len(typed))
		copy(out, typed)
		return out
	default:
		return value
	}
}
