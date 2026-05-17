package runner_test

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"path/filepath"
	"reflect"
	stdruntime "runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
	tursoevents "github.com/joshka0/foxctl/internal/v2/adapters/turso/events"
	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
	"github.com/joshka0/foxctl/internal/v2/core/events"
	"github.com/joshka0/foxctl/internal/v2/core/run"
	coretool "github.com/joshka0/foxctl/internal/v2/core/tool"
	"github.com/joshka0/foxctl/internal/v2/runtime/runner"
	"github.com/joshka0/foxctl/internal/v2/testkit/fakes"
	"github.com/joshka0/foxctl/internal/v2/testkit/golden"
)

func TestPipeline_HappyPath_OrderedExecution(t *testing.T) {
	t.Parallel()

	store := fakes.NewFakeEventStore()
	clock := fakes.NewFakeClock(time.Date(2026, time.February, 18, 13, 0, 0, 0, time.UTC), time.Second)
	ids := fakes.NewFakeUUID("evt")
	model := fakes.NewFakeModel(runner.ModelResponse{
		Message:   "completed",
		ToolCalls: []run.ToolCall{{Name: "fs_read"}},
		Done:      true,
	})
	tools := fakes.NewFakeToolExecutor()

	var stageOrder []string
	p := runner.New(runner.Config{
		EventStore:   store,
		Model:        model,
		ToolExecutor: tools,
		Now:          clock.Now,
		NewID:        ids.New,
		ObserveStage: func(name string) { stageOrder = append(stageOrder, name) },
	})

	out, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:         "run-0001",
		TurnID:        "turn-0001",
		Command:       "spawn",
		Mode:          "autonomous",
		Prompt:        "read a file",
		ActorID:       "actor-overseer",
		CorrelationID: "corr-001",
		CausationID:   "cause-001",
		RequestID:     "req-001",
		MaxIterations: 3,
	})
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	wantStages := []string{
		runner.StageInitContext,
		runner.StageResolveDependencies,
		runner.StageApplyPreHooks,
		runner.StageBuildToolset,
		runner.StageModelCall,
		runner.StageApplyPostHooks,
		runner.StagePersistTurn,
		runner.StageEmitEvents,
	}
	if !reflect.DeepEqual(stageOrder, wantStages) {
		t.Fatalf("stage order mismatch\ngot:  %v\nwant: %v", stageOrder, wantStages)
	}

	if out.TurnID != "turn-0001" {
		t.Fatalf("TurnID=%q want turn-0001", out.TurnID)
	}
	if out.Iterations != 1 {
		t.Fatalf("Iterations=%d want 1", out.Iterations)
	}
	if out.ToolCalls != 1 {
		t.Fatalf("ToolCalls=%d want 1", out.ToolCalls)
	}
	if out.Degraded {
		t.Fatal("expected non-degraded output")
	}

	eventsList := store.Events()
	assertEventTypes(
		t, eventsList,
		events.EventRunStarted,
		events.EventToolInvoked,
		events.EventToolResponded,
		events.EventTurnRecorded,
		events.EventRunCompleted,
	)
	assertSequenceAndVersion(t, eventsList)
}

func TestPipeline_NonStrictIdentityGeneratedBeforeRunStarted(t *testing.T) {
	t.Parallel()

	store := fakes.NewFakeEventStore()
	clock := fakes.NewFakeClock(time.Date(2026, time.March, 8, 10, 0, 0, 0, time.UTC), time.Second)
	ids := fakes.NewFakeUUID("identity")
	model := fakes.NewFakeModel(runner.ModelResponse{Message: "ok", Done: true})
	tools := fakes.NewFakeToolExecutor()
	p := runner.New(runner.Config{
		EventStore:   store,
		Model:        model,
		ToolExecutor: tools,
		Now:          clock.Now,
		NewID:        ids.New,
	})

	out, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:  "run-generated-identity",
		Prompt: "generate identity",
	})
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if out.TurnID != "turn-identity-0001" {
		t.Fatalf("TurnID=%q want turn-identity-0001", out.TurnID)
	}

	list := store.Events()
	if len(list) == 0 {
		t.Fatal("expected run.started event")
	}
	started := list[0]
	if started.EventType != events.EventRunStarted {
		t.Fatalf("first event type=%q want %q", started.EventType, events.EventRunStarted)
	}
	if started.RequestID != "identity-0002" {
		t.Fatalf("request_id=%q want identity-0002", started.RequestID)
	}
	if started.CorrelationID != "identity-0002" {
		t.Fatalf("correlation_id=%q want identity-0002", started.CorrelationID)
	}
	if started.CausationID != "identity-0002" {
		t.Fatalf("causation_id=%q want identity-0002", started.CausationID)
	}
}

func TestPipeline_StrictDurableIdentityRejectsMissingFieldsBeforeSideEffects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   run.TurnInput
	}{
		{
			name: "missing run id",
			in: run.TurnInput{
				TurnID:    "turn-strict",
				RequestID: "req-strict",
			},
		},
		{
			name: "missing turn id",
			in: run.TurnInput{
				RunID:     "run-strict",
				RequestID: "req-strict",
			},
		},
		{
			name: "missing request id",
			in: run.TurnInput{
				RunID:  "run-strict",
				TurnID: "turn-strict",
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := fakes.NewFakeEventStore()
			model := fakes.NewFakeModel(runner.ModelResponse{Message: "should not run", Done: true})
			tools := fakes.NewFakeToolExecutor()
			var stageOrder []string
			p := runner.New(runner.Config{
				EventStore:            store,
				Model:                 model,
				ToolExecutor:          tools,
				NewID:                 fakes.NewFakeUUID("strict").New,
				ObserveStage:          func(name string) { stageOrder = append(stageOrder, name) },
				StrictDurableIdentity: true,
			})

			_, err := p.RunTurn(context.Background(), tc.in)
			if err == nil {
				t.Fatal("RunTurn succeeded, want validation error")
			}
			var verr *v2errors.V2Error
			if !stderrors.As(err, &verr) {
				t.Fatalf("error type=%T want *V2Error", err)
			}
			if verr.Kind != v2errors.ErrValidation {
				t.Fatalf("kind=%q want %q", verr.Kind, v2errors.ErrValidation)
			}
			if store.Count() != 0 {
				t.Fatalf("events=%d want 0", store.Count())
			}
			if got := len(model.Inputs()); got != 0 {
				t.Fatalf("model calls=%d want 0", got)
			}
			if got := len(tools.Calls()); got != 0 {
				t.Fatalf("tool calls=%d want 0", got)
			}
			if len(stageOrder) != 0 {
				t.Fatalf("stages observed=%v want none", stageOrder)
			}
		})
	}
}

