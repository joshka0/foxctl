package transcriptpipeline

import (
	"context"
	"testing"

	"github.com/joshka0/foxctl/internal/context/companion"
	"github.com/joshka0/foxctl/internal/v2/adapters/sourceimport"
)

func TestCachedClaimClassifier_DeterministicFallback(t *testing.T) {
	classifier := NewCachedClaimClassifier(LocalModelRuntime{
		Mode: "deterministic",
	})

	result, err := classifier.Classify(context.Background(), nil, sourceimport.ParsedSession{
		Provider:  sourceimport.ProviderCodex,
		SessionID: "sess-1",
	}, []companion.AnchoredInteractionFrame{{ConversationID: "conv-1"}}, []companion.AnchoredMemoryDerivation{{
		FrameIndex:         0,
		InteractionSummary: "user accepted the pipeline design",
		Candidates: []companion.AnchoredMemoryCandidate{{
			Type:       companion.EntryTypeDecision,
			Scope:      companion.CandidateScopeDurable,
			Text:       "Use a pipeline architecture for transcript memory work.",
			Confidence: 0.84,
			Source:     "user",
		}},
	}})
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if len(result.Claims) != 1 {
		t.Fatalf("claims=%d want 1", len(result.Claims))
	}
	if len(result.ConsolidatedClaims) != 1 {
		t.Fatalf("consolidated_claims=%d want 1", len(result.ConsolidatedClaims))
	}
	if len(result.ReviewedClaims) != 1 {
		t.Fatalf("reviewed_claims=%d want 1", len(result.ReviewedClaims))
	}
	if result.Objective == nil || result.Objective.Objective == "" {
		t.Fatal("expected session objective")
	}
	if len(result.Synopses) != 1 {
		t.Fatalf("synopses=%d want 1", len(result.Synopses))
	}
	if result.Synopses[0].Line == "" {
		t.Fatal("expected synopsis line")
	}
	if result.Claims[0].Kind != ClaimKindDecision {
		t.Fatalf("kind=%q want %q", result.Claims[0].Kind, ClaimKindDecision)
	}
	if result.Claims[0].Durability != ClaimDurabilityDurable {
		t.Fatalf("durability=%q want %q", result.Claims[0].Durability, ClaimDurabilityDurable)
	}
}

func TestDecodeClassifiedClaims_ValidatesShape(t *testing.T) {
	raw := `{"claims":[{"text":"Use pipeline stages.","kind":"workflow_rule","durability":"durable","confidence":0.88,"source_basis":"user_approved","tags":["Pipeline Stages"],"group_keys":["workflow/pipeline"],"evidence_frame_indices":[0,99]}]}`
	got := decodeClassifiedClaims(raw, 2)
	if len(got) != 1 {
		t.Fatalf("claims=%d want 1", len(got))
	}
	if got[0].Tags[0] != "pipeline-stages" {
		t.Fatalf("tags=%v want normalized tag", got[0].Tags)
	}
	if len(got[0].EvidenceFrameIndices) != 1 || got[0].EvidenceFrameIndices[0] != 0 {
		t.Fatalf("evidence_frames=%v want [0]", got[0].EvidenceFrameIndices)
	}
}

func TestPromptVersionSelectors_ReturnDifferentBodies(t *testing.T) {
	classifyV2 := classifierSystemPromptForVersion("classified_claims_v2")
	classifyV3 := classifierSystemPromptForVersion("classified_claims_v3")
	if classifyV2 == classifyV3 {
		t.Fatal("expected different classifier prompts across versions")
	}
	classifyV4 := classifierSystemPromptForVersion("classified_claims_v4")
	if classifyV3 == classifyV4 {
		t.Fatal("expected different classifier prompts across later versions")
	}
	classifyV5 := classifierSystemPromptForVersion("classified_claims_v5")
	if classifyV4 == classifyV5 {
		t.Fatal("expected different classifier prompts across newest versions")
	}

	reviewV1 := claimReviewSystemPromptForVersion("classified_claim_review_v1")
	reviewV2 := claimReviewSystemPromptForVersion("classified_claim_review_v2")
	if reviewV1 == reviewV2 {
		t.Fatal("expected different review prompts across versions")
	}
	reviewV3 := claimReviewSystemPromptForVersion("classified_claim_review_v3")
	if reviewV2 == reviewV3 {
		t.Fatal("expected different review prompts across later versions")
	}
	reviewV4 := claimReviewSystemPromptForVersion("classified_claim_review_v4")
	if reviewV3 == reviewV4 {
		t.Fatal("expected different review prompts across newest versions")
	}
}

