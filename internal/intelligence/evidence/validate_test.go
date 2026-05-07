package evidence

import (
	"errors"
	"testing"
)

func TestValidateEvidenceMetaCoherentTuples(t *testing.T) {
	tests := []struct {
		name string
		meta EvidenceMeta
	}{
		{
			name: "semantic anchor evidence only",
			meta: EvidenceMeta{
				Source:            EvidenceSourceSemanticAnchor,
				SourcePlane:       EvidencePlaneSemanticAnchor,
				EvidenceClass:     EvidenceClassSourceComment,
				EvidenceAuthority: EvidenceAuthorityEvidenceOnly,
				AllowedAuthorityEffects: []AuthorityEffect{
					AuthorityEffectRetrievalRanking,
					AuthorityEffectReviewSignal,
				},
			},
		},
		{
			name: "git cochange warning",
			meta: EvidenceMeta{
				Source:            EvidenceSourceGitCoChange,
				SourcePlane:       EvidencePlaneGitEmpirical,
				EvidenceClass:     EvidenceClassContextSignal,
				EvidenceAuthority: EvidenceAuthorityContextSignal,
				AllowedAuthorityEffects: []AuthorityEffect{
					AuthorityEffectRetrievalRanking,
					AuthorityEffectReviewWarning,
				},
			},
		},
		{
			name: "structural graph non actionable",
			meta: EvidenceMeta{
				Source:            EvidenceSourceStructuralGraph,
				SourcePlane:       EvidencePlaneStructural,
				EvidenceClass:     EvidenceClassCodeFact,
				EvidenceAuthority: EvidenceAuthorityFact,
				AllowedAuthorityEffects: []AuthorityEffect{
					AuthorityEffectNone,
				},
			},
		},
		{
			name: "durable reviewed instruction source",
			meta: EvidenceMeta{
				Source:            EvidenceSourceDurableMemory,
				SourcePlane:       EvidencePlaneDurable,
				EvidenceClass:     EvidenceClassReviewedMemory,
				EvidenceAuthority: EvidenceAuthorityReviewed,
				AllowedAuthorityEffects: []AuthorityEffect{
					AuthorityEffectInstructionSource,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateEvidenceMeta(tt.meta); err != nil {
				t.Fatalf("ValidateEvidenceMeta() error = %v", err)
			}
		})
	}
}

func TestValidateEvidenceMetaRejectsIncoherentTuples(t *testing.T) {
	tests := []struct {
		name string
		meta EvidenceMeta
		want ValidationReason
	}{
		{
			name: "semantic anchor cannot self declare durable policy",
			meta: EvidenceMeta{
				Source:            EvidenceSourceSemanticAnchor,
				SourcePlane:       EvidencePlaneDurable,
				EvidenceClass:     EvidenceClassReviewedMemory,
				EvidenceAuthority: EvidenceAuthorityPolicy,
				AllowedAuthorityEffects: []AuthorityEffect{
					AuthorityEffectInstructionSource,
				},
			},
			want: ValidationReasonIncoherentEvidenceTuple,
		},
		{
			name: "git cannot masquerade as review signal",
			meta: EvidenceMeta{
				Source:            EvidenceSourceGitCoChange,
				SourcePlane:       EvidencePlaneGitEmpirical,
				EvidenceClass:     EvidenceClassContextSignal,
				EvidenceAuthority: EvidenceAuthorityContextSignal,
				AllowedAuthorityEffects: []AuthorityEffect{
					AuthorityEffectReviewSignal,
				},
			},
			want: ValidationReasonIllegalEffectForPlane,
		},
		{
			name: "durable memory must use durable source plane",
			meta: EvidenceMeta{
				Source:            EvidenceSourceDurableMemory,
				SourcePlane:       EvidencePlaneSemanticAnchor,
				EvidenceClass:     EvidenceClassReviewedMemory,
				EvidenceAuthority: EvidenceAuthorityReviewed,
				AllowedAuthorityEffects: []AuthorityEffect{
					AuthorityEffectRetrievalRanking,
				},
			},
			want: ValidationReasonIncoherentEvidenceTuple,
		},
		{
			name: "structural source must use code fact class and fact authority",
			meta: EvidenceMeta{
				Source:            EvidenceSourceStructuralGraph,
				SourcePlane:       EvidencePlaneStructural,
				EvidenceClass:     EvidenceClassContextSignal,
				EvidenceAuthority: EvidenceAuthorityContextSignal,
				AllowedAuthorityEffects: []AuthorityEffect{
					AuthorityEffectRetrievalRanking,
				},
			},
			want: ValidationReasonIncoherentEvidenceTuple,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEvidenceMeta(tt.meta)
			requireValidationReason(t, err, tt.want)
		})
	}
}