func TestPipeline_StrictDurableIdentityRequiresCursorReaderBeforeSideEffects(t *testing.T) {
	t.Parallel()

	store := fakes.NewFakeEventStore()
	model := fakes.NewFakeModel(runner.ModelResponse{Message: "should not run", Done: true})
	tools := fakes.NewFakeToolExecutor()
	var stageOrder []string
	p := runner.New(runner.Config{
		EventStore:            store,
		Model:                 model,
		ToolExecutor:          tools,
		NewID:                 fakes.NewFakeUUID("strict-cursor").New,
		ObserveStage:          func(name string) { stageOrder = append(stageOrder, name) },
		StrictDurableIdentity: true,
	})

	_, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:     "run-strict-cursor",
		TurnID:    "turn-strict-cursor",
		RequestID: "req-strict-cursor",
		Prompt:    "must have cursor reader",
	})
	if err == nil {
		t.Fatal("RunTurn succeeded, want cursor reader dependency error")
	}
	var verr *v2errors.V2Error
	if !stderrors.As(err, &verr) {
		t.Fatalf("error type=%T want *V2Error", err)
	}
	if verr.Kind != v2errors.ErrDependency {
		t.Fatalf("kind=%q want %q", verr.Kind, v2errors.ErrDependency)
	}
	if store.Count() != 0 {
		t.Fatalf("events=%d want 0", store.Count())
	}
	if got := len(model.Inputs()); got != 0 {
		t.Fatalf("model calls=%d want 0", got)
	}
	if got := len(tools.Calls()); got != 0 {
		t.Fatalf("tool calls=%d want 0", got)
	}
	if len(stageOrder) != 0 {
		t.Fatalf("stages observed=%v want none", stageOrder)
	}
}

func TestPipeline_StrictDurableIdentityCursorReadFailureFailsBeforeSideEffects(t *testing.T) {
	t.Parallel()

	store := &failingCursorEventStore{err: stderrors.New("cursor unavailable")}
	model := fakes.NewFakeModel(runner.ModelResponse{Message: "should not run", Done: true})
	tools := fakes.NewFakeToolExecutor()
	var stageOrder []string
	p := runner.New(runner.Config{
		EventStore:            store,
		Model:                 model,
		ToolExecutor:          tools,
		NewID:                 fakes.NewFakeUUID("strict-cursor").New,
		ObserveStage:          func(name string) { stageOrder = append(stageOrder, name) },
		StrictDurableIdentity: true,
	})

	_, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:     "run-strict-cursor-read",
		TurnID:    "turn-strict-cursor-read",
		RequestID: "req-strict-cursor-read",
		Prompt:    "cursor read fails",
	})
	if err == nil {
		t.Fatal("RunTurn succeeded, want cursor read dependency error")
	}
	var verr *v2errors.V2Error
	if !stderrors.As(err, &verr) {
		t.Fatalf("error type=%T want *V2Error", err)
	}
	if verr.Kind != v2errors.ErrDependency {
		t.Fatalf("kind=%q want %q", verr.Kind, v2errors.ErrDependency)
	}
	if !verr.Retryable {
		t.Fatal("retryable=false want true")
	}
	if store.appendCount != 0 {
		t.Fatalf("events=%d want 0", store.appendCount)
	}
	if got := len(model.Inputs()); got != 0 {
		t.Fatalf("model calls=%d want 0", got)
	}
	if got := len(tools.Calls()); got != 0 {
		t.Fatalf("tool calls=%d want 0", got)
	}
	if len(stageOrder) != 0 {
		t.Fatalf("stages observed=%v want none", stageOrder)
	}
}

func TestPipeline_TursoEventStore_SecondTurnSameRunContinuesStreamCursor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "runner_events.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoevents.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := tursoevents.NewStore(db, closeFn)
	clock := fakes.NewFakeClock(time.Date(2026, time.March, 8, 11, 0, 0, 0, time.UTC), time.Second)
	ids := fakes.NewFakeUUID("cursor")
	newPipeline := func(response string) *runner.Pipeline {
		return runner.New(runner.Config{
			EventStore:            store,
			Model:                 fakes.NewFakeModel(runner.ModelResponse{Message: response, Done: true}),
			ToolExecutor:          fakes.NewFakeToolExecutor(),
			Now:                   clock.Now,
			NewID:                 ids.New,
			StrictDurableIdentity: true,
		})
	}

	for _, input := range []run.TurnInput{
		{
			RunID:     "run-shared-cursor",
			TurnID:    "turn-shared-1",
			RequestID: "req-shared-1",
			Prompt:    "first turn",
		},
		{
			RunID:     "run-shared-cursor",
			TurnID:    "turn-shared-2",
			RequestID: "req-shared-2",
			Prompt:    "second turn",
		},
	} {
		p := newPipeline(input.TurnID)
		if _, err := p.RunTurn(ctx, input); err != nil {
			t.Fatalf("RunTurn(%s) returned error: %v", input.TurnID, err)
		}
	}

	stream, err := store.ListStream(ctx, events.StreamFilter{
		StreamID:   "run-shared-cursor",
		StreamType: events.StreamTypeRun,
		Limit:      20,
	})
	if err != nil {
		t.Fatalf("list stream: %v", err)
	}
	if len(stream) != 6 {
		t.Fatalf("events=%d want 6", len(stream))
	}
	for i, evt := range stream {
		want := int64(i + 1)
		if evt.StreamVersion != want {
			t.Fatalf("event[%d].stream_version=%d want %d", i, evt.StreamVersion, want)
		}
		if evt.Sequence != want {
			t.Fatalf("event[%d].sequence=%d want %d", i, evt.Sequence, want)
		}
	}
	assertEventTypes(
		t, stream,
		events.EventRunStarted,
		events.EventTurnRecorded,
		events.EventRunCompleted,
		events.EventRunStarted,
		events.EventTurnRecorded,
		events.EventRunCompleted,
	)
	for i, evt := range stream[:3] {
		if evt.RequestID != "req-shared-1" {
			t.Fatalf("first turn event[%d].request_id=%q want req-shared-1", i, evt.RequestID)
		}
	}
	for i, evt := range stream[3:] {
		if evt.RequestID != "req-shared-2" {
			t.Fatalf("second turn event[%d].request_id=%q want req-shared-2", i, evt.RequestID)
		}
	}
}

