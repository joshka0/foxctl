package actor

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// EventType identifies the type of event.
type EventType string

const (
	// EventMailReceived is a persisted mail-received event.
	EventMailReceived EventType = "mail.received"
	// EventMailSent is a persisted mail-sent event.
	EventMailSent EventType = "mail.sent"
	// EventMailAcked is a persisted mail-acked event.
	EventMailAcked EventType = "mail.acked"
	// EventMailExpired is a persisted mail-expired event.
	EventMailExpired EventType = "mail.expired"

	// EventTaskCreated is a task-created event.
	EventTaskCreated EventType = "task.created"
	// EventTaskUpdated is a task-updated event.
	EventTaskUpdated EventType = "task.updated"
	// EventTaskCompleted is a persisted task-completed event.
	EventTaskCompleted EventType = "task.completed"

	// EventAgentStarted is a persisted agent-started event.
	EventAgentStarted EventType = "agent.started"
	// EventAgentStopped is a persisted agent-stopped event.
	EventAgentStopped EventType = "agent.stopped"
	// EventAgentError is a persisted agent-error event.
	EventAgentError EventType = "agent.error"

	// EventHookTriggered is an ephemeral hook-triggered event.
	EventHookTriggered EventType = "hook.triggered"
	// EventHookBlocked is an ephemeral hook-blocked event.
	EventHookBlocked EventType = "hook.blocked"

	// EventFileChanged is an ephemeral file-changed event.
	EventFileChanged EventType = "file.changed"
	// EventFileCreated is an ephemeral file-created event.
	EventFileCreated EventType = "file.created"
)

// persistedEvents marks which events should be persisted to trajectory.db.
var persistedEvents = map[EventType]bool{
	EventMailReceived:  true,
	EventMailSent:      true,
	EventMailAcked:     true,
	EventMailExpired:   true,
	EventAgentStarted:  true,
	EventAgentStopped:  true,
	EventAgentError:    true,
	EventTaskCompleted: true,
}

// ShouldPersist returns whether this event type should be persisted.
func (t EventType) ShouldPersist() bool {
	return persistedEvents[t]
}

// Event represents a system event.
type Event struct {
	ID        string          `json:"id"`
	Type      EventType       `json:"type"`
	Source    string          `json:"source"` // Actor namespace or "supervisor"
	Target    string          `json:"target"` // Optional target namespace
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Workspace string          `json:"workspace,omitempty"`
}

// NewEvent creates a new Event with a generated ID.
func NewEvent(eventType EventType, source string) Event {
	return Event{
		ID:        ulid.Make().String(),
		Type:      eventType,
		Source:    source,
		Timestamp: time.Now(),
	}
}

// WithData sets the event data.
func (e Event) WithData(data any) Event {
	if data != nil {
		if b, err := json.Marshal(data); err == nil {
			e.Data = b
		}
	}
	return e
}

// WithTarget sets the event target.
func (e Event) WithTarget(target string) Event {
	e.Target = target
	return e
}

// WithSession sets the session ID.
func (e Event) WithSession(sessionID string) Event {
	e.SessionID = sessionID
	return e
}

// WithWorkspace sets the workspace.
func (e Event) WithWorkspace(workspace string) Event {
	e.Workspace = workspace
	return e
}

// Subscriber is a function that receives events.
type Subscriber func(Event)

// EventBus provides in-memory pub/sub for system events.
//
// Contract: EventBus is ephemeral
// - In-memory fanout for low-latency
// - Subscribers may miss events if slow (non-blocking send)
// - Important events are selectively persisted via Persister
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[EventType][]chan Event
	persister   Persister
	bufferSize  int
}

// Persister persists important events to durable storage.
type Persister interface {
	Persist(ctx context.Context, event Event) error
}

// EventBusOption configures an EventBus.
type EventBusOption func(*EventBus)

