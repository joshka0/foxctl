package transcriptpipeline

import (
	"context"
	"testing"

	"github.com/joshka0/foxctl/internal/v2/adapters/sourceimport"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

func TestBuildAnchoredDerivations_BuildsFramesFromParsedSession(t *testing.T) {
	ctx := context.Background()
	parsed := sourceimport.ParsedSession{
		Provider:  sourceimport.ProviderCodex,
		SessionID: "sess-1",
		Turns: []run.TurnRecord{{
			ID:     "turn-1",
			Prompt: "We should cache reference blobs by normalized hash.",
			FinalOutput: run.MessageRef{
				Text: "Use a cached reference_blob digest keyed by normalized content.",
			},
		}},
	}

	got, err := BuildAnchoredDerivations(ctx, parsed, 0)
	if err != nil {
		t.Fatalf("BuildAnchoredDerivations() error = %v", err)
	}
	if got.ConversationID != "source:codex:sess-1" {
		t.Fatalf("conversation_id=%q want %q", got.ConversationID, "source:codex:sess-1")
	}
	if len(got.Frames) != 1 {
		t.Fatalf("frames=%d want 1", len(got.Frames))
	}
	if len(got.Derivations) != 1 {
		t.Fatalf("derivations=%d want 1", len(got.Derivations))
	}
	if got.Frames[0].UserEvent.Content != parsed.Turns[0].Prompt {
		t.Fatalf("user_text=%q want %q", got.Frames[0].UserEvent.Content, parsed.Turns[0].Prompt)
	}
	if got.Frames[0].AssistantEvent.Content != parsed.Turns[0].FinalOutput.Text {
		t.Fatalf("assistant_text=%q want %q", got.Frames[0].AssistantEvent.Content, parsed.Turns[0].FinalOutput.Text)
	}
}