func TestValidateClassifiedClaims_DropsNegativeClassClaims(t *testing.T) {
	got := validateClassifiedClaims([]ClassifiedClaim{
		{
			Text:       "Experiment loop is built and working.",
			Kind:       ClaimKindWorkflowRule,
			Durability: ClaimDurabilityDurable,
			Tags:       []string{"experiment-loop", "progress"},
		},
		{
			Text:       "Hybrid runtime provides event sourcing and hard state.",
			Kind:       ClaimKindArchitecture,
			Durability: ClaimDurabilityDurable,
			Tags:       []string{"runtime", "event-sourcing"},
			GroupKeys:  []string{"architecture/pipeline"},
		},
	}, 3)
	if len(got) != 1 {
		t.Fatalf("claims=%d want 1", len(got))
	}
	if got[0].Text != "Hybrid runtime provides event sourcing and hard state." {
		t.Fatalf("claim=%q", got[0].Text)
	}
}

func TestConsolidateClassifiedClaims_MergesByGroupKey(t *testing.T) {
	in := []ClassifiedClaim{
		{
			Text:                 "Use a classifier layer for durable labeling.",
			Kind:                 ClaimKindWorkflowRule,
			Durability:           ClaimDurabilityDurable,
			Confidence:           0.82,
			SourceBasis:          "user_approved",
			Tags:                 []string{"classifier"},
			GroupKeys:            []string{"pipeline/classification"},
			EvidenceFrameIndices: []int{2},
		},
		{
			Text:                 "A classifier layer should label durable decisions semantically.",
			Kind:                 ClaimKindWorkflowRule,
			Durability:           ClaimDurabilitySession,
			Confidence:           0.70,
			SourceBasis:          "assistant",
			Tags:                 []string{"semantic-labels"},
			GroupKeys:            []string{"pipeline/classification"},
			EvidenceFrameIndices: []int{4},
		},
	}

	got := ConsolidateClassifiedClaims(in)
	if len(got) != 1 {
		t.Fatalf("claims=%d want 1", len(got))
	}
	if got[0].Durability != ClaimDurabilityDurable {
		t.Fatalf("durability=%q want durable", got[0].Durability)
	}
	if got[0].SourceBasis != "user_approved" {
		t.Fatalf("source_basis=%q want user_approved", got[0].SourceBasis)
	}
	if len(got[0].EvidenceFrameIndices) != 2 {
		t.Fatalf("evidence_frames=%v want merged indices", got[0].EvidenceFrameIndices)
	}
}

func TestApplyObjectiveAlignmentPayload_AnnotatesClaims(t *testing.T) {
	in := []ClassifiedClaim{{
		Text:       "Use a classifier layer for durable labeling.",
		Kind:       ClaimKindWorkflowRule,
		Durability: ClaimDurabilityDurable,
	}}
	got := applyObjectiveAlignmentPayload(in, objectiveAlignmentPayload{
		Alignments: []objectiveAlignmentItem{{
			Index:       0,
			Role:        ObjectiveRoleSupport,
			Action:      ObjectiveMemoryActionKeep,
			Score:       0.83,
			Explanation: "Directly codifies the main pipeline objective.",
		}},
	})
	if got[0].ObjectiveRole != ObjectiveRoleSupport {
		t.Fatalf("objective_role=%q want %q", got[0].ObjectiveRole, ObjectiveRoleSupport)
	}
	if got[0].ObjectiveScore != 0.83 {
		t.Fatalf("objective_score=%v want 0.83", got[0].ObjectiveScore)
	}
	if got[0].ObjectiveAction != ObjectiveMemoryActionKeep {
		t.Fatalf("objective_action=%q want %q", got[0].ObjectiveAction, ObjectiveMemoryActionKeep)
	}
	if got[0].ObjectiveExplanation == "" {
		t.Fatal("expected objective explanation")
	}
}

