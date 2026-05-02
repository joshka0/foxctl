package flow

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotFound is returned when a flow, node, edge, or run is not found.
var ErrNotFound = errors.New("flow: not found")

// MaxFlowNameLen is the maximum allowed length for a flow name.
// Names exceeding this limit are rejected with ErrNameTooLong.
const MaxFlowNameLen = 1024

// ErrNameTooLong is returned when a flow name exceeds MaxFlowNameLen.
var ErrNameTooLong = fmt.Errorf("flow: name exceeds maximum length of %d characters", MaxFlowNameLen)

// Store defines the persistence interface for flow entities.
//
// Implementations must be safe for concurrent use. All methods accept a context
// for cancellation and timeout propagation.
type Store interface {
	// Close releases all resources held by the store.
	Close() error

	// ---- Flow CRUD ----

	// CreateFlow stores a new flow. The ID, CreatedAt, and UpdatedAt fields
	// should be set by the caller before calling CreateFlow.
	CreateFlow(ctx context.Context, f Flow) (Flow, error)

	// GetFlow returns the flow with the given ID.
	// Returns ErrNotFound if no flow exists with that ID.
	GetFlow(ctx context.Context, id string) (Flow, error)

	// GetFlowByName returns the flow with the given name in the given workspace.
	// Returns ErrNotFound if no flow exists with that name in that workspace.
	GetFlowByName(ctx context.Context, workspace, name string) (Flow, error)

	// ListFlows returns all flows in the given workspace.
	// Returns an empty slice (not nil) when there are no flows.
	ListFlows(ctx context.Context, workspace string) ([]Flow, error)

	// UpdateFlow updates the mutable fields of a flow (state, description,
	// updated_at). The UpdatedAt field is set to time.Now().UTC().
	UpdateFlow(ctx context.Context, f Flow) (Flow, error)

	// DeleteFlow removes the flow and all associated nodes, edges, and runs
	// (cascade delete). Returns ErrNotFound if the flow does not exist.
	DeleteFlow(ctx context.Context, id string) error

	// ---- Node CRUD ----

	// AddNode stores a new node in the given flow.
	AddNode(ctx context.Context, n FlowNode) (FlowNode, error)

	// GetNode returns the node with the given ID.
	// Returns ErrNotFound if no node exists with that ID.
	GetNode(ctx context.Context, id string) (FlowNode, error)

	// RemoveNode removes the node and all edges connected to it (cascade
	// delete). Returns ErrNotFound if the node does not exist.
	RemoveNode(ctx context.Context, id string) error

	// ListNodesByFlow returns all nodes belonging to the given flow.
	// Returns an empty slice (not nil) when there are no nodes.
	ListNodesByFlow(ctx context.Context, flowID string) ([]FlowNode, error)

	// ---- Edge CRUD ----

	// AddEdge stores a new edge in the given flow.
	AddEdge(ctx context.Context, e FlowEdge) (FlowEdge, error)

	// GetEdge returns the edge with the given ID.
	// Returns ErrNotFound if no edge exists with that ID.
	GetEdge(ctx context.Context, id string) (FlowEdge, error)

	// RemoveEdge removes the edge with the given ID.
	// Returns ErrNotFound if the edge does not exist.
	RemoveEdge(ctx context.Context, id string) error

	// ListEdgesByFlow returns all edges belonging to the given flow.
	// Returns an empty slice (not nil) when there are no edges.
	ListEdgesByFlow(ctx context.Context, flowID string) ([]FlowEdge, error)

	// ---- Run CRUD ----

	// CreateRun stores a new flow run.
	CreateRun(ctx context.Context, r FlowRun) (FlowRun, error)

	// UpdateRun updates the mutable fields of a run (state, completed_at,
	// error).
	UpdateRun(ctx context.Context, r FlowRun) (FlowRun, error)
}
