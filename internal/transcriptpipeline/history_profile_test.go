package transcriptpipeline

import (
	"strings"
	"testing"

	historypkg "github.com/jkatigb/agentctl/internal/transcriptpipeline/history"
)

func TestDefaultHistoryProfile_HasCoreQuestions(t *testing.T) {
	t.Parallel()

	got := historypkg.DefaultHistoryProfile()
	if got == nil {
		t.Fatal("expected profile")
		return
	}
	if got.ProfileID == "" {
		t.Fatal("expected profile id")
	}
	if len(got.Questions) < 8 {
		t.Fatalf("questions=%d want at least 8", len(got.Questions))
	}
	if got.Questions[0].ID != HistoryQuestionObjective {
		t.Fatalf("first question=%q want %q", got.Questions[0].ID, HistoryQuestionObjective)
	}
}

func TestBuildHistoryAnswers_UsesInsightSurfaces(t *testing.T) {
	t.Parallel()

	profile := historypkg.DefaultHistoryProfile()
	brief := &InsightBrief{
		ActiveDirections: []string{"Finalize the release gate verdict."},
		LatestLearnings:  []string{"Use named_memory as the durable sink for transcript-derived memories."},
		OpenQuestions:    []string{"Do we still need manual QA signoff?"},
	}
	notable := []NotableInsight{
		{Kind: NotableInsightMisunderstanding, Headline: "We initially pushed doctrine too hard.", StartFrame: 3, EndFrame: 4},
		{Kind: NotableInsightSurprise, Headline: "Consensus surfaced the hybrid companion runtime as the real substrate.", StartFrame: 10, EndFrame: 10},
	}
	objective := &SessionObjective{
		Objective:  "Build a usable transcript history layer for agents and humans.",
		Confidence: 0.8,
		Evidence:   []string{"Build a usable transcript history layer for agents and humans."},
	}

	got := BuildHistoryAnswers(profile, objective, brief, notable, nil)
	if len(got) < 5 {
		t.Fatalf("answers=%d want at least 5", len(got))
	}
	var foundObjective, foundDirections, foundLearned, foundMisunderstanding, foundNext bool
	for _, item := range got {
		switch item.QuestionID {
		case HistoryQuestionObjective:
			foundObjective = true
		case HistoryQuestionActiveDirections:
			foundDirections = true
		case HistoryQuestionAcceptedLearnings:
			foundLearned = true
		case HistoryQuestionMisunderstandings:
			foundMisunderstanding = true
		case HistoryQuestionNextStep:
			foundNext = true
		}
	}
	if !foundObjective || !foundDirections || !foundLearned || !foundMisunderstanding || !foundNext {
		t.Fatalf("answers=%+v missing required question ids", got)
	}
}

func TestBuildHistoryAnswers_UsesRiskFallbackForGotchas(t *testing.T) {
	t.Parallel()

	profile := historypkg.DefaultHistoryProfile()
	brief := &InsightBrief{
		Risks: []string{"exec_command failed: sandbox denied"},
	}
	got := BuildHistoryAnswers(profile, nil, brief, nil, nil)
	found := false
	for _, item := range got {
		if item.QuestionID == HistoryQuestionGotchas {
			found = true
			if item.Answer == "" {
				t.Fatalf("gotcha answer empty in %+v", got)
			}
		}
	}
	if !found {
		t.Fatalf("expected gotcha fallback answer in %+v", got)
	}
}