func TestPipeline_StrictDurableIdentityUsesDeterministicEventIDs(t *testing.T) {
	t.Parallel()

	store := newStrictAppendIfAbsentStore()
	clock := fakes.NewFakeClock(time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC), time.Second)
	p := runner.New(runner.Config{
		EventStore:            store,
		Model:                 fakes.NewFakeModel(runner.ModelResponse{Message: "ok", Done: true}),
		ToolExecutor:          fakes.NewFakeToolExecutor(),
		Now:                   clock.Now,
		NewID:                 func() string { t.Fatal("NewID must not be used for strict durable event IDs"); return "" },
		StrictDurableIdentity: true,
	})

	if _, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:     "run-deterministic",
		TurnID:    "turn-deterministic",
		RequestID: "req-deterministic",
		Prompt:    "deterministic",
	}); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	first := store.Events()
	if len(first) != 3 {
		t.Fatalf("events=%d want 3", len(first))
	}
	for _, evt := range first {
		if evt.ID == "" {
			t.Fatalf("event %s has empty ID", evt.EventType)
		}
	}

	store2 := newStrictAppendIfAbsentStore()
	clock2 := fakes.NewFakeClock(time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC), time.Second)
	p2 := runner.New(runner.Config{
		EventStore:            store2,
		Model:                 fakes.NewFakeModel(runner.ModelResponse{Message: "ok", Done: true}),
		ToolExecutor:          fakes.NewFakeToolExecutor(),
		Now:                   clock2.Now,
		NewID:                 func() string { t.Fatal("NewID must not be used for strict durable event IDs"); return "" },
		StrictDurableIdentity: true,
	})
	if _, err := p2.RunTurn(context.Background(), run.TurnInput{
		RunID:     "run-deterministic",
		TurnID:    "turn-deterministic",
		RequestID: "req-deterministic",
		Prompt:    "deterministic",
	}); err != nil {
		t.Fatalf("second RunTurn returned error: %v", err)
	}

	second := store2.Events()
	if len(second) != len(first) {
		t.Fatalf("second events=%d want %d", len(second), len(first))
	}
	for i := range first {
		if second[i].ID != first[i].ID {
			t.Fatalf("event[%d].ID=%q want %q", i, second[i].ID, first[i].ID)
		}
	}
}

func TestPipeline_StrictDurableIdentityReplayDoesNotDuplicateOrRepublish(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newStrictAppendIfAbsentStore()
	bus := &recordingEventBus{}
	clock := fakes.NewFakeClock(time.Date(2026, time.March, 8, 13, 0, 0, 0, time.UTC), time.Second)
	newPipeline := func(response string) *runner.Pipeline {
		return runner.New(runner.Config{
			EventStore:            store,
			EventBus:              bus,
			Model:                 fakes.NewFakeModel(runner.ModelResponse{Message: response, Done: true}),
			ToolExecutor:          fakes.NewFakeToolExecutor(),
			Now:                   clock.Now,
			NewID:                 func() string { t.Fatal("NewID must not be used for strict durable event IDs"); return "" },
			StrictDurableIdentity: true,
		})
	}
	input := run.TurnInput{
		RunID:     "run-replay",
		TurnID:    "turn-replay",
		RequestID: "req-replay",
		Prompt:    "replay",
	}

	if _, err := newPipeline("first").RunTurn(ctx, input); err != nil {
		t.Fatalf("first RunTurn returned error: %v", err)
	}
	first := store.Events()
	if len(first) != 3 {
		t.Fatalf("events after first run=%d want 3", len(first))
	}
	if got := bus.Count(); got != 3 {
		t.Fatalf("published after first run=%d want 3", got)
	}

	if _, err := newPipeline("replay").RunTurn(ctx, input); err != nil {
		t.Fatalf("replay RunTurn returned error: %v", err)
	}
	replayed := store.Events()
	if len(replayed) != 3 {
		t.Fatalf("events after replay=%d want 3", len(replayed))
	}
	if got := bus.Count(); got != 3 {
		t.Fatalf("published after replay=%d want 3", got)
	}
	for i := range first {
		if replayed[i].ID != first[i].ID {
			t.Fatalf("event[%d].ID=%q want %q", i, replayed[i].ID, first[i].ID)
		}
		if replayed[i].StreamVersion != int64(i+1) {
			t.Fatalf("event[%d].stream_version=%d want %d", i, replayed[i].StreamVersion, i+1)
		}
		if replayed[i].Sequence != int64(i+1) {
			t.Fatalf("event[%d].sequence=%d want %d", i, replayed[i].Sequence, i+1)
		}
	}
}

func TestPipeline_ModelInputCarriesToolsAndToolResultHistory(t *testing.T) {
	t.Parallel()

	store := fakes.NewFakeEventStore()
	clock := fakes.NewFakeClock(time.Date(2026, time.February, 18, 13, 5, 0, 0, time.UTC), time.Second)
	ids := fakes.NewFakeUUID("evt")
	model := fakes.NewFakeModel(
		runner.ModelResponse{
			ToolCalls: []run.ToolCall{{ID: "model-call-1", Name: "context_show"}},
			Done:      false,
		},
		runner.ModelResponse{
			Message: "used context",
			Done:    true,
		},
	)
	tools := fakes.NewFakeToolExecutor()

	p := runner.New(runner.Config{
		EventStore: store,
		Model:      model,
		Tools: []coretool.ToolDef{{
			Name:        "context_show",
			Description: "Read context",
		}},
		ToolExecutor: tools,
		Now:          clock.Now,
		NewID:        ids.New,
	})

	out, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:         "run-model-input",
		TurnID:        "turn-model-input",
		Prompt:        "use a tool",
		CorrelationID: "corr-model-input",
		CausationID:   "cause-model-input",
		RequestID:     "req-model-input",
		MaxIterations: 2,
	})
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if out.ToolCalls != 1 {
		t.Fatalf("ToolCalls=%d want 1", out.ToolCalls)
	}
	inputs := model.Inputs()
	if len(inputs) != 2 {
		t.Fatalf("model inputs=%d want 2", len(inputs))
	}
	if len(inputs[0].Tools) != 1 || inputs[0].Tools[0].Name != "context_show" {
		t.Fatalf("first input tools=%+v", inputs[0].Tools)
	}
	if len(inputs[1].Messages) != 3 {
		t.Fatalf("second input messages=%d want 3", len(inputs[1].Messages))
	}
	if inputs[1].Messages[1].ToolCalls[0].ID != "model-call-1" {
		t.Fatalf("assistant tool call=%+v", inputs[1].Messages[1].ToolCalls[0])
	}
	if inputs[1].Messages[2].ToolCallID != "model-call-1" {
		t.Fatalf("tool result message=%+v", inputs[1].Messages[2])
	}
}

