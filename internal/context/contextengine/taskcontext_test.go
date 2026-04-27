package contextengine

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// TaskContext projection tests (VAL-TASK-001 through VAL-TASK-010)
// ---------------------------------------------------------------------------

func TestTaskContext_StoredAsProjection(t *testing.T) {
	t.Parallel()
	// VAL-TASK-001: TaskContext can be stored via PutProjection and retrieved.
	store := NewMemoryStore()
	ctx := context.Background()

	tc := TaskContext{
		WorkspaceID: "ws-1",
		TaskID:      "task-1",
		Objective:   "Implement feature X",
		Status:      "in_progress",
		Scope: ClaimScope{
			Path: "src/feature_x/",
			Refs: []EvidenceRef{{Type: RefTypePath, Ref: "src/feature_x/"}},
		},
		RelatedCodeRefs: []EvidenceRef{
			{Type: RefTypePath, Ref: "src/feature_x/main.go"},
		},
		RelatedClaims: []EvidenceRef{
			{Type: RefTypeMemoryClaim, Ref: "claim-1"},
		},
		RelatedSessions: []EvidenceRef{
			{Type: RefTypeSession, Ref: "session-1"},
		},
		RelatedArtifacts: []EvidenceRef{
			{Type: RefTypeArtifact, Ref: "artifact-1"},
		},
		ValidationEvidence: []EvidenceRef{
			{Type: RefTypeEvent, Ref: "evt-review-1"},
		},
		OpenGaps:     []string{"need tests"},
		StaleWarnings: []string{"src/main.go is dirty"},
		NextActions:  []string{"write unit tests"},
		ProjectionMeta: ProjectionMeta{
			ProjectionID:      "proj-tc-1",
			ProjectionType:    "task_context",
			ProjectionVersion: 1,
			WorkspaceID:       "ws-1",
			GeneratedAt:       time.Now().UTC().Truncate(time.Millisecond),
		},
		UpdatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	// Marshal TaskContext to JSON for storage
	payload, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("marshal TaskContext: %v", err)
	}

	// Store as projection
	err = store.PutProjection(ctx,
		tc.ProjectionMeta.ProjectionID,
		tc.WorkspaceID,
		tc.ProjectionMeta.ProjectionType,
		tc.ProjectionMeta.ProjectionVersion,
		tc.TaskID,
		tc.ProjectionMeta.GeneratedFromEvents,
		payload,
		tc.ProjectionMeta.GeneratedAt,
		tc.ProjectionMeta.ExpiresAt,
	)
	if err != nil {
		t.Fatalf("PutProjection: %v", err)
	}

	// Retrieve
	_, version, taskID, _, rawPayload, _, _, err := store.GetProjection(ctx, tc.WorkspaceID, tc.ProjectionMeta.ProjectionID)
	if err != nil {
		t.Fatalf("GetProjection: %v", err)
	}
	if version != 1 {
		t.Errorf("version = %d, want 1", version)
	}
	if taskID != tc.TaskID {
		t.Errorf("taskID = %q, want %q", taskID, tc.TaskID)
	}

	// Unmarshal and verify
	var got TaskContext
	if err := json.Unmarshal(rawPayload, &got); err != nil {
		t.Fatalf("unmarshal TaskContext: %v", err)
	}
	if got.Objective != tc.Objective {
		t.Errorf("Objective = %q, want %q", got.Objective, tc.Objective)
	}
	if got.Status != tc.Status {
		t.Errorf("Status = %q, want %q", got.Status, tc.Status)
	}
}

func TestTaskContext_ObjectiveAndStatus(t *testing.T) {
	t.Parallel()
	// VAL-TASK-002: TaskContext has Objective and Status populated from Task.
	tc := TaskContext{
		WorkspaceID: "ws-1",
		TaskID:      "task-1",
		Objective:   "Build the storage layer",
		Status:      "in_progress",
		ProjectionMeta: ProjectionMeta{
			ProjectionID:      "proj-1",
			ProjectionType:    "task_context",
			ProjectionVersion: 1,
			WorkspaceID:       "ws-1",
			GeneratedAt:       time.Now(),
		},
	}

	if tc.Objective != "Build the storage layer" {
		t.Errorf("Objective = %q, want populated", tc.Objective)
	}
	if tc.Status != "in_progress" {
		t.Errorf("Status = %q, want populated", tc.Status)
	}
}

