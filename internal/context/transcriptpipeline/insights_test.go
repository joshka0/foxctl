package transcriptpipeline

import (
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/companion"
)

func TestBuildDecisionInsights_ExtractsActionableKinds(t *testing.T) {
	t.Parallel()

	derivations := []companion.AnchoredMemoryDerivation{
		{
			FrameIndex: 0,
			Resolution: companion.InteractionResolutionResolved,
			Reaction:   companion.FollowUpReaction{Outcome: companion.ReactionOutcomeAccepted},
			Candidates: []companion.AnchoredMemoryCandidate{
				{
					Type:       companion.EntryTypeDecision,
					Scope:      companion.CandidateScopeDurable,
					Text:       "Use a classifier layer for transcript memory.",
					Confidence: 0.9,
					Source:     "user",
				},
				{
					Type:       companion.EntryTypeTechnicalContext,
					Scope:      companion.CandidateScopeDurable,
					Text:       "Accepted assistant guidance about pipeline layering.",
					Confidence: 0.8,
					Source:     "assistant_guidance",
				},
			},
		},
		{
			FrameIndex: 1,
			Resolution: companion.InteractionResolutionUnresolved,
			Reaction:   companion.FollowUpReaction{Outcome: companion.ReactionOutcomeConfused},
			Candidates: []companion.AnchoredMemoryCandidate{
				{
					Type:       "follow_up_needed",
					Scope:      companion.CandidateScopeSession,
					Text:       "Can we make the inner loop focusable?",
					Confidence: 0.7,
					Source:     "user",
				},
				{
					Type:       "user_pain_point",
					Scope:      companion.CandidateScopeSession,
					Text:       "The implementation bits are still noisy.",
					Confidence: 0.76,
					Source:     "followup_user",
				},
			},
		},
	}

	got := BuildDecisionInsights(derivations, 8)
	if len(got) < 4 {
		t.Fatalf("insights=%d want at least 4", len(got))
	}

	var foundDecision, foundDirection, foundQuestion, foundRisk bool
	for _, item := range got {
		switch item.Kind {
		case InsightKindDecision:
			foundDecision = true
			if item.Status != InsightStatusAccepted {
				t.Fatalf("decision status=%q want %q", item.Status, InsightStatusAccepted)
			}
		case InsightKindDirection:
			foundDirection = true
		case InsightKindQuestion:
			foundQuestion = true
			if item.Status != InsightStatusOpen {
				t.Fatalf("question status=%q want %q", item.Status, InsightStatusOpen)
			}
		case InsightKindRisk:
			foundRisk = true
		}
	}

	if !foundDecision || !foundDirection || !foundQuestion || !foundRisk {
		t.Fatalf("missing expected insight kinds in %#v", got)
	}
}

func TestInsightsFromConsensusClaims_ProducesSupportedDirections(t *testing.T) {
	t.Parallel()

	got := InsightsFromConsensusClaims([]ConsensusClaim{
		{
			Text:                  "Implement auto-memory as a second-pass consolidator over the existing hybrid companion runtime.",
			SupportCount:          3,
			MainlineEvidenceScore: 0.225,
		},
	}, 4)
	if len(got) != 1 {
		t.Fatalf("insights=%d want 1", len(got))
	}
	if got[0].Kind != InsightKindDirection {
		t.Fatalf("kind=%q want %q", got[0].Kind, InsightKindDirection)
	}
	if got[0].Status != InsightStatusSupported {
		t.Fatalf("status=%q want %q", got[0].Status, InsightStatusSupported)
	}
	if got[0].SourceBasis != "sidecar_consensus" {
		t.Fatalf("source_basis=%q want sidecar_consensus", got[0].SourceBasis)
	}
}

