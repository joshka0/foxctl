package longcoteval

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type Comparison struct {
	Baseline  ConditionID `json:"baseline"`
	Candidate ConditionID `json:"candidate"`
}

type Summary struct {
	Conditions        []ConditionSummary  `json:"conditions"`
	Comparisons       []ComparisonSummary `json:"comparisons,omitempty"`
	DuplicateAttempts int                 `json:"duplicate_attempts,omitempty"`
}

type ConditionSummary struct {
	ConditionID                  ConditionID `json:"condition_id"`
	Attempts                     int         `json:"attempts"`
	TerminalAttempts             int         `json:"terminal_attempts"`
	VerifiedAttempts             int         `json:"verified_attempts"`
	CorrectAttempts              int         `json:"correct_attempts"`
	WrongFormatting              int         `json:"wrong_formatting"`
	LeakedAttempts               int         `json:"leaked_attempts"`
	ReviewAttempts               int         `json:"review_attempts,omitempty"`
	ReviewRecursiveRequested     int         `json:"review_recursive_requested,omitempty"`
	ReviewRecursiveUsed          int         `json:"review_recursive_used,omitempty"`
	ReviewFallbacks              int         `json:"review_fallbacks,omitempty"`
	ReviewCandidateCompactions   int         `json:"review_candidate_compactions,omitempty"`
	OutputSanitizations          int         `json:"output_sanitizations,omitempty"`
	PreReviewOutputSanitizations int         `json:"pre_review_output_sanitizations,omitempty"`
	ChildSummariesTruncated      int         `json:"child_summaries_truncated,omitempty"`
	ChildSummariesRewritten      int         `json:"child_summaries_rewritten,omitempty"`
	MeanTotalTokens              float64     `json:"mean_total_tokens"`
	MeanBaseTokens               float64     `json:"mean_base_tokens,omitempty"`
	MeanReviewTokens             float64     `json:"mean_review_tokens,omitempty"`
	MeanCostUSD                  float64     `json:"mean_cost_usd"`
	MeanDurationMS               float64     `json:"mean_duration_ms"`
}

type ComparisonSummary struct {
	Baseline          ConditionID `json:"baseline"`
	Candidate         ConditionID `json:"candidate"`
	Pairs             int         `json:"pairs"`
	Wins              int         `json:"wins"`
	Losses            int         `json:"losses"`
	TieCorrect        int         `json:"tie_correct"`
	TieIncorrect      int         `json:"tie_incorrect"`
	MeanTokenDelta    float64     `json:"mean_token_delta"`
	MeanCostDeltaUSD  float64     `json:"mean_cost_delta_usd"`
	MeanDurationDelta float64     `json:"mean_duration_delta_ms"`
}

// Summarize aggregates attempts by condition and paired comparison. Duplicate
// (question, condition) attempts keep the last occurrence by input order and
// increment DuplicateAttempts.
func Summarize(attempts []Attempt, comparisons []Comparison) Summary {
	latest := map[string]Attempt{}
	duplicates := 0
	for _, attempt := range attempts {
		key := attemptKey(attempt.QuestionID, attempt.ConditionID)
		if _, exists := latest[key]; exists {
			duplicates++
		}
		latest[key] = attempt
	}

	conditionIDs := make([]ConditionID, 0)
	conditionSeen := map[ConditionID]struct{}{}
	for _, attempt := range latest {
		if _, ok := conditionSeen[attempt.ConditionID]; ok {
			continue
		}
		conditionSeen[attempt.ConditionID] = struct{}{}
		conditionIDs = append(conditionIDs, attempt.ConditionID)
	}
	sort.Slice(conditionIDs, func(i, j int) bool { return conditionIDs[i] < conditionIDs[j] })

	summary := Summary{DuplicateAttempts: duplicates}
	for _, conditionID := range conditionIDs {
		summary.Conditions = append(summary.Conditions, summarizeCondition(latest, conditionID))
	}
	for _, comparison := range comparisons {
		summary.Comparisons = append(summary.Comparisons, summarizeComparison(latest, comparison))
	}
	return summary
}

