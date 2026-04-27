package adapters

import (
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/contextplane"
)

func TestConvertTopOfMind(t *testing.T) {
	now := time.Now().UTC()
	src := contextplane.TopOfMind{
		WorkspaceID:     "ws1",
		Objective:       "Ship adapters",
		Phase:           "implementation",
		ActiveTaskIDs:   []string{"t1", "t2"},
		HardConstraints: []string{"no breaking changes"},
		Blockers:        []string{"missing types"},
		RecentDecisions: []contextplane.RecentDecision{
			{ID: "d1", Text: "Use TDD", Ref: "path:design.md"},
		},
		OpenLoops:    []string{"verify coverage"},
		NextActions:  []string{"write tests"},
		RelevantRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypePath, Ref: "types.go"},
			{Type: contextengine.RefTypeTask, Ref: "abc"},
		},
		UpdatedAt:    now,
	}

	got := ConvertTopOfMind(src)

	if got.WorkspaceID != src.WorkspaceID {
		t.Errorf("WorkspaceID = %q, want %q", got.WorkspaceID, src.WorkspaceID)
	}
	if got.Objective != src.Objective {
		t.Errorf("Objective = %q, want %q", got.Objective, src.Objective)
	}
	if got.Phase != src.Phase {
		t.Errorf("Phase = %q, want %q", got.Phase, src.Phase)
	}
	if len(got.HardConstraints) != 1 || got.HardConstraints[0] != "no breaking changes" {
		t.Errorf("HardConstraints = %v", got.HardConstraints)
	}
	if len(got.Blockers) != 1 || got.Blockers[0] != "missing types" {
		t.Errorf("Blockers = %v", got.Blockers)
	}
	if len(got.RecentDecisions) != 1 || got.RecentDecisions[0].Text != "Use TDD" {
		t.Errorf("RecentDecisions = %v", got.RecentDecisions)
	}
	if len(got.OpenLoops) != 1 || got.OpenLoops[0] != "verify coverage" {
		t.Errorf("OpenLoops = %v", got.OpenLoops)
	}
	if len(got.NextActions) != 1 || got.NextActions[0] != "write tests" {
		t.Errorf("NextActions = %v", got.NextActions)
	}
	if len(got.RelevantRefs) != 2 {
		t.Fatalf("RelevantRefs = %d, want 2", len(got.RelevantRefs))
	}
	if got.RelevantRefs[0].Type != contextengine.RefTypePath || got.RelevantRefs[0].Ref != "types.go" {
		t.Errorf("RelevantRefs[0] = %v", got.RelevantRefs[0])
	}
	if got.RelevantRefs[1].Type != contextengine.RefTypeTask || got.RelevantRefs[1].Ref != "abc" {
		t.Errorf("RelevantRefs[1] = %v", got.RelevantRefs[1])
	}

	// Verify metadata preserves active_task_ids
	tasks, _ := got.Metadata["active_task_ids"].([]string)
	if len(tasks) != 2 || tasks[0] != "t1" {
		t.Errorf("metadata[active_task_ids] = %v", tasks)
	}
}

func TestConvertHandoff(t *testing.T) {
	now := time.Now().UTC()
	src := contextplane.Handoff{
		TaskID:              "t1",
		Phase:               "done",
		Outcome:             "success",
		Summary:             "Implemented adapters",
		EvidenceRefs:        []contextengine.EvidenceRef{{Type: contextengine.RefTypePath, Ref: "adapters.go"}},
		FileRefs:            []contextengine.EvidenceRef{{Type: contextengine.RefTypePath, Ref: "new_file.go"}},
		Observations:        []string{"Types are clean"},
		Tensions:            []string{"Need more tests"},
		NextActions:         []string{"Run coverage"},
		PromotionCandidates: []string{"path:proposal.md"},
		CreatedAt:           now,
	}

	got := ConvertHandoff(src)

	if got.TaskID != "t1" {
		t.Errorf("TaskID = %q", got.TaskID)
	}
	if got.Phase != "done" {
		t.Errorf("Phase = %q", got.Phase)
	}
	if got.Objective != "Implemented adapters" {
		t.Errorf("Objective = %q", got.Objective)
	}
	// Should have both evidence refs and file refs
	if len(got.RelevantRefs) != 2 {
		t.Fatalf("RelevantRefs = %d, want 2", len(got.RelevantRefs))
	}
}

