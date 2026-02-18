package maintenance_test

import (
	"context"
	stderrors "errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	coreevents "github.com/jkatigb/agentctl/internal/v2/core/events"
	runtimeevents "github.com/jkatigb/agentctl/internal/v2/runtime/events"
	"github.com/jkatigb/agentctl/internal/v2/runtime/maintenance"
	"github.com/jkatigb/agentctl/internal/v2/runtime/snapshots"
)

func TestMaintenanceComponent_PublishesSnapshot(t *testing.T) {
	t.Parallel()

	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 64,
		OverflowPolicy:   runtimeevents.OverflowDropNewest,
	})
	defer bus.Close()

	store := snapshots.NewStore()
	component := maintenance.NewDigestComponent(maintenance.Config{
		Bus:   bus,
		Store: store,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- component.Run(ctx) }()
	waitForCondition(t, 2*time.Second, func() bool {
		return bus.Stats().Subscribers > 0
	})

	events := []coreevents.Event{
		event("evt-1", "run-1", coreevents.EventRunStarted),
		event("evt-2", "run-1", coreevents.EventTurnRecorded),
		event("evt-3", "run-1", coreevents.EventRunCompleted),
	}
	for _, evt := range events {
		if err := bus.Publish(context.Background(), evt); err != nil {
			t.Fatalf("Publish(%s) error = %v", evt.ID, err)
		}
	}

	waitForCondition(t, 2*time.Second, func() bool {
		snap := store.Load()
		return snap.Digest.TotalEvents == int64(len(events))
	})

	snap := store.Load()
	if snap.Version != int64(len(events)) {
		t.Fatalf("version=%d want=%d", snap.Version, len(events))
	}
	if snap.Digest.RunsStarted != 1 || snap.Digest.RunsCompleted != 1 || snap.Digest.TurnsRecorded != 1 {
		t.Fatalf("unexpected digest counters %+v", snap.Digest)
	}
	if got := snap.Digest.RunStatus["run-1"]; got != "completed" {
		t.Fatalf("run status=%q want completed", got)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for maintenance shutdown")
	}
}

func TestMaintenanceFailure_DoesNotBlockRunEngine(t *testing.T) {
	t.Parallel()

	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 1,
		OverflowPolicy:   runtimeevents.OverflowDropNewest,
	})
	defer bus.Close()

	store := snapshots.NewStore()
	var failures atomic.Int64
	component := maintenance.NewDigestComponent(maintenance.Config{
		Bus:   bus,
		Store: store,
		Apply: func(context.Context, coreevents.Event, snapshots.RuntimeSnapshot, time.Time) (snapshots.RuntimeSnapshot, error) {
			time.Sleep(5 * time.Millisecond)
			return snapshots.RuntimeSnapshot{}, stderrors.New("digest projector failed")
		},
		OnError: func(err error) {
			if err != nil {
				failures.Add(1)
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- component.Run(ctx) }()
	waitForCondition(t, 2*time.Second, func() bool {
		return bus.Stats().Subscribers > 0
	})

	start := time.Now()
	for i := 0; i < 500; i++ {
		if err := bus.Publish(context.Background(), event(fmt.Sprintf("evt-%03d", i), "run-1", coreevents.EventTurnRecorded)); err != nil {
			t.Fatalf("Publish() error = %v", err)
		}
	}
	elapsed := time.Since(start)
	if elapsed > 400*time.Millisecond {
		t.Fatalf("publish loop blocked too long (%s)", elapsed)
	}

	waitForCondition(t, 2*time.Second, func() bool {
		return failures.Load() > 0
	})

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for maintenance shutdown")
	}
}

func TestSnapshotProjection_Parity(t *testing.T) {
	t.Parallel()

	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 64,
		OverflowPolicy:   runtimeevents.OverflowDropNewest,
	})
	defer bus.Close()

	store := snapshots.NewStore()
	component := maintenance.NewDigestComponent(maintenance.Config{
		Bus:   bus,
		Store: store,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- component.Run(ctx) }()
	waitForCondition(t, 2*time.Second, func() bool {
		return bus.Stats().Subscribers > 0
	})

	events := []coreevents.Event{
		event("evt-1", "run-1", coreevents.EventRunStarted),
		event("evt-2", "run-1", coreevents.EventTurnRecorded),
		event("evt-3", "run-1", coreevents.EventRunCompleted),
		event("evt-4", "run-2", coreevents.EventRunStarted),
		event("evt-5", "run-2", coreevents.EventRunFailed),
		event("evt-6", "run-2", coreevents.EventTurnRecorded),
	}
	for _, evt := range events {
		if err := bus.Publish(context.Background(), evt); err != nil {
			t.Fatalf("Publish(%s) error = %v", evt.ID, err)
		}
	}

	waitForCondition(t, 2*time.Second, func() bool {
		return store.Load().Digest.TotalEvents == int64(len(events))
	})

	snap := store.Load()
	if snap.Digest.TotalEvents != int64(len(events)) {
		t.Fatalf("total_events=%d want=%d", snap.Digest.TotalEvents, len(events))
	}
	if snap.Digest.RunsStarted != 2 {
		t.Fatalf("runs_started=%d want=2", snap.Digest.RunsStarted)
	}
	if snap.Digest.RunsCompleted != 1 {
		t.Fatalf("runs_completed=%d want=1", snap.Digest.RunsCompleted)
	}
	if snap.Digest.RunsFailed != 1 {
		t.Fatalf("runs_failed=%d want=1", snap.Digest.RunsFailed)
	}
	if snap.Digest.TurnsRecorded != 2 {
		t.Fatalf("turns_recorded=%d want=2", snap.Digest.TurnsRecorded)
	}
	if got := snap.Digest.RunStatus["run-1"]; got != "completed" {
		t.Fatalf("run-1 status=%q want completed", got)
	}
	if got := snap.Digest.RunStatus["run-2"]; got != "failed" {
		t.Fatalf("run-2 status=%q want failed", got)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for maintenance shutdown")
	}
}

func event(id, runID string, kind coreevents.EventType) coreevents.Event {
	return coreevents.Event{
		ID:         id,
		StreamID:   runID,
		StreamType: coreevents.StreamTypeRun,
		EventType:  kind,
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout waiting for condition")
}