func TestPipeline_EffectJournalStoredModelResponseSkipsModelCall(t *testing.T) {
	t.Parallel()

	journal := newMemoryEffectJournal()
	responseJSON := mustJSON(t, runner.ModelResponse{Message: "from journal", Done: true})
	journal.models[effectKeyString(run.EffectKey{
		RunID:          "run-journal-model",
		RequestID:      "req-journal-model",
		TurnID:         "turn-journal-model",
		IterationIndex: 1,
	})] = run.ModelEffectRecord{
		EffectKey: run.EffectKey{
			RunID:          "run-journal-model",
			RequestID:      "req-journal-model",
			TurnID:         "turn-journal-model",
			IterationIndex: 1,
		},
		ResponseJSON: responseJSON,
	}

	model := fakes.NewFakeModel(runner.ModelResponse{Message: "should not run", Done: true})
	p := runner.New(runner.Config{
		EventStore:    fakes.NewFakeEventStore(),
		Model:         model,
		ToolExecutor:  fakes.NewFakeToolExecutor(),
		EffectJournal: journal,
		NewID:         fakes.NewFakeUUID("journal-model").New,
	})

	out, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:     "run-journal-model",
		TurnID:    "turn-journal-model",
		RequestID: "req-journal-model",
		Prompt:    "replay model",
	})
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if out.Summary != "from journal" {
		t.Fatalf("Summary=%q want from journal", out.Summary)
	}
	if got := len(model.Inputs()); got != 0 {
		t.Fatalf("model calls=%d want 0", got)
	}
}

func TestPipeline_EffectJournalIntentOnlyModelFailsClosed(t *testing.T) {
	t.Parallel()

	journal := newMemoryEffectJournal()
	key := run.EffectKey{
		RunID:          "run-journal-model-intent",
		RequestID:      "req-journal-model-intent",
		TurnID:         "turn-journal-model-intent",
		IterationIndex: 1,
	}
	journal.models[effectKeyString(key)] = run.ModelEffectRecord{
		EffectKey: key,
		Status:    run.ModelEffectIntent,
	}

	model := fakes.NewFakeModel(runner.ModelResponse{Message: "should not run", Done: true})
	p := runner.New(runner.Config{
		EventStore:    fakes.NewFakeEventStore(),
		Model:         model,
		ToolExecutor:  fakes.NewFakeToolExecutor(),
		EffectJournal: journal,
		NewID:         fakes.NewFakeUUID("journal-model-intent").New,
	})

	_, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:     "run-journal-model-intent",
		TurnID:    "turn-journal-model-intent",
		RequestID: "req-journal-model-intent",
		Prompt:    "replay model intent",
	})
	if err == nil {
		t.Fatal("RunTurn succeeded, want fail-closed error")
	}
	var verr *v2errors.V2Error
	if !stderrors.As(err, &verr) {
		t.Fatalf("error type=%T want *V2Error", err)
	}
	if verr.Kind != v2errors.ErrConflict {
		t.Fatalf("kind=%q want %q", verr.Kind, v2errors.ErrConflict)
	}
	if !stderrors.Is(verr.Cause, run.ErrEffectIncomplete) {
		t.Fatalf("cause=%v want ErrEffectIncomplete", verr.Cause)
	}
	if got := len(model.Inputs()); got != 0 {
		t.Fatalf("model calls=%d want 0", got)
	}
}

func TestPipeline_EffectJournalModelInputConflictFailsClosed(t *testing.T) {
	t.Parallel()

	journal := newMemoryEffectJournal()
	key := run.EffectKey{
		RunID:          "run-journal-model-conflict",
		RequestID:      "req-journal-model-conflict",
		TurnID:         "turn-journal-model-conflict",
		IterationIndex: 1,
	}
	journal.models[effectKeyString(key)] = run.ModelEffectRecord{
		EffectKey:    key,
		InputJSON:    json.RawMessage(`{"prompt":"different"}`),
		Status:       run.ModelEffectSucceeded,
		ResponseJSON: mustJSON(t, runner.ModelResponse{Message: "stale", Done: true}),
	}

	model := fakes.NewFakeModel(runner.ModelResponse{Message: "should not run", Done: true})
	p := runner.New(runner.Config{
		EventStore:    fakes.NewFakeEventStore(),
		Model:         model,
		ToolExecutor:  fakes.NewFakeToolExecutor(),
		EffectJournal: journal,
		NewID:         fakes.NewFakeUUID("journal-model-conflict").New,
	})

	_, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:     "run-journal-model-conflict",
		TurnID:    "turn-journal-model-conflict",
		RequestID: "req-journal-model-conflict",
		Prompt:    "current prompt",
	})
	if err == nil {
		t.Fatal("RunTurn succeeded, want conflict error")
	}
	var verr *v2errors.V2Error
	if !stderrors.As(err, &verr) {
		t.Fatalf("error type=%T want *V2Error", err)
	}
	if verr.Kind != v2errors.ErrConflict || !stderrors.Is(verr.Cause, run.ErrEffectConflict) {
		t.Fatalf("error=%v cause=%v want conflict/ErrEffectConflict", verr.Kind, verr.Cause)
	}
	if got := len(model.Inputs()); got != 0 {
		t.Fatalf("model calls=%d want 0", got)
	}
}

func TestPipeline_EffectJournalStoredToolResultSkipsToolExecutor(t *testing.T) {
	t.Parallel()

	journal := newMemoryEffectJournal()
	key := run.EffectKey{
		RunID:          "run-journal-tool",
		RequestID:      "req-journal-tool",
		TurnID:         "turn-journal-tool",
		IterationIndex: 1,
		ToolCallID:     "stored-call",
	}
	journal.tools[effectKeyString(key)] = run.ToolEffectRecord{
		EffectKey:  key,
		ToolName:   "context_show",
		Status:     run.ToolEffectSucceeded,
		ResultJSON: mustJSON(t, runner.ToolResult{Status: "ok", Output: "stored output"}),
	}

	model := fakes.NewFakeModel(runner.ModelResponse{
		ToolCalls: []run.ToolCall{{ID: "stored-call", Name: "context_show"}},
		Done:      true,
	})
	tools := fakes.NewFakeToolExecutor()
	p := runner.New(runner.Config{
		EventStore:    fakes.NewFakeEventStore(),
		Model:         model,
		ToolExecutor:  tools,
		EffectJournal: journal,
		NewID:         fakes.NewFakeUUID("journal-tool").New,
	})

	out, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:     "run-journal-tool",
		TurnID:    "turn-journal-tool",
		RequestID: "req-journal-tool",
		Prompt:    "replay tool",
	})
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if out.ToolCalls != 1 {
		t.Fatalf("ToolCalls=%d want 1", out.ToolCalls)
	}
	if got := len(tools.Calls()); got != 0 {
		t.Fatalf("tool executor calls=%d want 0", got)
	}
}

