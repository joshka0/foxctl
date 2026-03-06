package runner_test

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/v2/core/run"
	"github.com/jkatigb/agentctl/internal/v2/runtime/runner"
	"github.com/jkatigb/agentctl/internal/v2/testkit/fakes"
)

func TestTurnRecord_PersistsIterationAndToolCallLineage(t *testing.T) {
	t.Parallel()

	store := fakes.NewFakeEventStore()
	clock := fakes.NewFakeClock(time.Date(2026, time.February, 18, 22, 0, 0, 0, time.UTC), time.Second)
	ids := fakes.NewFakeUUID("evt")
	model := fakes.NewFakeModel(runner.ModelResponse{
		Message: "done",
		ToolCalls: []run.ToolCall{
			{Name: "fs_read", Args: []byte(`{"path":"README.md"}`)},
		},
		Done: true,
	})
	tools := fakes.NewFakeToolExecutor()
	recorder := &captureTurnRecorder{}

	p := runner.New(runner.Config{
		EventStore:   store,
		Model:        model,
		ToolExecutor: tools,
		TurnRecorder: recorder,
		Now:          clock.Now,
		NewID:        ids.New,
	})

	_, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:         "run-009-1",
		TurnID:        "turn-009-1",
		Command:       "run",
		Prompt:        "read one file",
		ActorID:       "actor-overseer",
		CorrelationID: "trace-009-1",
		CausationID:   "cause-009-1",
		RequestID:     "req-009-1",
		MaxIterations: 2,
	})
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}

	saved, ok := recorder.Last()
	if !ok {
		t.Fatal("expected saved turn record")
	}
	if saved.ID != "turn-009-1" {
		t.Fatalf("turn id=%q want turn-009-1", saved.ID)
	}
	if saved.SessionID != "run-009-1" {
		t.Fatalf("session id=%q want run-009-1", saved.SessionID)
	}
	if len(saved.Iterations) != 1 {
		t.Fatalf("iterations=%d want 1", len(saved.Iterations))
	}
	iter := saved.Iterations[0]
	if iter.IterationIndex != 1 {
		t.Fatalf("iteration index=%d want 1", iter.IterationIndex)
	}
	if len(iter.ToolCalls) != 1 {
		t.Fatalf("tool calls=%d want 1", len(iter.ToolCalls))
	}
	call := iter.ToolCalls[0]
	if call.Name != "fs_read" {
		t.Fatalf("tool name=%q want fs_read", call.Name)
	}
	if call.CallID == "" {
		t.Fatal("tool call id is empty")
	}
	if call.ResultRef.Kind != "tool_result" {
		t.Fatalf("result ref kind=%q want tool_result", call.ResultRef.Kind)
	}
}

func TestTraceLineage_ParentSpanRelationships(t *testing.T) {
	t.Parallel()

	store := fakes.NewFakeEventStore()
	clock := fakes.NewFakeClock(time.Date(2026, time.February, 18, 22, 10, 0, 0, time.UTC), time.Second)
	ids := fakes.NewFakeUUID("evt")
	model := fakes.NewFakeModel(
		runner.ModelResponse{
			Message: "first",
			ToolCalls: []run.ToolCall{
				{Name: "code_search"},
			},
			Done: false,
		},
		runner.ModelResponse{
			Message: "second",
			ToolCalls: []run.ToolCall{
				{Name: "fs_read"},
				{Name: "think"},
			},
			Done: true,
		},
	)
	tools := fakes.NewFakeToolExecutor()
	recorder := &captureTurnRecorder{}

	p := runner.New(runner.Config{
		EventStore:   store,
		Model:        model,
		ToolExecutor: tools,
		TurnRecorder: recorder,
		Now:          clock.Now,
		NewID:        ids.New,
	})

	_, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:         "run-009-2",
		TurnID:        "turn-009-2",
		Command:       "ask",
		Prompt:        "trace this",
		ActorID:       "actor-overseer",
		CorrelationID: "trace-009-2",
		CausationID:   "cause-009-2",
		RequestID:     "req-009-2",
		MaxIterations: 3,
	})
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}

	saved, ok := recorder.Last()
	if !ok {
		t.Fatal("expected saved turn record")
	}
	if saved.TraceID != "trace-009-2" {
		t.Fatalf("trace_id=%q want trace-009-2", saved.TraceID)
	}
	if saved.RootSpanID == "" {
		t.Fatal("root span is empty")
	}

	for _, iter := range saved.Iterations {
		if iter.ParentSpanID != saved.RootSpanID {
			t.Fatalf("iteration %d parent=%q want root=%q", iter.IterationIndex, iter.ParentSpanID, saved.RootSpanID)
		}
		if iter.TraceID != saved.TraceID {
			t.Fatalf("iteration %d trace=%q want %q", iter.IterationIndex, iter.TraceID, saved.TraceID)
		}
		for _, call := range iter.ToolCalls {
			if call.ParentSpanID != iter.SpanID {
				t.Fatalf("tool %s parent=%q want iteration span=%q", call.CallID, call.ParentSpanID, iter.SpanID)
			}
			if call.TraceID != saved.TraceID {
				t.Fatalf("tool %s trace=%q want %q", call.CallID, call.TraceID, saved.TraceID)
			}
		}
	}
}

