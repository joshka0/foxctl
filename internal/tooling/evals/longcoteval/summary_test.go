package longcoteval

import "testing"

func TestSummarizePairedComparisons(t *testing.T) {
	t.Parallel()

	attempts := []Attempt{
		attempt("q1", ConditionBaselineNoToolsOfficial, true, 100, 10),
		attempt("q1", ConditionRLMNoToolsSingle, false, 90, 12),
		attempt("q2", ConditionBaselineNoToolsOfficial, false, 200, 20),
		attempt("q2", ConditionRLMNoToolsSingle, true, 180, 25),
		attempt("q3", ConditionBaselineNoToolsOfficial, true, 300, 30),
		attempt("q3", ConditionRLMNoToolsSingle, true, 330, 35),
		attempt("q4", ConditionBaselineNoToolsOfficial, false, 400, 40),
		attempt("q4", ConditionRLMNoToolsSingle, false, 410, 50),
	}

	summary := Summarize(attempts, []Comparison{{
		Baseline:  ConditionBaselineNoToolsOfficial,
		Candidate: ConditionRLMNoToolsSingle,
	}})
	if len(summary.Comparisons) != 1 {
		t.Fatalf("comparisons=%d", len(summary.Comparisons))
	}
	got := summary.Comparisons[0]
	if got.Pairs != 4 || got.Wins != 1 || got.Losses != 1 || got.TieCorrect != 1 || got.TieIncorrect != 1 {
		t.Fatalf("comparison=%+v", got)
	}
	if got.MeanTokenDelta != 2.5 {
		t.Fatalf("mean token delta=%v", got.MeanTokenDelta)
	}
}

func TestSummarizeKeepsLatestDuplicate(t *testing.T) {
	t.Parallel()

	attempts := []Attempt{
		attempt("q1", ConditionRLMNoToolsSingle, false, 100, 10),
		attempt("q1", ConditionRLMNoToolsSingle, true, 80, 10),
	}
	summary := Summarize(attempts, nil)
	if summary.DuplicateAttempts != 1 {
		t.Fatalf("duplicates=%d", summary.DuplicateAttempts)
	}
	if len(summary.Conditions) != 1 || summary.Conditions[0].CorrectAttempts != 1 || summary.Conditions[0].MeanTotalTokens != 80 {
		t.Fatalf("summary=%+v", summary)
	}
}

func attempt(questionID string, conditionID ConditionID, correct bool, tokens int, duration int64) Attempt {
	status := VerifierStatusIncorrect
	if correct {
		status = VerifierStatusCorrect
	}
	return Attempt{
		QuestionID:     questionID,
		ConditionID:    conditionID,
		ConditionKind:  ConditionKindRLM,
		Status:         AttemptStatusOK,
		Correct:        correct,
		VerifierStatus: status,
		Usage: Usage{
			TotalTokens: tokens,
		},
		DurationMS: duration,
	}
}
