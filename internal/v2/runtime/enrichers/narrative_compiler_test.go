package enrichers_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coreevents "github.com/joshka0/foxctl/internal/v2/core/events"
	"github.com/joshka0/foxctl/internal/v2/core/run"
	"github.com/joshka0/foxctl/internal/v2/runtime/enrichers"
	runtimeevents "github.com/joshka0/foxctl/internal/v2/runtime/events"
)

func TestNarrativeCompilerComponent_TurnRecordedRefreshesOnTurnsTrigger(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.February, 23, 13, 0, 0, 0, time.UTC)
	sessionID := "run-narrative-turn-trigger"
	turns := buildIndexedTurns(sessionID, 13)

	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 16,
		OverflowPolicy:   runtimeevents.OverflowDropNewest,
	})
	turnReader := &narrativeTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-013": turns[len(turns)-1],
		},
	}
	timeline := &narrativeTimelineReader{
		turnsBySession: map[string][]run.TurnRecord{
			sessionID: turns,
		},
	}
	narrReader := &narrativeReaderMap{
		records: map[string]run.NarrativeRecord{
			sessionID: {
				SessionID:       sessionID,
				ArtifactVersion: "v1",
				SourceTurnIndex: 1,
				UpdatedAt:       now.Add(-5 * time.Minute),
				Claims: []run.NarrativeClaim{
					{Text: "seed", AnchorRefs: []string{"turn/turn-001"}},
				},
				AnchorRefs: []string{"turn/turn-001"},
			},
		},
	}
	writer := &narrativeWriterCapture{}
	var triggerSeen atomic.Value
	triggerSeen.Store(enrichers.NarrativeRefreshTrigger(""))
	compiler := narrativeCompilerFunc(func(_ context.Context, input enrichers.NarrativeCompileInput) (run.NarrativeRecord, error) {
		triggerSeen.Store(input.Trigger)
		return run.NarrativeRecord{
			Summary: "compiled narrative",
			Claims: []run.NarrativeClaim{
				{
					Text:       "compiled claim",
					AnchorRefs: []string{"turn/turn-013"},
				},
			},
		}, nil
	})

	component := enrichers.NewNarrativeCompilerComponent(enrichers.NarrativeCompilerConfig{
		Bus:                bus,
		TurnReader:         turnReader,
		TurnTimelineReader: timeline,
		NarrativeReader:    narrReader,
		NarrativeWriter:    writer,
		Compiler:           compiler,
		Now:                func() time.Time { return now },
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- component.Run(ctx) }()

	waitForNarrativeCondition(t, 2*time.Second, func() bool {
		return bus.Stats().Subscribers > 0
	})

	if err := bus.Publish(context.Background(), coreevents.Event{
		ID:         "evt-narrative-013",
		StreamID:   sessionID,
		StreamType: coreevents.StreamTypeRun,
		EventType:  coreevents.EventTurnRecorded,
		Payload: coreevents.MustMarshalPayload(coreevents.TurnRecordedPayload{
			TurnID: "turn-013",
		}),
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	waitForNarrativeCondition(t, 2*time.Second, func() bool {
		return writer.Len() == 1
	})
	got := writer.Get(0)
	if got.SessionID != sessionID {
		t.Fatalf("session_id=%q want %q", got.SessionID, sessionID)
	}
	if got.SourceTurnIndex != 13 {
		t.Fatalf("source_turn_index=%d want 13", got.SourceTurnIndex)
	}
	if got.SourceTurnCount != 13 {
		t.Fatalf("source_turn_count=%d want 13", got.SourceTurnCount)
	}
	seen, _ := triggerSeen.Load().(enrichers.NarrativeRefreshTrigger)
	if seen != enrichers.NarrativeRefreshEvent {
		t.Fatalf("trigger=%q want %q", seen, enrichers.NarrativeRefreshEvent)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for component shutdown")
	}
}

func TestNarrativeCompilerComponent_TurnRecordedRefreshesOnTimeTrigger(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.February, 23, 14, 0, 0, 0, time.UTC)
	sessionID := "run-narrative-time-trigger"
	turns := buildIndexedTurns(sessionID, 14)

	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 16,
		OverflowPolicy:   runtimeevents.OverflowDropNewest,
	})
	turnReader := &narrativeTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-014": turns[len(turns)-1],
		},
	}
	timeline := &narrativeTimelineReader{
		turnsBySession: map[string][]run.TurnRecord{
			sessionID: turns,
		},
	}
	narrReader := &narrativeReaderMap{
		records: map[string]run.NarrativeRecord{
			sessionID: {
				SessionID:       sessionID,
				ArtifactVersion: "v1",
				SourceTurnIndex: 12,
				UpdatedAt:       now.Add(-45 * time.Minute),
				Claims: []run.NarrativeClaim{
					{Text: "seed", AnchorRefs: []string{"turn/turn-012"}},
				},
				AnchorRefs: []string{"turn/turn-012"},
			},
		},
	}
	writer := &narrativeWriterCapture{}
	var triggerSeen atomic.Value
	triggerSeen.Store(enrichers.NarrativeRefreshTrigger(""))
	compiler := narrativeCompilerFunc(func(_ context.Context, input enrichers.NarrativeCompileInput) (run.NarrativeRecord, error) {
		triggerSeen.Store(input.Trigger)
		return run.NarrativeRecord{
			Summary: "time refresh",
			Claims: []run.NarrativeClaim{
				{
					Text:       "time-triggered claim",
					AnchorRefs: []string{"turn/turn-014"},
				},
			},
		}, nil
	})

	component := enrichers.NewNarrativeCompilerComponent(enrichers.NarrativeCompilerConfig{
		Bus:                bus,
		TurnReader:         turnReader,
		TurnTimelineReader: timeline,
		NarrativeReader:    narrReader,
		NarrativeWriter:    writer,
		Compiler:           compiler,
		Now:                func() time.Time { return now },
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- component.Run(ctx) }()

	waitForNarrativeCondition(t, 2*time.Second, func() bool {
		return bus.Stats().Subscribers > 0
	})

	if err := bus.Publish(context.Background(), coreevents.Event{
		ID:         "evt-narrative-014",
		StreamID:   sessionID,
		StreamType: coreevents.StreamTypeRun,
		EventType:  coreevents.EventTurnRecorded,
		Payload: coreevents.MustMarshalPayload(coreevents.TurnRecordedPayload{
			TurnID: "turn-014",
		}),
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	waitForNarrativeCondition(t, 2*time.Second, func() bool {
		return writer.Len() == 1
	})
	seen, _ := triggerSeen.Load().(enrichers.NarrativeRefreshTrigger)
	if seen != enrichers.NarrativeRefreshTime {
		t.Fatalf("trigger=%q want %q", seen, enrichers.NarrativeRefreshTime)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for component shutdown")
	}
}

func TestNarrativeCompilerComponent_TurnRecordedSkipsWhenPolicyNotMet(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.February, 23, 15, 0, 0, 0, time.UTC)
	sessionID := "run-narrative-skip"
	turns := buildIndexedTurns(sessionID, 10)

	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 16,
		OverflowPolicy:   runtimeevents.OverflowDropNewest,
	})
	turnReader := &narrativeTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-010": turns[len(turns)-1],
		},
	}
	timeline := &narrativeTimelineReader{
		turnsBySession: map[string][]run.TurnRecord{
			sessionID: turns,
		},
	}
	narrReader := &narrativeReaderMap{
		records: map[string]run.NarrativeRecord{
			sessionID: {
				SessionID:       sessionID,
				ArtifactVersion: "v1",
				SourceTurnIndex: 5,
				UpdatedAt:       now.Add(-5 * time.Minute),
				Claims: []run.NarrativeClaim{
					{Text: "seed", AnchorRefs: []string{"turn/turn-005"}},
				},
				AnchorRefs: []string{"turn/turn-005"},
			},
		},
	}
	writer := &narrativeWriterCapture{}
	compileCalled := make(chan struct{}, 1)
	compiler := narrativeCompilerFunc(func(context.Context, enrichers.NarrativeCompileInput) (run.NarrativeRecord, error) {
		select {
		case compileCalled <- struct{}{}:
		default:
		}
		return run.NarrativeRecord{
			Summary: "should not compile",
			Claims: []run.NarrativeClaim{
				{Text: "noop", AnchorRefs: []string{"turn/turn-010"}},
			},
		}, nil
	})

	component := enrichers.NewNarrativeCompilerComponent(enrichers.NarrativeCompilerConfig{
		Bus:                bus,
		TurnReader:         turnReader,
		TurnTimelineReader: timeline,
		NarrativeReader:    narrReader,
		NarrativeWriter:    writer,
		Compiler:           compiler,
		Now:                func() time.Time { return now },
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- component.Run(ctx) }()

	waitForNarrativeCondition(t, 2*time.Second, func() bool {
		return bus.Stats().Subscribers > 0
	})

	if err := bus.Publish(context.Background(), coreevents.Event{
		ID:         "evt-narrative-010",
		StreamID:   sessionID,
		StreamType: coreevents.StreamTypeRun,
		EventType:  coreevents.EventTurnRecorded,
		Payload: coreevents.MustMarshalPayload(coreevents.TurnRecordedPayload{
			TurnID: "turn-010",
		}),
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	select {
	case <-compileCalled:
		t.Fatal("unexpected compile invocation")
	case <-time.After(200 * time.Millisecond):
	}
	if writer.Len() != 0 {
		t.Fatalf("writer len=%d want 0", writer.Len())
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for component shutdown")
	}
}

func TestNarrativeCompilerComponent_ManualRefreshForcesCompile(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.February, 23, 16, 0, 0, 0, time.UTC)
	sessionID := "run-narrative-manual"
	turns := buildIndexedTurns(sessionID, 3)
	writer := &narrativeWriterCapture{}

	component := enrichers.NewNarrativeCompilerComponent(enrichers.NarrativeCompilerConfig{
		TurnReader: &narrativeTurnReader{
			turns: map[string]run.TurnRecord{
				"turn-003": turns[len(turns)-1],
			},
		},
		TurnTimelineReader: &narrativeTimelineReader{
			turnsBySession: map[string][]run.TurnRecord{
				sessionID: turns,
			},
		},
		NarrativeReader: &narrativeReaderMap{
			records: map[string]run.NarrativeRecord{
				sessionID: {
					SessionID:       sessionID,
					ArtifactVersion: "v1",
					SourceTurnIndex: 3,
					UpdatedAt:       now,
					Claims: []run.NarrativeClaim{
						{Text: "seed", AnchorRefs: []string{"turn/turn-003"}},
					},
					AnchorRefs: []string{"turn/turn-003"},
				},
			},
		},
		NarrativeWriter: writer,
		Compiler: narrativeCompilerFunc(func(_ context.Context, input enrichers.NarrativeCompileInput) (run.NarrativeRecord, error) {
			if input.Trigger != enrichers.NarrativeRefreshManual {
				t.Fatalf("trigger=%q want %q", input.Trigger, enrichers.NarrativeRefreshManual)
			}
			return run.NarrativeRecord{
				Summary: "manual refresh",
				Claims: []run.NarrativeClaim{
					{Text: "manual claim", AnchorRefs: []string{"turn/turn-003"}},
				},
			}, nil
		}),
		Now: func() time.Time { return now },
	})

	if err := component.RefreshSession(context.Background(), sessionID); err != nil {
		t.Fatalf("RefreshSession() error = %v", err)
	}
	if writer.Len() != 1 {
		t.Fatalf("writer len=%d want 1", writer.Len())
	}
}

func TestNarrativeCompilerComponent_CompilerErrorReportsWithoutBlocking(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.February, 23, 17, 0, 0, 0, time.UTC)
	sessionID := "run-narrative-error"
	turns := buildIndexedTurns(sessionID, 12)

	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 16,
		OverflowPolicy:   runtimeevents.OverflowDropNewest,
	})
	turnReader := &narrativeTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-012": turns[len(turns)-1],
		},
	}
	timeline := &narrativeTimelineReader{
		turnsBySession: map[string][]run.TurnRecord{
			sessionID: turns,
		},
	}
	narrReader := &narrativeReaderMap{
		records: map[string]run.NarrativeRecord{},
	}
	errs := make(chan error, 1)
	component := enrichers.NewNarrativeCompilerComponent(enrichers.NarrativeCompilerConfig{
		Bus:                bus,
		TurnReader:         turnReader,
		TurnTimelineReader: timeline,
		NarrativeReader:    narrReader,
		NarrativeWriter:    &narrativeWriterCapture{},
		Compiler: narrativeCompilerFunc(func(context.Context, enrichers.NarrativeCompileInput) (run.NarrativeRecord, error) {
			return run.NarrativeRecord{}, errors.New("forced narrative failure")
		}),
		Now: func() time.Time { return now },
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
	go func() { done <- component.Run(ctx) }()

	waitForNarrativeCondition(t, 2*time.Second, func() bool {
		return bus.Stats().Subscribers > 0
	})

	start := time.Now()
	if err := bus.Publish(context.Background(), coreevents.Event{
		ID:         "evt-narrative-error",
		StreamID:   sessionID,
		StreamType: coreevents.StreamTypeRun,
		EventType:  coreevents.EventTurnRecorded,
		Payload: coreevents.MustMarshalPayload(coreevents.TurnRecordedPayload{
			TurnID: "turn-012",
		}),
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 1*time.Second {
		t.Fatalf("publish blocked too long: %s", elapsed)
	}

	waitForNarrativeCondition(t, 2*time.Second, func() bool {
		return len(errs) > 0
	})

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for component shutdown")
	}
}

func TestDeterministicNarrativeCompiler_RequiresNow(t *testing.T) {
	t.Parallel()

	compiler := enrichers.DeterministicNarrativeCompiler{}
	_, err := compiler.Compile(context.Background(), enrichers.NarrativeCompileInput{
		SessionID: "run-narrative-now-required",
		Turns: []run.TurnRecord{
			{
				ID:        "turn-001",
				SessionID: "run-narrative-now-required",
				TurnIndex: 1,
				Prompt:    "hello",
			},
		},
		Now: time.Time{},
	})
	if err == nil {
		t.Fatal("Compile() error=nil want non-nil")
	}
	if !strings.Contains(err.Error(), "now timestamp is required") {
		t.Fatalf("Compile() error=%q want missing now timestamp message", err.Error())
	}
}

type narrativeCompilerFunc func(ctx context.Context, input enrichers.NarrativeCompileInput) (run.NarrativeRecord, error)

func (f narrativeCompilerFunc) Compile(ctx context.Context, input enrichers.NarrativeCompileInput) (run.NarrativeRecord, error) {
	return f(ctx, input)
}

type narrativeTurnReader struct {
	mu    sync.Mutex
	turns map[string]run.TurnRecord
}

func (r *narrativeTurnReader) GetTurn(_ context.Context, turnID string) (run.TurnRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	turn, ok := r.turns[turnID]
	if !ok {
		return run.TurnRecord{}, run.ErrTurnNotFound
	}
	return turn.Clone(), nil
}

type narrativeTimelineReader struct {
	mu             sync.Mutex
	turnsBySession map[string][]run.TurnRecord
}

func (r *narrativeTimelineReader) ListTurns(_ context.Context, sessionID string, opts run.TurnListOptions) ([]run.TurnRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	turns := append([]run.TurnRecord(nil), r.turnsBySession[strings.TrimSpace(sessionID)]...)
	sort.SliceStable(turns, func(i, j int) bool {
		if turns[i].TurnIndex == turns[j].TurnIndex {
			return turns[i].ID < turns[j].ID
		}
		if opts.Asc {
			return turns[i].TurnIndex < turns[j].TurnIndex
		}
		return turns[i].TurnIndex > turns[j].TurnIndex
	})
	limit := opts.Limit
	if limit > 0 && len(turns) > limit {
		turns = turns[:limit]
	}
	out := make([]run.TurnRecord, len(turns))
	for i := range turns {
		out[i] = turns[i].Clone()
	}
	return out, nil
}

type narrativeReaderMap struct {
	mu      sync.Mutex
	records map[string]run.NarrativeRecord
}

func (r *narrativeReaderMap) GetNarrative(_ context.Context, sessionID, _ string) (run.NarrativeRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[strings.TrimSpace(sessionID)]
	if !ok {
		return run.NarrativeRecord{}, run.ErrNarrativeNotFound
	}
	return record.Clone(), nil
}

type narrativeWriterCapture struct {
	mu      sync.Mutex
	records []run.NarrativeRecord
}

func (w *narrativeWriterCapture) SaveNarrative(_ context.Context, narrative run.NarrativeRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.records = append(w.records, narrative.Clone())
	return nil
}

func (w *narrativeWriterCapture) Len() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.records)
}

func (w *narrativeWriterCapture) Get(i int) run.NarrativeRecord {
	w.mu.Lock()
	defer w.mu.Unlock()
	if i < 0 || i >= len(w.records) {
		return run.NarrativeRecord{}
	}
	return w.records[i].Clone()
}

func buildIndexedTurns(sessionID string, n int) []run.TurnRecord {
	out := make([]run.TurnRecord, 0, n)
	for i := 1; i <= n; i++ {
		turnID := fmt.Sprintf("turn-%03d", i)
		out = append(out, run.TurnRecord{
			ID:        turnID,
			SessionID: sessionID,
			TurnIndex: i,
			Prompt:    "prompt " + turnID,
			FinalOutput: run.MessageRef{
				ID:   "msg-" + turnID,
				Role: "assistant",
				Text: "final output " + turnID,
			},
			CreatedAt: time.Date(2026, time.February, 23, 12, i, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, time.February, 23, 12, i, 0, 0, time.UTC),
		})
	}
	return out
}

func waitForNarrativeCondition(t *testing.T, timeout time.Duration, condition func() bool) {
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
