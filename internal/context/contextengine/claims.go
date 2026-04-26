package contextengine

import (
	"fmt"
	"time"
)

// ClaimStatus is the lifecycle status of a memory claim.
type ClaimStatus string

const (
	ClaimStatusCandidate         ClaimStatus = "candidate"
	ClaimStatusCurrent           ClaimStatus = "current"
	ClaimStatusNeedsRevalidation ClaimStatus = "needs_revalidation"
	ClaimStatusStale             ClaimStatus = "stale"
	ClaimStatusSuperseded        ClaimStatus = "superseded"
	ClaimStatusRejected          ClaimStatus = "rejected"
)

// IsValid reports whether s is a known ClaimStatus.
func (s ClaimStatus) IsValid() bool {
	switch s {
	case ClaimStatusCandidate, ClaimStatusCurrent, ClaimStatusNeedsRevalidation,
		ClaimStatusStale, ClaimStatusSuperseded, ClaimStatusRejected:
		return true
	default:
		return false
	}
}

// ClaimScope describes what a claim applies to.
type ClaimScope struct {
	// Path is the file or directory scope, if applicable.
	Path string `json:"path,omitempty"`
	// TaskID is the task scope, if applicable.
	TaskID string `json:"task_id,omitempty"`
	// SessionID is the session scope, if applicable.
	SessionID string `json:"session_id,omitempty"`
}

// MemoryClaim is a derived memory assertion with a lifecycle.
type MemoryClaim struct {
	// ID is the unique claim identifier.
	ID string `json:"id"`
	// WorkspaceID is the owning workspace.
	WorkspaceID string `json:"workspace_id"`
	// ClaimType describes the kind of claim.
	ClaimType string `json:"claim_type"`
	// Status is the current lifecycle status.
	Status ClaimStatus `json:"status"`
	// Scope describes what the claim applies to.
	Scope ClaimScope `json:"scope,omitempty"`
	// Summary is a short description of the claim.
	Summary string `json:"summary,omitempty"`
	// Confidence is a 0-1 score.
	Confidence float64 `json:"confidence,omitempty"`
	// BlastRadius describes expected impact if the claim is wrong.
	BlastRadius string `json:"blast_radius,omitempty"`
	// SourceRefs point at the evidence that generated this claim.
	SourceRefs []EvidenceRef `json:"source_refs,omitempty"`
	// SourceEventID is the event that created this claim.
	SourceEventID string `json:"source_event_id,omitempty"`
	// SupersededBy is the claim that superseded this one, if any.
	SupersededBy string `json:"superseded_by,omitempty"`
	// Reason is why the current status was set (required for demotions).
	Reason string `json:"reason,omitempty"`
	// CreatedAt is when the claim was created.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is when the claim was last changed.
	UpdatedAt time.Time `json:"updated_at"`
}

// Validate checks that the claim has required fields.
func (c MemoryClaim) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("memory claim: missing id")
	}
	if c.WorkspaceID == "" {
		return fmt.Errorf("memory claim: missing workspace_id")
	}
	if !c.Status.IsValid() {
		return fmt.Errorf("memory claim: unknown status %q", c.Status)
	}
	return nil
}

// claimTransitionValid records which claim transitions are allowed.
var claimTransitionValid = map[ClaimStatus]map[ClaimStatus]bool{
	ClaimStatusCandidate: {
		ClaimStatusCurrent:           true,
		ClaimStatusRejected:         true,
		ClaimStatusNeedsRevalidation: true,
	},
	ClaimStatusCurrent: {
		ClaimStatusNeedsRevalidation: true,
		ClaimStatusSuperseded:       true,
		ClaimStatusRejected:         true,
		ClaimStatusStale:            true,
	},
	ClaimStatusNeedsRevalidation: {
		ClaimStatusCurrent:    true,
		ClaimStatusStale:      true,
		ClaimStatusSuperseded: true,
		ClaimStatusRejected:   true,
	},
	ClaimStatusStale: {
		ClaimStatusCurrent:    true,
		ClaimStatusSuperseded: true,
	},
	ClaimStatusSuperseded: {},
	ClaimStatusRejected:   {},
}

// ClaimTransitionError describes an invalid claim transition.
type ClaimTransitionError struct {
	From   ClaimStatus `json:"from"`
	To     ClaimStatus `json:"to"`
	Reason string      `json:"reason,omitempty"`
}

func (e ClaimTransitionError) Error() string {
	return fmt.Sprintf("claim: invalid transition %s -> %s", e.From, e.To)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e ClaimTransitionError) Unwrap() error {
	return ErrInvalidTransition
}

// CanTransitionClaimStatus reports whether a claim can move from one status to another.
// Valid transitions:
//   - candidate → current, rejected, needs_revalidation
//   - current → needs_revalidation, superseded, rejected, stale
//   - needs_revalidation → current, stale, superseded, rejected
//   - stale → current, superseded
//   - superseded → (none)
//   - rejected → (none)
func CanTransitionClaimStatus(from, to ClaimStatus) bool {
	if from == to {
		return true
	}
	if !from.IsValid() || !to.IsValid() {
		return false
	}
	allowed, ok := claimTransitionValid[from]
	if !ok {
		return false
	}
	return allowed[to]
}

// ValidateClaimTransition checks that a transition is valid and that demotions
// from current/NeedsRevalidation/Stale have a non-empty reason.
func ValidateClaimTransition(from, to ClaimStatus, reason string) error {
	if !from.IsValid() {
		return fmt.Errorf("%w: from=%q", ErrInvalidClaimStatus, from)
	}
	if !to.IsValid() {
		return fmt.Errorf("%w: to=%q", ErrInvalidClaimStatus, to)
	}
	if !CanTransitionClaimStatus(from, to) {
		return ClaimTransitionError{From: from, To: to}
	}
	// Demotions require a reason
	demotion := (from == ClaimStatusCurrent || from == ClaimStatusNeedsRevalidation || from == ClaimStatusStale) &&
		(to != ClaimStatusCurrent && from != to)
	if demotion && reason == "" {
		return fmt.Errorf("claim: demotion %s -> %s requires non-empty reason", from, to)
	}
	return nil
}

// ApplyClaimTransition applies a validated transition and returns the updated claim.
func ApplyClaimTransition(claim MemoryClaim, to ClaimStatus, reason string, at time.Time) (MemoryClaim, error) {
	if err := ValidateClaimTransition(claim.Status, to, reason); err != nil {
		return MemoryClaim{}, err
	}
	if claim.Status == to {
		return claim, nil
	}
	claim.Status = to
	claim.Reason = reason
	claim.UpdatedAt = at
	return claim, nil
}
