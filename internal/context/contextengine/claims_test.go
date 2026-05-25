package contextengine

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// roundTripJSON marshals and unmarshals a value to test JSON round-trip.
func roundTripJSON[T any](t *testing.T, v T) error {
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var got T
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	// Compare JSON serialization to ensure equality
	data2, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("re-marshal failed: %v", err)
	}
	if string(data) != string(data2) {
		t.Errorf("JSON differs after round-trip:\noriginal: %s\nround-tripped: %s", data, data2)
		return errors.New("JSON mismatch")
	}
	return nil
}

func TestClaimStatus_IsValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status ClaimStatus
		want   bool
	}{
		{ClaimStatusCandidate, true},
		{ClaimStatusCurrent, true},
		{ClaimStatusNeedsRevalidation, true},
		{ClaimStatusStale, true},
		{ClaimStatusSuperseded, true},
		{ClaimStatusRejected, true},
		{ClaimStatus("invalid"), false},
		{ClaimStatus(""), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			got := tt.status.IsValid()
			if got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClaimStatus_AllValues(t *testing.T) {
	t.Parallel()
	values := []ClaimStatus{
		ClaimStatusCandidate,
		ClaimStatusCurrent,
		ClaimStatusNeedsRevalidation,
		ClaimStatusStale,
		ClaimStatusSuperseded,
		ClaimStatusRejected,
	}
	for _, v := range values {
		if !v.IsValid() {
			t.Errorf("expected %q to be valid", v)
		}
	}
}

func TestClaimScope(t *testing.T) {
	t.Parallel()
	// Test JSON round-trip
	t.Run("RoundTrip", func(t *testing.T) {
		scope := ClaimScope{
			Path:      "/path/to/file",
			TaskID:    "task-123",
			SessionID: "session-456",
		}
		if err := roundTripJSON(t, scope); err != nil {
			t.Errorf("ClaimScope round-trip failed: %v", err)
		}
	})

	t.Run("PartialScope", func(t *testing.T) {
		scope := ClaimScope{
			Path: "/path/to/file",
		}
		if err := roundTripJSON(t, scope); err != nil {
			t.Errorf("ClaimScope partial round-trip failed: %v", err)
		}
	})

	t.Run("EmptyScope", func(t *testing.T) {
		scope := ClaimScope{}
		if err := roundTripJSON(t, scope); err != nil {
			t.Errorf("ClaimScope empty round-trip failed: %v", err)
		}
	})
}

func TestMemoryClaim_Validate(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)

	tests := []struct {
		name    string
		claim   MemoryClaim
		wantErr bool
	}{
		{
			name: "valid claim",
			claim: MemoryClaim{
				ID:          "claim-1",
				WorkspaceID: "ws-1",
				Status:      ClaimStatusCurrent,
				CreatedAt:   now,
				UpdatedAt:   now,
			},
			wantErr: false,
		},
		{
			name: "missing id",
			claim: MemoryClaim{
				WorkspaceID: "ws-1",
				Status:      ClaimStatusCurrent,
			},
			wantErr: true,
		},
		{
			name: "missing workspace_id",
			claim: MemoryClaim{
				ID:     "claim-1",
				Status: ClaimStatusCurrent,
			},
			wantErr: true,
		},
		{
			name: "invalid status",
			claim: MemoryClaim{
				ID:          "claim-1",
				WorkspaceID: "ws-1",
				Status:      ClaimStatus("invalid"),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.claim.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMemoryClaim_RoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	claim := MemoryClaim{
		ID:          "claim-123",
		WorkspaceID: "ws-abc",
		ClaimType:   "test_claim",
		Status:      ClaimStatusCurrent,
		Scope: ClaimScope{
			Path: "/path/to/file.go",
		},
		Summary:     "This is a test claim",
		Confidence:  0.85,
		BlastRadius: "low",
		SourceRefs: []EvidenceRef{
			{Type: RefTypePath, Ref: "main.go:42"},
		},
		SourceEventID: "evt-1",
		Reason:        "test reason",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := roundTripJSON(t, claim); err != nil {
		t.Errorf("MemoryClaim round-trip failed: %v", err)
	}
}

func TestMemoryClaim_ValidateWithAllStatuses(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	statuses := []ClaimStatus{
		ClaimStatusCandidate,
		ClaimStatusCurrent,
		ClaimStatusNeedsRevalidation,
		ClaimStatusStale,
		ClaimStatusSuperseded,
		ClaimStatusRejected,
	}
	for _, status := range statuses {
		claim := MemoryClaim{
			ID:          "claim-test",
			WorkspaceID: "ws-test",
			Status:      status,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := claim.Validate(); err != nil {
			t.Errorf("Validate() for status %q failed: %v", status, err)
		}
	}
}

func TestCanTransitionClaimStatus_ValidTransitions(t *testing.T) {
	t.Parallel()
	// Valid transitions: candidate → current, rejected, needs_revalidation
	validTransitions := []struct {
		from, to ClaimStatus
	}{
		// candidate transitions
		{ClaimStatusCandidate, ClaimStatusCurrent},
		{ClaimStatusCandidate, ClaimStatusRejected},
		{ClaimStatusCandidate, ClaimStatusNeedsRevalidation},
		// current transitions
		{ClaimStatusCurrent, ClaimStatusNeedsRevalidation},
		{ClaimStatusCurrent, ClaimStatusSuperseded},
		{ClaimStatusCurrent, ClaimStatusRejected},
		{ClaimStatusCurrent, ClaimStatusStale},
		// needs_revalidation transitions
		{ClaimStatusNeedsRevalidation, ClaimStatusCurrent},
		{ClaimStatusNeedsRevalidation, ClaimStatusStale},
		{ClaimStatusNeedsRevalidation, ClaimStatusSuperseded},
		{ClaimStatusNeedsRevalidation, ClaimStatusRejected},
		// stale transitions
		{ClaimStatusStale, ClaimStatusCurrent},
		{ClaimStatusStale, ClaimStatusSuperseded},
		// Same status is always valid (no-op)
		{ClaimStatusCandidate, ClaimStatusCandidate},
		{ClaimStatusCurrent, ClaimStatusCurrent},
	}
	for _, tt := range validTransitions {
		t.Run(string(tt.from)+"_to_"+string(tt.to), func(t *testing.T) {
			if !CanTransitionClaimStatus(tt.from, tt.to) {
				t.Errorf("CanTransitionClaimStatus(%s, %s) = false, want true", tt.from, tt.to)
			}
		})
	}
}

func TestCanTransitionClaimStatus_InvalidTransitions(t *testing.T) {
	t.Parallel()
	// Invalid transitions
	invalidTransitions := []struct {
		from, to ClaimStatus
	}{
		// superseded has no outgoing transitions
		{ClaimStatusSuperseded, ClaimStatusCurrent},
		{ClaimStatusSuperseded, ClaimStatusStale},
		// rejected has no outgoing transitions
		{ClaimStatusRejected, ClaimStatusCurrent},
		{ClaimStatusRejected, ClaimStatusStale},
		// candidate cannot go to stale
		{ClaimStatusCandidate, ClaimStatusStale},
		// candidate cannot go to superseded
		{ClaimStatusCandidate, ClaimStatusSuperseded},
	}
	for _, tt := range invalidTransitions {
		t.Run(string(tt.from)+"_to_"+string(tt.to), func(t *testing.T) {
			if CanTransitionClaimStatus(tt.from, tt.to) {
				t.Errorf("CanTransitionClaimStatus(%s, %s) = true, want false", tt.from, tt.to)
			}
		})
	}
}

func TestCanTransitionClaimStatus_InvalidStatuses(t *testing.T) {
	t.Parallel()
	invalidStatus := ClaimStatus("invalid")
	if CanTransitionClaimStatus(invalidStatus, ClaimStatusCurrent) {
		t.Error("CanTransitionClaimStatus with invalid 'from' should return false")
	}
	if CanTransitionClaimStatus(ClaimStatusCurrent, invalidStatus) {
		t.Error("CanTransitionClaimStatus with invalid 'to' should return false")
	}
}

func TestValidateClaimTransition(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		from    ClaimStatus
		to      ClaimStatus
		reason  string
		wantErr bool
	}{
		{
			name:    "valid same status",
			from:    ClaimStatusCurrent,
			to:      ClaimStatusCurrent,
			reason:  "",
			wantErr: false,
		},
		{
			name:    "valid promotion",
			from:    ClaimStatusCandidate,
			to:      ClaimStatusCurrent,
			reason:  "",
			wantErr: false,
		},
		{
			name:    "invalid from status",
			from:    ClaimStatus("invalid"),
			to:      ClaimStatusCurrent,
			reason:  "",
			wantErr: true,
		},
		{
			name:    "invalid to status",
			from:    ClaimStatusCurrent,
			to:      ClaimStatus("invalid"),
			reason:  "",
			wantErr: true,
		},
		{
			name:    "forbidden transition",
			from:    ClaimStatusCandidate,
			to:      ClaimStatusStale,
			reason:  "",
			wantErr: true,
		},
		{
			name:    "demotion without reason",
			from:    ClaimStatusCurrent,
			to:      ClaimStatusRejected,
			reason:  "",
			wantErr: true,
		},
		{
			name:    "demotion with reason",
			from:    ClaimStatusCurrent,
			to:      ClaimStatusRejected,
			reason:  "claim disproved by evidence",
			wantErr: false,
		},
		{
			name:    "demotion from needs_revalidation without reason",
			from:    ClaimStatusNeedsRevalidation,
			to:      ClaimStatusStale,
			reason:  "",
			wantErr: true,
		},
		{
			name:    "demotion from needs_revalidation with reason",
			from:    ClaimStatusNeedsRevalidation,
			to:      ClaimStatusStale,
			reason:  "stale after timeout",
			wantErr: false,
		},
		{
			name:    "demotion from stale without reason",
			from:    ClaimStatusStale,
			to:      ClaimStatusRejected,
			reason:  "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateClaimTransition(tt.from, tt.to, tt.reason)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateClaimTransition() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClaimTransitionStateMachineInvariants(t *testing.T) {
	t.Parallel()

	statuses := []ClaimStatus{
		ClaimStatusCandidate,
		ClaimStatusCurrent,
		ClaimStatusNeedsRevalidation,
		ClaimStatusStale,
		ClaimStatusSuperseded,
		ClaimStatusRejected,
	}
	terminal := map[ClaimStatus]bool{
		ClaimStatusSuperseded: true,
		ClaimStatusRejected:   true,
	}
	requiresReason := func(from, to ClaimStatus) bool {
		if from == to {
			return false
		}
		switch from {
		case ClaimStatusCurrent, ClaimStatusNeedsRevalidation, ClaimStatusStale:
			return to != ClaimStatusCurrent
		default:
			return false
		}
	}

	for _, from := range statuses {
		for _, to := range statuses {
			canTransition := CanTransitionClaimStatus(from, to)
			errWithoutReason := ValidateClaimTransition(from, to, "")
			errWithReason := ValidateClaimTransition(from, to, "verified state change")

			if from == to {
				if !canTransition {
					t.Fatalf("%s -> %s no-op transition should be allowed", from, to)
				}
				if errWithoutReason != nil || errWithReason != nil {
					t.Fatalf("%s -> %s no-op validation failed without=%v with=%v", from, to, errWithoutReason, errWithReason)
				}
				continue
			}

			if terminal[from] {
				if canTransition || !errors.Is(errWithReason, ErrInvalidTransition) {
					t.Fatalf("terminal status %s should not transition to %s: can=%v err=%v", from, to, canTransition, errWithReason)
				}
				continue
			}

			if !canTransition {
				if !errors.Is(errWithReason, ErrInvalidTransition) {
					t.Fatalf("forbidden transition %s -> %s should return ErrInvalidTransition, got %v", from, to, errWithReason)
				}
				continue
			}

			if requiresReason(from, to) {
				if errWithoutReason == nil {
					t.Fatalf("transition %s -> %s should require a reason", from, to)
				}
				if errWithReason != nil {
					t.Fatalf("transition %s -> %s with reason should pass: %v", from, to, errWithReason)
				}
				continue
			}

			if errWithoutReason != nil || errWithReason != nil {
				t.Fatalf("transition %s -> %s should pass without=%v with=%v", from, to, errWithoutReason, errWithReason)
			}
		}
	}
}

func TestValidateClaimTransition_ErrorsAreClaimTransitionErrors(t *testing.T) {
	t.Parallel()
	// Invalid transition should return ClaimTransitionError
	err := ValidateClaimTransition(ClaimStatusCandidate, ClaimStatusSuperseded, "")
	// Verify it's a transition error
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}
	// The error should unwrap to ErrInvalidTransition for errors.Is
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("error should unwrap to ErrInvalidTransition, got: %v", err)
	}
	// Check error message format
	expectedMsg := "claim: invalid transition candidate -> superseded"
	if err.Error() != expectedMsg {
		t.Errorf("Error() = %q, want %q", err.Error(), expectedMsg)
	}
}

func TestApplyClaimTransition(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	claim := MemoryClaim{
		ID:          "claim-1",
		WorkspaceID: "ws-1",
		Status:      ClaimStatusCandidate,
		Reason:      "",
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}

	// Apply valid transition
	updated, err := ApplyClaimTransition(claim, ClaimStatusCurrent, "validated", now)
	if err != nil {
		t.Errorf("ApplyClaimTransition() failed: %v", err)
	}
	if updated.Status != ClaimStatusCurrent {
		t.Errorf("Status = %v, want %v", updated.Status, ClaimStatusCurrent)
	}
	if updated.Reason != "validated" {
		t.Errorf("Reason = %v, want %v", updated.Reason, "validated")
	}
	if updated.UpdatedAt != now {
		t.Errorf("UpdatedAt = %v, want %v", updated.UpdatedAt, now)
	}
}

func TestApplyClaimTransition_SameStatus(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	claim := MemoryClaim{
		ID:          "claim-1",
		WorkspaceID: "ws-1",
		Status:      ClaimStatusCurrent,
		Reason:      "original",
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}

	// Applying same status should be a no-op
	updated, err := ApplyClaimTransition(claim, ClaimStatusCurrent, "", now)
	if err != nil {
		t.Errorf("ApplyClaimTransition() with same status failed: %v", err)
	}
	if updated.Reason != "original" {
		t.Errorf("Reason should not change for same-status: got %v, want %v", updated.Reason, "original")
	}
	if !updated.UpdatedAt.Equal(claim.UpdatedAt) {
		t.Error("UpdatedAt should not change for same-status")
	}
}

func TestApplyClaimTransition_InvalidTransition(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	claim := MemoryClaim{
		ID:          "claim-1",
		WorkspaceID: "ws-1",
		Status:      ClaimStatusCandidate,
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}

	_, err := ApplyClaimTransition(claim, ClaimStatusStale, "", now)
	if err == nil {
		t.Error("ApplyClaimTransition() should fail for invalid transition")
	}
}

func TestApplyClaimTransition_DemotionWithoutReason(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	claim := MemoryClaim{
		ID:          "claim-1",
		WorkspaceID: "ws-1",
		Status:      ClaimStatusCurrent,
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}

	// Demotion from current requires reason
	_, err := ApplyClaimTransition(claim, ClaimStatusRejected, "", now)
	if err == nil {
		t.Error("ApplyClaimTransition() should fail demotion without reason")
	}
}

func TestApplyClaimTransition_AllValidPaths(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)

	validPaths := []struct {
		from, to ClaimStatus
		reason   string
	}{
		{ClaimStatusCandidate, ClaimStatusCurrent, "promoted"},
		{ClaimStatusCandidate, ClaimStatusRejected, "rejected"},
		{ClaimStatusCandidate, ClaimStatusNeedsRevalidation, "needs review"},
		{ClaimStatusCurrent, ClaimStatusNeedsRevalidation, "needs review"},
		{ClaimStatusCurrent, ClaimStatusSuperseded, "superseded"},
		{ClaimStatusCurrent, ClaimStatusRejected, "rejected"},
		{ClaimStatusCurrent, ClaimStatusStale, "stale"},
		{ClaimStatusNeedsRevalidation, ClaimStatusCurrent, "revalidated"},
		{ClaimStatusNeedsRevalidation, ClaimStatusStale, "stale"},
		{ClaimStatusNeedsRevalidation, ClaimStatusSuperseded, "superseded"},
		{ClaimStatusNeedsRevalidation, ClaimStatusRejected, "rejected"},
		{ClaimStatusStale, ClaimStatusCurrent, "refreshed"},
		{ClaimStatusStale, ClaimStatusSuperseded, "superseded"},
	}

	for _, path := range validPaths {
		t.Run(string(path.from)+"_to_"+string(path.to), func(t *testing.T) {
			claim := MemoryClaim{
				ID:          "claim-test",
				WorkspaceID: "ws-test",
				Status:      path.from,
				CreatedAt:   now.Add(-time.Hour),
				UpdatedAt:   now.Add(-time.Hour),
			}
			_, err := ApplyClaimTransition(claim, path.to, path.reason, now)
			if err != nil {
				t.Errorf("ApplyClaimTransition() failed for %s -> %s: %v", path.from, path.to, err)
			}
		})
	}
}

func TestClaimTransitionMatrix_Coverage(t *testing.T) {
	t.Parallel()
	// Ensure all statuses are covered in the transition matrix
	allStatuses := []ClaimStatus{
		ClaimStatusCandidate,
		ClaimStatusCurrent,
		ClaimStatusNeedsRevalidation,
		ClaimStatusStale,
		ClaimStatusSuperseded,
		ClaimStatusRejected,
	}
	for _, from := range allStatuses {
		for _, to := range allStatuses {
			// This test just ensures no panic occurs and the function returns deterministically
			_ = CanTransitionClaimStatus(from, to)
		}
	}
}
