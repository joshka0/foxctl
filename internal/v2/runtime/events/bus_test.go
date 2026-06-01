package events_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"testing/quick"
	"time"

	coreevents "github.com/joshka0/foxctl/internal/v2/core/events"
	runtimeevents "github.com/joshka0/foxctl/internal/v2/runtime/events"
)

func TestEventBus_BoundedQueue_OverflowPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		policy           runtimeevents.OverflowPolicy
		wantIDs          []string
		wantDelivered    int64
		wantDropped      int64
		wantOverflowHits int64
	}{
		{
			name:             "drop_newest",
			policy:           runtimeevents.OverflowDropNewest,
			wantIDs:          []string{"evt-1"},
			wantDelivered:    1,
			wantDropped:      1,
			wantOverflowHits: 1,
		},
		{
			name:             "drop_oldest",
			policy:           runtimeevents.OverflowDropOldest,
			wantIDs:          []string{"evt-2"},
			wantDelivered:    2,
			wantDropped:      1,
			wantOverflowHits: 1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bus := runtimeevents.NewBus(runtimeevents.Config{
				SubscriberBuffer: 1,
				OverflowPolicy:   tt.policy,
			})
			defer bus.Close()

			ch, unsubscribe := bus.Subscribe(1)
			defer unsubscribe()

			if err := bus.Publish(context.Background(), testEvent("evt-1")); err != nil {
				t.Fatalf("Publish(evt-1) error = %v", err)
			}
			if err := bus.Publish(context.Background(), testEvent("evt-2")); err != nil {
				t.Fatalf("Publish(evt-2) error = %v", err)
			}

			gotIDs := drainIDs(ch)
			if len(gotIDs) != len(tt.wantIDs) {
				t.Fatalf("buffered ids len=%d want=%d ids=%v", len(gotIDs), len(tt.wantIDs), gotIDs)
			}
			for i := range tt.wantIDs {
				if gotIDs[i] != tt.wantIDs[i] {
					t.Fatalf("buffered ids[%d]=%q want=%q (all=%v)", i, gotIDs[i], tt.wantIDs[i], gotIDs)
				}
			}

			stats := bus.Stats()
			if stats.Published != 2 {
				t.Fatalf("Published=%d want=2", stats.Published)
			}
			if stats.Delivered != tt.wantDelivered {
				t.Fatalf("Delivered=%d want=%d", stats.Delivered, tt.wantDelivered)
			}
			if stats.Dropped != tt.wantDropped {
				t.Fatalf("Dropped=%d want=%d", stats.Dropped, tt.wantDropped)
			}
			if stats.Overflow != tt.wantOverflowHits {
				t.Fatalf("Overflow=%d want=%d", stats.Overflow, tt.wantOverflowHits)
			}
			if stats.Policy != tt.policy {
				t.Fatalf("Policy=%q want=%q", stats.Policy, tt.policy)
			}
		})
	}
}

func TestEventBus_BoundedQueueOverflowProperty(t *testing.T) {
	t.Parallel()

	property := func(rawBuffer uint8, rawPublishes uint8, dropOldest bool) bool {
		buffer := int(rawBuffer%8) + 1
		publishes := int(rawPublishes % 24)
		policy := runtimeevents.OverflowDropNewest
		if dropOldest {
			policy = runtimeevents.OverflowDropOldest
		}

		bus := runtimeevents.NewBus(runtimeevents.Config{
			SubscriberBuffer: buffer,
			OverflowPolicy:   policy,
		})
		defer bus.Close()

		ch, unsubscribe := bus.Subscribe(buffer)
		defer unsubscribe()

		ids := make([]string, 0, publishes)
		for i := 0; i < publishes; i++ {
			id := fmt.Sprintf("evt-%03d", i)
			ids = append(ids, id)
			if err := bus.Publish(context.Background(), testEvent(id)); err != nil {
				return false
			}
		}

		gotIDs := drainIDs(ch)
		wantIDs := expectedBufferedIDs(ids, buffer, policy)
		if !slices.Equal(gotIDs, wantIDs) {
			return false
		}

		overflow := max(0, publishes-buffer)
		stats := bus.Stats()
		if stats.Published != int64(publishes) {
			return false
		}
		if stats.Dropped != int64(overflow) || stats.Overflow != int64(overflow) {
			return false
		}
		if policy == runtimeevents.OverflowDropOldest {
			return stats.Delivered == int64(publishes)
		}
		return stats.Delivered == int64(min(publishes, buffer))
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 128}); err != nil {
		t.Fatalf("bounded queue overflow property failed: %v", err)
	}
}