func TestConvertObservation(t *testing.T) {
	now := time.Now().UTC()
	src := contextplane.Observation{
		ID:           "obs1",
		Statement:    "Tests pass consistently",
		Confidence:   0.95,
		Count:        5,
		Project:      "foxctl",
		Area:         "contextengine",
		EvidenceRefs: []contextengine.EvidenceRef{{Type: contextengine.RefTypePath, Ref: "test.go"}},
		FirstSeen:    now,
		LastSeen:     now,
	}

	got := ConvertObservation("ws1", src)

	if got.ID != "obs1" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.WorkspaceID != "ws1" {
		t.Errorf("WorkspaceID = %q", got.WorkspaceID)
	}
	if got.NodeType != contextengine.EvidenceNodeTypeObservation {
		t.Errorf("NodeType = %q", got.NodeType)
	}
	if got.Statement != "Tests pass consistently" {
		t.Errorf("Statement = %q", got.Statement)
	}
	if got.Confidence != 0.95 {
		t.Errorf("Confidence = %f", got.Confidence)
	}
	if got.Count != 5 {
		t.Errorf("Count = %d", got.Count)
	}
}

func TestConvertTension(t *testing.T) {
	now := time.Now().UTC()
	src := contextplane.Tension{
		ID:          "ten1",
		Kind:        "architectural",
		Statement:   "Circular dependency risk",
		Impact:      "high",
		RelatedRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypePath, Ref: "a.go"},
			{Type: contextengine.RefTypePath, Ref: "b.go"},
		},
		Status:      "open",
		Count:       3,
		CreatedAt:   now,
		LastSeen:    now,
	}

	got := ConvertTension("ws1", src)

	if got.NodeType != contextengine.EvidenceNodeTypeTension {
		t.Errorf("NodeType = %q", got.NodeType)
	}
	if got.Statement != "Circular dependency risk" {
		t.Errorf("Statement = %q", got.Statement)
	}
	if got.Metadata["kind"] != "architectural" {
		t.Errorf("metadata[kind] = %v", got.Metadata["kind"])
	}
	if got.Metadata["impact"] != "high" {
		t.Errorf("metadata[impact] = %v", got.Metadata["impact"])
	}
}

