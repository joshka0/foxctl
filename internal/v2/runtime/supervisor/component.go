package supervisor

import "context"

// Component is the canonical lifecycle contract for v2 background services.
type Component interface {
	Run(ctx context.Context) error
}

// Spec registers one named component with the host.
type Spec struct {
	Name      string
	Component Component
}

// EventKind identifies host lifecycle events.
type EventKind string

const (
	EventStarting EventKind = "starting"
	EventStopped  EventKind = "stopped"
	EventFailed   EventKind = "failed"
)

// Event reports supervisor lifecycle transitions for observability.
type Event struct {
	Kind EventKind
	Name string
	Err  error
}

// Observer receives component lifecycle events.
type Observer func(evt Event)
