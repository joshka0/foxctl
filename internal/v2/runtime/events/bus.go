package events

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	coreevents "github.com/joshka0/foxctl/internal/v2/core/events"
)

const (
	// DefaultSubscriberBuffer is used when Config.SubscriberBuffer is not set.
	DefaultSubscriberBuffer = 64
	// DefaultPublishTimeout is used by block overflow mode when Config.PublishTimeout is not set.
	DefaultPublishTimeout = 25 * time.Millisecond
)

var (
	// ErrClosed indicates the bus has been closed.
	ErrClosed = errors.New("v2 runtime events: bus closed")
	// ErrBackpressure indicates publish could not deliver to at least one subscriber in block mode.
	ErrBackpressure = errors.New("v2 runtime events: backpressure")
)

// OverflowPolicy defines behavior when a subscriber queue is full.
type OverflowPolicy string

const (
	// OverflowDropNewest drops the event being published for that subscriber.
	OverflowDropNewest OverflowPolicy = "drop_newest"
	// OverflowDropOldest drops one queued event and keeps the newest event.
	OverflowDropOldest OverflowPolicy = "drop_oldest"
	// OverflowBlock blocks with timeout/context until capacity is available.
	OverflowBlock OverflowPolicy = "block"
)

// Config controls bus behavior.
type Config struct {
	SubscriberBuffer int
	OverflowPolicy   OverflowPolicy
	PublishTimeout   time.Duration
}

// Stats captures runtime bus telemetry counters.
type Stats struct {
	Published    int64
	Delivered    int64
	Dropped      int64
	Overflow     int64
	Backpressure int64
	Subscribers  int
	Policy       OverflowPolicy
}

// Bus fan-outs v2 events to in-memory subscribers with bounded queues.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[uint64]chan coreevents.Event
	closed      bool
	nextID      uint64

	cfg Config

	published    atomic.Int64
	delivered    atomic.Int64
	dropped      atomic.Int64
	overflow     atomic.Int64
	backpressure atomic.Int64
}

// NewBus creates a bounded runtime event bus.
func NewBus(cfg Config) *Bus {
	if cfg.SubscriberBuffer <= 0 {
		cfg.SubscriberBuffer = DefaultSubscriberBuffer
	}
	if cfg.OverflowPolicy == "" {
		cfg.OverflowPolicy = OverflowDropNewest
	}
	if cfg.PublishTimeout <= 0 {
		cfg.PublishTimeout = DefaultPublishTimeout
	}

	return &Bus{
		cfg:         cfg,
		subscribers: make(map[uint64]chan coreevents.Event),
	}
}

// Subscribe registers a subscriber and returns its channel and an unsubscribe function.
func (b *Bus) Subscribe(buffer int) (<-chan coreevents.Event, func()) {
	if buffer <= 0 {
		buffer = b.cfg.SubscriberBuffer
	}

	ch := make(chan coreevents.Event, buffer)
	id := atomic.AddUint64(&b.nextID, 1)

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	b.subscribers[id] = ch
	b.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			sub, ok := b.subscribers[id]
			if ok {
				delete(b.subscribers, id)
				close(sub)
			}
			b.mu.Unlock()
		})
	}

	return ch, unsubscribe
}

// Publish fan-outs one event to all subscribers using the configured overflow policy.
func (b *Bus) Publish(ctx context.Context, evt coreevents.Event) error {
	if ctx == nil {
		ctx = context.Background()
	}

	b.published.Add(1)

	var deliveredCount int64
	var droppedCount int64
	var overflowCount int64
	var backpressureCount int64

	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return ErrClosed
	}

	var backpressureErr error
	for _, ch := range b.subscribers {
		delivered, dropped, overflowed, pressureErr := b.deliver(ctx, ch, evt)
		if delivered {
			deliveredCount++
		}
		droppedCount += dropped
		overflowCount += overflowed
		if pressureErr != nil {
			backpressureCount++
			if backpressureErr == nil {
				backpressureErr = pressureErr
			} else if errors.Is(pressureErr, context.Canceled) || errors.Is(pressureErr, context.DeadlineExceeded) {
				backpressureErr = pressureErr
			}
		}
	}
	b.mu.RUnlock()

	if deliveredCount != 0 {
		b.delivered.Add(deliveredCount)
	}
	if droppedCount != 0 {
		b.dropped.Add(droppedCount)
	}
	if overflowCount != 0 {
		b.overflow.Add(overflowCount)
	}
	if backpressureCount != 0 {
		b.backpressure.Add(backpressureCount)
	}

	if backpressureErr != nil {
		return backpressureErr
	}
	return nil
}

// Close shuts down the bus and closes all subscriber channels.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	for id, ch := range b.subscribers {
		close(ch)
		delete(b.subscribers, id)
	}
}

// Stats returns a point-in-time snapshot of bus counters.
func (b *Bus) Stats() Stats {
	b.mu.RLock()
	subscribers := len(b.subscribers)
	b.mu.RUnlock()

	return Stats{
		Published:    b.published.Load(),
		Delivered:    b.delivered.Load(),
		Dropped:      b.dropped.Load(),
		Overflow:     b.overflow.Load(),
		Backpressure: b.backpressure.Load(),
		Subscribers:  subscribers,
		Policy:       b.cfg.OverflowPolicy,
	}
}

func (b *Bus) deliver(ctx context.Context, ch chan coreevents.Event, evt coreevents.Event) (delivered bool, dropped int64, overflow int64, pressureErr error) {
	msg := evt.Clone()
	select {
	case ch <- msg:
		return true, 0, 0, nil
	default:
		overflow = 1
	}

	switch b.cfg.OverflowPolicy {
	case OverflowDropOldest:
		select {
		case <-ch:
			dropped = 1 // dropped one queued event
		default:
			return false, 1, overflow, nil // queue still unavailable
		}
		select {
		case ch <- msg:
			return true, dropped, overflow, nil
		default:
			return false, dropped + 1, overflow, nil
		}

	case OverflowBlock:
		timer := time.NewTimer(b.cfg.PublishTimeout)
		defer timer.Stop()
		select {
		case ch <- msg:
			return true, 0, overflow, nil
		case <-ctx.Done():
			return false, 1, overflow, ctx.Err()
		case <-timer.C:
			return false, 1, overflow, ErrBackpressure
		}

	case OverflowDropNewest:
		fallthrough
	default:
		return false, 1, overflow, nil
	}
}
