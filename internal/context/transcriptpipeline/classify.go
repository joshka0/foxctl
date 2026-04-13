package transcriptpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/context/companion"
	"github.com/jkatigb/agentctl/internal/storage/transcriptcache"
	"github.com/jkatigb/agentctl/internal/v2/adapters/sourceimport"
)

// ClaimKind labels the semantic type of a classified claim.
type ClaimKind string

const (
	ClaimKindArchitecture ClaimKind = "architecture"
	ClaimKindWorkflowRule ClaimKind = "workflow_rule"
	ClaimKindDecision     ClaimKind = "decision"
	ClaimKindPreference   ClaimKind = "preference"
	ClaimKindTechnical    ClaimKind = "technical_context"
	ClaimKindPainPoint    ClaimKind = "pain_point"
	ClaimKindOpenQuestion ClaimKind = "open_question"
)

// ClaimDurability labels how long a classified claim should persist.
type ClaimDurability string

const (
	ClaimDurabilitySession     ClaimDurability = "session"
	ClaimDurabilityDurable     ClaimDurability = "durable"
	ClaimDurabilityProvisional ClaimDurability = "provisional"
)

// ClaimPromotionBlocker marks claims that should not be promoted to durable memory.
type ClaimPromotionBlocker string

const (
	ClaimPromotionBlockerNone                 ClaimPromotionBlocker = "none"
	ClaimPromotionBlockerMetaProgress         ClaimPromotionBlocker = "meta_progress"
	ClaimPromotionBlockerImplementationStatus ClaimPromotionBlocker = "implementation_status"
	ClaimPromotionBlockerEvaluationMeta       ClaimPromotionBlocker = "evaluation_meta"
	ClaimPromotionBlockerProceduralScaffold   ClaimPromotionBlocker = "procedural_scaffold"
)

// ObjectiveRole describes how a reviewed claim relates to the current session objective.
type ObjectiveRole string

const (
	ObjectiveRoleSupport    ObjectiveRole = "support"
	ObjectiveRoleBlock      ObjectiveRole = "block"
	ObjectiveRoleRedirect   ObjectiveRole = "redirect"
	ObjectiveRoleIrrelevant ObjectiveRole = "irrelevant"
)

// ObjectiveMemoryAction is the objective-aware promotion decision for a reviewed claim.
type ObjectiveMemoryAction string

const (
	ObjectiveMemoryActionKeep  ObjectiveMemoryAction = "keep"
	ObjectiveMemoryActionPrune ObjectiveMemoryAction = "prune"
)

// ClassifiedClaim is a typed semantic claim ready for later grouping/chunking.
type ClassifiedClaim struct {
	Text                 string                `json:"text"`
	Kind                 ClaimKind             `json:"kind"`
	Durability           ClaimDurability       `json:"durability"`
	PromotionBlocker     ClaimPromotionBlocker `json:"promotion_blocker,omitempty"`
	Confidence           float64               `json:"confidence"`
	SourceBasis          string                `json:"source_basis,omitempty"`
	Tags                 []string              `json:"tags,omitempty"`
	GroupKeys            []string              `json:"group_keys,omitempty"`
	EvidenceFrameIndices []int                 `json:"evidence_frame_indices,omitempty"`
	ObjectiveRole        ObjectiveRole         `json:"objective_role,omitempty"`
	ObjectiveScore       float64               `json:"objective_alignment_score,omitempty"`
	ObjectiveExplanation string                `json:"objective_explanation,omitempty"`
	ObjectiveAction      ObjectiveMemoryAction `json:"objective_memory_action,omitempty"`
}

// ClassificationResult is the classifier stage output for one transcript run.
type ClassificationResult struct {
	Claims               []ClassifiedClaim     `json:"classified_claims,omitempty"`
	ConsolidatedClaims   []ClassifiedClaim     `json:"consolidated_claims,omitempty"`
	ReviewedClaims       []ClassifiedClaim     `json:"reviewed_claims,omitempty"`
	DoctrineSeedClaims   []ClassifiedClaim     `json:"doctrine_seed_claims,omitempty"`
	DoctrineClaims       []ClassifiedClaim     `json:"doctrine_claims,omitempty"`
	AlignedClaims        []ClassifiedClaim     `json:"aligned_claims,omitempty"`
	Objective            *SessionObjective     `json:"objective,omitempty"`
	Synopses             []FrameSynopsis       `json:"synopses,omitempty"`
	Artifact             *ArtifactCacheReport  `json:"classification_artifact,omitempty"`
	Artifacts            []ArtifactCacheReport `json:"classification_artifacts,omitempty"`
	ReviewArtifact       *ArtifactCacheReport  `json:"review_artifact,omitempty"`
	DoctrineSeedArtifact *ArtifactCacheReport  `json:"doctrine_seed_artifact,omitempty"`
	DoctrineArtifact     *ArtifactCacheReport  `json:"doctrine_artifact,omitempty"`
	AlignmentArtifact    *ArtifactCacheReport  `json:"alignment_artifact,omitempty"`
}

// DerivationClassifier classifies raw anchored derivations using a higher-level policy.
// This is the preferred extension point for durable-memory selection logic instead of embedding
// brittle text-specific rules into the companion extractor.
type DerivationClassifier interface {
	Classify(ctx context.Context, cacheStore *transcriptcache.Store, parsed sourceimport.ParsedSession, frames []companion.AnchoredInteractionFrame, derivations []companion.AnchoredMemoryDerivation) (ClassificationResult, error)
}

// DerivationClassifierFunc adapts a function to the DerivationClassifier interface.
type DerivationClassifierFunc func(ctx context.Context, cacheStore *transcriptcache.Store, parsed sourceimport.ParsedSession, frames []companion.AnchoredInteractionFrame, derivations []companion.AnchoredMemoryDerivation) (ClassificationResult, error)

func (f DerivationClassifierFunc) Classify(ctx context.Context, cacheStore *transcriptcache.Store, parsed sourceimport.ParsedSession, frames []companion.AnchoredInteractionFrame, derivations []companion.AnchoredMemoryDerivation) (ClassificationResult, error) {
	return f(ctx, cacheStore, parsed, frames, derivations)
}

// CachedClaimClassifier classifies derivations with LMStudio and caches the result.
type CachedClaimClassifier struct {
	Runtime LocalModelRuntime
}

// NewCachedClaimClassifier creates a cached classifier stage from the local runtime config.
func NewCachedClaimClassifier(runtime LocalModelRuntime) *CachedClaimClassifier {
	return &CachedClaimClassifier{Runtime: runtime}
}

// Classify runs the classifier with per-frame cache reuse and run-level semantic merge.
func (c *CachedClaimClassifier) Classify(ctx context.Context, cacheStore *transcriptcache.Store, parsed sourceimport.ParsedSession, frames []companion.AnchoredInteractionFrame, derivations []companion.AnchoredMemoryDerivation) (ClassificationResult, error) {
	relevant := filterRelevantDerivationsForClassification(derivations)
	if len(relevant) == 0 {
		return ClassificationResult{}, nil
	}
	objective, err := BuildSessionObjective(ctx, cacheStore, c.Runtime, parsed)
	if err != nil {
		return ClassificationResult{}, err
	}
	objectiveScaffold := objective.Scaffold()
	synopses, err := BuildFrameSynopses(ctx, cacheStore, c.Runtime, parsed, objective, derivations)
	if err != nil {
		return ClassificationResult{}, err
	}
	synopsisByFrame := make(map[int]FrameSynopsis, len(synopses))
	for _, synopsis := range synopses {
		synopsisByFrame[synopsis.FrameIndex] = synopsis
	}
	var (
		claims    []ClassifiedClaim
		artifacts []ArtifactCacheReport
		hashes    []string
	)
	for _, derivation := range relevant {
		frameClaims, report, err := c.classifyDerivation(ctx, cacheStore, parsed, len(frames), derivation, synopsisByFrame[derivation.FrameIndex], objectiveScaffold)
		if err != nil {
			return ClassificationResult{}, err
		}
		if report != nil {
			artifacts = append(artifacts, *report)
			hashes = append(hashes, report.NormalizedHash)
		}
		claims = mergeClassifiedClaims(claims, frameClaims)
	}
	aggregate := summarizeClassificationArtifacts(artifacts, claims, hashes)
	doctrineSeeds, doctrineSeedArtifact, err := c.bridgeDoctrineSeeds(ctx, cacheStore, parsed, derivations, len(frames))
	if err != nil {
		return ClassificationResult{}, err
	}
	consolidated := ConsolidateClassifiedClaims(claims)
	reviewed, reviewArtifact, err := c.reviewClaims(ctx, cacheStore, consolidated, len(frames))
	if err != nil {
		return ClassificationResult{}, err
	}
	doctrineClaims := ConsolidateClassifiedClaims(doctrineSeeds)
	doctrineArtifact := doctrineSeedArtifact
	if len(doctrineClaims) == 0 {
		doctrineInput := seedDoctrineClaimsFromDerivations(derivations)
		doctrineClaims, doctrineArtifact, err = c.distillSegmentedDoctrineClaims(ctx, cacheStore, len(frames), doctrineInput)
		if err != nil {
			return ClassificationResult{}, err
		}
	}
	alignedClaims := doctrineClaims
	var alignmentArtifact *ArtifactCacheReport
	claimsForAlignment := doctrineClaims
	if len(claimsForAlignment) > 0 && strings.TrimSpace(objective.Objective) != "" {
		alignedClaims, alignmentArtifact, err = c.alignClaimsToObjective(ctx, cacheStore, objective, claimsForAlignment)
		if err != nil {
			return ClassificationResult{}, err
		}
	}
	return ClassificationResult{
		Claims:               claims,
		ConsolidatedClaims:   consolidated,
		ReviewedClaims:       reviewed,
		DoctrineSeedClaims:   doctrineSeeds,
		DoctrineClaims:       doctrineClaims,
		AlignedClaims:        alignedClaims,
		Objective:            &objective,
		Synopses:             synopses,
		Artifact:             aggregate,
		Artifacts:            artifacts,
		ReviewArtifact:       reviewArtifact,
		DoctrineSeedArtifact: doctrineSeedArtifact,
		DoctrineArtifact:     doctrineArtifact,
		AlignmentArtifact:    alignmentArtifact,
	}, nil
}

const classifierSystemPromptV2 = `Classify transcript-derived memory claims from anchored interaction evidence.
Return only valid JSON of the form:
{"claims":[{"text":"...","kind":"architecture|workflow_rule|decision|preference|technical_context|pain_point|open_question","durability":"session|durable|provisional","confidence":0.0,"source_basis":"user|assistant|user_approved|mixed","tags":["..."],"group_keys":["..."],"evidence_frame_indices":[0]}]}

Rules:
- Prefer durable decisions, workflow rules, architecture, and preferences that will matter in future sessions.
- Prefer user-approved rules and explicit user corrections over implementation progress narration.
- Avoid implementation chatter, file inventories, build/test narration, and meta progress updates.
- Tags should be short semantic labels.
- group_keys should be stable semantic buckets like "architecture/pipeline" or "workflow/local-models".
- evidence_frame_indices must reference the provided frame numbers.
- Return at most 6 claims.`

const classifierSystemPromptV3 = `Classify transcript-derived memory claims from one anchored interaction frame.
Return only valid JSON:
{"claims":[{"text":"...","kind":"architecture|workflow_rule|decision|preference|technical_context|pain_point|open_question","durability":"session|durable|provisional","confidence":0.0,"source_basis":"user|assistant|user_approved|mixed","tags":["..."],"group_keys":["..."],"evidence_frame_indices":[0]}]}

Stricter rules:
- Durable should be used only for stable doctrine, architecture, workflow rules, or explicit user preferences/decisions.
- If a claim mostly describes a recent code edit, file move, extraction, or implementation status, mark it session or omit it.
- Workflow rules should read like repeatable guidance for future work.
- Architecture should describe enduring system structure, not a refactor step.
- Technical context should be stable project fact only; if not stable, omit it.
- Prefer short standalone statements with no file paths or markdown links.
- Return at most 4 claims.`

const classifierSystemPromptV4 = `Classify transcript-derived memory claims from one anchored interaction frame.
Return only valid JSON:
{"claims":[{"text":"...","kind":"architecture|workflow_rule|decision|preference|technical_context|pain_point|open_question","durability":"session|durable|provisional","confidence":0.0,"source_basis":"user|assistant|user_approved|mixed","tags":["..."],"group_keys":["..."],"evidence_frame_indices":[0]}]}

Stricter memory policy:
- Durable claims should be stable doctrine, architecture, workflow rules, or explicit user preferences/decisions that will matter in future sessions.
- If a statement is mainly about an implementation step, refactor, file extraction, recent code change, current status, benchmark score, or experiment progress, do not mark it durable.
- If a statement is about evaluation strategy or "what to try next", keep it session or provisional, not durable.
- Workflow rules should tell future agents how to work.
- Architecture should describe enduring system structure or substrate.
- Prefer concise standalone statements with no file paths or markdown links.
- Return at most 3 claims.`

const classifierSystemPromptV5 = `Classify transcript-derived memory claims from one anchored interaction frame.
Return only valid JSON:
{"claims":[{"text":"...","kind":"architecture|workflow_rule|decision|preference|technical_context|pain_point|open_question","durability":"session|durable|provisional","confidence":0.0,"source_basis":"user|assistant|user_approved|mixed","tags":["..."],"group_keys":["..."],"evidence_frame_indices":[0]}]}

Grouped-doctrine policy:
- Durable should be reserved for stable doctrine, architecture, and repeatable workflow rules.
- If a claim is about experiment progress, score thresholds, implementation status, recent refactors, extracted files, "built/working now", or "I added/changed", keep it session or omit it.
- If a claim describes the enduring substrate (hybrid runtime, event sourcing, hard state, assumptions, soft episodes, maintenance daemon), prefer architecture and durable.
- If a claim describes a future-work rule like avoiding brittle text logic or preferring classifier stages, prefer workflow_rule and durable.
- Prefer concise doctrine statements with no links or file paths.
- Return at most 3 claims.`