// WithPersister sets the event persister.
func WithPersister(p Persister) EventBusOption {
	return func(eb *EventBus) {
		eb.persister = p
	}
}

// WithSubscriberBuffer sets the buffer size for subscriber channels.
func WithSubscriberBuffer(size int) EventBusOption {
	return func(eb *EventBus) {
		eb.bufferSize = size
	}
}

// NewEventBus creates a new EventBus.
func NewEventBus(opts ...EventBusOption) *EventBus {
	eb := &EventBus{
		subscribers: make(map[EventType][]chan Event),
		bufferSize:  100,
	}

	for _, opt := range opts {
		opt(eb)
	}

	return eb
}

// Subscribe subscribes to events of the given types.
// Returns a channel that receives matching events.
// The channel should be closed by calling Unsubscribe.
func (eb *EventBus) Subscribe(types ...EventType) <-chan Event {
	ch := make(chan Event, eb.bufferSize)

	eb.mu.Lock()
	defer eb.mu.Unlock()

	for _, t := range types {
		eb.subscribers[t] = append(eb.subscribers[t], ch)
	}

	return ch
}

// SubscribeAll subscribes to all event types.
func (eb *EventBus) SubscribeAll() <-chan Event {
	return eb.Subscribe(
		EventMailReceived, EventMailSent, EventMailAcked, EventMailExpired,
		EventTaskCreated, EventTaskUpdated, EventTaskCompleted,
		EventAgentStarted, EventAgentStopped, EventAgentError,
		EventHookTriggered, EventHookBlocked,
		EventFileChanged, EventFileCreated,
	)
}

// Unsubscribe removes a subscription channel.
func (eb *EventBus) Unsubscribe(ch <-chan Event) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	seen := make(map[chan Event]bool)

	for eventType, subs := range eb.subscribers {
		var remaining []chan Event
		for _, sub := range subs {
			if sub != ch {
				remaining = append(remaining, sub)
			} else {
				if !seen[sub] {
					close(sub)
					seen[sub] = true
				}
			}
		}
		eb.subscribers[eventType] = remaining
	}
}

// Publish publishes an event to all subscribers.
//
// Contract: Non-blocking send
// - If a subscriber channel is full, the event is dropped for that subscriber
// - Important events are persisted regardless of subscriber state
func (eb *EventBus) Publish(event Event) {
	// Ensure ID and timestamp are set
	if event.ID == "" {
		event.ID = ulid.Make().String()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Fan out to subscribers (ephemeral)
	eb.mu.RLock()
	subs := eb.subscribers[event.Type]
	eb.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- event:
		default:
			// Drop if subscriber is slow
		}
	}

	// Persist important events
	if event.Type.ShouldPersist() && eb.persister != nil {
		// Fire and forget - don't block publishing
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = eb.persister.Persist(ctx, event)
		}()
	}
}

// PublishSync publishes an event and waits for persistence.
// Use sparingly - prefer Publish for non-critical events.
func (eb *EventBus) PublishSync(event Event) error {
	// Ensure ID and timestamp are set
	if event.ID == "" {
		event.ID = ulid.Make().String()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Fan out to subscribers
	eb.mu.RLock()
	subs := eb.subscribers[event.Type]
	eb.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- event:
		default:
		}
	}

	// Persist synchronously
	if event.Type.ShouldPersist() && eb.persister != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return eb.persister.Persist(ctx, event)
	}

	return nil
}

// Stats returns EventBus statistics.
func (eb *EventBus) Stats() EventBusStats {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	stats := EventBusStats{
		SubscriberCounts: make(map[EventType]int),
	}

	for t, subs := range eb.subscribers {
		stats.SubscriberCounts[t] = len(subs)
		stats.TotalSubscribers += len(subs)
	}

	return stats
}

// EventBusStats holds EventBus statistics.
type EventBusStats struct {
	TotalSubscribers int
	SubscriberCounts map[EventType]int
}
