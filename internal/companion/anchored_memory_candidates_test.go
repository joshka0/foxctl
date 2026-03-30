package companion

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestDeriveMemoryCandidatesFromFrames_UsesToolReceipts(t *testing.T) {
	frames := []AnchoredInteractionFrame{{
		ConversationID: "conv-tool",
		UserEvent: ConversationEvent{
			ID:        1,
			EventType: EventTypeUserMessage,
			Content:   "Please test the memory pipeline.",
		},
		AssistantEvent: ConversationEvent{
			ID:        2,
			EventType: EventTypeAssistantMessage,
			Content:   "I ran the checks.",
		},
		ToolReceipts: []string{
			"tool_result: exec_command: tests failed with assertion on candidate memory",
		},
		Resolution: InteractionResolutionUnresolved,
		Reaction:   FollowUpReaction{Outcome: ReactionOutcomeUnresolved},
	}}

	got := DeriveMemoryCandidatesFromFrames(frames)
	if len(got) != 1 {
		t.Fatalf("derivations=%d want 1", len(got))
	}
	found := false
	for _, candidate := range got[0].Candidates {
		if candidate.Type == "follow_up_needed" && candidate.Source == "tool_receipt" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected follow_up_needed candidate derived from tool receipt, got %#v", got[0].Candidates)
	}
}