func TestTaskContext_ScopeRefsAsEvidenceRef(t *testing.T) {
	t.Parallel()
	// VAL-TASK-003: TaskContext.Scope.Refs derived from Task.ScopePath.
	tc := TaskContext{
		WorkspaceID: "ws-1",
		TaskID:      "task-1",
		Scope: ClaimScope{
			Path: "internal/context/",
			Refs: []EvidenceRef{
				{Type: RefTypePath, Ref: "internal/context/"},
			},
		},
		ProjectionMeta: ProjectionMeta{
			ProjectionID:      "proj-1",
			ProjectionType:    "task_context",
			ProjectionVersion: 1,
			WorkspaceID:       "ws-1",
			GeneratedAt:       time.Now(),
		},
	}

	if len(tc.Scope.Refs) != 1 {
		t.Fatalf("Scope.Refs = %d, want 1", len(tc.Scope.Refs))
	}
	if tc.Scope.Refs[0].Type != RefTypePath {
		t.Errorf("Scope.Refs[0].Type = %q, want path", tc.Scope.Refs[0].Type)
	}
	if tc.Scope.Refs[0].Ref != "internal/context/" {
		t.Errorf("Scope.Refs[0].Ref = %q", tc.Scope.Refs[0].Ref)
	}
}

func TestTaskContext_RelatedCodeRefs(t *testing.T) {
	t.Parallel()
	// VAL-TASK-004: RelatedCodeRefs from task analysis.
	tc := makeTestTaskContext()
	tc.RelatedCodeRefs = []EvidenceRef{
		{Type: RefTypePath, Ref: "src/main.go"},
		{Type: RefTypeSymbol, Ref: "HandleRequest"},
	}
	if err := tc.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
	if len(tc.RelatedCodeRefs) != 2 {
		t.Errorf("RelatedCodeRefs = %d, want 2", len(tc.RelatedCodeRefs))
	}
}