func TestConvertMemoryProposal(t *testing.T) {
	now := time.Now().UTC()
	src := contextplane.MemoryProposal{
		ID:             "mp1",
		Kind:           contextplane.PolicyKindMemoryDraft,
		Classification: "workspace",
		Status:         "active",
		Confidence:     0.8,
		BlastRadius:    "internal/context/",
		Summary:        "Prefer typed enums over raw strings",
		SourceRefs:     []contextengine.EvidenceRef{{Type: contextengine.RefTypePath, Ref: "refs.go"}},
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	got := ConvertMemoryProposal("ws1", src)

	if got.ClaimType != string(contextplane.PolicyKindMemoryDraft) {
		t.Errorf("ClaimType = %q", got.ClaimType)
	}
	if got.Status != contextengine.ClaimStatusCandidate {
		t.Errorf("Status = %q, want candidate", got.Status)
	}
	if got.Summary != "Prefer typed enums over raw strings" {
		t.Errorf("Summary = %q", got.Summary)
	}
	if got.Confidence != 0.8 {
		t.Errorf("Confidence = %f", got.Confidence)
	}
	if got.BlastRadius != "internal/context/" {
		t.Errorf("BlastRadius = %q", got.BlastRadius)
	}
}

func TestMapProposalStatus(t *testing.T) {
	tests := []struct {
		status string
		want   contextengine.ClaimStatus
	}{
		{"active", contextengine.ClaimStatusCandidate},
		{"pending", contextengine.ClaimStatusCandidate},
		{"open", contextengine.ClaimStatusCandidate},
		{"current", contextengine.ClaimStatusCurrent},
		{"accepted", contextengine.ClaimStatusCurrent},
		{"approved", contextengine.ClaimStatusCurrent},
		{"rejected", contextengine.ClaimStatusRejected},
		{"closed", contextengine.ClaimStatusRejected},
		{"superseded", contextengine.ClaimStatusSuperseded},
		{"unknown_status", contextengine.ClaimStatusCandidate},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := mapProposalStatus(tt.status)
			if got != tt.want {
				t.Errorf("mapProposalStatus(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestConvertTaskPacket(t *testing.T) {
	now := time.Now().UTC()
	src := contextplane.TaskPacket{
		WorkspaceID: "ws1",
		Task: contextplane.TaskCandidate{
			ID:        "t1",
			Title:     "Implement adapters",
			Status:    "in_progress",
			ScopePath: "internal/context/",
		},
		Objective:   "Create 8 adapter files",
		Phase:       "implementation",
		NextActions: []string{"Write tests"},
		RelevantRefs: []contextengine.EvidenceRef{{Type: contextengine.RefTypePath, Ref: "types.go"}},
		GeneratedAt: now,
	}

	got := ConvertTaskPacket(src)

	if got.WorkspaceID != "ws1" {
		t.Errorf("WorkspaceID = %q", got.WorkspaceID)
	}
	if got.TaskID != "t1" {
		t.Errorf("TaskID = %q", got.TaskID)
	}
	if got.Objective != "Create 8 adapter files" {
		t.Errorf("Objective = %q", got.Objective)
	}
	if got.Status != "in_progress" {
		t.Errorf("Status = %q", got.Status)
	}
	if got.Scope.Path != "internal/context/" {
		t.Errorf("Scope.Path = %q", got.Scope.Path)
	}
	if len(got.RelatedCodeRefs) != 1 {
		t.Fatalf("RelatedCodeRefs = %d, want 1", len(got.RelatedCodeRefs))
	}
	if got.RelatedCodeRefs[0].Ref != "types.go" {
		t.Errorf("RelatedCodeRefs[0].Ref = %q", got.RelatedCodeRefs[0].Ref)
	}
	if got.ProjectionMeta.ProjectionVersion != 1 {
		t.Errorf("ProjectionVersion = %d", got.ProjectionMeta.ProjectionVersion)
	}
}

func TestConvertRetrievalHit(t *testing.T) {
	src := contextplane.RetrievalHit{
		Path:              "docs/arch.md",
		Title:             "Architecture",
		Type:              "note",
		Trust:             "canonical",
		Score:             85,
		Snippet:           "System uses events",
		PrimaryAnchorPath: "docs/arch.md",
		RepoPaths:         []string{"src/"},
		AnchorPaths:       []string{"docs/"},
		Symbols:           []string{"func Foo"},
	}

	got := ConvertRetrievalHit("ws1", src)

	if got.NodeType != contextengine.EvidenceNodeTypeRetrieval {
		t.Errorf("NodeType = %q", got.NodeType)
	}
	if got.Ref.Ref != "docs/arch.md" {
		t.Errorf("Ref.Ref = %q", got.Ref.Ref)
	}
	if got.Confidence != 0.85 {
		t.Errorf("Confidence = %f", got.Confidence)
	}
	if got.Metadata["title"] != "Architecture" {
		t.Errorf("metadata[title] = %v", got.Metadata["title"])
	}
}

func TestConvertRetrievalResult(t *testing.T) {
	now := time.Now().UTC()
	src := contextplane.RetrievalResult{
		WorkspaceID: "ws1",
		Query:       "architecture",
		Observations: []contextplane.Observation{
			{ID: "obs1", Statement: "Test obs", FirstSeen: now, LastSeen: now},
		},
		Tensions: []contextplane.Tension{
			{ID: "ten1", Statement: "Test tension", CreatedAt: now},
		},
		VaultHits: []contextplane.RetrievalHit{
			{Path: "docs/a.md", Title: "A"},
		},
		GeneratedAt: now,
	}

	got := ConvertRetrievalResult(src)

	if got.WorkspaceID != "ws1" {
		t.Errorf("WorkspaceID = %q", got.WorkspaceID)
	}
	if got.Query != "architecture" {
		t.Errorf("Query = %q", got.Query)
	}
	if got.Lane != contextengine.LaneContext {
		t.Errorf("Lane = %q", got.Lane)
	}
	// Should have 3 nodes: 1 obs + 1 tension + 1 vault hit
	if len(got.Nodes) != 3 {
		t.Errorf("Nodes = %d, want 3", len(got.Nodes))
	}
}
