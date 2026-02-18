package events

import (
	"context"
	"errors"
)

var (
	// ErrNotFound indicates an entity was not found in event-backed storage.
	ErrNotFound = errors.New("v2 events: not found")
	// ErrVersionConflict indicates non-monotonic stream version or sequence append attempts.
	ErrVersionConflict = errors.New("v2 events: version conflict")
)

// StreamFilter selects stream-scoped events.
type StreamFilter struct {
	StreamID     string
	StreamType   StreamType
	AfterVersion int64
	Limit        int
}

// ReplayFilter selects replay ranges over ordered events.
type ReplayFilter struct {
	StreamID     string
	StreamType   StreamType
	FromSequence int64
	ToSequence   int64
	FromVersion  int64
	ToVersion    int64
	Limit        int
}

// ReplayHandler consumes a replayed event.
type ReplayHandler func(ctx context.Context, event Event) error

// Repository defines append and replay behavior for v2 event persistence.
type Repository interface {
	Appender
	ListStream(ctx context.Context, filter StreamFilter) ([]Event, error)
	Replay(ctx context.Context, filter ReplayFilter, handler ReplayHandler) error
}
