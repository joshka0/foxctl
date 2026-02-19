package contextbuilder

import (
	"slices"
	"testing"
)

func TestEvaluateSemanticRolloutGate_Pass(t *testing.T) {
	t.Parallel()

	input := SemanticRolloutGateInput{
		Cases: []SemanticValidationCase{
			{
				ID:                    "case-1",
				DeterministicOrdering: true,
				StableRefs:            true,
				RequiredTemporalBlock: true,
				VectorArtifactRefs: []string{
					"turn/a/1", "turn/a/2", "turn/a/3", "turn/a/4", "turn/a/5",
					"turn/a/6", "turn/a/7", "turn/a/8", "turn/a/9", "turn/a/10",
				},
				FallbackArtifactRefs: []string{
					"turn/a/1", "turn/a/2", "turn/a/3", "turn/a/4", "turn/a/5",
					"turn/a/6", "turn/a/7", "turn/a/8", "turn/a/9", "turn/a/10",
				},
			},
		},
		Stats: ArtifactSearchStats{
			VectorCalls:   95,
			FallbackCalls: 5,
		},
		VectorCapabilityExpected: true,
		Thresholds:               DefaultSemanticRolloutGateThresholds(),
	}

	got := EvaluateSemanticRolloutGate(input)
	if !got.Passed {
		t.Fatalf("Passed=false want true; failed=%v", got.FailedChecks)
	}
	if got.FallbackInvariantPassRate != 1.0 {
		t.Fatalf("FallbackInvariantPassRate=%.3f want 1.0", got.FallbackInvariantPassRate)
	}
	if got.VectorFallbackOverlapAtK != 1.0 {
		t.Fatalf("VectorFallbackOverlapAtK=%.3f want 1.0", got.VectorFallbackOverlapAtK)
	}
	if got.FallbackRatio != 0.05 {
		t.Fatalf("FallbackRatio=%.3f want 0.05", got.FallbackRatio)
	}
	if len(got.CaseResults) != 1 {
		t.Fatalf("CaseResults len=%d want 1", len(got.CaseResults))
	}
}

func TestEvaluateSemanticRolloutGate_FailsFallbackInvariantPassRate(t *testing.T) {
	t.Parallel()

	got := EvaluateSemanticRolloutGate(SemanticRolloutGateInput{
		Cases: []SemanticValidationCase{
			{
				ID:                    "case-pass",
				DeterministicOrdering: true,
				StableRefs:            true,
				RequiredTemporalBlock: true,
				VectorArtifactRefs:    []string{"r1"},
				FallbackArtifactRefs:  []string{"r1"},
			},
			{
				ID:                    "case-fail",
				DeterministicOrdering: false,
				StableRefs:            true,
				RequiredTemporalBlock: true,
				VectorArtifactRefs:    []string{"r2"},
				FallbackArtifactRefs:  []string{"r2"},
			},
		},
		Stats: ArtifactSearchStats{
			VectorCalls:   95,
			FallbackCalls: 5,
		},
		VectorCapabilityExpected: true,
		Thresholds:               DefaultSemanticRolloutGateThresholds(),
	})

	if got.Checks.FallbackInvariantPassRate {
		t.Fatalf("FallbackInvariantPassRate check=true want false")
	}
	if !slices.Contains(got.FailedChecks, "fallback_invariant_pass_rate") {
		t.Fatalf("missing fallback_invariant_pass_rate in failed checks: %v", got.FailedChecks)
	}
}

func TestEvaluateSemanticRolloutGate_FailsOverlapAtK(t *testing.T) {
	t.Parallel()

	got := EvaluateSemanticRolloutGate(SemanticRolloutGateInput{
		Cases: []SemanticValidationCase{
			{
				ID:                    "case-overlap-low",
				DeterministicOrdering: true,
				StableRefs:            true,
				RequiredTemporalBlock: true,
				VectorArtifactRefs: []string{
					"r1", "r2", "r3", "r4", "r5", "r6", "r7", "r8", "r9", "r10",
				},
				FallbackArtifactRefs: []string{
					"r1", "r2", "r3", "r4", "r5", "r6", "r7", "r8",
				},
			},
		},
		Stats: ArtifactSearchStats{
			VectorCalls:   95,
			FallbackCalls: 5,
		},
		VectorCapabilityExpected: true,
		Thresholds:               DefaultSemanticRolloutGateThresholds(),
	})

	if got.Checks.VectorFallbackOverlapAtK {
		t.Fatalf("VectorFallbackOverlapAtK check=true want false")
	}
	if !slices.Contains(got.FailedChecks, "vector_fallback_overlap_at_k") {
		t.Fatalf("missing vector_fallback_overlap_at_k in failed checks: %v", got.FailedChecks)
	}
}