func TestEventBus_BlockPolicyReportsBackpressure(t *testing.T) {
	t.Parallel()

	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 1,
		OverflowPolicy:   runtimeevents.OverflowBlock,
		PublishTimeout:   5 * time.Millisecond,
	})
	defer bus.Close()

	ch, unsubscribe := bus.Subscribe(1)
	defer unsubscribe()

	if err := bus.Publish(context.Background(), testEvent("evt-1")); err != nil {
		t.Fatalf("Publish(evt-1) error = %v", err)
	}
	if err := bus.Publish(context.Background(), testEvent("evt-2")); !errors.Is(err, runtimeevents.ErrBackpressure) {
		t.Fatalf("Publish(evt-2) error = %v want %v", err, runtimeevents.ErrBackpressure)
	}

	if gotIDs := drainIDs(ch); !slices.Equal(gotIDs, []string{"evt-1"}) {
		t.Fatalf("buffered ids=%v want [evt-1]", gotIDs)
	}

	stats := bus.Stats()
	if stats.Published != 2 {
		t.Fatalf("Published=%d want=2", stats.Published)
	}
	if stats.Delivered != 1 {
		t.Fatalf("Delivered=%d want=1", stats.Delivered)
	}
	if stats.Dropped != 1 {
		t.Fatalf("Dropped=%d want=1", stats.Dropped)
	}
	if stats.Overflow != 1 {
		t.Fatalf("Overflow=%d want=1", stats.Overflow)
	}
	if stats.Backpressure != 1 {
		t.Fatalf("Backpressure=%d want=1", stats.Backpressure)
	}
}

func TestEventBus_BlockPolicyReportsContextCancellation(t *testing.T) {
	t.Parallel()

	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 1,
		OverflowPolicy:   runtimeevents.OverflowBlock,
		PublishTimeout:   time.Second,
	})
	defer bus.Close()

	ch, unsubscribe := bus.Subscribe(1)
	defer unsubscribe()

	if err := bus.Publish(context.Background(), testEvent("evt-1")); err != nil {
		t.Fatalf("Publish(evt-1) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := bus.Publish(ctx, testEvent("evt-2")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Publish(evt-2) error = %v want %v", err, context.Canceled)
	}

	if gotIDs := drainIDs(ch); !slices.Equal(gotIDs, []string{"evt-1"}) {
		t.Fatalf("buffered ids=%v want [evt-1]", gotIDs)
	}

	stats := bus.Stats()
	if stats.Backpressure != 1 {
		t.Fatalf("Backpressure=%d want=1", stats.Backpressure)
	}
	if stats.Dropped != 1 {
		t.Fatalf("Dropped=%d want=1", stats.Dropped)
	}
	if stats.Overflow != 1 {
		t.Fatalf("Overflow=%d want=1", stats.Overflow)
	}
}

func TestEventBus_DeliversPayloadClone(t *testing.T) {
	t.Parallel()

	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 1,
		OverflowPolicy:   runtimeevents.OverflowDropNewest,
	})
	defer bus.Close()

	ch, unsubscribe := bus.Subscribe(1)
	defer unsubscribe()

	payload := []byte(`{"status":"original"}`)
	evt := testEvent("evt-1")
	evt.Payload = payload
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	copy(payload, []byte(`{"status":"mutated"}`))

	got := <-ch
	if string(got.Payload) != `{"status":"original"}` {
		t.Fatalf("payload=%s want original payload copy", got.Payload)
	}
}

func TestEventBus_NoSubscriberDeadlock(t *testing.T) {
	t.Parallel()

	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 1,
		OverflowPolicy:   runtimeevents.OverflowDropNewest,
	})
	defer bus.Close()

	_, slowUnsubscribe := bus.Subscribe(1) // intentionally never consumed
	defer slowUnsubscribe()

	fastCh, fastUnsubscribe := bus.Subscribe(16)
	defer fastUnsubscribe()

	var fastReceived atomic.Int64
	stopReader := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stopReader:
				return
			case _, ok := <-fastCh:
				if !ok {
					return
				}
				fastReceived.Add(1)
			}
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 500; i++ {
			_ = bus.Publish(context.Background(), testEvent(fmt.Sprintf("evt-%d", i)))
		}
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("publish loop blocked; expected non-blocking fanout with slow subscriber")
	}

	deadline := time.Now().Add(200 * time.Millisecond)
	for fastReceived.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if fastReceived.Load() == 0 {
		t.Fatal("fast subscriber received no events")
	}

	close(stopReader)
	select {
	case <-readerDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("reader goroutine did not stop")
	}

	stats := bus.Stats()
	if stats.Overflow == 0 {
		t.Fatal("expected overflow count > 0 with slow subscriber")
	}
	if stats.Backpressure != 0 {
		t.Fatalf("Backpressure=%d want=0 in drop_newest mode", stats.Backpressure)
	}
}

func expectedBufferedIDs(ids []string, buffer int, policy runtimeevents.OverflowPolicy) []string {
	if len(ids) <= buffer {
		return append([]string(nil), ids...)
	}
	switch policy {
	case runtimeevents.OverflowDropOldest:
		return append([]string(nil), ids[len(ids)-buffer:]...)
	default:
		return append([]string(nil), ids[:buffer]...)
	}
}

func testEvent(id string) coreevents.Event {
	return coreevents.Event{
		ID:        id,
		StreamID:  "run-1",
		EventType: coreevents.EventTurnRecorded,
	}
}

func drainIDs(ch <-chan coreevents.Event) []string {
	var out []string
	for {
		select {
		case evt := <-ch:
			out = append(out, evt.ID)
		default:
			return out
		}
	}
}