func TestValidateAllowedAuthorityEffects(t *testing.T) {
	tests := []struct {
		name      string
		plane     EvidencePlane
		authority EvidenceAuthority
		effects   []AuthorityEffect
		want      ValidationReason
	}{
		{
			name:      "empty",
			plane:     EvidencePlaneSemanticAnchor,
			authority: EvidenceAuthorityEvidenceOnly,
			want:      ValidationReasonEmptyEffects,
		},
		{
			name:      "unknown plane",
			plane:     EvidencePlane("old_plane"),
			authority: EvidenceAuthorityEvidenceOnly,
			effects: []AuthorityEffect{
				AuthorityEffectRetrievalRanking,
			},
			want: ValidationReasonUnknownPlane,
		},
		{
			name:      "unknown authority",
			plane:     EvidencePlaneSemanticAnchor,
			authority: EvidenceAuthority("trusted"),
			effects: []AuthorityEffect{
				AuthorityEffectRetrievalRanking,
			},
			want: ValidationReasonUnknownAuthority,
		},
		{
			name:      "duplicate",
			plane:     EvidencePlaneSemanticAnchor,
			authority: EvidenceAuthorityEvidenceOnly,
			effects: []AuthorityEffect{
				AuthorityEffectReviewSignal,
				AuthorityEffectReviewSignal,
			},
			want: ValidationReasonDuplicateEffect,
		},
		{
			name:      "none mixed",
			plane:     EvidencePlaneStructural,
			authority: EvidenceAuthorityFact,
			effects: []AuthorityEffect{
				AuthorityEffectNone,
				AuthorityEffectRetrievalRanking,
			},
			want: ValidationReasonNoneNotExclusive,
		},
		{
			name:      "semantic anchor instruction",
			plane:     EvidencePlaneSemanticAnchor,
			authority: EvidenceAuthorityEvidenceOnly,
			effects: []AuthorityEffect{
				AuthorityEffectInstructionSource,
			},
			want: ValidationReasonIllegalEffectForPlane,
		},
		{
			name:      "structural instruction",
			plane:     EvidencePlaneStructural,
			authority: EvidenceAuthorityFact,
			effects: []AuthorityEffect{
				AuthorityEffectInstructionSource,
			},
			want: ValidationReasonIllegalEffectForPlane,
		},
		{
			name:      "git instruction",
			plane:     EvidencePlaneGitEmpirical,
			authority: EvidenceAuthorityContextSignal,
			effects: []AuthorityEffect{
				AuthorityEffectInstructionSource,
			},
			want: ValidationReasonIllegalEffectForPlane,
		},
		{
			name:      "git review signal",
			plane:     EvidencePlaneGitEmpirical,
			authority: EvidenceAuthorityContextSignal,
			effects: []AuthorityEffect{
				AuthorityEffectReviewSignal,
			},
			want: ValidationReasonIllegalEffectForPlane,
		},
		{
			name:      "durable context signal instruction",
			plane:     EvidencePlaneDurable,
			authority: EvidenceAuthorityContextSignal,
			effects: []AuthorityEffect{
				AuthorityEffectInstructionSource,
			},
			want: ValidationReasonIllegalEffectForAuthority,
		},
		{
			name:      "unknown effect",
			plane:     EvidencePlaneDurable,
			authority: EvidenceAuthorityReviewed,
			effects: []AuthorityEffect{
				AuthorityEffect("rewrite_source"),
			},
			want: ValidationReasonUnknownEffect,
		},
		{
			name:      "durable context signal retrieval",
			plane:     EvidencePlaneDurable,
			authority: EvidenceAuthorityContextSignal,
			effects: []AuthorityEffect{
				AuthorityEffectRetrievalRanking,
			},
		},
		{
			name:      "durable reviewed review signal",
			plane:     EvidencePlaneDurable,
			authority: EvidenceAuthorityReviewed,
			effects: []AuthorityEffect{
				AuthorityEffectReviewSignal,
			},
		},
		{
			name:      "durable policy review signal",
			plane:     EvidencePlaneDurable,
			authority: EvidenceAuthorityPolicy,
			effects: []AuthorityEffect{
				AuthorityEffectReviewSignal,
			},
		},
		{
			name:      "durable reviewed instruction",
			plane:     EvidencePlaneDurable,
			authority: EvidenceAuthorityReviewed,
			effects: []AuthorityEffect{
				AuthorityEffectInstructionSource,
			},
		},
		{
			name:      "durable policy instruction",
			plane:     EvidencePlaneDurable,
			authority: EvidenceAuthorityPolicy,
			effects: []AuthorityEffect{
				AuthorityEffectInstructionSource,
			},
		},
		{
			name:      "durable context signal review signal",
			plane:     EvidencePlaneDurable,
			authority: EvidenceAuthorityContextSignal,
			effects: []AuthorityEffect{
				AuthorityEffectReviewSignal,
			},
			want: ValidationReasonIllegalEffectForAuthority,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAllowedAuthorityEffects(tt.plane, tt.authority, tt.effects)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("ValidateAllowedAuthorityEffects() error = %v", err)
				}
				return
			}
			requireValidationReason(t, err, tt.want)
		})
	}
}

