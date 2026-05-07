package contextplane

import (
	"errors"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/memorycore"
	"github.com/joshka0/foxctl/internal/intelligence/evidence"
)

func TestValidateMemoryRecordForInstructionAcceptsActiveReviewedPolicyRule(t *testing.T) {
	now := memoryInstructionGateNow()

	if err := ValidateMemoryRecordForInstruction(validInstructionMemoryRecord(), evidence.RenderSurfacePolicy, now); err != nil {
		t.Fatalf("ValidateMemoryRecordForInstruction() error = %v", err)
	}
}

func TestValidateMemoryRecordForInstructionRequiresProjectionGateBeyondGenericSurface(t *testing.T) {
	now := memoryInstructionGateNow()
	record := validInstructionMemoryRecord()
	record.Lifecycle.State = memorycore.LifecycleStateStale

	if err := evidence.ValidateRenderSurface(instructionEvidenceMetaForMemoryRecord(record), evidence.RenderSurfaceInstruction); err != nil {
		t.Fatalf("generic ValidateRenderSurface() error = %v", err)
	}
	assertMemoryInstructionGateReason(t,
		ValidateMemoryRecordForInstruction(record, evidence.RenderSurfaceInstruction, now),
		MemoryInstructionGateInactiveLifecycle,
	)
}

func TestValidateMemoryRecordForInstructionRejectsInvalidSurfaces(t *testing.T) {
	now := memoryInstructionGateNow()
	for _, surface := range []evidence.RenderSurface{
		evidence.RenderSurfaceEvidencePack,
		evidence.RenderSurfaceReview,
		evidence.RenderSurfaceReviewWarning,
		evidence.RenderSurfaceEvidenceHint,
		evidence.RenderSurface("not-a-surface"),
	} {
		t.Run(string(surface), func(t *testing.T) {
			assertMemoryInstructionGateReason(t,
				ValidateMemoryRecordForInstruction(validInstructionMemoryRecord(), surface, now),
				MemoryInstructionGateInvalidInstructionSurface,
			)
		})
	}
}

func TestValidateMemoryRecordForInstructionRejectsNonTemporalEligibilityFailures(t *testing.T) {
	now := memoryInstructionGateNow()

	tests := []struct {
		name   string
		mutate func(*memorycore.Record)
		want   MemoryInstructionGateReason
	}{
		{
			name: "inactive lifecycle",
			mutate: func(record *memorycore.Record) {
				record.Lifecycle.State = memorycore.LifecycleStateCandidate
			},
			want: MemoryInstructionGateInactiveLifecycle,
		},
		{
			name: "instruction eligible false",
			mutate: func(record *memorycore.Record) {
				record.Usage.InstructionEligible = false
			},
			want: MemoryInstructionGateInstructionNotEligible,
		},
		{
			name: "evidence only true",
			mutate: func(record *memorycore.Record) {
				record.Usage.EvidenceOnly = true
			},
			want: MemoryInstructionGateEvidenceOnly,
		},
		{
			name: "superseded by",
			mutate: func(record *memorycore.Record) {
				record.Lifecycle.SupersededBy = "memory-new"
			},
			want: MemoryInstructionGateSuperseded,
		},
		{
			name: "tainted",
			mutate: func(record *memorycore.Record) {
				record.Trust.Tainted = true
			},
			want: MemoryInstructionGateTainted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := validInstructionMemoryRecord()
			tt.mutate(&record)
			assertMemoryInstructionGateReason(t,
				ValidateMemoryRecordForInstruction(record, evidence.RenderSurfaceInstruction, now),
				tt.want,
			)
		})
	}
}

func TestValidateMemoryRecordForInstructionReviewStatusGate(t *testing.T) {
	now := memoryInstructionGateNow()

	tests := []struct {
		status memorycore.ReviewStatus
		want   MemoryInstructionGateReason
	}{
		{status: memorycore.ReviewStatusReviewed},
		{status: memorycore.ReviewStatusValidated},
		{status: memorycore.ReviewStatusUnreviewed, want: MemoryInstructionGateInvalidReviewStatus},
		{status: memorycore.ReviewStatusNeedsReview, want: MemoryInstructionGateInvalidReviewStatus},
		{status: memorycore.ReviewStatusFailedValidation, want: MemoryInstructionGateFailedValidation},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			record := validInstructionMemoryRecord()
			record.Lifecycle.ReviewStatus = tt.status

			err := ValidateMemoryRecordForInstruction(record, evidence.RenderSurfaceInstruction, now)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("ValidateMemoryRecordForInstruction() error = %v", err)
				}
				return
			}
			assertMemoryInstructionGateReason(t, err, tt.want)
		})
	}
}

func TestValidateMemoryRecordForInstructionKindAllowlist(t *testing.T) {
	now := memoryInstructionGateNow()

	tests := []struct {
		kind memorycore.Kind
		want MemoryInstructionGateReason
	}{
		{kind: memorycore.KindPolicyRule},
		{kind: memorycore.KindProceduralSkill},
		{kind: memorycore.KindDecision, want: MemoryInstructionGateInvalidInstructionKind},
		{kind: memorycore.KindSemanticFact, want: MemoryInstructionGateInvalidInstructionKind},
		{kind: memorycore.KindWorkingContext, want: MemoryInstructionGateInvalidInstructionKind},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			record := validInstructionMemoryRecord()
			record.Kind = tt.kind

			err := ValidateMemoryRecordForInstruction(record, evidence.RenderSurfaceInstruction, now)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("ValidateMemoryRecordForInstruction() error = %v", err)
				}
				return
			}
			assertMemoryInstructionGateReason(t, err, tt.want)
		})
	}
}

