package services

import (
	"context"
	"strings"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	"github.com/jkatigb/agentctl/internal/v2/core/list"
)

// ListService exposes projected run listing.
type ListService struct {
	projections ProjectionStore
}

// NewListService builds a list service.
func NewListService(projections ProjectionStore) *ListService {
	return &ListService{projections: projections}
}

// List returns projection-backed runs with deterministic filtering.
func (s *ListService) List(ctx context.Context, req list.Request) (list.Response, error) {
	if s == nil || s.projections == nil {
		return list.Response{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "run projection store is not configured",
			Fatal:   true,
		}
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	states, err := s.projections.ListRunStates(ctx, RunStateFilter{
		Limit:   limit,
		Status:  strings.TrimSpace(req.Status),
		Command: strings.TrimSpace(req.Command),
		ActorID: strings.TrimSpace(req.ActorID),
	})
	if err != nil {
		return list.Response{}, asProjectionListError(err)
	}

	items := make([]list.Item, 0, len(states))
	for _, state := range states {
		items = append(items, list.Item{
			RunID:     state.RunID,
			Status:    state.Status,
			Command:   state.Command,
			RequestID: state.RequestID,
			ActorID:   state.ActorID,
			UpdatedAt: state.UpdatedAt,
		})
	}
	return list.Response{
		Items: items,
		Count: len(items),
	}, nil
}
