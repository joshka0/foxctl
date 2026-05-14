package shellreduce

// ReportCase captures one measured shell reduction result.
type ReportCase struct {
	Command              string  `json:"command"`
	Operation            string  `json:"operation,omitempty"`
	Weight               int     `json:"weight,omitempty"`
	Status               string  `json:"status"`
	Intent               string  `json:"intent,omitempty"`
	Skill                string  `json:"skill,omitempty"`
	Summary              string  `json:"summary,omitempty"`
	MeasureSummary       string  `json:"measure_summary,omitempty"`
	Recommendation       string  `json:"recommendation,omitempty"`
	RecommendationWhy    string  `json:"recommendation_reason,omitempty"`
	RawBytes             int     `json:"raw_bytes,omitempty"`
	ReducedBytes         int     `json:"reduced_bytes,omitempty"`
	BytesSaved           int     `json:"bytes_saved,omitempty"`
	BytesSavedPercent    float64 `json:"bytes_saved_percent,omitempty"`
	RawTokens            int     `json:"raw_tokens,omitempty"`
	ReducedTokens        int     `json:"reduced_tokens,omitempty"`
	TokensSaved          int     `json:"tokens_saved,omitempty"`
	TokensSavedPercent   float64 `json:"tokens_saved_percent,omitempty"`
	RawDurationMS        int     `json:"raw_duration_ms,omitempty"`
	ReducedDurationMS    int     `json:"reduced_duration_ms,omitempty"`
	DurationSavedMS      int     `json:"duration_saved_ms,omitempty"`
	DurationSavedPercent float64 `json:"duration_saved_percent,omitempty"`
	RawCostUSD           float64 `json:"raw_estimated_input_cost_usd,omitempty"`
	ReducedCostUSD       float64 `json:"reduced_estimated_input_cost_usd,omitempty"`
	CostSavedUSD         float64 `json:"estimated_input_cost_saved_usd,omitempty"`
	CostSavedPercent     float64 `json:"estimated_input_cost_saved_percent,omitempty"`
	RawComparable        bool    `json:"raw_comparable"`
	RawError             string  `json:"raw_error,omitempty"`
	Error                string  `json:"error,omitempty"`
}

// ReportSummary aggregates many measured shell reduction results.
type ReportSummary struct {
	CaseCount                 int     `json:"case_count"`
	WeightedCaseCount         int     `json:"weighted_case_count"`
	OKCount                   int     `json:"ok_count"`
	ErrorCount                int     `json:"error_count"`
	OptionalUnavailableCount  int     `json:"optional_unavailable_count"`
	SkippedCount              int     `json:"skipped_count"`
	ComparableCaseCount       int     `json:"comparable_case_count"`
	NonComparableCaseCount    int     `json:"non_comparable_case_count"`
	TotalRawBytes             int     `json:"total_raw_bytes"`
	TotalReducedBytes         int     `json:"total_reduced_bytes"`
	TotalBytesSaved           int     `json:"total_bytes_saved"`
	TotalBytesSavedPercent    float64 `json:"total_bytes_saved_percent"`
	WeightedRawBytes          int     `json:"weighted_raw_bytes"`
	WeightedReducedBytes      int     `json:"weighted_reduced_bytes"`
	WeightedBytesSaved        int     `json:"weighted_bytes_saved"`
	WeightedBytesSavedPct     float64 `json:"weighted_bytes_saved_percent"`
	TotalRawTokens            int     `json:"total_raw_tokens"`
	TotalReducedTokens        int     `json:"total_reduced_tokens"`
	TotalTokensSaved          int     `json:"total_tokens_saved"`
	TotalTokensSavedPercent   float64 `json:"total_tokens_saved_percent"`
	WeightedRawTokens         int     `json:"weighted_raw_tokens"`
	WeightedReducedTokens     int     `json:"weighted_reduced_tokens"`
	WeightedTokensSaved       int     `json:"weighted_tokens_saved"`
	WeightedTokensSavedPct    float64 `json:"weighted_tokens_saved_percent"`
	TotalRawDurationMS        int     `json:"total_raw_duration_ms"`
	TotalReducedDurationMS    int     `json:"total_reduced_duration_ms"`
	TotalDurationSavedMS      int     `json:"total_duration_saved_ms"`
	TotalDurationSavedPercent float64 `json:"total_duration_saved_percent"`
	WeightedRawDurationMS     int     `json:"weighted_raw_duration_ms"`
	WeightedReducedDurationMS int     `json:"weighted_reduced_duration_ms"`
	WeightedDurationSavedMS   int     `json:"weighted_duration_saved_ms"`
	WeightedDurationSavedPct  float64 `json:"weighted_duration_saved_percent"`
	TotalRawCostUSD           float64 `json:"total_raw_estimated_input_cost_usd"`
	TotalReducedCostUSD       float64 `json:"total_reduced_estimated_input_cost_usd"`
	TotalCostSavedUSD         float64 `json:"total_estimated_input_cost_saved_usd"`
	TotalCostSavedPercent     float64 `json:"total_estimated_input_cost_saved_percent"`
	WeightedRawCostUSD        float64 `json:"weighted_raw_estimated_input_cost_usd"`
	WeightedReducedCostUSD    float64 `json:"weighted_reduced_estimated_input_cost_usd"`
	WeightedCostSavedUSD      float64 `json:"weighted_estimated_input_cost_saved_usd"`
	WeightedCostSavedPercent  float64 `json:"weighted_estimated_input_cost_saved_percent"`
	PreferReduceCount         int     `json:"prefer_reduce_count"`
	PreferKeepRawCount        int     `json:"prefer_keep_raw_count"`
	PreferEitherCount         int     `json:"prefer_either_count"`
	RawUnavailableCount       int     `json:"raw_unavailable_count"`
}

