package runtime

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrMissingNodeBackend marks scheduler configs without a backend.
	ErrMissingNodeBackend = errors.New("rlm runtime: missing node backend")
	// ErrRequiredSubcallsNotSatisfied marks a runtime-enforced recursion shape failure.
	ErrRequiredSubcallsNotSatisfied = errors.New("rlm runtime: required subcalls not satisfied")
)

// RequiredSubcallsError records a failed runtime recursion-shape constraint.
type RequiredSubcallsError struct {
	Required int
	Used     int
}

func (err RequiredSubcallsError) Error() string {
	return fmt.Sprintf("child did not satisfy required_subcalls=%d (used=%d); child must call rlm_query then rlm_wait before finalizing", err.Required, err.Used)
}

func (err RequiredSubcallsError) Unwrap() error {
	return ErrRequiredSubcallsNotSatisfied
}

// QueryRequest defines one asynchronous child query submission.
type QueryRequest struct {
	Prompt           string         `json:"prompt"`
	MaxIterations    int            `json:"max_iterations,omitempty"`
	SummaryMaxChars  int            `json:"summary_max_chars,omitempty"`
	RequiredSubcalls int            `json:"required_subcalls,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

// NodeInput is the backend-facing execution input for one node.
type NodeInput struct {
	Prompt           string         `json:"prompt"`
	MaxIterations    int            `json:"max_iterations,omitempty"`
	SummaryMaxChars  int            `json:"summary_max_chars,omitempty"`
	RequiredSubcalls int            `json:"required_subcalls,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

// NodeBackend executes one node and returns its structured result.
type NodeBackend interface {
	RunNode(ctx context.Context, node Node, input NodeInput) (NodeResult, error)
}

// NodeBackendFunc adapts a function into a NodeBackend.
type NodeBackendFunc func(ctx context.Context, node Node, input NodeInput) (NodeResult, error)

// RunNode calls the wrapped function.
func (fn NodeBackendFunc) RunNode(ctx context.Context, node Node, input NodeInput) (NodeResult, error) {
	return fn(ctx, node, input)
}
