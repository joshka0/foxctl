package memorycore

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/storage"
)

func TestParseKindsRejectsInvalidKind(t *testing.T) {
	if _, err := ParseKinds("semantic_fact,not_a_kind"); err == nil {
		t.Fatalf("ParseKinds accepted a non-canonical kind")
	}
}

func TestParseLifecycleStatesRejectsInvalidState(t *testing.T) {
	if _, err := ParseLifecycleStates("active,unknown"); err == nil {
		t.Fatalf("ParseLifecycleStates accepted a non-canonical lifecycle state")
	}
}

func TestParseTelemetryActionRejectsInvalidAction(t *testing.T) {
	if _, err := ParseTelemetryAction("queried"); err == nil {
		t.Fatalf("ParseTelemetryAction accepted a non-canonical telemetry action")
	}
}

func TestRecordFromContextClaimLabelsEvidenceOnly(t *testing.T) {
	createdAt := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	record := RecordFromContextClaim(contextengine.MemoryClaim{
		ID:          "claim-1",
		WorkspaceID: "/repo",
		ClaimType:   "decision",
		Status:      contextengine.ClaimStatusCurrent,
		Scope: contextengine.ClaimScope{
			SessionID: "session-1",
			Path:      "internal/context/memorycore/record.go",
			Refs: []contextengine.EvidenceRef{
				{Type: contextengine.RefTypePath, Ref: "internal/context/contextengine/claims.go"},
			},
		},
		Summary:    "Context claims should project into canonical memory records.",
		Confidence: 0.72,
		SourceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeMemoryClaim, Ref: "claim-parent"},
			{Type: contextengine.RefTypePath, Ref: "internal/storage/contextengine/store.go"},
		},
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, ContextClaimOptions{Score: 0.9})

	if record.Kind != KindDecision {
		t.Fatalf("kind=%q want %q", record.Kind, KindDecision)
	}
	if record.SourceLane != SourceLaneContextClaim {
		t.Fatalf("source_lane=%q want %q", record.SourceLane, SourceLaneContextClaim)
	}
	if record.Lifecycle.State != LifecycleStateActive {
		t.Fatalf("lifecycle=%q want %q", record.Lifecycle.State, LifecycleStateActive)
	}
	if record.Usage.InstructionEligible {
		t.Fatalf("context claim should not be instruction eligible")
	}
	if !record.Usage.EvidenceOnly {
		t.Fatalf("context claim should be evidence-only")
	}
	if record.Provenance.SessionID != "session-1" {
		t.Fatalf("session_id=%q want session-1", record.Provenance.SessionID)
	}
	if len(record.Provenance.ParentMemoryIDs) != 1 || record.Provenance.ParentMemoryIDs[0] != "claim-parent" {
		t.Fatalf("parent ids=%v want [claim-parent]", record.Provenance.ParentMemoryIDs)
	}
	if len(record.Links.FileRefs) != 3 {
		t.Fatalf("file refs=%v want three refs", record.Links.FileRefs)
	}
}

func TestRecordFromContextClaimMarksRejectedAsQuarantined(t *testing.T) {
	record := RecordFromContextClaim(contextengine.MemoryClaim{
		ID:        "claim-rejected",
		ClaimType: "semantic_fact",
		Status:    contextengine.ClaimStatusRejected,
		Summary:   "This claim contradicted trusted state.",
		Reason:    "contradicted trusted repo state",
	}, ContextClaimOptions{})

	if record.Lifecycle.State != LifecycleStateQuarantined {
		t.Fatalf("lifecycle=%q want %q", record.Lifecycle.State, LifecycleStateQuarantined)
	}
	if !record.Trust.Tainted {
		t.Fatalf("rejected claim should be tainted")
	}
	if len(record.Trust.TaintReasons) != 1 {
		t.Fatalf("taint reasons=%v want one reason", record.Trust.TaintReasons)
	}
}

func TestRecordFromNamedEntryLabelsEvidenceOnly(t *testing.T) {
	createdAt := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	raw, err := json.Marshal(map[string]any{"note": "watch storage driver choice"})
	if err != nil {
		t.Fatal(err)
	}
	record := RecordFromNamedEntry(storage.NamedEntry{
		ID:              "mem-1",
		Name:            "storage-driver-decision",
		Type:            "decision",
		Summary:         "Use the canonical Turso storage path for SQLite-family tests.",
		Result:          raw,
		CreatedAt:       createdAt,
		LastAccess:      createdAt,
		AccessCount:     2,
		SelectedCount:   1,
		UseCount:        3,
		SuccessCount:    2,
		FailureCount:    1,
		PatchCount:      1,
		RestoreCount:    1,
		SessionID:       "session-1",
		LifecycleState:  "stale",
		ReviewStatus:    "needs_review",
		SupersededBy:    "mem-2",
		ReviewNotes:     "old runner claim needs review",
		LastValidatedAt: createdAt.Add(-time.Hour),
		LastSelectedAt:  createdAt.Add(time.Minute),
		LastUsedAt:      createdAt.Add(2 * time.Minute),
		LastSucceededAt: createdAt.Add(3 * time.Minute),
		LastFailedAt:    createdAt.Add(4 * time.Minute),
		LastPatchedAt:   createdAt.Add(5 * time.Minute),
	}, NamedEntryOptions{
		Score:          0.8,
		FileRefs:       []string{"internal/storage/memory/store.go"},
		IncludeContent: true,
	})

	if record.Kind != KindDecision {
		t.Fatalf("kind=%q want %q", record.Kind, KindDecision)
	}
	if record.SourceLane != SourceLaneNamedMemory {
		t.Fatalf("source_lane=%q want %q", record.SourceLane, SourceLaneNamedMemory)
	}
	if record.Usage.InstructionEligible {
		t.Fatalf("named memory record should not be instruction eligible")
	}
	if !record.Usage.EvidenceOnly {
		t.Fatalf("named memory record should be evidence-only")
	}
	if record.Provenance.SessionID != "session-1" {
		t.Fatalf("session_id=%q want session-1", record.Provenance.SessionID)
	}
	if record.Telemetry.ViewCount != 2 {
		t.Fatalf("view_count=%d want 2", record.Telemetry.ViewCount)
	}
	if record.Telemetry.SelectedCount != 1 || record.Telemetry.UseCount != 3 || record.Telemetry.SuccessCount != 2 || record.Telemetry.FailureCount != 1 || record.Telemetry.PatchCount != 1 || record.Telemetry.RestoreCount != 1 {
		t.Fatalf("telemetry counters not projected: %#v", record.Telemetry)
	}
	if record.Telemetry.LastSelectedAt == "" || record.Telemetry.LastUsedAt == "" || record.Telemetry.LastSucceededAt == "" || record.Telemetry.LastFailedAt == "" {
		t.Fatalf("telemetry timestamps not projected: %#v", record.Telemetry)
	}
	if record.Lifecycle.State != LifecycleStateStale || record.Lifecycle.ReviewStatus != ReviewStatusNeedsReview {
		t.Fatalf("lifecycle=%#v", record.Lifecycle)
	}
	if record.Lifecycle.SupersededBy != "mem-2" || record.Lifecycle.ReviewNotes == "" {
		t.Fatalf("lifecycle metadata not projected: %#v", record.Lifecycle)
	}
	if record.Temporal.LastValidatedAt == "" {
		t.Fatalf("last_validated_at should be projected")
	}
	if record.Temporal.LastPatchedAt == "" {
		t.Fatalf("last_patched_at should be projected")
	}
	if record.Content == "" {
		t.Fatalf("content should be included")
	}
}