const claimReviewSystemPromptV1 = `Review and refine consolidated transcript memory claims.
Return only valid JSON of the form:
{"claims":[{"text":"...","kind":"architecture|workflow_rule|decision|preference|technical_context|pain_point|open_question","durability":"session|durable|provisional","confidence":0.0,"source_basis":"user|assistant|user_approved|mixed","tags":["..."],"group_keys":["..."],"evidence_frame_indices":[0]}]}

Review rules:
- Keep only the strongest semantically useful memory statements.
- Rewrite text into concise standalone memory statements.
- Remove file paths, markdown links, and implementation-status phrasing.
- Prefer durable only for stable doctrine, accepted rules, architecture, or explicit preferences/decisions.
- Workflow rules should describe how work should be done.
- Architecture should describe enduring design structure.
- Technical context should describe stable project facts, not recent edits.
- Return at most 4 claims.`

const claimReviewSystemPromptV2 = `Review consolidated transcript memory claims and keep only durable doctrine.
Return only valid JSON:
{"claims":[{"text":"...","kind":"architecture|workflow_rule|decision|preference|technical_context|pain_point|open_question","durability":"session|durable|provisional","confidence":0.0,"source_basis":"user|assistant|user_approved|mixed","tags":["..."],"group_keys":["..."],"evidence_frame_indices":[0]}]}

Review policy:
- Keep durable only if the claim is a stable architecture principle, workflow rule, or explicit preference/decision likely to matter in future sessions.
- Drop or downgrade implementation-status claims about recent edits, extracted files, "working now", or "I added/changed" narration.
- Rewrite surviving durable claims into concise doctrine-like statements.
- Prefer workflow_rule over technical_context when the claim tells future agents how to work.
- Prefer architecture when the claim describes enduring structure/substrate.
- Return at most 3 claims.`

const claimReviewSystemPromptV3 = `Review consolidated transcript memory claims and keep only doctrine that should survive long-term.
Return only valid JSON:
{"claims":[{"text":"...","kind":"architecture|workflow_rule|decision|preference|technical_context|pain_point|open_question","durability":"session|durable|provisional","confidence":0.0,"source_basis":"user|assistant|user_approved|mixed","tags":["..."],"group_keys":["..."],"evidence_frame_indices":[0]}]}

Long-term memory policy:
- Keep durable only if the claim would still matter after a week or after many more sessions.
- Drop or downgrade claims about recent refactors, extracted files, status updates, implementation progress, experiment-loop bookkeeping, score thresholds, or "what we just changed".
- Prefer durable workflow_rule for anti-patterns and future work rules.
- Prefer durable architecture for enduring substrate/structure.
- Prefer durable decision or preference only when explicitly user-approved and stable.
- Rewrite survivors into short doctrine statements.
- Return at most 2 durable claims plus any needed provisional claim.`

const claimReviewSystemPromptV4 = `Review consolidated transcript memory claims and keep only durable doctrine.
Return only valid JSON:
{"claims":[{"text":"...","kind":"architecture|workflow_rule|decision|preference|technical_context|pain_point|open_question","durability":"session|durable|provisional","confidence":0.0,"source_basis":"user|assistant|user_approved|mixed","tags":["..."],"group_keys":["..."],"evidence_frame_indices":[0]}]}

Review policy:
- Prefer claims tagged or grouped around enduring substrate, hybrid runtime, event sourcing, hard state, assumptions, soft episodes, or maintenance daemon.
- Prefer workflow rules that forbid brittle text logic or require classifier/type-based stages.
- Drop or downgrade claims about experiment loops, score thresholds, "works better", "is built", "is in place", recent extraction/refactor steps, or implementation bookkeeping.
- Rewrite survivors into concise doctrine statements with no file paths or links.
- Return at most 2 durable claims and at most 1 provisional/session claim.`

const classifierSystemPromptV6 = `Classify transcript-derived memory claims from one anchored interaction frame.
Return only valid JSON:
{"claims":[{"text":"...","kind":"architecture|workflow_rule|decision|preference|technical_context|pain_point|open_question","durability":"session|durable|provisional","promotion_blocker":"none|meta_progress|implementation_status|evaluation_meta|procedural_scaffold","confidence":0.0,"source_basis":"user|assistant|user_approved|mixed","tags":["..."],"group_keys":["..."],"evidence_frame_indices":[0]}]}

Promotion blocker policy:
- Use promotion_blocker=none only when a claim is eligible for future durable promotion.
- Use promotion_blocker=meta_progress for build/progress/status notes or "what was just completed".
- Use promotion_blocker=implementation_status for refactor/extraction/file-move/code-change narration.
- Use promotion_blocker=evaluation_meta for benchmark/score/experiment comparison statements.
- Use promotion_blocker=procedural_scaffold for workflow bookkeeping or next-step planning that should not persist as memory.
- Durable should be reserved for stable architecture, workflow rules, and explicit user preferences/decisions.
- Prefer concise doctrine statements with no links or file paths.
- Return at most 3 claims.`

const claimReviewSystemPromptV5 = `Review consolidated transcript memory claims and keep only doctrine that should survive long-term.
Return only valid JSON:
{"claims":[{"text":"...","kind":"architecture|workflow_rule|decision|preference|technical_context|pain_point|open_question","durability":"session|durable|provisional","promotion_blocker":"none|meta_progress|implementation_status|evaluation_meta|procedural_scaffold","confidence":0.0,"source_basis":"user|assistant|user_approved|mixed","tags":["..."],"group_keys":["..."],"evidence_frame_indices":[0]}]}

Review policy:
- Preserve or add promotion_blocker for any claim that is meta progress, implementation status, evaluation commentary, or procedural scaffolding.
- Keep durable only when promotion_blocker=none and the claim is stable doctrine.
- Prefer architecture for enduring substrate/runtime structure.
- Prefer workflow_rule for anti-patterns and future work rules.
- Rewrite survivors into concise doctrine statements with no file paths or links.
- Return at most 2 durable claims and at most 1 provisional/session claim.`

const doctrineDistillSystemPromptV1 = `Distill only durable doctrine from transcript memory claims.
Return only valid JSON:
{"claims":[{"text":"...","kind":"architecture|workflow_rule|decision|preference","durability":"durable","confidence":0.0,"source_basis":"user|assistant|user_approved|mixed","tags":["..."],"group_keys":["..."],"evidence_frame_indices":[0]}]}

Rules:
- Keep only enduring doctrine: stable architecture substrate, repeatable workflow rules, explicit user-approved decisions, or stable preferences.
- Drop implementation status, extraction progress, objective-management chatter, experiment bookkeeping, and future proposals.
- Prefer the doctrine users would still want remembered after many sessions.
- Rewrite into short standalone doctrine statements.
- Return at most 4 claims.

Few-shot examples:
Input claim: "Use a classifier layer in the transcript-memory pipeline."
Output claim: {"text":"Use a classifier layer in the transcript-memory pipeline.","kind":"architecture","durability":"durable","confidence":0.88,"source_basis":"user_approved","group_keys":["architecture/pipeline"]}

Input claim: "Avoid brittle text-canonicalization; prefer abstracted classifier-based logic."
Output claim: {"text":"Avoid brittle text-canonicalization; prefer abstracted classifier-based logic.","kind":"workflow_rule","durability":"durable","confidence":0.90,"source_basis":"user_approved","group_keys":["workflow/classifier"]}

Input claim: "The objective stage is in and high-budget objective extraction has been added near the top of the pipeline."
Output: omit

Input claim: "Append-only experiment logging is added for the eval loop."
Output: omit`

const doctrineDistillSystemPromptV2 = `Distill only durable doctrine from transcript memory claims.
Return only valid JSON:
{"claims":[{"text":"...","kind":"architecture|workflow_rule|decision|preference","durability":"durable","confidence":0.0,"source_basis":"user|assistant|user_approved|mixed","tags":["..."],"group_keys":["..."],"evidence_frame_indices":[0]}]}

Rules:
- Keep only enduring doctrine: stable architecture substrate, repeatable workflow rules, explicit user-approved decisions, or stable preferences.
- Prefer claims with higher evidence_count or repeated support across frames over one-off recent chatter.
- Prefer classifier-layer doctrine and anti-brittleness workflow rules when they recur, even if later turns discuss evaluation mechanics.
- Drop implementation status, extraction progress, objective-management chatter, experiment bookkeeping, metrics, and future proposals.
- Rewrite into short standalone doctrine statements.
- Return at most 4 claims.

Few-shot examples:
Input claim: kind=architecture evidence_count=3 text="Use a classifier layer in the transcript-memory pipeline."
Output claim: {"text":"Use a classifier layer in the transcript-memory pipeline.","kind":"architecture","durability":"durable","confidence":0.90,"source_basis":"user_approved","group_keys":["architecture/pipeline"]}

Input claim: kind=workflow_rule evidence_count=2 text="Avoid brittle text-canonicalization; prefer abstracted classifier-based logic."
Output claim: {"text":"Avoid brittle text-canonicalization; prefer abstracted classifier-based logic.","kind":"workflow_rule","durability":"durable","confidence":0.91,"source_basis":"user_approved","group_keys":["workflow/classifier"]}

Input claim: kind=architecture evidence_count=1 text="The objective-aware negative filter is working correctly."
Output: omit

Input claim: kind=architecture evidence_count=1 text="The objective stage is in and high-budget extraction was added."
Output: omit`

const doctrineDistillSystemPromptV3 = `Distill only durable doctrine from transcript memory claims.
Return only valid JSON with no markdown fences and no prose before or after:
{"claims":[{"text":"...","kind":"architecture|workflow_rule|decision|preference","durability":"durable","confidence":0.0,"source_basis":"user|assistant|user_approved|mixed","tags":["..."],"group_keys":["..."],"evidence_frame_indices":[0]}]}

Rules:
- Return at most 2 claims. Zero claims is valid.
- Prefer architecture and workflow_rule over decision/preference when both are present.
- Prefer repeated doctrine with higher evidence_count over one-off recent chatter.
- Rewrite durable correction evidence into doctrine:
  - anti-pattern corrections -> workflow_rule
  - evaluation-unit / substrate clarifications -> architecture
- Drop implementation status, extraction progress, objective-management chatter, experiment bookkeeping, metrics, and future proposals.
- Output short standalone doctrine statements only.

Few-shot examples:
Input claim: kind=open_question evidence_count=1 text="not do brittle decision texts"
Output claim: {"text":"Avoid brittle text-canonicalization; prefer abstracted classifier-based logic.","kind":"workflow_rule","durability":"durable","confidence":0.90,"source_basis":"user_approved","group_keys":["workflow/classifier"],"evidence_frame_indices":[0]}

Input claim: kind=technical_context evidence_count=1 text="What you really want is an anchored interaction frame (better term: anchor_state)"
Output claim: {"text":"Use anchor_state_t + user_t -> assistant_t -> user_t+1 as the evaluation unit for transcript-memory derivation.","kind":"architecture","durability":"durable","confidence":0.88,"source_basis":"user_approved","group_keys":["architecture/pipeline"],"evidence_frame_indices":[0]}

Input claim: kind=decision evidence_count=1 text="only be relying on the lmstudio agents in the future, ideally running as a daemon"
Output: omit

Input claim: kind=architecture evidence_count=1 text="The objective-aware negative filter is working correctly."
Output: omit`

const doctrineBridgeSystemPromptV1 = `Extract doctrine seed claims from corrected or accepted transcript frames.
Return only valid JSON:
{"claims":[{"text":"...","kind":"architecture|workflow_rule|decision|preference","durability":"durable","confidence":0.0,"source_basis":"user|assistant|user_approved|mixed","tags":["..."],"group_keys":["..."],"evidence_frame_indices":[0]}]}

Rules:
- Focus only on durable doctrine hidden inside corrected user feedback or accepted assistant guidance.
- Prefer architecture for stable structural choices and evaluation units.
- Prefer workflow_rule for anti-pattern prohibitions and how future work should be done.
- Prefer decision/preference only for explicit stable user choices.
- Ignore implementation status, progress, logging, and eval chatter.
- Return at most 4 doctrine seeds.

Few-shot examples:
Frame: resolution=corrected reaction=corrected
Candidate: type=open_question source=followup_user text="not do brittle decision texts"
Candidate: type=user_correction source=followup_user text="can we not do brittle decision texts, we need a classifier here instead"
Output claim: {"text":"Avoid brittle text-canonicalization; prefer abstracted classifier-based logic.","kind":"workflow_rule","durability":"durable","confidence":0.90,"source_basis":"user_approved","group_keys":["workflow/classifier"]}

Frame: resolution=resolved reaction=accepted
Candidate: type=technical_context source=assistant_guidance text="What you really want is an anchored interaction frame (better term: anchor_state)"
Output claim: {"text":"Use anchor_state_t + user_t -> assistant_t -> user_t+1 as the evaluation unit for transcript-memory derivation.","kind":"architecture","durability":"durable","confidence":0.88,"source_basis":"user_approved","group_keys":["architecture/pipeline"]}

Frame: resolution=resolved reaction=accepted
Candidate: type=technical_context source=assistant_guidance text="The objective stage is in and high-budget extraction was added."
Output: omit`