func TestPipeline_EffectJournalToolInputConflictFailsClosed(t *testing.T) {
	t.Parallel()

	journal := newMemoryEffectJournal()
	key := run.EffectKey{
		RunID:          "run-journal-tool-conflict",
		RequestID:      "req-journal-tool-conflict",
		TurnID:         "turn-journal-tool-conflict",
		IterationIndex: 1,
		ToolCallID:     "conflict-call",
	}
	journal.tools[effectKeyString(key)] = run.ToolEffectRecord{
		EffectKey:  key,
		ToolName:   "context_show",
		ArgsJSON:   json.RawMessage(`{"path":"old"}`),
		Status:     run.ToolEffectSucceeded,
		ResultJSON: mustJSON(t, runner.ToolResult{Status: "ok", Output: "stored output"}),
	}

	model := fakes.NewFakeModel(runner.ModelResponse{
		ToolCalls: []run.ToolCall{{
			ID:   "conflict-call",
			Name: "context_show",
			Args: json.RawMessage(`{"path":"new"}`),
		}},
		Done: true,
	})
	tools := fakes.NewFakeToolExecutor()
	p := runner.New(runner.Config{
		EventStore:    fakes.NewFakeEventStore(),
		Model:         model,
		ToolExecutor:  tools,
		EffectJournal: journal,
		NewID:         fakes.NewFakeUUID("journal-tool-conflict").New,
	})

	_, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:     "run-journal-tool-conflict",
		TurnID:    "turn-journal-tool-conflict",
		RequestID: "req-journal-tool-conflict",
		Prompt:    "replay conflicting tool",
	})
	if err == nil {
		t.Fatal("RunTurn succeeded, want conflict error")
	}
	var verr *v2errors.V2Error
	if !stderrors.As(err, &verr) {
		t.Fatalf("error type=%T want *V2Error", err)
	}
	if verr.Kind != v2errors.ErrConflict || !stderrors.Is(verr.Cause, run.ErrEffectConflict) {
		t.Fatalf("error=%v cause=%v want conflict/ErrEffectConflict", verr.Kind, verr.Cause)
	}
	if got := len(tools.Calls()); got != 0 {
		t.Fatalf("tool executor calls=%d want 0", got)
	}
}

func TestPipeline_EffectJournalReadOnlyToolIntentRetriesExecutor(t *testing.T) {
	t.Parallel()

	journal := newMemoryEffectJournal()
	key := run.EffectKey{
		RunID:          "run-journal-tool-retry",
		RequestID:      "req-journal-tool-retry",
		TurnID:         "turn-journal-tool-retry",
		IterationIndex: 1,
		ToolCallID:     "read-only-call",
	}
	journal.tools[effectKeyString(key)] = run.ToolEffectRecord{
		EffectKey: key,
		ToolName:  "context_show",
		Status:    run.ToolEffectIntent,
	}

	model := fakes.NewFakeModel(runner.ModelResponse{
		ToolCalls: []run.ToolCall{{ID: "read-only-call", Name: "context_show"}},
		Done:      true,
	})
	tools := fakes.NewFakeToolExecutor()
	p := runner.New(runner.Config{
		EventStore:   fakes.NewFakeEventStore(),
		Model:        model,
		ToolExecutor: tools,
		Tools: []coretool.ToolDef{{
			Name: "context_show",
			Policy: coretool.ToolPolicy{
				EffectReplay: coretool.EffectReplayReadOnly,
			},
		}},
		EffectJournal: journal,
		NewID:         fakes.NewFakeUUID("journal-tool-retry").New,
	})

	out, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:     "run-journal-tool-retry",
		TurnID:    "turn-journal-tool-retry",
		RequestID: "req-journal-tool-retry",
		Prompt:    "retry read-only tool",
	})
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if out.ToolCalls != 1 {
		t.Fatalf("ToolCalls=%d want 1", out.ToolCalls)
	}
	if got := tools.CallCount("context_show"); got != 1 {
		t.Fatalf("tool executor calls=%d want 1", got)
	}
	record := journal.tools[effectKeyString(key)]
	if record.Status != run.ToolEffectSucceeded {
		t.Fatalf("journal status=%q want succeeded", record.Status)
	}
	if record.ReplayPolicy != string(coretool.EffectReplayReadOnly) {
		t.Fatalf("journal replay_policy=%q want read_only", record.ReplayPolicy)
	}
}

func TestPipeline_EffectJournalIntentOnlyToolFailsClosed(t *testing.T) {
	t.Parallel()

	journal := newMemoryEffectJournal()
	key := run.EffectKey{
		RunID:          "run-journal-intent",
		RequestID:      "req-journal-intent",
		TurnID:         "turn-journal-intent",
		IterationIndex: 1,
		ToolCallID:     "intent-call",
	}
	journal.tools[effectKeyString(key)] = run.ToolEffectRecord{
		EffectKey: key,
		ToolName:  "side_effect",
		Status:    run.ToolEffectIntent,
	}

	model := fakes.NewFakeModel(runner.ModelResponse{
		ToolCalls: []run.ToolCall{{ID: "intent-call", Name: "side_effect"}},
		Done:      true,
	})
	tools := fakes.NewFakeToolExecutor()
	p := runner.New(runner.Config{
		EventStore:    fakes.NewFakeEventStore(),
		Model:         model,
		ToolExecutor:  tools,
		EffectJournal: journal,
		NewID:         fakes.NewFakeUUID("journal-intent").New,
	})

	_, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:     "run-journal-intent",
		TurnID:    "turn-journal-intent",
		RequestID: "req-journal-intent",
		Prompt:    "do side effect",
	})
	if err == nil {
		t.Fatal("RunTurn succeeded, want fail-closed error")
	}
	var verr *v2errors.V2Error
	if !stderrors.As(err, &verr) {
		t.Fatalf("error type=%T want *V2Error", err)
	}
	if verr.Kind != v2errors.ErrConflict {
		t.Fatalf("kind=%q want %q", verr.Kind, v2errors.ErrConflict)
	}
	if !verr.Fatal {
		t.Fatal("Fatal=false want true")
	}
	if verr.Retryable {
		t.Fatal("Retryable=true want false")
	}
	if got := len(tools.Calls()); got != 0 {
		t.Fatalf("tool executor calls=%d want 0", got)
	}
}

