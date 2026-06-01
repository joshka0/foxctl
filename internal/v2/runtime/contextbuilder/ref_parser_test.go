package contextbuilder

import (
	"errors"
	"fmt"
	"testing"
	"testing/quick"
)

func TestParseRefCanonicalFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want Ref
	}{
		{
			name: "episode",
			raw:  " episode/ep-1 ",
			want: Ref{
				Raw:       "episode/ep-1",
				Kind:      RefEpisode,
				EpisodeID: "ep-1",
			},
		},
		{
			name: "whole turn",
			raw:  "turn/turn-1",
			want: Ref{
				Raw:    "turn/turn-1",
				Kind:   RefWholeTurn,
				TurnID: "turn-1",
			},
		},
		{
			name: "iteration",
			raw:  "turn/turn-1/iter/2",
			want: Ref{
				Raw:            "turn/turn-1/iter/2",
				Kind:           RefIteration,
				TurnID:         "turn-1",
				IterationIndex: 2,
			},
		},
		{
			name: "tool call",
			raw:  "turn/turn-1/iter/2/tool/tool-1",
			want: Ref{
				Raw:            "turn/turn-1/iter/2/tool/tool-1",
				Kind:           RefToolCall,
				TurnID:         "turn-1",
				IterationIndex: 2,
				ToolCallID:     "tool-1",
			},
		},
		{
			name: "message slice",
			raw:  "turn/turn-1#msg:msg-1:4-9",
			want: Ref{
				Raw:       "turn/turn-1#msg:msg-1:4-9",
				Kind:      RefSlice,
				TurnID:    "turn-1",
				MessageID: "msg-1",
				Start:     4,
				End:       9,
			},
		},
		{
			name: "zero-width message slice",
			raw:  "turn/turn-1#msg:msg-1:4-4",
			want: Ref{
				Raw:       "turn/turn-1#msg:msg-1:4-4",
				Kind:      RefSlice,
				TurnID:    "turn-1",
				MessageID: "msg-1",
				Start:     4,
				End:       4,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseRef(tt.raw)
			if err != nil {
				t.Fatalf("ParseRef(%q) error = %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("ParseRef(%q)=%+v want %+v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseRefRejectsMalformedRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantErr error
	}{
		{name: "blank", raw: "   ", wantErr: ErrInvalidRef},
		{name: "missing turn id", raw: "turn/", wantErr: ErrInvalidRef},
		{name: "negative iteration", raw: "turn/turn-1/iter/-1", wantErr: ErrInvalidRef},
		{name: "missing tool call id", raw: "turn/turn-1/iter/2/tool/", wantErr: ErrInvalidRef},
		{name: "slice end before start", raw: "turn/turn-1#msg:msg-1:9-4", wantErr: ErrInvalidSlice},
		{name: "unsupported artifact ref", raw: "turn/turn-1/artifact/summary/v1", wantErr: ErrInvalidRef},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseRef(tt.raw)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ParseRef(%q) error = %v want %v", tt.raw, err, tt.wantErr)
			}
		})
	}
}

func TestParseRefGeneratedValidRefsPreserveFields(t *testing.T) {
	t.Parallel()

	property := func(item refCase) bool {
		raw, want := generatedRef(item)
		got, err := ParseRef(raw)
		if err != nil {
			return false
		}
		return got == want
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 256}); err != nil {
		t.Fatalf("generated valid refs should preserve parsed fields: %v", err)
	}
}

type refCase struct {
	Kind  uint8
	SeedA uint8
	SeedB uint8
	SeedC uint8
	Start uint8
	Width uint8
}

func generatedRef(item refCase) (string, Ref) {
	turnID := generatedRefID("turn", item.SeedA)
	switch item.Kind % 5 {
	case 0:
		episodeID := generatedRefID("ep", item.SeedA)
		raw := "episode/" + episodeID
		return raw, Ref{Raw: raw, Kind: RefEpisode, EpisodeID: episodeID}
	case 1:
		raw := "turn/" + turnID
		return raw, Ref{Raw: raw, Kind: RefWholeTurn, TurnID: turnID}
	case 2:
		iter := int(item.SeedB % 16)
		raw := fmt.Sprintf("turn/%s/iter/%d", turnID, iter)
		return raw, Ref{Raw: raw, Kind: RefIteration, TurnID: turnID, IterationIndex: iter}
	case 3:
		iter := int(item.SeedB % 16)
		toolID := generatedRefID("tool", item.SeedC)
		raw := fmt.Sprintf("turn/%s/iter/%d/tool/%s", turnID, iter, toolID)
		return raw, Ref{Raw: raw, Kind: RefToolCall, TurnID: turnID, IterationIndex: iter, ToolCallID: toolID}
	default:
		start := int(item.Start % 64)
		end := start + int(item.Width%16)
		msgID := generatedRefID("msg", item.SeedB)
		raw := fmt.Sprintf("turn/%s#msg:%s:%d-%d", turnID, msgID, start, end)
		return raw, Ref{Raw: raw, Kind: RefSlice, TurnID: turnID, MessageID: msgID, Start: start, End: end}
	}
}

func generatedRefID(prefix string, seed uint8) string {
	return fmt.Sprintf("%s-%03d", prefix, seed)
}
