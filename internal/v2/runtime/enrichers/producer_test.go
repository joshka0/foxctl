package enrichers_test

import (
	"context"
	"sync"
	"testing"
	"time"

	coreevents "github.com/joshka0/foxctl/internal/v2/core/events"
	"github.com/joshka0/foxctl/internal/v2/core/run"
	"github.com/joshka0/foxctl/internal/v2/runtime/enrichers"
	runtimeevents "github.com/joshka0/foxctl/internal/v2/runtime/events"
)

func TestProducer_TurnRecordedEnqueuesConfiguredArtifacts(t *testing.T) {
	t.Parallel()

	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 16,
		OverflowPolicy:   runtimeevents.OverflowDropNewest,
	})
	queue := enrichers.NewQueue(16)
	defer queue.Close()

	reader := &fakeTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-100": {ID: "turn-100", CorrelationID: "corr-100"},
		},
	}
	producer := enrichers.NewProducer(enrichers.ProducerConfig{
		Bus:        bus,
		Queue:      queue,
		TurnReader: reader,
		ArtifactSpecs: []enrichers.ArtifactSpec{
			{Type: "embedding", Version: "v2"},
			{Type: "annotation", Version: "v1"},
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- producer.Run(ctx) }()

	waitForCondition(t, 2*time.Second, func() bool {
		return bus.Stats().Subscribers > 0
	})

	if err := bus.Publish(context.Background(), coreevents.Event{
		ID:         "evt-turn-100",
		StreamID:   "run-100",
		StreamType: coreevents.StreamTypeRun,
		EventType:  coreevents.EventTurnRecorded,
		Payload: coreevents.MustMarshalPayload(coreevents.TurnRecordedPayload{
			TurnID: "turn-100",
		}),
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	jobs := drainJobs(t, queue.Jobs(), 2, 2*time.Second)
	if len(jobs) != 2 {
		t.Fatalf("jobs len=%d want 2", len(jobs))
	}
	if jobs[0].TurnID != "turn-100" || jobs[1].TurnID != "turn-100" {
		t.Fatalf("unexpected turn ids: %+v", jobs)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("producer Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for producer shutdown")
	}
}

func TestProducer_QueueFullReportsErrorWithoutBlocking(t *testing.T) {
	t.Parallel()

	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 16,
		OverflowPolicy:   runtimeevents.OverflowDropNewest,
	})
	queue := enrichers.NewQueue(1)
	defer queue.Close()

	// Fill the queue so producer enqueue attempts are rejected.
	_ = queue.Enqueue(enrichers.NewJob(run.TurnRecord{ID: "turn-pre"}, "embedding", "v1"))

	reader := &fakeTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-200": {ID: "turn-200"},
		},
	}
	errs := make(chan error, 8)
	producer := enrichers.NewProducer(enrichers.ProducerConfig{
		Bus:        bus,
		Queue:      queue,
		TurnReader: reader,
		ArtifactSpecs: []enrichers.ArtifactSpec{
			{Type: "embedding", Version: "v1"},
			{Type: "learning", Version: "v1"},
		},
		OnError: func(err error) {
			if err == nil {
				return
			}
			select {
			case errs <- err:
			default:
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- producer.Run(ctx) }()

	waitForCondition(t, 2*time.Second, func() bool {
		return bus.Stats().Subscribers > 0
	})

	start := time.Now()
	if err := bus.Publish(context.Background(), coreevents.Event{
		ID:         "evt-turn-200",
		StreamID:   "run-200",
		StreamType: coreevents.StreamTypeRun,
		EventType:  coreevents.EventTurnRecorded,
		Payload: coreevents.MustMarshalPayload(coreevents.TurnRecordedPayload{
			TurnID: "turn-200",
		}),
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("publish blocked too long: %s", elapsed)
	}

	waitForCondition(t, 2*time.Second, func() bool {
		return len(errs) > 0
	})

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("producer Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for producer shutdown")
	}
}

type fakeTurnReader struct {
	mu    sync.Mutex
	turns map[string]run.TurnRecord
}

func (f *fakeTurnReader) GetTurn(_ context.Context, turnID string) (run.TurnRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	turn, ok := f.turns[turnID]
	if !ok {
		return run.TurnRecord{}, run.ErrTurnNotFound
	}
	return turn.Clone(), nil
}

func drainJobs(t *testing.T, jobs <-chan enrichers.Job, want int, timeout time.Duration) []enrichers.Job {
	t.Helper()
	out := make([]enrichers.Job, 0, want)
	deadline := time.After(timeout)
	for len(out) < want {
		select {
		case job := <-jobs:
			out = append(out, job)
		case <-deadline:
			return out
		}
	}
	return out
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