func TestEvaluateSemanticRolloutGate_FallbackRatioGuard(t *testing.T) {
	t.Parallel()

	baseInput := SemanticRolloutGateInput{
		Cases: []SemanticValidationCase{
			{
				ID:                    "case-1",
				DeterministicOrdering: true,
				StableRefs:            true,
				RequiredTemporalBlock: true,
				VectorArtifactRefs:    []string{"r1"},
				FallbackArtifactRefs:  []string{"r1"},
			},
		},
		Stats: ArtifactSearchStats{
			VectorCalls:   90,
			FallbackCalls: 10, // 10% > 5%
		},
		Thresholds: DefaultSemanticRolloutGateThresholds(),
	}

	withExpectation := baseInput
	withExpectation.VectorCapabilityExpected = true
	gotFail := EvaluateSemanticRolloutGate(withExpectation)
	if gotFail.Checks.FallbackRatio {
		t.Fatalf("FallbackRatio check=true want false")
	}
	if !slices.Contains(gotFail.FailedChecks, "fallback_ratio") {
		t.Fatalf("missing fallback_ratio in failed checks: %v", gotFail.FailedChecks)
	}

	withoutExpectation := baseInput
	withoutExpectation.VectorCapabilityExpected = false
	gotPass := EvaluateSemanticRolloutGate(withoutExpectation)
	if !gotPass.Checks.FallbackRatio {
		t.Fatalf("FallbackRatio check=false want true when capability not expected")
	}
}

func TestEvaluateSemanticRolloutGate_FallbackRatioNoSamples(t *testing.T) {
	t.Parallel()

	got := EvaluateSemanticRolloutGate(SemanticRolloutGateInput{
		Cases: []SemanticValidationCase{
			{
				ID:                    "case-1",
				DeterministicOrdering: true,
				StableRefs:            true,
				RequiredTemporalBlock: true,
				VectorArtifactRefs:    []string{"r1"},
				FallbackArtifactRefs:  []string{"r1"},
			},
		},
		Stats: ArtifactSearchStats{
			VectorCalls:   0,
			FallbackCalls: 0,
		},
		VectorCapabilityExpected: true,
		Thresholds:               DefaultSemanticRolloutGateThresholds(),
	})

	if got.FallbackRatio != 0 {
		t.Fatalf("FallbackRatio=%.3f want 0", got.FallbackRatio)
	}
	if !got.Checks.FallbackRatio {
		t.Fatalf("FallbackRatio check=false want true for zero-sample window")
	}
}

func TestEvaluateSemanticRolloutGate_OverlapAtK_DedupAndLimit(t *testing.T) {
	t.Parallel()

	vectorRefs := []string{
		"r1", "r2", "r2", "r3", "r4", "r5", "r6", "r7", "r8", "r9", "r10", "r11",
	}
	fallbackRefs := []string{
		"r1", "r2", "r3", "r4", "r5", "r6", "r7", "r8", "r9", "rx", "r1", "r2",
	}

	got := EvaluateSemanticRolloutGate(SemanticRolloutGateInput{
		Cases: []SemanticValidationCase{
			{
				ID:                    "case-overlap-dedup",
				DeterministicOrdering: true,
				StableRefs:            true,
				RequiredTemporalBlock: true,
				VectorArtifactRefs:    vectorRefs,
				FallbackArtifactRefs:  fallbackRefs,
			},
		},
		Stats: ArtifactSearchStats{
			VectorCalls:   95,
			FallbackCalls: 5,
		},
		VectorCapabilityExpected: true,
		Thresholds:               DefaultSemanticRolloutGateThresholds(),
	})

	// Top-10 unique vector refs are r1..r10; fallback contains 9/10 (missing r10).
	if got.VectorFallbackOverlapAtK != 0.9 {
		t.Fatalf("VectorFallbackOverlapAtK=%.3f want 0.900", got.VectorFallbackOverlapAtK)
	}
	if !got.Checks.VectorFallbackOverlapAtK {
		t.Fatalf("VectorFallbackOverlapAtK check=false want true at threshold")
	}
	if len(got.CaseResults) != 1 || got.CaseResults[0].ExpectedAtK != 10 {
		t.Fatalf("CaseResults unexpected: %+v", got.CaseResults)
	}
}
