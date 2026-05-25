package optimization_test

import (
	"math"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/agent/optimization"
)

func TestPromptJudgeScoresTargetAlignedOutputHigherThanGeneric(t *testing.T) {
	t.Parallel()

	judge := optimization.DefaultPromptJudge()
	input := optimization.PromptJudgeInput{
		Question:       "I applied the changes, can you review",
		TargetResponse: "Ignore formatter churn and review only semantic changes.",
	}

	good := input
	good.Output = "Ignore formatter churn and review only semantic changes."
	bad := input
	bad.Output = "Please share the file or code snippet you'd like reviewed."

	if judge.Score(good) <= judge.Score(bad) {
		t.Fatalf("expected target-aligned output to outscore generic request")
	}
}

func TestPromptJudgeUsesQueryWhenTargetMissing(t *testing.T) {
	t.Parallel()

	judge := optimization.DefaultPromptJudge()
	good := optimization.PromptJudgeInput{
		Question: "Theres a hook error PreToolUse Bash hook error",
		Context:  "Strict mode is failing on Bash PreToolUse hooks.",
		Output:   "Read the installed hook file directly and diagnose the Bash strict-mode failure without invoking it again.",
	}
	bad := optimization.PromptJudgeInput{
		Question: good.Question,
		Context:  good.Context,
		Output:   "Please provide more details about the issue.",
	}

	if judge.Score(good) <= judge.Score(bad) {
		t.Fatalf("expected query-aligned answer to outscore generic fallback")
	}
}

func TestPromptJudgePenalizesExcessiveLength(t *testing.T) {
	t.Parallel()

	judge := optimization.DefaultPromptJudge()
	concise := optimization.PromptJudgeInput{
		Question:       "Lets do a small reindex first before the full one",
		TargetResponse: "Build first, then run a targeted reindex on a small package before the full workspace rebuild.",
		Output:         "Build first, then run a targeted reindex on a small package before the full workspace rebuild.",
	}
	verbose := concise
	verbose.Output = concise.Output + " Then explain every step in detail, add extra commentary, and repeat the plan with more background context and additional narrative about why this is safer."

	if judge.Score(concise) <= judge.Score(verbose) {
		t.Fatalf("expected concise answer to outscore overly long version")
	}
}

func TestPromptJudgeWeightsNormalizePropertyProducesValidWeights(t *testing.T) {
	t.Parallel()

	property := func(targetSimilarity, querySimilarity, lengthQuality uint16) bool {
		weights := optimization.PromptJudgeWeights{
			TargetSimilarity: float64(targetSimilarity) + 1,
			QuerySimilarity:  float64(querySimilarity) + 1,
			LengthQuality:    float64(lengthQuality) + 1,
		}

		weights.Normalize()

		sum := weights.TargetSimilarity + weights.QuerySimilarity + weights.LengthQuality
		return math.Abs(sum-1.0) <= 1e-12 &&
			finiteNonNegative(weights.TargetSimilarity) &&
			finiteNonNegative(weights.QuerySimilarity) &&
			finiteNonNegative(weights.LengthQuality)
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatalf("PromptJudgeWeights.Normalize property failed: %v", err)
	}
}

func TestPromptJudgeInvalidWeightsReturnFiniteBoundedScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		weights optimization.PromptJudgeWeights
	}{
		{
			name: "nan target weight",
			weights: optimization.PromptJudgeWeights{
				TargetSimilarity: math.NaN(),
				QuerySimilarity:  0.35,
				LengthQuality:    0.10,
			},
		},
		{
			name: "infinite query weight",
			weights: optimization.PromptJudgeWeights{
				TargetSimilarity: 0.55,
				QuerySimilarity:  math.Inf(1),
				LengthQuality:    0.10,
			},
		},
		{
			name: "negative weights",
			weights: optimization.PromptJudgeWeights{
				TargetSimilarity: -1,
				QuerySimilarity:  -2,
				LengthQuality:    -3,
			},
		},
	}

	input := optimization.PromptJudgeInput{
		Question:       "I applied the changes, can you review",
		TargetResponse: "Ignore formatter churn and review only semantic changes.",
		Output:         "Ignore formatter churn and review only semantic changes.",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := optimization.NewPromptJudge(tt.weights).Evaluate(input)

			if !finiteBoundedScore(result.Score) {
				t.Fatalf("Score = %v, want finite score in [0,1]", result.Score)
			}
			if !finiteBoundedScore(result.TargetSimilarity) {
				t.Fatalf("TargetSimilarity = %v, want finite score in [0,1]", result.TargetSimilarity)
			}
			if !finiteBoundedScore(result.QuerySimilarity) {
				t.Fatalf("QuerySimilarity = %v, want finite score in [0,1]", result.QuerySimilarity)
			}
			if !finiteBoundedScore(result.LengthQuality) {
				t.Fatalf("LengthQuality = %v, want finite score in [0,1]", result.LengthQuality)
			}
			if !finiteBoundedScore(result.GenericPenalty) {
				t.Fatalf("GenericPenalty = %v, want finite score in [0,1]", result.GenericPenalty)
			}
		})
	}
}

func finiteBoundedScore(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}