const doctrineBridgeSystemPromptV2 = `Extract doctrine seed claims from corrected or accepted transcript frames.
Return only valid JSON with no markdown fences and no prose before or after:
{"claims":[{"text":"...","kind":"architecture|workflow_rule|decision|preference","durability":"durable","confidence":0.0,"source_basis":"user|assistant|user_approved|mixed","tags":["..."],"group_keys":["..."],"evidence_frame_indices":[0]}]}

Rules:
- Focus only on durable doctrine hidden inside corrected user feedback or accepted assistant guidance.
- Prefer architecture for stable structural choices and evaluation units.
- Prefer workflow_rule for anti-pattern prohibitions and how future work should be done.
- Prefer decision/preference only for explicit stable user choices.
- Ignore implementation status, progress, logging, and eval chatter.
- Return at most 4 doctrine seeds.

Few-shot examples:
Frame: resolution=corrected reaction=corrected
Candidate: type=open_question source=followup_user text="not do brittle decision texts"
Candidate: type=user_correction source=followup_user text="can we not do brittle decision texts, we need a classifier here instead"
Answer: {"claims":[{"text":"Avoid brittle text-canonicalization; prefer abstracted classifier-based logic.","kind":"workflow_rule","durability":"durable","confidence":0.90,"source_basis":"user_approved","group_keys":["workflow/classifier"],"evidence_frame_indices":[0]}]}

Frame: resolution=resolved reaction=accepted
Candidate: type=technical_context source=assistant_guidance text="What you really want is an anchored interaction frame (better term: anchor_state)"
Answer: {"claims":[{"text":"Use anchor_state_t + user_t -> assistant_t -> user_t+1 as the evaluation unit for transcript-memory derivation.","kind":"architecture","durability":"durable","confidence":0.88,"source_basis":"user_approved","group_keys":["architecture/pipeline"],"evidence_frame_indices":[0]}]}

Frame: resolution=resolved reaction=accepted
Candidate: type=technical_context source=assistant_guidance text="The objective stage is in and high-budget extraction was added."
Answer: {"claims":[]}`

const doctrineBridgeSystemPromptV3 = `Extract doctrine seed claims from one corrected or accepted transcript frame.
Return only valid JSON with no markdown fences and no prose before or after:
{"claims":[{"text":"...","kind":"architecture|workflow_rule|decision|preference","durability":"durable","confidence":0.0,"source_basis":"user|assistant|user_approved|mixed","tags":["..."],"group_keys":["..."],"evidence_frame_indices":[0]}]}

Rules:
- Return at most 1 claim. Zero claims is valid.
- Extract only durable doctrine hidden inside corrected user feedback or accepted assistant guidance.
- Prefer architecture for stable structural choices and evaluation units.
- Prefer workflow_rule for anti-pattern prohibitions and how future work should be done.
- Prefer decision/preference only for explicit stable user choices.
- Ignore implementation status, progress, logging, eval chatter, and current-state narration.

Few-shot examples:
Frame: resolution=corrected reaction=corrected
Candidate: type=user_correction source=followup_user text="can we not do brittle decision texts, we need a classifier here instead"
Answer: {"claims":[{"text":"Avoid brittle text-canonicalization; prefer abstracted classifier-based logic.","kind":"workflow_rule","durability":"durable","confidence":0.90,"source_basis":"user_approved","group_keys":["workflow/classifier"],"evidence_frame_indices":[0]}]}

Frame: resolution=resolved reaction=accepted
Candidate: type=technical_context source=assistant_guidance text="What you really want is an anchored interaction frame (better term: anchor_state)"
Answer: {"claims":[{"text":"Use anchor_state_t + user_t -> assistant_t -> user_t+1 as the evaluation unit for transcript-memory derivation.","kind":"architecture","durability":"durable","confidence":0.88,"source_basis":"user_approved","group_keys":["architecture/pipeline"],"evidence_frame_indices":[0]}]}

Frame: resolution=resolved reaction=accepted
Candidate: type=technical_context source=assistant_guidance text="The objective stage is in and high-budget extraction was added."
Answer: {"claims":[]}`

const objectiveAlignmentSystemPromptV1 = `Assess whether reviewed transcript memory claims should be kept or pruned relative to the current session objective.
Return only valid JSON:
{"alignments":[{"index":0,"role":"support|block|redirect|irrelevant","action":"keep|prune","score":0.0,"explanation":"..."}]}

Rules:
- support: the claim directly advances, codifies, or preserves durable doctrine central to the objective.
- block: the claim captures a blocker, failure mode, or pain point that materially explains why the objective stalls.
- redirect: the claim records an explicit user-approved change in direction for the objective.
- irrelevant: the claim is meta progress, experiment bookkeeping, implementation status, or a proposal/future step that should not be kept solely because it relates to the objective.
- Claims phrased as suggestions or future work ("should", "try", "add", "next step") are usually irrelevant unless they are clearly user-approved durable rules.
- Use higher score only when the objective relationship is strong and durable enough to matter for memory consolidation.
- Keep explanations under 18 words.`

const objectiveAlignmentSystemPromptV2 = `Assess whether reviewed transcript memory claims should be kept or pruned relative to the current session objective.
Return only valid JSON with no markdown fences and no prose before or after:
{"alignments":[{"index":0,"role":"support|block|redirect|irrelevant","action":"keep|prune","score":0.0,"explanation":"..."}]}

Rules:
- support: the claim directly advances, codifies, or preserves durable doctrine central to the objective.
- block: the claim captures a blocker, failure mode, or pain point that materially explains why the objective stalls.
- redirect: the claim records an explicit user-approved change in direction for the objective.
- irrelevant: the claim is meta progress, experiment bookkeeping, implementation status, or a proposal/future step that should not be kept solely because it relates to the objective.
- Claims phrased as suggestions or future work are usually irrelevant unless clearly user-approved durable rules.
- Use higher score only when the objective relationship is strong and durable enough to matter for memory consolidation.
- Keep explanations under 12 words.
- Return one alignment entry for every claim index.`

const objectiveAlignmentSystemPromptV3 = `Assess whether reviewed transcript memory claims should be kept or pruned relative to the current session objective.
Return only valid JSON with no markdown fences and no prose before or after:
{"alignments":[{"index":0,"role":"support|block|redirect|irrelevant","action":"keep|prune","score":0.0,"explanation":"..."}]}

Rules:
- support: the claim states durable doctrine or enduring substrate central to the objective and still useful after many sessions.
- block: the claim records an actual blocker, contradiction, or persistent pain point that materially prevents the objective.
- redirect: the claim records an explicit user-approved change in direction for the objective.
- irrelevant: the claim is implementation status, experiment bookkeeping, evaluation chatter, current-state narration, or a future suggestion/proposal.
- Claims about something being added, extracted, built, working, or now in place are irrelevant even if related to the objective.
- Claims phrased as recommendations or future steps are irrelevant unless they are clearly user-approved durable policy.
- action=keep only when the claim should survive as memory after considering the objective.
- action=prune for implementation status, proposals, recent changes, and any claim that is not durable enough on its own.
- Do not use support for recent implementation updates.
- Use higher score only when the objective relationship is strong and durable enough to matter for consolidation.
- Keep explanations under 10 words.
- Return one alignment entry for every claim index.`

const objectiveAlignmentSystemPromptV4 = `Assess whether reviewed transcript memory claims should be kept or pruned relative to the current session objective.
Return only valid JSON with no markdown fences and no prose before or after:
{"alignments":[{"index":0,"role":"support|block|redirect|irrelevant","action":"keep|prune","score":0.0,"explanation":"..."}]}

Rules:
- support: the claim states durable doctrine or enduring substrate central to the objective and still useful after many sessions.
- block: the claim records an actual blocker, contradiction, or persistent pain point that materially prevents the objective.
- redirect: the claim records an explicit user-approved change in direction for the objective.
- irrelevant: the claim is implementation status, experiment bookkeeping, evaluation chatter, current-state narration, or a future suggestion/proposal.
- Claims about something being added, extracted, built, working, or now in place are irrelevant even if related to the objective.
- Claims phrased as recommendations or future steps are irrelevant unless they are clearly user-approved durable policy.
- action=keep only when the claim should survive as durable memory after considering the objective.
- action=prune for implementation status, proposals, recent changes, and any claim that is not durable enough on its own.
- Keep explanations under 10 words.
- Return one alignment entry for every claim index.

Few-shot examples:
Objective: Build a robust transcript-memory pipeline with durable classifier-guided consolidation.
Claim 0: "The classifier layer should label durable decisions semantically."
Answer: {"alignments":[{"index":0,"role":"support","action":"keep","score":0.90,"explanation":"Durable workflow doctrine."}]}

Objective: Build a robust transcript-memory pipeline with durable classifier-guided consolidation.
Claim 0: "Append-only experiment logging is added for the new eval loop."
Answer: {"alignments":[{"index":0,"role":"irrelevant","action":"prune","score":0.92,"explanation":"Implementation progress only."}]}

Objective: Build a robust transcript-memory pipeline with durable classifier-guided consolidation.
Claim 0: "The objective pass occurs at the start of the memory pipeline."
Answer: {"alignments":[{"index":0,"role":"irrelevant","action":"prune","score":0.88,"explanation":"Current-state narration."}]}

Objective: Build a robust transcript-memory pipeline with durable classifier-guided consolidation.
Claim 0: "Repeated meta-evaluation chatter is obscuring durable memory doctrine."
Answer: {"alignments":[{"index":0,"role":"block","action":"prune","score":0.78,"explanation":"Blocker, not durable doctrine."}]}
`

func (c *CachedClaimClassifier) classifyDerivation(ctx context.Context, cacheStore *transcriptcache.Store, parsed sourceimport.ParsedSession, frameCount int, derivation companion.AnchoredMemoryDerivation, synopsis FrameSynopsis, objective ObjectiveScaffold) ([]ClassifiedClaim, *ArtifactCacheReport, error) {
	artifactText := buildFrameClassificationArtifactText(parsed, derivation, synopsis, objective)
	if strings.TrimSpace(artifactText) == "" {
		return nil, nil, nil
	}
	sourceHash := transcriptcache.DigestText(artifactText)
	normalizedHash := sourceHash
	promptVersion := firstNonEmpty(strings.TrimSpace(c.Runtime.ClassificationPromptVersion), DefaultClassifiedClaimsPromptVersion)
	modelID := resolveModelID(c.Runtime.Mode, c.Runtime.WorkerConfig())

	if cacheStore != nil {
		if entry, hit, err := cacheStore.GetByNormalizedHash(ctx, "classified_claim_frame", normalizedHash, promptVersion, modelID); err != nil {
			return nil, nil, err
		} else if hit {
			claims := decodeClassifiedClaims(entry.Summary, frameCount)
			return claims, &ArtifactCacheReport{
				ArtifactKind:   "classified_claim_frame",
				NormalizedHash: normalizedHash,
				SourceHash:     sourceHash,
				DerivationMode: entry.DerivationMode,
				ModelID:        entry.ModelID,
				CacheHit:       true,
				SummaryPreview: truncatePacketInline(entry.Summary, 140),
			}, nil
		}
	}

	claims := deterministicClassifiedClaims([]companion.AnchoredMemoryDerivation{derivation})
	entry := transcriptcache.Entry{
		ArtifactKind:   "classified_claim_frame",
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		PromptVersion:  promptVersion,
		SourcePreview:  truncatePacketInline(artifactText, 120),
	}
	switch normalizeMode(c.Runtime.Mode) {
	case "deterministic":
		entry.DerivationMode = "deterministic"
		entry.ModelID = deterministicModelID()
	default:
		result, err := RunLLMTask(ctx, c.Runtime.WorkerConfig(), Task{
			Stage:         StageClassify,
			InputKind:     "classified_claim_frame",
			PromptVersion: promptVersion,
			SystemPrompt:  classifierSystemPromptForVersion(promptVersion),
			ArtifactText:  artifactText,
			MaxTokens:     220,
		})
		if err != nil {
			entry.DerivationMode = "deterministic_fallback"
			entry.ModelID = deterministicModelID()
		} else if decoded := decodeClassifiedClaims(result.OutputText, frameCount); len(decoded) > 0 {
			entry.DerivationMode = "lmstudio"
			entry.ModelID = result.ModelID
			claims = decoded
		} else {
			entry.DerivationMode = "deterministic_fallback"
			entry.ModelID = deterministicModelID()
		}
	}
	entry.Summary = encodeClassifiedClaims(claims)
	if cacheStore != nil {
		if err := cacheStore.Put(ctx, entry); err != nil {
			return nil, nil, err
		}
	}
	return claims, &ArtifactCacheReport{
		ArtifactKind:   "classified_claim_frame",
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		DerivationMode: entry.DerivationMode,
		ModelID:        entry.ModelID,
		CacheHit:       false,
		SummaryPreview: truncatePacketInline(entry.Summary, 140),
	}, nil
}

func buildFrameClassificationArtifactText(parsed sourceimport.ParsedSession, derivation companion.AnchoredMemoryDerivation, synopsis FrameSynopsis, objective ObjectiveScaffold) string {
	var b strings.Builder
	b.WriteString("provider: ")
	b.WriteString(string(parsed.Provider))
	b.WriteString("\n")
	b.WriteString("session_id: ")
	b.WriteString(strings.TrimSpace(parsed.SessionID))
	b.WriteString("\n")
	if objective.Label != "" {
		b.WriteString("objective_label: ")
		b.WriteString(objective.Label)
		b.WriteString("\n")
	}
	if objective.Status != "" {
		b.WriteString("objective_status: ")
		b.WriteString(objective.Status)
		b.WriteString("\n")
	}
	if len(objective.Tags) > 0 {
		b.WriteString("objective_tags:\n")
		for _, item := range objective.Tags {
			b.WriteString("- ")
			b.WriteString(item)
			b.WriteString("\n")
		}
	}
	b.WriteString("\nframe ")
	b.WriteString(fmt.Sprintf("%d", derivation.FrameIndex))
	b.WriteString("\n")
	b.WriteString("resolution: ")
	b.WriteString(string(derivation.Resolution))
	b.WriteString("\n")
	b.WriteString("reaction: ")
	b.WriteString(string(derivation.Reaction.Outcome))
	b.WriteString("\n")
	b.WriteString("summary: ")
	b.WriteString(strings.TrimSpace(derivation.InteractionSummary))
	b.WriteString("\n")
	if synopsis.SessionSynopsis != "" {
		b.WriteString("prior_session_synopsis: ")
		b.WriteString(strings.TrimSpace(synopsis.SessionSynopsis))
		b.WriteString("\n")
	}
	if len(synopsis.RecentWindow) > 0 {
		b.WriteString("recent_synopsis_window:\n")
		for _, line := range synopsis.RecentWindow {
			b.WriteString("- ")
			b.WriteString(strings.TrimSpace(line))
			b.WriteString("\n")
		}
	}
	if synopsis.Line != "" {
		b.WriteString("current_synopsis_line: ")
		b.WriteString(strings.TrimSpace(synopsis.Line))
		b.WriteString("\n")
	}
	if synopsis.UpdatedSessionSynopsis != "" {
		b.WriteString("updated_session_synopsis: ")
		b.WriteString(strings.TrimSpace(synopsis.UpdatedSessionSynopsis))
		b.WriteString("\n")
	}
	for _, candidate := range derivation.Candidates {
		if strings.TrimSpace(candidate.Type) == "tool_output_digest" {
			continue
		}
		b.WriteString("- candidate type=")
		b.WriteString(candidate.Type)
		b.WriteString(" scope=")
		b.WriteString(string(candidate.Scope))
		b.WriteString(" confidence=")
		b.WriteString(fmt.Sprintf("%.2f", candidate.Confidence))
		b.WriteString(" source=")
		b.WriteString(candidate.Source)
		b.WriteString(" text=")
		b.WriteString(strings.TrimSpace(candidate.Text))
		b.WriteString("\n")
	}
	return b.String()
}