func TestBuildDecisionInsights_TreatsFollowUpRequestsAsDirections(t *testing.T) {
	t.Parallel()

	got := BuildDecisionInsights([]companion.AnchoredMemoryDerivation{{
		FrameIndex: 1,
		Resolution: companion.InteractionResolutionUnresolved,
		Reaction:   companion.FollowUpReaction{Outcome: companion.ReactionOutcomeUnresolved},
		Candidates: []companion.AnchoredMemoryCandidate{{
			Type:       "follow_up_needed",
			Scope:      companion.CandidateScopeSession,
			Text:       "Finalize release gate verdict and readiness handoff.\n\n```text\nuse gtr skill\n...`",
			Confidence: 0.68,
			Source:     "user",
		}},
	}}, 4)
	if len(got) != 1 {
		t.Fatalf("insights=%d want 1", len(got))
	}
	if got[0].Kind != InsightKindDirection {
		t.Fatalf("kind=%q want %q", got[0].Kind, InsightKindDirection)
	}
	if got[0].Status != InsightStatusActive {
		t.Fatalf("status=%q want %q", got[0].Status, InsightStatusActive)
	}
}

func TestBuildDecisionInsights_CompactsLongLocationPrefixedDirections(t *testing.T) {
	t.Parallel()

	got := BuildDecisionInsights([]companion.AnchoredMemoryDerivation{{
		FrameIndex: 0,
		Resolution: companion.InteractionResolutionUnresolved,
		Reaction:   companion.FollowUpReaction{Outcome: companion.ReactionOutcomeUnresolved},
		Candidates: []companion.AnchoredMemoryCandidate{{
			Type:       "follow_up_needed",
			Scope:      companion.CandidateScopeSession,
			Text:       "Also this: In `@praze-website/web/index.html` around lines 1330 - 1331, Guard the unguarded.",
			Confidence: 0.68,
			Source:     "user",
		}},
	}}, 4)
	if len(got) != 1 {
		t.Fatalf("insights=%d want 1", len(got))
	}
	if strings.Contains(got[0].Summary, "Also this:") {
		t.Fatalf("summary=%q still has lead-in", got[0].Summary)
	}
	if strings.Contains(got[0].Summary, "@praze-website/web/index.html") {
		t.Fatalf("summary=%q still has path-heavy prefix", got[0].Summary)
	}
	if !strings.Contains(got[0].Summary, "Guard the unguarded") {
		t.Fatalf("summary=%q missing compacted directive", got[0].Summary)
	}
}

func TestBuildDecisionInsights_TreatsToolReceiptFailuresAsRisks(t *testing.T) {
	t.Parallel()

	got := BuildDecisionInsights([]companion.AnchoredMemoryDerivation{{
		FrameIndex: 0,
		Resolution: companion.InteractionResolutionContinues,
		Reaction:   companion.FollowUpReaction{Outcome: companion.ReactionOutcomeNeutral},
		Candidates: []companion.AnchoredMemoryCandidate{{
			Type:       "follow_up_needed",
			Scope:      companion.CandidateScopeSession,
			Text:       "tool_result: exec_command: exec_command failed: CreateProcess sandbox denied",
			Confidence: 0.7,
			Source:     "tool_receipt",
		}},
	}}, 4)
	if len(got) != 1 {
		t.Fatalf("insights=%d want 1", len(got))
	}
	if got[0].Kind != InsightKindRisk {
		t.Fatalf("kind=%q want %q", got[0].Kind, InsightKindRisk)
	}
	if strings.Contains(strings.ToLower(got[0].Summary), "tool_result:") {
		t.Fatalf("summary=%q still contains tool_result prefix", got[0].Summary)
	}
	if got[0].Summary != "exec_command sandbox denied" {
		t.Fatalf("summary=%q want normalized sandbox issue", got[0].Summary)
	}
}

