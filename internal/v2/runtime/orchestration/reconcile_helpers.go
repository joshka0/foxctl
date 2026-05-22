package orchestration

import (
	"context"
	"time"

	v2events "github.com/joshka0/foxctl/internal/v2/core/events"
)

// EventAppender persists canonical v2 events.
type EventAppender interface {
	Append(ctx context.Context, event v2events.Event) error
}

// EventProjector materializes a canonical v2 event into read-side state.
type EventProjector interface {
	Apply(ctx context.Context, evt v2events.Event) error
}

// AppendAndProject appends the event before updating projections.
func AppendAndProject(ctx context.Context, appender EventAppender, projector EventProjector, evt v2events.Event) error {
	if err := appender.Append(ctx, evt); err != nil {
		return err
	}
	if projector != nil {
		if err := projector.Apply(ctx, evt); err != nil {
			return err
		}
	}
	return nil
}

// RetryDelay returns bounded exponential backoff for a one-based retry attempt.
func RetryDelay(attempt int, base, max time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if base <= 0 {
		base = time.Second
	}
	if max <= 0 {
		max = 5 * time.Minute
	}
	delay := base
	for i := 1; i < attempt; i++ {
		if delay >= max {
			return max
		}
		delay *= 2
		if delay > max {
			return max
		}
	}
	return delay
}