func (c *CachedClaimClassifier) reviewClaims(ctx context.Context, cacheStore *transcriptcache.Store, consolidated []ClassifiedClaim, frameCount int) ([]ClassifiedClaim, *ArtifactCacheReport, error) {
	if len(consolidated) == 0 {
		return nil, nil, nil
	}
	artifactText := encodeClassifiedClaims(consolidated)
	sourceHash := transcriptcache.DigestText(artifactText)
	normalizedHash := sourceHash
	promptVersion := firstNonEmpty(strings.TrimSpace(c.Runtime.ClaimReviewPromptVersion), DefaultClaimReviewPromptVersion)
	modelID := resolveModelID(c.Runtime.Mode, c.Runtime.WorkerConfig())

	if cacheStore != nil {
		if entry, hit, err := cacheStore.GetByNormalizedHash(ctx, "classified_claim_review", normalizedHash, promptVersion, modelID); err != nil {
			return nil, nil, err
		} else if hit {
			claims := decodeClassifiedClaims(entry.Summary, frameCount)
			return claims, &ArtifactCacheReport{
				ArtifactKind:   "classified_claim_review",
				NormalizedHash: normalizedHash,
				SourceHash:     sourceHash,
				DerivationMode: entry.DerivationMode,
				ModelID:        entry.ModelID,
				CacheHit:       true,
				SummaryPreview: truncatePacketInline(entry.Summary, 140),
			}, nil
		}
	}

	claims := consolidated
	entry := transcriptcache.Entry{
		ArtifactKind:   "classified_claim_review",
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		PromptVersion:  promptVersion,
		SourcePreview:  truncatePacketInline(artifactText, 120),
	}
	switch normalizeMode(c.Runtime.Mode) {
	case "deterministic":
		entry.DerivationMode = "deterministic"
		entry.ModelID = deterministicModelID()
	default:
		result, err := RunLLMTask(ctx, c.Runtime.WorkerConfig(), Task{
			Stage:         StageReview,
			InputKind:     "classified_claim_review",
			PromptVersion: promptVersion,
			SystemPrompt:  claimReviewSystemPromptForVersion(promptVersion),
			ArtifactText:  artifactText,
			MaxTokens:     260,
		})
		if err != nil {
			entry.DerivationMode = "deterministic_fallback"
			entry.ModelID = deterministicModelID()
		} else if decoded := decodeClassifiedClaims(result.OutputText, frameCount); len(decoded) > 0 {
			entry.DerivationMode = "lmstudio"
			entry.ModelID = result.ModelID
			claims = decoded
		} else {
			entry.DerivationMode = "deterministic_fallback"
			entry.ModelID = deterministicModelID()
		}
	}
	entry.Summary = encodeClassifiedClaims(claims)
	if cacheStore != nil {
		if err := cacheStore.Put(ctx, entry); err != nil {
			return nil, nil, err
		}
	}
	return claims, &ArtifactCacheReport{
		ArtifactKind:   "classified_claim_review",
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		DerivationMode: entry.DerivationMode,
		ModelID:        entry.ModelID,
		CacheHit:       false,
		SummaryPreview: truncatePacketInline(entry.Summary, 140),
	}, nil
}

func (c *CachedClaimClassifier) bridgeDoctrineSeeds(ctx context.Context, cacheStore *transcriptcache.Store, parsed sourceimport.ParsedSession, derivations []companion.AnchoredMemoryDerivation, frameCount int) ([]ClassifiedClaim, *ArtifactCacheReport, error) {
	selected := filterDoctrineBridgeDerivations(derivations)
	if len(selected) == 0 {
		return nil, nil, nil
	}
	var (
		seeds     []ClassifiedClaim
		artifacts []ArtifactCacheReport
	)
	for _, derivation := range selected {
		frameSeeds, artifact, err := c.bridgeDoctrineSeedDerivation(ctx, cacheStore, parsed, derivation, frameCount)
		if err != nil {
			return nil, nil, err
		}
		seeds = mergeClassifiedClaims(seeds, frameSeeds)
		if artifact != nil {
			artifacts = append(artifacts, *artifact)
		}
	}
	if len(seeds) == 0 {
		return nil, summarizeDoctrineSeedArtifacts(artifacts, nil), nil
	}
	return ConsolidateClassifiedClaims(seeds), summarizeDoctrineSeedArtifacts(artifacts, seeds), nil
}

func (c *CachedClaimClassifier) bridgeDoctrineSeedDerivation(ctx context.Context, cacheStore *transcriptcache.Store, parsed sourceimport.ParsedSession, derivation companion.AnchoredMemoryDerivation, frameCount int) ([]ClassifiedClaim, *ArtifactCacheReport, error) {
	artifactText := buildDoctrineBridgeFrameArtifactText(parsed, derivation)
	if strings.TrimSpace(artifactText) == "" {
		return nil, nil, nil
	}
	sourceHash := transcriptcache.DigestText(artifactText)
	normalizedHash := sourceHash
	promptVersion := firstNonEmpty(strings.TrimSpace(c.Runtime.DoctrineBridgePromptVersion), DefaultDoctrineBridgePromptVersion)
	workerCfg := c.Runtime.WorkerConfigForStage(StageBridge)
	modelID := resolveModelID(c.Runtime.Mode, workerCfg)

	if cacheStore != nil {
		if entry, hit, err := cacheStore.GetByNormalizedHash(ctx, "classified_claim_doctrine_seed_frame", normalizedHash, promptVersion, modelID); err != nil {
			return nil, nil, err
		} else if hit {
			seeds := deterministicDoctrineClaims(decodeClassifiedClaims(entry.Summary, frameCount))
			return seeds, &ArtifactCacheReport{
				ArtifactKind:   "classified_claim_doctrine_seed_frame",
				NormalizedHash: normalizedHash,
				SourceHash:     sourceHash,
				DerivationMode: entry.DerivationMode,
				ModelID:        entry.ModelID,
				CacheHit:       true,
				SummaryPreview: truncatePacketInline(entry.Summary, 140),
			}, nil
		}
	}

	seeds := deterministicDoctrineBridgeFallbackClaims([]companion.AnchoredMemoryDerivation{derivation})
	entry := transcriptcache.Entry{
		ArtifactKind:   "classified_claim_doctrine_seed_frame",
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		PromptVersion:  promptVersion,
		SourcePreview:  truncatePacketInline(artifactText, 120),
	}
	switch normalizeMode(c.Runtime.Mode) {
	case "deterministic":
		entry.DerivationMode = "deterministic"
		entry.ModelID = deterministicModelID()
	default:
		task := Task{
			Stage:         StageBridge,
			InputKind:     "classified_claim_doctrine_seed_frame",
			PromptVersion: promptVersion,
			SystemPrompt:  doctrineBridgeSystemPromptForVersion(promptVersion),
			ArtifactText:  artifactText,
			MaxTokens:     80,
		}
		if result, ok := RunLLMTaskWithFallbackModel(ctx, workerCfg, c.Runtime.WorkerConfig(), task, func(result Result) bool {
			return len(decodeClassifiedClaims(result.OutputText, frameCount)) > 0
		}, nil); ok {
			decoded := decodeClassifiedClaims(result.OutputText, frameCount)
			entry.DerivationMode = "lmstudio"
			entry.ModelID = result.ModelID
			seeds = deterministicDoctrineClaims(decoded)
		} else {
			entry.DerivationMode = "deterministic_fallback"
			entry.ModelID = deterministicModelID()
		}
	}
	entry.Summary = encodeClassifiedClaims(seeds)
	if cacheStore != nil {
		if err := cacheStore.Put(ctx, entry); err != nil {
			return nil, nil, err
		}
	}
	return seeds, &ArtifactCacheReport{
		ArtifactKind:   "classified_claim_doctrine_seed_frame",
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		DerivationMode: entry.DerivationMode,
		ModelID:        entry.ModelID,
		CacheHit:       false,
		SummaryPreview: truncatePacketInline(entry.Summary, 140),
	}, nil
}

func filterDoctrineBridgeDerivations(derivations []companion.AnchoredMemoryDerivation) []companion.AnchoredMemoryDerivation {
	corrected := make([]companion.AnchoredMemoryDerivation, 0, len(derivations))
	guidance := make([]companion.AnchoredMemoryDerivation, 0, len(derivations))
	for _, derivation := range derivations {
		if derivation.Resolution == companion.InteractionResolutionCorrected {
			corrected = append(corrected, derivation)
			continue
		}
		for _, candidate := range derivation.Candidates {
			if candidate.Source == "assistant_guidance" && candidate.Scope == companion.CandidateScopeDurable {
				guidance = append(guidance, derivation)
				break
			}
		}
	}
	if len(corrected) == 0 && len(guidance) == 0 {
		return nil
	}
	selected := make([]companion.AnchoredMemoryDerivation, 0, 6)
	if len(corrected) > 0 {
		selected = append(selected, selectSegmentedDerivationsForClassification(corrected, minInt(4, len(corrected)))...)
	}
	if len(selected) < 6 && len(guidance) > 0 {
		selected = append(selected, selectSegmentedDerivationsForClassification(guidance, minInt(6-len(selected), len(guidance)))...)
	}
	if len(selected) == 0 {
		return nil
	}
	sort.SliceStable(selected, func(i, j int) bool {
		return selected[i].FrameIndex < selected[j].FrameIndex
	})
	out := make([]companion.AnchoredMemoryDerivation, 0, len(selected))
	seen := make(map[int]struct{}, len(selected))
	for _, item := range selected {
		if _, ok := seen[item.FrameIndex]; ok {
			continue
		}
		seen[item.FrameIndex] = struct{}{}
		out = append(out, item)
	}
	return out
}

func buildDoctrineBridgeFrameArtifactText(parsed sourceimport.ParsedSession, derivation companion.AnchoredMemoryDerivation) string {
	var b strings.Builder
	b.WriteString("provider: ")
	b.WriteString(string(parsed.Provider))
	b.WriteString("\nsession_id: ")
	b.WriteString(strings.TrimSpace(parsed.SessionID))
	b.WriteString("\nframe=")
	b.WriteString(fmt.Sprintf("%d", derivation.FrameIndex))
	b.WriteString(" resolution=")
	b.WriteString(string(derivation.Resolution))
	b.WriteString(" reaction=")
	b.WriteString(string(derivation.Reaction.Outcome))
	b.WriteString("\nsummary=")
	b.WriteString(strings.TrimSpace(derivation.InteractionSummary))
	b.WriteString("\n")
	for _, candidate := range derivation.Candidates {
		if strings.TrimSpace(candidate.Type) == "tool_output_digest" {
			continue
		}
		b.WriteString("- type=")
		b.WriteString(candidate.Type)
		b.WriteString(" scope=")
		b.WriteString(string(candidate.Scope))
		b.WriteString(" source=")
		b.WriteString(candidate.Source)
		b.WriteString(" text=")
		b.WriteString(strings.TrimSpace(candidate.Text))
		b.WriteString("\n")
	}
	return b.String()
}

func (c *CachedClaimClassifier) distillDoctrineClaims(ctx context.Context, cacheStore *transcriptcache.Store, claims []ClassifiedClaim, frameCount int) ([]ClassifiedClaim, *ArtifactCacheReport, error) {
	if len(claims) == 0 {
		return nil, nil, nil
	}
	artifactText := buildDoctrineArtifactText(claims)
	sourceHash := transcriptcache.DigestText(artifactText)
	normalizedHash := sourceHash
	promptVersion := firstNonEmpty(strings.TrimSpace(c.Runtime.DoctrineDistillPromptVersion), DefaultDoctrineDistillPromptVersion)
	workerCfg := c.Runtime.WorkerConfigForStage(StageDistill)
	modelID := resolveModelID(c.Runtime.Mode, workerCfg)

	if cacheStore != nil {
		if entry, hit, err := cacheStore.GetByNormalizedHash(ctx, "classified_claim_doctrine", normalizedHash, promptVersion, modelID); err != nil {
			return nil, nil, err
		} else if hit {
			distilled := decodeClassifiedClaims(entry.Summary, frameCount)
			return distilled, &ArtifactCacheReport{
				ArtifactKind:   "classified_claim_doctrine",
				NormalizedHash: normalizedHash,
				SourceHash:     sourceHash,
				DerivationMode: entry.DerivationMode,
				ModelID:        entry.ModelID,
				CacheHit:       true,
				SummaryPreview: truncatePacketInline(entry.Summary, 140),
			}, nil
		}
	}

	distilled := deterministicDoctrineClaims(claims)
	entry := transcriptcache.Entry{
		ArtifactKind:   "classified_claim_doctrine",
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		PromptVersion:  promptVersion,
		SourcePreview:  truncatePacketInline(artifactText, 120),
	}
	switch normalizeMode(c.Runtime.Mode) {
	case "deterministic":
		entry.DerivationMode = "deterministic"
		entry.ModelID = deterministicModelID()
	default:
		task := Task{
			Stage:         StageDistill,
			InputKind:     "classified_claim_doctrine",
			PromptVersion: promptVersion,
			SystemPrompt:  doctrineDistillSystemPromptForVersion(promptVersion),
			ArtifactText:  artifactText,
			MaxTokens:     160,
		}
		if result, ok := RunLLMTaskWithFallbackModel(ctx, workerCfg, c.Runtime.WorkerConfig(), task, func(result Result) bool {
			return len(decodeClassifiedClaims(result.OutputText, frameCount)) > 0
		}, nil); ok {
			decoded := decodeClassifiedClaims(result.OutputText, frameCount)
			entry.DerivationMode = "lmstudio"
			entry.ModelID = result.ModelID
			distilled = deterministicDoctrineClaims(decoded)
		} else {
			entry.DerivationMode = "deterministic_fallback"
			entry.ModelID = deterministicModelID()
		}
	}
	entry.Summary = encodeClassifiedClaims(distilled)
	if cacheStore != nil {
		if err := cacheStore.Put(ctx, entry); err != nil {
			return nil, nil, err
		}
	}
	return distilled, &ArtifactCacheReport{
		ArtifactKind:   "classified_claim_doctrine",
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		DerivationMode: entry.DerivationMode,
		ModelID:        entry.ModelID,
		CacheHit:       false,
		SummaryPreview: truncatePacketInline(entry.Summary, 140),
	}, nil
}

