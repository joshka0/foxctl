package adapters

import (
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/context/contextplane/taskhistory"
)

func TestConvertPack(t *testing.T) {
	now := time.Now().UTC()
	src := taskhistory.Pack{
		WorkspacePath: "/path/to/ws",
		WorkspaceID:   "ws1",
		GeneratedAt:   now,
		Task: contextplane.TaskCandidate{
			ID:        "t1",
			Title:     "Build adapters",
			Status:    "in_progress",
			ScopePath: "internal/context/",
		},
		TaskPacket: contextplane.TaskPacket{
			WorkspaceID: "ws1",
			Task: contextplane.TaskCandidate{
				ID:     "t1",
				Title:  "Build adapters",
				Status: "in_progress",
			},
			Objective:   "Create 8 adapter files",
			Phase:       "implementation",
			NextActions: []string{"Write tests"},
		},
		FilesTouched: []string{"path:adapters.go"},
		ExternalRefs: []string{"task:abc"},
		Sessions: []taskhistory.SessionSummary{
			{
				ID:      "sess1",
				Summary: "Discussed adapter design",
			},
		},
		Summary: "Pack for adapter task",
	}

	got := ConvertPack(src)

	if got.WorkspaceID != "ws1" {
		t.Errorf("WorkspaceID = %q", got.WorkspaceID)
	}
	if got.TaskID != "t1" {
		t.Errorf("TaskID = %q", got.TaskID)
	}
	if got.Objective != "Create 8 adapter files" {
		t.Errorf("Objective = %q", got.Objective)
	}
	if got.Phase != "implementation" {
		t.Errorf("Phase = %q", got.Phase)
	}
	if len(got.NextActions) != 1 || got.NextActions[0] != "Write tests" {
		t.Errorf("NextActions = %v", got.NextActions)
	}
	// Should have file refs + external refs
	if len(got.RelevantRefs) != 2 {
		t.Fatalf("RelevantRefs = %d, want 2", len(got.RelevantRefs))
	}
}

func TestConvertSessionSummary(t *testing.T) {
	now := time.Now().UTC()
	src := taskhistory.SessionSummary{
		ID:                "sess1",
		Reason:            "task handoff",
		ProjectName:       "foxctl",
		Summary:           "Implemented store layer",
		Accomplished:      []string{"Created SQLite store"},
		Decisions:         []string{"Use WAL mode"},
		Gotchas:           []string{"Watch for lock contention"},
		KeyFiles:          []string{"internal/storage/store.go"},
		TimelineSummaries: []string{"Started store"},
		TimelineTools:     []string{"go test"},
		TimelineFiles:     []string{"store.go"},
		RecentFilesTouched: []string{"store.go"},
		StartedAt:         now,
		EndedAt:           now,
	}

	got := ConvertSessionSummary("ws1", src)

	if got.ID != "sess1" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.WorkspaceID != "ws1" {
		t.Errorf("WorkspaceID = %q", got.WorkspaceID)
	}
	if got.NodeType != contextengine.EvidenceNodeTypeContext {
		t.Errorf("NodeType = %q", got.NodeType)
	}
	if got.Ref.Type != contextengine.RefTypeSession || got.Ref.Ref != "sess1" {
		t.Errorf("Ref = %v", got.Ref)
	}
	if got.Statement != "Implemented store layer" {
		t.Errorf("Statement = %q", got.Statement)
	}
	// Verify metadata preserves all fields
	if got.Metadata["reason"] != "task handoff" {
		t.Errorf("metadata[reason] = %v", got.Metadata["reason"])
	}
	if got.Metadata["project_name"] != "foxctl" {
		t.Errorf("metadata[project_name] = %v", got.Metadata["project_name"])
	}
}

func TestExtractSessionEvidence(t *testing.T) {
	sessions := []taskhistory.SessionSummary{
		{ID: "s1", Summary: "First session"},
		{ID: "s2", Summary: "Second session"},
	}
	nodes := ExtractSessionEvidence("ws1", sessions)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0].ID != "s1" {
		t.Errorf("nodes[0].ID = %q", nodes[0].ID)
	}
	if nodes[1].ID != "s2" {
		t.Errorf("nodes[1].ID = %q", nodes[1].ID)
	}
}

func TestExtractSessionEvidence_Empty(t *testing.T) {
	nodes := ExtractSessionEvidence("ws1", nil)
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes for nil, got %d", len(nodes))
	}
}
