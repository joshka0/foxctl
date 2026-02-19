package enrichers_test

import (
	"context"
	stderrors "errors"
	"fmt"
	"testing"
	"time"

	coreevents "github.com/jkatigb/agentctl/internal/v2/core/events"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
	"github.com/jkatigb/agentctl/internal/v2/runtime/enrichers"
	"github.com/jkatigb/agentctl/internal/v2/testkit/fakes"
)

func TestEnricherQueue_IdempotentByArtifactVersion(t *testing.T) {
	t.Parallel()

	queue := enrichers.NewQueue(4)
	defer queue.Close()

	turn := run.TurnRecord{ID: "turn-1"}

	added := queue.Enqueue(enrichers.NewJob(turn, "embedding", "v1"))
	if !added {
		t.Fatal("expected first enqueue to be accepted")
	}
	added = queue.Enqueue(enrichers.NewJob(turn, "embedding", "v1"))
	if added {
		t.Fatal("expected duplicate key enqueue to be rejected")
	}
	added = queue.Enqueue(enrichers.NewJob(turn, "embedding", "v2"))
	if !added {
		t.Fatal("expected new artifact version enqueue to be accepted")
	}
}

func TestEnricherFailure_DoesNotBlockTurnCompletion(t *testing.T) {
	t.Parallel()

	queue := enrichers.NewQueue(16)
	defer queue.Close()

	store := fakes.NewFakeEventStore()
	worker := enrichers.NewWorker(enrichers.Config{
		Queue: queue,
		Enricher: enrichers.EnricherFunc(func(context.Context, enrichers.Job) error {
			time.Sleep(10 * time.Millisecond)
			return stderrors.New("boom")
		}),
		EventStore: store,
		Now: func() time.Time {
			return time.Date(2026, time.February, 18, 22, 20, 0, 0, time.UTC)
		},
		NewID: fakes.NewFakeUUID("artifact").New,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	start := time.Now()
	for i := 0; i < 200; i++ {
		// "Turn completion path": enqueue and continue; never wait for enrichment.
		_ = queue.Enqueue(enrichers.NewJob(run.TurnRecord{
			ID:            fmt.Sprintf("turn-%03d", i),
			CorrelationID: "trace-1",
			CausationID:   "cause-1",
			RequestID:     "req-1",
			ActorID:       "actor-overseer",
			Command:       "run",
		}, "embedding", "v1"))
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("enqueue loop blocked too long: %s", elapsed)
	}

	waitFor(t, 2*time.Second, func() bool {
		events := store.Events()
		for _, evt := range events {
			if evt.EventType == coreevents.EventArtifactFailed {
				return true
			}
		}
		return false
	})

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("worker Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for worker shutdown")
	}
}

func TestEnricherQueue_KeyReleasedAfterProcessing(t *testing.T) {
	t.Parallel()

	queue := enrichers.NewQueue(8)
	defer queue.Close()

	store := fakes.NewFakeEventStore()
	worker := enrichers.NewWorker(enrichers.Config{
		Queue: queue,
		Enricher: enrichers.EnricherFunc(func(context.Context, enrichers.Job) error {
			return stderrors.New("still retryable")
		}),
		EventStore: store,
		Now: func() time.Time {
			return time.Date(2026, time.February, 18, 22, 30, 0, 0, time.UTC)
		},
		NewID: fakes.NewFakeUUID("artifact").New,
	})

	job := enrichers.NewJob(run.TurnRecord{ID: "turn-retry"}, "embedding", "v1")
	if accepted := queue.Enqueue(job); !accepted {
		t.Fatal("expected first enqueue accepted")
	}
	if accepted := queue.Enqueue(job); accepted {
		t.Fatal("expected duplicate enqueue rejected while in flight")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	waitFor(t, 2*time.Second, func() bool {
		events := store.Events()
		for _, evt := range events {
			if evt.EventType == coreevents.EventArtifactFailed {
				return true
			}
		}
		return false
	})

	if accepted := queue.Enqueue(job); !accepted {
		t.Fatal("expected enqueue accepted after worker released dedupe key")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("worker Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for worker shutdown")
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
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
