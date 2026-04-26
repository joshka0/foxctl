package contextengine

import (
	"testing"
	"time"
)

func TestStalenessStatus_IsValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status  StalenessStatus
		want    bool
	}{
		{StalenessStatusFresh, true},
		{StalenessStatusDirty, true},
		{StalenessStatusNeedsRevalidation, true},
		{StalenessStatusStale, true},
		{StalenessStatusContradicted, true},
		{StalenessStatusSuperseded, true},
		{StalenessStatusUnknown, true},
		{StalenessStatus("invalid"), false},
		{StalenessStatus(""), false},
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

func TestStalenessStatus_AllValues(t *testing.T) {
	t.Parallel()
	values := []StalenessStatus{
		StalenessStatusFresh,
		StalenessStatusDirty,
		StalenessStatusNeedsRevalidation,
		StalenessStatusStale,
		StalenessStatusContradicted,
		StalenessStatusSuperseded,
		StalenessStatusUnknown,
	}
	for _, v := range values {
		if !v.IsValid() {
			t.Errorf("expected %q to be valid", v)
		}
	}
}

func TestStalenessMarker_Validate(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)

	tests := []struct {
		name    string
		marker  StalenessMarker
		wantErr bool
	}{
		{
			name: "valid fresh marker",
			marker: StalenessMarker{
				ID:          "marker-1",
				WorkspaceID: "ws-1",
				TargetRef:   EvidenceRef{Type: RefTypePath, Ref: "main.go:42"},
				Status:      StalenessStatusFresh,
				ResolvedByEvent: "evt-1",
				CreatedAt:   now,
				UpdatedAt:   now,
			},
			wantErr: false,
		},
		{
			name: "valid dirty marker",
			marker: StalenessMarker{
				ID:          "marker-2",
				WorkspaceID: "ws-1",
				TargetRef:   EvidenceRef{Type: RefTypePath, Ref: "main.go:42"},
				Status:      StalenessStatusDirty,
				CausedByEvents: []string{"evt-1"},
				CreatedAt:   now,
				UpdatedAt:   now,
			},
			wantErr: false,
		},
		{
			name: "valid superseded marker",
			marker: StalenessMarker{
				ID:          "marker-3",
				WorkspaceID: "ws-1",
				TargetRef:   EvidenceRef{Type: RefTypePath, Ref: "main.go:42"},
				Status:      StalenessStatusSuperseded,
				ResolvedByEvent: "evt-2",
				CreatedAt:   now,
				UpdatedAt:   now,
			},
			wantErr: false,
		},
		{
			name: "missing id",
			marker: StalenessMarker{
				WorkspaceID: "ws-1",
				TargetRef:   EvidenceRef{Type: RefTypePath, Ref: "main.go:42"},
				Status:      StalenessStatusFresh,
			},
			wantErr: true,
		},
		{
			name: "missing workspace_id",
			marker: StalenessMarker{
				ID:        "marker-1",
				TargetRef: EvidenceRef{Type: RefTypePath, Ref: "main.go:42"},
				Status:    StalenessStatusFresh,
			},
			wantErr: true,
		},
		{
			name: "invalid target ref",
			marker: StalenessMarker{
				ID:          "marker-1",
				WorkspaceID: "ws-1",
				TargetRef:   EvidenceRef{Type: RefTypePath, Ref: ""},
				Status:      StalenessStatusFresh,
			},
			wantErr: true,
		},
		{
			name: "invalid status",
			marker: StalenessMarker{
				ID:          "marker-1",
				WorkspaceID: "ws-1",
				TargetRef:   EvidenceRef{Type: RefTypePath, Ref: "main.go:42"},
				Status:      StalenessStatus("invalid"),
			},
			wantErr: true,
		},
		{
			name: "dirty without caused_by_events",
			marker: StalenessMarker{
				ID:          "marker-1",
				WorkspaceID: "ws-1",
				TargetRef:   EvidenceRef{Type: RefTypePath, Ref: "main.go:42"},
				Status:      StalenessStatusDirty,
				CreatedAt:   now,
				UpdatedAt:   now,
			},
			wantErr: true,
		},
		{
			name: "fresh without resolved_by_event",
			marker: StalenessMarker{
				ID:          "marker-1",
				WorkspaceID: "ws-1",
				TargetRef:   EvidenceRef{Type: RefTypePath, Ref: "main.go:42"},
				Status:      StalenessStatusFresh,
				CreatedAt:   now,
				UpdatedAt:   now,
			},
			wantErr: true,
		},
		{
			name: "superseded without resolved_by_event",
			marker: StalenessMarker{
				ID:          "marker-1",
				WorkspaceID: "ws-1",
				TargetRef:   EvidenceRef{Type: RefTypePath, Ref: "main.go:42"},
				Status:      StalenessStatusSuperseded,
				CreatedAt:   now,
				UpdatedAt:   now,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.marker.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestStalenessMarker_RoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	marker := StalenessMarker{
		ID:            "marker-123",
		WorkspaceID:   "ws-abc",
		TargetRef:     EvidenceRef{Type: RefTypePath, Ref: "main.go:42", WorkspaceID: "ws-abc"},
		Status:        StalenessStatusFresh,
		CausedByEvents: []string{"evt-1", "evt-2"},
		ResolvedByEvent: "evt-3",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := roundTripJSON(t, marker); err != nil {
		t.Errorf("StalenessMarker round-trip failed: %v", err)
	}
}

func TestStalenessMarker_ValidateWithAllStatuses(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	statuses := []StalenessStatus{
		StalenessStatusFresh,
		StalenessStatusDirty,
		StalenessStatusNeedsRevalidation,
		StalenessStatusStale,
		StalenessStatusContradicted,
		StalenessStatusSuperseded,
		StalenessStatusUnknown,
	}

	for _, status := range statuses {
		var marker StalenessMarker
		switch status {
		case StalenessStatusDirty:
			marker = StalenessMarker{
				ID:          "marker-test",
				WorkspaceID: "ws-test",
				TargetRef:   EvidenceRef{Type: RefTypePath, Ref: "main.go:42"},
				Status:      status,
				CausedByEvents: []string{"evt-1"},
				CreatedAt:   now,
				UpdatedAt:   now,
			}
		case StalenessStatusFresh, StalenessStatusSuperseded:
			marker = StalenessMarker{
				ID:          "marker-test",
				WorkspaceID: "ws-test",
				TargetRef:   EvidenceRef{Type: RefTypePath, Ref: "main.go:42"},
				Status:      status,
				ResolvedByEvent: "evt-1",
				CreatedAt:   now,
				UpdatedAt:   now,
			}
		default:
			marker = StalenessMarker{
				ID:          "marker-test",
				WorkspaceID: "ws-test",
				TargetRef:   EvidenceRef{Type: RefTypePath, Ref: "main.go:42"},
				Status:      status,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
		}
		if err := marker.Validate(); err != nil {
			t.Errorf("Validate() for status %q failed: %v", status, err)
		}
	}
}

func TestCanTransitionStalenessStatus_ValidTransitions(t *testing.T) {
	t.Parallel()
	// Valid transitions per spec:
	// - fresh → dirty, needs_revalidation, unknown
	// - dirty → needs_revalidation
	// - needs_revalidation → fresh, stale, superseded
	// - stale → superseded
	// - unknown → needs_revalidation
	validTransitions := []struct {
		from, to StalenessStatus
	}{
		// fresh transitions
		{StalenessStatusFresh, StalenessStatusDirty},
		{StalenessStatusFresh, StalenessStatusNeedsRevalidation},
		{StalenessStatusFresh, StalenessStatusUnknown},
		// dirty transitions
		{StalenessStatusDirty, StalenessStatusNeedsRevalidation},
		// needs_revalidation transitions
		{StalenessStatusNeedsRevalidation, StalenessStatusFresh},
		{StalenessStatusNeedsRevalidation, StalenessStatusStale},
		{StalenessStatusNeedsRevalidation, StalenessStatusSuperseded},
		// stale transitions
		{StalenessStatusStale, StalenessStatusSuperseded},
		// unknown transitions
		{StalenessStatusUnknown, StalenessStatusNeedsRevalidation},
		// Same status is always valid (no-op)
		{StalenessStatusFresh, StalenessStatusFresh},
		{StalenessStatusDirty, StalenessStatusDirty},
	}
	for _, tt := range validTransitions {
		t.Run(string(tt.from)+"_to_"+string(tt.to), func(t *testing.T) {
			if !CanTransitionStalenessStatus(tt.from, tt.to) {
				t.Errorf("CanTransitionStalenessStatus(%s, %s) = false, want true", tt.from, tt.to)
			}
		})
	}
}

func TestCanTransitionStalenessStatus_InvalidTransitions(t *testing.T) {
	t.Parallel()
	// Invalid transitions:
	// - contradicted has no outgoing transitions
	// - superseded has no outgoing transitions
	// - fresh cannot go directly to stale
	// - fresh cannot go directly to superseded
	// - dirty cannot go to fresh
	invalidTransitions := []struct {
		from, to StalenessStatus
	}{
		{StalenessStatusContradicted, StalenessStatusFresh},
		{StalenessStatusContradicted, StalenessStatusStale},
		{StalenessStatusSuperseded, StalenessStatusFresh},
		{StalenessStatusSuperseded, StalenessStatusStale},
		{StalenessStatusFresh, StalenessStatusStale},
		{StalenessStatusFresh, StalenessStatusSuperseded},
		{StalenessStatusDirty, StalenessStatusFresh},
		{StalenessStatusDirty, StalenessStatusStale},
		{StalenessStatusStale, StalenessStatusFresh},
		{StalenessStatusUnknown, StalenessStatusFresh},
		{StalenessStatusNeedsRevalidation, StalenessStatusDirty},
	}
	for _, tt := range invalidTransitions {
		t.Run(string(tt.from)+"_to_"+string(tt.to), func(t *testing.T) {
			if CanTransitionStalenessStatus(tt.from, tt.to) {
				t.Errorf("CanTransitionStalenessStatus(%s, %s) = true, want false", tt.from, tt.to)
			}
		})
	}
}

func TestCanTransitionStalenessStatus_InvalidStatuses(t *testing.T) {
	t.Parallel()
	invalidStatus := StalenessStatus("invalid")
	if CanTransitionStalenessStatus(invalidStatus, StalenessStatusFresh) {
		t.Error("CanTransitionStalenessStatus with invalid 'from' should return false")
	}
	if CanTransitionStalenessStatus(StalenessStatusFresh, invalidStatus) {
		t.Error("CanTransitionStalenessStatus with invalid 'to' should return false")
	}
}

func TestValidateStalenessTransition(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		from    StalenessStatus
		to      StalenessStatus
		wantErr bool
	}{
		{
			name:    "valid same status",
			from:    StalenessStatusFresh,
			to:      StalenessStatusFresh,
			wantErr: false,
		},
		{
			name:    "valid fresh to dirty",
			from:    StalenessStatusFresh,
			to:      StalenessStatusDirty,
			wantErr: false,
		},
		{
			name:    "valid fresh to needs_revalidation",
			from:    StalenessStatusFresh,
			to:      StalenessStatusNeedsRevalidation,
			wantErr: false,
		},
		{
			name:    "invalid from status",
			from:    StalenessStatus("invalid"),
			to:      StalenessStatusFresh,
			wantErr: true,
		},
		{
			name:    "invalid to status",
			from:    StalenessStatusFresh,
			to:      StalenessStatus("invalid"),
			wantErr: true,
		},
		{
			name:    "forbidden transition fresh to stale",
			from:    StalenessStatusFresh,
			to:      StalenessStatusStale,
			wantErr: true,
		},
		{
			name:    "forbidden transition contradicted to any",
			from:    StalenessStatusContradicted,
			to:      StalenessStatusFresh,
			wantErr: true,
		},
		{
			name:    "forbidden transition superseded to any",
			from:    StalenessStatusSuperseded,
			to:      StalenessStatusFresh,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStalenessTransition(tt.from, tt.to)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateStalenessTransition() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateStalenessTransition_ErrorsAreStalenessTransitionErrors(t *testing.T) {
	t.Parallel()
	// Invalid transition should return StalenessTransitionError
	err := ValidateStalenessTransition(StalenessStatusFresh, StalenessStatusStale)
	// Verify it's a transition error
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}
	// Check error message format
	expectedMsg := "staleness: invalid transition fresh -> stale"
	if err.Error() != expectedMsg {
		t.Errorf("Error() = %q, want %q", err.Error(), expectedMsg)
	}
}

func TestApplyStalenessTransition(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	marker := StalenessMarker{
		ID:          "marker-1",
		WorkspaceID: "ws-1",
		TargetRef:   EvidenceRef{Type: RefTypePath, Ref: "main.go:42"},
		Status:      StalenessStatusFresh,
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}

	// Apply valid transition
	updated, err := ApplyStalenessTransition(marker, StalenessStatusDirty, "", now)
	if err != nil {
		t.Errorf("ApplyStalenessTransition() failed: %v", err)
	}
	if updated.Status != StalenessStatusDirty {
		t.Errorf("Status = %v, want %v", updated.Status, StalenessStatusDirty)
	}
	if updated.UpdatedAt != now {
		t.Errorf("UpdatedAt = %v, want %v", updated.UpdatedAt, now)
	}
}

func TestApplyStalenessTransition_SameStatus(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	marker := StalenessMarker{
		ID:          "marker-1",
		WorkspaceID: "ws-1",
		TargetRef:   EvidenceRef{Type: RefTypePath, Ref: "main.go:42"},
		Status:      StalenessStatusFresh,
		ResolvedByEvent: "evt-1",
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}

	// Applying same status should be a no-op
	updated, err := ApplyStalenessTransition(marker, StalenessStatusFresh, "", now)
	if err != nil {
		t.Errorf("ApplyStalenessTransition() with same status failed: %v", err)
	}
	if !updated.UpdatedAt.Equal(marker.UpdatedAt) {
		t.Error("UpdatedAt should not change for same-status")
	}
}

func TestApplyStalenessTransition_InvalidTransition(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	marker := StalenessMarker{
		ID:          "marker-1",
		WorkspaceID: "ws-1",
		TargetRef:   EvidenceRef{Type: RefTypePath, Ref: "main.go:42"},
		Status:      StalenessStatusFresh,
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}

	// Invalid: fresh cannot go directly to stale
	_, err := ApplyStalenessTransition(marker, StalenessStatusStale, "", now)
	if err == nil {
		t.Error("ApplyStalenessTransition() should fail for invalid transition")
	}
}

func TestApplyStalenessTransition_WithResolvedByEvent(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	marker := StalenessMarker{
		ID:          "marker-1",
		WorkspaceID: "ws-1",
		TargetRef:   EvidenceRef{Type: RefTypePath, Ref: "main.go:42"},
		Status:      StalenessStatusNeedsRevalidation,
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}

	// Transition to superseded with resolved event
	updated, err := ApplyStalenessTransition(marker, StalenessStatusSuperseded, "evt-resolved", now)
	if err != nil {
		t.Errorf("ApplyStalenessTransition() failed: %v", err)
	}
	if updated.ResolvedByEvent != "evt-resolved" {
		t.Errorf("ResolvedByEvent = %v, want %v", updated.ResolvedByEvent, "evt-resolved")
	}
}

func TestApplyStalenessTransition_AllValidPaths(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)

	validPaths := []struct {
		from, to StalenessStatus
	}{
		{StalenessStatusFresh, StalenessStatusDirty},
		{StalenessStatusFresh, StalenessStatusNeedsRevalidation},
		{StalenessStatusFresh, StalenessStatusUnknown},
		{StalenessStatusDirty, StalenessStatusNeedsRevalidation},
		{StalenessStatusNeedsRevalidation, StalenessStatusFresh},
		{StalenessStatusNeedsRevalidation, StalenessStatusStale},
		{StalenessStatusNeedsRevalidation, StalenessStatusSuperseded},
		{StalenessStatusStale, StalenessStatusSuperseded},
		{StalenessStatusUnknown, StalenessStatusNeedsRevalidation},
	}

	for _, path := range validPaths {
		t.Run(string(path.from)+"_to_"+string(path.to), func(t *testing.T) {
			marker := StalenessMarker{
				ID:          "marker-test",
				WorkspaceID: "ws-test",
				TargetRef:   EvidenceRef{Type: RefTypePath, Ref: "main.go:42"},
				Status:      path.from,
				CreatedAt:   now.Add(-time.Hour),
				UpdatedAt:   now.Add(-time.Hour),
			}
			_, err := ApplyStalenessTransition(marker, path.to, "", now)
			if err != nil {
				t.Errorf("ApplyStalenessTransition() failed for %s -> %s: %v", path.from, path.to, err)
			}
		})
	}
}

func TestStalenessTransitionMatrix_Coverage(t *testing.T) {
	t.Parallel()
	// Ensure all statuses are covered in the transition matrix
	allStatuses := []StalenessStatus{
		StalenessStatusFresh,
		StalenessStatusDirty,
		StalenessStatusNeedsRevalidation,
		StalenessStatusStale,
		StalenessStatusContradicted,
		StalenessStatusSuperseded,
		StalenessStatusUnknown,
	}
	for _, from := range allStatuses {
		for _, to := range allStatuses {
			// This test just ensures no panic occurs and the function returns deterministically
			_ = CanTransitionStalenessStatus(from, to)
		}
	}
}