func TestDeterministicDoctrineClaims_KeepsOnlyDoctrine(t *testing.T) {
	got := deterministicDoctrineClaims([]ClassifiedClaim{
		{
			Text:        "Use a classifier layer in the transcript-memory pipeline.",
			Kind:        ClaimKindArchitecture,
			Durability:  ClaimDurabilityDurable,
			SourceBasis: "user_approved",
		},
		{
			Text:        "Avoid brittle text-canonicalization; prefer abstracted classifier-based logic.",
			Kind:        ClaimKindWorkflowRule,
			Durability:  ClaimDurabilityDurable,
			SourceBasis: "user_approved",
		},
		{
			Text:             "The objective-aware negative filter is functioning as intended.",
			Kind:             ClaimKindWorkflowRule,
			Durability:       ClaimDurabilityProvisional,
			PromotionBlocker: ClaimPromotionBlockerMetaProgress,
		},
		{
			Text:       "The objective stage is in and high-budget extraction was added.",
			Kind:       ClaimKindTechnical,
			Durability: ClaimDurabilityDurable,
		},
	})
	if len(got) != 2 {
		t.Fatalf("doctrine_claims=%d want 2", len(got))
	}
}

func TestBuildDoctrineInputClaims_PrefersDoctrineKinds(t *testing.T) {
	got := buildDoctrineInputClaims(
		[]ClassifiedClaim{
			{Text: "Implementation status note.", Kind: ClaimKindTechnical, Durability: ClaimDurabilityDurable, SourceBasis: "mixed", Confidence: 0.9},
			{Text: "Use a classifier layer in the transcript-memory pipeline.", Kind: ClaimKindArchitecture, Durability: ClaimDurabilityDurable, SourceBasis: "user_approved", Confidence: 0.8, GroupKeys: []string{"architecture/pipeline"}},
		},
		[]ClassifiedClaim{
			{Text: "Avoid brittle text-canonicalization; prefer abstracted classifier-based logic.", Kind: ClaimKindWorkflowRule, Durability: ClaimDurabilityDurable, SourceBasis: "user_approved", Confidence: 0.82, GroupKeys: []string{"workflow/classifier"}},
		},
	)
	if len(got) != 3 {
		t.Fatalf("doctrine_inputs=%d want 3", len(got))
	}
	foundClassifier := false
	foundBrittle := false
	for _, claim := range got {
		if claim.Text == "Use a classifier layer in the transcript-memory pipeline." {
			foundClassifier = true
		}
		if claim.Text == "Avoid brittle text-canonicalization; prefer abstracted classifier-based logic." {
			foundBrittle = true
		}
	}
	if !foundClassifier || !foundBrittle {
		t.Fatal("expected doctrine input to preserve classifier and brittle-rule claims")
	}
}

func TestBuildDoctrineSegments_CreatesEarlyMiddleLateViews(t *testing.T) {
	in := []ClassifiedClaim{
		{Text: "Use a classifier layer in the transcript-memory pipeline.", Kind: ClaimKindArchitecture, Durability: ClaimDurabilityDurable, SourceBasis: "user_approved", EvidenceFrameIndices: []int{0}},
		{Text: "Avoid brittle text-canonicalization; prefer abstracted classifier-based logic.", Kind: ClaimKindWorkflowRule, Durability: ClaimDurabilityDurable, SourceBasis: "user_approved", EvidenceFrameIndices: []int{1}},
		{Text: "Meta discussion about eval plumbing.", Kind: ClaimKindTechnical, Durability: ClaimDurabilitySession, SourceBasis: "mixed", EvidenceFrameIndices: []int{9}},
		{Text: "Later objective-management discussion.", Kind: ClaimKindTechnical, Durability: ClaimDurabilitySession, SourceBasis: "mixed", EvidenceFrameIndices: []int{10}},
		{Text: "Another late implementation note.", Kind: ClaimKindTechnical, Durability: ClaimDurabilitySession, SourceBasis: "mixed", EvidenceFrameIndices: []int{11}},
	}
	segments := buildDoctrineSegments(in)
	if len(segments) < 2 {
		t.Fatalf("segments=%d want at least 2", len(segments))
	}
	foundClassifier := false
	for _, segment := range segments {
		for _, claim := range segment {
			if claim.Text == "Use a classifier layer in the transcript-memory pipeline." {
				foundClassifier = true
			}
		}
	}
	if !foundClassifier {
		t.Fatal("expected early doctrine claim to survive in segmented inputs")
	}
}

func TestSeedDoctrineClaimsFromDerivations_PullsDurableDoctrineFromRawFrames(t *testing.T) {
	got := seedDoctrineClaimsFromDerivations([]companion.AnchoredMemoryDerivation{
		{
			FrameIndex: 10,
			Candidates: []companion.AnchoredMemoryCandidate{
				{
					Type:       companion.EntryTypeDecision,
					Scope:      companion.CandidateScopeDurable,
					Text:       "Use a classifier layer in the transcript-memory pipeline.",
					Confidence: 0.88,
					Source:     "user",
				},
				{
					Type:       companion.EntryTypeTechnicalContext,
					Scope:      companion.CandidateScopeSession,
					Text:       "Implementation note.",
					Confidence: 0.5,
					Source:     "assistant",
				},
			},
		},
	})
	if len(got) != 1 {
		t.Fatalf("seeds=%d want 1", len(got))
	}
	if got[0].Text != "Use a classifier layer in the transcript-memory pipeline." {
		t.Fatalf("seed=%q", got[0].Text)
	}
}