func TestBuildHistoryAnswers_AddsRegressionAndRecurringMistakeSlots(t *testing.T) {
	t.Parallel()

	profile := historypkg.DefaultHistoryProfile()
	notable := []NotableInsight{
		{Kind: NotableInsightMisunderstanding, Headline: "We initially pushed doctrine too hard.", StartFrame: 3, EndFrame: 4},
		{Kind: NotableInsightGotcha, Headline: "Sandbox denied while checking git state.", StartFrame: 5, EndFrame: 5},
	}
	got := BuildHistoryAnswers(profile, nil, &InsightBrief{
		Risks: []string{"Sandbox denied while checking git state."},
	}, notable, []DecisionInsight{
		{
			Kind:        InsightKindRisk,
			Summary:     "Sandbox denied while checking git state.",
			Status:      InsightStatusActive,
			Confidence:  0.7,
			SourceBasis: "mixed",
			Tags:        []string{"tool-receipt"},
		},
	})
	var foundRegression, foundRecurring bool
	for _, item := range got {
		switch item.QuestionID {
		case HistoryQuestionRegressions:
			foundRegression = true
		case HistoryQuestionRecurringMistakes:
			foundRecurring = true
		}
	}
	if !foundRegression || !foundRecurring {
		t.Fatalf("answers=%+v missing regression/recurring slots", got)
	}
}

func TestBuildHistoryAnswers_UsesInsightFallbacksForLearningsAndSurprises(t *testing.T) {
	t.Parallel()

	profile := historypkg.DefaultHistoryProfile()
	brief := &InsightBrief{}
	got := BuildHistoryAnswers(profile, nil, brief, nil, []DecisionInsight{
		{
			Kind:        InsightKindDirection,
			Summary:     "Prefer a shared query layer for group-aware Pulse and Margin filters.",
			Status:      InsightStatusAccepted,
			Confidence:  0.82,
			SourceBasis: "user_approved",
			Tags:        []string{"assistant-guidance"},
		},
		{
			Kind:        InsightKindRisk,
			Summary:     "CreateProcess sandbox denied while checking git state.",
			Status:      InsightStatusActive,
			Confidence:  0.7,
			SourceBasis: "mixed",
			Tags:        []string{"tool-receipt"},
		},
	})
	var foundLearnings, foundSurprises bool
	for _, item := range got {
		switch item.QuestionID {
		case HistoryQuestionAcceptedLearnings:
			foundLearnings = true
		case HistoryQuestionSurprises:
			foundSurprises = true
		}
	}
	if !foundLearnings || !foundSurprises {
		t.Fatalf("answers=%+v missing learning/surprise fallbacks", got)
	}
}