func TestPipeline_MaxIterations_StopsAndMarksDegraded(t *testing.T) {
	t.Parallel()

	store := fakes.NewFakeEventStore()
	clock := fakes.NewFakeClock(time.Date(2026, time.February, 18, 13, 10, 0, 0, time.UTC), time.Second)
	ids := fakes.NewFakeUUID("evt")
	model := fakes.NewFakeModel(
		runner.ModelResponse{Message: "working", ToolCalls: []run.ToolCall{{Name: "fs_read"}}, Done: false},
		runner.ModelResponse{Message: "still working", ToolCalls: []run.ToolCall{{Name: "fs_read"}}, Done: false},
	)
	model.WithDefault(runner.ModelResponse{Message: "still working", ToolCalls: []run.ToolCall{{Name: "fs_read"}}, Done: false})
	tools := fakes.NewFakeToolExecutor()

	p := runner.New(runner.Config{
		EventStore:   store,
		Model:        model,
		ToolExecutor: tools,
		Now:          clock.Now,
		NewID:        ids.New,
	})

	out, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:         "run-0002",
		TurnID:        "turn-0002",
		Command:       "run",
		Mode:          "autonomous",
		Prompt:        "keep iterating",
		ActorID:       "actor-overseer",
		CorrelationID: "corr-002",
		CausationID:   "cause-002",
		RequestID:     "req-002",
		MaxIterations: 2,
	})
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if !out.Degraded {
		t.Fatal("expected degraded output when max iterations reached")
	}
	if out.Iterations != 2 {
		t.Fatalf("Iterations=%d want 2", out.Iterations)
	}
	if out.ToolCalls != 2 {
		t.Fatalf("ToolCalls=%d want 2", out.ToolCalls)
	}
	if len(out.StageFailures) != 1 {
		t.Fatalf("StageFailures=%d want 1", len(out.StageFailures))
	}
	if out.StageFailures[0].Stage != runner.StageModelCall {
		t.Fatalf("StageFailures[0].Stage=%q want %q", out.StageFailures[0].Stage, runner.StageModelCall)
	}

	eventsList := store.Events()
	containsStageFailed := false
	for _, evt := range eventsList {
		if evt.EventType == events.EventStageFailed {
			containsStageFailed = true
			break
		}
	}
	if !containsStageFailed {
		t.Fatal("expected stage.failed event when max iterations reached")
	}
	assertSequenceAndVersion(t, eventsList)
}

func TestPipeline_ModelResponseWithoutToolsRespectsDoneFlag(t *testing.T) {
	t.Parallel()

	store := fakes.NewFakeEventStore()
	clock := fakes.NewFakeClock(time.Date(2026, time.February, 18, 13, 15, 0, 0, time.UTC), time.Second)
	ids := fakes.NewFakeUUID("evt")
	model := fakes.NewFakeModel(
		runner.ModelResponse{Message: "thinking", Done: false},
		runner.ModelResponse{Message: "completed", Done: true},
	)
	tools := fakes.NewFakeToolExecutor()

	p := runner.New(runner.Config{
		EventStore:   store,
		Model:        model,
		ToolExecutor: tools,
		Now:          clock.Now,
		NewID:        ids.New,
	})

	out, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:         "run-0002b",
		TurnID:        "turn-0002b",
		Command:       "run",
		Mode:          "autonomous",
		Prompt:        "text only iterations",
		ActorID:       "actor-overseer",
		CorrelationID: "corr-002b",
		CausationID:   "cause-002b",
		RequestID:     "req-002b",
		MaxIterations: 3,
	})
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if out.Degraded {
		t.Fatal("expected non-degraded output")
	}
	if out.Iterations != 2 {
		t.Fatalf("Iterations=%d want 2", out.Iterations)
	}
	assertEventTypes(
		t, store.Events(),
		events.EventRunStarted,
		events.EventTurnRecorded,
		events.EventRunCompleted,
	)
}

func TestPipeline_StageFailure_NonFatalContinues(t *testing.T) {
	t.Parallel()

	store := fakes.NewFakeEventStore()
	clock := fakes.NewFakeClock(time.Date(2026, time.February, 18, 13, 20, 0, 0, time.UTC), time.Second)
	ids := fakes.NewFakeUUID("evt")
	model := fakes.NewFakeModel(runner.ModelResponse{Message: "completed", Done: true})
	tools := fakes.NewFakeToolExecutor()

	hooks := fakeHooks{
		preErr: &v2errors.V2Error{
			Kind:    v2errors.ErrStageFailed,
			Message: "non-fatal pre-hook issue",
			Fatal:   false,
		},
	}

	p := runner.New(runner.Config{
		EventStore:   store,
		Model:        model,
		ToolExecutor: tools,
		Hooks:        hooks,
		Now:          clock.Now,
		NewID:        ids.New,
	})

	out, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:         "run-0003",
		TurnID:        "turn-0003",
		Command:       "run",
		Mode:          "reactive",
		Prompt:        "hello",
		ActorID:       "actor-overseer",
		CorrelationID: "corr-003",
		CausationID:   "cause-003",
		RequestID:     "req-003",
		MaxIterations: 1,
	})
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if !out.Degraded {
		t.Fatal("expected degraded output")
	}
	if len(out.StageFailures) != 1 {
		t.Fatalf("StageFailures=%d want 1", len(out.StageFailures))
	}
	if out.StageFailures[0].Stage != runner.StageApplyPreHooks {
		t.Fatalf("StageFailures[0].Stage=%q want %q", out.StageFailures[0].Stage, runner.StageApplyPreHooks)
	}

	assertEventTypes(
		t, store.Events(),
		events.EventRunStarted,
		events.EventStageFailed,
		events.EventTurnRecorded,
		events.EventRunCompleted,
	)
}

func TestPipeline_ToolExecutorError_StopsBeforePersist(t *testing.T) {
	t.Parallel()

	store := fakes.NewFakeEventStore()
	clock := fakes.NewFakeClock(time.Date(2026, time.February, 18, 13, 25, 0, 0, time.UTC), time.Second)
	ids := fakes.NewFakeUUID("evt")
	model := fakes.NewFakeModel(runner.ModelResponse{
		Message:   "attempted",
		ToolCalls: []run.ToolCall{{Name: "fs_read"}},
		Done:      true,
	})
	tools := fakes.NewFakeToolExecutor()
	tools.SetError("fs_read", stderrors.New("boom"))

	p := runner.New(runner.Config{
		EventStore:   store,
		Model:        model,
		ToolExecutor: tools,
		Now:          clock.Now,
		NewID:        ids.New,
	})

	_, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:         "run-0003b",
		TurnID:        "turn-0003b",
		Command:       "run",
		Mode:          "autonomous",
		Prompt:        "tool fails",
		ActorID:       "actor-overseer",
		CorrelationID: "corr-003b",
		CausationID:   "cause-003b",
		RequestID:     "req-003b",
		MaxIterations: 1,
	})
	if err == nil {
		t.Fatal("expected tool failure")
	}
	var verr *v2errors.V2Error
	if !stderrors.As(err, &verr) {
		t.Fatalf("expected V2Error, got %T", err)
	}
	if verr.Kind != v2errors.ErrToolFailed {
		t.Fatalf("error kind=%q want %q", verr.Kind, v2errors.ErrToolFailed)
	}

	assertEventTypes(
		t, store.Events(),
		events.EventRunStarted,
		events.EventToolInvoked,
		events.EventToolResponded,
		events.EventRunFailed,
	)
}

