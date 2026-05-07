package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	tursoevents "github.com/joshka0/foxctl/internal/v2/adapters/turso/events"
	tursoprojections "github.com/joshka0/foxctl/internal/v2/adapters/turso/projections"
	v2events "github.com/joshka0/foxctl/internal/v2/core/events"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
)

type fakeJidoRunStateReader struct {
	states         []tursoprojections.RunState
	notFoundReads  int
	err            error
	currentStateIx int
}

type fakeJidoRunEventReader struct {
	events []v2events.Event
	err    error
}

func (f *fakeJidoRunEventReader) ListStream(context.Context, v2events.StreamFilter) ([]v2events.Event, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]v2events.Event(nil), f.events...), nil
}

func (f *fakeJidoRunStateReader) GetRunState(context.Context, string) (tursoprojections.RunState, error) {
	if f.notFoundReads > 0 {
		f.notFoundReads--
		return tursoprojections.RunState{}, tursoprojections.ErrNotFound
	}
	if f.err != nil {
		return tursoprojections.RunState{}, f.err
	}
	if len(f.states) == 0 {
		return tursoprojections.RunState{}, tursoprojections.ErrNotFound
	}
	idx := f.currentStateIx
	if idx >= len(f.states) {
		idx = len(f.states) - 1
	}
	if f.currentStateIx < len(f.states)-1 {
		f.currentStateIx++
	}
	return f.states[idx], nil
}

func TestResolveJidoTerminalCallback_ReturnsLatestTerminalPayload(t *testing.T) {
	t.Parallel()

	reader := &fakeJidoRunEventReader{
		events: []v2events.Event{
			{
				ID:        "evt-started",
				EventType: v2events.EventRunStarted,
				Payload:   json.RawMessage(`{"status":"dispatching"}`),
			},
			{
				ID:        "evt-completed",
				EventType: v2events.EventRunCompleted,
				Payload: json.RawMessage(`{
					"status":"completed",
					"summary":"tool=fs/ls envelope_status=ok",
					"error":"",
					"metadata":{"tool":"fs/ls","envelope_status":"ok"}
				}`),
			},
		},
	}

	callback, err := resolveJidoTerminalCallback(context.Background(), reader, "ask-evt-1")
	if err != nil {
		t.Fatalf("resolveJidoTerminalCallback() error = %v", err)
	}
	if callback.EventID != "evt-completed" {
		t.Fatalf("event_id=%q want evt-completed", callback.EventID)
	}
	if callback.Status != "completed" {
		t.Fatalf("status=%q want completed", callback.Status)
	}
	if callback.Summary == "" {
		t.Fatal("expected non-empty callback summary")
	}
	if callback.Metadata["tool"] != "fs/ls" {
		t.Fatalf("metadata.tool=%v want fs/ls", callback.Metadata["tool"])
	}
}

func TestResolveJidoTerminalCallback_NotFoundWhenNoTerminalEvent(t *testing.T) {
	t.Parallel()

	reader := &fakeJidoRunEventReader{
		events: []v2events.Event{
			{ID: "evt-started", EventType: v2events.EventRunStarted},
		},
	}

	_, err := resolveJidoTerminalCallback(context.Background(), reader, "ask-evt-2")
	if err == nil {
		t.Fatal("expected not found error")
	}
	if !errors.Is(err, v2events.ErrNotFound) {
		t.Fatalf("error=%v want ErrNotFound", err)
	}
}

func TestResolveJidoTerminalCallback_FailedFallsBackFromEventType(t *testing.T) {
	t.Parallel()

	reader := &fakeJidoRunEventReader{
		events: []v2events.Event{
			{
				ID:        "evt-failed",
				EventType: v2events.EventRunFailed,
				Payload: json.RawMessage(`{
					"summary":"tool=code/semantic_search envelope_status=error",
					"error":"tool failed",
					"metadata":{"tool":"code/semantic_search","envelope_status":"error"}
				}`),
			},
		},
	}

	callback, err := resolveJidoTerminalCallback(context.Background(), reader, "ask-evt-3")
	if err != nil {
		t.Fatalf("resolveJidoTerminalCallback() error = %v", err)
	}
	if callback.EventID != "evt-failed" {
		t.Fatalf("event_id=%q want evt-failed", callback.EventID)
	}
	if callback.Status != "failed" {
		t.Fatalf("status=%q want failed", callback.Status)
	}
	if callback.Error != "tool failed" {
		t.Fatalf("error=%q want tool failed", callback.Error)
	}
}

