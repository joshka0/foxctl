package foxcular

import "context"

// Drain receives events from a Client. Implementations must be safe for
// concurrent use if the Client may call Emit/Flush from multiple goroutines.
type Drain interface {
	// Send delivers an event snapshot. The event is immutable; implementations
	// must not mutate it. Return a non-nil error to signal failure.
	Send(ctx context.Context, event *Event) error

	// Flush waits for any buffered events to be delivered.
	Flush(ctx context.Context) error

	// Close flushes remaining events and releases resources.
	// Implementations must be safe to call multiple times.
	Close() error
}

// DrainFunc is an adapter to allow the use of ordinary functions as Drains.
// Only Send is delegated; Flush and Close are no-ops.
type DrainFunc func(ctx context.Context, event *Event) error

func (f DrainFunc) Send(ctx context.Context, event *Event) error { return f(ctx, event) }
func (f DrainFunc) Flush(_ context.Context) error                { return nil }
func (f DrainFunc) Close() error                                 { return nil }

// FanoutDrain sends each event to all configured drains. If any drain fails,
// the first error is returned. Events are delivered to all drains regardless
// of individual failures.
type FanoutDrain struct {
	drains []Drain
}

// NewFanoutDrain creates a fanout drain that delivers to all provided drains.
func NewFanoutDrain(drains ...Drain) *FanoutDrain {
	cp := make([]Drain, len(drains))
	copy(cp, drains)
	return &FanoutDrain{drains: cp}
}

func (f *FanoutDrain) Send(ctx context.Context, event *Event) error {
	var firstErr error
	for _, d := range f.drains {
		if err := d.Send(ctx, event); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (f *FanoutDrain) Flush(ctx context.Context) error {
	var firstErr error
	for _, d := range f.drains {
		if err := d.Flush(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (f *FanoutDrain) Close() error {
	var firstErr error
	for _, d := range f.drains {
		if err := d.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
