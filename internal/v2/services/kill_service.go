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
	IDMap       IDMapStore
}

// KillService handles kill orchestration with id-map fallback.
type KillService struct {
	deps KillDependencies
}

// NewKillService builds a kill service.
func NewKillService(deps KillDependencies) *KillService {
	return &KillService{deps: deps}
}

// Kill validates target IDs, resolves legacy IDs, and invokes the runtime killer.
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

	resolvedID := requestedID
	mapped := false
	missingProjection := false

	// First try direct v2 projection lookup when available.
	if s.deps.Projections != nil {
		if _, err := s.deps.Projections.GetRunState(ctx, resolvedID); err != nil {
			if !isNotFound(err) {
				return kill.Response{}, asDependencyError("read run projection", err)
			}
			missingProjection = true
		}
	}

	// Resolve legacy IDs regardless of projection wiring. When projections are not
	// configured, we optimistically try idmap and fall back to original run ID.
	if s.deps.IDMap != nil {
		mappedID, mapErr := s.deps.IDMap.ResolveV2ID(ctx, "run", requestedID)
		switch {
		case mapErr == nil:
			candidate := strings.TrimSpace(mappedID)
			if candidate != "" {
				resolvedID = candidate
				mapped = resolvedID != requestedID
			}
		case isNotFound(mapErr):
			if missingProjection {
				return kill.Response{}, asNotFoundError("run not found", map[string]any{
					"run_id": requestedID,
				})
			}
		default:
			return kill.Response{}, asDependencyError("resolve legacy run id", mapErr)
		}
	} else if missingProjection {
		return kill.Response{}, asNotFoundError("run not found", map[string]any{
			"run_id": requestedID,
		})
	}

	if err := s.deps.Killer.Kill(ctx, resolvedID); err != nil {
		return kill.Response{}, asKillError(resolvedID, err)
	}

	return kill.Response{
		RunID:            resolvedID,
		Status:           "killed",
		MappedFromLegacy: mapped,
	}, nil
}