func TestBuildDecisionInsights_NormalizesSandboxDeniedVariant(t *testing.T) {
	t.Parallel()

	got := BuildDecisionInsights([]companion.AnchoredMemoryDerivation{{
		FrameIndex: 0,
		Resolution: companion.InteractionResolutionContinues,
		Reaction:   companion.FollowUpReaction{Outcome: companion.ReactionOutcomeNeutral},
		Candidates: []companion.AnchoredMemoryCandidate{{
			Type:       "follow_up_needed",
			Scope:      companion.CandidateScopeSession,
			Text:       "tool_result: exec_command: exec_command failed: SandboxDenied { message: \"## test/compare...origin/test/compare\" }",
			Confidence: 0.7,
			Source:     "tool_receipt",
		}},
	}}, 4)
	if len(got) != 1 {
		t.Fatalf("insights=%d want 1", len(got))
	}
	if got[0].Summary != "exec_command sandbox denied" {
		t.Fatalf("summary=%q want normalized sandbox issue", got[0].Summary)
	}
}

func TestInsightFromObjective_DropsEnvironmentScaffold(t *testing.T) {
	t.Parallel()

	if got := InsightFromObjective(&SessionObjective{
		Objective: "<environment_context>\n  <cwd>/Users/joshka/repos/personal/praze</cwd>\n</environment_context>",
	}); len(got) != 0 {
		t.Fatalf("insights=%v want none", got)
	}

	got := InsightFromObjective(&SessionObjective{
		Objective:  "Build final findings-to-evidence matrix across waves.",
		Confidence: 0.8,
	})
	if len(got) != 1 {
		t.Fatalf("insights=%d want 1", len(got))
	}
	if got[0].Kind != InsightKindGoal {
		t.Fatalf("kind=%q want %q", got[0].Kind, InsightKindGoal)
	}
}

func TestBuildInsightBrief_GroupsForScanning(t *testing.T) {
	t.Parallel()

	brief := BuildInsightBrief([]DecisionInsight{
		{Kind: InsightKindGoal, Summary: "Finalize invitation release readiness."},
		{Kind: InsightKindDirection, Summary: "Close W6-C release gate verdict.", Status: InsightStatusActive},
		{Kind: InsightKindDecision, Summary: "Stick to Liquid as the default local proposer.", Status: InsightStatusAccepted},
		{Kind: InsightKindQuestion, Summary: "Do we still need manual QA signoff?", Status: InsightStatusOpen},
		{Kind: InsightKindRisk, Summary: "Docs may drift from evidence links."},
	})
	if brief == nil {
		t.Fatal("expected brief")
		return
	}
	if len(brief.CurrentGoals) != 1 || brief.CurrentGoals[0] != "Finalize invitation release readiness." {
		t.Fatalf("goals=%v", brief.CurrentGoals)
	}
	if len(brief.ActiveDirections) != 1 || brief.ActiveDirections[0] != "Close W6-C release gate verdict." {
		t.Fatalf("active_directions=%v", brief.ActiveDirections)
	}
	if len(brief.AcceptedItems) != 1 || brief.AcceptedItems[0] != "Stick to Liquid as the default local proposer." {
		t.Fatalf("accepted_items=%v", brief.AcceptedItems)
	}
	if len(brief.LatestLearnings) != 1 || brief.LatestLearnings[0] != "Stick to Liquid as the default local proposer." {
		t.Fatalf("latest_learnings=%v", brief.LatestLearnings)
	}
	if len(brief.OpenQuestions) != 1 || brief.OpenQuestions[0] != "Do we still need manual QA signoff?" {
		t.Fatalf("open_questions=%v", brief.OpenQuestions)
	}
	if brief.Overview == "" {
		t.Fatal("expected overview")
	}
}

func TestBuildInsightBrief_LearnsFromAcceptedAssistantGuidance(t *testing.T) {
	t.Parallel()

	brief := BuildInsightBrief([]DecisionInsight{
		{
			Kind:        InsightKindDirection,
			Summary:     "Prefer a shared query layer for group-aware Pulse and Margin filters.",
			Status:      InsightStatusAccepted,
			Confidence:  0.82,
			SourceBasis: "assistant_guidance",
		},
	})
	if brief == nil {
		t.Fatal("expected brief")
		return
	}
	if len(brief.LatestLearnings) != 1 {
		t.Fatalf("latest_learnings=%v want 1 item", brief.LatestLearnings)
	}
}

