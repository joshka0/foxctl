package tui

import "testing"

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
