package tui

import (
	"reflect"
	"testing"
)

func TestShellStateApplyConsoleStreamEvent(t *testing.T) {
	t.Parallel()

	initial := ShellState{
		Transcript: []TranscriptEntry{
			{Speaker: "system", Kind: "seed", Text: "existing"},
		},
	}

	tests := []struct {
		name       string
		event      ConsoleStreamEvent
		wantAppend bool
		wantEntry  TranscriptEntry
	}{
		{
			name: "maps ask payload to user-like transcript row",
			event: ConsoleStreamEvent{
				Type: "ask",
				Payload: &ConsoleEventPayload{
					Type:    "ask",
					Content: "ship this",
				},
			},
			wantAppend: true,
			wantEntry: TranscriptEntry{
				Speaker: "you",
				Kind:    "ask",
				Text:    "ship this",
			},
		},
		{
			name: "maps reply payload to assistant transcript row",
			event: ConsoleStreamEvent{
				Type: "reply",
				Payload: &ConsoleEventPayload{
					Type:    "reply",
					Content: "done",
				},
			},
			wantAppend: true,
			wantEntry: TranscriptEntry{
				Speaker: "assistant",
				Kind:    "reply",
				Text:    "done",
			},
		},
		{
			name: "ignores unmappable heartbeat",
			event: ConsoleStreamEvent{
				Type: "heartbeat",
			},
			wantAppend: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := initial.ApplyConsoleStreamEvent(tc.event, 0)

			if !reflect.DeepEqual(initial.Transcript, []TranscriptEntry{{Speaker: "system", Kind: "seed", Text: "existing"}}) {
				t.Fatalf("initial transcript mutated: %#v", initial.Transcript)
			}
			if got.Transcript[0] != initial.Transcript[0] {
				t.Fatalf("got.Transcript[0] = %#v, want %#v", got.Transcript[0], initial.Transcript[0])
			}

			if tc.wantAppend {
				if len(got.Transcript) != len(initial.Transcript)+1 {
					t.Fatalf("len(got.Transcript) = %d, want %d", len(got.Transcript), len(initial.Transcript)+1)
				}
				if got.Transcript[len(got.Transcript)-1] != tc.wantEntry {
					t.Fatalf("last entry = %#v, want %#v", got.Transcript[len(got.Transcript)-1], tc.wantEntry)
				}
				return
			}

			if !reflect.DeepEqual(got.Transcript, initial.Transcript) {
				t.Fatalf("got.Transcript = %#v, want %#v", got.Transcript, initial.Transcript)
			}
		})
	}
}

func TestShellStateApplyConsoleStreamEventsPreservesBatchOrderAndExisting(t *testing.T) {
	t.Parallel()

	initial := ShellState{
		Transcript: []TranscriptEntry{
			{Speaker: "system", Kind: "seed", Text: "before"},
		},
	}

	events := []ConsoleStreamEvent{
		{
			Type: "ask",
			Payload: &ConsoleEventPayload{
				Type:    "ask",
				Content: "first",
			},
		},
		{Type: "heartbeat"},
		{
			Type: "reply",
			Payload: &ConsoleEventPayload{
				Type:    "reply",
				Content: "second",
			},
		},
	}

	got := initial.ApplyConsoleStreamEvents(events, 0)
	want := []TranscriptEntry{
		{Speaker: "system", Kind: "seed", Text: "before"},
		{Speaker: "you", Kind: "ask", Text: "first"},
		{Speaker: "assistant", Kind: "reply", Text: "second"},
	}

	if !reflect.DeepEqual(got.Transcript, want) {
		t.Fatalf("got.Transcript = %#v, want %#v", got.Transcript, want)
	}
}

func TestShellStateApplyConsoleStreamEventsUnmappableEventsDoNotCapExistingTranscript(t *testing.T) {
	t.Parallel()

	initial := ShellState{
		Transcript: []TranscriptEntry{
			{Speaker: "system", Kind: "seed", Text: "one"},
			{Speaker: "system", Kind: "seed", Text: "two"},
		},
	}

	got := initial.ApplyConsoleStreamEvents([]ConsoleStreamEvent{{Type: "heartbeat"}}, 1)
	if !reflect.DeepEqual(got.Transcript, initial.Transcript) {
		t.Fatalf("got.Transcript = %#v, want unchanged %#v", got.Transcript, initial.Transcript)
	}
}