func TestIsDoctrineEvidenceClaim_AllowsDurableCorrectionEvidence(t *testing.T) {
	if !isDoctrineEvidenceClaim(ClassifiedClaim{
		Text:        "can we not do brittle decision texts, we need a classifier here instead",
		Kind:        ClaimKindOpenQuestion,
		Durability:  ClaimDurabilityDurable,
		SourceBasis: "user",
	}) {
		t.Fatal("expected durable open question from user correction to count as doctrine evidence")
	}
	if !isDoctrineEvidenceClaim(ClassifiedClaim{
		Text:        "no brittle text-specific coding where we can use an explicit classifier or typed stage",
		Kind:        ClaimKindTechnical,
		Durability:  ClaimDurabilityDurable,
		SourceBasis: "user_approved",
	}) {
		t.Fatal("expected durable user-approved technical context to count as doctrine evidence")
	}
}

func TestFinalizeDoctrineClaims_PrefersOneWorkflowAndOneArchitecture(t *testing.T) {
	got := finalizeDoctrineClaims([]ClassifiedClaim{
		{Text: "Avoid brittle text-canonicalization; prefer abstracted classifier-based logic.", Kind: ClaimKindWorkflowRule, Durability: ClaimDurabilityDurable, SourceBasis: "user_approved", Confidence: 0.9},
		{Text: "Avoid brittle text-canonicalization; prefer abstracted classifier-based logic.", Kind: ClaimKindWorkflowRule, Durability: ClaimDurabilityDurable, SourceBasis: "user_approved", Confidence: 0.88},
		{Text: "Use anchor_state_t + user_t -> assistant_t -> user_t+1 as the evaluation unit for transcript-memory derivation.", Kind: ClaimKindArchitecture, Durability: ClaimDurabilityDurable, SourceBasis: "user_approved", Confidence: 0.88},
		{Text: "persistence_user + user_t -> assistant_t -> user_t+1", Kind: ClaimKindArchitecture, Durability: ClaimDurabilityDurable, SourceBasis: "user_approved", Confidence: 0.95},
		{Text: "only be relying on the lmstudio agents in the future, ideally running as a daemon, so this was informative", Kind: ClaimKindDecision, Durability: ClaimDurabilityDurable, SourceBasis: "user", Confidence: 0.8},
	}, 2)
	if len(got) != 2 {
		t.Fatalf("doctrine_final=%d want 2", len(got))
	}
	seenWorkflow := false
	seenArchitecture := false
	for _, claim := range got {
		if claim.Kind == ClaimKindWorkflowRule {
			seenWorkflow = true
		}
		if claim.Kind == ClaimKindArchitecture {
			seenArchitecture = true
		}
	}
	if !seenWorkflow || !seenArchitecture {
		t.Fatalf("finalized=%v want workflow+architecture", got)
	}
}

func TestFinalizeGroupedDoctrineClaims_PrefersConsensusArchitecture(t *testing.T) {
	got := finalizeGroupedDoctrineClaims([]ClassifiedClaim{
		{Text: "persistence_user + user_t -> assistant_t -> user_t+1", Kind: ClaimKindArchitecture, Durability: ClaimDurabilityDurable, SourceBasis: "user_approved", Confidence: 0.95},
		{Text: "The companion hybrid pipeline supports event sourcing, typed hard state, active assumptions, soft episodes, evidence, and recent turns.", Kind: ClaimKindArchitecture, Durability: ClaimDurabilityDurable, SourceBasis: "mixed", Confidence: 0.8, Tags: []string{"consensus", "sidecar"}, GroupKeys: []string{"architecture/pipeline"}},
		{Text: "Avoid brittle text-canonicalization; prefer abstracted classifier-based logic.", Kind: ClaimKindWorkflowRule, Durability: ClaimDurabilityDurable, SourceBasis: "user_approved", Confidence: 0.9},
	}, 2)
	if len(got) != 2 {
		t.Fatalf("grouped_doctrine=%d want 2", len(got))
	}
	foundConsensusArchitecture := false
	for _, claim := range got {
		if claim.Text == "The companion hybrid pipeline supports event sourcing, typed hard state, active assumptions, soft episodes, evidence, and recent turns." {
			foundConsensusArchitecture = true
		}
	}
	if !foundConsensusArchitecture {
		t.Fatalf("finalized=%v want consensus architecture claim", got)
	}
}

