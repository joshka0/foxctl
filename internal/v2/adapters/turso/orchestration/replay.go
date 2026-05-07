package orchestration

import (
	"context"
	"fmt"

	coreevents "github.com/joshka0/foxctl/internal/v2/core/events"
)

// ReplayFrom rebuilds orchestration projection tables from event history.
func (s *Store) ReplayFrom(ctx context.Context, repo coreevents.Repository, filter coreevents.ReplayFilter) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("v2 orchestration replay: nil store")
	}
	if repo == nil {
		return fmt.Errorf("v2 orchestration replay: nil repository")
	}
	return repo.Replay(ctx, filter, func(ctx context.Context, event coreevents.Event) error {
		return s.Apply(ctx, event)
	})
}
