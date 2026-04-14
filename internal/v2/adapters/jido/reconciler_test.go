package jido

import (
	"context"
	"testing"
	"time"

	v2events "github.com/joshka0/foxctl/internal/v2/core/events"
)

func TestReconciler_RecordAskDispatchedAndCallback(t *testing.T) {
	t.Parallel()

	store := &fakeEventAppender{}
	proj := &fakeProjectionApplier{}
	now := time.Date(2026, time.March, 5, 12, 0, 0, 0, time.UTC)
	reconciler, err := NewReconciler(ReconcilerConfig{
		Events:      store,
		Projections: proj,
		Now:         func() time.Time { return now },
		NewID:       func() string { return "id-1" },
	})
	if err != nil {
		t.Fatalf("NewReconciler() error = %v", err)
	}

	evt, err := reconciler.RecordAskDispatched(context.Background(), "msg-1", SignalRequest{
		RequestID: "req-1",
		AgentID:   "agent-1",
		Signal: Signal{
			ID:            "ask-1",
			CorrelationID: "ask-1",
			Type:          DefaultAskSignal,
		},
	})
	if err != nil {
		t.Fatalf("RecordAskDispatched() error = %v", err)
	}
	if evt.EventType != v2events.EventRunStarted {
		t.Fatalf("event_type=%q want %q", evt.EventType, v2events.EventRunStarted)
	}
	if evt.StreamID != "ask:ask-1" {
		t.Fatalf("stream_id=%q want ask:ask-1", evt.StreamID)
	}
	if len(store.events) != 1 || len(proj.events) != 1 {
		t.Fatalf("events=%d projection=%d want both 1", len(store.events), len(proj.events))
	}

	cb, err := reconciler.ReconcileSignalCallback(context.Background(), SignalCallback{
		AskID:     "ask-1",
		RequestID: "req-1",
		AgentID:   "agent-1",
		Status:    "completed",
		Summary:   "done",
	})
	if err != nil {
		t.Fatalf("ReconcileSignalCallback() error = %v", err)
	}
	if cb.EventType != v2events.EventRunCompleted {
		t.Fatalf("event_type=%q want %q", cb.EventType, v2events.EventRunCompleted)
	}
	if len(store.events) != 2 || len(proj.events) != 2 {
		t.Fatalf("events=%d projection=%d want both 2", len(store.events), len(proj.events))
	}
}

type fakeEventAppender struct {
	events []v2events.Event
}

func (f *fakeEventAppender) Append(_ context.Context, event v2events.Event) error {
	f.events = append(f.events, event)
	return nil
}

type fakeProjectionApplier struct {
	events []v2events.Event
}

func (f *fakeProjectionApplier) Apply(_ context.Context, evt v2events.Event) error {
	f.events = append(f.events, evt)
	return nil
}