func TestTurnRecord_AssignsMonotonicTurnIndexPerRun(t *testing.T) {
	t.Parallel()

	store := fakes.NewFakeEventStore()
	clock := fakes.NewFakeClock(time.Date(2026, time.February, 18, 23, 0, 0, 0, time.UTC), time.Second)
	ids := fakes.NewFakeUUID("evt")
	model := fakes.NewFakeModel(
		runner.ModelResponse{Message: "first", Done: true},
		runner.ModelResponse{Message: "second", Done: true},
	)
	tools := fakes.NewFakeToolExecutor()
	recorder := &captureTurnRecorder{}

	p := runner.New(runner.Config{
		EventStore:   store,
		Model:        model,
		ToolExecutor: tools,
		TurnRecorder: recorder,
		Now:          clock.Now,
		NewID:        ids.New,
	})

	if _, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:         "run-turn-index",
		TurnID:        "turn-idx-1",
		Command:       "run",
		Prompt:        "first",
		CorrelationID: "trace-idx-1",
		CausationID:   "cause-idx-1",
		RequestID:     "req-idx-1",
	}); err != nil {
		t.Fatalf("RunTurn(first) error = %v", err)
	}

	if _, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:         "run-turn-index",
		TurnID:        "turn-idx-2",
		Command:       "run",
		Prompt:        "second",
		CorrelationID: "trace-idx-2",
		CausationID:   "cause-idx-2",
		RequestID:     "req-idx-2",
	}); err != nil {
		t.Fatalf("RunTurn(second) error = %v", err)
	}

	all := recorder.All()
	if len(all) != 2 {
		t.Fatalf("saved turns=%d want 2", len(all))
	}
	if all[0].TurnIndex != 1 {
		t.Fatalf("first turn_index=%d want 1", all[0].TurnIndex)
	}
	if all[1].TurnIndex != 2 {
		t.Fatalf("second turn_index=%d want 2", all[1].TurnIndex)
	}
}

func TestTurnRecord_AssignsUniqueTurnIndexUnderConcurrentRuns(t *testing.T) {
	t.Parallel()

	store := fakes.NewFakeEventStore()
	clock := fakes.NewFakeClock(time.Date(2026, time.February, 18, 23, 30, 0, 0, time.UTC), time.Second)
	ids := fakes.NewFakeUUID("evt")
	model := fakes.NewFakeModel().WithDefault(runner.ModelResponse{Message: "ok", Done: true})
	tools := fakes.NewFakeToolExecutor()
	recorder := &captureTurnRecorder{}

	p := runner.New(runner.Config{
		EventStore:   store,
		Model:        model,
		ToolExecutor: tools,
		TurnRecorder: recorder,
		Now:          clock.Now,
		NewID:        ids.New,
	})

	const concurrentTurns = 24
	var wg sync.WaitGroup
	errs := make(chan error, concurrentTurns)
	for i := 0; i < concurrentTurns; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.RunTurn(context.Background(), run.TurnInput{
				RunID:         "run-turn-concurrent",
				TurnID:        fmt.Sprintf("turn-conc-%02d", i+1),
				Command:       "run",
				Prompt:        "concurrent",
				CorrelationID: fmt.Sprintf("trace-conc-%02d", i+1),
				CausationID:   fmt.Sprintf("cause-conc-%02d", i+1),
				RequestID:     fmt.Sprintf("req-conc-%02d", i+1),
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("RunTurn(concurrent) error = %v", err)
		}
	}

	all := recorder.All()
	if len(all) != concurrentTurns {
		t.Fatalf("saved turns=%d want %d", len(all), concurrentTurns)
	}

	indexes := make([]int, 0, len(all))
	for _, turn := range all {
		indexes = append(indexes, turn.TurnIndex)
	}
	sort.Ints(indexes)
	for i := 0; i < concurrentTurns; i++ {
		want := i + 1
		if indexes[i] != want {
			t.Fatalf("turn indexes=%v want contiguous 1..%d", indexes, concurrentTurns)
		}
	}
}

type captureTurnRecorder struct {
	mu    sync.Mutex
	turns []run.TurnRecord
}

func (r *captureTurnRecorder) SaveTurn(_ context.Context, turn run.TurnRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turns = append(r.turns, turn.Clone())
	return nil
}

func (r *captureTurnRecorder) Last() (run.TurnRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.turns) == 0 {
		return run.TurnRecord{}, false
	}
	return r.turns[len(r.turns)-1].Clone(), true
}

func (r *captureTurnRecorder) All() []run.TurnRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]run.TurnRecord, 0, len(r.turns))
	for _, turn := range r.turns {
		out = append(out, turn.Clone())
	}
	return out
}

func (r *captureTurnRecorder) ListTurns(_ context.Context, sessionID string, opts run.TurnListOptions) ([]run.TurnRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]run.TurnRecord, 0, len(r.turns))
	for _, turn := range r.turns {
		if sessionID != "" && turn.SessionID != sessionID {
			continue
		}
		out = append(out, turn.Clone())
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TurnIndex == out[j].TurnIndex {
			return out[i].ID < out[j].ID
		}
		return out[i].TurnIndex < out[j].TurnIndex
	})
	if !opts.Asc {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	if opts.Limit > 0 && len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	return out, nil
}
