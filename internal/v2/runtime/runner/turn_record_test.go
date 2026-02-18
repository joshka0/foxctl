package runner_test

import (
	"context"
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
