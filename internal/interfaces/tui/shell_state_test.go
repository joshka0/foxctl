package tui

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDefaultShellStateUsesWorkspaceFallback(t *testing.T) {
	state := DefaultShellState(Options{})

	if state.Workspace != "." {
		t.Fatalf("Workspace = %q, want %q", state.Workspace, ".")
	}
	if state.EpicStatus != "READY" {
		t.Fatalf("EpicStatus = %q, want READY", state.EpicStatus)
	}
	if len(state.Transcript) == 0 {
		t.Fatal("Transcript is empty")
	}
}

func TestNextRailCyclesDeterministically(t *testing.T) {
	if got := nextRail(RailMemory, 1); got != RailContinuity {
		t.Fatalf("nextRail(RailMemory, 1) = %v, want %v", got, RailContinuity)
	}
	if got := nextRail(RailMemory, -1); got != RailTask {
		t.Fatalf("nextRail(RailMemory, -1) = %v, want %v", got, RailTask)
	}
	if got := nextRail(RailTask, 1); got != RailMemory {
		t.Fatalf("nextRail(RailTask, 1) = %v, want %v", got, RailMemory)
	}
}

func TestShellComposerSubmitAppendsDraft(t *testing.T) {
	shell := NewShell(DefaultShellState(Options{}))
	before := len(shell.state.Get().Transcript)

	shell.updateComposer("ship the first slice")
	shell.submitComposer()

	state := shell.state.Get()
	if state.Composer != "" {
		t.Fatalf("Composer = %q, want empty", state.Composer)
	}
	if len(state.Transcript) != before+1 {
		t.Fatalf("Transcript length = %d, want %d", len(state.Transcript), before+1)
	}
	entry := state.Transcript[len(state.Transcript)-1]
	if entry.Speaker != "you" || entry.Kind != "draft" || entry.Text != "ship the first slice" {
		t.Fatalf("last entry = %#v, want user draft", entry)
	}
}

func TestShellComposerBackspaceHandlesRunes(t *testing.T) {
	shell := NewShell(DefaultShellState(Options{}))

	shell.updateComposer("abc")
	shell.backspaceComposer()

	if got := shell.state.Get().Composer; got != "ab" {
		t.Fatalf("Composer = %q, want %q", got, "ab")
	}
}

func TestShellComposerSubmitEnqueuesWhenConfigured(t *testing.T) {
	queued := make(chan AskConsoleSessionRequest, 1)
	shell := NewShellWithRuntime(
		DefaultShellState(Options{}),
		nil,
		nil,
		func(_ context.Context, req AskConsoleSessionRequest) error {
			queued <- req
			return nil
		},
		0,
		50*time.Millisecond,
	)
	before := len(shell.state.Get().Transcript)

	shell.updateComposer("  ship the next slice  ")
	shell.submitComposer()

	state := shell.state.Get()
	if state.Composer != "" {
		t.Fatalf("Composer = %q, want empty", state.Composer)
	}
	if len(state.Transcript) != before+1 {
		t.Fatalf("Transcript length = %d, want %d", len(state.Transcript), before+1)
	}
	entry := state.Transcript[len(state.Transcript)-1]
	if entry.Speaker != "you" || entry.Kind != "pending" || entry.Text != "ship the next slice" {
		t.Fatalf("last entry = %#v, want pending row", entry)
	}

	select {
	case req := <-queued:
		if req.Content != "ship the next slice" {
			t.Fatalf("queued content = %q, want %q", req.Content, "ship the next slice")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for queued ask request")
	}
}

func TestShellComposerSubmitEnqueueFailureAppendsError(t *testing.T) {
	shell := NewShellWithRuntime(
		DefaultShellState(Options{}),
		nil,
		nil,
		func(_ context.Context, _ AskConsoleSessionRequest) error {
			return errors.New("queue full")
		},
		0,
		50*time.Millisecond,
	)
	before := len(shell.state.Get().Transcript)

	shell.updateComposer("ship the next slice")
	shell.submitComposer()

	state := shell.state.Get()
	if state.Composer != "" {
		t.Fatalf("Composer = %q, want empty", state.Composer)
	}
	if len(state.Transcript) != before+2 {
		t.Fatalf("Transcript length = %d, want %d", len(state.Transcript), before+2)
	}
	pending := state.Transcript[len(state.Transcript)-2]
	if pending.Speaker != "you" || pending.Kind != "pending" || pending.Text != "ship the next slice" {
		t.Fatalf("pending row = %#v, want pending row", pending)
	}
	failed := state.Transcript[len(state.Transcript)-1]
	if failed.Speaker != "system" || failed.Kind != "error" || failed.Text != "ask enqueue failed: queue full" {
		t.Fatalf("failure row = %#v, want deterministic enqueue error row", failed)
	}
}

func TestShellComposerSubmitUsesBoundedEnqueueTimeout(t *testing.T) {
	shell := NewShellWithRuntime(
		DefaultShellState(Options{}),
		nil,
		nil,
		func(ctx context.Context, _ AskConsoleSessionRequest) error {
			<-ctx.Done()
			return ctx.Err()
		},
		0,
		25*time.Millisecond,
	)
	before := len(shell.state.Get().Transcript)
	startedAt := time.Now()

	shell.updateComposer("ship the next slice")
	shell.submitComposer()

	if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("submitComposer elapsed = %s, want <= 500ms", elapsed)
	}

	state := shell.state.Get()
	if len(state.Transcript) != before+2 {
		t.Fatalf("Transcript length = %d, want %d", len(state.Transcript), before+2)
	}
	failed := state.Transcript[len(state.Transcript)-1]
	if failed.Speaker != "system" || failed.Kind != "error" {
		t.Fatalf("failure row = %#v, want system error row", failed)
	}
	if failed.Text != "ask enqueue failed: context deadline exceeded" {
		t.Fatalf("failure text = %q, want timeout message", failed.Text)
	}
}

