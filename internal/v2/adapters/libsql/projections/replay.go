package projections

import (
	"context"
	"fmt"

	v2events "github.com/jkatigb/agentctl/internal/v2/core/events"
)

// ReplayFrom rebuilds projections from an events repository.
func (s *Store) ReplayFrom(ctx context.Context, repo v2events.Repository, filter v2events.ReplayFilter) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("v2 projections replay: nil store")
	}
	if repo == nil {
		return fmt.Errorf("v2 projections replay: nil repository")
	}
	return repo.Replay(ctx, filter, func(ctx context.Context, event v2events.Event) error {
		return s.Apply(ctx, event)
	})
}
