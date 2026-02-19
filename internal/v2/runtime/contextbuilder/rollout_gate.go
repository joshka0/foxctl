package contextbuilder

import "strings"

// SemanticRolloutGateThresholds defines rollout thresholds for semantic retrieval quality.
type SemanticRolloutGateThresholds struct {
	// MinFallbackInvariantPassRate is the minimum required pass rate for fallback corpus invariants.
	MinFallbackInvariantPassRate float64 `json:"min_fallback_invariant_pass_rate"`
	// MinVectorFallbackOverlapAtK is the minimum weighted overlap@K threshold across validation corpus.
	MinVectorFallbackOverlapAtK float64 `json:"min_vector_fallback_overlap_at_k"`
	// MaxFallbackRatio is the maximum allowed fallback ratio when vector capability is expected.
	MaxFallbackRatio float64 `json:"max_fallback_ratio"`
	// OverlapTopK is the top-K depth used for vector/fallback overlap comparison.
	OverlapTopK int `json:"overlap_top_k"`
}

// DefaultSemanticRolloutGateThresholds returns the PR-18 rollout thresholds.
func DefaultSemanticRolloutGateThresholds() SemanticRolloutGateThresholds {
	return SemanticRolloutGateThresholds{
		MinFallbackInvariantPassRate: 1.0,
		MinVectorFallbackOverlapAtK:  0.9,
		MaxFallbackRatio:             0.05,
		OverlapTopK:                  10,
	}
}

// SemanticValidationCase captures one corpus case used for rollout gate checks.
type SemanticValidationCase struct {
	ID string `json:"id,omitempty"`

	DeterministicOrdering bool `json:"deterministic_ordering"`
	StableRefs            bool `json:"stable_refs"`
	RequiredTemporalBlock bool `json:"required_temporal_block"`

	VectorArtifactRefs   []string `json:"vector_artifact_refs,omitempty"`
	FallbackArtifactRefs []string `json:"fallback_artifact_refs,omitempty"`
}

// SemanticValidationCaseResult is the computed result for one validation case.
type SemanticValidationCaseResult struct {
	ID                    string  `json:"id,omitempty"`
	FallbackInvariantPass bool    `json:"fallback_invariant_pass"`
	ExpectedAtK           int     `json:"expected_at_k"`
	OverlapAtK            float64 `json:"overlap_at_k"`
}

// SemanticRolloutGateInput is the input to rollout gate evaluation.
type SemanticRolloutGateInput struct {
	Cases                    []SemanticValidationCase `json:"cases,omitempty"`
	Stats                    ArtifactSearchStats      `json:"stats"`
	VectorCapabilityExpected bool                     `json:"vector_capability_expected"`
	Thresholds               SemanticRolloutGateThresholds
}

// SemanticRolloutGateChecks indicates pass/fail per rollout condition.
type SemanticRolloutGateChecks struct {
	FallbackInvariantPassRate bool `json:"fallback_invariant_pass_rate"`
	VectorFallbackOverlapAtK  bool `json:"vector_fallback_overlap_at_k"`
	FallbackRatio             bool `json:"fallback_ratio"`
}

// SemanticRolloutGateResult is the deterministic output for rollout gate checks.
type SemanticRolloutGateResult struct {
	Passed bool `json:"passed"`

	FallbackInvariantPassRate float64 `json:"fallback_invariant_pass_rate"`
	VectorFallbackOverlapAtK  float64 `json:"vector_fallback_overlap_at_k"`
	FallbackRatio             float64 `json:"fallback_ratio"`

	Checks       SemanticRolloutGateChecks      `json:"checks"`
	FailedChecks []string                       `json:"failed_checks,omitempty"`
	CaseResults  []SemanticValidationCaseResult `json:"case_results,omitempty"`
}