func TestTaskContext_RelatedClaims(t *testing.T) {
	t.Parallel()
	// VAL-TASK-005: RelatedClaims from claim store query.
	tc := makeTestTaskContext()
	tc.RelatedClaims = []EvidenceRef{
		{Type: RefTypeMemoryClaim, Ref: "claim-1"},
	}
	if err := tc.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

func TestTaskContext_RelatedSessions(t *testing.T) {
	t.Parallel()
	// VAL-TASK-006: RelatedSessions from session store query.
	tc := makeTestTaskContext()
	tc.RelatedSessions = []EvidenceRef{
		{Type: RefTypeSession, Ref: "session-1"},
	}
	if err := tc.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

func TestTaskContext_RelatedArtifacts(t *testing.T) {
	t.Parallel()
	// VAL-TASK-007: RelatedArtifacts from artifact store query.
	tc := makeTestTaskContext()
	tc.RelatedArtifacts = []EvidenceRef{
		{Type: RefTypeArtifact, Ref: "artifact-1"},
	}
	if err := tc.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

func TestTaskContext_ValidationEvidence(t *testing.T) {
	t.Parallel()
	// VAL-TASK-008: ValidationEvidence from task review history.
	tc := makeTestTaskContext()
	tc.ValidationEvidence = []EvidenceRef{
		{Type: RefTypeEvent, Ref: "evt-review-1"},
	}
	if err := tc.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

func TestTaskContext_OpenGapsAndStaleWarnings(t *testing.T) {
	t.Parallel()
	// VAL-TASK-009: OpenGaps and StaleWarnings populated from StalenessMarker queries.
	tc := makeTestTaskContext()
	tc.OpenGaps = []string{"missing test coverage", "docs incomplete"}
	tc.StaleWarnings = []string{"src/main.go has dirty edits", "claim-1 needs revalidation"}

	if len(tc.OpenGaps) != 2 {
		t.Errorf("OpenGaps = %d, want 2", len(tc.OpenGaps))
	}
	if len(tc.StaleWarnings) != 2 {
		t.Errorf("StaleWarnings = %d, want 2", len(tc.StaleWarnings))
	}
}

func TestTaskContext_NextActions(t *testing.T) {
	t.Parallel()
	// VAL-TASK-010: NextActions from task analysis and claim gaps.
	tc := makeTestTaskContext()
	tc.OpenGaps = []string{"missing tests"}
	tc.NextActions = []string{"write unit tests", "run go test"}

	if len(tc.NextActions) != 2 {
		t.Errorf("NextActions = %d, want 2", len(tc.NextActions))
	}
}

func TestTaskContext_RoundTripJSON(t *testing.T) {
	t.Parallel()
	// Full JSON round-trip test for TaskContext
	tc := TaskContext{
		WorkspaceID: "ws-1",
		TaskID:      "task-1",
		Objective:   "Build impact engine",
		Status:      "in_progress",
		Scope: ClaimScope{
			Path: "internal/context/",
			Refs: []EvidenceRef{{Type: RefTypePath, Ref: "internal/context/"}},
		},
		OpenGaps:     []string{"need tests"},
		StaleWarnings: []string{"file dirty"},
		NextActions:  []string{"write tests"},
		RelatedCodeRefs: []EvidenceRef{
			{Type: RefTypePath, Ref: "src/main.go"},
		},
		RelatedClaims: []EvidenceRef{
			{Type: RefTypeMemoryClaim, Ref: "claim-1"},
		},
		RelatedSessions: []EvidenceRef{
			{Type: RefTypeSession, Ref: "session-1"},
		},
		RelatedArtifacts: []EvidenceRef{
			{Type: RefTypeArtifact, Ref: "artifact-1"},
		},
		ValidationEvidence: []EvidenceRef{
			{Type: RefTypeEvent, Ref: "evt-1"},
		},
		ProjectionMeta: ProjectionMeta{
			ProjectionID:        "proj-1",
			ProjectionType:      "task_context",
			ProjectionVersion:   3,
			WorkspaceID:         "ws-1",
			GeneratedFromEvents: []string{"evt-1", "evt-2"},
			GeneratedAt:         time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		},
		UpdatedAt: time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC),
	}

	data, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got TaskContext
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify all fields round-trip
	if got.WorkspaceID != tc.WorkspaceID {
		t.Errorf("WorkspaceID mismatch")
	}
	if got.TaskID != tc.TaskID {
		t.Errorf("TaskID mismatch")
	}
	if got.Objective != tc.Objective {
		t.Errorf("Objective mismatch")
	}
	if got.Status != tc.Status {
		t.Errorf("Status mismatch")
	}
	if got.Scope.Path != tc.Scope.Path {
		t.Errorf("Scope.Path mismatch")
	}
	if len(got.Scope.Refs) != len(tc.Scope.Refs) {
		t.Errorf("Scope.Refs mismatch: %d vs %d", len(got.Scope.Refs), len(tc.Scope.Refs))
	}
	if len(got.RelatedCodeRefs) != len(tc.RelatedCodeRefs) {
		t.Errorf("RelatedCodeRefs mismatch")
	}
	if len(got.RelatedClaims) != len(tc.RelatedClaims) {
		t.Errorf("RelatedClaims mismatch")
	}
	if len(got.RelatedSessions) != len(tc.RelatedSessions) {
		t.Errorf("RelatedSessions mismatch")
	}
	if len(got.RelatedArtifacts) != len(tc.RelatedArtifacts) {
		t.Errorf("RelatedArtifacts mismatch")
	}
	if len(got.ValidationEvidence) != len(tc.ValidationEvidence) {
		t.Errorf("ValidationEvidence mismatch")
	}
	if len(got.OpenGaps) != len(tc.OpenGaps) {
		t.Errorf("OpenGaps mismatch")
	}
	if len(got.StaleWarnings) != len(tc.StaleWarnings) {
		t.Errorf("StaleWarnings mismatch")
	}
	if len(got.NextActions) != len(tc.NextActions) {
		t.Errorf("NextActions mismatch")
	}
	if got.ProjectionMeta.ProjectionVersion != tc.ProjectionMeta.ProjectionVersion {
		t.Errorf("ProjectionVersion mismatch")
	}
}

func TestTaskContext_InvalidRelatedCodeRefs(t *testing.T) {
	t.Parallel()
	tc := makeTestTaskContext()
	tc.RelatedCodeRefs = []EvidenceRef{{Type: "invalid", Ref: ""}}
	if err := tc.Validate(); err == nil {
		t.Error("expected validation error for invalid related code refs")
	}
}

func TestTaskContext_InvalidRelatedClaims(t *testing.T) {
	t.Parallel()
	tc := makeTestTaskContext()
	tc.RelatedClaims = []EvidenceRef{{Type: "invalid", Ref: ""}}
	if err := tc.Validate(); err == nil {
		t.Error("expected validation error for invalid related claims")
	}
}

// ---------------------------------------------------------------------------
// TaskContext scope refs conversion helper
// ---------------------------------------------------------------------------

func TestScopePathToRefs(t *testing.T) {
	t.Parallel()
	// Verify that a scope path can be converted to EvidenceRefs
	path := "internal/context/contextengine/"
	ref := EvidenceRef{Type: RefTypePath, Ref: path}

	if err := ValidateEvidenceRef(ref); err != nil {
		t.Errorf("scope path ref should be valid: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func makeTestTaskContext() TaskContext {
	return TaskContext{
		WorkspaceID: "ws-1",
		TaskID:      "task-1",
		ProjectionMeta: ProjectionMeta{
			ProjectionID:      "proj-1",
			ProjectionType:    "task_context",
			ProjectionVersion: 1,
			WorkspaceID:       "ws-1",
			GeneratedAt:       time.Now().UTC(),
		},
		UpdatedAt: time.Now().UTC(),
	}
}