func TestValidateEvidenceMetaUnknownEnumReasons(t *testing.T) {
	valid := EvidenceMeta{
		Source:            EvidenceSourceSemanticAnchor,
		SourcePlane:       EvidencePlaneSemanticAnchor,
		EvidenceClass:     EvidenceClassSourceComment,
		EvidenceAuthority: EvidenceAuthorityEvidenceOnly,
		AllowedAuthorityEffects: []AuthorityEffect{
			AuthorityEffectRetrievalRanking,
			AuthorityEffectReviewSignal,
		},
	}
	tests := []struct {
		name   string
		mutate func(*EvidenceMeta)
		want   ValidationReason
	}{
		{name: "source", mutate: func(meta *EvidenceMeta) { meta.Source = EvidenceSource("comment") }, want: ValidationReasonUnknownSource},
		{name: "plane", mutate: func(meta *EvidenceMeta) { meta.SourcePlane = EvidencePlane("comment") }, want: ValidationReasonUnknownPlane},
		{name: "class", mutate: func(meta *EvidenceMeta) { meta.EvidenceClass = EvidenceClass("note") }, want: ValidationReasonUnknownClass},
		{name: "authority", mutate: func(meta *EvidenceMeta) { meta.EvidenceAuthority = EvidenceAuthority("trusted") }, want: ValidationReasonUnknownAuthority},
		{name: "effect", mutate: func(meta *EvidenceMeta) {
			meta.AllowedAuthorityEffects = []AuthorityEffect{AuthorityEffect("directive")}
		}, want: ValidationReasonUnknownEffect},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := valid
			meta.AllowedAuthorityEffects = append([]AuthorityEffect(nil), valid.AllowedAuthorityEffects...)
			tt.mutate(&meta)
			requireValidationReason(t, ValidateEvidenceMeta(meta), tt.want)
		})
	}
}

