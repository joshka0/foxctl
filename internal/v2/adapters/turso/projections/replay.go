package projections

import (
	"context"
	"fmt"

	v2events "github.com/joshka0/foxctl/internal/v2/core/events"
)

// EventProjector applies events into an additional projection.
type EventProjector interface {
	Apply(ctx context.Context, evt v2events.Event) error
}

// ReplayFrom rebuilds projections from an events repository.
func (s *Store) ReplayFrom(ctx context.Context, repo v2events.Repository, filter v2events.ReplayFilter, projectors ...EventProjector) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("v2 projections replay: nil store")
	}
	if repo == nil {
		return fmt.Errorf("v2 projections replay: nil repository")
	}
	return repo.Replay(ctx, filter, func(ctx context.Context, event v2events.Event) error {
		if err := s.Apply(ctx, event); err != nil {
			return err
		}
		for _, projector := range projectors {
			if projector == nil {
				continue
			}
			if err := projector.Apply(ctx, event); err != nil {
				return err
			}
		}
		return nil
	})
}
