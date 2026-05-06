package projections

import (
	"context"
	"errors"

	v2events "github.com/joshka0/foxctl/internal/v2/core/events"
	v2services "github.com/joshka0/foxctl/internal/v2/services"
)

// ServiceAdapter bridges projection storage to the v2 services ProjectionStore contract.
type ServiceAdapter struct {
	store *Store
}

// NewServiceAdapter wraps a projection store for v2 services.
func NewServiceAdapter(store *Store) *ServiceAdapter {
	return &ServiceAdapter{store: store}
}

var _ v2services.ProjectionStore = (*ServiceAdapter)(nil)

func (a *ServiceAdapter) Apply(ctx context.Context, evt v2events.Event) error {
	if a == nil || a.store == nil {
		return errors.New("v2 projections service adapter: nil store")
	}
	return a.store.Apply(ctx, evt)
}

func (a *ServiceAdapter) GetRunState(ctx context.Context, runID string) (v2services.RunState, error) {
	if a == nil || a.store == nil {
		return v2services.RunState{}, errors.New("v2 projections service adapter: nil store")
	}
	state, err := a.store.GetRunState(ctx, runID)
	if err != nil {
		return v2services.RunState{}, normalizeServiceNotFound(err)
	}
	return toServiceRunState(state), nil
}

func (a *ServiceAdapter) GetRunStateByRequestID(ctx context.Context, requestID string) (v2services.RunState, error) {
	if a == nil || a.store == nil {
		return v2services.RunState{}, errors.New("v2 projections service adapter: nil store")
	}
	state, err := a.store.GetRunStateByRequestID(ctx, requestID)
	if err != nil {
		return v2services.RunState{}, normalizeServiceNotFound(err)
	}
	return toServiceRunState(state), nil
}

func (a *ServiceAdapter) ListRunStates(ctx context.Context, filter v2services.RunStateFilter) ([]v2services.RunState, error) {
	if a == nil || a.store == nil {
		return nil, errors.New("v2 projections service adapter: nil store")
	}
	states, err := a.store.ListRunStates(ctx, RunStateFilter{
		Limit:   filter.Limit,
		Status:  filter.Status,
		Command: filter.Command,
		ActorID: filter.ActorID,
	})
	if err != nil {
		return nil, err
	}

	out := make([]v2services.RunState, 0, len(states))
	for _, state := range states {
		out = append(out, toServiceRunState(state))
	}
	return out, nil
}

func toServiceRunState(state RunState) v2services.RunState {
	return v2services.RunState{
		RunID:     state.RunID,
		Status:    state.Status,
		Command:   state.Command,
		RequestID: state.RequestID,
		ActorID:   state.ActorID,
		UpdatedAt: state.UpdatedAt,
	}
}

func normalizeServiceNotFound(err error) error {
	if errors.Is(err, ErrNotFound) || errors.Is(err, v2events.ErrNotFound) {
		return v2events.ErrNotFound
	}
	return err
}