func summarizeCondition(attempts map[string]Attempt, conditionID ConditionID) ConditionSummary {
	var out ConditionSummary
	out.ConditionID = conditionID
	var tokenSum, baseTokenSum, reviewTokenSum, durationSum int64
	var costSum float64
	for _, attempt := range attempts {
		if attempt.ConditionID != conditionID {
			continue
		}
		out.Attempts++
		if attempt.IsTerminal() {
			out.TerminalAttempts++
		}
		if attempt.VerifierStatus != "" || attempt.Correct || attempt.WrongFormatting {
			out.VerifiedAttempts++
		}
		if attempt.IsCorrect() {
			out.CorrectAttempts++
		}
		if attempt.WrongFormatting || attempt.VerifierStatus == VerifierStatusWrongFormatting {
			out.WrongFormatting++
		}
		if attempt.Status == AttemptStatusLeaked || attempt.LeakageFlags.Leaked() {
			out.LeakedAttempts++
		}
		tokenSum += int64(attempt.Usage.TotalTokens)
		reviewMetrics := attemptReviewMetrics(attempt)
		out.ReviewAttempts += reviewMetrics.reviewAttempts
		out.ReviewRecursiveRequested += reviewMetrics.recursiveRequested
		out.ReviewRecursiveUsed += reviewMetrics.recursiveUsed
		out.ReviewFallbacks += reviewMetrics.fallbacks
		out.ReviewCandidateCompactions += reviewMetrics.candidateCompactions
		out.OutputSanitizations += reviewMetrics.outputSanitizations
		out.PreReviewOutputSanitizations += reviewMetrics.preReviewOutputSanitizations
		out.ChildSummariesTruncated += reviewMetrics.childSummariesTruncated
		out.ChildSummariesRewritten += reviewMetrics.childSummariesRewritten
		baseTokenSum += int64(reviewMetrics.baseTokens)
		reviewTokenSum += int64(reviewMetrics.reviewTokens)
		costSum += attempt.Usage.TotalCostUSD
		durationSum += attempt.DurationMS
	}
	if out.Attempts > 0 {
		scale := float64(out.Attempts)
		out.MeanTotalTokens = float64(tokenSum) / scale
		out.MeanBaseTokens = float64(baseTokenSum) / scale
		out.MeanReviewTokens = float64(reviewTokenSum) / scale
		out.MeanCostUSD = costSum / scale
		out.MeanDurationMS = float64(durationSum) / scale
	}
	return out
}

type reviewSummaryMetrics struct {
	reviewAttempts               int
	recursiveRequested           int
	recursiveUsed                int
	fallbacks                    int
	candidateCompactions         int
	outputSanitizations          int
	preReviewOutputSanitizations int
	childSummariesTruncated      int
	childSummariesRewritten      int
	baseTokens                   int
	reviewTokens                 int
}

func attemptReviewMetrics(attempt Attempt) reviewSummaryMetrics {
	if attempt.RLM == nil {
		return reviewSummaryMetrics{baseTokens: attempt.Usage.TotalTokens}
	}
	meta := anyMap(attempt.RLM.Metadata)
	baseTokens := firstPositiveSummaryInt(
		summaryInt(meta["parent_total_tokens"])+summaryInt(meta["child_total_tokens"]),
		attempt.Usage.TotalTokens,
	)
	out := reviewSummaryMetrics{baseTokens: baseTokens}
	if changedMap(meta["output_sanitization"]) {
		out.outputSanitizations++
	}
	if changedMap(meta["pre_review_output_sanitization"]) {
		out.preReviewOutputSanitizations++
	}

	review := anyMap(meta["review"])
	if len(review) == 0 {
		return out
	}
	out.reviewAttempts = 1
	out.reviewTokens = summaryInt(review["parent_total_tokens"]) + summaryInt(review["child_total_tokens"])
	if out.reviewTokens == 0 && attempt.Usage.TotalTokens > baseTokens {
		out.reviewTokens = attempt.Usage.TotalTokens - baseTokens
	}
	if attempt.Usage.TotalTokens > 0 && out.baseTokens+out.reviewTokens > attempt.Usage.TotalTokens {
		out.reviewTokens = attempt.Usage.TotalTokens - out.baseTokens
		if out.reviewTokens < 0 {
			out.reviewTokens = 0
		}
	}
	if summaryBool(review["review_recursive_requested"]) {
		out.recursiveRequested = 1
	}
	if summaryBool(review["review_recursive_used"]) {
		out.recursiveUsed = 1
	}
	if strings.TrimSpace(summaryString(review["review_fallback"])) != "" {
		out.fallbacks = 1
	}
	if changedMap(review["review_candidate_compaction"]) {
		out.candidateCompactions = 1
	}
	if changedMap(review["output_sanitization"]) {
		out.outputSanitizations++
	}
	traceMetrics := recursiveTraceSummaryMetrics(review["recursive_trace"])
	out.childSummariesTruncated += traceMetrics.truncated
	out.childSummariesRewritten += traceMetrics.rewritten
	return out
}