func TestDeterministicDoctrineBridgeFallbackClaims_PreservesCorrectionEvidence(t *testing.T) {
	got := deterministicDoctrineBridgeFallbackClaims([]companion.AnchoredMemoryDerivation{
		{
			FrameIndex: 48,
			Candidates: []companion.AnchoredMemoryCandidate{
				{
					Type:       companion.EntryTypeOpenQuestion,
					Scope:      companion.CandidateScopeDurable,
					Text:       "not do brittle decision texts",
					Confidence: 0.8,
					Source:     "followup_user",
				},
				{
					Type:       "user_correction",
					Scope:      companion.CandidateScopeSession,
					Text:       "can we not do brittle decision texts, we need a classifier here instead",
					Confidence: 0.9,
					Source:     "followup_user",
				},
				{
					Type:       companion.EntryTypeTechnicalContext,
					Scope:      companion.CandidateScopeDurable,
					Text:       "Agreed. no brittle text-specific coding where we can use an explicit classifier or typed stage.",
					Confidence: 0.88,
					Source:     "assistant_guidance",
				},
			},
		},
	})
	if len(got) < 2 {
		t.Fatalf("bridge_fallback_claims=%d want at least 2", len(got))
	}
}

func TestDeterministicDoctrineBridgeFallbackClaims_DropsGenericDecisionClaims(t *testing.T) {
	got := deterministicDoctrineBridgeFallbackClaims([]companion.AnchoredMemoryDerivation{
		{
			FrameIndex: 12,
			Candidates: []companion.AnchoredMemoryCandidate{
				{
					Type:       companion.EntryTypeDecision,
					Scope:      companion.CandidateScopeDurable,
					Text:       "only be relying on the lmstudio agents in the future, ideally running as a daemon, so this was informative",
					Confidence: 0.8,
					Source:     "user",
				},
				{
					Type:       companion.EntryTypeOpenQuestion,
					Scope:      companion.CandidateScopeDurable,
					Text:       "not do brittle decision texts",
					Confidence: 0.9,
					Source:     "followup_user",
				},
			},
		},
	})
	if len(got) != 1 {
		t.Fatalf("bridge_fallback_claims=%d want 1", len(got))
	}
	if got[0].Kind != ClaimKindOpenQuestion {
		t.Fatalf("kind=%q want %q", got[0].Kind, ClaimKindOpenQuestion)
	}
}

func TestFilterRelevantDerivationsForClassification_PreservesEarlyRecurringDoctrine(t *testing.T) {
	derivations := make([]companion.AnchoredMemoryDerivation, 0, 14)
	for i := 0; i < 14; i++ {
		item := companion.AnchoredMemoryDerivation{
			FrameIndex: i,
			Resolution: companion.InteractionResolutionResolved,
			Candidates: []companion.AnchoredMemoryCandidate{{
				Type:       companion.EntryTypeTechnicalContext,
				Scope:      companion.CandidateScopeSession,
				Text:       "later meta discussion",
				Confidence: 0.6,
			}},
		}
		if i == 0 || i == 2 {
			item.Candidates = append(item.Candidates, companion.AnchoredMemoryCandidate{
				Type:       companion.EntryTypePolicy,
				Scope:      companion.CandidateScopeDurable,
				Text:       "Avoid brittle text-canonicalization; prefer abstracted classifier-based logic.",
				Confidence: 0.9,
			})
		}
		derivations = append(derivations, item)
	}
	got := filterRelevantDerivationsForClassification(derivations)
	if len(got) != 12 {
		t.Fatalf("derivations=%d want 12", len(got))
	}
	foundEarly := false
	for _, item := range got {
		if item.FrameIndex == 0 || item.FrameIndex == 2 {
			foundEarly = true
		}
	}
	if !foundEarly {
		t.Fatal("expected early recurring doctrine frame to be preserved")
	}
}

