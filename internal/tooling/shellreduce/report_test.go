package shellreduce

import "testing"

func TestSummarizeReport(t *testing.T) {
	cases := []ReportCase{
		{
			Command:           "git log --stat -5",
			Operation:         "git log",
			Weight:            5,
			Status:            "ok",
			Recommendation:    "reduce",
			RawComparable:     true,
			RawBytes:          3000,
			ReducedBytes:      300,
			BytesSaved:        2700,
			RawTokens:         900,
			ReducedTokens:     90,
			TokensSaved:       810,
			RawDurationMS:     120,
			ReducedDurationMS: 30,
			DurationSavedMS:   90,
			RawCostUSD:        0.0009,
			ReducedCostUSD:    0.00009,
			CostSavedUSD:      0.00081,
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
	if got.TotalRawDurationMS != 120 || got.TotalReducedDurationMS != 30 || got.TotalDurationSavedMS != 90 {
		t.Fatalf("duration totals=%+v", got)
	}
	if got.WeightedRawDurationMS != 600 || got.WeightedReducedDurationMS != 150 || got.WeightedDurationSavedMS != 450 {
		t.Fatalf("weighted duration totals=%+v", got)
	}
	if got.TotalCostSavedUSD <= 0 || got.WeightedCostSavedUSD <= got.TotalCostSavedUSD {
		t.Fatalf("cost totals=%+v", got)
	}
	if got.TotalTokensSavedPercent <= 0 {
		t.Fatalf("expected positive token savings, got %+v", got)
	}
}

func TestBuildReportCaseIncludesLatencyAndCost(t *testing.T) {
	got := BuildReportCase("git log --stat -5", "git log", 5, map[string]any{
		"route": map[string]any{
			"intent": "git_log",
			"native": "git",
		},
		"summary": "git log: 5 commits",
		"measure": map[string]any{
			"raw": map[string]any{
				"combined_bytes":           1000,
				"combined_tokens":          300,
				"duration_ms":              40,
				"estimated_input_cost_usd": 0.0003,
			},
			"reduced": map[string]any{
				"bytes":                    100,
				"tokens":                   30,
				"duration_ms":              10,
				"estimated_input_cost_usd": 0.00003,
			},
			"savings": map[string]any{
				"bytes_saved":                        900,
				"bytes_saved_percent":                90.0,
				"tokens_saved":                       270,
				"tokens_saved_percent":               90.0,
				"duration_saved_ms":                  30,
				"duration_saved_percent":             75.0,
				"estimated_input_cost_saved_usd":     0.00027,
				"estimated_input_cost_saved_percent": 90.0,
			},
			"advice": map[string]any{
				"mode":   "reduce",
				"reason": "saves output",
			},
		},
	})

	if got.RawDurationMS != 40 || got.ReducedDurationMS != 10 || got.DurationSavedMS != 30 {
		t.Fatalf("duration fields=%+v", got)
	}
	if got.RawCostUSD != 0.0003 || got.ReducedCostUSD != 0.00003 || got.CostSavedUSD != 0.00027 {
		t.Fatalf("cost fields=%+v", got)
	}
	if got.CostSavedPercent != 90 {
		t.Fatalf("CostSavedPercent=%v want 90", got.CostSavedPercent)
	}
	if got.MeasureSummary == "" || got.Recommendation != "reduce" {
		t.Fatalf("summary/recommendation=%+v", got)
	}
}
