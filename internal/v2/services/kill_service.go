package services

import (
	"context"
	"strings"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	"github.com/jkatigb/agentctl/internal/v2/core/kill"
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

	// Validate against projection read model when available.
	if s.deps.Projections != nil {
		if _, err := s.deps.Projections.GetRunState(ctx, requestedID); err != nil {
			if !isNotFound(err) {
				return kill.Response{}, asDependencyError("read run projection", err)
			}
			return kill.Response{}, asNotFoundError("run not found", map[string]any{
				"run_id": requestedID,
			})
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
