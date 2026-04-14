package taskhistory

import (
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/context/contextplane"
)

func TestRenderHookContextWithArtifact_IncludesTranscriptHistory(t *testing.T) {
	t.Parallel()

	rendered := RenderHookContextWithArtifact(Pack{
		Task: contextplane.TaskCandidate{
			ID:     "T-1",
			Title:  "Use transcript continuity",
			Status: "in_progress",
		},
		Transcript: &TranscriptHistory{
			Overview:            "Objective: Use transcript continuity. Continue with: inject history pack.",
			ObjectiveLabel:      "use transcript continuity",
			AgentBrief:          "Objective: Use transcript continuity\nContinue with: inject history pack",
			ContinueWith:        []string{"inject history pack"},
			WatchOutFor:         []string{"Do not overfit to doctrine-only prompts."},
			Regressions:         []string{"Sandbox denied while checking git state."},
			RecurringMistakes:   []string{"We initially pushed doctrine too hard."},
			RecentLearnings:     []string{"Use history_answer as the continuity backbone."},
			RecentSurprises:     []string{"Consensus-backed grouped findings transferred well."},
			RetrievedBrief:      "Continue with: inject history pack\nLearned: Use history_answer as the continuity backbone.",
			RetrievedHighlights: []string{"Consensus-backed grouped findings transferred well."},
		},
	}, "sha256:artifact")

	if !strings.Contains(rendered, "**Transcript history:**") {
		t.Fatalf("rendered=%q missing transcript section", rendered)
	}
	if !strings.Contains(rendered, "Continue with: inject history pack") {
		t.Fatalf("rendered=%q missing transcript brief", rendered)
	}
	if !strings.Contains(rendered, "**Transcript watch-outs:**") {
		t.Fatalf("rendered=%q missing transcript watch-outs", rendered)
	}
	if !strings.Contains(rendered, "**Transcript regressions:**") {
		t.Fatalf("rendered=%q missing transcript regressions", rendered)
	}
	if !strings.Contains(rendered, "**Transcript next work:**") {
		t.Fatalf("rendered=%q missing transcript next work", rendered)
	}
	if !strings.Contains(rendered, "**Transcript highlights:**") {
		t.Fatalf("rendered=%q missing transcript highlights", rendered)
	}
}

func TestRenderJidoStateWithArtifact_IncludesTranscriptHistory(t *testing.T) {
	t.Parallel()

	state := RenderJidoStateWithArtifact(Pack{
		Task: contextplane.TaskCandidate{
			ID:     "T-1",
			Title:  "Use transcript continuity",
			Status: "in_progress",
		},
		Transcript: &TranscriptHistory{
			Overview:            "Objective: Use transcript continuity.",
			ObjectiveLabel:      "use transcript continuity",
			AgentBrief:          "Objective: Use transcript continuity\nNext: inject history pack",
			ContinueWith:        []string{"inject history pack"},
			WatchOutFor:         []string{"Do not overfit to doctrine-only prompts."},
			Regressions:         []string{"Sandbox denied while checking git state."},
			RecurringMistakes:   []string{"We initially pushed doctrine too hard."},
			RecentLearnings:     []string{"Use history_answer as the continuity backbone."},
			RecentSurprises:     []string{"Consensus-backed grouped findings transferred well."},
			RetrievedBrief:      "Continue with: inject history pack\nLearned: Use history_answer as the continuity backbone.",
			RetrievedHighlights: []string{"Consensus-backed grouped findings transferred well."},
			EvidenceRefs:        []string{"frames:1-2"},
			SourceNames:         []string{"transcript-history:sess-1:answer:abc"},
		},
	}, "sha256:artifact")

	if got := state["transcript_history_overview"]; got != "Objective: Use transcript continuity." {
		t.Fatalf("transcript_history_overview=%v", got)
	}
	if got := state["transcript_history_objective_label"]; got != "use transcript continuity" {
		t.Fatalf("transcript_history_objective_label=%v", got)
	}
	if got := state["transcript_history_agent_brief"]; got == nil {
		t.Fatalf("missing transcript_history_agent_brief in %v", state)
	}
	if got := state["transcript_history_continue_with"]; got == nil {
		t.Fatalf("missing transcript_history_continue_with in %v", state)
	}
	if got := state["transcript_history_watch_out_for"]; got == nil {
		t.Fatalf("missing transcript_history_watch_out_for in %v", state)
	}
	if got := state["transcript_history_regressions"]; got == nil {
		t.Fatalf("missing transcript_history_regressions in %v", state)
	}
	if got := state["transcript_history_recurring_mistakes"]; got == nil {
		t.Fatalf("missing transcript_history_recurring_mistakes in %v", state)
	}
	if got := state["transcript_history_recent_learnings"]; got == nil {
		t.Fatalf("missing transcript_history_recent_learnings in %v", state)
	}
	if got := state["transcript_history_recent_surprises"]; got == nil {
		t.Fatalf("missing transcript_history_recent_surprises in %v", state)
	}
	if got := state["transcript_history_retrieved_brief"]; got == nil {
		t.Fatalf("missing transcript_history_retrieved_brief in %v", state)
	}
	if got := state["transcript_history_retrieved_highlights"]; got == nil {
		t.Fatalf("missing transcript_history_retrieved_highlights in %v", state)
	}
	if got := state["transcript_history_evidence_refs"]; got == nil {
		t.Fatalf("missing transcript_history_evidence_refs in %v", state)
	}
	if got := state["transcript_history_sources"]; got == nil {
		t.Fatalf("missing transcript_history_sources in %v", state)
	}
}