func TestFilterRelevantDerivationsForClassification_PreservesCorrectedFrames(t *testing.T) {
	derivations := make([]companion.AnchoredMemoryDerivation, 0, 14)
	for i := 0; i < 14; i++ {
		item := companion.AnchoredMemoryDerivation{
			FrameIndex: i,
			Resolution: companion.InteractionResolutionResolved,
			Reaction:   companion.FollowUpReaction{Outcome: companion.ReactionOutcomeAccepted},
			Candidates: []companion.AnchoredMemoryCandidate{{
				Type:       companion.EntryTypeTechnicalContext,
				Scope:      companion.CandidateScopeSession,
				Text:       "later meta discussion",
				Confidence: 0.6,
			}},
		}
		if i == 3 {
			item.Resolution = companion.InteractionResolutionCorrected
			item.Reaction = companion.FollowUpReaction{Outcome: companion.ReactionOutcomeCorrected}
			item.Candidates = append(item.Candidates, companion.AnchoredMemoryCandidate{
				Type:       "user_correction",
				Scope:      companion.CandidateScopeSession,
				Text:       "can we not do brittle decision texts, we need a classifier here instead",
				Confidence: 0.9,
				Source:     "followup_user",
			})
		}
		derivations = append(derivations, item)
	}
	got := filterRelevantDerivationsForClassification(derivations)
	foundCorrected := false
	for _, item := range got {
		if item.FrameIndex == 3 {
			foundCorrected = true
		}
	}
	if !foundCorrected {
		t.Fatal("expected corrected frame to be preserved")
	}
}

func TestSelectSegmentedDerivationsForClassification_PreservesLateCorrectedFrame(t *testing.T) {
	derivations := make([]companion.AnchoredMemoryDerivation, 0, 18)
	for i := 0; i < 18; i++ {
		item := companion.AnchoredMemoryDerivation{
			FrameIndex: i,
			Resolution: companion.InteractionResolutionResolved,
			Reaction:   companion.FollowUpReaction{Outcome: companion.ReactionOutcomeAccepted},
			Candidates: []companion.AnchoredMemoryCandidate{{
				Type:       companion.EntryTypeTechnicalContext,
				Scope:      companion.CandidateScopeSession,
				Text:       "meta discussion",
				Confidence: 0.6,
			}},
		}
		if i == 16 {
			item.Resolution = companion.InteractionResolutionCorrected
			item.Reaction = companion.FollowUpReaction{Outcome: companion.ReactionOutcomeCorrected}
			item.Candidates = append(item.Candidates, companion.AnchoredMemoryCandidate{
				Type:       "user_correction",
				Scope:      companion.CandidateScopeSession,
				Text:       "can we not do brittle decision texts, we need a classifier here instead",
				Confidence: 0.92,
				Source:     "followup_user",
			})
		}
		derivations = append(derivations, item)
	}
	got := selectSegmentedDerivationsForClassification(derivations, 12)
	foundLateCorrected := false
	for _, item := range got {
		if item.FrameIndex == 16 {
			foundLateCorrected = true
		}
	}
	if !foundLateCorrected {
		t.Fatal("expected late corrected frame to survive segmented selection")
	}
}

func TestFilterDoctrineBridgeDerivations_PreservesLateCorrectedFrame(t *testing.T) {
	derivations := make([]companion.AnchoredMemoryDerivation, 0, 18)
	for i := 0; i < 18; i++ {
		item := companion.AnchoredMemoryDerivation{
			FrameIndex: i,
			Resolution: companion.InteractionResolutionResolved,
			Reaction:   companion.FollowUpReaction{Outcome: companion.ReactionOutcomeAccepted},
			Candidates: []companion.AnchoredMemoryCandidate{{
				Type:       companion.EntryTypeTechnicalContext,
				Scope:      companion.CandidateScopeDurable,
				Text:       "accepted assistant guidance",
				Confidence: 0.7,
				Source:     "assistant_guidance",
			}},
		}
		if i == 16 {
			item.Resolution = companion.InteractionResolutionCorrected
			item.Reaction = companion.FollowUpReaction{Outcome: companion.ReactionOutcomeCorrected}
			item.Candidates = append(item.Candidates, companion.AnchoredMemoryCandidate{
				Type:       "user_correction",
				Scope:      companion.CandidateScopeSession,
				Text:       "can we not do brittle decision texts, we need a classifier here instead",
				Confidence: 0.92,
				Source:     "followup_user",
			})
		}
		derivations = append(derivations, item)
	}
	got := filterDoctrineBridgeDerivations(derivations)
	foundLateCorrected := false
	for _, item := range got {
		if item.FrameIndex == 16 {
			foundLateCorrected = true
		}
	}
	if !foundLateCorrected {
		t.Fatal("expected late corrected frame to survive doctrine bridge selection")
	}
}
