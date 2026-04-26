package adapters

import (
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/storage/tasks"
)

func TestConvertTask(t *testing.T) {
	now := time.Now().UTC()
	src := tasks.Task{
		ID:              "t1",
		WorkspaceID:     "ws1",
		Title:           "Implement adapters",
		Description:     "Create 8 adapter files",
		ScopePath:       "internal/context/",
		Status:          "in_progress",
		AssignedActorID: "actor:agent:coder",
		OwnerActorID:    "actor:human:josh",
		BlockedReason:   "",
		CreatedAt:       now,
	}

	got := ConvertTask(src)

	if got.WorkspaceID != "ws1" {
		t.Errorf("WorkspaceID = %q", got.WorkspaceID)
	}
	if got.TaskID != "t1" {
		t.Errorf("TaskID = %q", got.TaskID)
	}
	if got.Objective != "Implement adapters" {
		t.Errorf("Objective = %q", got.Objective)
	}
	if got.Status != "in_progress" {
		t.Errorf("Status = %q", got.Status)
	}
	if got.Scope.Path != "internal/context/" {
		t.Errorf("Scope.Path = %q", got.Scope.Path)
	}
	if got.Scope.TaskID != "t1" {
		t.Errorf("Scope.TaskID = %q", got.Scope.TaskID)
	}
	if got.ProjectionMeta.ProjectionID == "" {
		t.Error("ProjectionMeta.ProjectionID should not be empty")
	}
	if got.ProjectionMeta.ProjectionVersion != 1 {
		t.Errorf("ProjectionVersion = %d", got.ProjectionMeta.ProjectionVersion)
	}
}

func TestConvertTaskToContextPacket(t *testing.T) {
	now := time.Now().UTC()
	src := tasks.Task{
		ID:              "t2",
		WorkspaceID:     "ws2",
		Title:           "Write tests",
		Description:     "Comprehensive test coverage",
		ScopePath:       "internal/storage/",
		Status:          "pending",
		AssignedActorID: "agent1",
		PlanFile:        "plan.md",
		PlanSection:     "Phase 1",
		SessionID:       "sess1",
		EpicID:          "epic1",
		BlockedReason:   "waiting for types",
		CreatedAt:       now,
	}

	got := ConvertTaskToContextPacket(src)

	if got.WorkspaceID != "ws2" {
		t.Errorf("WorkspaceID = %q", got.WorkspaceID)
	}
	if got.TaskID != "t2" {
		t.Errorf("TaskID = %q", got.TaskID)
	}
	if got.Objective != "Write tests" {
		t.Errorf("Objective = %q", got.Objective)
	}
	if got.Metadata["description"] != "Comprehensive test coverage" {
		t.Errorf("metadata[description] = %v", got.Metadata["description"])
	}
	if got.Metadata["scope_path"] != "internal/storage/" {
		t.Errorf("metadata[scope_path] = %v", got.Metadata["scope_path"])
	}
	if got.Metadata["plan_file"] != "plan.md" {
		t.Errorf("metadata[plan_file] = %v", got.Metadata["plan_file"])
	}
	if got.Metadata["session_id"] != "sess1" {
		t.Errorf("metadata[session_id] = %v", got.Metadata["session_id"])
	}
	if got.Metadata["epic_id"] != "epic1" {
		t.Errorf("metadata[epic_id] = %v", got.Metadata["epic_id"])
	}
	// Blocked reason should appear in Blockers
	if len(got.Blockers) != 1 || got.Blockers[0] != "waiting for types" {
		t.Errorf("Blockers = %v", got.Blockers)
	}
}

func TestConvertEpic(t *testing.T) {
	now := time.Now().UTC()
	src := tasks.Epic{
		ID:          "epic1",
		WorkspaceID: "ws1",
		Title:       "Context Engine",
		Goal:        "Unify all context lanes",
		Status:      "active",
		SessionID:   "sess1",
		CreatedAt:   now,
	}

	got := ConvertEpic("ws1", src)

	if got.ID != "epic1" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.WorkspaceID != "ws1" {
		t.Errorf("WorkspaceID = %q", got.WorkspaceID)
	}
	if got.NodeType != contextengine.EvidenceNodeTypeTask {
		t.Errorf("NodeType = %q", got.NodeType)
	}
	if got.Ref.Type != contextengine.RefTypeTask || got.Ref.Ref != "epic1" {
		t.Errorf("Ref = %v", got.Ref)
	}
	if got.Statement != "Unify all context lanes" {
		t.Errorf("Statement = %q", got.Statement)
	}
	if got.Metadata["title"] != "Context Engine" {
		t.Errorf("metadata[title] = %v", got.Metadata["title"])
	}
	if got.Metadata["status"] != "active" {
		t.Errorf("metadata[status] = %v", got.Metadata["status"])
	}
}

func TestConvertTask_WithScopePath(t *testing.T) {
	src := tasks.Task{
		ID:          "t3",
		WorkspaceID: "ws1",
		Title:       "Task",
		ScopePath:   "internal/context/",
		CreatedAt:   time.Now().UTC(),
	}
	got := ConvertTask(src)
	if len(got.RelatedCodeRefs) != 1 {
		t.Fatalf("RelatedCodeRefs = %d, want 1", len(got.RelatedCodeRefs))
	}
	if got.RelatedCodeRefs[0].Type != contextengine.RefTypePath {
		t.Errorf("RelatedCodeRefs[0].Type = %q", got.RelatedCodeRefs[0].Type)
	}
}

func TestConvertTask_NoScopePath(t *testing.T) {
	src := tasks.Task{
		ID:          "t4",
		WorkspaceID: "ws1",
		Title:       "Task",
		CreatedAt:   time.Now().UTC(),
	}
	got := ConvertTask(src)
	if len(got.RelatedCodeRefs) != 0 {
		t.Errorf("RelatedCodeRefs = %d, want 0 for empty scope", len(got.RelatedCodeRefs))
	}
}