// EvaluateSemanticRolloutGate applies PR-18 rollout thresholds against corpus cases and stats.
func EvaluateSemanticRolloutGate(input SemanticRolloutGateInput) SemanticRolloutGateResult {
	thresholds := normalizeRolloutThresholds(input.Thresholds)

	caseResults := make([]SemanticValidationCaseResult, 0, len(input.Cases))
	passCount := 0
	totalExpected := 0
	totalOverlap := 0

	for _, c := range input.Cases {
		invariantPass := c.DeterministicOrdering && c.StableRefs && c.RequiredTemporalBlock
		if invariantPass {
			passCount++
		}

		expected, overlapCount := overlapAtK(c.VectorArtifactRefs, c.FallbackArtifactRefs, thresholds.OverlapTopK)
		overlapRate := 1.0
		if expected > 0 {
			overlapRate = float64(overlapCount) / float64(expected)
		}
		totalExpected += expected
		totalOverlap += overlapCount

		caseResults = append(caseResults, SemanticValidationCaseResult{
			ID:                    strings.TrimSpace(c.ID),
			FallbackInvariantPass: invariantPass,
			ExpectedAtK:           expected,
			OverlapAtK:            overlapRate,
		})
	}

	fallbackInvariantPassRate := 0.0
	if len(input.Cases) > 0 {
		fallbackInvariantPassRate = float64(passCount) / float64(len(input.Cases))
	}

	vectorFallbackOverlapAtK := 0.0
	if totalExpected > 0 {
		vectorFallbackOverlapAtK = float64(totalOverlap) / float64(totalExpected)
	}

	fallbackRatio := 0.0
	vectorPlusFallback := input.Stats.VectorCalls + input.Stats.FallbackCalls
	if vectorPlusFallback > 0 {
		fallbackRatio = float64(input.Stats.FallbackCalls) / float64(vectorPlusFallback)
	}

	checks := SemanticRolloutGateChecks{
		FallbackInvariantPassRate: fallbackInvariantPassRate >= thresholds.MinFallbackInvariantPassRate,
		VectorFallbackOverlapAtK:  vectorFallbackOverlapAtK >= thresholds.MinVectorFallbackOverlapAtK,
		FallbackRatio:             !input.VectorCapabilityExpected || fallbackRatio <= thresholds.MaxFallbackRatio,
	}

	failed := make([]string, 0, 3)
	if !checks.FallbackInvariantPassRate {
		failed = append(failed, "fallback_invariant_pass_rate")
	}
	if !checks.VectorFallbackOverlapAtK {
		failed = append(failed, "vector_fallback_overlap_at_k")
	}
	if !checks.FallbackRatio {
		failed = append(failed, "fallback_ratio")
	}

	return SemanticRolloutGateResult{
		Passed:                    checks.FallbackInvariantPassRate && checks.VectorFallbackOverlapAtK && checks.FallbackRatio,
		FallbackInvariantPassRate: fallbackInvariantPassRate,
		VectorFallbackOverlapAtK:  vectorFallbackOverlapAtK,
		FallbackRatio:             fallbackRatio,
		Checks:                    checks,
		FailedChecks:              failed,
		CaseResults:               caseResults,
	}
}

func normalizeRolloutThresholds(t SemanticRolloutGateThresholds) SemanticRolloutGateThresholds {
	if t.MinFallbackInvariantPassRate <= 0 {
		t.MinFallbackInvariantPassRate = 1.0
	}
	if t.MinVectorFallbackOverlapAtK <= 0 {
		t.MinVectorFallbackOverlapAtK = 0.9
	}
	if t.MaxFallbackRatio <= 0 {
		t.MaxFallbackRatio = 0.05
	}
	if t.OverlapTopK <= 0 {
		t.OverlapTopK = 10
	}
	return t
}

func overlapAtK(vectorRefs []string, fallbackRefs []string, k int) (expected int, overlap int) {
	vectorNorm := normalizeRefSet(vectorRefs, k)
	fallbackNorm := normalizeRefSet(fallbackRefs, k)

	expected = len(vectorNorm)
	if expected == 0 {
		return 0, 0
	}

	fallbackSet := make(map[string]struct{}, len(fallbackNorm))
	for _, ref := range fallbackNorm {
		fallbackSet[ref] = struct{}{}
	}
	for _, ref := range vectorNorm {
		if _, ok := fallbackSet[ref]; ok {
			overlap++
		}
	}
	return expected, overlap
}

func normalizeRefSet(in []string, k int) []string {
	if k <= 0 {
		return nil
	}
	out := make([]string, 0, min(k, len(in)))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		ref := strings.TrimSpace(raw)
		if ref == "" {
			continue
		}
		if _, dup := seen[ref]; dup {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
		if len(out) == k {
			break
		}
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
