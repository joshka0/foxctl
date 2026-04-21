package services

import (
	"context"
	"strings"

	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
	"github.com/joshka0/foxctl/internal/v2/core/kill"
)

// KillDependencies wires KillService collaborators.
type KillDependencies struct {
	Killer      RunKiller
	Projections ProjectionStore
}

// KillService handles kill orchestration for v2 run IDs.
type KillService struct {
	deps KillDependencies
}

// NewKillService builds a kill service.
func NewKillService(deps KillDependencies) *KillService {
	return &KillService{deps: deps}
}

// Kill validates target IDs and invokes the runtime killer.
func (s *KillService) Kill(ctx context.Context, req kill.Request) (kill.Response, error) {
	if s == nil || s.deps.Killer == nil {
		return kill.Response{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "run killer is not configured",
			Fatal:   true,
		}
	}

	requestedID := strings.TrimSpace(req.RunID)
	if requestedID == "" {
		return kill.Response{}, asValidationError("run_id is required", map[string]any{
			"field": "run_id",
		})
	}

	// Validate against projection read model when available. Terminal run states
	// are idempotent: there is no live runtime to terminate.
	if s.deps.Projections != nil {
		state, err := s.deps.Projections.GetRunState(ctx, requestedID)
		if err != nil {
			if !isNotFound(err) {
				return kill.Response{}, asDependencyError("read run projection", err)
			}
			return kill.Response{}, asNotFoundError("run not found", map[string]any{
				"run_id": requestedID,
			})
		}
		switch strings.TrimSpace(state.Status) {
		case "completed", "failed", "killed":
			return kill.Response{
				RunID:  requestedID,
				Status: state.Status,
			}, nil
		}
	}

	if err := s.deps.Killer.Kill(ctx, requestedID); err != nil {
		return kill.Response{}, asKillError(requestedID, err)
	}

	return kill.Response{
		RunID:  requestedID,
		Status: "killed",
	}, nil
}
