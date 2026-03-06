package contextbuilder

import (
	"encoding/json"
	"math"
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

func TestEvaluateSemanticRolloutGate_NoComparableVectorRefsFailsOverlap(t *testing.T) {
	t.Parallel()

	got := EvaluateSemanticRolloutGate(SemanticRolloutGateInput{
		Cases: []SemanticValidationCase{
			{
				ID:                    "case-no-vector-refs",
				DeterministicOrdering: true,
				StableRefs:            true,
				RequiredTemporalBlock: true,
				VectorArtifactRefs:    nil,
				FallbackArtifactRefs:  []string{"r1", "r2"},
			},
		},
		Stats: ArtifactSearchStats{
			VectorCalls:   95,
			FallbackCalls: 5,
		},
		VectorCapabilityExpected: true,
		Thresholds:               DefaultSemanticRolloutGateThresholds(),
	})

	if got.VectorFallbackOverlapAtK != 0 {
		t.Fatalf("VectorFallbackOverlapAtK=%.3f want 0", got.VectorFallbackOverlapAtK)
	}
	if len(got.CaseResults) != 1 {
		t.Fatalf("CaseResults len=%d want 1", len(got.CaseResults))
	}
	if got.CaseResults[0].ExpectedAtK != 0 || got.CaseResults[0].OverlapAtK != 0 {
		t.Fatalf("case result unexpected: %+v", got.CaseResults[0])
	}
	if got.Checks.VectorFallbackOverlapAtK {
		t.Fatalf("VectorFallbackOverlapAtK check=true want false")
	}
	if !slices.Contains(got.FailedChecks, "vector_fallback_overlap_at_k") {
		t.Fatalf("missing vector_fallback_overlap_at_k in failed checks: %v", got.FailedChecks)
	}
}

func TestEvaluateSemanticRolloutGate_DefaultThresholdsWhenUnset(t *testing.T) {
	t.Parallel()

	got := EvaluateSemanticRolloutGate(SemanticRolloutGateInput{
		Cases: []SemanticValidationCase{
			{
				ID:                    "case-default-thresholds",
				DeterministicOrdering: true,
				StableRefs:            true,
				RequiredTemporalBlock: true,
				VectorArtifactRefs:    []string{"r1"},
				FallbackArtifactRefs:  []string{"r1"},
			},
		},
		Stats: ArtifactSearchStats{
			VectorCalls:   97,
			FallbackCalls: 3, // 3% should pass the default 5% ratio
		},
		VectorCapabilityExpected: true,
	})
	if !got.Checks.FallbackRatio {
		t.Fatalf("FallbackRatio check=false want true with default threshold; failed=%v", got.FailedChecks)
	}
}

func TestNormalizeRolloutThresholds_AllowExplicitZeroMaxFallbackRatio(t *testing.T) {
	t.Parallel()

	zero := 0.0
	normalized := normalizeRolloutThresholds(SemanticRolloutGateThresholds{
		MinFallbackInvariantPassRate: 1.0,
		MinVectorFallbackOverlapAtK:  0.9,
		MaxFallbackRatio:             &zero,
		OverlapTopK:                  10,
	})
	if normalized.MaxFallbackRatio == nil {
		t.Fatalf("MaxFallbackRatio=nil want non-nil")
	}
	if *normalized.MaxFallbackRatio != 0.0 {
		t.Fatalf("MaxFallbackRatio=%.3f want 0.000", *normalized.MaxFallbackRatio)
	}
}

func TestNormalizeRolloutThresholds_DefaultsUnsetMaxFallbackRatio(t *testing.T) {
	t.Parallel()

	normalized := normalizeRolloutThresholds(SemanticRolloutGateThresholds{
		MinFallbackInvariantPassRate: 1.0,
		MinVectorFallbackOverlapAtK:  0.9,
		OverlapTopK:                  10,
	})
	if normalized.MaxFallbackRatio == nil {
		t.Fatalf("MaxFallbackRatio=nil want non-nil")
	}
	if *normalized.MaxFallbackRatio != 0.05 {
		t.Fatalf("MaxFallbackRatio=%.3f want 0.050", *normalized.MaxFallbackRatio)
	}
}

func TestNormalizeRolloutThresholds_JSONOmittedMaxFallbackRatioDefaults(t *testing.T) {
	t.Parallel()

	var decoded SemanticRolloutGateThresholds
	if err := json.Unmarshal([]byte(`{
		"min_fallback_invariant_pass_rate": 1.0,
		"min_vector_fallback_overlap_at_k": 0.9,
		"overlap_top_k": 10
	}`), &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	normalized := normalizeRolloutThresholds(decoded)
	if normalized.MaxFallbackRatio == nil {
		t.Fatalf("MaxFallbackRatio=nil want non-nil")
	}
	if *normalized.MaxFallbackRatio != 0.05 {
		t.Fatalf("MaxFallbackRatio=%.3f want 0.050", *normalized.MaxFallbackRatio)
	}
}

func TestNormalizeRolloutThresholds_JSONExplicitZeroMaxFallbackRatioPreserved(t *testing.T) {
	t.Parallel()

	var decoded SemanticRolloutGateThresholds
	if err := json.Unmarshal([]byte(`{
		"min_fallback_invariant_pass_rate": 1.0,
		"min_vector_fallback_overlap_at_k": 0.9,
		"max_fallback_ratio": 0.0,
		"overlap_top_k": 10
	}`), &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	normalized := normalizeRolloutThresholds(decoded)
	if normalized.MaxFallbackRatio == nil {
		t.Fatalf("MaxFallbackRatio=nil want non-nil")
	}
	if *normalized.MaxFallbackRatio != 0.0 {
		t.Fatalf("MaxFallbackRatio=%.3f want 0.000", *normalized.MaxFallbackRatio)
	}
}

func TestNormalizeRolloutThresholds_Table(t *testing.T) {
	t.Parallel()

	negative := -1.0
	explicit := 0.2
	tests := []struct {
		name string
		in   SemanticRolloutGateThresholds
		want SemanticRolloutGateThresholds
	}{
		{
			name: "defaults_when_unset",
			in:   SemanticRolloutGateThresholds{},
			want: SemanticRolloutGateThresholds{
				MinFallbackInvariantPassRate: 1.0,
				MinVectorFallbackOverlapAtK:  0.9,
				MaxFallbackRatio:             ptrFloat64(0.05),
				OverlapTopK:                  10,
			},
		},
		{
			name: "negative_max_ratio_defaults",
			in: SemanticRolloutGateThresholds{
				MinFallbackInvariantPassRate: 0.95,
				MinVectorFallbackOverlapAtK:  0.85,
				MaxFallbackRatio:             &negative,
				OverlapTopK:                  8,
			},
			want: SemanticRolloutGateThresholds{
				MinFallbackInvariantPassRate: 0.95,
				MinVectorFallbackOverlapAtK:  0.85,
				MaxFallbackRatio:             ptrFloat64(0.05),
				OverlapTopK:                  8,
			},
		},
		{
			name: "explicit_values_preserved",
			in: SemanticRolloutGateThresholds{
				MinFallbackInvariantPassRate: 0.8,
				MinVectorFallbackOverlapAtK:  0.75,
				MaxFallbackRatio:             &explicit,
				OverlapTopK:                  12,
			},
			want: SemanticRolloutGateThresholds{
				MinFallbackInvariantPassRate: 0.8,
				MinVectorFallbackOverlapAtK:  0.75,
				MaxFallbackRatio:             ptrFloat64(0.2),
				OverlapTopK:                  12,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeRolloutThresholds(tt.in)
			if got.MinFallbackInvariantPassRate != tt.want.MinFallbackInvariantPassRate {
				t.Fatalf("MinFallbackInvariantPassRate=%.3f want %.3f", got.MinFallbackInvariantPassRate, tt.want.MinFallbackInvariantPassRate)
			}
			if got.MinVectorFallbackOverlapAtK != tt.want.MinVectorFallbackOverlapAtK {
				t.Fatalf("MinVectorFallbackOverlapAtK=%.3f want %.3f", got.MinVectorFallbackOverlapAtK, tt.want.MinVectorFallbackOverlapAtK)
			}
			if got.OverlapTopK != tt.want.OverlapTopK {
				t.Fatalf("OverlapTopK=%d want %d", got.OverlapTopK, tt.want.OverlapTopK)
			}
			if got.MaxFallbackRatio == nil || tt.want.MaxFallbackRatio == nil {
				t.Fatalf("MaxFallbackRatio nil mismatch got=%v want=%v", got.MaxFallbackRatio, tt.want.MaxFallbackRatio)
			}
			if math.Abs(*got.MaxFallbackRatio-*tt.want.MaxFallbackRatio) > 1e-9 {
				t.Fatalf("MaxFallbackRatio=%.6f want %.6f", *got.MaxFallbackRatio, *tt.want.MaxFallbackRatio)
			}
		})
	}
}

func TestEvaluateSemanticRolloutGate_ResultShapeSnapshot(t *testing.T) {
	t.Parallel()

	input := SemanticRolloutGateInput{
		Cases: []SemanticValidationCase{
			{
				ID:                    "shape-case",
				DeterministicOrdering: true,
				StableRefs:            true,
				RequiredTemporalBlock: false,
				VectorArtifactRefs:    []string{"a", "b", "c"},
				FallbackArtifactRefs:  []string{"a", "z"},
			},
		},
		Stats: ArtifactSearchStats{
			VectorCalls:   8,
			FallbackCalls: 2,
		},
		VectorCapabilityExpected: true,
		Thresholds: SemanticRolloutGateThresholds{
			MinFallbackInvariantPassRate: 1.0,
			MinVectorFallbackOverlapAtK:  0.5,
			MaxFallbackRatio:             ptrFloat64(0.1),
			OverlapTopK:                  3,
		},
	}

	got := EvaluateSemanticRolloutGate(input)

	wantJSON := `{"passed":false,"fallback_invariant_pass_rate":0,"vector_fallback_overlap_at_k":0.3333333333333333,"fallback_ratio":0.2,"checks":{"fallback_invariant_pass_rate":false,"vector_fallback_overlap_at_k":false,"fallback_ratio":false},"failed_checks":["fallback_invariant_pass_rate","vector_fallback_overlap_at_k","fallback_ratio"],"case_results":[{"id":"shape-case","fallback_invariant_pass":false,"expected_at_k":3,"overlap_at_k":0.3333333333333333}]}`
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if string(gotJSON) != wantJSON {
		t.Fatalf("result snapshot mismatch\ngot:  %s\nwant: %s", string(gotJSON), wantJSON)
	}
}

func ptrFloat64(v float64) *float64 {
	return &v
}