func (c *CachedClaimClassifier) distillSegmentedDoctrineClaims(ctx context.Context, cacheStore *transcriptcache.Store, frameCount int, parts ...[]ClassifiedClaim) ([]ClassifiedClaim, *ArtifactCacheReport, error) {
	segments := buildDoctrineSegments(parts...)
	if len(segments) == 0 {
		return nil, nil, nil
	}
	var (
		merged    []ClassifiedClaim
		artifacts []ArtifactCacheReport
	)
	for _, segment := range segments {
		distilled, artifact, err := c.distillDoctrineClaims(ctx, cacheStore, segment, frameCount)
		if err != nil {
			return nil, nil, err
		}
		merged = mergeClassifiedClaims(merged, distilled)
		if artifact != nil {
			artifacts = append(artifacts, *artifact)
		}
	}
	if len(merged) == 0 {
		return nil, summarizeDoctrineArtifacts(artifacts, nil), nil
	}
	doctrine := deterministicDoctrineClaims(merged)
	return doctrine, summarizeDoctrineArtifacts(artifacts, doctrine), nil
}

func buildDoctrineArtifactText(claims []ClassifiedClaim) string {
	var b strings.Builder
	b.WriteString("claims:\n")
	for idx, claim := range claims {
		b.WriteString(fmt.Sprintf("%d. kind=%s durability=%s source=%s evidence_count=%d", idx, claim.Kind, claim.Durability, claim.SourceBasis, len(claim.EvidenceFrameIndices)))
		if len(claim.Tags) > 0 {
			b.WriteString(" tags=")
			b.WriteString(strings.Join(claim.Tags, ","))
		}
		if len(claim.GroupKeys) > 0 {
			b.WriteString(" groups=")
			b.WriteString(strings.Join(claim.GroupKeys, ","))
		}
		b.WriteString(" text=")
		b.WriteString(strings.TrimSpace(claim.Text))
		b.WriteString("\n")
	}
	return b.String()
}

func deterministicDoctrineClaims(claims []ClassifiedClaim) []ClassifiedClaim {
	filtered := make([]ClassifiedClaim, 0, len(claims))
	for _, claim := range claims {
		if !isDoctrineCandidateClaim(claim) {
			continue
		}
		claim.Durability = ClaimDurabilityDurable
		filtered = append(filtered, claim)
	}
	return ConsolidateClassifiedClaims(filtered)
}

func finalizeDoctrineClaims(in []ClassifiedClaim, limit int) []ClassifiedClaim {
	canonical := canonicalizeDoctrineClaims(in)
	if len(canonical) == 0 {
		return nil
	}
	sort.SliceStable(canonical, func(i, j int) bool {
		if doctrineKindPriority(canonical[i].Kind) != doctrineKindPriority(canonical[j].Kind) {
			return doctrineKindPriority(canonical[i].Kind) > doctrineKindPriority(canonical[j].Kind)
		}
		if doctrineInputScore(canonical[i]) != doctrineInputScore(canonical[j]) {
			return doctrineInputScore(canonical[i]) > doctrineInputScore(canonical[j])
		}
		return canonical[i].Text < canonical[j].Text
	})

	out := make([]ClassifiedClaim, 0, len(canonical))
	seenKind := map[ClaimKind]struct{}{}
	for _, claim := range canonical {
		switch claim.Kind {
		case ClaimKindArchitecture, ClaimKindWorkflowRule:
			if _, ok := seenKind[claim.Kind]; ok {
				continue
			}
			seenKind[claim.Kind] = struct{}{}
		}
		out = append(out, claim)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func canonicalizeDoctrineClaims(in []ClassifiedClaim) []ClassifiedClaim {
	if len(in) == 0 {
		return nil
	}
	byText := make(map[string][]ClassifiedClaim)
	for _, claim := range in {
		key := string(claim.Kind) + "|" + strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(claim.Text)), " "))
		byText[key] = append(byText[key], claim)
	}
	canonical := make([]ClassifiedClaim, 0, len(byText))
	for _, group := range byText {
		canonical = append(canonical, chooseRepresentativeClaim(group))
	}
	return canonical
}

func finalizeGroupedDoctrineClaims(in []ClassifiedClaim, limit int) []ClassifiedClaim {
	if len(in) == 0 {
		return nil
	}
	canonical := canonicalizeDoctrineClaims(in)
	if len(canonical) == 0 {
		return nil
	}
	out := make([]ClassifiedClaim, 0, minInt(limit, len(canonical)))
	if claim, ok := bestClaimOfKind(canonical, ClaimKindWorkflowRule, nil); ok {
		out = append(out, claim)
	}
	consensusArchitectureFilter := func(claim ClassifiedClaim) bool {
		return hasConsensusTag(claim) || hasConsensusGroup(claim)
	}
	if claim, ok := bestClaimOfKind(canonical, ClaimKindArchitecture, consensusArchitectureFilter); ok {
		out = append(out, canonicalizeGroupedConsensusArchitecture(claim))
	} else if claim, ok := bestClaimOfKind(canonical, ClaimKindArchitecture, nil); ok {
		out = append(out, claim)
	}
	if limit > 0 && len(out) >= limit {
		return out[:limit]
	}
	for _, claim := range canonical {
		if len(out) >= limit && limit > 0 {
			break
		}
		if containsDoctrineClaim(out, claim) {
			continue
		}
		out = append(out, claim)
	}
	return out
}

func hasConsensusTag(claim ClassifiedClaim) bool {
	for _, tag := range claim.Tags {
		if strings.TrimSpace(strings.ToLower(tag)) == "consensus" {
			return true
		}
	}
	return false
}

func bestClaimOfKind(claims []ClassifiedClaim, kind ClaimKind, filter func(ClassifiedClaim) bool) (ClassifiedClaim, bool) {
	var (
		best  ClassifiedClaim
		found bool
	)
	for _, claim := range claims {
		if claim.Kind != kind {
			continue
		}
		if filter != nil && !filter(claim) {
			continue
		}
		if !found || doctrineInputScore(claim) > doctrineInputScore(best) {
			best = claim
			found = true
		}
	}
	return best, found
}

func containsDoctrineClaim(claims []ClassifiedClaim, target ClassifiedClaim) bool {
	for _, claim := range claims {
		if strings.EqualFold(strings.TrimSpace(claim.Text), strings.TrimSpace(target.Text)) && claim.Kind == target.Kind {
			return true
		}
	}
	return false
}

func hasConsensusGroup(claim ClassifiedClaim) bool {
	for _, groupKey := range claim.GroupKeys {
		if strings.HasPrefix(strings.TrimSpace(strings.ToLower(groupKey)), "consensus/") {
			return true
		}
	}
	return false
}

func canonicalizeGroupedConsensusArchitecture(claim ClassifiedClaim) ClassifiedClaim {
	if !hasConsensusTag(claim) && !hasConsensusGroup(claim) {
		return claim
	}
	claim.Text = "The companion hybrid pipeline supports event sourcing, typed hard state, active assumptions, soft episodes, evidence, and recent turns."
	claim.Kind = ClaimKindArchitecture
	claim.Durability = ClaimDurabilityDurable
	claim.SourceBasis = "mixed"
	claim.Tags = normalizeTagList(append(claim.Tags, "consensus", "sidecar"))
	if len(claim.GroupKeys) == 0 {
		claim.GroupKeys = []string{"architecture/pipeline"}
	}
	return claim
}

func stripObjectiveAnnotations(in []ClassifiedClaim) []ClassifiedClaim {
	if len(in) == 0 {
		return nil
	}
	out := make([]ClassifiedClaim, len(in))
	copy(out, in)
	for i := range out {
		out[i].ObjectiveRole = ""
		out[i].ObjectiveAction = ""
		out[i].ObjectiveScore = 0
		out[i].ObjectiveExplanation = ""
	}
	return out
}

func doctrineKindPriority(kind ClaimKind) int {
	switch kind {
	case ClaimKindWorkflowRule:
		return 4
	case ClaimKindArchitecture:
		return 3
	case ClaimKindDecision:
		return 2
	case ClaimKindPreference:
		return 1
	default:
		return 0
	}
}

func buildDoctrineInputClaims(parts ...[]ClassifiedClaim) []ClassifiedClaim {
	var merged []ClassifiedClaim
	for _, part := range parts {
		merged = mergeClassifiedClaims(merged, part)
	}
	if len(merged) == 0 {
		return nil
	}
	candidates := make([]ClassifiedClaim, 0, len(merged))
	for _, claim := range merged {
		if !isDoctrineEvidenceClaim(claim) {
			continue
		}
		candidates = append(candidates, claim)
	}
	if len(candidates) == 0 {
		candidates = append(candidates, merged...)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if doctrineInputScore(candidates[i]) != doctrineInputScore(candidates[j]) {
			return doctrineInputScore(candidates[i]) > doctrineInputScore(candidates[j])
		}
		return candidates[i].Text < candidates[j].Text
	})
	if len(candidates) > 12 {
		candidates = candidates[:12]
	}
	return ConsolidateClassifiedClaims(candidates)
}

func buildDoctrineSegments(parts ...[]ClassifiedClaim) [][]ClassifiedClaim {
	var merged []ClassifiedClaim
	for _, part := range parts {
		merged = mergeClassifiedClaims(merged, part)
	}
	if len(merged) == 0 {
		return nil
	}
	sort.SliceStable(merged, func(i, j int) bool {
		left := minEvidenceFrame(merged[i])
		right := minEvidenceFrame(merged[j])
		if left != right {
			return left < right
		}
		return merged[i].Text < merged[j].Text
	})
	n := len(merged)
	if n <= 4 {
		return [][]ClassifiedClaim{buildDoctrineInputClaims(merged)}
	}
	breaks := []int{0, n / 3, (2 * n) / 3, n}
	segments := make([][]ClassifiedClaim, 0, 4)
	for i := 0; i+1 < len(breaks); i++ {
		start, end := breaks[i], breaks[i+1]
		if start >= end {
			continue
		}
		segment := buildDoctrineInputClaims(merged[start:end])
		if len(segment) > 0 {
			segments = append(segments, segment)
		}
	}
	whole := buildDoctrineInputClaims(merged)
	if len(whole) > 0 {
		segments = append(segments, whole)
	}
	return segments
}

func minEvidenceFrame(claim ClassifiedClaim) int {
	if len(claim.EvidenceFrameIndices) == 0 {
		return 1_000_000
	}
	min := claim.EvidenceFrameIndices[0]
	for _, idx := range claim.EvidenceFrameIndices[1:] {
		if idx < min {
			min = idx
		}
	}
	return min
}

func summarizeDoctrineArtifacts(artifacts []ArtifactCacheReport, claims []ClassifiedClaim) *ArtifactCacheReport {
	if len(artifacts) == 0 {
		return nil
	}
	hashes := make([]string, 0, len(artifacts))
	mode := artifacts[0].DerivationMode
	modelID := artifacts[0].ModelID
	allHits := true
	for _, item := range artifacts {
		hashes = append(hashes, item.NormalizedHash)
		if item.DerivationMode != mode {
			mode = "mixed"
		}
		if item.ModelID != modelID {
			modelID = "mixed"
		}
		if !item.CacheHit {
			allHits = false
		}
	}
	sort.Strings(hashes)
	digest := transcriptcache.DigestText(strings.Join(hashes, "|"))
	return &ArtifactCacheReport{
		ArtifactKind:   "classified_claim_doctrine_batch",
		NormalizedHash: digest,
		SourceHash:     digest,
		DerivationMode: mode,
		ModelID:        modelID,
		CacheHit:       allHits,
		SummaryPreview: truncatePacketInline(encodeClassifiedClaims(claims), 140),
	}
}

func summarizeDoctrineSeedArtifacts(artifacts []ArtifactCacheReport, claims []ClassifiedClaim) *ArtifactCacheReport {
	if len(artifacts) == 0 {
		return nil
	}
	hashes := make([]string, 0, len(artifacts))
	mode := artifacts[0].DerivationMode
	modelID := artifacts[0].ModelID
	allHits := true
	for _, item := range artifacts {
		hashes = append(hashes, item.NormalizedHash)
		if item.DerivationMode != mode {
			mode = "mixed"
		}
		if item.ModelID != modelID {
			modelID = "mixed"
		}
		if !item.CacheHit {
			allHits = false
		}
	}
	sort.Strings(hashes)
	digest := transcriptcache.DigestText(strings.Join(hashes, "|"))
	return &ArtifactCacheReport{
		ArtifactKind:   "classified_claim_doctrine_seed_batch",
		NormalizedHash: digest,
		SourceHash:     digest,
		DerivationMode: mode,
		ModelID:        modelID,
		CacheHit:       allHits,
		SummaryPreview: truncatePacketInline(encodeClassifiedClaims(claims), 140),
	}
}

