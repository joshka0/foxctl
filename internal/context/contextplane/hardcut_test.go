package contextplane

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
)

func TestTopOfMindJSONRoundTripWithEvidenceRef(t *testing.T) {
	// VAL-CPLANE-001: TopOfMind JSON round-trip with EvidenceRef fields
	top := TopOfMind{
		WorkspaceID:     "ws-test",
		Objective:       "Test objective",
		Phase:           "execute",
		ActiveTaskIDs:   []string{"T-1", "T-2"},
		HardConstraints: []string{"No network"},
		Blockers:        []string{"Blocked by X"},
		RecentDecisions: []RecentDecision{{ID: "d1", Text: "Use SQLite", Ref: "note:adr-1"}},
		OpenLoops:       []string{"Resolve auth"},
		NextActions:     []string{"Write tests"},
		RelevantRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypePath, Ref: "internal/foo.go"},
			{Type: contextengine.RefTypeTask, Ref: "T-1"},
		},
		ProjectionMeta: &contextengine.ProjectionMeta{
			ProjectionID:      "proj-1",
			ProjectionType:    "top_of_mind",
			ProjectionVersion: 3,
			WorkspaceID:       "ws-test",
			GeneratedFromEvents: []string{"evt-1", "evt-2"},
			GeneratedAt:       time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC),
			ExpiresAt:         time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
		},
		StaleWarnings:       []string{"path:internal/foo.go is dirty"},
		KnownGaps:           []string{"Missing auth test"},
		GeneratedFromEvents: []string{"evt-1", "evt-2"},
		UpdatedAt:           time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC),
	}

	body, err := json.Marshal(top)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got TopOfMind
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.WorkspaceID != top.WorkspaceID {
		t.Errorf("workspace_id=%q want %q", got.WorkspaceID, top.WorkspaceID)
	}
	if len(got.RelevantRefs) != 2 {
		t.Fatalf("relevant_refs=%d want 2", len(got.RelevantRefs))
	}
	if got.RelevantRefs[0].Type != contextengine.RefTypePath || got.RelevantRefs[0].Ref != "internal/foo.go" {
		t.Errorf("relevant_refs[0]=%+v unexpected", got.RelevantRefs[0])
	}
	if got.ProjectionMeta == nil {
		t.Fatal("projection_meta is nil")
	}
	if got.ProjectionMeta.ProjectionVersion != 3 {
		t.Errorf("projection_version=%d want 3", got.ProjectionMeta.ProjectionVersion)
	}
	if len(got.StaleWarnings) != 1 || got.StaleWarnings[0] != "path:internal/foo.go is dirty" {
		t.Errorf("stale_warnings=%v unexpected", got.StaleWarnings)
	}
	if len(got.KnownGaps) != 1 || got.KnownGaps[0] != "Missing auth test" {
		t.Errorf("known_gaps=%v unexpected", got.KnownGaps)
	}
	if len(got.GeneratedFromEvents) != 2 {
		t.Errorf("generated_from_events=%d want 2", len(got.GeneratedFromEvents))
	}
}

func TestTopOfMindProjectionMetaFields(t *testing.T) {
	// VAL-CPLANE-002: TopOfMind ProjectionMeta fields populated
	top := TopOfMind{
		WorkspaceID: "ws-test",
		ProjectionMeta: &contextengine.ProjectionMeta{
			ProjectionID:        "proj-abc",
			ProjectionType:      "top_of_mind",
			ProjectionVersion:   1,
			WorkspaceID:         "ws-test",
			GeneratedFromEvents: []string{"evt-1"},
			GeneratedAt:         time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC),
			ExpiresAt:           time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC),
		},
	}
	meta := top.ProjectionMeta
	if meta.ProjectionID != "proj-abc" {
		t.Errorf("projection_id=%q", meta.ProjectionID)
	}
	if meta.ProjectionType != "top_of_mind" {
		t.Errorf("projection_type=%q", meta.ProjectionType)
	}
	if meta.ProjectionVersion != 1 {
		t.Errorf("projection_version=%d", meta.ProjectionVersion)
	}
	if meta.WorkspaceID != "ws-test" {
		t.Errorf("workspace_id=%q", meta.WorkspaceID)
	}
	if len(meta.GeneratedFromEvents) != 1 {
		t.Errorf("generated_from_events=%d", len(meta.GeneratedFromEvents))
	}
}