// BuildReportCase converts one successful shell command payload into a report row.
func BuildReportCase(command, operation string, weight int, data map[string]any) ReportCase {
	route := asMap(data["route"])
	measure := asMap(data["measure"])
	raw := asMap(measure["raw"])
	reduced := asMap(measure["reduced"])
	savings := asMap(measure["savings"])
	advice := asMap(measure["advice"])

	rawError := stringValue(raw["error"])
	comparable := rawError == ""

	return ReportCase{
		Command:              command,
		Operation:            operation,
		Weight:               normalizedWeight(weight),
		Status:               "ok",
		Intent:               stringValue(route["intent"]),
		Skill:                firstNonEmpty(stringValue(route["skill"]), stringValue(route["native"])),
		Summary:              stringValue(data["summary"]),
		MeasureSummary:       MeasureSummaryLine(measure),
		Recommendation:       stringValue(advice["mode"]),
		RecommendationWhy:    stringValue(advice["reason"]),
		RawBytes:             intValue(raw["combined_bytes"]),
		ReducedBytes:         intValue(reduced["bytes"]),
		BytesSaved:           intValue(savings["bytes_saved"]),
		BytesSavedPercent:    floatValue(savings["bytes_saved_percent"]),
		RawTokens:            intValue(raw["combined_tokens"]),
		ReducedTokens:        intValue(reduced["tokens"]),
		TokensSaved:          intValue(savings["tokens_saved"]),
		TokensSavedPercent:   floatValue(savings["tokens_saved_percent"]),
		RawDurationMS:        intValue(raw["duration_ms"]),
		ReducedDurationMS:    intValue(reduced["duration_ms"]),
		DurationSavedMS:      intValue(savings["duration_saved_ms"]),
		DurationSavedPercent: floatValue(savings["duration_saved_percent"]),
		RawCostUSD:           floatValue(raw["estimated_input_cost_usd"]),
		ReducedCostUSD:       floatValue(reduced["estimated_input_cost_usd"]),
		CostSavedUSD:         floatValue(savings["estimated_input_cost_saved_usd"]),
		CostSavedPercent:     floatValue(savings["estimated_input_cost_saved_percent"]),
		RawComparable:        comparable,
		RawError:             rawError,
	}
}

// ErrorReportCase captures a failed shell reduction case.
func ErrorReportCase(command, errText string) ReportCase {
	return ReportCase{
		Command: command,
		Weight:  1,
		Status:  "error",
		Error:   errText,
	}
}

// OptionalUnavailableCase captures an optional preset command that cannot be measured in this environment.
func OptionalUnavailableCase(command, operation string, weight int, errText string) ReportCase {
	return ReportCase{
		Command:   command,
		Operation: operation,
		Weight:    normalizedWeight(weight),
		Status:    "optional_unavailable",
		Error:     errText,
	}
}

// SkippedCase captures a deliberately skipped benchmark row.
func SkippedCase(operation string, weight int, reason string) ReportCase {
	return ReportCase{
		Operation: operation,
		Weight:    normalizedWeight(weight),
		Status:    "skipped",
		Error:     reason,
	}
}

