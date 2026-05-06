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
	// ErrIdempotencyConflict indicates a replay used an existing event ID with different material fields.
	ErrIdempotencyConflict = errors.New("v2 events: idempotency conflict")
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

// StreamCursorRequest selects the current cursor for one event stream.
type StreamCursorRequest struct {
	StreamID   string
	StreamType StreamType
}

// StreamCursor is the current append position for one event stream.
type StreamCursor struct {
	StreamVersion int64
	Sequence      int64
}

// AppendResult reports whether an idempotent append inserted a new event or returned an existing row.
type AppendResult struct {
	Event    Event
	Appended bool
}

// ReplayHandler consumes a replayed event.
type ReplayHandler func(ctx context.Context, event Event) error

// StreamCursorReader reads the current cursor for one event stream.
type StreamCursorReader interface {
	ReadStreamCursor(ctx context.Context, request StreamCursorRequest) (StreamCursor, error)
}

// AppendIfAbsent stores an event once and returns an existing equivalent row on replay.
type AppendIfAbsent interface {
	AppendIfAbsent(ctx context.Context, event Event) (AppendResult, error)
}

// Repository defines append and replay behavior for v2 event persistence.
type Repository interface {
	Appender
	AppendIfAbsent
	StreamCursorReader
	ListStream(ctx context.Context, filter StreamFilter) ([]Event, error)
	Replay(ctx context.Context, filter ReplayFilter, handler ReplayHandler) error
}