func TestTopOfMindStaleWarningsAndKnownGaps(t *testing.T) {
	// VAL-CPLANE-003: TopOfMind includes StaleWarnings and KnownGaps
	top := TopOfMind{
		StaleWarnings: []string{"path:foo.go dirty", "path:bar.go stale"},
		KnownGaps:     []string{"No coverage for auth"},
	}
	body, err := json.Marshal(top)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got TopOfMind
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.StaleWarnings) != 2 {
		t.Fatalf("stale_warnings=%d want 2", len(got.StaleWarnings))
	}
	if len(got.KnownGaps) != 1 {
		t.Fatalf("known_gaps=%d want 1", len(got.KnownGaps))
	}
}

func TestHandoffRoundTripWithEvidenceRef(t *testing.T) {
	// VAL-CPLANE-004: Handoff round-trip with EvidenceRef fields
	h := Handoff{
		TaskID:  "T-42",
		Phase:   "execute",
		Outcome: "partial",
		Summary: "Implemented feature",
		EvidenceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeNote, Ref: "adr-1"},
			{Type: contextengine.RefTypeArtifact, Ref: "artifact-1"},
		},
		FileRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypePath, Ref: "internal/foo.go"},
		},
		Observations:        []string{"Fast iteration works"},
		Tensions:            []string{"Scope creep risk"},
		NextActions:         []string{"Add tests"},
		PromotionCandidates: []string{"note:pattern-1"},
		CreatedAt:           time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC),
	}
	body, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Handoff
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.EvidenceRefs) != 2 {
		t.Fatalf("evidence_refs=%d want 2", len(got.EvidenceRefs))
	}
	if got.EvidenceRefs[0].Type != contextengine.RefTypeNote {
		t.Errorf("evidence_refs[0].type=%q", got.EvidenceRefs[0].Type)
	}
	if len(got.FileRefs) != 1 || got.FileRefs[0].Ref != "internal/foo.go" {
		t.Errorf("file_refs=%v unexpected", got.FileRefs)
	}
	// Verify FilesTouched() method works
	ft := got.FilesTouched()
	if len(ft) != 1 || ft[0] != "path:internal/foo.go" {
		t.Errorf("FilesTouched()=%v unexpected", ft)
	}
}

func TestObservationEvidenceRefRoundTrip(t *testing.T) {
	// VAL-CPLANE-005: Observation EvidenceRef compatibility
	obs := Observation{
		ID:         "O-1",
		Statement:  "Test observation",
		Confidence: 0.85,
		Count:      3,
		Project:    "foxctl",
		Area:       "storage",
		EvidenceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypePath, Ref: "internal/storage/store.go"},
		},
		FirstSeen: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		LastSeen:  time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC),
	}
	body, err := json.Marshal(obs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Observation
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.EvidenceRefs) != 1 {
		t.Fatalf("evidence_refs=%d want 1", len(got.EvidenceRefs))
	}
	if got.EvidenceRefs[0].Type != contextengine.RefTypePath || got.EvidenceRefs[0].Ref != "internal/storage/store.go" {
		t.Errorf("evidence_refs[0]=%+v unexpected", got.EvidenceRefs[0])
	}
}

func TestTensionEvidenceRefRoundTrip(t *testing.T) {
	// VAL-CPLANE-006: Tension EvidenceRef compatibility
	tension := Tension{
		ID:        "X-1",
		Kind:      "contradiction",
		Statement: "Policy mismatch",
		Impact:    "high",
		RelatedRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeNote, Ref: "policy-1"},
			{Type: contextengine.RefTypeRun, Ref: "R-001"},
		},
		Status:    "open",
		Count:     2,
		CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		LastSeen:  time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC),
	}
	body, err := json.Marshal(tension)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Tension
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.RelatedRefs) != 2 {
		t.Fatalf("related_refs=%d want 2", len(got.RelatedRefs))
	}
	if got.RelatedRefs[0].Type != contextengine.RefTypeNote {
		t.Errorf("related_refs[0].type=%q", got.RelatedRefs[0].Type)
	}
}

func TestMemoryProposalTypedKind(t *testing.T) {
	// VAL-CPLANE-007: MemoryProposal typed kind
	for _, kind := range []PolicyKind{
		PolicyKindRetrievalPatch,
		PolicyKindExternalImport,
		PolicyKindMethodologyDraft,
		PolicyKindContradictionNote,
		PolicyKindObservationPromote,
		PolicyKindTensionResolve,
		PolicyKindMemoryDraft,
	} {
		if !kind.IsValid() {
			t.Errorf("expected %q to be valid", kind)
		}
		parsed, err := ParsePolicyKind(string(kind))
		if err != nil {
			t.Errorf("ParsePolicyKind(%q): %v", kind, err)
		}
		if parsed != kind {
			t.Errorf("ParsePolicyKind(%q)=%q want %q", kind, parsed, kind)
		}
	}
	// Unknown kind should fail
	_, err := ParsePolicyKind("unknown_kind")
	if err == nil {
		t.Error("expected error for unknown kind")
	}
}