func TestValidateMemoryRecordForInstructionSupersedesDoesNotReject(t *testing.T) {
	record := validInstructionMemoryRecord()
	record.Lifecycle.Supersedes = []string{"memory-old"}

	if err := ValidateMemoryRecordForInstruction(record, evidence.RenderSurfaceInstruction, memoryInstructionGateNow()); err != nil {
		t.Fatalf("ValidateMemoryRecordForInstruction() error = %v", err)
	}
}

func TestValidateMemoryRecordForInstructionTemporalGate(t *testing.T) {
	now := memoryInstructionGateNow()

	tests := []struct {
		name   string
		mutate func(*memorycore.Record)
		want   MemoryInstructionGateReason
	}{
		{
			name: "valid until unparsable",
			mutate: func(record *memorycore.Record) {
				record.Temporal.ValidUntil = "tomorrow"
			},
			want: MemoryInstructionGateValidUntilUnparsable,
		},
		{
			name: "valid until equality expired",
			mutate: func(record *memorycore.Record) {
				record.Temporal.ValidUntil = now.Format(time.RFC3339)
			},
			want: MemoryInstructionGateValidUntilExpired,
		},
		{
			name: "valid until past expired",
			mutate: func(record *memorycore.Record) {
				record.Temporal.ValidUntil = now.Add(-time.Nanosecond).Format(time.RFC3339Nano)
			},
			want: MemoryInstructionGateValidUntilExpired,
		},
		{
			name: "valid until future accepted",
			mutate: func(record *memorycore.Record) {
				record.Temporal.ValidUntil = now.Add(time.Nanosecond).Format(time.RFC3339Nano)
			},
		},
		{
			name: "ttl missing base",
			mutate: func(record *memorycore.Record) {
				record.Temporal.TTLSeconds = 60
			},
			want: MemoryInstructionGateTTLBaseMissing,
		},
		{
			name: "ttl unparsable base",
			mutate: func(record *memorycore.Record) {
				record.Temporal.TTLSeconds = 60
				record.Temporal.ValidFrom = "yesterday"
			},
			want: MemoryInstructionGateTTLBaseUnparsable,
		},
		{
			name: "ttl equality expired",
			mutate: func(record *memorycore.Record) {
				record.Temporal.TTLSeconds = 60
				record.Temporal.ValidFrom = now.Add(-60 * time.Second).Format(time.RFC3339)
			},
			want: MemoryInstructionGateTTLExpired,
		},
		{
			name: "ttl past expired",
			mutate: func(record *memorycore.Record) {
				record.Temporal.TTLSeconds = 60
				record.Temporal.ObservedAt = now.Add(-61 * time.Second).Format(time.RFC3339)
			},
			want: MemoryInstructionGateTTLExpired,
		},
		{
			name: "ttl unexpired accepted",
			mutate: func(record *memorycore.Record) {
				record.Temporal.TTLSeconds = 60
				record.Temporal.IngestedAt = now.Add(-59 * time.Second).Format(time.RFC3339)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := validInstructionMemoryRecord()
			tt.mutate(&record)

			err := ValidateMemoryRecordForInstruction(record, evidence.RenderSurfaceInstruction, now)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("ValidateMemoryRecordForInstruction() error = %v", err)
				}
				return
			}
			assertMemoryInstructionGateReason(t, err, tt.want)
		})
	}
}

func TestValidateMemoryRecordForInstructionRejectsZeroNow(t *testing.T) {
	assertMemoryInstructionGateReason(t,
		ValidateMemoryRecordForInstruction(validInstructionMemoryRecord(), evidence.RenderSurfaceInstruction, time.Time{}),
		MemoryInstructionGateZeroNow,
	)
}

func validInstructionMemoryRecord() memorycore.Record {
	return memorycore.Record{
		ID:   "memory-policy-rule",
		Kind: memorycore.KindPolicyRule,
		Trust: memorycore.TrustEnvelope{
			SourceTrust: "reviewed",
			Confidence:  0.9,
			Authority:   1.0,
		},
		Lifecycle: memorycore.LifecycleEnvelope{
			State:        memorycore.LifecycleStateActive,
			ReviewStatus: memorycore.ReviewStatusReviewed,
		},
		Usage: memorycore.UsageEnvelope{
			InstructionEligible: true,
			EvidenceOnly:        false,
		},
	}
}

func memoryInstructionGateNow() time.Time {
	return time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
}

func assertMemoryInstructionGateReason(t *testing.T, err error, want MemoryInstructionGateReason) {
	t.Helper()
	if err == nil {
		t.Fatalf("ValidateMemoryRecordForInstruction() error = nil, want %s", want)
	}
	var gateErr MemoryInstructionGateError
	if !errors.As(err, &gateErr) {
		t.Fatalf("ValidateMemoryRecordForInstruction() error = %T %v, want MemoryInstructionGateError", err, err)
	}
	if gateErr.Reason != want {
		t.Fatalf("gate reason = %s, want %s", gateErr.Reason, want)
	}
}
