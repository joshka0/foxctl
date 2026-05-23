package observability

import (
	"context"
	"sync"
)

// Sink receives observability events from neutral packages.
type Sink interface {
	Emit(context.Context, *Event)
	EmitSync(context.Context, *Event) error
}

var (
	sinkMu sync.RWMutex
	sink   Sink
)

// SetSink installs the process-wide observability sink and returns a restore function.
func SetSink(next Sink) func() {
	sinkMu.Lock()
	previous := sink
	sink = next
	sinkMu.Unlock()
	return func() {
		sinkMu.Lock()
		sink = previous
		sinkMu.Unlock()
	}
}

func currentSink() Sink {
	sinkMu.RLock()
	current := sink
	sinkMu.RUnlock()
	return current
}

// Emit sends an event to the configured sink. If no sink is installed, it is a no-op.
func Emit(ctx context.Context, event *Event) {
	if current := currentSink(); current != nil {
		current.Emit(ctx, event)
	}
}

// EmitSync sends an event synchronously to the configured sink.
func EmitSync(ctx context.Context, event *Event) error {
	if current := currentSink(); current != nil {
		return current.EmitSync(ctx, event)
	}
	return nil
}
