package contextplane

import (
	"time"

	"github.com/joshka0/foxctl/internal/context/memorycore"
	"github.com/joshka0/foxctl/internal/intelligence/evidence"
)

type MemoryInstructionGateReason string

const (
	MemoryInstructionGateInvalidInstructionSurface MemoryInstructionGateReason = "invalid_instruction_surface"
	MemoryInstructionGateZeroNow                   MemoryInstructionGateReason = "zero_now"
	MemoryInstructionGateInactiveLifecycle         MemoryInstructionGateReason = "inactive_lifecycle"
	MemoryInstructionGateInvalidReviewStatus       MemoryInstructionGateReason = "invalid_review_status"
	MemoryInstructionGateInvalidInstructionKind    MemoryInstructionGateReason = "invalid_instruction_kind"
	MemoryInstructionGateInstructionNotEligible    MemoryInstructionGateReason = "instruction_not_eligible"
	MemoryInstructionGateEvidenceOnly              MemoryInstructionGateReason = "evidence_only"
	MemoryInstructionGateSuperseded                MemoryInstructionGateReason = "superseded"
	MemoryInstructionGateTainted                   MemoryInstructionGateReason = "tainted"
	MemoryInstructionGateFailedValidation          MemoryInstructionGateReason = "failed_validation"
	MemoryInstructionGateValidUntilUnparsable      MemoryInstructionGateReason = "valid_until_unparsable"
	MemoryInstructionGateValidUntilExpired         MemoryInstructionGateReason = "valid_until_expired"
	MemoryInstructionGateTTLBaseMissing            MemoryInstructionGateReason = "ttl_base_missing"
	MemoryInstructionGateTTLBaseUnparsable         MemoryInstructionGateReason = "ttl_base_unparsable"
	MemoryInstructionGateTTLExpired                MemoryInstructionGateReason = "ttl_expired"
)

type MemoryInstructionGateError struct {
	Reason MemoryInstructionGateReason
}

func (e MemoryInstructionGateError) Error() string {
	if e.Reason == "" {
		return "memory instruction gate failed"
	}
	return "memory instruction gate failed: " + string(e.Reason)
}

// [[invariant:instruction-authority-requires-reviewed-memory]]
// [[doc:docs/plans/features/semantic-code-anchors.md#Four-Plane Model]]
// [[test:internal/context/contextplane/memory_instruction_gate_test.go#TestValidateMemoryRecordForInstructionAcceptsActiveReviewedPolicyRule]]
func ValidateMemoryRecordForInstruction(record memorycore.Record, surface evidence.RenderSurface, now time.Time) error {
	if err := evidence.ValidateRenderSurface(instructionEvidenceMetaForMemoryRecord(record), surface); err != nil {
		return MemoryInstructionGateError{Reason: MemoryInstructionGateInvalidInstructionSurface}
	}
	if !isInstructionSurface(surface) {
		return MemoryInstructionGateError{Reason: MemoryInstructionGateInvalidInstructionSurface}
	}
	if now.IsZero() {
		return MemoryInstructionGateError{Reason: MemoryInstructionGateZeroNow}
	}
	if record.Lifecycle.State != memorycore.LifecycleStateActive {
		return MemoryInstructionGateError{Reason: MemoryInstructionGateInactiveLifecycle}
	}
	if record.Lifecycle.ReviewStatus == memorycore.ReviewStatusFailedValidation {
		return MemoryInstructionGateError{Reason: MemoryInstructionGateFailedValidation}
	}
	if record.Lifecycle.ReviewStatus != memorycore.ReviewStatusReviewed &&
		record.Lifecycle.ReviewStatus != memorycore.ReviewStatusValidated {
		return MemoryInstructionGateError{Reason: MemoryInstructionGateInvalidReviewStatus}
	}
	if record.Kind != memorycore.KindPolicyRule && record.Kind != memorycore.KindProceduralSkill {
		return MemoryInstructionGateError{Reason: MemoryInstructionGateInvalidInstructionKind}
	}
	if !record.Usage.InstructionEligible {
		return MemoryInstructionGateError{Reason: MemoryInstructionGateInstructionNotEligible}
	}
	if record.Usage.EvidenceOnly {
		return MemoryInstructionGateError{Reason: MemoryInstructionGateEvidenceOnly}
	}
	if record.Lifecycle.SupersededBy != "" {
		return MemoryInstructionGateError{Reason: MemoryInstructionGateSuperseded}
	}
	if record.Trust.Tainted {
		return MemoryInstructionGateError{Reason: MemoryInstructionGateTainted}
	}
	if err := validateMemoryInstructionTemporal(record, now); err != nil {
		return err
	}
	return nil
}

func instructionEvidenceMetaForMemoryRecord(record memorycore.Record) evidence.EvidenceMeta {
	authority := evidence.EvidenceAuthorityReviewed
	if record.Kind == memorycore.KindPolicyRule {
		authority = evidence.EvidenceAuthorityPolicy
	}
	return evidence.EvidenceMeta{
		Source:            evidence.EvidenceSourceDurableMemory,
		SourcePlane:       evidence.EvidencePlaneDurable,
		EvidenceClass:     evidence.EvidenceClassReviewedMemory,
		EvidenceAuthority: authority,
		AllowedAuthorityEffects: []evidence.AuthorityEffect{
			evidence.AuthorityEffectInstructionSource,
		},
	}
}

func isInstructionSurface(surface evidence.RenderSurface) bool {
	switch surface {
	case evidence.RenderSurfaceInstruction, evidence.RenderSurfacePolicy, evidence.RenderSurfaceHardConstraint,
		evidence.RenderSurfaceToolAuthorization, evidence.RenderSurfaceRuntimeGuardrail:
		return true
	default:
		return false
	}
}

func validateMemoryInstructionTemporal(record memorycore.Record, now time.Time) error {
	if record.Temporal.ValidUntil != "" {
		validUntil, err := parseMemoryInstructionTime(record.Temporal.ValidUntil)
		if err != nil {
			return MemoryInstructionGateError{Reason: MemoryInstructionGateValidUntilUnparsable}
		}
		if !now.Before(validUntil) {
			return MemoryInstructionGateError{Reason: MemoryInstructionGateValidUntilExpired}
		}
	}
	if record.Temporal.TTLSeconds <= 0 {
		return nil
	}
	baseRaw := firstNonEmpty(record.Temporal.ValidFrom, record.Temporal.ObservedAt, record.Temporal.IngestedAt)
	if baseRaw == "" {
		return MemoryInstructionGateError{Reason: MemoryInstructionGateTTLBaseMissing}
	}
	base, err := parseMemoryInstructionTime(baseRaw)
	if err != nil {
		return MemoryInstructionGateError{Reason: MemoryInstructionGateTTLBaseUnparsable}
	}
	expiresAt := base.Add(time.Duration(record.Temporal.TTLSeconds) * time.Second)
	if !now.Before(expiresAt) {
		return MemoryInstructionGateError{Reason: MemoryInstructionGateTTLExpired}
	}
	return nil
}

func parseMemoryInstructionTime(raw string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339Nano, raw)
}
