package transcriptpipeline

import (
	"context"
	"testing"

	"github.com/jkatigb/agentctl/internal/context/companion"
	"github.com/jkatigb/agentctl/internal/v2/adapters/sourceimport"
)

func TestBuildFrameSynopses_DeterministicRollingWindow(t *testing.T) {
	derivations := []companion.AnchoredMemoryDerivation{
		{
			FrameIndex:         0,
			InteractionSummary: "user asked for a pipeline and the assistant proposed one",
			Resolution:         companion.InteractionResolutionResolved,
			Candidates: []companion.AnchoredMemoryCandidate{{
				Type:       companion.EntryTypeDecision,
				Scope:      companion.CandidateScopeDurable,
				Text:       "Use a staged transcript-memory pipeline.",
				Confidence: 0.9,
			}},
		},
		{
			FrameIndex:         1,
			InteractionSummary: "user rejected brittle text matching and pushed for classification",
			Resolution:         companion.InteractionResolutionCorrected,
			Candidates: []companion.AnchoredMemoryCandidate{{
				Type:       companion.EntryTypePreference,
				Scope:      companion.CandidateScopeDurable,
				Text:       "Avoid brittle text-specific coding when a classifier stage can do the job.",
				Confidence: 0.92,
			}},
		},
		{
			FrameIndex:         2,
			InteractionSummary: "user asked for a bounded rolling summary window",
			Resolution:         companion.InteractionResolutionResolved,
			Candidates: []companion.AnchoredMemoryCandidate{{
				Type:       companion.EntryTypeTechnicalContext,
				Scope:      companion.CandidateScopeDurable,
				Text:       "Carry the last few one-line summaries plus a running session synopsis.",
				Confidence: 0.87,
			}},
		},
	}

	got, err := BuildFrameSynopses(context.Background(), nil, LocalModelRuntime{
		Mode:               "deterministic",
		SynopsisWindowSize: 2,
	}, sourceimport.ParsedSession{
		Provider:  sourceimport.ProviderCodex,
		SessionID: "sess-1",
	}, SessionObjective{
		Objective: "Build a staged transcript-memory pipeline.",
		Status:    "active",
	}, derivations)
	if err != nil {
		t.Fatalf("BuildFrameSynopses() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("synopses=%d want 3", len(got))
	}
	if len(got[0].RecentWindow) != 0 {
		t.Fatalf("frame0 recent_window=%v want empty", got[0].RecentWindow)
	}
	if got[1].SessionSynopsis == "" {
		t.Fatal("frame1 session synopsis should be carried forward")
	}
	if len(got[2].RecentWindow) != 2 {
		t.Fatalf("frame2 recent_window=%v want 2 prior lines", got[2].RecentWindow)
	}
	if got[2].UpdatedSessionSynopsis == "" {
		t.Fatal("frame2 updated session synopsis should not be empty")
	}
}
