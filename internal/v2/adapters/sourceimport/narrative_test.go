package sourceimport

import (
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/v2/adapters/turso/turns"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

func TestBuildNarrative_DeterministicAndEvidenceCited(t *testing.T) {
	t.Parallel()

	parsed := ParsedSession{
		Provider:   ProviderClaude,
		SessionID:  "session-narrative-1",
		SourcePath: "/tmp/source.jsonl",
		Turns: []run.TurnRecord{
			{
				ID:        "turn-n-1",
				SessionID: "session-narrative-1",
				TurnIndex: 1,
				Prompt:    "First prompt",
				FinalOutput: run.MessageRef{
					ID:   "msg-n-1",
					Role: "assistant",
					Text: "We should enforce narrative citations.",
				},
				CreatedAt: time.Date(2026, time.February, 22, 10, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, time.February, 22, 10, 0, 0, 0, time.UTC),
			},
			{
				ID:        "turn-n-2",
				SessionID: "session-narrative-1",
				TurnIndex: 2,
				Prompt:    "Second prompt",
				FinalOutput: run.MessageRef{
					ID:   "msg-n-2",
					Role: "assistant",
					Text: "Context builder now includes episodes and narrative.",
				},
				CreatedAt: time.Date(2026, time.February, 22, 10, 5, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, time.February, 22, 10, 5, 0, 0, time.UTC),
			},
		},
	}
	artifacts := []turns.Artifact{
		{
			TurnID:          "turn-n-1",
			ArtifactType:    turns.ArtifactTypeAnnotation,
			ArtifactVersion: "v1",
			Ref:             turns.BuildArtifactRef("turn-n-1", turns.ArtifactTypeAnnotation, "v1"),
		},
		{
			TurnID:          "turn-n-2",
			ArtifactType:    turns.ArtifactTypeLearning,
			ArtifactVersion: "v1",
			Ref:             turns.BuildArtifactRef("turn-n-2", turns.ArtifactTypeLearning, "v1"),
		},
	}

	opts := NarrativeBuildOptions{
		ArtifactVersion: "v1",
		MaxClaims:       4,
		Now: func() time.Time {
			return time.Date(2026, time.February, 22, 11, 0, 0, 0, time.UTC)
		},
	}
	first := BuildNarrative(parsed, artifacts, opts)
	second := BuildNarrative(parsed, artifacts, opts)
	if !first.HasResult || !second.HasResult {
		t.Fatalf("expected narrative result, got first=%+v second=%+v", first, second)
	}
	if first.Narrative.Summary != second.Narrative.Summary {
		t.Fatalf("summary not deterministic: %q vs %q", first.Narrative.Summary, second.Narrative.Summary)
	}
	if len(first.Narrative.Claims) != 2 {
		t.Fatalf("claims len=%d want 2", len(first.Narrative.Claims))
	}
	for i, claim := range first.Narrative.Claims {
		if claim.Text == "" {
			t.Fatalf("claim[%d] text should be set", i)
		}
		if len(claim.AnchorRefs) == 0 {
			t.Fatalf("claim[%d] should include anchor refs", i)
		}
	}
	if first.Narrative.SourceTurnID != "turn-n-2" {
		t.Fatalf("source_turn_id=%q want turn-n-2", first.Narrative.SourceTurnID)
	}
	if first.Narrative.SourceTurnIndex != 2 {
		t.Fatalf("source_turn_index=%d want 2", first.Narrative.SourceTurnIndex)
	}
	if first.Narrative.SourceTurnCount != 2 {
		t.Fatalf("source_turn_count=%d want 2", first.Narrative.SourceTurnCount)
	}
}

func TestBuildNarrative_EmptyInputReturnsNoResult(t *testing.T) {
	t.Parallel()

	got := BuildNarrative(ParsedSession{}, nil, NarrativeBuildOptions{})
	if got.HasResult {
		t.Fatalf("HasResult=%v want false", got.HasResult)
	}
	if len(got.Narrative.Claims) != 0 {
		t.Fatalf("claims len=%d want 0", len(got.Narrative.Claims))
	}
}