func TestShellCancelWithoutRuntimeIsNoOp(t *testing.T) {
	shell := NewShell(DefaultShellState(Options{}))
	before := shell.state.Get()

	shell.submitCancel()

	after := shell.state.Get()
	if len(after.Transcript) != len(before.Transcript) {
		t.Fatalf("len(transcript) = %d, want %d", len(after.Transcript), len(before.Transcript))
	}
}

func TestShellCancelEnqueuesCurrentCorrelationFromAcceptedAsk(t *testing.T) {
	queued := make(chan CancelConsoleSessionRequest, 1)
	shell := NewShellWithRuntimes(
		DefaultShellState(Options{}),
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, req CancelConsoleSessionRequest) error {
			queued <- req
			return nil
		},
		0,
		defaultComposerAskEnqueueTimeout,
		50*time.Millisecond,
	)

	shell.handleConsoleAskUpdate(ConsoleAskUpdate{
		Type: ConsoleAskUpdateAccepted,
		Accepted: &ConsoleAskAccepted{
			CorrelationID: "corr-ask-1",
		},
	})
	shell.submitCancel()

	select {
	case req := <-queued:
		if req.CorrelationID != "corr-ask-1" {
			t.Fatalf("queued correlation_id = %q, want %q", req.CorrelationID, "corr-ask-1")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for queued cancel request")
	}
}

