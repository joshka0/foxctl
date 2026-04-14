package fakes

import (
	"context"
	"sync"

	"github.com/joshka0/foxctl/internal/v2/core/events"
)

// FakeEventStore is an in-memory deterministic events appender for tests.
type FakeEventStore struct {
	mu     sync.Mutex
	events []events.Event
}

func NewFakeEventStore() *FakeEventStore {
	return &FakeEventStore{}
}

// Append stores a cloned event in insertion order.
func (s *FakeEventStore) Append(ctx context.Context, event events.Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event.Clone())
	return nil
}

// Events returns a defensive copy of captured events.
func (s *FakeEventStore) Events() []events.Event {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]events.Event, len(s.events))
	for i := range s.events {
		out[i] = s.events[i].Clone()
	}
	return out
}

// Count returns number of appended events.
func (s *FakeEventStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

var _ events.Appender = (*FakeEventStore)(nil)
