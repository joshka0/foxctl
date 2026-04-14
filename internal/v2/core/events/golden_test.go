package events_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/v2/core/events"
	"github.com/joshka0/foxctl/internal/v2/testkit/fakes"
	"github.com/joshka0/foxctl/internal/v2/testkit/golden"
)

func TestEventsJSONL_GoldenStableOutput(t *testing.T) {
	t.Parallel()

	store := fakes.NewFakeEventStore()
	clock := fakes.NewFakeClock(time.Date(2026, time.February, 18, 12, 0, 0, 0, time.UTC), time.Second)
	ids := fakes.NewFakeUUID("evt")

	appendEvent := func(version, sequence int64, eventType events.EventType, payload any) {
		t.Helper()
		raw, err := events.MarshalPayload(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		err = store.Append(context.Background(), events.Event{
			ID:            ids.New(),
			StreamID:      "run-0001",
			StreamType:    events.StreamTypeRun,
			StreamVersion: version,
			Sequence:      sequence,
			EventType:     eventType,
			OccurredAt:    clock.Now(),
			CorrelationID: "corr-001",
			CausationID:   "cause-001",
			ActorID:       "actor-overseer",
			RequestID:     "req-001",
			Command:       "spawn",
			Payload:       raw,
		})
		if err != nil {
			t.Fatalf("append event: %v", err)
		}
	}

	appendEvent(1, 1, events.EventRunStarted, events.RunStartedPayload{Mode: "autonomous"})
	appendEvent(2, 2, events.EventToolInvoked, events.ToolInvokedPayload{Name: "agent_spawn", IterationIndex: 1})
	appendEvent(3, 3, events.EventToolResponded, events.ToolRespondedPayload{Name: "agent_spawn", Status: "ok"})
	appendEvent(4, 4, events.EventTurnRecorded, events.TurnRecordedPayload{TurnID: "turn-0001", Iterations: 1, ToolCalls: 1})
	appendEvent(5, 5, events.EventRunCompleted, events.RunCompletedPayload{Summary: "completed"})

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	goldenPath := filepath.Join(
		filepath.Dir(thisFile),
		"..", "..", "runtime", "runner", "testdata", "golden_events", "pr01b_event_contract.jsonl",
	)
	golden.AssertEventsJSONLMatchesFile(t, store.Events(), goldenPath)
}
