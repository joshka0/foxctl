package console

import (
	"testing"
	"testing/quick"
)

func TestPayloadCompletionInvariants(t *testing.T) {
	tests := []struct {
		name string
		p    Payload
		want bool
	}{
		{
			name: "reply is always complete",
			p:    NewReplyPayload("actor-1", "console-1", "corr-1", "done"),
			want: true,
		},
		{
			name: "streaming partial event is incomplete",
			p:    NewEventPayload("actor-1", "console-1", "corr-1", "chunk", map[string]any{"partial": true}),
			want: false,
		},
		{
			name: "explicit non-partial event is complete",
			p:    NewEventPayload("actor-1", "console-1", "corr-1", "done", map[string]any{"partial": false}),
			want: true,
		},
		{
			name: "event without completion metadata is incomplete",
			p:    NewEventPayload("actor-1", "console-1", "corr-1", "chunk", nil),
			want: false,
		},
		{
			name: "cancel command is not a completed response",
			p:    NewCancelPayload("actor-1", "console-1", "corr-1"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.IsComplete(); got != tt.want {
				t.Fatalf("IsComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPayloadCompletionGeneratedPartialMetadataInvariant(t *testing.T) {
	prop := func(rawType uint8, partialPresent bool, partial bool) bool {
		p := Payload{Type: generatedPayloadType(rawType)}
		if partialPresent {
			p.Metadata = map[string]any{"partial": partial}
		}

		want := false
		if p.Type == PayloadTypeReply {
			want = true
		} else if partialPresent {
			want = !partial
		}
		return p.IsComplete() == want
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatal(err)
	}
}

func TestPayloadErrorInvariant(t *testing.T) {
	tests := []struct {
		name string
		p    Payload
		want bool
	}{
		{
			name: "nonempty error string",
			p:    Payload{Metadata: map[string]any{"error": "failed"}},
			want: true,
		},
		{
			name: "empty error string",
			p:    Payload{Metadata: map[string]any{"error": ""}},
			want: false,
		},
		{
			name: "non-string error value",
			p:    Payload{Metadata: map[string]any{"error": true}},
			want: false,
		},
		{
			name: "missing metadata",
			p:    Payload{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.HasError(); got != tt.want {
				t.Fatalf("HasError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func generatedPayloadType(raw uint8) PayloadType {
	switch raw % 4 {
	case 0:
		return PayloadTypeAsk
	case 1:
		return PayloadTypeReply
	case 2:
		return PayloadTypeEvent
	default:
		return PayloadTypeCmd
	}
}