func TestWaitForJidoRunStateWithPoll_CompletesAfterRunning(t *testing.T) {
	t.Parallel()

	reader := &fakeJidoRunStateReader{
		states: []tursoprojections.RunState{
			{RunID: "ask:ask-1", Status: "running"},
			{RunID: "ask:ask-1", Status: "completed", LastEventID: "evt-done"},
		},
	}

	state, err := waitForJidoRunStateWithPoll(context.Background(), reader, "ask-1", 200*time.Millisecond, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("waitForJidoRunStateWithPoll() error = %v", err)
	}
	if state.Status != "completed" {
		t.Fatalf("status=%q want completed", state.Status)
	}
	if state.LastEventID != "evt-done" {
		t.Fatalf("last_event_id=%q want evt-done", state.LastEventID)
	}
}

func TestWaitForJidoRunStateWithPoll_ReturnsFailedTerminalState(t *testing.T) {
	t.Parallel()

	reader := &fakeJidoRunStateReader{
		states: []tursoprojections.RunState{
			{RunID: "ask:ask-2", Status: "failed"},
		},
	}

	state, err := waitForJidoRunStateWithPoll(context.Background(), reader, "ask-2", 100*time.Millisecond, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("waitForJidoRunStateWithPoll() error = %v", err)
	}
	if state.Status != "failed" {
		t.Fatalf("status=%q want failed", state.Status)
	}
}