type traceSummaryMetrics struct {
	truncated int
	rewritten int
}

func recursiveTraceSummaryMetrics(value any) traceSummaryMetrics {
	trace := anyMap(value)
	if len(trace) == 0 {
		return traceSummaryMetrics{}
	}
	children, _ := trace["children"].([]any)
	var out traceSummaryMetrics
	for _, child := range children {
		childMap := anyMap(child)
		if len(childMap) == 0 {
			continue
		}
		if summaryBool(childMap["summary_truncated"]) {
			out.truncated++
		}
		if strings.EqualFold(summaryString(childMap["summary_compaction_method"]), "rewrite") ||
			summaryBool(childMap["summary_rewrite_used"]) {
			out.rewritten++
		}
		nested := recursiveTraceSummaryMetrics(map[string]any{"children": childMap["children"]})
		out.truncated += nested.truncated
		out.rewritten += nested.rewritten
		if internal := childMap["internal_trace"]; internal != nil {
			nested = recursiveTraceSummaryMetrics(internal)
			out.truncated += nested.truncated
			out.rewritten += nested.rewritten
		}
	}
	return out
}

func summarizeComparison(attempts map[string]Attempt, comparison Comparison) ComparisonSummary {
	out := ComparisonSummary{Baseline: comparison.Baseline, Candidate: comparison.Candidate}
	questionIDs := map[string]struct{}{}
	for _, attempt := range attempts {
		if attempt.ConditionID == comparison.Baseline || attempt.ConditionID == comparison.Candidate {
			questionIDs[attempt.QuestionID] = struct{}{}
		}
	}
	ids := make([]string, 0, len(questionIDs))
	for id := range questionIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var tokenDelta, durationDelta int64
	var costDelta float64
	for _, questionID := range ids {
		baseline, okBaseline := attempts[attemptKey(questionID, comparison.Baseline)]
		candidate, okCandidate := attempts[attemptKey(questionID, comparison.Candidate)]
		if !okBaseline || !okCandidate || !baseline.IsTerminal() || !candidate.IsTerminal() {
			continue
		}
		out.Pairs++
		baselineCorrect := baseline.IsCorrect()
		candidateCorrect := candidate.IsCorrect()
		switch {
		case !baselineCorrect && candidateCorrect:
			out.Wins++
		case baselineCorrect && !candidateCorrect:
			out.Losses++
		case baselineCorrect && candidateCorrect:
			out.TieCorrect++
		default:
			out.TieIncorrect++
		}
		tokenDelta += int64(candidate.Usage.TotalTokens - baseline.Usage.TotalTokens)
		costDelta += candidate.Usage.TotalCostUSD - baseline.Usage.TotalCostUSD
		durationDelta += candidate.DurationMS - baseline.DurationMS
	}
	if out.Pairs > 0 {
		scale := float64(out.Pairs)
		out.MeanTokenDelta = float64(tokenDelta) / scale
		out.MeanCostDeltaUSD = costDelta / scale
		out.MeanDurationDelta = float64(durationDelta) / scale
	}
	return out
}

func anyMap(value any) map[string]any {
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]any:
		return typed
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil
		}
		return out
	}
}

func changedMap(value any) bool {
	values := anyMap(value)
	return len(values) > 0 && summaryBool(values["changed"])
}

func summaryBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func summaryInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	default:
		return 0
	}
}

func summaryString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func firstPositiveSummaryInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func attemptKey(questionID string, conditionID ConditionID) string {
	return strings.TrimSpace(questionID) + "\x00" + string(conditionID)
}
