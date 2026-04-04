package shellreduce

// ReportCase captures one measured shell reduction result.
type ReportCase struct {
	Command            string  `json:"command"`
	Operation          string  `json:"operation,omitempty"`
	Weight             int     `json:"weight,omitempty"`
	Status             string  `json:"status"`
	Intent             string  `json:"intent,omitempty"`
	Skill              string  `json:"skill,omitempty"`
	Summary            string  `json:"summary,omitempty"`
	MeasureSummary     string  `json:"measure_summary,omitempty"`
	Recommendation     string  `json:"recommendation,omitempty"`
	RecommendationWhy  string  `json:"recommendation_reason,omitempty"`
	RawBytes           int     `json:"raw_bytes,omitempty"`
	ReducedBytes       int     `json:"reduced_bytes,omitempty"`
	BytesSaved         int     `json:"bytes_saved,omitempty"`
	BytesSavedPercent  float64 `json:"bytes_saved_percent,omitempty"`
	RawTokens          int     `json:"raw_tokens,omitempty"`
	ReducedTokens      int     `json:"reduced_tokens,omitempty"`
	TokensSaved        int     `json:"tokens_saved,omitempty"`
	TokensSavedPercent float64 `json:"tokens_saved_percent,omitempty"`
	RawComparable      bool    `json:"raw_comparable"`
	RawError           string  `json:"raw_error,omitempty"`
	Error              string  `json:"error,omitempty"`
}

// ReportSummary aggregates many measured shell reduction results.
type ReportSummary struct {
	CaseCount                int     `json:"case_count"`
	WeightedCaseCount        int     `json:"weighted_case_count"`
	OKCount                  int     `json:"ok_count"`
	ErrorCount               int     `json:"error_count"`
	OptionalUnavailableCount int     `json:"optional_unavailable_count"`
	SkippedCount             int     `json:"skipped_count"`
	ComparableCaseCount      int     `json:"comparable_case_count"`
	NonComparableCaseCount   int     `json:"non_comparable_case_count"`
	TotalRawBytes            int     `json:"total_raw_bytes"`
	TotalReducedBytes        int     `json:"total_reduced_bytes"`
	TotalBytesSaved          int     `json:"total_bytes_saved"`
	TotalBytesSavedPercent   float64 `json:"total_bytes_saved_percent"`
	WeightedRawBytes         int     `json:"weighted_raw_bytes"`
	WeightedReducedBytes     int     `json:"weighted_reduced_bytes"`
	WeightedBytesSaved       int     `json:"weighted_bytes_saved"`
	WeightedBytesSavedPct    float64 `json:"weighted_bytes_saved_percent"`
	TotalRawTokens           int     `json:"total_raw_tokens"`
	TotalReducedTokens       int     `json:"total_reduced_tokens"`
	TotalTokensSaved         int     `json:"total_tokens_saved"`
	TotalTokensSavedPercent  float64 `json:"total_tokens_saved_percent"`
	WeightedRawTokens        int     `json:"weighted_raw_tokens"`
	WeightedReducedTokens    int     `json:"weighted_reduced_tokens"`
	WeightedTokensSaved      int     `json:"weighted_tokens_saved"`
	WeightedTokensSavedPct   float64 `json:"weighted_tokens_saved_percent"`
	PreferReduceCount        int     `json:"prefer_reduce_count"`
	PreferKeepRawCount       int     `json:"prefer_keep_raw_count"`
	PreferEitherCount        int     `json:"prefer_either_count"`
	RawUnavailableCount      int     `json:"raw_unavailable_count"`
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
		Command:            command,
		Operation:          operation,
		Weight:             normalizedWeight(weight),
		Status:             "ok",
		Intent:             stringValue(route["intent"]),
		Skill:              firstNonEmpty(stringValue(route["skill"]), stringValue(route["native"])),
		Summary:            stringValue(data["summary"]),
		MeasureSummary:     MeasureSummaryLine(measure),
		Recommendation:     stringValue(advice["mode"]),
		RecommendationWhy:  stringValue(advice["reason"]),
		RawBytes:           intValue(raw["combined_bytes"]),
		ReducedBytes:       intValue(reduced["bytes"]),
		BytesSaved:         intValue(savings["bytes_saved"]),
		BytesSavedPercent:  floatValue(savings["bytes_saved_percent"]),
		RawTokens:          intValue(raw["combined_tokens"]),
		ReducedTokens:      intValue(reduced["tokens"]),
		TokensSaved:        intValue(savings["tokens_saved"]),
		TokensSavedPercent: floatValue(savings["tokens_saved_percent"]),
		RawComparable:      comparable,
		RawError:           rawError,
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
		summary.WeightedRawBytes += item.RawBytes * weight
		summary.WeightedReducedBytes += item.ReducedBytes * weight
		summary.WeightedBytesSaved += item.BytesSaved * weight
		summary.WeightedRawTokens += item.RawTokens * weight
		summary.WeightedReducedTokens += item.ReducedTokens * weight
		summary.WeightedTokensSaved += item.TokensSaved * weight
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
	return summary
}

func normalizedWeight(weight int) int {
	if weight <= 0 {
		return 1
	}
	return weight
}
