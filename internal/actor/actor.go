// Package actor provides a reactive actor system for agentctl agents.
//
// The actor system transforms poll-based agent daemons into event-driven actors
// that react to messages as they arrive. A supervisor manages actor lifecycles
// and routes messages using SQLite-based notifications.
//
// See docs/designs/reactive-actor-system.md for the full design.
package actor

import (
	"context"
	"time"
)

// Actor defines the reactive interface for all agents.
//
// Actors are event-driven: they react to messages (OnMailReceived),
// timeouts (OnTimeout), and errors (OnError) rather than polling.
type Actor interface {
	// Identity
	ID() string
	Namespace() string

	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error

	// Reactive handlers
	OnMailReceived(ctx context.Context, msg *Message) error
	OnTimeout(ctx context.Context, timer TimerEvent) error
	OnError(ctx context.Context, err error) Directive

	// State
	State() State
	SetState(state State)
}

// State represents the current state of an actor.
type State string

const (
	StateStarting   State = "starting"
	StateIdle       State = "idle"
	StateProcessing State = "processing"
	StateStopped    State = "stopped"
	StateError      State = "error"
)

// Directive tells the supervisor how to handle actor failures.
type Directive int

const (
	// DirectiveResume continues processing after a recoverable error.
	DirectiveResume Directive = iota
	// DirectiveRestart restarts the actor from scratch.
	DirectiveRestart
	// DirectiveStop stops the actor permanently.
	DirectiveStop
	// DirectiveEscalate escalates to parent/supervisor for decision.
	DirectiveEscalate
)

// String returns the string representation of a Directive.
func (d Directive) String() string {
	switch d {
	case DirectiveResume:
		return "resume"
	case DirectiveRestart:
		return "restart"
	case DirectiveStop:
		return "stop"
	case DirectiveEscalate:
		return "escalate"
	default:
		return "unknown"
	}
}

// Config holds actor configuration.
type Config struct {
	// ID is the unique identifier for this actor instance.
	ID string

	// Namespace is the mailbox namespace this actor listens to.
	Namespace string

	// Role defines the actor's behavior (e.g., "coder", "planner", "reviewer").
	Role string

	// LeaseTimeout is how long messages are leased during processing.
	// If processing takes longer, the message becomes visible again.
	LeaseTimeout time.Duration

	// MaxRetries is the maximum number of times to retry a failed message.
	MaxRetries int

	// Metadata holds additional actor-specific configuration.
	Metadata map[string]any
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig(namespace string) Config {
	return Config{
		ID:           namespace, // Default to namespace as ID
		Namespace:    namespace,
		LeaseTimeout: 5 * time.Minute,
		MaxRetries:   3,
		Metadata:     make(map[string]any),
	}
}

// Registration holds actor registration information for the supervisor.
type Registration struct {
	Config    Config
	Factory   func(cfg Config) (Actor, error)
	CreatedAt time.Time
}

// Stats holds runtime statistics for an actor.
type Stats struct {
	MessagesProcessed int64
	MessagesErrored   int64
	TotalProcessingNs int64
	LastMessageAt     time.Time
	RestartCount      int
	State             State
}

// AverageProcessingTime returns the average message processing time.
func (s Stats) AverageProcessingTime() time.Duration {
	if s.MessagesProcessed == 0 {
		return 0
	}
	return time.Duration(s.TotalProcessingNs / s.MessagesProcessed)
}

// ActorRef is a lightweight reference to an actor.
type ActorRef struct {
	ID        string
	Namespace string
	State     State
}

// Message represents a message delivered to an actor.
// This is a local definition; the full implementation lives in internal/mailbox.
type Message struct {
	ID        string
	FromNS    string
	ToNS      string
	Subject   string
	Body      []byte
	Headers   map[string]string
	Priority  int
	CreatedAt time.Time
	ExpiresAt time.Time
	LeaseID   string
	LeasedAt  time.Time
	Retries   int
	SessionID string
	Workspace string
	AgentID   string
}

// TimerEvent represents a scheduled timer that has fired.
type TimerEvent struct {
	Name      string
	Deadline  time.Time
	Namespace string
	Data      any
}
