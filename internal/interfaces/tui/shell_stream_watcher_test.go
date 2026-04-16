package tui

import (
	"errors"
	"testing"
	"time"

	tui "github.com/grindlemire/go-tui"
)

func TestShellWatchersWithoutStream(t *testing.T) {
	shell := NewShell(DefaultShellState(Options{}))
	if got := len(shell.Watchers()); got != 0 {
		t.Fatalf("len(shell.Watchers()) = %d, want 0", got)
	}
}

func TestShellWatchersApplyConsoleStreamUpdates(t *testing.T) {
	initial := ShellState{
		Transcript: []TranscriptEntry{
			{Speaker: "seed", Kind: "seed", Text: "seed"},
		},
	}
	updates := make(chan ConsoleStreamUpdate, 8)
	shell := NewShellWithStream(initial, updates, 3)

	watchers := shell.Watchers()
	if got := len(watchers); got != 1 {
		t.Fatalf("len(shell.Watchers()) = %d, want 1", got)
	}

	eventQueue := make(chan func(), 8)
	stopCh := make(chan struct{})
	watchers[0].Start(eventQueue, stopCh)
	defer close(stopCh)

	updates <- ConsoleStreamUpdate{
		Type: ConsoleStreamUpdateEvent,
		Event: ConsoleStreamEvent{
			Type: "ask",
			Payload: &ConsoleEventPayload{
				Type:    "ask",
				Content: "hello",
			},
		},
	}
	updates <- ConsoleStreamUpdate{
		Type: ConsoleStreamUpdateError,
		Err:  errors.New("boom"),
	}
	updates <- ConsoleStreamUpdate{
		Type: ConsoleStreamUpdateDone,
	}

	runWatcherHandler(t, eventQueue)
	runWatcherHandler(t, eventQueue)
	runWatcherHandler(t, eventQueue)

	got := shell.state.Get().Transcript
	if len(got) != 3 {
		t.Fatalf("len(transcript) = %d, want 3", len(got))
	}
	if got[0].Kind != "ask" || got[0].Speaker != "you" || got[0].Text != "hello" {
		t.Fatalf("transcript[0] = %#v, want ask/you/hello", got[0])
	}
	if got[1].Kind != "error" || got[1].Speaker != "system" || got[1].Text != "console stream error: boom" {
		t.Fatalf("transcript[1] = %#v, want deterministic error row", got[1])
	}
	if got[2].Kind != "status" || got[2].Speaker != "system" || got[2].Text != "console stream closed" {
		t.Fatalf("transcript[2] = %#v, want deterministic done row", got[2])
	}
}

func TestShellWatchersUseReducerForEventUpdates(t *testing.T) {
	initial := ShellState{
		Transcript: []TranscriptEntry{
			{Speaker: "seed", Kind: "seed", Text: "seed"},
		},
	}
	updates := make(chan ConsoleStreamUpdate, 2)
	shell := NewShellWithStream(initial, updates, 0)

	watchers := shell.Watchers()
	if len(watchers) != 1 {
		t.Fatalf("len(shell.Watchers()) = %d, want 1", len(watchers))
	}

	eventQueue := make(chan func(), 2)
	stopCh := make(chan struct{})
	watchers[0].Start(eventQueue, stopCh)
	defer close(stopCh)

	updates <- ConsoleStreamUpdate{
		Type:  ConsoleStreamUpdateEvent,
		Event: ConsoleStreamEvent{Type: "heartbeat"},
	}

	runWatcherHandler(t, eventQueue)

	got := shell.state.Get().Transcript
	if len(got) != 1 {
		t.Fatalf("len(transcript) = %d, want 1 (heartbeat should be no-op)", len(got))
	}
	if got[0] != initial.Transcript[0] {
		t.Fatalf("transcript[0] = %#v, want %#v", got[0], initial.Transcript[0])
	}
}

func TestShellWatchersZeroTranscriptLimitIsUncapped(t *testing.T) {
	initial := ShellState{
		Transcript: []TranscriptEntry{
			{Speaker: "seed", Kind: "seed", Text: "seed"},
		},
	}
	updates := make(chan ConsoleStreamUpdate, 4)
	shell := NewShellWithStream(initial, updates, 0)

	watchers := shell.Watchers()
	if len(watchers) != 1 {
		t.Fatalf("len(shell.Watchers()) = %d, want 1", len(watchers))
	}

	eventQueue := make(chan func(), 4)
	stopCh := make(chan struct{})
	watchers[0].Start(eventQueue, stopCh)
	defer close(stopCh)

	updates <- ConsoleStreamUpdate{
		Type: ConsoleStreamUpdateEvent,
		Event: ConsoleStreamEvent{
			Type:    "ask",
			Payload: &ConsoleEventPayload{Type: "ask", Content: "one"},
		},
	}
	updates <- ConsoleStreamUpdate{
		Type: ConsoleStreamUpdateEvent,
		Event: ConsoleStreamEvent{
			Type:    "reply",
			Payload: &ConsoleEventPayload{Type: "reply", Content: "two"},
		},
	}

	runWatcherHandler(t, eventQueue)
	runWatcherHandler(t, eventQueue)

	got := shell.state.Get().Transcript
	if len(got) != 3 {
		t.Fatalf("len(transcript) = %d, want existing row plus two stream rows", len(got))
	}
	if got[0] != initial.Transcript[0] {
		t.Fatalf("transcript[0] = %#v, want preserved seed row %#v", got[0], initial.Transcript[0])
	}
}

func runWatcherHandler(t *testing.T, q <-chan func()) {
	t.Helper()
	select {
	case fn := <-q:
		if fn == nil {
			t.Fatal("watcher handler is nil")
		}
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for watcher handler")
	}
}

var _ tui.WatcherProvider = (*Shell)(nil)
