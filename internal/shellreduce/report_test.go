package shellreduce

import "testing"

func TestSummarizeReport(t *testing.T) {
	cases := []ReportCase{
		{
			Command:        "git log --stat -5",
			Operation:      "git log",
			Weight:         5,
			Status:         "ok",
			Recommendation: "reduce",
			RawComparable:  true,
			RawBytes:       3000,
			ReducedBytes:   300,
			BytesSaved:     2700,
			RawTokens:      900,
			ReducedTokens:  90,
			TokensSaved:    810,
		},
		{
			Command:   "find internal -maxdepth 1 -name '*.go'",
			Operation: "find",
			Weight:    10,
			Status:    "optional_unavailable",
			Error:     "requires gfind",
		},
		{
			Command:   "docker ps",
			Operation: "docker ps",
			Weight:    3,
			Status:    "skipped",
			Error:     "mutating command family intentionally excluded",
		},
	}

	got := SummarizeReport(cases)
	if got.CaseCount != 3 || got.OKCount != 1 || got.ErrorCount != 0 {
		t.Fatalf("counts=%+v", got)
	}
	if got.WeightedCaseCount != 18 {
		t.Fatalf("weighted_case_count=%d want 18", got.WeightedCaseCount)
	}
	if got.ComparableCaseCount != 1 || got.NonComparableCaseCount != 1 {
		t.Fatalf("comparable counts=%+v", got)
	}
	if got.PreferReduceCount != 1 || got.OptionalUnavailableCount != 1 || got.SkippedCount != 1 {
		t.Fatalf("recommendations=%+v", got)
	}
	if got.TotalRawTokens != 900 || got.TotalReducedTokens != 90 || got.TotalTokensSaved != 810 {
		t.Fatalf("token totals=%+v", got)
	}
	if got.WeightedRawTokens != 4500 || got.WeightedReducedTokens != 450 || got.WeightedTokensSaved != 4050 {
		t.Fatalf("weighted token totals=%+v", got)
	}
	if got.TotalTokensSavedPercent <= 0 {
		t.Fatalf("expected positive token savings, got %+v", got)
	}
}
