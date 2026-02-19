package runner_test

import (
	"context"
	stderrors "errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coreevents "github.com/jkatigb/agentctl/internal/v2/core/events"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
	"github.com/jkatigb/agentctl/internal/v2/runtime/enrichers"
	runtimeevents "github.com/jkatigb/agentctl/internal/v2/runtime/events"
	"github.com/jkatigb/agentctl/internal/v2/runtime/runner"
	"github.com/jkatigb/agentctl/internal/v2/testkit/fakes"
)

func TestPipeline_EventBusPublishFailure_DoesNotFailTurn(t *testing.T) {
	t.Parallel()

	store := fakes.NewFakeEventStore()
	clock := fakes.NewFakeClock(time.Date(2026, time.February, 18, 23, 0, 0, 0, time.UTC), time.Second)
	ids := fakes.NewFakeUUID("evt")
	model := fakes.NewFakeModel(runner.ModelResponse{Message: "ok", Done: true})
	tools := fakes.NewFakeToolExecutor()

	recorder := &memoryTurnStore{turns: make(map[string]run.TurnRecord)}
	var eventErrors atomic.Int64

	p := runner.New(runner.Config{
		EventStore:   store,
		EventBus:     failingEventBus{err: stderrors.New("bus publish failed")},
		Model:        model,
		ToolExecutor: tools,
		TurnRecorder: recorder,
		Now:          clock.Now,
		NewID:        ids.New,
		OnEventError: func(err error) {
			if err != nil {
				eventErrors.Add(1)
			}
		},
	})

	out, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:         "run-bus-failure",
		TurnID:        "turn-bus-failure",
		Command:       "run",
		Prompt:        "hello",
		ActorID:       "actor-overseer",
		CorrelationID: "corr-bus-failure",
		CausationID:   "cause-bus-failure",
		RequestID:     "req-bus-failure",
		MaxIterations: 1,
	})
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}
	if out.TurnID != "turn-bus-failure" {
		t.Fatalf("turn id=%q want turn-bus-failure", out.TurnID)
	}
	if eventErrors.Load() == 0 {
		t.Fatal("expected non-fatal event bus publish errors to be observed")
	}
	assertEventTypes(t, store.Events(),
		coreevents.EventRunStarted,
		coreevents.EventTurnRecorded,
		coreevents.EventRunCompleted,
	)
}

func TestPipeline_TurnRecordedTriggersAsyncEnricherFailureEvent(t *testing.T) {
	t.Parallel()

	store := fakes.NewFakeEventStore()
	clock := fakes.NewFakeClock(time.Date(2026, time.February, 18, 23, 10, 0, 0, time.UTC), time.Second)
	ids := fakes.NewFakeUUID("evt")
	model := fakes.NewFakeModel(runner.ModelResponse{Message: "ok", Done: true})
	tools := fakes.NewFakeToolExecutor()

	turnStore := &memoryTurnStore{turns: make(map[string]run.TurnRecord)}
	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 32,
		OverflowPolicy:   runtimeevents.OverflowDropNewest,
	})
	queue := enrichers.NewQueue(32)
	defer queue.Close()

	producer := enrichers.NewProducer(enrichers.ProducerConfig{
		Bus:        bus,
		Queue:      queue,
		TurnReader: turnStore,
		ArtifactSpecs: []enrichers.ArtifactSpec{
			{Type: "embedding", Version: "v1"},
		},
	})
	worker := enrichers.NewWorker(enrichers.Config{
		Queue: queue,
		Enricher: enrichers.EnricherFunc(func(context.Context, enrichers.Job) error {
			return stderrors.New("forced enrich failure")
		}),
		EventStore: store,
		Now:        clock.Now,
		NewID:      ids.New,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	producerDone := make(chan error, 1)
	workerDone := make(chan error, 1)
	go func() { producerDone <- producer.Run(ctx) }()
	go func() { workerDone <- worker.Run(ctx) }()

	waitFor(t, 2*time.Second, func() bool { return bus.Stats().Subscribers > 0 })

	p := runner.New(runner.Config{
		EventStore:   store,
		EventBus:     bus,
		Model:        model,
		ToolExecutor: tools,
		TurnRecorder: turnStore,
		Now:          clock.Now,
		NewID:        ids.New,
	})

	start := time.Now()
	out, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:         "run-enricher-001",
		TurnID:        "turn-enricher-001",
		Command:       "run",
		Prompt:        "trigger enrichers",
		ActorID:       "actor-overseer",
		CorrelationID: "corr-enricher-001",
		CausationID:   "cause-enricher-001",
		RequestID:     "req-enricher-001",
		MaxIterations: 1,
	})
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("RunTurn blocked too long: %s", elapsed)
	}
	if out.TurnID != "turn-enricher-001" {
		t.Fatalf("turn id=%q want turn-enricher-001", out.TurnID)
	}

	waitFor(t, 2*time.Second, func() bool {
		for _, evt := range store.Events() {
			if evt.EventType == coreevents.EventArtifactFailed {
				return true
			}
		}
		return false
	})

	cancel()
	select {
	case err := <-producerDone:
		if err != nil {
			t.Fatalf("producer Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for producer shutdown")
	}
	select {
	case err := <-workerDone:
		if err != nil {
			t.Fatalf("worker Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for worker shutdown")
	}
}

type failingEventBus struct {
	err error
}

func (b failingEventBus) Publish(context.Context, coreevents.Event) error {
	return b.err
}

type memoryTurnStore struct {
	mu    sync.Mutex
	turns map[string]run.TurnRecord
}

func (s *memoryTurnStore) SaveTurn(_ context.Context, turn run.TurnRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.turns == nil {
		s.turns = make(map[string]run.TurnRecord)
	}
	s.turns[turn.ID] = turn.Clone()
	return nil
}

func (s *memoryTurnStore) GetTurn(_ context.Context, turnID string) (run.TurnRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	turn, ok := s.turns[turnID]
	if !ok {
		return run.TurnRecord{}, run.ErrTurnNotFound
	}
	return turn.Clone(), nil
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
