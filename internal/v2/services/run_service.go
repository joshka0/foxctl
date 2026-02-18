package services

import (
	"context"
	stderrors "errors"
	"strings"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

// RunService executes the canonical v2 turn pipeline.
type RunService struct {
	runner TurnRunner
}

// NewRunService builds a run service.
func NewRunService(runner TurnRunner) *RunService {
	return &RunService{runner: runner}
}

// Run validates input and executes one canonical run turn.
func (s *RunService) Run(ctx context.Context, in run.TurnInput) (run.TurnOutput, error) {
	in.RunID = strings.TrimSpace(in.RunID)
	if in.RunID == "" {
		return run.TurnOutput{}, asValidationError("run_id is required", map[string]any{
			"field": "run_id",
		})
	}
	if in.MaxIterations < 0 {
		return run.TurnOutput{}, asValidationError("max_iterations must be >= 0", map[string]any{
			"field": "max_iterations",
		})
	}
	if strings.TrimSpace(in.Command) == "" {
		in.Command = "run"
	}

	if s == nil || s.runner == nil {
		return run.TurnOutput{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "turn runner is not configured",
			Fatal:   true,
		}
	}

	out, err := s.runner.RunTurn(ctx, in)
	if err == nil {
		return out, nil
	}

	var verr *v2errors.V2Error
	if stderrors.As(err, &verr) {
		return out, verr
	}
	return out, &v2errors.V2Error{
		Kind:      v2errors.ErrInternal,
		Message:   "run execution failed",
		Cause:     err,
		Fatal:     true,
		Retryable: false,
	}
}