func TestShellCancelBroadWhenNoCorrelation(t *testing.T) {
	queued := make(chan CancelConsoleSessionRequest, 1)
	shell := NewShellWithRuntimes(
		DefaultShellState(Options{}),
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, req CancelConsoleSessionRequest) error {
			queued <- req
			return nil
		},
		0,
		defaultComposerAskEnqueueTimeout,
		50*time.Millisecond,
	)
	before := len(shell.state.Get().Transcript)

	shell.submitCancel()

	select {
	case req := <-queued:
		if req.CorrelationID != "" {
			t.Fatalf("queued correlation_id = %q, want empty for broad cancel", req.CorrelationID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for queued cancel request")
	}

	state := shell.state.Get()
	if len(state.Transcript) != before+1 {
		t.Fatalf("len(transcript) = %d, want %d", len(state.Transcript), before+1)
	}
	last := state.Transcript[len(state.Transcript)-1]
	if last.Speaker != "system" || last.Kind != "status" || last.Text != "cancel requested: broad" {
		t.Fatalf("last row = %#v, want deterministic broad cancel status row", last)
	}
}

func TestShellCancelEnqueuesCorrelationFromStreamEvent(t *testing.T) {
	queued := make(chan CancelConsoleSessionRequest, 1)
	shell := NewShellWithRuntimes(
		DefaultShellState(Options{}),
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, req CancelConsoleSessionRequest) error {
			queued <- req
			return nil
		},
		0,
		defaultComposerAskEnqueueTimeout,
		50*time.Millisecond,
	)

	shell.handleConsoleStreamUpdate(ConsoleStreamUpdate{
		Type: ConsoleStreamUpdateEvent,
		Event: ConsoleStreamEvent{
			Type: "event",
			Payload: &ConsoleEventPayload{
				Type:          "event",
				CorrelationID: "corr-stream-1",
				Content:       "working",
			},
		},
	})
	shell.submitCancel()

	select {
	case req := <-queued:
		if req.CorrelationID != "corr-stream-1" {
			t.Fatalf("queued correlation_id = %q, want %q", req.CorrelationID, "corr-stream-1")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for queued cancel request")
	}
}

func TestShellCancelFallsBackToBroadAfterStreamDoneClearsInFlight(t *testing.T) {
	queued := make(chan CancelConsoleSessionRequest, 1)
	shell := NewShellWithRuntimes(
		DefaultShellState(Options{}),
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, req CancelConsoleSessionRequest) error {
			queued <- req
			return nil
		},
		0,
		defaultComposerAskEnqueueTimeout,
		50*time.Millisecond,
	)

	shell.handleConsoleStreamUpdate(ConsoleStreamUpdate{
		Type: ConsoleStreamUpdateEvent,
		Event: ConsoleStreamEvent{
			Type: "event",
			Payload: &ConsoleEventPayload{
				Type:          "event",
				CorrelationID: "corr-stream-2",
				Content:       "working",
			},
		},
	})
	shell.handleConsoleStreamUpdate(ConsoleStreamUpdate{Type: ConsoleStreamUpdateDone})
	shell.submitCancel()

	select {
	case req := <-queued:
		if req.CorrelationID != "" {
			t.Fatalf("queued correlation_id = %q, want empty after stream done", req.CorrelationID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for queued cancel request")
	}
}

func TestShellCancelFallsBackToBroadAfterReplyClearsMatchingInFlight(t *testing.T) {
	queued := make(chan CancelConsoleSessionRequest, 1)
	shell := NewShellWithRuntimes(
		DefaultShellState(Options{}),
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, req CancelConsoleSessionRequest) error {
			queued <- req
			return nil
		},
		0,
		defaultComposerAskEnqueueTimeout,
		50*time.Millisecond,
	)

	shell.handleConsoleStreamUpdate(ConsoleStreamUpdate{
		Type: ConsoleStreamUpdateEvent,
		Event: ConsoleStreamEvent{
			Type: "event",
			Payload: &ConsoleEventPayload{
				Type:          "event",
				CorrelationID: "corr-stream-3",
				Content:       "working",
			},
		},
	})
	shell.handleConsoleStreamUpdate(ConsoleStreamUpdate{
		Type: ConsoleStreamUpdateEvent,
		Event: ConsoleStreamEvent{
			Type: "reply",
			Payload: &ConsoleEventPayload{
				Type:          "reply",
				CorrelationID: "corr-stream-3",
				Content:       "done",
			},
		},
	})
	shell.submitCancel()

	select {
	case req := <-queued:
		if req.CorrelationID != "" {
			t.Fatalf("queued correlation_id = %q, want empty after matching reply", req.CorrelationID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for queued cancel request")
	}
}

func TestShellCancelKeepsInFlightAfterUnrelatedReply(t *testing.T) {
	queued := make(chan CancelConsoleSessionRequest, 1)
	shell := NewShellWithRuntimes(
		DefaultShellState(Options{}),
		nil,
		nil,
		nil,
		nil,
		func(_ context.Context, req CancelConsoleSessionRequest) error {
			queued <- req
			return nil
		},
		0,
		defaultComposerAskEnqueueTimeout,
		50*time.Millisecond,
	)

	shell.handleConsoleStreamUpdate(ConsoleStreamUpdate{
		Type: ConsoleStreamUpdateEvent,
		Event: ConsoleStreamEvent{
			Type: "event",
			Payload: &ConsoleEventPayload{
				Type:          "event",
				CorrelationID: "corr-stream-4",
				Content:       "working",
			},
		},
	})
	shell.handleConsoleStreamUpdate(ConsoleStreamUpdate{
		Type: ConsoleStreamUpdateEvent,
		Event: ConsoleStreamEvent{
			Type: "reply",
			Payload: &ConsoleEventPayload{
				Type:          "reply",
				CorrelationID: "different-corr",
				Content:       "done",
			},
		},
	})
	shell.submitCancel()

	select {
	case req := <-queued:
		if req.CorrelationID != "corr-stream-4" {
			t.Fatalf("queued correlation_id = %q, want %q", req.CorrelationID, "corr-stream-4")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for queued cancel request")
	}
}