// SummarizeReport aggregates comparable rows and counts failures explicitly.
func SummarizeReport(cases []ReportCase) ReportSummary {
	summary := ReportSummary{
		CaseCount: len(cases),
	}
	for _, item := range cases {
		weight := normalizedWeight(item.Weight)
		summary.WeightedCaseCount += weight
		if item.Status == "error" {
			summary.ErrorCount++
			continue
		}
		if item.Status == "optional_unavailable" {
			summary.OptionalUnavailableCount++
			summary.NonComparableCaseCount++
			continue
		}
		if item.Status == "skipped" {
			summary.SkippedCount++
			continue
		}
		summary.OKCount++
		if !item.RawComparable {
			summary.NonComparableCaseCount++
			if item.Recommendation == "raw_unavailable" {
				summary.RawUnavailableCount++
			}
			continue
		}
		switch item.Recommendation {
		case "reduce":
			summary.PreferReduceCount++
		case "keep_raw":
			summary.PreferKeepRawCount++
		default:
			summary.PreferEitherCount++
		}
		summary.ComparableCaseCount++
		summary.TotalRawBytes += item.RawBytes
		summary.TotalReducedBytes += item.ReducedBytes
		summary.TotalBytesSaved += item.BytesSaved
		summary.TotalRawTokens += item.RawTokens
		summary.TotalReducedTokens += item.ReducedTokens
		summary.TotalTokensSaved += item.TokensSaved
		summary.TotalRawDurationMS += item.RawDurationMS
		summary.TotalReducedDurationMS += item.ReducedDurationMS
		summary.TotalDurationSavedMS += item.DurationSavedMS
		summary.TotalRawCostUSD += item.RawCostUSD
		summary.TotalReducedCostUSD += item.ReducedCostUSD
		summary.TotalCostSavedUSD += item.CostSavedUSD
		summary.WeightedRawBytes += item.RawBytes * weight
		summary.WeightedReducedBytes += item.ReducedBytes * weight
		summary.WeightedBytesSaved += item.BytesSaved * weight
		summary.WeightedRawTokens += item.RawTokens * weight
		summary.WeightedReducedTokens += item.ReducedTokens * weight
		summary.WeightedTokensSaved += item.TokensSaved * weight
		summary.WeightedRawDurationMS += item.RawDurationMS * weight
		summary.WeightedReducedDurationMS += item.ReducedDurationMS * weight
		summary.WeightedDurationSavedMS += item.DurationSavedMS * weight
		summary.WeightedRawCostUSD += item.RawCostUSD * float64(weight)
		summary.WeightedReducedCostUSD += item.ReducedCostUSD * float64(weight)
		summary.WeightedCostSavedUSD += item.CostSavedUSD * float64(weight)
	}
	if summary.TotalRawBytes > 0 {
		summary.TotalBytesSavedPercent = percentSaved(summary.TotalRawBytes, summary.TotalReducedBytes)
	}
	if summary.TotalRawTokens > 0 {
		summary.TotalTokensSavedPercent = percentSaved(summary.TotalRawTokens, summary.TotalReducedTokens)
	}
	if summary.WeightedRawBytes > 0 {
		summary.WeightedBytesSavedPct = percentSaved(summary.WeightedRawBytes, summary.WeightedReducedBytes)
	}
	if summary.WeightedRawTokens > 0 {
		summary.WeightedTokensSavedPct = percentSaved(summary.WeightedRawTokens, summary.WeightedReducedTokens)
	}
	if summary.TotalRawDurationMS > 0 {
		summary.TotalDurationSavedPercent = percentSaved(summary.TotalRawDurationMS, summary.TotalReducedDurationMS)
	}
	if summary.WeightedRawDurationMS > 0 {
		summary.WeightedDurationSavedPct = percentSaved(summary.WeightedRawDurationMS, summary.WeightedReducedDurationMS)
	}
	if summary.TotalRawCostUSD > 0 {
		summary.TotalCostSavedPercent = percentSavedFloat(summary.TotalRawCostUSD, summary.TotalReducedCostUSD)
	}
	if summary.WeightedRawCostUSD > 0 {
		summary.WeightedCostSavedPercent = percentSavedFloat(summary.WeightedRawCostUSD, summary.WeightedReducedCostUSD)
	}
	return summary
}

func normalizedWeight(weight int) int {
	if weight <= 0 {
		return 1
	}
	return weight
}
