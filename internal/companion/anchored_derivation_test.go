package companion

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestBuildAnchoredInteractionFrames_ClassifiesAcceptanceAndCorrection(t *testing.T) {
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

	base := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)
	turns := []ConversationTurn{
		{ConversationID: "conv-frames", Role: "user", Content: "Use sqlite for local development.", CreatedAt: base},
		{ConversationID: "conv-frames", Role: "assistant", Content: "I'll configure sqlite locally.", CreatedAt: base.Add(time.Second)},
		{ConversationID: "conv-frames", Role: "user", Content: "Thanks, that works.", CreatedAt: base.Add(2 * time.Second)},
		{ConversationID: "conv-frames", Role: "assistant", Content: "Great, moving on.", CreatedAt: base.Add(3 * time.Second)},
		{ConversationID: "conv-frames", Role: "user", Content: "No, that's wrong. I asked for postgres in staging only.", CreatedAt: base.Add(4 * time.Second)},
	}
	for _, turn := range turns {
		if err := mem.AppendTurn(ctx, turn); err != nil {
			t.Fatalf("append turn %q: %v", turn.Content, err)
		}
	}

	frames, err := mem.BuildAnchoredInteractionFrames(ctx, "conv-frames", 0)
	if err != nil {
		t.Fatalf("build frames: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("frames=%d want 2", len(frames))
	}

	if frames[0].Reaction.Outcome != ReactionOutcomeAccepted {
		t.Fatalf("frame[0] reaction=%q want %q", frames[0].Reaction.Outcome, ReactionOutcomeAccepted)
	}
	if frames[0].Resolution != InteractionResolutionResolved {
		t.Fatalf("frame[0] resolution=%q want %q", frames[0].Resolution, InteractionResolutionResolved)
	}
	if frames[1].Reaction.Outcome != ReactionOutcomeCorrected {
		t.Fatalf("frame[1] reaction=%q want %q", frames[1].Reaction.Outcome, ReactionOutcomeCorrected)
	}
	if frames[1].Resolution != InteractionResolutionCorrected {
		t.Fatalf("frame[1] resolution=%q want %q", frames[1].Resolution, InteractionResolutionCorrected)
	}
	if frames[1].FollowUpUser == nil {
		t.Fatal("frame[1] follow-up user missing")
	}
}

func TestBuildAnchoredInteractionFrames_UsesHistoricalAnchorState(t *testing.T) {
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

	base := time.Date(2026, 3, 25, 13, 0, 0, 0, time.UTC)
	turns := []ConversationTurn{
		{ConversationID: "conv-anchor", Role: "user", Content: "I prefer concise answers.", CreatedAt: base},
		{ConversationID: "conv-anchor", Role: "assistant", Content: "Understood.", CreatedAt: base.Add(time.Second)},
		{ConversationID: "conv-anchor", Role: "user", Content: "Actually I prefer detailed answers.", CreatedAt: base.Add(2 * time.Second)},
		{ConversationID: "conv-anchor", Role: "assistant", Content: "I'll switch to detailed answers.", CreatedAt: base.Add(3 * time.Second)},
		{ConversationID: "conv-anchor", Role: "user", Content: "Thanks.", CreatedAt: base.Add(4 * time.Second)},
	}
	for _, turn := range turns {
		if err := mem.AppendTurn(ctx, turn); err != nil {
			t.Fatalf("append turn %q: %v", turn.Content, err)
		}
	}

	frames, err := mem.BuildAnchoredInteractionFrames(ctx, "conv-anchor", 0)
	if err != nil {
		t.Fatalf("build frames: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("frames=%d want 2", len(frames))
	}

	second := frames[1]
	if len(second.AnchorState.HardState) == 0 {
		t.Fatal("expected anchor hard state for second frame")
	}

	var sawConcise bool
	var sawDetailed bool
	for _, fact := range second.AnchorState.HardState {
		if fact.EntryType != EntryTypePreference {
			continue
		}
		value := strings.ToLower(fact.Value)
		if strings.Contains(value, "concise") {
			sawConcise = true
		}
		if strings.Contains(value, "detailed") {
			sawDetailed = true
		}
	}

	if !sawConcise {
		t.Fatal("expected prior concise preference in anchor snapshot")
	}
	if sawDetailed {
		t.Fatal("did not expect future detailed preference to leak into anchor snapshot")
	}
}

func TestBuildAnchoredInteractionFrames_UnresolvedWithoutFollowUp(t *testing.T) {
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

	base := time.Date(2026, 3, 25, 14, 0, 0, 0, time.UTC)
	if err := mem.AppendTurn(ctx, ConversationTurn{
		ConversationID: "conv-unresolved",
		Role:           "user",
		Content:        "Can you remember that our deploy window is Tuesday 14:00 UTC?",
		CreatedAt:      base,
	}); err != nil {
		t.Fatalf("append user turn: %v", err)
	}
	if err := mem.AppendTurn(ctx, ConversationTurn{
		ConversationID: "conv-unresolved",
		Role:           "assistant",
		Content:        "Yes, I'll remember that.",
		CreatedAt:      base.Add(time.Second),
	}); err != nil {
		t.Fatalf("append assistant turn: %v", err)
	}

	frames, err := mem.BuildAnchoredInteractionFrames(ctx, "conv-unresolved", 0)
	if err != nil {
		t.Fatalf("build frames: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("frames=%d want 1", len(frames))
	}
	if frames[0].FollowUpUser != nil {
		t.Fatal("expected no follow-up user")
	}
	if frames[0].Reaction.Outcome != ReactionOutcomeUnresolved {
		t.Fatalf("reaction=%q want %q", frames[0].Reaction.Outcome, ReactionOutcomeUnresolved)
	}
	if frames[0].Resolution != InteractionResolutionUnresolved {
		t.Fatalf("resolution=%q want %q", frames[0].Resolution, InteractionResolutionUnresolved)
	}
}

func TestBuildAnchoredInteractionFrames_ClassifiesLetsTryItAsAcceptance(t *testing.T) {
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

	base := time.Date(2026, 3, 25, 16, 0, 0, 0, time.UTC)
	turns := []ConversationTurn{
		{ConversationID: "conv-accept", Role: "user", Content: "We should cache reference blobs.", CreatedAt: base},
		{ConversationID: "conv-accept", Role: "assistant", Content: "Use a content-hash keyed cache and reuse summaries.", CreatedAt: base.Add(time.Second)},
		{ConversationID: "conv-accept", Role: "user", Content: "lets try it", CreatedAt: base.Add(2 * time.Second)},
	}
	for _, turn := range turns {
		if err := mem.AppendTurn(ctx, turn); err != nil {
			t.Fatalf("append turn %q: %v", turn.Content, err)
		}
	}

	frames, err := mem.BuildAnchoredInteractionFrames(ctx, "conv-accept", 0)
	if err != nil {
		t.Fatalf("build frames: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("frames=%d want 1", len(frames))
	}
	if frames[0].Reaction.Outcome != ReactionOutcomeAccepted {
		t.Fatalf("reaction=%q want %q", frames[0].Reaction.Outcome, ReactionOutcomeAccepted)
	}
	if frames[0].Resolution != InteractionResolutionResolved {
		t.Fatalf("resolution=%q want %q", frames[0].Resolution, InteractionResolutionResolved)
	}
}