func TestBuildInsightBrief_SuppressesAssistantStatusUpdatesFromActiveDirections(t *testing.T) {
	t.Parallel()

	brief := BuildInsightBrief([]DecisionInsight{
		{
			Kind:        InsightKindDirection,
			Summary:     "Committed as `9c397f87` with `feat` after the frontend/auth batch.",
			Status:      InsightStatusActive,
			Confidence:  0.66,
			SourceBasis: "user_approved",
			Tags:        []string{"assistant-guidance", "technical-context"},
		},
		{
			Kind:        InsightKindDirection,
			Summary:     "Guard the unguarded",
			Status:      InsightStatusActive,
			Confidence:  0.68,
			SourceBasis: "user",
		},
	})
	if brief == nil {
		t.Fatal("expected brief")
		return
	}
	if len(brief.ActiveDirections) != 1 || brief.ActiveDirections[0] != "Guard the unguarded" {
		t.Fatalf("active_directions=%v", brief.ActiveDirections)
	}
}

func TestBuildInsightBrief_DoesNotUseAssistantGuidanceAsActiveDirection(t *testing.T) {
	t.Parallel()

	brief := BuildInsightBrief([]DecisionInsight{
		{
			Kind:        InsightKindDirection,
			Summary:     "non-presence hooks are real instead of client-only scaffolds.",
			Status:      InsightStatusActive,
			Confidence:  0.66,
			SourceBasis: "user_approved",
			Tags:        []string{"assistant-guidance", "technical-context"},
		},
	})
	if brief != nil && len(brief.ActiveDirections) != 0 {
		t.Fatalf("active_directions=%v want none for assistant guidance", brief.ActiveDirections)
	}
}

func TestFinalizeDecisionInsights_PreservesRiskWhenDirectionsDominate(t *testing.T) {
	t.Parallel()

	in := []DecisionInsight{
		{Kind: InsightKindDirection, Summary: "Direction one", Status: InsightStatusActive, Confidence: 0.8},
		{Kind: InsightKindDirection, Summary: "Direction two", Status: InsightStatusActive, Confidence: 0.79},
		{Kind: InsightKindDirection, Summary: "Direction three", Status: InsightStatusActive, Confidence: 0.78},
		{Kind: InsightKindRisk, Summary: "Sandbox denied while checking git state.", Status: InsightStatusActive, Confidence: 0.7},
	}
	got := FinalizeDecisionInsights(in, 3)
	foundRisk := false
	for _, item := range got {
		if item.Kind == InsightKindRisk {
			foundRisk = true
			break
		}
	}
	if !foundRisk {
		t.Fatalf("expected risk to survive capped finalization, got %#v", got)
	}
}

func TestBuildInsightTimeline_BuildsNotableWindows(t *testing.T) {
	t.Parallel()

	derivations := []companion.AnchoredMemoryDerivation{
		{
			FrameIndex:         0,
			InteractionSummary: "user: <environment_context> <cwd>/Users/joshka/repos/personal/praze</cwd> </environment_context> | assistant: ok",
			Resolution:         companion.InteractionResolutionContinues,
			Reaction:           companion.FollowUpReaction{Outcome: companion.ReactionOutcomeNeutral},
		},
		{
			FrameIndex:         1,
			InteractionSummary: "user asked to finalize the release gate verdict",
			Resolution:         companion.InteractionResolutionUnresolved,
			Reaction:           companion.FollowUpReaction{Outcome: companion.ReactionOutcomeUnresolved},
		},
		{
			FrameIndex:         2,
			InteractionSummary: "assistant confirmed the remaining blockers",
			Resolution:         companion.InteractionResolutionResolved,
			Reaction:           companion.FollowUpReaction{Outcome: companion.ReactionOutcomeAccepted},
		},
	}
	insights := []DecisionInsight{
		{
			Kind:                 InsightKindDirection,
			Summary:              "Finalize the release gate verdict.",
			Status:               InsightStatusActive,
			EvidenceFrameIndices: []int{1},
		},
	}

	got := BuildInsightTimeline(derivations, insights, 4)
	if len(got) != 1 {
		t.Fatalf("timeline=%d want 1", len(got))
	}
	if got[0].StartFrame != 1 || got[0].EndFrame != 1 {
		t.Fatalf("window=%d..%d want 1..1", got[0].StartFrame, got[0].EndFrame)
	}
	if got[0].Headline != "Finalize the release gate verdict." {
		t.Fatalf("headline=%q", got[0].Headline)
	}
	if len(got[0].ContextWindow) != 3 {
		t.Fatalf("context_window=%v want 3 items", got[0].ContextWindow)
	}
	for _, line := range got[0].ContextWindow {
		if line == "" {
			t.Fatal("expected non-empty timeline context line")
		}
		if containsAlphaNum(line) && line == "Continued: ." {
			t.Fatalf("unexpected degenerate line %q", line)
		}
		if strings.Contains(strings.ToLower(line), "environment_context") {
			t.Fatalf("timeline context leaked scaffold: %q", line)
		}
	}
	if strings.Contains(got[0].ContextWindow[0], "user: | assistant:") {
		t.Fatalf("timeline context kept empty placeholders: %q", got[0].ContextWindow[0])
	}
}