func TestBuildHistoryAnswers_UsesProvisionalAssistantGuidanceAsLearningFallback(t *testing.T) {
	t.Parallel()

	profile := historypkg.DefaultHistoryProfile()
	got := BuildHistoryAnswers(profile, nil, &InsightBrief{}, []NotableInsight{
		{Kind: NotableInsightSurprise, Headline: "Pulse Feed should hang off the main feed route, while Reader Workspace should center on the reader screen.", StartFrame: 0, EndFrame: 0},
	}, []DecisionInsight{
		{
			Kind:        InsightKindDirection,
			Summary:     "Pulse Feed should hang off the main feed route, while Reader Workspace should center on the reader screen.",
			Status:      InsightStatusActive,
			Confidence:  0.66,
			SourceBasis: "user_approved",
			Tags:        []string{"technical-context", "assistant-guidance"},
		},
	})
	found := false
	for _, item := range got {
		if item.QuestionID == HistoryQuestionAcceptedLearnings {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("answers=%+v missing assistant guidance learning fallback", got)
	}
}

func TestBuildHistoryAnswers_UsesCompactedAssistantGuidanceAsLearningFallback(t *testing.T) {
	t.Parallel()

	profile := historypkg.DefaultHistoryProfile()
	got := BuildHistoryAnswers(profile, nil, &InsightBrief{}, []NotableInsight{
		{Kind: NotableInsightSurprise, Headline: "most surfaces are still first-pass renderers over rebuilt endpoints rather than polished flows", StartFrame: 0, EndFrame: 0},
	}, []DecisionInsight{
		{
			Kind:        InsightKindDirection,
			Summary:     "the rebuilt app has the right terrain routes and `src/api/**` boundaries, but most surfaces are still first-pass renderers over rebuilt endpoints rather than polished flows",
			Status:      InsightStatusActive,
			Confidence:  0.66,
			SourceBasis: "user_approved",
			Tags:        []string{"technical-context", "assistant-guidance"},
		},
	})
	found := false
	for _, item := range got {
		if item.QuestionID == HistoryQuestionAcceptedLearnings {
			found = true
			if !strings.Contains(strings.ToLower(item.Answer), "most surfaces are still first-pass renderers") {
				t.Fatalf("learning answer=%q", item.Answer)
			}
		}
	}
	if !found {
		t.Fatalf("answers=%+v missing compacted assistant guidance learning fallback", got)
	}
}

func TestBuildHistoryAnswers_DoesNotPromoteOperationalProgressAsLearningWithoutNotable(t *testing.T) {
	t.Parallel()

	profile := historypkg.DefaultHistoryProfile()
	got := BuildHistoryAnswers(profile, nil, &InsightBrief{}, nil, []DecisionInsight{
		{
			Kind:        InsightKindDirection,
			Summary:     "Implemented the requested hardening changes and pushed them to PR #116.",
			Status:      InsightStatusActive,
			Confidence:  0.66,
			SourceBasis: "user_approved",
			Tags:        []string{"technical-context", "assistant-guidance"},
		},
	})
	for _, item := range got {
		if item.QuestionID == HistoryQuestionAcceptedLearnings {
			t.Fatalf("unexpected operational progress learning fallback: %+v", got)
		}
	}
}

func TestBuildHistoryPack_ComposesAgentAndHumanViews(t *testing.T) {
	t.Parallel()

	pack := historypkg.BuildHistoryPack([]historypkg.HistoryAnswer{
		{QuestionID: HistoryQuestionObjective, Answer: "Build a usable transcript history layer for agents and humans.", Confidence: 0.8},
		{QuestionID: HistoryQuestionActiveDirections, Answer: "Continue the second-pass consolidator direction. | Persist history records for retrieval.", Confidence: 0.74},
		{QuestionID: HistoryQuestionAcceptedLearnings, Answer: "Use named_memory as the durable sink for transcript-derived memories.", Confidence: 0.8},
		{QuestionID: HistoryQuestionMisunderstandings, Answer: "We initially pushed doctrine too hard.", Confidence: 0.82},
		{QuestionID: HistoryQuestionSurprises, Answer: "Consensus surfaced the hybrid companion runtime as the real substrate.", Confidence: 0.8},
		{QuestionID: HistoryQuestionOpenQuestions, Answer: "Do we still need manual QA signoff?", Confidence: 0.7},
		{QuestionID: HistoryQuestionNextStep, Answer: "Persist history records for retrieval.", Confidence: 0.72},
	})
	if pack == nil {
		t.Fatal("expected pack")
		return
	}
	if pack.Overview == "" {
		t.Fatal("expected overview")
	}
	if pack.CurrentObjective == "" || len(pack.ContinueWith) != 2 {
		t.Fatalf("pack=%+v", pack)
	}
	if pack.AgentBrief == "" {
		t.Fatal("expected agent brief")
	}
	if len(pack.HumanBrief) < 4 {
		t.Fatalf("human_brief=%v want at least 4 lines", pack.HumanBrief)
	}
	if pack.NextStep != "Persist history records for retrieval." {
		t.Fatalf("next_step=%q", pack.NextStep)
	}
}

func TestBuildHistoryPack_DedupesObjectiveAndContinueWith(t *testing.T) {
	t.Parallel()

	pack := historypkg.BuildHistoryPack([]historypkg.HistoryAnswer{
		{QuestionID: HistoryQuestionObjective, Answer: "Map the rebuilt frontend architecture to the Paper target surfaces.", Confidence: 0.8},
		{QuestionID: HistoryQuestionActiveDirections, Answer: "Map the rebuilt frontend architecture to the Paper target surfaces.", Confidence: 0.74},
		{QuestionID: HistoryQuestionNextStep, Answer: "Map the rebuilt frontend architecture to the Paper target surfaces.", Confidence: 0.72},
	})
	if pack == nil {
		t.Fatal("expected pack")
		return
	}
	if len(pack.ContinueWith) != 0 {
		t.Fatalf("continue_with=%v want empty after dedupe", pack.ContinueWith)
	}
	if pack.NextStep != "" {
		t.Fatalf("next_step=%q want empty after dedupe", pack.NextStep)
	}
	if pack.Overview != "Objective: Map the rebuilt frontend architecture to the Paper target surfaces." {
		t.Fatalf("overview=%q", pack.Overview)
	}
}

func TestBuildHistoryPack_PrefersCompactObjectiveLabelWhenMeaningfullyShorter(t *testing.T) {
	t.Parallel()

	pack := historypkg.BuildHistoryPack([]historypkg.HistoryAnswer{
		{QuestionID: HistoryQuestionObjective, Answer: "Can we go over our app as well and try to look at all the possible error states and error boundaries and make our app production ready for that.", Label: "make our app production ready", Confidence: 0.8},
		{QuestionID: HistoryQuestionAcceptedLearnings, Answer: "The reflections list was fetched from optional seed state.", Confidence: 0.8},
	})
	if pack == nil {
		t.Fatal("expected pack")
		return
	}
	if pack.ObjectiveLabel != "make our app production ready" {
		t.Fatalf("objective_label=%q", pack.ObjectiveLabel)
	}
	if pack.Overview != "Objective: make our app production ready" {
		t.Fatalf("overview=%q want compact objective label", pack.Overview)
	}
	if !strings.Contains(pack.AgentBrief, "Objective: make our app production ready") {
		t.Fatalf("agent_brief=%q missing compact objective label", pack.AgentBrief)
	}
}

func TestBuildHistoryAnswers_PrefersUserDirectionsForActiveDirections(t *testing.T) {
	t.Parallel()

	profile := historypkg.DefaultHistoryProfile()
	brief := &InsightBrief{
		ActiveDirections: []string{
			"Guard the unguarded",
			"Implemented the requested hardening changes and pushed them to PR #116.",
		},
	}
	got := BuildHistoryAnswers(profile, nil, brief, nil, []DecisionInsight{
		{
			Kind:        InsightKindDirection,
			Summary:     "Guard the unguarded",
			Status:      InsightStatusActive,
			Confidence:  0.68,
			SourceBasis: "user",
		},
		{
			Kind:        InsightKindDirection,
			Summary:     "Implemented the requested hardening changes and pushed them to PR #116.",
			Status:      InsightStatusActive,
			Confidence:  0.66,
			SourceBasis: "user_approved",
			Tags:        []string{"assistant-guidance", "technical-context"},
		},
	})
	for _, item := range got {
		if item.QuestionID == HistoryQuestionActiveDirections {
			if strings.Contains(item.Answer, "Implemented the requested hardening changes") {
				t.Fatalf("active_directions=%q should prefer user-driven continuation", item.Answer)
			}
			return
		}
	}
	t.Fatalf("missing active directions answer in %+v", got)
}

func TestBuildHistoryPack_CarriesRegressionFields(t *testing.T) {
	t.Parallel()

	pack := historypkg.BuildHistoryPack([]historypkg.HistoryAnswer{
		{QuestionID: HistoryQuestionObjective, Answer: "Stabilize transcript continuity.", Confidence: 0.8},
		{QuestionID: HistoryQuestionRegressions, Answer: "Sandbox denied while checking git state.", Confidence: 0.72},
		{QuestionID: HistoryQuestionRecurringMistakes, Answer: "We initially pushed doctrine too hard. | Sandbox denied while checking git state.", Confidence: 0.7},
	})
	if pack == nil {
		t.Fatal("expected pack")
		return
	}
	if len(pack.Regressions) != 1 || len(pack.RecurringMistakes) != 2 {
		t.Fatalf("pack=%+v", pack)
	}
	if pack.AgentBrief == "" || !strings.Contains(pack.AgentBrief, "Regressions:") || !strings.Contains(pack.AgentBrief, "Recurring mistakes:") {
		t.Fatalf("agent_brief=%q", pack.AgentBrief)
	}
}