func TestPipeline_PostHookFailure_StopsBeforePersist(t *testing.T) {
	t.Parallel()

	store := fakes.NewFakeEventStore()
	clock := fakes.NewFakeClock(time.Date(2026, time.February, 18, 13, 27, 0, 0, time.UTC), time.Second)
	ids := fakes.NewFakeUUID("evt")
	model := fakes.NewFakeModel(runner.ModelResponse{Message: "completed", Done: true})
	tools := fakes.NewFakeToolExecutor()
	hooks := fakeHooks{postErr: stderrors.New("post-hook failure")}

	p := runner.New(runner.Config{
		EventStore:   store,
		Model:        model,
		ToolExecutor: tools,
		Hooks:        hooks,
		Now:          clock.Now,
		NewID:        ids.New,
	})

	_, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:         "run-0003c",
		TurnID:        "turn-0003c",
		Command:       "run",
		Mode:          "reactive",
		Prompt:        "post hook fails",
		ActorID:       "actor-overseer",
		CorrelationID: "corr-003c",
		CausationID:   "cause-003c",
		RequestID:     "req-003c",
		MaxIterations: 1,
	})
	if err == nil {
		t.Fatal("expected post-hook failure")
	}
	var verr *v2errors.V2Error
	if !stderrors.As(err, &verr) {
		t.Fatalf("expected V2Error, got %T", err)
	}
	if verr.Kind != v2errors.ErrStageFailed {
		t.Fatalf("error kind=%q want %q", verr.Kind, v2errors.ErrStageFailed)
	}

	assertEventTypes(
		t, store.Events(),
		events.EventRunStarted,
		events.EventRunFailed,
	)
}

func TestPipeline_ContextCancel_StopsBeforePersist(t *testing.T) {
	t.Parallel()

	store := fakes.NewFakeEventStore()
	clock := fakes.NewFakeClock(time.Date(2026, time.February, 18, 13, 30, 0, 0, time.UTC), time.Second)
	ids := fakes.NewFakeUUID("evt")
	model := fakes.NewFakeModel(runner.ModelResponse{Message: "completed", Done: true})
	tools := fakes.NewFakeToolExecutor()

	ctx, cancel := context.WithCancel(context.Background())
	model.SetOnCall(func(_ int, _ runner.ModelInput) {
		cancel()
	})

	p := runner.New(runner.Config{
		EventStore:   store,
		Model:        model,
		ToolExecutor: tools,
		Now:          clock.Now,
		NewID:        ids.New,
	})

	_, err := p.RunTurn(ctx, run.TurnInput{
		RunID:         "run-0004",
		TurnID:        "turn-0004",
		Command:       "run",
		Mode:          "reactive",
		Prompt:        "cancel",
		ActorID:       "actor-overseer",
		CorrelationID: "corr-004",
		CausationID:   "cause-004",
		RequestID:     "req-004",
		MaxIterations: 1,
	})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	var verr *v2errors.V2Error
	if !stderrors.As(err, &verr) {
		t.Fatalf("expected V2Error, got %T", err)
	}
	if verr.Kind != v2errors.ErrTimeout {
		t.Fatalf("error kind=%q want %q", verr.Kind, v2errors.ErrTimeout)
	}

	for _, evt := range store.Events() {
		if evt.EventType == events.EventTurnRecorded {
			t.Fatal("turn.recorded must not be emitted after cancellation before persist")
		}
	}
}

func TestPipeline_Golden_EventOrder_StableOutput(t *testing.T) {
	t.Parallel()

	store := fakes.NewFakeEventStore()
	clock := fakes.NewFakeClock(time.Date(2026, time.February, 18, 14, 0, 0, 0, time.UTC), time.Second)
	ids := fakes.NewFakeUUID("evt")
	model := fakes.NewFakeModel(runner.ModelResponse{
		Message:   "completed",
		ToolCalls: []run.ToolCall{{Name: "fs_read"}},
		Done:      true,
	})
	tools := fakes.NewFakeToolExecutor()

	p := runner.New(runner.Config{
		EventStore:   store,
		Model:        model,
		ToolExecutor: tools,
		Now:          clock.Now,
		NewID:        ids.New,
	})

	_, err := p.RunTurn(context.Background(), run.TurnInput{
		RunID:         "run-worker-001",
		TurnID:        "turn-0001",
		Command:       "run",
		Mode:          "autonomous",
		Prompt:        "run tool",
		ActorID:       "actor-overseer",
		CorrelationID: "corr-001",
		CausationID:   "cause-001",
		RequestID:     "req-001",
		MaxIterations: 2,
	})
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	_, thisFile, _, ok := stdruntime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	goldenPath := filepath.Join(
		filepath.Dir(thisFile),
		"testdata", "golden_events", "run_worker_toolflow_success.jsonl",
	)
	golden.AssertEventsJSONLMatchesFile(t, store.Events(), goldenPath)
}

func assertEventTypes(t *testing.T, list []events.Event, want ...events.EventType) {
	t.Helper()
	got := make([]events.EventType, len(list))
	for i := range list {
		got[i] = list[i].EventType
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event types mismatch\ngot:  %v\nwant: %v", got, want)
	}
}

func assertSequenceAndVersion(t *testing.T, list []events.Event) {
	t.Helper()
	for i := range list {
		want := int64(i + 1)
		if list[i].Sequence != want {
			t.Fatalf("event[%d].sequence=%d want %d", i, list[i].Sequence, want)
		}
		if list[i].StreamVersion != want {
			t.Fatalf("event[%d].stream_version=%d want %d", i, list[i].StreamVersion, want)
		}
	}
}

type fakeHooks struct {
	preErr  error
	postErr error
}

func (f fakeHooks) RunPreHooks(_ context.Context, _ run.TurnInput) error {
	return f.preErr
}

func (f fakeHooks) RunPostHooks(_ context.Context, _ run.TurnInput, _ run.TurnOutput) error {
	return f.postErr
}

type failingCursorEventStore struct {
	err         error
	appendCount int
}

func (s *failingCursorEventStore) Append(_ context.Context, _ events.Event) error {
	s.appendCount++
	return nil
}

func (s *failingCursorEventStore) ReadStreamCursor(_ context.Context, _ events.StreamCursorRequest) (events.StreamCursor, error) {
	return events.StreamCursor{}, s.err
}

