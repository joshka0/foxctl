// Package model defines the foxctl-native evolve domain records.
package model

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// MetricDirection controls whether higher or lower benchmark scores are better.
type MetricDirection string

const (
	MetricMax MetricDirection = "max"
	MetricMin MetricDirection = "min"
)

// Valid reports whether the direction is supported.
func (m MetricDirection) Valid() bool {
	switch m {
	case MetricMax, MetricMin:
		return true
	default:
		return false
	}
}

// RunStatus is the lifecycle state of an evolve run.
type RunStatus string

const (
	RunStatusActive    RunStatus = "active"
	RunStatusPaused    RunStatus = "paused"
	RunStatusCompleted RunStatus = "completed"
	RunStatusArchived  RunStatus = "archived"
)

// Valid reports whether the status is supported.
func (s RunStatus) Valid() bool {
	switch s {
	case RunStatusActive, RunStatusPaused, RunStatusCompleted, RunStatusArchived:
		return true
	default:
		return false
	}
}

// NodeStatus is the lifecycle state of one experiment branch in a run tree.
type NodeStatus string

const (
	NodeStatusRoot      NodeStatus = "root"
	NodeStatusPending   NodeStatus = "pending"
	NodeStatusActive    NodeStatus = "active"
	NodeStatusCommitted NodeStatus = "committed"
	NodeStatusEvaluated NodeStatus = "evaluated"
	NodeStatusFailed    NodeStatus = "failed"
	NodeStatusDiscarded NodeStatus = "discarded"
	NodeStatusPruned    NodeStatus = "pruned"
)

// Valid reports whether the status is supported.
func (s NodeStatus) Valid() bool {
	switch s {
	case NodeStatusRoot, NodeStatusPending, NodeStatusActive, NodeStatusCommitted,
		NodeStatusEvaluated, NodeStatusFailed, NodeStatusDiscarded, NodeStatusPruned:
		return true
	default:
		return false
	}
}

// AttemptStatus is the lifecycle state of one benchmark/gate execution attempt.
type AttemptStatus string

const (
	AttemptStatusActive    AttemptStatus = "active"
	AttemptStatusCompleted AttemptStatus = "completed"
	AttemptStatusFailed    AttemptStatus = "failed"
)

// Valid reports whether the status is supported.
func (s AttemptStatus) Valid() bool {
	switch s {
	case AttemptStatusActive, AttemptStatusCompleted, AttemptStatusFailed:
		return true
	default:
		return false
	}
}

// Run represents one experiment campaign for a workspace.
type Run struct {
	ID               string
	WorkspacePath    string
	TargetPath       string
	BenchmarkCommand string
	Metric           MetricDirection
	Status           RunStatus
	Active           bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Validate returns an error when the run cannot be persisted safely.
func (r Run) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("run id is required")
	}
	if strings.TrimSpace(r.WorkspacePath) == "" {
		return fmt.Errorf("run workspace path is required")
	}
	if strings.TrimSpace(r.TargetPath) == "" {
		return fmt.Errorf("run target path is required")
	}
	if strings.TrimSpace(r.BenchmarkCommand) == "" {
		return fmt.Errorf("run benchmark command is required")
	}
	if !r.Metric.Valid() {
		return fmt.Errorf("run metric must be %q or %q", MetricMax, MetricMin)
	}
	if !r.Status.Valid() {
		return fmt.Errorf("run status %q is invalid", r.Status)
	}
	if r.CreatedAt.IsZero() {
		return fmt.Errorf("run created_at is required")
	}
	if r.UpdatedAt.IsZero() {
		return fmt.Errorf("run updated_at is required")
	}
	return nil
}