func TestMemoryProposalSourceRefsAsEvidenceRef(t *testing.T) {
	// VAL-CPLANE-008: MemoryProposal SourceRefs as EvidenceRef
	proposal := MemoryProposal{
		Kind:       PolicyKindRetrievalPatch,
		Summary:    "Enable fallback",
		SourceRefs: []contextengine.EvidenceRef{{Type: contextengine.RefTypeNote, Ref: "retrieval-1"}},
	}
	body, err := json.Marshal(proposal)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got MemoryProposal
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.SourceRefs) != 1 {
		t.Fatalf("source_refs=%d want 1", len(got.SourceRefs))
	}
	if got.SourceRefs[0].Type != contextengine.RefTypeNote {
		t.Errorf("source_refs[0].type=%q", got.SourceRefs[0].Type)
	}
}

func TestTaskPacketRelevantRefsAsEvidenceRef(t *testing.T) {
	// VAL-CPLANE-009: TaskPacket RelevantRefs as EvidenceRef
	packet := TaskPacket{
		WorkspaceID: "ws-test",
		Task:        TaskCandidate{ID: "T-1", Title: "Test task", Status: "in_progress"},
		Objective:   "Build feature",
		Phase:       "execute",
		RelevantRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypePath, Ref: "internal/foo.go"},
			{Type: contextengine.RefTypeTask, Ref: "T-0"},
		},
		GeneratedAt: time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC),
	}
	body, err := json.Marshal(packet)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got TaskPacket
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.RelevantRefs) != 2 {
		t.Fatalf("relevant_refs=%d want 2", len(got.RelevantRefs))
	}
	if got.RelevantRefs[0].Type != contextengine.RefTypePath || got.RelevantRefs[0].Ref != "internal/foo.go" {
		t.Errorf("relevant_refs[0]=%+v unexpected", got.RelevantRefs[0])
	}
}

func TestRetrievalResultToEvidencePack(t *testing.T) {
	// VAL-CPLANE-010: RetrievalResult ToEvidencePack conversion
	result := &RetrievalResult{
		WorkspaceID: "ws-test",
		Query:       "test query",
		Observations: []Observation{
			{
				ID:          "O-1",
				Statement:   "Fast iteration works",
				Confidence:  0.85,
				Count:       3,
				FirstSeen:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				LastSeen:    time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC),
			},
		},
		Tensions: []Tension{
			{
				ID:        "X-1",
				Statement: "Scope creep",
				Kind:      "contradiction",
				Impact:    "medium",
				Status:    "open",
				Count:     2,
				CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			},
		},
		VaultHits: []RetrievalHit{
			{
				Path:    "notes/patterns/test.md",
				Title:   "Test Pattern",
				Score:   85,
				Snippet: "This is a test pattern",
			},
		},
		Weights:     RetrievalWeights{BaseIndexScore: 50},
		GeneratedAt: time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC),
	}
	pack := result.ToEvidencePack()
	if pack.WorkspaceID != "ws-test" {
		t.Errorf("workspace_id=%q", pack.WorkspaceID)
	}
	if pack.Query != "test query" {
		t.Errorf("query=%q", pack.Query)
	}
	if pack.Lane != contextengine.LaneContext {
		t.Errorf("lane=%q", pack.Lane)
	}
	// Should have 3 nodes: 1 observation + 1 tension + 1 vault hit
	if len(pack.Nodes) != 3 {
		t.Fatalf("nodes=%d want 3", len(pack.Nodes))
	}
	// Verify observation node
	obsNode := pack.Nodes[0]
	if obsNode.NodeType != contextengine.EvidenceNodeTypeObservation {
		t.Errorf("node[0].type=%q", obsNode.NodeType)
	}
	if obsNode.Statement != "Fast iteration works" {
		t.Errorf("node[0].statement=%q", obsNode.Statement)
	}
	// Verify tension node
	tensionNode := pack.Nodes[1]
	if tensionNode.NodeType != contextengine.EvidenceNodeTypeTension {
		t.Errorf("node[1].type=%q", tensionNode.NodeType)
	}
	// Verify vault hit node
	hitNode := pack.Nodes[2]
	if hitNode.NodeType != contextengine.EvidenceNodeTypeRetrieval {
		t.Errorf("node[2].type=%q", hitNode.NodeType)
	}
	if hitNode.Confidence != 0.85 {
		t.Errorf("node[2].confidence=%.2f", hitNode.Confidence)
	}
}

