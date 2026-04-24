package runtime

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrInvalidNodeStatus marks unknown or unsupported node statuses.
	ErrInvalidNodeStatus = errors.New("rlm runtime: invalid node status")
	// ErrInvalidNodeStatusTransition marks disallowed node lifecycle transitions.
	ErrInvalidNodeStatusTransition = errors.New("rlm runtime: invalid node status transition")
)

// NodeStatus is one lifecycle status for an RLM node.
type NodeStatus string

const (
	NodeStatusQueued    NodeStatus = "queued"
	NodeStatusRunning   NodeStatus = "running"
	NodeStatusWaiting   NodeStatus = "waiting"
	NodeStatusCompleted NodeStatus = "completed"
	NodeStatusFailed    NodeStatus = "failed"
	NodeStatusCanceled  NodeStatus = "canceled"
)

// IsValid reports whether the status is a known lifecycle value.
func (status NodeStatus) IsValid() bool {
	switch status {
	case NodeStatusQueued, NodeStatusRunning, NodeStatusWaiting, NodeStatusCompleted, NodeStatusFailed, NodeStatusCanceled:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether the status cannot transition further.
func (status NodeStatus) IsTerminal() bool {
	switch status {
	case NodeStatusCompleted, NodeStatusFailed, NodeStatusCanceled:
		return true
	default:
		return false
	}
}

// NodeStatusTransitionError wraps invalid transition details.
type NodeStatusTransitionError struct {
	From NodeStatus `json:"from"`
	To   NodeStatus `json:"to"`
}

func (err NodeStatusTransitionError) Error() string {
	return fmt.Sprintf("rlm runtime: invalid node status transition (%s -> %s)", err.From, err.To)
}

// Unwrap allows errors.Is(err, ErrInvalidNodeStatusTransition).
func (err NodeStatusTransitionError) Unwrap() error {
	return ErrInvalidNodeStatusTransition
}

// CanTransitionNodeStatus checks whether one status can move to the next status.
func CanTransitionNodeStatus(from, to NodeStatus) bool {
	if !from.IsValid() || !to.IsValid() {
		return false
	}
	if from == to {
		return true
	}
	switch from {
	case NodeStatusQueued:
		return to == NodeStatusRunning || to == NodeStatusWaiting || to == NodeStatusCanceled
	case NodeStatusRunning:
		return to == NodeStatusWaiting || to == NodeStatusCompleted || to == NodeStatusFailed || to == NodeStatusCanceled
	case NodeStatusWaiting:
		return to == NodeStatusRunning || to == NodeStatusCompleted || to == NodeStatusFailed || to == NodeStatusCanceled
	default:
		return false
	}
}

// ValidateNodeStatusTransition validates one status transition pair.
func ValidateNodeStatusTransition(from, to NodeStatus) error {
	if !from.IsValid() {
		return fmt.Errorf("%w: from=%q", ErrInvalidNodeStatus, from)
	}
	if !to.IsValid() {
		return fmt.Errorf("%w: to=%q", ErrInvalidNodeStatus, to)
	}
	if CanTransitionNodeStatus(from, to) {
		return nil
	}
	return NodeStatusTransitionError{From: from, To: to}
}

// ApplyNodeStatusTransition applies one validated status transition.
func ApplyNodeStatusTransition(node Node, to NodeStatus, at time.Time) (Node, error) {
	if err := ValidateNodeStatusTransition(node.Status, to); err != nil {
		return Node{}, err
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}

	next := node
	if next.Status == to {
		return next, nil
	}

	next.Status = to
	next.UpdatedAt = at
	if to == NodeStatusRunning && next.StartedAt.IsZero() {
		next.StartedAt = at
	}
	if to.IsTerminal() && next.FinishedAt.IsZero() {
		next.FinishedAt = at
	}
	return next, nil
}

// Run is the canonical root record for one recursive RLM execution.
type Run struct {
	ID         string         `json:"id"`
	RootNodeID string         `json:"root_node_id,omitempty"`
	Status     NodeStatus     `json:"status"`
	CreatedAt  time.Time      `json:"created_at,omitempty"`
	UpdatedAt  time.Time      `json:"updated_at,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// Node is one unit of recursive RLM execution.
type Node struct {
	RunID        string         `json:"run_id"`
	ID           string         `json:"id"`
	ParentNodeID string         `json:"parent_node_id,omitempty"`
	Depth        int            `json:"depth,omitempty"`
	Status       NodeStatus     `json:"status"`
	Prompt       string         `json:"prompt,omitempty"`
	CreatedAt    time.Time      `json:"created_at,omitempty"`
	UpdatedAt    time.Time      `json:"updated_at,omitempty"`
	StartedAt    time.Time      `json:"started_at,omitempty"`
	FinishedAt   time.Time      `json:"finished_at,omitempty"`
	Result       *NodeResult    `json:"result,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// CanTransition reports whether this node can move to the requested status.
func (node Node) CanTransition(to NodeStatus) bool {
	return CanTransitionNodeStatus(node.Status, to)
}

// NodeResult is the structured output for one node execution.
type NodeResult struct {
	Status       NodeStatus     `json:"status"`
	Summary      string         `json:"summary,omitempty"`
	Answer       string         `json:"answer,omitempty"`
	Findings     []Finding      `json:"findings,omitempty"`
	EvidenceRefs []EvidenceRef  `json:"evidence_refs,omitempty"`
	ArtifactRefs []ArtifactRef  `json:"artifact_refs,omitempty"`
	ErrorCode    string         `json:"error_code,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	StartedAt    time.Time      `json:"started_at,omitempty"`
	CompletedAt  time.Time      `json:"completed_at,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// Finding is one distilled child output with supporting references.
type Finding struct {
	ID           string         `json:"id,omitempty"`
	Summary      string         `json:"summary,omitempty"`
	EvidenceRefs []EvidenceRef  `json:"evidence_refs,omitempty"`
	ArtifactRefs []ArtifactRef  `json:"artifact_refs,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// EvidenceRef points at source material used by a node finding.
type EvidenceRef struct {
	Kind    string `json:"kind,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Title   string `json:"title,omitempty"`
	Excerpt string `json:"excerpt,omitempty"`
}

// ArtifactRef points at persisted output generated by a node run.
type ArtifactRef struct {
	ID        string `json:"id,omitempty"`
	URI       string `json:"uri,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Summary   string `json:"summary,omitempty"`
}
