package runner_test

import (
	"context"
	stderrors "errors"
	"path/filepath"
	"reflect"
	stdruntime "runtime"
	"testing"
	"time"

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
	assertEventTypes(t, eventsList,
		events.EventRunStarted,
		events.EventToolInvoked,
		events.EventToolResponded,
		events.EventTurnRecorded,
		events.EventRunCompleted,
	)
	assertSequenceAndVersion(t, eventsList)
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
	assertEventTypes(t, store.Events(),
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

	assertEventTypes(t, store.Events(),
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

	assertEventTypes(t, store.Events(),
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

	assertEventTypes(t, store.Events(),
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