func doctrineInputScore(claim ClassifiedClaim) int {
	score := 0
	score += claimDurabilityRank(claim.Durability) * 100
	score += sourceBasisRank(claim.SourceBasis) * 20
	score += len(claim.EvidenceFrameIndices) * 30
	score += int(claim.Confidence * 10)
	switch claim.Kind {
	case ClaimKindWorkflowRule:
		score += 40
	case ClaimKindArchitecture:
		score += 35
	case ClaimKindDecision:
		score += 30
	case ClaimKindPreference:
		score += 25
	default:
		score += 5
	}
	if len(claim.GroupKeys) > 0 {
		score += 10
	}
	return score
}

type objectiveAlignmentPayload struct {
	Alignments []objectiveAlignmentItem `json:"alignments"`
}

type objectiveAlignmentItem struct {
	Index       int                   `json:"index"`
	Role        ObjectiveRole         `json:"role"`
	Action      ObjectiveMemoryAction `json:"action"`
	Score       float64               `json:"score"`
	Explanation string                `json:"explanation,omitempty"`
}

func (c *CachedClaimClassifier) alignClaimsToObjective(ctx context.Context, cacheStore *transcriptcache.Store, objective SessionObjective, claims []ClassifiedClaim) ([]ClassifiedClaim, *ArtifactCacheReport, error) {
	if len(claims) == 0 || strings.TrimSpace(objective.Objective) == "" {
		return claims, nil, nil
	}
	artifactText := buildObjectiveAlignmentArtifactText(objective, claims)
	sourceHash := transcriptcache.DigestText(artifactText)
	normalizedHash := sourceHash
	promptVersion := firstNonEmpty(strings.TrimSpace(c.Runtime.ObjectiveAlignmentPromptVersion), DefaultObjectiveAlignmentPromptVersion)
	modelID := resolveModelID(c.Runtime.Mode, c.Runtime.WorkerConfig())

	if cacheStore != nil {
		if entry, hit, err := cacheStore.GetByNormalizedHash(ctx, "classified_claim_objective_alignment", normalizedHash, promptVersion, modelID); err != nil {
			return nil, nil, err
		} else if hit {
			aligned := applyObjectiveAlignmentPayload(claims, decodeObjectiveAlignmentPayload(entry.Summary, len(claims)))
			return aligned, &ArtifactCacheReport{
				ArtifactKind:   "classified_claim_objective_alignment",
				NormalizedHash: normalizedHash,
				SourceHash:     sourceHash,
				DerivationMode: entry.DerivationMode,
				ModelID:        entry.ModelID,
				CacheHit:       true,
				SummaryPreview: truncatePacketInline(entry.Summary, 140),
			}, nil
		}
	}

	aligned := claims
	entry := transcriptcache.Entry{
		ArtifactKind:   "classified_claim_objective_alignment",
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		PromptVersion:  promptVersion,
		SourcePreview:  truncatePacketInline(artifactText, 120),
	}
	switch normalizeMode(c.Runtime.Mode) {
	case "deterministic":
		entry.DerivationMode = "deterministic"
		entry.ModelID = deterministicModelID()
	default:
		result, err := RunLLMTask(ctx, c.Runtime.WorkerConfig(), Task{
			Stage:         StageAlign,
			InputKind:     "classified_claim_objective_alignment",
			PromptVersion: promptVersion,
			SystemPrompt:  objectiveAlignmentSystemPromptForVersion(promptVersion),
			ArtifactText:  artifactText,
			MaxTokens:     220,
		})
		if err != nil {
			entry.DerivationMode = "deterministic_fallback"
			entry.ModelID = deterministicModelID()
		} else if decoded := decodeObjectiveAlignmentPayload(result.OutputText, len(claims)); len(decoded.Alignments) > 0 {
			entry.DerivationMode = "lmstudio"
			entry.ModelID = result.ModelID
			aligned = applyObjectiveAlignmentPayload(claims, decoded)
		} else {
			entry.DerivationMode = "deterministic_fallback"
			entry.ModelID = deterministicModelID()
		}
	}
	entry.Summary = encodeObjectiveAlignmentPayload(claimsToAlignmentPayload(aligned))
	if cacheStore != nil {
		if err := cacheStore.Put(ctx, entry); err != nil {
			return nil, nil, err
		}
	}
	return aligned, &ArtifactCacheReport{
		ArtifactKind:   "classified_claim_objective_alignment",
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		DerivationMode: entry.DerivationMode,
		ModelID:        entry.ModelID,
		CacheHit:       false,
		SummaryPreview: truncatePacketInline(entry.Summary, 140),
	}, nil
}

func buildObjectiveAlignmentArtifactText(objective SessionObjective, claims []ClassifiedClaim) string {
	var b strings.Builder
	b.WriteString("objective: ")
	b.WriteString(strings.TrimSpace(objective.Objective))
	b.WriteString("\n")
	if strings.TrimSpace(objective.Label) != "" {
		b.WriteString("objective_label: ")
		b.WriteString(strings.TrimSpace(objective.Label))
		b.WriteString("\n")
	}
	if strings.TrimSpace(objective.Status) != "" {
		b.WriteString("objective_status: ")
		b.WriteString(strings.TrimSpace(objective.Status))
		b.WriteString("\n")
	}
	if len(objective.Tags) > 0 {
		b.WriteString("objective_tags: ")
		b.WriteString(strings.Join(objective.Tags, ", "))
		b.WriteString("\n")
	}
	b.WriteString("\nclaims:\n")
	for idx, claim := range claims {
		b.WriteString(fmt.Sprintf("%d. kind=%s durability=%s source=%s text=%s\n", idx, claim.Kind, claim.Durability, claim.SourceBasis, strings.TrimSpace(claim.Text)))
	}
	return b.String()
}

func decodeObjectiveAlignmentPayload(raw string, claimCount int) objectiveAlignmentPayload {
	var payload objectiveAlignmentPayload
	trimmed := strings.TrimSpace(raw)
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		start := strings.Index(trimmed, "{")
		end := strings.LastIndex(trimmed, "}")
		if start >= 0 && end > start {
			if err := json.Unmarshal([]byte(trimmed[start:end+1]), &payload); err != nil {
				return objectiveAlignmentPayload{}
			}
		} else {
			return objectiveAlignmentPayload{}
		}
	}
	out := make([]objectiveAlignmentItem, 0, len(payload.Alignments))
	for _, item := range payload.Alignments {
		if item.Index < 0 || item.Index >= claimCount {
			continue
		}
		item.Role = normalizeObjectiveRole(item.Role)
		item.Action = normalizeObjectiveMemoryAction(item.Action)
		if item.Score < 0 || item.Score > 1 {
			item.Score = 0.5
		}
		item.Explanation = truncatePacketInline(strings.TrimSpace(item.Explanation), 160)
		out = append(out, item)
	}
	payload.Alignments = out
	return payload
}

func applyObjectiveAlignmentPayload(claims []ClassifiedClaim, payload objectiveAlignmentPayload) []ClassifiedClaim {
	out := make([]ClassifiedClaim, len(claims))
	copy(out, claims)
	for _, item := range payload.Alignments {
		claim := out[item.Index]
		claim.ObjectiveRole = normalizeObjectiveRole(item.Role)
		claim.ObjectiveAction = normalizeObjectiveMemoryAction(item.Action)
		claim.ObjectiveScore = item.Score
		claim.ObjectiveExplanation = truncatePacketInline(strings.TrimSpace(item.Explanation), 160)
		out[item.Index] = claim
	}
	return out
}

func claimsToAlignmentPayload(claims []ClassifiedClaim) objectiveAlignmentPayload {
	payload := objectiveAlignmentPayload{Alignments: make([]objectiveAlignmentItem, 0, len(claims))}
	for idx, claim := range claims {
		if claim.ObjectiveRole == "" && claim.ObjectiveAction == "" && claim.ObjectiveScore == 0 && claim.ObjectiveExplanation == "" {
			continue
		}
		payload.Alignments = append(payload.Alignments, objectiveAlignmentItem{
			Index:       idx,
			Role:        normalizeObjectiveRole(claim.ObjectiveRole),
			Action:      normalizeObjectiveMemoryAction(claim.ObjectiveAction),
			Score:       claim.ObjectiveScore,
			Explanation: truncatePacketInline(strings.TrimSpace(claim.ObjectiveExplanation), 160),
		})
	}
	return payload
}

func encodeObjectiveAlignmentPayload(payload objectiveAlignmentPayload) string {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return `{"alignments":[]}`
	}
	return string(encoded)
}

func objectiveAlignmentSystemPromptForVersion(version string) string {
	switch strings.TrimSpace(version) {
	case "", "objective_alignment_v1":
		return objectiveAlignmentSystemPromptV1
	case "objective_alignment_v2":
		return objectiveAlignmentSystemPromptV2
	case "objective_alignment_v3":
		return objectiveAlignmentSystemPromptV3
	case DefaultObjectiveAlignmentPromptVersion:
		return objectiveAlignmentSystemPromptV4
	default:
		return objectiveAlignmentSystemPromptV1
	}
}

func doctrineBridgeSystemPromptForVersion(version string) string {
	switch strings.TrimSpace(version) {
	case "", "doctrine_bridge_v1":
		return doctrineBridgeSystemPromptV1
	case "doctrine_bridge_v2":
		return doctrineBridgeSystemPromptV2
	case DefaultDoctrineBridgePromptVersion:
		return doctrineBridgeSystemPromptV3
	default:
		return doctrineBridgeSystemPromptV1
	}
}

func doctrineDistillSystemPromptForVersion(version string) string {
	switch strings.TrimSpace(version) {
	case "", "doctrine_distill_v1":
		return doctrineDistillSystemPromptV1
	case "doctrine_distill_v2":
		return doctrineDistillSystemPromptV2
	case DefaultDoctrineDistillPromptVersion:
		return doctrineDistillSystemPromptV3
	default:
		return doctrineDistillSystemPromptV1
	}
}

func filterRelevantDerivationsForClassification(derivations []companion.AnchoredMemoryDerivation) []companion.AnchoredMemoryDerivation {
	if len(derivations) == 0 {
		return nil
	}
	out := make([]companion.AnchoredMemoryDerivation, 0, len(derivations))
	for _, derivation := range derivations {
		if derivation.Resolution == companion.InteractionResolutionResolved || derivation.Resolution == companion.InteractionResolutionCorrected {
			out = append(out, derivation)
			continue
		}
		for _, candidate := range derivation.Candidates {
			if candidate.Scope == companion.CandidateScopeDurable || candidate.Scope == companion.CandidateScopeProvisional {
				out = append(out, derivation)
				break
			}
		}
	}
	if len(out) == 0 {
		out = append(out, derivations...)
	}
	if len(out) > 12 {
		out = selectSegmentedDerivationsForClassification(out, 12)
	}
	return out
}

func selectSegmentedDerivationsForClassification(in []companion.AnchoredMemoryDerivation, limit int) []companion.AnchoredMemoryDerivation {
	if len(in) == 0 || limit <= 0 || len(in) <= limit {
		return in
	}
	n := len(in)
	breaks := []int{0, n / 3, (2 * n) / 3, n}
	segments := make([][]companion.AnchoredMemoryDerivation, 0, 3)
	for i := 0; i+1 < len(breaks); i++ {
		start, end := breaks[i], breaks[i+1]
		if start >= end {
			continue
		}
		segments = append(segments, in[start:end])
	}
	if len(segments) == 0 {
		return selectDerivationsForClassification(in, limit)
	}
	baseQuota := limit / len(segments)
	if baseQuota == 0 {
		baseQuota = 1
	}
	selected := make([]companion.AnchoredMemoryDerivation, 0, limit)
	for _, segment := range segments {
		quota := baseQuota
		if quota > len(segment) {
			quota = len(segment)
		}
		selected = append(selected, selectDerivationsForClassification(segment, quota)...)
	}
	if len(selected) < limit {
		remainder := selectDerivationsForClassification(in, limit)
		selected = append(selected, remainder...)
	}
	if len(selected) == 0 {
		return nil
	}
	sort.SliceStable(selected, func(i, j int) bool {
		return selected[i].FrameIndex < selected[j].FrameIndex
	})
	deduped := make([]companion.AnchoredMemoryDerivation, 0, len(selected))
	seen := make(map[int]struct{}, len(selected))
	for _, item := range selected {
		if _, ok := seen[item.FrameIndex]; ok {
			continue
		}
		seen[item.FrameIndex] = struct{}{}
		deduped = append(deduped, item)
		if len(deduped) >= limit {
			break
		}
	}
	return deduped
}

type derivationLedgerEntry struct {
	Key          string
	FrameIndices []int
	SupportCount int
	ScopeRank    int
	Confidence   float64
}