type strictAppendIfAbsentStore struct {
	mu     sync.Mutex
	events []events.Event
	byID   map[string]events.Event
}

func newStrictAppendIfAbsentStore() *strictAppendIfAbsentStore {
	return &strictAppendIfAbsentStore{byID: make(map[string]events.Event)}
}

func (s *strictAppendIfAbsentStore) Append(ctx context.Context, event events.Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) > 0 {
		last := s.events[len(s.events)-1]
		if event.StreamVersion != last.StreamVersion+1 || event.Sequence != last.Sequence+1 {
			return events.ErrVersionConflict
		}
	}
	if _, ok := s.byID[event.ID]; ok {
		return events.ErrVersionConflict
	}
	clone := event.Clone()
	s.events = append(s.events, clone)
	s.byID[clone.ID] = clone
	return nil
}

func (s *strictAppendIfAbsentStore) AppendIfAbsent(ctx context.Context, event events.Event) (events.AppendResult, error) {
	select {
	case <-ctx.Done():
		return events.AppendResult{}, ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if stored, ok := s.byID[event.ID]; ok {
		return events.AppendResult{Event: stored.Clone(), Appended: false}, nil
	}
	if len(s.events) > 0 {
		last := s.events[len(s.events)-1]
		if event.StreamVersion != last.StreamVersion+1 || event.Sequence != last.Sequence+1 {
			return events.AppendResult{}, events.ErrVersionConflict
		}
	}
	clone := event.Clone()
	s.events = append(s.events, clone)
	s.byID[clone.ID] = clone
	return events.AppendResult{Event: clone.Clone(), Appended: true}, nil
}

func (s *strictAppendIfAbsentStore) ReadStreamCursor(ctx context.Context, _ events.StreamCursorRequest) (events.StreamCursor, error) {
	select {
	case <-ctx.Done():
		return events.StreamCursor{}, ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) == 0 {
		return events.StreamCursor{}, nil
	}
	last := s.events[len(s.events)-1]
	return events.StreamCursor{StreamVersion: last.StreamVersion, Sequence: last.Sequence}, nil
}

func (s *strictAppendIfAbsentStore) Events() []events.Event {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]events.Event, len(s.events))
	for i := range s.events {
		out[i] = s.events[i].Clone()
	}
	return out
}

type memoryEffectJournal struct {
	mu     sync.Mutex
	models map[string]run.ModelEffectRecord
	tools  map[string]run.ToolEffectRecord
}

func newMemoryEffectJournal() *memoryEffectJournal {
	return &memoryEffectJournal{
		models: make(map[string]run.ModelEffectRecord),
		tools:  make(map[string]run.ToolEffectRecord),
	}
}

func (j *memoryEffectJournal) GetModelEffect(ctx context.Context, key run.EffectKey) (run.ModelEffectRecord, error) {
	select {
	case <-ctx.Done():
		return run.ModelEffectRecord{}, ctx.Err()
	default:
	}

	j.mu.Lock()
	defer j.mu.Unlock()
	record, ok := j.models[effectKeyString(key)]
	if !ok {
		return run.ModelEffectRecord{}, run.ErrEffectNotFound
	}
	return record, nil
}

func (j *memoryEffectJournal) BeginModelEffect(ctx context.Context, record run.ModelEffectRecord) (run.ModelEffectRecord, error) {
	select {
	case <-ctx.Done():
		return run.ModelEffectRecord{}, ctx.Err()
	default:
	}

	j.mu.Lock()
	defer j.mu.Unlock()
	key := effectKeyString(record.EffectKey)
	if existing, ok := j.models[key]; ok {
		return existing, nil
	}
	record.Status = run.ModelEffectIntent
	record.ResponseJSON = nil
	record.ErrorMessage = ""
	j.models[key] = record
	return record, nil
}

func (j *memoryEffectJournal) CompleteModelEffect(ctx context.Context, record run.ModelEffectRecord) (run.ModelEffectRecord, error) {
	select {
	case <-ctx.Done():
		return run.ModelEffectRecord{}, ctx.Err()
	default:
	}

	j.mu.Lock()
	defer j.mu.Unlock()
	key := effectKeyString(record.EffectKey)
	if _, ok := j.models[key]; !ok {
		return run.ModelEffectRecord{}, run.ErrEffectNotFound
	}
	j.models[key] = record
	return record, nil
}

func (j *memoryEffectJournal) SaveModelEffect(ctx context.Context, record run.ModelEffectRecord) (run.ModelEffectRecord, error) {
	select {
	case <-ctx.Done():
		return run.ModelEffectRecord{}, ctx.Err()
	default:
	}

	j.mu.Lock()
	defer j.mu.Unlock()
	if record.Status == "" {
		record.Status = run.ModelEffectSucceeded
	}
	j.models[effectKeyString(record.EffectKey)] = record
	return record, nil
}

func (j *memoryEffectJournal) GetToolEffect(ctx context.Context, key run.EffectKey) (run.ToolEffectRecord, error) {
	select {
	case <-ctx.Done():
		return run.ToolEffectRecord{}, ctx.Err()
	default:
	}

	j.mu.Lock()
	defer j.mu.Unlock()
	record, ok := j.tools[effectKeyString(key)]
	if !ok {
		return run.ToolEffectRecord{}, run.ErrEffectNotFound
	}
	return record, nil
}

func (j *memoryEffectJournal) BeginToolEffect(ctx context.Context, record run.ToolEffectRecord) (run.ToolEffectRecord, error) {
	select {
	case <-ctx.Done():
		return run.ToolEffectRecord{}, ctx.Err()
	default:
	}

	j.mu.Lock()
	defer j.mu.Unlock()
	j.tools[effectKeyString(record.EffectKey)] = record
	return record, nil
}

func (j *memoryEffectJournal) CompleteToolEffect(ctx context.Context, record run.ToolEffectRecord) (run.ToolEffectRecord, error) {
	select {
	case <-ctx.Done():
		return run.ToolEffectRecord{}, ctx.Err()
	default:
	}

	j.mu.Lock()
	defer j.mu.Unlock()
	j.tools[effectKeyString(record.EffectKey)] = record
	return record, nil
}

func effectKeyString(key run.EffectKey) string {
	return key.RunID + "\x00" + key.RequestID + "\x00" + key.TurnID + "\x00" + strconv.Itoa(key.IterationIndex) + "\x00" + key.ToolCallID
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return json.RawMessage(b)
}

type recordingEventBus struct {
	mu     sync.Mutex
	events []events.Event
}

func (b *recordingEventBus) Publish(ctx context.Context, evt events.Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, evt.Clone())
	return nil
}

func (b *recordingEventBus) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.events)
}
