// Package runtime provides generic bounded-runtime primitives for the TUI
// operator cockpit. Bounded[Req, Upd] encapsulates the goroutine lifecycle
// pattern shared by all request-driven runtimes.
package runtime

import (
	"context"
	"errors"
	"sync"
)

const defaultBufferSize = 16

// ErrStopped is returned by Enqueue when the runtime has been stopped.
var ErrStopped = errors.New("runtime stopped")

// Handler processes a single request and returns an update. The handler
// must respect context cancellation — if ctx.Done(), it should return
// promptly with the ctx error.
type Handler[Req any, Upd any] func(ctx context.Context, req Req) Upd

// Bounded is a generic bounded-queue runtime that owns one goroutine processing
// requests from a buffered channel and emitting updates to a buffered channel.
//
// Construction starts the goroutine immediately. Call Stop() (or Close()) to
// cancel the goroutine and wait for it to exit. The updates channel is closed
// exactly once after the goroutine drains.
//
// Bounded is safe for concurrent use: multiple goroutines may call Enqueue,
// while exactly one goroutine should call Stop.
type Bounded[Req any, Upd any] struct {
	handler    Handler[Req, Upd]
	requests   chan Req
	updates    chan Upd
	sourceMode bool // if true, handler manages its own updates via SendUpdate

	ctx    context.Context
	cancel context.CancelFunc

	stopOnce  sync.Once
	waitGroup sync.WaitGroup
}

// NewBounded creates and starts a bounded runtime. The handler is called for
// each request in the run goroutine. bufferSize controls the capacity of both
// the request and update channels (0 defaults to 16). The goroutine starts
// immediately and must be stopped with Stop() or Close().
func NewBounded[Req any, Upd any](
	parent context.Context,
	requestBufferSize int,
	updateBufferSize int,
	handler Handler[Req, Upd],
) (*Bounded[Req, Upd], error) {
	if handler == nil {
		return nil, errors.New("handler is required")
	}
	if parent == nil {
		parent = context.Background()
	}
	if requestBufferSize < 0 {
		return nil, errors.New("request buffer size must be >= 0")
	}
	if requestBufferSize == 0 {
		requestBufferSize = defaultBufferSize
	}
	if updateBufferSize < 0 {
		return nil, errors.New("update buffer size must be >= 0")
	}
	if updateBufferSize == 0 {
		updateBufferSize = defaultBufferSize
	}

	ctx, cancel := context.WithCancel(parent)
	b := &Bounded[Req, Upd]{
		handler:  handler,
		requests: make(chan Req, requestBufferSize),
		updates:  make(chan Upd, updateBufferSize),
		ctx:      ctx,
		cancel:   cancel,
	}
	b.waitGroup.Add(1)
	go b.run()
	return b, nil
}

// NewBoundedSource creates a bounded runtime in source mode. In source mode,
// the handler is responsible for sending its own updates via SendUpdate.
// The handler's return value is ignored. This is useful for source-driven
// runtimes (e.g., stream pumps) where the handler emits multiple updates
// per request.
func NewBoundedSource[Req any, Upd any](
	parent context.Context,
	requestBufferSize int,
	updateBufferSize int,
	handler Handler[Req, Upd],
) (*Bounded[Req, Upd], error) {
	b, err := NewBounded(parent, requestBufferSize, updateBufferSize, handler)
	if err != nil {
		return nil, err
	}
	b.sourceMode = true
	return b, nil
}

// Enqueue adds a request to the bounded queue. It blocks if the queue is full.
// Returns ErrStopped if the runtime has been stopped. Returns ctx.Err() if the
// caller's context is cancelled while blocked.
func (b *Bounded[Req, Upd]) Enqueue(ctx context.Context, req Req) error {
	if b == nil {
		return errors.New("bounded runtime is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// Fast-path check: if already stopped, return immediately.
	select {
	case <-b.ctx.Done():
		return ErrStopped
	default:
	}

	// Block until we can send or something cancels.
	select {
	case <-b.ctx.Done():
		return ErrStopped
	case <-ctx.Done():
		return ctx.Err()
	case b.requests <- req:
		return nil
	}
}

// Updates returns the receive-only update channel. It is closed exactly once
// after the runtime goroutine exits (after draining any pending items).
func (b *Bounded[Req, Upd]) Updates() <-chan Upd {
	if b == nil {
		return nil
	}
	return b.updates
}

// Stop cancels the runtime goroutine and waits for it to exit. Safe to call
// concurrently — only the first call performs the shutdown.
func (b *Bounded[Req, Upd]) Stop() {
	if b == nil {
		return
	}
	b.stopOnce.Do(func() {
		b.cancel()
		b.waitGroup.Wait()
	})
}

// Close is an alias for Stop.
func (b *Bounded[Req, Upd]) Close() {
	b.Stop()
}

// Requests returns the internal request channel for advanced use cases
// (e.g., source-driven runtimes that don't use Enqueue).
func (b *Bounded[Req, Upd]) Requests() chan Req {
	if b == nil {
		return nil
	}
	return b.requests
}

// SendUpdate sends an update through the update channel. It is safe to call
// from within a Handler (e.g., for source-driven runtimes that emit multiple
// updates per request). Returns the context error if the runtime is stopped.
func (b *Bounded[Req, Upd]) SendUpdate(upd Upd) error {
	if b == nil {
		return errors.New("bounded runtime is nil")
	}
	select {
	case <-b.ctx.Done():
		return b.ctx.Err()
	case b.updates <- upd:
		return nil
	}
}

// Context returns the runtime's internal context, which is cancelled on Stop.
func (b *Bounded[Req, Upd]) Context() context.Context {
	if b == nil {
		return context.Background()
	}
	return b.ctx
}

func (b *Bounded[Req, Upd]) run() {
	defer b.waitGroup.Done()
	defer close(b.updates)

	for {
		select {
		case <-b.ctx.Done():
			return
		case req := <-b.requests:
			upd := b.handler(b.ctx, req)
			// In source mode, the handler manages its own updates via SendUpdate.
			// The return value is ignored.
			if b.sourceMode {
				continue
			}
			select {
			case <-b.ctx.Done():
				return
			case b.updates <- upd:
			}
		}
	}
}