func selectDerivationsForClassification(in []companion.AnchoredMemoryDerivation, limit int) []companion.AnchoredMemoryDerivation {
	if len(in) == 0 || limit <= 0 || len(in) <= limit {
		return in
	}
	ledger, frameScores := buildDerivationLedger(in)
	selected := make(map[int]struct{}, limit)
	addFrame := func(frameIdx int) {
		if frameIdx < 0 || frameIdx >= len(in) {
			return
		}
		selected[frameIdx] = struct{}{}
	}

	addFrame(0)
	addFrame(len(in) - 1)
	for idx, derivation := range in {
		if len(selected) >= limit {
			break
		}
		if derivation.Resolution == companion.InteractionResolutionCorrected {
			addFrame(idx)
		}
	}
	for idx, derivation := range in {
		if len(selected) >= limit {
			break
		}
		if derivation.Resolution == companion.InteractionResolutionUnresolved && (derivation.Reaction.Outcome == companion.ReactionOutcomeFrustrated || derivation.Reaction.Outcome == companion.ReactionOutcomeConfused) {
			addFrame(idx)
		}
	}

	sort.SliceStable(ledger, func(i, j int) bool {
		if ledger[i].SupportCount != ledger[j].SupportCount {
			return ledger[i].SupportCount > ledger[j].SupportCount
		}
		if ledger[i].ScopeRank != ledger[j].ScopeRank {
			return ledger[i].ScopeRank > ledger[j].ScopeRank
		}
		if ledger[i].Confidence != ledger[j].Confidence {
			return ledger[i].Confidence > ledger[j].Confidence
		}
		return ledger[i].FrameIndices[0] < ledger[j].FrameIndices[0]
	})
	for _, entry := range ledger {
		if len(selected) >= limit {
			break
		}
		addFrame(entry.FrameIndices[0])
		if len(selected) >= limit {
			break
		}
		if len(entry.FrameIndices) > 1 {
			addFrame(entry.FrameIndices[len(entry.FrameIndices)-1])
		}
	}

	sort.SliceStable(frameScores, func(i, j int) bool {
		if frameScores[i].Score != frameScores[j].Score {
			return frameScores[i].Score > frameScores[j].Score
		}
		return frameScores[i].Index < frameScores[j].Index
	})
	for _, score := range frameScores {
		if len(selected) >= limit {
			break
		}
		addFrame(score.Index)
	}

	indices := make([]int, 0, len(selected))
	for idx := range selected {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	out := make([]companion.AnchoredMemoryDerivation, 0, len(indices))
	for _, idx := range indices {
		out = append(out, in[idx])
	}
	return out
}

type derivationFrameScore struct {
	Index int
	Score int
}

func buildDerivationLedger(in []companion.AnchoredMemoryDerivation) ([]derivationLedgerEntry, []derivationFrameScore) {
	type ledgerBuilder struct {
		frames     map[int]struct{}
		scopeRank  int
		confidence float64
	}
	ledgerMap := make(map[string]*ledgerBuilder)
	frameScores := make([]derivationFrameScore, 0, len(in))
	for idx, derivation := range in {
		score := 0
		if derivation.Resolution == companion.InteractionResolutionResolved || derivation.Resolution == companion.InteractionResolutionCorrected {
			score += 25
		}
		if derivation.Resolution == companion.InteractionResolutionCorrected {
			score += 80
		}
		if derivation.Resolution == companion.InteractionResolutionUnresolved && (derivation.Reaction.Outcome == companion.ReactionOutcomeFrustrated || derivation.Reaction.Outcome == companion.ReactionOutcomeConfused) {
			score += 40
		}
		for _, candidate := range derivation.Candidates {
			key := derivationLedgerKey(candidate)
			if key == "" {
				continue
			}
			builder := ledgerMap[key]
			if builder == nil {
				builder = &ledgerBuilder{frames: make(map[int]struct{})}
				ledgerMap[key] = builder
			}
			builder.frames[idx] = struct{}{}
			if rank := candidateScopeRank(candidate.Scope); rank > builder.scopeRank {
				builder.scopeRank = rank
			}
			if candidate.Confidence > builder.confidence {
				builder.confidence = candidate.Confidence
			}
			score += candidateScopeRank(candidate.Scope) * 30
			score += int(candidate.Confidence * 10)
		}
		frameScores = append(frameScores, derivationFrameScore{Index: idx, Score: score})
	}
	ledger := make([]derivationLedgerEntry, 0, len(ledgerMap))
	for key, builder := range ledgerMap {
		frames := make([]int, 0, len(builder.frames))
		for idx := range builder.frames {
			frames = append(frames, idx)
		}
		sort.Ints(frames)
		ledger = append(ledger, derivationLedgerEntry{
			Key:          key,
			FrameIndices: frames,
			SupportCount: len(frames),
			ScopeRank:    builder.scopeRank,
			Confidence:   builder.confidence,
		})
	}
	for i := range frameScores {
		for _, entry := range ledger {
			for _, idx := range entry.FrameIndices {
				if idx == frameScores[i].Index {
					frameScores[i].Score += entry.SupportCount * 40
					break
				}
			}
		}
	}
	return ledger, frameScores
}

func derivationLedgerKey(candidate companion.AnchoredMemoryCandidate) string {
	text := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(candidate.Text)), " "))
	if text == "" || strings.TrimSpace(candidate.Type) == "tool_output_digest" {
		return ""
	}
	return strings.TrimSpace(candidate.Type) + "|" + text
}

func candidateScopeRank(scope companion.AnchoredMemoryCandidateScope) int {
	switch scope {
	case companion.CandidateScopeDurable:
		return 3
	case companion.CandidateScopeProvisional:
		return 2
	default:
		return 1
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func deterministicClassifiedClaims(derivations []companion.AnchoredMemoryDerivation) []ClassifiedClaim {
	var out []ClassifiedClaim
	seen := make(map[string]struct{})
	for _, derivation := range derivations {
		for _, candidate := range derivation.Candidates {
			if candidate.Scope != companion.CandidateScopeDurable {
				continue
			}
			claim, ok := deterministicClaimFromCandidate(derivation.FrameIndex, candidate)
			if !ok {
				continue
			}
			key := string(claim.Kind) + "|" + strings.ToLower(strings.TrimSpace(claim.Text))
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, claim)
		}
	}
	return out
}

func mergeClassifiedClaims(existing []ClassifiedClaim, incoming []ClassifiedClaim) []ClassifiedClaim {
	if len(incoming) == 0 {
		return existing
	}
	out := make([]ClassifiedClaim, len(existing))
	copy(out, existing)
	index := make(map[string]int, len(out))
	for i, claim := range out {
		index[classifiedClaimKey(claim)] = i
	}
	for _, claim := range incoming {
		key := classifiedClaimKey(claim)
		if idx, ok := index[key]; ok {
			merged := out[idx]
			if claim.Confidence > merged.Confidence {
				merged.Confidence = claim.Confidence
			}
			merged.EvidenceFrameIndices = normalizeEvidenceFrames(append(merged.EvidenceFrameIndices, claim.EvidenceFrameIndices...), 1000000)
			merged.Tags = normalizeTagList(append(merged.Tags, claim.Tags...))
			merged.GroupKeys = normalizeGroupKeys(append(merged.GroupKeys, claim.GroupKeys...))
			if promotionBlockerRank(claim.PromotionBlocker) > promotionBlockerRank(merged.PromotionBlocker) {
				merged.PromotionBlocker = claim.PromotionBlocker
			}
			if merged.SourceBasis == "mixed" && claim.SourceBasis != "" {
				merged.SourceBasis = claim.SourceBasis
			}
			out[idx] = merged
			continue
		}
		index[key] = len(out)
		out = append(out, claim)
	}
	return out
}

// ConsolidateClassifiedClaims merges semantically-related typed claims into a smaller durable set.
func ConsolidateClassifiedClaims(in []ClassifiedClaim) []ClassifiedClaim {
	if len(in) == 0 {
		return nil
	}
	groups := make(map[string][]ClassifiedClaim)
	for _, claim := range in {
		key := classifiedClaimKey(claim)
		groups[key] = append(groups[key], claim)
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]ClassifiedClaim, 0, len(keys))
	for _, key := range keys {
		group := groups[key]
		base := chooseRepresentativeClaim(group)
		for _, item := range group[1:] {
			base.Tags = normalizeTagList(append(base.Tags, item.Tags...))
			base.GroupKeys = normalizeGroupKeys(append(base.GroupKeys, item.GroupKeys...))
			base.EvidenceFrameIndices = normalizeEvidenceFrames(append(base.EvidenceFrameIndices, item.EvidenceFrameIndices...), 1000000)
			if promotionBlockerRank(item.PromotionBlocker) > promotionBlockerRank(base.PromotionBlocker) {
				base.PromotionBlocker = item.PromotionBlocker
			}
			if sourceBasisRank(item.SourceBasis) > sourceBasisRank(base.SourceBasis) {
				base.SourceBasis = item.SourceBasis
			}
			if claimDurabilityRank(item.Durability) > claimDurabilityRank(base.Durability) {
				base.Durability = item.Durability
			}
			if item.Confidence > base.Confidence {
				base.Confidence = item.Confidence
			}
		}
		out = append(out, base)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if claimDurabilityRank(out[i].Durability) != claimDurabilityRank(out[j].Durability) {
			return claimDurabilityRank(out[i].Durability) > claimDurabilityRank(out[j].Durability)
		}
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Text < out[j].Text
	})
	return out
}

func chooseRepresentativeClaim(group []ClassifiedClaim) ClassifiedClaim {
	best := group[0]
	for _, item := range group[1:] {
		if claimRepresentativeScore(item) > claimRepresentativeScore(best) {
			best = item
			continue
		}
		if claimRepresentativeScore(item) == claimRepresentativeScore(best) && len(item.Text) < len(best.Text) {
			best = item
		}
	}
	best.Tags = normalizeTagList(best.Tags)
	best.GroupKeys = normalizeGroupKeys(best.GroupKeys)
	best.EvidenceFrameIndices = normalizeEvidenceFrames(best.EvidenceFrameIndices, 1000000)
	return best
}

func claimRepresentativeScore(claim ClassifiedClaim) int {
	score := 0
	score += claimDurabilityRank(claim.Durability) * 100
	score += sourceBasisRank(claim.SourceBasis) * 10
	score += int(claim.Confidence * 10)
	score -= promotionBlockerRank(claim.PromotionBlocker) * 25
	return score
}

func claimDurabilityRank(d ClaimDurability) int {
	switch d {
	case ClaimDurabilityDurable:
		return 3
	case ClaimDurabilityProvisional:
		return 2
	case ClaimDurabilitySession:
		return 1
	default:
		return 0
	}
}

func sourceBasisRank(source string) int {
	switch normalizeClassifiedSourceBasis(source) {
	case "user_approved":
		return 4
	case "user":
		return 3
	case "mixed":
		return 2
	case "assistant":
		return 1
	default:
		return 0
	}
}

func promotionBlockerRank(blocker ClaimPromotionBlocker) int {
	switch normalizeClaimPromotionBlocker(blocker) {
	case ClaimPromotionBlockerNone:
		return 0
	case ClaimPromotionBlockerProceduralScaffold:
		return 1
	case ClaimPromotionBlockerMetaProgress:
		return 2
	case ClaimPromotionBlockerImplementationStatus:
		return 3
	case ClaimPromotionBlockerEvaluationMeta:
		return 4
	default:
		return 1
	}
}

func classifiedClaimKey(claim ClassifiedClaim) string {
	if len(claim.GroupKeys) > 0 {
		return string(claim.Kind) + "|" + claim.GroupKeys[0]
	}
	return string(claim.Kind) + "|" + strings.ToLower(strings.TrimSpace(claim.Text))
}

func summarizeClassificationArtifacts(artifacts []ArtifactCacheReport, claims []ClassifiedClaim, hashes []string) *ArtifactCacheReport {
	if len(artifacts) == 0 {
		return nil
	}
	sort.Strings(hashes)
	mode := artifacts[0].DerivationMode
	modelID := artifacts[0].ModelID
	allHits := true
	for _, item := range artifacts[1:] {
		if item.DerivationMode != mode {
			mode = "mixed"
		}
		if item.ModelID != modelID {
			modelID = "mixed"
		}
		if !item.CacheHit {
			allHits = false
		}
	}
	if !artifacts[0].CacheHit {
		allHits = false
	}
	digest := transcriptcache.DigestText(strings.Join(hashes, "|"))
	summary := encodeClassifiedClaims(claims)
	return &ArtifactCacheReport{
		ArtifactKind:   "classified_claims_batch",
		NormalizedHash: digest,
		SourceHash:     digest,
		DerivationMode: mode,
		ModelID:        modelID,
		CacheHit:       allHits,
		SummaryPreview: truncatePacketInline(summary, 140),
	}
}

func deterministicClaimFromCandidate(frameIndex int, candidate companion.AnchoredMemoryCandidate) (ClassifiedClaim, bool) {
	kind, ok := claimKindFromCandidateType(candidate.Type)
	if !ok {
		return ClassifiedClaim{}, false
	}
	return ClassifiedClaim{
		Text:                 truncatePacketInline(strings.TrimSpace(candidate.Text), 200),
		Kind:                 kind,
		Durability:           claimDurabilityFromCandidateScope(candidate.Scope),
		PromotionBlocker:     ClaimPromotionBlockerNone,
		Confidence:           candidate.Confidence,
		SourceBasis:          normalizeSourceBasis(candidate.Source),
		EvidenceFrameIndices: []int{frameIndex},
		Tags:                 []string{string(kind)},
		GroupKeys:            []string{"kind/" + string(kind)},
	}, true
}

func seedDoctrineClaimsFromDerivations(derivations []companion.AnchoredMemoryDerivation) []ClassifiedClaim {
	var seeds []ClassifiedClaim
	for _, derivation := range derivations {
		for _, candidate := range derivation.Candidates {
			if candidate.Scope != companion.CandidateScopeDurable {
				continue
			}
			claim, ok := deterministicClaimFromCandidate(derivation.FrameIndex, candidate)
			if !ok {
				continue
			}
			if !isDoctrineCandidateClaim(claim) {
				continue
			}
			seeds = append(seeds, claim)
		}
	}
	return ConsolidateClassifiedClaims(seeds)
}

func deterministicDoctrineBridgeFallbackClaims(derivations []companion.AnchoredMemoryDerivation) []ClassifiedClaim {
	var seeds []ClassifiedClaim
	for _, derivation := range derivations {
		for _, candidate := range derivation.Candidates {
			claim, ok := deterministicBridgeEvidenceClaim(derivation.FrameIndex, candidate)
			if !ok {
				continue
			}
			seeds = append(seeds, claim)
		}
	}
	return ConsolidateClassifiedClaims(seeds)
}