func TestValidateRenderSurface(t *testing.T) {
	semantic := EvidenceMeta{
		Source:            EvidenceSourceSemanticAnchor,
		SourcePlane:       EvidencePlaneSemanticAnchor,
		EvidenceClass:     EvidenceClassSourceComment,
		EvidenceAuthority: EvidenceAuthorityEvidenceOnly,
		AllowedAuthorityEffects: []AuthorityEffect{
			AuthorityEffectRetrievalRanking,
			AuthorityEffectReviewSignal,
		},
	}
	gitWarning := EvidenceMeta{
		Source:            EvidenceSourceGitCoChange,
		SourcePlane:       EvidencePlaneGitEmpirical,
		EvidenceClass:     EvidenceClassContextSignal,
		EvidenceAuthority: EvidenceAuthorityContextSignal,
		AllowedAuthorityEffects: []AuthorityEffect{
			AuthorityEffectReviewWarning,
		},
	}
	durableInstruction := EvidenceMeta{
		Source:            EvidenceSourceDurableMemory,
		SourcePlane:       EvidencePlaneDurable,
		EvidenceClass:     EvidenceClassReviewedMemory,
		EvidenceAuthority: EvidenceAuthorityPolicy,
		AllowedAuthorityEffects: []AuthorityEffect{
			AuthorityEffectInstructionSource,
		},
	}
	durableReview := EvidenceMeta{
		Source:            EvidenceSourceDurableMemory,
		SourcePlane:       EvidencePlaneDurable,
		EvidenceClass:     EvidenceClassReviewedMemory,
		EvidenceAuthority: EvidenceAuthorityReviewed,
		AllowedAuthorityEffects: []AuthorityEffect{
			AuthorityEffectReviewSignal,
		},
	}
	durableRetrieval := EvidenceMeta{
		Source:            EvidenceSourceDurableMemory,
		SourcePlane:       EvidencePlaneDurable,
		EvidenceClass:     EvidenceClassReviewedMemory,
		EvidenceAuthority: EvidenceAuthorityContextSignal,
		AllowedAuthorityEffects: []AuthorityEffect{
			AuthorityEffectRetrievalRanking,
		},
	}
	durableInstructionHint := EvidenceMeta{
		Source:            EvidenceSourceDurableMemory,
		SourcePlane:       EvidencePlaneDurable,
		EvidenceClass:     EvidenceClassReviewedMemory,
		EvidenceAuthority: EvidenceAuthorityPolicy,
		AllowedAuthorityEffects: []AuthorityEffect{
			AuthorityEffectInstructionSource,
			AuthorityEffectRetrievalRanking,
		},
	}
	structuralNone := EvidenceMeta{
		Source:            EvidenceSourceStructuralGraph,
		SourcePlane:       EvidencePlaneStructural,
		EvidenceClass:     EvidenceClassCodeFact,
		EvidenceAuthority: EvidenceAuthorityFact,
		AllowedAuthorityEffects: []AuthorityEffect{
			AuthorityEffectNone,
		},
	}

	tests := []struct {
		name    string
		meta    EvidenceMeta
		surface RenderSurface
		want    ValidationReason
	}{
		{name: "semantic review ok", meta: semantic, surface: RenderSurfaceReview},
		{name: "semantic evidence hint ok", meta: semantic, surface: RenderSurfaceEvidenceHint},
		{name: "semantic instruction rejected", meta: semantic, surface: RenderSurfaceInstruction, want: ValidationReasonIllegalRenderSurface},
		{name: "semantic policy rejected", meta: semantic, surface: RenderSurfacePolicy, want: ValidationReasonIllegalRenderSurface},
		{name: "semantic hard constraint rejected", meta: semantic, surface: RenderSurfaceHardConstraint, want: ValidationReasonIllegalRenderSurface},
		{name: "semantic tool authorization rejected", meta: semantic, surface: RenderSurfaceToolAuthorization, want: ValidationReasonIllegalRenderSurface},
		{name: "semantic runtime guardrail rejected", meta: semantic, surface: RenderSurfaceRuntimeGuardrail, want: ValidationReasonIllegalRenderSurface},
		{name: "git review warning ok", meta: gitWarning, surface: RenderSurfaceReviewWarning},
		{name: "git normal review rejected", meta: gitWarning, surface: RenderSurfaceReview, want: ValidationReasonIllegalRenderSurface},
		{name: "git instruction rejected", meta: gitWarning, surface: RenderSurfaceInstruction, want: ValidationReasonIllegalRenderSurface},
		{name: "durable instruction ok", meta: durableInstruction, surface: RenderSurfaceInstruction},
		{name: "durable policy ok", meta: durableInstruction, surface: RenderSurfacePolicy},
		{name: "durable review ok", meta: durableReview, surface: RenderSurfaceReview},
		{name: "durable context signal retrieval is evidence hint", meta: durableRetrieval, surface: RenderSurfaceEvidenceHint},
		{name: "durable context signal retrieval is not review", meta: durableRetrieval, surface: RenderSurfaceReview, want: ValidationReasonMissingRequiredEffect},
		{name: "durable context signal retrieval is not instruction", meta: durableRetrieval, surface: RenderSurfaceInstruction, want: ValidationReasonMissingRequiredEffect},
		{name: "durable instruction is not evidence hint", meta: durableInstruction, surface: RenderSurfaceEvidenceHint, want: ValidationReasonIllegalRenderSurface},
		{name: "evidence hint rejects instruction source even with retrieval", meta: durableInstructionHint, surface: RenderSurfaceEvidenceHint, want: ValidationReasonIllegalRenderSurface},
		{name: "none can appear in evidence pack", meta: structuralNone, surface: RenderSurfaceEvidencePack},
		{name: "none is not evidence hint", meta: structuralNone, surface: RenderSurfaceEvidenceHint, want: ValidationReasonIllegalRenderSurface},
		{name: "none is not review", meta: structuralNone, surface: RenderSurfaceReview, want: ValidationReasonMissingRequiredEffect},
		{name: "none is not instruction", meta: structuralNone, surface: RenderSurfaceInstruction, want: ValidationReasonIllegalRenderSurface},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRenderSurface(tt.meta, tt.surface)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("ValidateRenderSurface() error = %v", err)
				}
				return
			}
			requireValidationReason(t, err, tt.want)
		})
	}
}

func requireValidationReason(t *testing.T, err error, want ValidationReason) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected validation reason %q, got nil", want)
	}
	var validationErr ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T %[1]v", err)
	}
	if validationErr.Reason != want {
		t.Fatalf("reason=%q want %q", validationErr.Reason, want)
	}
}