func TestShellStateApplyConsoleStreamEventsTranscriptLimit(t *testing.T) {
	t.Parallel()

	initial := ShellState{
		Transcript: []TranscriptEntry{
			{Speaker: "system", Kind: "seed", Text: "one"},
			{Speaker: "system", Kind: "seed", Text: "two"},
		},
	}

	events := []ConsoleStreamEvent{
		{
			Type: "ask",
			Payload: &ConsoleEventPayload{
				Type:    "ask",
				Content: "three",
			},
		},
		{
			Type: "reply",
			Payload: &ConsoleEventPayload{
				Type:    "reply",
				Content: "four",
			},
		},
		{
			Type: "event",
			Payload: &ConsoleEventPayload{
				Type:    "event",
				Content: "five",
			},
		},
	}

	tests := []struct {
		name      string
		limit     int
		wantTexts []string
	}{
		{
			name:      "zero limit means no cap",
			limit:     0,
			wantTexts: []string{"one", "two", "three", "four", "five"},
		},
		{
			name:      "negative limit means no cap",
			limit:     -3,
			wantTexts: []string{"one", "two", "three", "four", "five"},
		},
		{
			name:      "keeps most recent two entries in order",
			limit:     2,
			wantTexts: []string{"four", "five"},
		},
		{
			name:      "keeps most recent three entries in order",
			limit:     3,
			wantTexts: []string{"three", "four", "five"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := initial.ApplyConsoleStreamEvents(events, tc.limit)
			if gotTexts := transcriptTexts(got.Transcript); !reflect.DeepEqual(gotTexts, tc.wantTexts) {
				t.Fatalf("transcript texts = %#v, want %#v", gotTexts, tc.wantTexts)
			}
		})
	}
}

func TestShellStateApplyConsoleStreamEventsSuppressesAskEchoWithSameCorrelation(t *testing.T) {
	t.Parallel()

	initial := ShellState{
		Transcript: []TranscriptEntry{
			{Speaker: "you", Kind: "pending", Text: "ship this", CorrelationID: "corr-1"},
		},
	}

	events := []ConsoleStreamEvent{
		{
			Type: "ask",
			Payload: &ConsoleEventPayload{
				Type:          "ask",
				CorrelationID: "corr-1",
				Content:       "ship this",
			},
		},
	}

	got := initial.ApplyConsoleStreamEvents(events, 0)
	if !reflect.DeepEqual(got.Transcript, initial.Transcript) {
		t.Fatalf("got.Transcript = %#v, want %#v", got.Transcript, initial.Transcript)
	}
}

func TestShellStateApplyConsoleStreamEventsReplyWithSameCorrelationStillAppends(t *testing.T) {
	t.Parallel()

	initial := ShellState{
		Transcript: []TranscriptEntry{
			{Speaker: "you", Kind: "pending", Text: "ship this", CorrelationID: "corr-1"},
		},
	}

	events := []ConsoleStreamEvent{
		{
			Type: "reply",
			Payload: &ConsoleEventPayload{
				Type:          "reply",
				CorrelationID: "corr-1",
				Content:       "done",
			},
		},
	}

	got := initial.ApplyConsoleStreamEvents(events, 0)
	if len(got.Transcript) != 2 {
		t.Fatalf("len(got.Transcript) = %d, want 2", len(got.Transcript))
	}
	last := got.Transcript[1]
	if last.Kind != "reply" || last.CorrelationID != "corr-1" {
		t.Fatalf("last = %#v, want appended reply with same correlation", last)
	}
}

func TestShellStateApplyConsoleStreamEventsNoCorrelationAskStillAppends(t *testing.T) {
	t.Parallel()

	initial := ShellState{
		Transcript: []TranscriptEntry{
			{Speaker: "you", Kind: "pending", Text: "ship this", CorrelationID: "corr-1"},
		},
	}

	events := []ConsoleStreamEvent{
		{
			Type: "ask",
			Payload: &ConsoleEventPayload{
				Type:    "ask",
				Content: "ship this",
			},
		},
	}

	got := initial.ApplyConsoleStreamEvents(events, 0)
	if len(got.Transcript) != 2 {
		t.Fatalf("len(got.Transcript) = %d, want 2", len(got.Transcript))
	}
	last := got.Transcript[1]
	if last.Kind != "ask" || last.CorrelationID != "" {
		t.Fatalf("last = %#v, want appended ask with empty correlation", last)
	}
}

func TestShellStateApplyConsoleStreamEventsStillCapsAfterAppend(t *testing.T) {
	t.Parallel()

	initial := ShellState{
		Transcript: []TranscriptEntry{
			{Speaker: "system", Kind: "seed", Text: "one"},
			{Speaker: "system", Kind: "seed", Text: "two"},
		},
	}

	events := []ConsoleStreamEvent{
		{
			Type: "reply",
			Payload: &ConsoleEventPayload{
				Type:          "reply",
				CorrelationID: "corr-cap",
				Content:       "three",
			},
		},
	}

	got := initial.ApplyConsoleStreamEvents(events, 2)
	want := []TranscriptEntry{
		{Speaker: "system", Kind: "seed", Text: "two"},
		{Speaker: "assistant", Kind: "reply", Text: "[corr-cap] three", CorrelationID: "corr-cap"},
	}
	if !reflect.DeepEqual(got.Transcript, want) {
		t.Fatalf("got.Transcript = %#v, want %#v", got.Transcript, want)
	}
}

func transcriptTexts(entries []TranscriptEntry) []string {
	texts := make([]string, 0, len(entries))
	for _, entry := range entries {
		texts = append(texts, entry.Text)
	}
	return texts
}
