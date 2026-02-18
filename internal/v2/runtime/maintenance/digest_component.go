package maintenance

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/observability"
	coreevents "github.com/jkatigb/agentctl/internal/v2/core/events"
	"github.com/jkatigb/agentctl/internal/v2/runtime/snapshots"
)

var (
	// ErrMissingBus indicates a nil event subscriber dependency.
	ErrMissingBus = errors.New("v2 maintenance: missing event subscriber")
	// ErrMissingStore indicates a nil snapshot store dependency.
	ErrMissingStore = errors.New("v2 maintenance: missing snapshot store")
)

const (
	defaultSubscriberBuffer = 64
)

// EventSubscriber subscribes to runtime event fanout.
type EventSubscriber interface {
	Subscribe(buffer int) (<-chan coreevents.Event, func())
}

// SnapshotStore provides immutable snapshot load/store semantics.
type SnapshotStore interface {
	Load() snapshots.RuntimeSnapshot
	Store(snapshot snapshots.RuntimeSnapshot)
}

// DigestApplyFunc updates a runtime snapshot with one event.
type DigestApplyFunc func(ctx context.Context, evt coreevents.Event, current snapshots.RuntimeSnapshot, now time.Time) (snapshots.RuntimeSnapshot, error)

// Config wires the digest projector component.
type Config struct {
	Bus     EventSubscriber
	Store   SnapshotStore
	Buffer  int
	Now     func() time.Time
	Apply   DigestApplyFunc
	OnError func(error)
}

// DigestComponent projects runtime events into digest snapshots.
//
// It is intentionally non-blocking with respect to turn execution: event loss/backpressure is
// handled by the upstream bounded bus, and apply errors are observed but do not stop the loop.
type DigestComponent struct {
	bus     EventSubscriber
	store   SnapshotStore
	buffer  int
	now     func() time.Time
	apply   DigestApplyFunc
	onError func(error)
}

// NewDigestComponent builds a digest projection component.
func NewDigestComponent(cfg Config) *DigestComponent {
	if cfg.Buffer <= 0 {
		cfg.Buffer = defaultSubscriberBuffer
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.Apply == nil {
		cfg.Apply = DefaultDigestApply
	}
	if cfg.OnError == nil {
		cfg.OnError = func(err error) {
			if err == nil {
				return
			}
			observability.Emit(context.Background(), observability.NewEvent("v2.runtime.maintenance.error").
				WithComponent(observability.ComponentAgent).
				Error(err, 0))
		}
	}

	return &DigestComponent{
		bus:     cfg.Bus,
		store:   cfg.Store,
		buffer:  cfg.Buffer,
		now:     cfg.Now,
		apply:   cfg.Apply,
		onError: cfg.OnError,
	}
}

// Run consumes runtime events and continuously publishes digest snapshots.
func (c *DigestComponent) Run(ctx context.Context) error {
	if c == nil || c.bus == nil {
		return ErrMissingBus
	}
	if c.store == nil {
		return ErrMissingStore
	}

	eventsCh, unsubscribe := c.bus.Subscribe(c.buffer)
	defer unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return nil
		case evt, ok := <-eventsCh:
			if !ok {
				return nil
			}
			c.handleEvent(ctx, evt)
		}
	}
}

func (c *DigestComponent) handleEvent(ctx context.Context, evt coreevents.Event) {
	current := c.store.Load()
	now := c.now().UTC()

	next, err := c.apply(ctx, evt, current, now)
	if err != nil {
		c.onError(err)
		return
	}

	if next.Version <= current.Version {
		next.Version = current.Version + 1
	}
	if next.UpdatedAt.IsZero() {
		next.UpdatedAt = now
	}
	c.store.Store(next)
}

// DefaultDigestApply projects one runtime event into a digest snapshot.
func DefaultDigestApply(_ context.Context, evt coreevents.Event, current snapshots.RuntimeSnapshot, now time.Time) (snapshots.RuntimeSnapshot, error) {
	next := current
	next.UpdatedAt = now

	next.Digest.TotalEvents++
	next.Digest.LastEventID = strings.TrimSpace(evt.ID)
	next.Digest.LastEventType = evt.EventType
	if next.Digest.RunStatus == nil {
		next.Digest.RunStatus = make(map[string]string)
	}

	runID := strings.TrimSpace(evt.StreamID)
	switch evt.EventType {
	case coreevents.EventRunStarted:
		next.Digest.RunsStarted++
		if runID != "" {
			next.Digest.RunStatus[runID] = "running"
		}
	case coreevents.EventRunCompleted:
		next.Digest.RunsCompleted++
		if runID != "" {
			next.Digest.RunStatus[runID] = "completed"
		}
	case coreevents.EventRunFailed:
		next.Digest.RunsFailed++
		if runID != "" {
			next.Digest.RunStatus[runID] = "failed"
		}
	case coreevents.EventTurnRecorded:
		next.Digest.TurnsRecorded++
	}

	return next, nil
}