func TestBuildAnchoredInteractionFrames_IncludesToolReceiptsBetweenUserAndAssistant(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	mem, err := NewConversationMemory(db)
	if err != nil {
		t.Fatalf("new conversation memory: %v", err)
	}

	base := time.Date(2026, 3, 25, 15, 0, 0, 0, time.UTC)
	if err := mem.AppendTurn(ctx, ConversationTurn{
		ConversationID: "conv-tool-frame",
		Role:           "user",
		Content:        "Can you validate the migration path?",
		CreatedAt:      base,
	}); err != nil {
		t.Fatalf("append user turn: %v", err)
	}
	if err := mem.InsertEvent(ctx, &ConversationEvent{
		ConversationID: "conv-tool-frame",
		EventType:      EventTypeToolResult,
		ToolName:       "exec_command",
		ToolRunID:      "tool-1",
		PayloadJSON:    `{"summary":"migration checks passed"}`,
	}); err != nil {
		t.Fatalf("insert tool result: %v", err)
	}
	if err := mem.AppendTurn(ctx, ConversationTurn{
		ConversationID: "conv-tool-frame",
		Role:           "assistant",
		Content:        "The migration path looks clean.",
		CreatedAt:      base.Add(time.Second),
	}); err != nil {
		t.Fatalf("append assistant turn: %v", err)
	}

	frames, err := mem.BuildAnchoredInteractionFrames(ctx, "conv-tool-frame", 0)
	if err != nil {
		t.Fatalf("build frames: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("frames=%d want 1", len(frames))
	}
	if len(frames[0].ToolReceipts) == 0 {
		t.Fatal("expected tool receipts in frame")
	}
}

func TestDeriveMemoryCandidatesFromFrames_AddsAcceptedAssistantGuidance(t *testing.T) {
	frames := []AnchoredInteractionFrame{{
		ConversationID: "conv-guidance",
		UserEvent: ConversationEvent{
			ID:        1,
			EventType: EventTypeUserMessage,
			Content:   "How should we cache transcript reference blobs?",
		},
		AssistantEvent: ConversationEvent{
			ID:        2,
			EventType: EventTypeAssistantMessage,
			Content:   "Use a content-hash keyed cache for prederived transcript artifacts and only summarize each blob once.",
		},
		FollowUpUser: &ConversationEvent{
			ID:        3,
			EventType: EventTypeUserMessage,
			Content:   "lets try it",
		},
		Resolution: InteractionResolutionResolved,
		Reaction:   FollowUpReaction{Outcome: ReactionOutcomeAccepted},
	}}

	got := DeriveMemoryCandidatesFromFrames(frames)
	if len(got) != 1 {
		t.Fatalf("derivations=%d want 1", len(got))
	}
	found := false
	for _, candidate := range got[0].Candidates {
		if candidate.Type == EntryTypeTechnicalContext && candidate.Scope == CandidateScopeDurable {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected durable technical_context candidate, got %#v", got[0].Candidates)
	}
}

func TestDeriveMemoryCandidatesFromFrames_AddsTentativeAssistantGuidanceForUnresolved(t *testing.T) {
	frames := []AnchoredInteractionFrame{{
		ConversationID: "conv-guidance-unresolved",
		UserEvent: ConversationEvent{
			ID:        1,
			EventType: EventTypeUserMessage,
			Content:   "Map the rebuilt frontend architecture to the target surfaces.",
		},
		AssistantEvent: ConversationEvent{
			ID:        2,
			EventType: EventTypeAssistantMessage,
			Content:   "Pulse Feed should hang off the main feed route, while Reader Workspace should center on the reader screen and its verse-selection flows.",
		},
		Resolution: InteractionResolutionUnresolved,
		Reaction:   FollowUpReaction{Outcome: ReactionOutcomeUnresolved},
	}}

	got := DeriveMemoryCandidatesFromFrames(frames)
	if len(got) != 1 {
		t.Fatalf("derivations=%d want 1", len(got))
	}
	found := false
	for _, candidate := range got[0].Candidates {
		if candidate.Type == EntryTypeTechnicalContext && candidate.Source == "assistant_guidance" && candidate.Scope == CandidateScopeProvisional {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected provisional assistant guidance candidate, got %#v", got[0].Candidates)
	}
}

func TestDeriveMemoryCandidatesFromFrames_IgnoresToolOutputWhenDerivingTentativeGuidance(t *testing.T) {
	frames := []AnchoredInteractionFrame{{
		ConversationID: "conv-guidance-tool-noise",
		UserEvent: ConversationEvent{
			ID:        1,
			EventType: EventTypeUserMessage,
			Content:   "What should we do next?",
		},
		AssistantEvent: ConversationEvent{
			ID:        2,
			EventType: EventTypeAssistantMessage,
			Content:   "Command: /bin/zsh -lc \"mix test\"\n\nChunk ID: 123abc\nWall time: 0.0523 seconds\nProcess exited with code 0\nOriginal token count: 42",
		},
		Resolution: InteractionResolutionUnresolved,
		Reaction:   FollowUpReaction{Outcome: ReactionOutcomeUnresolved},
	}}

	got := DeriveMemoryCandidatesFromFrames(frames)
	if len(got) != 1 {
		t.Fatalf("derivations=%d want 1", len(got))
	}
	for _, candidate := range got[0].Candidates {
		if candidate.Source == "assistant_guidance" {
			t.Fatalf("unexpected assistant guidance candidate from tool output noise: %#v", got[0].Candidates)
		}
	}
}

func TestDeriveMemoryCandidatesFromFrames_NormalizesTentativeAssistantGuidance(t *testing.T) {
	frames := []AnchoredInteractionFrame{{
		ConversationID: "conv-guidance-normalized",
		UserEvent: ConversationEvent{
			ID:        1,
			EventType: EventTypeUserMessage,
			Content:   "What should we carry forward?",
		},
		AssistantEvent: ConversationEvent{
			ID:        2,
			EventType: EventTypeAssistantMessage,
			Content:   "Agreed. I kept going on the hook work and made the non-presence hooks real instead of client-only scaffolds.",
		},
		Resolution: InteractionResolutionUnresolved,
		Reaction:   FollowUpReaction{Outcome: ReactionOutcomeUnresolved},
	}}

	got := DeriveMemoryCandidatesFromFrames(frames)
	if len(got) != 1 {
		t.Fatalf("derivations=%d want 1", len(got))
	}
	for _, candidate := range got[0].Candidates {
		if candidate.Source != "assistant_guidance" {
			continue
		}
		if strings.Contains(candidate.Text, "Agreed.") || strings.Contains(candidate.Text, "I kept going") {
			t.Fatalf("candidate text kept progress lead: %q", candidate.Text)
		}
		if !strings.Contains(candidate.Text, "non-presence hooks are real instead of client-only scaffolds.") {
			t.Fatalf("candidate text=%q missing normalized takeaway", candidate.Text)
		}
		return
	}
	t.Fatalf("expected assistant guidance candidate in %#v", got[0].Candidates)
}

func TestDeriveMemoryCandidatesFromFrames_IgnoresCommitStatusGuidance(t *testing.T) {
	frames := []AnchoredInteractionFrame{{
		ConversationID: "conv-guidance-commit-status",
		UserEvent: ConversationEvent{
			ID:        1,
			EventType: EventTypeUserMessage,
			Content:   "What changed?",
		},
		AssistantEvent: ConversationEvent{
			ID:        2,
			EventType: EventTypeAssistantMessage,
			Content:   "Committed as `9c397f87` with `feat` after the frontend/auth batch.",
		},
		Resolution: InteractionResolutionUnresolved,
		Reaction:   FollowUpReaction{Outcome: ReactionOutcomeUnresolved},
	}}

	got := DeriveMemoryCandidatesFromFrames(frames)
	if len(got) != 1 {
		t.Fatalf("derivations=%d want 1", len(got))
	}
	for _, candidate := range got[0].Candidates {
		if candidate.Source == "assistant_guidance" {
			t.Fatalf("unexpected assistant guidance candidate from commit status: %#v", got[0].Candidates)
		}
	}
}
