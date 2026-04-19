package session

import "sync/atomic"

// subscriber is the unexported per-subscription state held by OutputLog.
// It owns the delivery channel and tracks dropped-frame counts using a
// non-blocking send strategy so slow readers cannot stall the owner goroutine.
type subscriber struct {
	ch      chan Chunk
	done    chan struct{}
	dropped atomic.Uint64
	closed  atomic.Bool
}

// notify delivers c to the subscriber without blocking. If the channel is
// full, the chunk is dropped and the dropped counter advances.
func (s *subscriber) notify(c Chunk) {
	if s.closed.Load() {
		return
	}
	select {
	case s.ch <- c:
	default:
		s.dropped.Add(1)
	}
}

// close releases the channel exactly once and signals the context goroutine
// started in Subscribe to exit.
func (s *subscriber) close() {
	if !s.closed.CompareAndSwap(false, true) {
		return
	}
	close(s.ch)
	close(s.done)
}

func (s *subscriber) droppedLoad() uint64 {
	return s.dropped.Load()
}