func TestBuildNotableInsights_ClassifiesTypedWindows(t *testing.T) {
	t.Parallel()

	derivations := []companion.AnchoredMemoryDerivation{
		{
			FrameIndex:         0,
			InteractionSummary: "user asked for a release plan",
			Resolution:         companion.InteractionResolutionContinues,
			Reaction:           companion.FollowUpReaction{Outcome: companion.ReactionOutcomeNeutral},
		},
		{
			FrameIndex:         1,
			InteractionSummary: "user corrected the earlier direction",
			Resolution:         companion.InteractionResolutionCorrected,
			Reaction:           companion.FollowUpReaction{Outcome: companion.ReactionOutcomeCorrected},
			Candidates: []companion.AnchoredMemoryCandidate{{
				Type:       "user_correction",
				Scope:      companion.CandidateScopeSession,
				Text:       "No, use the grouped doctrine command instead.",
				Confidence: 0.8,
				Source:     "followup_user",
			}},
		},
		{
			FrameIndex:         2,
			InteractionSummary: "assistant proposed a reusable review flow",
			Resolution:         companion.InteractionResolutionResolved,
			Reaction:           companion.FollowUpReaction{Outcome: companion.ReactionOutcomeAccepted},
		},
	}
	insights := []DecisionInsight{
		{
			Kind:                 InsightKindDirection,
			Summary:              "Use the grouped doctrine command instead.",
			Status:               InsightStatusAccepted,
			EvidenceFrameIndices: []int{1},
		},
	}

	got := BuildNotableInsights(derivations, insights, 4)
	if len(got) == 0 {
		t.Fatal("expected notable insights")
	}
	if got[0].Kind != NotableInsightMisunderstanding {
		t.Fatalf("kind=%q want %q", got[0].Kind, NotableInsightMisunderstanding)
	}
	if got[0].Headline == "" || got[0].WhyItMatters == "" {
		t.Fatalf("notable=%+v", got[0])
	}
}

func TestBuildNotableInsights_AddsGlobalConsensusNotable(t *testing.T) {
	t.Parallel()

	derivations := []companion.AnchoredMemoryDerivation{
		{
			FrameIndex:         0,
			InteractionSummary: "user asked for a better pipeline",
			Resolution:         companion.InteractionResolutionResolved,
			Reaction:           companion.FollowUpReaction{Outcome: companion.ReactionOutcomeAccepted},
		},
	}
	got := BuildNotableInsights(derivations, []DecisionInsight{
		{
			Kind:        InsightKindDirection,
			Summary:     "Implement auto-memory as a second-pass consolidator over the existing hybrid companion runtime.",
			Status:      InsightStatusSupported,
			SourceBasis: "sidecar_consensus",
			Tags:        []string{"consensus", "sidecar"},
		},
	}, 4)
	if len(got) != 1 {
		t.Fatalf("notables=%d want 1", len(got))
	}
	if got[0].Kind != NotableInsightSurprise {
		t.Fatalf("kind=%q want %q", got[0].Kind, NotableInsightSurprise)
	}
}