func TestWaitForJidoRunStateWithPoll_TimesOutWhenStateNeverAppears(t *testing.T) {
	t.Parallel()

	reader := &fakeJidoRunStateReader{notFoundReads: 1000}

	_, err := waitForJidoRunStateWithPoll(context.Background(), reader, "ask-3", 40*time.Millisecond, 5*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timeout waiting for jido ask run to complete") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitForJidoRunStateWithPoll_ReturnsReaderError(t *testing.T) {
	t.Parallel()

	reader := &fakeJidoRunStateReader{err: errors.New("db unavailable")}

	_, err := waitForJidoRunStateWithPoll(context.Background(), reader, "ask-4", 100*time.Millisecond, 5*time.Millisecond)
	if err == nil {
		t.Fatal("expected reader error")
	}
	if !strings.Contains(err.Error(), "db unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAskWaitFailureHint_IncludesStructuredCallbackDetails(t *testing.T) {
	t.Parallel()

	hint := askWaitFailureHint(jidoTerminalCallback{
		EventID: "evt-123",
		Status:  "failed",
		Summary: "tool=fs/ls envelope_status=error",
		Metadata: map[string]any{
			"tool":            "fs/ls",
			"envelope_status": "error",
		},
	})

	if !strings.Contains(hint, "callback_event_id=evt-123") {
		t.Fatalf("hint missing callback_event_id: %q", hint)
	}
	if !strings.Contains(hint, "callback_status=failed") {
		t.Fatalf("hint missing callback_status: %q", hint)
	}
	if !strings.Contains(hint, "callback_summary=tool=fs/ls envelope_status=error") {
		t.Fatalf("hint missing callback_summary: %q", hint)
	}
	if !strings.Contains(hint, "callback_metadata=") {
		t.Fatalf("hint missing callback_metadata: %q", hint)
	}
}

func TestBuildAskRunResponseData_IncludesCallbackDetails(t *testing.T) {
	t.Parallel()

	data := buildAskRunResponseData("ask-123", tursoprojections.RunState{
		RunID:       "ask:ask-123",
		Status:      "completed",
		LastEventID: "evt-123",
		RequestID:   "req-123",
		ActorID:     "actor:coder:123",
	}, jidoTerminalCallback{
		EventID: "evt-callback-123",
		Status:  "completed",
		Summary: "tool=fs/ls envelope_status=ok",
		Metadata: map[string]any{
			"tool": "fs/ls",
		},
	})

	if data["ask_id"] != "ask-123" {
		t.Fatalf("ask_id=%v want ask-123", data["ask_id"])
	}
	if data["callback_event_id"] != "evt-callback-123" {
		t.Fatalf("callback_event_id=%v want evt-callback-123", data["callback_event_id"])
	}
	if data["callback_status"] != "completed" {
		t.Fatalf("callback_status=%v want completed", data["callback_status"])
	}
}

func TestRunAgentAskStatus_ReportsNotFound(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfg := config.Config{Storage: config.StorageSettings{Root: tmp}}

	cmd := &cobra.Command{Use: "agent ask-status"}
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetContext(config.WithContext(context.Background(), cfg))

	err := runAgentAskStatus(cmd, []string{"missing-ask"})
	if err == nil {
		t.Fatal("expected error for missing ask")
	}

	var env envelope.Envelope
	if unmarshalErr := json.Unmarshal(out.Bytes(), &env); unmarshalErr != nil {
		t.Fatalf("decode envelope: %v", unmarshalErr)
	}
	if env.Status != "error" {
		t.Fatalf("status=%q want error", env.Status)
	}
	if env.Command != "agent/ask_status" {
		t.Fatalf("command=%q want agent/ask_status", env.Command)
	}
	if env.Error.Code != string(protocol.ErrorCodeENotFound) {
		t.Fatalf("error.code=%q want %q", env.Error.Code, protocol.ErrorCodeENotFound)
	}
}

func TestRunAgentAskStatus_ReturnsCallbackEnrichedStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmp := t.TempDir()
	now := time.Now().UTC()
	askID := "ask-" + ulid.Make().String()
	runID := "ask:" + askID
	requestID := "req-" + ulid.Make().String()
	actorID := "actor:coder:test"

	eventStore, err := tursoevents.Open(ctx, tmp)
	if err != nil {
		t.Fatalf("open event store: %v", err)
	}
	t.Cleanup(func() { _ = eventStore.Close() })

	projStore, closeProj, err := tursoprojections.Open(ctx, tmp)
	if err != nil {
		t.Fatalf("open projection store: %v", err)
	}
	t.Cleanup(func() {
		if closeProj != nil {
			_ = closeProj()
		}
	})

	startEvt := v2events.Event{
		ID:            "evt-start-" + ulid.Make().String(),
		StreamID:      runID,
		StreamType:    v2events.StreamTypeRun,
		EventType:     v2events.EventRunStarted,
		OccurredAt:    now,
		CorrelationID: askID,
		RequestID:     requestID,
		ActorID:       actorID,
		Command:       "ask",
		Payload:       json.RawMessage(`{"status":"dispatching"}`),
	}
	completeEvt := v2events.Event{
		ID:            "evt-complete-" + ulid.Make().String(),
		StreamID:      runID,
		StreamType:    v2events.StreamTypeRun,
		EventType:     v2events.EventRunCompleted,
		OccurredAt:    now.Add(10 * time.Millisecond),
		CorrelationID: askID,
		RequestID:     requestID,
		ActorID:       actorID,
		Command:       "ask",
		Payload: json.RawMessage(`{
			"status":"completed",
			"summary":"tool=fs/ls envelope_status=ok",
			"metadata":{"tool":"fs/ls","envelope_status":"ok"}
		}`),
	}

	if err := eventStore.Append(ctx, startEvt); err != nil {
		t.Fatalf("append start event: %v", err)
	}
	if err := projStore.Apply(ctx, startEvt); err != nil {
		t.Fatalf("apply start projection: %v", err)
	}
	if err := eventStore.Append(ctx, completeEvt); err != nil {
		t.Fatalf("append complete event: %v", err)
	}
	if err := projStore.Apply(ctx, completeEvt); err != nil {
		t.Fatalf("apply complete projection: %v", err)
	}

	cfg := config.Config{Storage: config.StorageSettings{Root: tmp}}
	cmd := &cobra.Command{Use: "agent ask-status"}
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetContext(config.WithContext(ctx, cfg))

	if err := runAgentAskStatus(cmd, []string{askID}); err != nil {
		t.Fatalf("runAgentAskStatus() error = %v", err)
	}

	var env envelope.Envelope
	if unmarshalErr := json.Unmarshal(out.Bytes(), &env); unmarshalErr != nil {
		t.Fatalf("decode envelope: %v", unmarshalErr)
	}
	if env.Status != "ok" {
		t.Fatalf("status=%q want ok", env.Status)
	}
	if env.Command != "agent/ask_status" {
		t.Fatalf("command=%q want agent/ask_status", env.Command)
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type=%T want map[string]any", env.Data)
	}
	if data["ask_id"] != askID {
		t.Fatalf("ask_id=%v want %s", data["ask_id"], askID)
	}
	if data["status"] != "completed" {
		t.Fatalf("status=%v want completed", data["status"])
	}
	if data["callback_status"] != "completed" {
		t.Fatalf("callback_status=%v want completed", data["callback_status"])
	}
	if data["callback_summary"] != "tool=fs/ls envelope_status=ok" {
		t.Fatalf("callback_summary=%v unexpected", data["callback_summary"])
	}
	if data["callback_event_id"] != completeEvt.ID {
		t.Fatalf("callback_event_id=%v want %s", data["callback_event_id"], completeEvt.ID)
	}
}
