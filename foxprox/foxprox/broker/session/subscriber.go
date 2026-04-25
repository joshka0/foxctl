package session

import (
	"sync"
	"sync/atomic"
)

// subscriber is the unexported per-subscription state held by OutputLog.
//
// It owns the delivery channel and tracks dropped-frame counts using a
// non-blocking send strategy so slow readers cannot stall the owner goroutine.
// A per-subscriber mutex serialises notify and close so the channel can never
// be written-to after it has been closed, which would otherwise panic under
// realistic scenarios: a client cancels its SSE context while the PTY reader
// is mid-Append.
type subscriber struct {
	mu      sync.Mutex
	ch      chan Chunk
	done    chan struct{}
	dropped atomic.Uint64
	closed  bool
}

// notify delivers a copy of c to the subscriber without blocking. If the
// channel is full the chunk is dropped and the dropped counter advances.
// If the subscriber is already closed, notify is a no-op.
//
// notify always clones c.Bytes so subscribers hold a private slice — mutating
// the received chunk must not mutate retained log state.
func (s *subscriber) notify(c Chunk) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- cloneChunk(c):
	default:
		s.dropped.Add(1)
	}
}

// close releases the channel exactly once and signals the context goroutine
// started in Subscribe to exit. Safe to call concurrently; subsequent calls
// are no-ops.
func (s *subscriber) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
	close(s.done)
}

func (s *subscriber) droppedLoad() uint64 {
	return s.dropped.Load()
}