func TestBuildNotableInsights_ClassifiesAssistantGuidanceGapAsSurprise(t *testing.T) {
	t.Parallel()

	derivations := []companion.AnchoredMemoryDerivation{{
		FrameIndex:         0,
		InteractionSummary: "user asked to map the rebuilt frontend architecture to the target surfaces",
		Resolution:         companion.InteractionResolutionUnresolved,
		Reaction:           companion.FollowUpReaction{Outcome: companion.ReactionOutcomeUnresolved},
	}}
	got := BuildNotableInsights(derivations, []DecisionInsight{
		{
			Kind:                 InsightKindDirection,
			Summary:              "map the current rebuilt frontend architecture to these Paper target surfaces",
			Status:               InsightStatusActive,
			SourceBasis:          "user",
			EvidenceFrameIndices: []int{0},
			Tags:                 []string{"follow-up-needed", "user"},
		},
		{
			Kind:                 InsightKindDirection,
			Summary:              "most surfaces are still first-pass renderers over rebuilt endpoints rather than polished destination experiences.",
			Status:               InsightStatusActive,
			SourceBasis:          "user_approved",
			EvidenceFrameIndices: []int{0},
			Tags:                 []string{"assistant-guidance", "technical-context"},
		},
	}, 4)
	if len(got) == 0 {
		t.Fatal("expected notable insights")
	}
	if got[0].Kind != NotableInsightSurprise {
		t.Fatalf("kind=%q want %q", got[0].Kind, NotableInsightSurprise)
	}
}

func TestBuildNotableInsights_DoesNotTreatSimpleRephraseAsSurprise(t *testing.T) {
	t.Parallel()

	derivations := []companion.AnchoredMemoryDerivation{{
		FrameIndex:         0,
		InteractionSummary: "user asked to finalize the release gate verdict",
		Resolution:         companion.InteractionResolutionUnresolved,
		Reaction:           companion.FollowUpReaction{Outcome: companion.ReactionOutcomeUnresolved},
	}}
	got := BuildNotableInsights(derivations, []DecisionInsight{
		{
			Kind:                 InsightKindDirection,
			Summary:              "finalize the release gate verdict",
			Status:               InsightStatusActive,
			SourceBasis:          "user",
			EvidenceFrameIndices: []int{0},
		},
		{
			Kind:                 InsightKindDirection,
			Summary:              "close the release gate verdict",
			Status:               InsightStatusActive,
			SourceBasis:          "user_approved",
			EvidenceFrameIndices: []int{0},
			Tags:                 []string{"assistant-guidance", "technical-context"},
		},
	}, 4)
	if len(got) == 0 {
		t.Fatal("expected notable insights")
	}
	if got[0].Kind == NotableInsightSurprise {
		t.Fatalf("unexpected surprise classification: %+v", got[0])
	}
}

func TestBuildNotableInsights_SuppressesAcceptedMetaAsEpisodic(t *testing.T) {
	t.Parallel()

	derivations := []companion.AnchoredMemoryDerivation{
		{
			FrameIndex:         0,
			InteractionSummary: "assistant explained a completed pipeline refactor",
			Resolution:         companion.InteractionResolutionResolved,
			Reaction:           companion.FollowUpReaction{Outcome: companion.ReactionOutcomeAccepted},
		},
	}
	got := BuildNotableInsights(derivations, []DecisionInsight{
		{
			Kind:                 InsightKindDirection,
			Summary:              "The insight lane is now more usable for both agents and humans.",
			Status:               InsightStatusAccepted,
			SourceBasis:          "user_approved",
			EvidenceFrameIndices: []int{0},
		},
	}, 4)
	if len(got) != 0 {
		t.Fatalf("notables=%v want none", got)
	}
}