func TestRenderTranscriptFamilyOverview(t *testing.T) {
	t.Parallel()

	rendered := RenderTranscriptFamilyOverview(TranscriptFamilyOverview{
		SummaryMode:        "llm",
		SummaryModel:       "google/gemini-3.1-flash-lite-preview",
		DateFrom:           "2026-03-20",
		DateTo:             "2026-03-29",
		Overview:           "Focus: complete backend tasks | Learning: non-presence hooks are real instead of client-only scaffolds.",
		CurrentFocus:       []string{"complete backend tasks"},
		RecentChanges:      []string{"Canonical integration now supports split socket targets"},
		TopLearnings:       []string{"non-presence hooks are real instead of client-only scaffolds."},
		RecurringLearnings: []string{"Non-presence hooks function as real logic rather than client-side scaffolds."},
		TopRisks:           []string{"anonymous guest enforcement"},
		TopSurprises:       []string{"split socket targets"},
		NextWork:           []string{"finalize integration checks"},
		RecurringMistakes:  []string{"exec_command sandbox denied"},
		SupportMetadata: []TranscriptFamilySupportMetadata{
			{Category: "top_learnings", Text: "non-presence hooks are real instead of client-only scaffolds.", OwnerCount: 2, LatestUpdatedAt: "2026-03-29T12:00:00Z", LatestAgeDays: 0},
		},
		SourceOwners: []string{"sess-a", "sess-b"},
	}, "sha256:artifact")

	if !strings.Contains(rendered, "## Transcript Family Overview") {
		t.Fatalf("rendered=%q missing heading", rendered)
	}
	if !strings.Contains(rendered, "**Summary mode:** llm (google/gemini-3.1-flash-lite-preview)") {
		t.Fatalf("rendered=%q missing summary mode", rendered)
	}
	if !strings.Contains(rendered, "**Current focus:**") {
		t.Fatalf("rendered=%q missing current focus", rendered)
	}
	if !strings.Contains(rendered, "**Date range:** 2026-03-20 to 2026-03-29") {
		t.Fatalf("rendered=%q missing date range", rendered)
	}
	if !strings.Contains(rendered, "**Recurring learnings:**") {
		t.Fatalf("rendered=%q missing recurring learnings", rendered)
	}
	if !strings.Contains(rendered, "**Support metadata:**") {
		t.Fatalf("rendered=%q missing support metadata", rendered)
	}
	if !strings.Contains(rendered, "**Recurring mistakes:**") {
		t.Fatalf("rendered=%q missing recurring mistakes", rendered)
	}
}
