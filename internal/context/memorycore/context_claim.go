package memorycore

import (
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextengine"
)

type ContextClaimOptions struct {
	Score          float64
	Summary        string
	IncludeContent bool
}

func RecordFromContextClaim(claim contextengine.MemoryClaim, opts ContextClaimOptions) Record {
	kind := KindForClaimType(claim.ClaimType)
	summary := strings.TrimSpace(opts.Summary)
	if summary == "" {
		summary = strings.TrimSpace(claim.Summary)
	}
	fileRefs := fileRefsFromClaim(claim)
	lifecycle := LifecycleForClaimStatus(claim.Status)
	confidence := claim.Confidence
	if confidence <= 0 {
		confidence = defaultClaimConfidence(claim.Status)
	}
	record := Record{
		ID:         strings.TrimSpace(claim.ID),
		Kind:       kind,
		SourceLane: SourceLaneContextClaim,
		SourceID:   strings.TrimSpace(claim.ID),
		Summary:    summary,
		Score:      opts.Score,
		Temporal: TemporalEnvelope{
			ObservedAt:         formatTime(claim.CreatedAt),
			IngestedAt:         formatTime(claim.CreatedAt),
			ValidFrom:          formatTime(claim.CreatedAt),
			LastAccessedAt:     formatTime(claim.UpdatedAt),
			LastValidatedAt:    formatTime(claim.UpdatedAt),
			TemporalScope:      "durative",
			RequiresValidation: claim.Status == contextengine.ClaimStatusNeedsRevalidation,
		},
		Provenance: Provenance{
			SourceType:      "system",
			SessionID:       strings.TrimSpace(claim.Scope.SessionID),
			FileRefs:        fileRefs,
			ParentMemoryIDs: memoryClaimSourceIDs(claim.SourceRefs),
			CreatedBy:       "foxctl.contextengine",
		},
		Trust: TrustEnvelope{
			SourceTrust:  "agent_generated",
			Confidence:   confidence,
			Authority:    claimAuthority(kind, claim.Status),
			Tainted:      claim.Status == contextengine.ClaimStatusRejected,
			TaintReasons: claimTaintReasons(claim),
		},
		Lifecycle: lifecycle,
		Telemetry: TelemetryEnvelope{
			LastViewedAt: formatTime(claim.UpdatedAt),
		},
		Usage: UsageEnvelope{
			InstructionEligible: false,
			EvidenceOnly:        true,
			Reason:              "context claims are evidence unless promoted as validated policy or skill",
		},
		Links: Links{
			FileRefs: fileRefs,
		},
		Tags: claimTags(claim),
	}
	if record.SourceID == "" {
		record.SourceID = strings.TrimSpace(claim.Summary)
	}
	record.Lifecycle.SupersededBy = strings.TrimSpace(claim.SupersededBy)
	record.Lifecycle.ReviewNotes = strings.TrimSpace(claim.Reason)
	if opts.IncludeContent {
		record.Content = strings.TrimSpace(claim.Reason)
	}
	return record
}

func KindForClaimType(claimType string) Kind {
	switch strings.TrimSpace(claimType) {
	case "working_context", "context":
		return KindWorkingContext
	case "decision":
		return KindDecision
	case "procedural_skill", "skill", "procedure", "pattern":
		return KindProceduralSkill
	case "policy_rule", "policy", "rule":
		return KindPolicyRule
	case "episodic_trace", "trace", "event", "session":
		return KindEpisodicTrace
	case "reflection":
		return KindReflection
	case "eval_result", "eval", "evaluation":
		return KindEvalResult
	case "adapter_example":
		return KindAdapterExample
	default:
		return KindSemanticFact
	}
}

func LifecycleForClaimStatus(status contextengine.ClaimStatus) LifecycleEnvelope {
	switch status {
	case contextengine.ClaimStatusCurrent:
		return LifecycleEnvelope{
			State:        LifecycleStateActive,
			ReviewStatus: ReviewStatusReviewed,
		}
	case contextengine.ClaimStatusStale, contextengine.ClaimStatusNeedsRevalidation:
		return LifecycleEnvelope{
			State:        LifecycleStateStale,
			ReviewStatus: ReviewStatusNeedsReview,
		}
	case contextengine.ClaimStatusSuperseded:
		return LifecycleEnvelope{
			State:        LifecycleStateDeprecated,
			ReviewStatus: ReviewStatusReviewed,
		}
	case contextengine.ClaimStatusRejected:
		return LifecycleEnvelope{
			State:        LifecycleStateQuarantined,
			ReviewStatus: ReviewStatusFailedValidation,
		}
	default:
		return LifecycleEnvelope{
			State:        LifecycleStateCandidate,
			ReviewStatus: ReviewStatusUnreviewed,
		}
	}
}

func fileRefsFromClaim(claim contextengine.MemoryClaim) []string {
	refs := []string{claim.Scope.Path}
	for _, ref := range claim.Scope.Refs {
		if ref.Type == contextengine.RefTypePath {
			refs = append(refs, ref.Ref)
		}
	}
	for _, ref := range claim.SourceRefs {
		if ref.Type == contextengine.RefTypePath {
			refs = append(refs, ref.Ref)
		}
	}
	return dedupeStrings(refs)
}

func memoryClaimSourceIDs(refs []contextengine.EvidenceRef) []string {
	var ids []string
	for _, ref := range refs {
		if ref.Type == contextengine.RefTypeMemoryClaim {
			ids = append(ids, ref.Ref)
		}
	}
	return dedupeStrings(ids)
}

func defaultClaimConfidence(status contextengine.ClaimStatus) float64 {
	switch status {
	case contextengine.ClaimStatusCurrent:
		return 0.65
	case contextengine.ClaimStatusNeedsRevalidation, contextengine.ClaimStatusStale:
		return 0.35
	case contextengine.ClaimStatusSuperseded, contextengine.ClaimStatusRejected:
		return 0.2
	default:
		return 0.45
	}
}

func claimAuthority(kind Kind, status contextengine.ClaimStatus) float64 {
	authority := authorityForKind(kind)
	switch status {
	case contextengine.ClaimStatusCurrent:
		return authority
	case contextengine.ClaimStatusCandidate:
		return authority * 0.55
	case contextengine.ClaimStatusNeedsRevalidation, contextengine.ClaimStatusStale:
		return authority * 0.35
	default:
		return authority * 0.1
	}
}

func claimTaintReasons(claim contextengine.MemoryClaim) []string {
	switch claim.Status {
	case contextengine.ClaimStatusRejected:
		if strings.TrimSpace(claim.Reason) != "" {
			return []string{strings.TrimSpace(claim.Reason)}
		}
		return []string{"claim rejected"}
	default:
		return nil
	}
}

func claimTags(claim contextengine.MemoryClaim) []string {
	tags := []string{}
	if claim.Status != "" {
		tags = append(tags, "status:"+string(claim.Status))
	}
	if strings.TrimSpace(claim.ClaimType) != "" {
		tags = append(tags, "claim_type:"+strings.TrimSpace(claim.ClaimType))
	}
	if strings.TrimSpace(claim.BlastRadius) != "" {
		tags = append(tags, "blast_radius:"+strings.TrimSpace(claim.BlastRadius))
	}
	return dedupeStrings(tags)
}