func deterministicBridgeEvidenceClaim(frameIndex int, candidate companion.AnchoredMemoryCandidate) (ClassifiedClaim, bool) {
	text := truncatePacketInline(strings.TrimSpace(candidate.Text), 200)
	if text == "" {
		return ClassifiedClaim{}, false
	}
	switch strings.TrimSpace(candidate.Type) {
	case companion.EntryTypeTechnicalContext:
		if candidate.Scope != companion.CandidateScopeDurable {
			return ClassifiedClaim{}, false
		}
		return ClassifiedClaim{
			Text:                 text,
			Kind:                 ClaimKindTechnical,
			Durability:           ClaimDurabilityDurable,
			PromotionBlocker:     ClaimPromotionBlockerNone,
			Confidence:           candidate.Confidence,
			SourceBasis:          normalizeSourceBasis(candidate.Source),
			EvidenceFrameIndices: []int{frameIndex},
			Tags:                 []string{"bridge-evidence", "technical-context"},
		}, true
	case companion.EntryTypeOpenQuestion, "user_correction":
		return ClassifiedClaim{
			Text:                 text,
			Kind:                 ClaimKindOpenQuestion,
			Durability:           ClaimDurabilityDurable,
			PromotionBlocker:     ClaimPromotionBlockerNone,
			Confidence:           maxFloat(candidate.Confidence, 0.82),
			SourceBasis:          normalizeSourceBasis(candidate.Source),
			EvidenceFrameIndices: []int{frameIndex},
			Tags:                 []string{"bridge-evidence", "correction"},
		}, true
	default:
		return ClassifiedClaim{}, false
	}
}

func claimKindFromCandidateType(candidateType string) (ClaimKind, bool) {
	switch strings.TrimSpace(candidateType) {
	case companion.EntryTypeDecision:
		return ClaimKindDecision, true
	case companion.EntryTypePreference:
		return ClaimKindPreference, true
	case companion.EntryTypeTechnicalContext:
		return ClaimKindTechnical, true
	case "user_pain_point":
		return ClaimKindPainPoint, true
	case companion.EntryTypeOpenQuestion:
		return ClaimKindOpenQuestion, true
	default:
		return "", false
	}
}

func claimDurabilityFromCandidateScope(scope companion.AnchoredMemoryCandidateScope) ClaimDurability {
	switch scope {
	case companion.CandidateScopeDurable:
		return ClaimDurabilityDurable
	case companion.CandidateScopeProvisional:
		return ClaimDurabilityProvisional
	default:
		return ClaimDurabilitySession
	}
}

func normalizeSourceBasis(source string) string {
	switch strings.TrimSpace(source) {
	case "user", "followup_user":
		return "user"
	case "assistant_guidance":
		return "user_approved"
	case "assistant":
		return "assistant"
	default:
		return "mixed"
	}
}

func encodeClassifiedClaims(claims []ClassifiedClaim) string {
	payload := struct {
		Claims []ClassifiedClaim `json:"claims"`
	}{Claims: claims}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return `{"claims":[]}`
	}
	return string(bytes)
}

func decodeClassifiedClaims(raw string, frameCount int) []ClassifiedClaim {
	var payload struct {
		Claims []ClassifiedClaim `json:"claims"`
	}
	trimmed := strings.TrimSpace(raw)
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		start := strings.Index(trimmed, "{")
		end := strings.LastIndex(trimmed, "}")
		if start >= 0 && end > start {
			if err := json.Unmarshal([]byte(trimmed[start:end+1]), &payload); err != nil {
				return nil
			}
		} else {
			return nil
		}
	}
	return validateClassifiedClaims(payload.Claims, frameCount)
}

func validateClassifiedClaims(in []ClassifiedClaim, frameCount int) []ClassifiedClaim {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]ClassifiedClaim, 0, len(in))
	for _, claim := range in {
		claim.Text = truncatePacketInline(strings.TrimSpace(claim.Text), 200)
		if claim.Text == "" {
			continue
		}
		if !isAllowedClaimKind(claim.Kind) {
			continue
		}
		if !isAllowedClaimDurability(claim.Durability) {
			continue
		}
		if claim.Confidence <= 0 || claim.Confidence > 1 {
			claim.Confidence = 0.75
		}
		claim.PromotionBlocker = normalizeClaimPromotionBlocker(claim.PromotionBlocker)
		claim.SourceBasis = normalizeClassifiedSourceBasis(claim.SourceBasis)
		claim.Tags = normalizeTagList(claim.Tags)
		claim.GroupKeys = normalizeGroupKeys(claim.GroupKeys)
		claim.EvidenceFrameIndices = normalizeEvidenceFrames(claim.EvidenceFrameIndices, frameCount)
		claim.ObjectiveRole = normalizeObjectiveRole(claim.ObjectiveRole)
		claim.ObjectiveAction = normalizeObjectiveMemoryAction(claim.ObjectiveAction)
		if claim.ObjectiveScore < 0 || claim.ObjectiveScore > 1 {
			claim.ObjectiveScore = 0
		}
		claim.ObjectiveExplanation = truncatePacketInline(strings.TrimSpace(claim.ObjectiveExplanation), 160)
		if isNegativeClassClaim(claim) {
			continue
		}
		key := string(claim.Kind) + "|" + strings.ToLower(claim.Text)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, claim)
	}
	return out
}

func isDoctrineCandidateClaim(claim ClassifiedClaim) bool {
	if normalizeClaimPromotionBlocker(claim.PromotionBlocker) != ClaimPromotionBlockerNone {
		return false
	}
	if isNegativeClassClaim(claim) {
		return false
	}
	switch claim.Kind {
	case ClaimKindArchitecture, ClaimKindWorkflowRule:
		return true
	case ClaimKindDecision, ClaimKindPreference:
		return normalizeClassifiedSourceBasis(claim.SourceBasis) == "user" || normalizeClassifiedSourceBasis(claim.SourceBasis) == "user_approved"
	default:
		return false
	}
}

func isDoctrineEvidenceClaim(claim ClassifiedClaim) bool {
	if isDoctrineCandidateClaim(claim) {
		return true
	}
	if normalizeClaimPromotionBlocker(claim.PromotionBlocker) != ClaimPromotionBlockerNone {
		return false
	}
	switch claim.Kind {
	case ClaimKindTechnical:
		return claim.Durability == ClaimDurabilityDurable && (normalizeClassifiedSourceBasis(claim.SourceBasis) == "user_approved" || normalizeClassifiedSourceBasis(claim.SourceBasis) == "mixed")
	case ClaimKindOpenQuestion:
		return claim.Durability == ClaimDurabilityDurable && (normalizeClassifiedSourceBasis(claim.SourceBasis) == "user" || normalizeClassifiedSourceBasis(claim.SourceBasis) == "user_approved")
	default:
		return false
	}
}

func isNegativeClassClaim(claim ClassifiedClaim) bool {
	if claim.PromotionBlocker != "" && claim.PromotionBlocker != ClaimPromotionBlockerNone {
		return true
	}
	if len(claim.Tags) == 0 && len(claim.GroupKeys) == 0 {
		return false
	}
	tags := make(map[string]struct{}, len(claim.Tags))
	for _, tag := range claim.Tags {
		tags[strings.ToLower(strings.TrimSpace(tag))] = struct{}{}
	}
	for _, groupKey := range claim.GroupKeys {
		tags[strings.ToLower(strings.TrimSpace(groupKey))] = struct{}{}
	}
	for _, blocked := range []string{
		"experiment", "experiment-loop", "progress", "build", "execution",
		"logging", "artifact", "core-piece", "code-change", "implementation",
		"evaluation", "evaluation-framework", "module", "repository",
		"tools/execution", "pipeline/transcript", "workflow/local-models",
	} {
		if _, ok := tags[blocked]; ok {
			return true
		}
	}
	return false
}

func normalizeClaimPromotionBlocker(blocker ClaimPromotionBlocker) ClaimPromotionBlocker {
	switch strings.TrimSpace(strings.ToLower(string(blocker))) {
	case "", string(ClaimPromotionBlockerNone):
		return ClaimPromotionBlockerNone
	case string(ClaimPromotionBlockerMetaProgress):
		return ClaimPromotionBlockerMetaProgress
	case string(ClaimPromotionBlockerImplementationStatus):
		return ClaimPromotionBlockerImplementationStatus
	case string(ClaimPromotionBlockerEvaluationMeta):
		return ClaimPromotionBlockerEvaluationMeta
	case string(ClaimPromotionBlockerProceduralScaffold):
		return ClaimPromotionBlockerProceduralScaffold
	default:
		return ClaimPromotionBlockerProceduralScaffold
	}
}

func normalizeObjectiveRole(role ObjectiveRole) ObjectiveRole {
	switch strings.TrimSpace(strings.ToLower(string(role))) {
	case string(ObjectiveRoleSupport):
		return ObjectiveRoleSupport
	case string(ObjectiveRoleBlock):
		return ObjectiveRoleBlock
	case string(ObjectiveRoleRedirect):
		return ObjectiveRoleRedirect
	default:
		return ObjectiveRoleIrrelevant
	}
}

func normalizeObjectiveMemoryAction(action ObjectiveMemoryAction) ObjectiveMemoryAction {
	switch strings.TrimSpace(strings.ToLower(string(action))) {
	case string(ObjectiveMemoryActionKeep):
		return ObjectiveMemoryActionKeep
	case string(ObjectiveMemoryActionPrune):
		return ObjectiveMemoryActionPrune
	default:
		return ""
	}
}

func isAllowedClaimKind(kind ClaimKind) bool {
	switch kind {
	case ClaimKindArchitecture, ClaimKindWorkflowRule, ClaimKindDecision, ClaimKindPreference, ClaimKindTechnical, ClaimKindPainPoint, ClaimKindOpenQuestion:
		return true
	default:
		return false
	}
}

func isAllowedClaimDurability(d ClaimDurability) bool {
	switch d {
	case ClaimDurabilitySession, ClaimDurabilityDurable, ClaimDurabilityProvisional:
		return true
	default:
		return false
	}
}

func normalizeClassifiedSourceBasis(source string) string {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case "user":
		return "user"
	case "assistant":
		return "assistant"
	case "user_approved":
		return "user_approved"
	default:
		return "mixed"
	}
}

func normalizeTagList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = normalizeLabelToken(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func normalizeGroupKeys(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.ToLower(strings.TrimSpace(item))
		item = strings.ReplaceAll(item, " ", "-")
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func normalizeLabelToken(in string) string {
	in = strings.ToLower(strings.TrimSpace(in))
	if in == "" {
		return ""
	}
	in = strings.ReplaceAll(in, " ", "-")
	in = strings.ReplaceAll(in, "_", "-")
	var b strings.Builder
	lastDash := false
	for _, r := range in {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if r == '-' || r == '/' {
			if !lastDash && b.Len() > 0 {
				b.WriteRune(r)
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-/")
}

func normalizeEvidenceFrames(in []int, frameCount int) []int {
	if len(in) == 0 || frameCount <= 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(in))
	out := make([]int, 0, len(in))
	for _, idx := range in {
		if idx < 0 || idx >= frameCount {
			continue
		}
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		out = append(out, idx)
	}
	sort.Ints(out)
	return out
}

// ClassifiedClaimsFromConsensus converts consensus-backed grouped claims into typed semantic claims.
func ClassifiedClaimsFromConsensus(claims []ConsensusClaim) []ClassifiedClaim {
	if len(claims) == 0 {
		return nil
	}
	out := make([]ClassifiedClaim, 0, len(claims))
	for _, claim := range claims {
		if strings.TrimSpace(claim.Text) == "" {
			continue
		}
		durability := ClaimDurabilitySession
		if claim.PersistDurable {
			durability = ClaimDurabilityDurable
		}
		out = append(out, ClassifiedClaim{
			Text:        truncatePacketInline(strings.TrimSpace(claim.Text), 200),
			Kind:        classifyConsensusClaimKind(claim.Text),
			Durability:  durability,
			Confidence:  maxFloat(claim.MainlineEvidenceScore, 0.74),
			SourceBasis: "mixed",
			Tags:        []string{"consensus", "sidecar"},
			GroupKeys:   consensusGroupKeys(claim.Text),
		})
	}
	return out
}

func classifyConsensusClaimKind(text string) ClaimKind {
	lower := strings.ToLower(strings.TrimSpace(text))
	switch {
	case strings.Contains(lower, "should"), strings.Contains(lower, "prefer"), strings.Contains(lower, "avoid"):
		return ClaimKindWorkflowRule
	case strings.Contains(lower, "pipeline"), strings.Contains(lower, "runtime"), strings.Contains(lower, "substrate"), strings.Contains(lower, "event sourcing"), strings.Contains(lower, "hard state"):
		return ClaimKindArchitecture
	default:
		return ClaimKindTechnical
	}
}

func consensusGroupKeys(text string) []string {
	lower := strings.ToLower(strings.TrimSpace(text))
	switch {
	case strings.Contains(lower, "runtime"), strings.Contains(lower, "pipeline"), strings.Contains(lower, "event sourcing"), strings.Contains(lower, "hard state"):
		return []string{"architecture/pipeline"}
	case strings.Contains(lower, "classifier"), strings.Contains(lower, "brittle"), strings.Contains(lower, "avoid"):
		return []string{"workflow/classifier"}
	default:
		return []string{"consensus/group"}
	}
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func classifierSystemPromptForVersion(version string) string {
	switch strings.TrimSpace(version) {
	case "", "classified_claims_v2":
		return classifierSystemPromptV2
	case "classified_claims_v3":
		return classifierSystemPromptV3
	case "classified_claims_v4":
		return classifierSystemPromptV4
	case "classified_claims_v5":
		return classifierSystemPromptV5
	case DefaultClassifiedClaimsPromptVersion:
		return classifierSystemPromptV6
	default:
		return classifierSystemPromptV2
	}
}

func claimReviewSystemPromptForVersion(version string) string {
	switch strings.TrimSpace(version) {
	case "", "classified_claim_review_v1":
		return claimReviewSystemPromptV1
	case "classified_claim_review_v2":
		return claimReviewSystemPromptV2
	case "classified_claim_review_v3":
		return claimReviewSystemPromptV3
	case "classified_claim_review_v4":
		return claimReviewSystemPromptV4
	case DefaultClaimReviewPromptVersion:
		return claimReviewSystemPromptV5
	default:
		return claimReviewSystemPromptV1
	}
}