// Node represents one experiment branch in a run tree.
type Node struct {
	ID                string
	RunID             string
	ParentID          string
	Status            NodeStatus
	Hypothesis        string
	Score             *float64
	EvalEpoch         int
	Branch            string
	WorktreePath      string
	CommitSHA         string
	PrunedReason      string
	CurrentAttempt    int
	EvaluatedAttempts int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Validate returns an error when the node cannot be persisted safely.
func (n Node) Validate() error {
	if strings.TrimSpace(n.ID) == "" {
		return fmt.Errorf("node id is required")
	}
	if strings.TrimSpace(n.RunID) == "" {
		return fmt.Errorf("node run id is required")
	}
	if !n.Status.Valid() {
		return fmt.Errorf("node status %q is invalid", n.Status)
	}
	if n.EvalEpoch < 0 {
		return fmt.Errorf("node eval epoch cannot be negative")
	}
	if n.CurrentAttempt < 0 {
		return fmt.Errorf("node current attempt cannot be negative")
	}
	if n.EvaluatedAttempts < 0 {
		return fmt.Errorf("node evaluated attempts cannot be negative")
	}
	if err := validateOptionalFiniteScore("node score", n.Score); err != nil {
		return err
	}
	if n.CreatedAt.IsZero() {
		return fmt.Errorf("node created_at is required")
	}
	if n.UpdatedAt.IsZero() {
		return fmt.Errorf("node updated_at is required")
	}
	return nil
}

// Gate is a command inherited by descendant nodes until shadowed by name.
type Gate struct {
	ID        string
	RunID     string
	NodeID    string
	Name      string
	Command   string
	CreatedAt time.Time
}

// Validate returns an error when the gate cannot be persisted safely.
func (g Gate) Validate() error {
	if strings.TrimSpace(g.ID) == "" {
		return fmt.Errorf("gate id is required")
	}
	if strings.TrimSpace(g.RunID) == "" {
		return fmt.Errorf("gate run id is required")
	}
	if strings.TrimSpace(g.NodeID) == "" {
		return fmt.Errorf("gate node id is required")
	}
	if strings.TrimSpace(g.Name) == "" {
		return fmt.Errorf("gate name is required")
	}
	if strings.TrimSpace(g.Command) == "" {
		return fmt.Errorf("gate command is required")
	}
	if g.CreatedAt.IsZero() {
		return fmt.Errorf("gate created_at is required")
	}
	return nil
}

// Attempt records one benchmark/gate execution attempt for a node.
type Attempt struct {
	ID                string
	NodeID            string
	AttemptNo         int
	Status            AttemptStatus
	Score             *float64
	BenchmarkArtifact string
	TraceArtifact     string
	DiffArtifact      string
	Error             string
	StartedAt         time.Time
	FinishedAt        time.Time
}

// Validate returns an error when the attempt cannot be persisted safely.
func (a Attempt) Validate() error {
	if strings.TrimSpace(a.ID) == "" {
		return fmt.Errorf("attempt id is required")
	}
	if strings.TrimSpace(a.NodeID) == "" {
		return fmt.Errorf("attempt node id is required")
	}
	if a.AttemptNo <= 0 {
		return fmt.Errorf("attempt number must be positive")
	}
	if !a.Status.Valid() {
		return fmt.Errorf("attempt status %q is invalid", a.Status)
	}
	if err := validateOptionalFiniteScore("attempt score", a.Score); err != nil {
		return err
	}
	if a.StartedAt.IsZero() {
		return fmt.Errorf("attempt started_at is required")
	}
	return nil
}

func validateOptionalFiniteScore(field string, score *float64) error {
	if score == nil {
		return nil
	}
	if math.IsNaN(*score) || math.IsInf(*score, 0) {
		return fmt.Errorf("%s must be finite", field)
	}
	return nil
}

// GateResult records one gate outcome for an attempt.
type GateResult struct {
	AttemptID    string
	GateName     string
	SourceNodeID string
	Passed       bool
	ReturnCode   *int
	LogArtifact  string
}

// Validate returns an error when the gate result cannot be persisted safely.
func (r GateResult) Validate() error {
	if strings.TrimSpace(r.AttemptID) == "" {
		return fmt.Errorf("gate result attempt id is required")
	}
	if strings.TrimSpace(r.GateName) == "" {
		return fmt.Errorf("gate result gate name is required")
	}
	if strings.TrimSpace(r.SourceNodeID) == "" {
		return fmt.Errorf("gate result source node id is required")
	}
	return nil
}

// Annotation stores review or task analysis attached to a run or node.
type Annotation struct {
	ID        string
	RunID     string
	NodeID    string
	TaskID    string
	Analysis  string
	CreatedAt time.Time
}

// Validate returns an error when the annotation cannot be persisted safely.
func (a Annotation) Validate() error {
	if strings.TrimSpace(a.ID) == "" {
		return fmt.Errorf("annotation id is required")
	}
	if strings.TrimSpace(a.RunID) == "" {
		return fmt.Errorf("annotation run id is required")
	}
	if strings.TrimSpace(a.Analysis) == "" {
		return fmt.Errorf("annotation analysis is required")
	}
	if a.CreatedAt.IsZero() {
		return fmt.Errorf("annotation created_at is required")
	}
	return nil
}

// InfraEvent records tool-level infrastructure signals for a run.
type InfraEvent struct {
	ID        string
	RunID     string
	Message   string
	Breaking  bool
	CreatedAt time.Time
}

// Validate returns an error when the infra event cannot be persisted safely.
func (e InfraEvent) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf("infra event id is required")
	}
	if strings.TrimSpace(e.RunID) == "" {
		return fmt.Errorf("infra event run id is required")
	}
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Errorf("infra event message is required")
	}
	if e.CreatedAt.IsZero() {
		return fmt.Errorf("infra event created_at is required")
	}
	return nil
}