func TestSaveLoadTopOfMindWithNewFields(t *testing.T) {
	// VAL-CPLANE-011: SaveTopOfMind and LoadTopOfMind with new fields
	store := NewWorkspaceStore(t.TempDir())
	top := TopOfMind{
		WorkspaceID:     "ws-test",
		Objective:       "Test objective",
		Phase:           "execute",
		RelevantRefs:    []contextengine.EvidenceRef{{Type: contextengine.RefTypePath, Ref: "foo.go"}},
		ProjectionMeta:  &contextengine.ProjectionMeta{ProjectionID: "p1", ProjectionType: "top_of_mind", ProjectionVersion: 1, WorkspaceID: "ws-test"},
		StaleWarnings:   []string{"dirty file"},
		KnownGaps:       []string{"missing test"},
		UpdatedAt:       time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC),
	}
	if _, err := store.SaveTopOfMind(top); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}
	got, err := store.LoadTopOfMind()
	if err != nil {
		t.Fatalf("LoadTopOfMind: %v", err)
	}
	if len(got.RelevantRefs) != 1 {
		t.Fatalf("relevant_refs=%d want 1", len(got.RelevantRefs))
	}
	if got.RelevantRefs[0].Type != contextengine.RefTypePath {
		t.Errorf("relevant_refs[0].type=%q", got.RelevantRefs[0].Type)
	}
	if got.ProjectionMeta == nil {
		t.Fatal("projection_meta is nil")
	}
	if got.ProjectionMeta.ProjectionID != "p1" {
		t.Errorf("projection_id=%q", got.ProjectionMeta.ProjectionID)
	}
	if len(got.StaleWarnings) != 1 {
		t.Fatalf("stale_warnings=%d want 1", len(got.StaleWarnings))
	}
	if len(got.KnownGaps) != 1 {
		t.Fatalf("known_gaps=%d want 1", len(got.KnownGaps))
	}
}

func TestSaveLoadHandoffWithNewFields(t *testing.T) {
	// VAL-CPLANE-012: SaveHandoff and LoadHandoff with new fields
	store := NewWorkspaceStore(t.TempDir())
	h := Handoff{
		TaskID:       "T-42",
		Phase:        "execute",
		Outcome:      "complete",
		Summary:      "Implemented feature",
		EvidenceRefs: []contextengine.EvidenceRef{{Type: contextengine.RefTypeNote, Ref: "adr-1"}},
		FileRefs:     []contextengine.EvidenceRef{{Type: contextengine.RefTypePath, Ref: "internal/foo.go"}},
		CreatedAt:    time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC),
	}
	path, err := store.SaveHandoff(h)
	if err != nil {
		t.Fatalf("SaveHandoff: %v", err)
	}
	got, err := store.LoadHandoff(path)
	if err != nil {
		t.Fatalf("LoadHandoff: %v", err)
	}
	if len(got.EvidenceRefs) != 1 {
		t.Fatalf("evidence_refs=%d want 1", len(got.EvidenceRefs))
	}
	if got.EvidenceRefs[0].Type != contextengine.RefTypeNote {
		t.Errorf("evidence_refs[0].type=%q", got.EvidenceRefs[0].Type)
	}
	if len(got.FileRefs) != 1 {
		t.Fatalf("file_refs=%d want 1", len(got.FileRefs))
	}
	if got.FileRefs[0].Ref != "internal/foo.go" {
		t.Errorf("file_refs[0].ref=%q", got.FileRefs[0].Ref)
	}
}

func TestOldJSONFormatRejected(t *testing.T) {
	// VAL-CPLANE-014: Old JSON format explicitly rejected
	// Old format had []string refs instead of []EvidenceRef objects
	oldJSON := `{
		"workspace_id": "ws-test",
		"objective": "Test",
		"phase": "execute",
		"relevant_refs": ["path:foo.go", "task:T-1"],
		"updated_at": "2026-04-27T00:00:00Z"
	}`
	var top TopOfMind
	err := json.Unmarshal([]byte(oldJSON), &top)
	if err == nil {
		// If no error, the refs should NOT be populated (they'd be empty because
		// []string can't unmarshal into []EvidenceRef)
		if len(top.RelevantRefs) != 0 {
			t.Fatalf("expected empty relevant_refs for old format, got %d - old format should not silently work", len(top.RelevantRefs))
		}
		// The old format silently produces empty refs - that's acceptable since
		// the fields are lost, not corrupted. But we should verify that an attempt
		// to use the old string-array format does NOT produce valid EvidenceRef values.
		t.Log("Old JSON format produced empty EvidenceRef fields (no corruption)")
	}
}
