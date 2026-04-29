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

func TestSummarizeReviewTelemetry(t *testing.T) {
	t.Parallel()

	attempts := []Attempt{attempt("q1", ConditionRLMReplRecursive, true, 1000, 10)}
	attempts[0].RLM = &RLMAttemptMeta{Metadata: map[string]any{
		"parent_total_tokens":            250,
		"child_total_tokens":             50,
		"pre_review_output_sanitization": map[string]any{"changed": true},
		"review": map[string]any{
			"review_recursive_requested":  true,
			"review_recursive_used":       true,
			"review_candidate_compaction": map[string]any{"changed": true},
			"parent_total_tokens":         400,
			"child_total_tokens":          300,
			"recursive_trace": map[string]any{
				"children": []any{
					map[string]any{
						"summary_truncated":         true,
						"summary_compaction_method": "rewrite",
					},
				},
			},
		},
	}}

	summary := Summarize(attempts, nil)
	if len(summary.Conditions) != 1 {
		t.Fatalf("conditions=%d", len(summary.Conditions))
	}
	got := summary.Conditions[0]
	if got.ReviewAttempts != 1 ||
		got.ReviewRecursiveRequested != 1 ||
		got.ReviewRecursiveUsed != 1 ||
		got.ReviewCandidateCompactions != 1 ||
		got.PreReviewOutputSanitizations != 1 ||
		got.ChildSummariesTruncated != 1 ||
		got.ChildSummariesRewritten != 1 {
		t.Fatalf("review summary=%+v", got)
	}
	if got.MeanBaseTokens != 300 || got.MeanReviewTokens != 700 {
		t.Fatalf("token split base=%v review=%v", got.MeanBaseTokens, got.MeanReviewTokens)
	}

	attempts[0].Usage.TotalTokens = 500
	summary = Summarize(attempts, nil)
	got = summary.Conditions[0]
	if got.MeanBaseTokens != 300 || got.MeanReviewTokens != 200 {
		t.Fatalf("clamped token split base=%v review=%v", got.MeanBaseTokens, got.MeanReviewTokens)
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
