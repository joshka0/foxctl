package longcoteval

import (
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
	ConditionID      ConditionID `json:"condition_id"`
	Attempts         int         `json:"attempts"`
	TerminalAttempts int         `json:"terminal_attempts"`
	VerifiedAttempts int         `json:"verified_attempts"`
	CorrectAttempts  int         `json:"correct_attempts"`
	WrongFormatting  int         `json:"wrong_formatting"`
	LeakedAttempts   int         `json:"leaked_attempts"`
	MeanTotalTokens  float64     `json:"mean_total_tokens"`
	MeanCostUSD      float64     `json:"mean_cost_usd"`
	MeanDurationMS   float64     `json:"mean_duration_ms"`
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
	var tokenSum, durationSum int64
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
		costSum += attempt.Usage.TotalCostUSD
		durationSum += attempt.DurationMS
	}
	if out.Attempts > 0 {
		scale := float64(out.Attempts)
		out.MeanTotalTokens = float64(tokenSum) / scale
		out.MeanCostUSD = costSum / scale
		out.MeanDurationMS = float64(durationSum) / scale
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

func attemptKey(questionID string, conditionID ConditionID) string {
	return strings.TrimSpace(questionID) + "\x00" + string(conditionID)
}
