package optimization

import (
	"strings"
	"unicode"
)

// PromptJudgeInput is one output-scoring request.
type PromptJudgeInput struct {
	Question       string
	Context        string
	TargetResponse string
	Output         string
}

// PromptJudgeWeights controls heuristic output scoring.
type PromptJudgeWeights struct {
	TargetSimilarity float64
	QuerySimilarity  float64
	LengthQuality    float64
}

// Normalize adjusts weights to sum to 1 when positive.
func (w *PromptJudgeWeights) Normalize() {
	sum := w.TargetSimilarity + w.QuerySimilarity + w.LengthQuality
	if sum <= 0 {
		return
	}
	w.TargetSimilarity /= sum
	w.QuerySimilarity /= sum
	w.LengthQuality /= sum
}

// DefaultPromptJudgeWeights returns the initial heuristic weighting.
func DefaultPromptJudgeWeights() PromptJudgeWeights {
	w := PromptJudgeWeights{
		TargetSimilarity: 0.55,
		QuerySimilarity:  0.35,
		LengthQuality:    0.10,
	}
	w.Normalize()
	return w
}

// PromptJudge scores candidate outputs for eval cases.
type PromptJudge struct {
	Weights PromptJudgeWeights
}

// PromptJudgeResult is a scored breakdown for one output.
type PromptJudgeResult struct {
	Score            float64
	TargetSimilarity float64
	QuerySimilarity  float64
	LengthQuality    float64
	GenericPenalty   float64
}

// NewPromptJudge creates a prompt-output judge.
func NewPromptJudge(weights PromptJudgeWeights) *PromptJudge {
	weights.Normalize()
	return &PromptJudge{Weights: weights}
}

// DefaultPromptJudge returns the default heuristic judge.
func DefaultPromptJudge() *PromptJudge {
	return NewPromptJudge(DefaultPromptJudgeWeights())
}

// Evaluate returns a normalized score and subscore breakdown in [0,1].
func (j *PromptJudge) Evaluate(in PromptJudgeInput) PromptJudgeResult {
	output := strings.TrimSpace(in.Output)
	if output == "" {
		return PromptJudgeResult{}
	}

	weights := j.Weights
	targetScore := 0.0
	queryScore := 0.0
	lengthScore := promptJudgeLengthQuality(output)

	targetText := strings.TrimSpace(in.TargetResponse)
	if targetText != "" {
		targetScore = promptJudgeTokenF1(output, targetText)
	} else {
		weights.TargetSimilarity = 0
	}

	queryText := strings.TrimSpace(strings.TrimSpace(in.Question) + " " + strings.TrimSpace(in.Context))
	if queryText != "" {
		queryScore = promptJudgeTokenF1(output, queryText)
	} else {
		weights.QuerySimilarity = 0
	}

	weights.Normalize()
	genericPenalty := promptJudgeGenericPenalty(output, targetText)
	score := weights.TargetSimilarity*targetScore +
		weights.QuerySimilarity*queryScore +
		weights.LengthQuality*lengthScore

	score *= genericPenalty
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return PromptJudgeResult{
		Score:            score,
		TargetSimilarity: targetScore,
		QuerySimilarity:  queryScore,
		LengthQuality:    lengthScore,
		GenericPenalty:   genericPenalty,
	}
}

// Score returns a normalized score in [0,1].
func (j *PromptJudge) Score(in PromptJudgeInput) float64 {
	return j.Evaluate(in).Score
}

func promptJudgeLengthQuality(output string) float64 {
	n := len(promptJudgeTokens(output))
	switch {
	case n == 0:
		return 0
	case n <= 24:
		return 1.0
	case n <= 48:
		return 0.9
	case n <= 80:
		return 0.75
	case n <= 128:
		return 0.6
	default:
		return 0.45
	}
}

func promptJudgeGenericPenalty(output, target string) float64 {
	lower := strings.ToLower(strings.TrimSpace(output))
	if target != "" {
		generic := []string{
			"please share the file",
			"please share the code",
			"please provide more details",
			"let me know if",
			"would you like me to",
		}
		for _, phrase := range generic {
			if strings.Contains(lower, phrase) {
				return 0.55
			}
		}
	}
	if strings.Contains(lower, "let me ") {
		return 0.92
	}
	return 1.0
}

func promptJudgeTokenF1(actual, expected string) float64 {
	actualSet := promptJudgeTokenSet(actual)
	expectedSet := promptJudgeTokenSet(expected)
	if len(actualSet) == 0 || len(expectedSet) == 0 {
		return 0
	}
	shared := 0
	for token := range actualSet {
		if _, ok := expectedSet[token]; ok {
			shared++
		}
	}
	precision := float64(shared) / float64(len(actualSet))
	recall := float64(shared) / float64(len(expectedSet))
	if precision == 0 || recall == 0 {
		return 0
	}
	return 2 * precision * recall / (precision + recall)
}

func promptJudgeTokens(text string) []string {
	shortStopwords := map[string]struct{}{
		"a":   {},
		"an":  {},
		"as":  {},
		"at":  {},
		"by":  {},
		"do":  {},
		"for": {},
		"if":  {},
		"in":  {},
		"is":  {},
		"it":  {},
		"of":  {},
		"on":  {},
		"or":  {},
		"the": {},
		"to":  {},
	}
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r == '_' || r == '-' || ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') || unicode.IsLetter(r) || unicode.IsNumber(r))
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, "_-")
		if field == "" {
			continue
		}
		if _, ok := shortStopwords[field]; ok {
			continue
		}
		out = append(out, field)
	}
	return out
}

func promptJudgeTokenSet(text string) map[string]struct{} {
	tokens := promptJudgeTokens(text)
	set := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		set[token] = struct{}{}
	}
	return set
}
