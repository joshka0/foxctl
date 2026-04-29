package adapters

import (
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

func TestConvertTrajectoryEvent(t *testing.T) {
	now := time.Now().UTC()
	src := trajectory.Event{
		ID:           "evt1",
		TrajectoryID: "traj1",
		TS:           now,
		Kind:         trajectory.EventKindToolCall,
		Actor:        "agent:coder",
		Command:      "skill.run",
		Status:       "ok",
		DataInline:   map[string]any{"key": "value"},
		DataArtifact: "sha256:abc",
		Meta: &trajectory.EventMeta{
			TraceID: "trace1",
			JobID:   "job1",
			TaskID:  "t1",
		},
	}

	got := ConvertTrajectoryEvent("ws1", src)

	if got.ID != "evt1" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.WorkspaceID != "ws1" {
		t.Errorf("WorkspaceID = %q", got.WorkspaceID)
	}
	if got.Kind != contextengine.EventKindToolEvidenceProduced {
		t.Errorf("Kind = %q", got.Kind)
	}
	if got.Source != "agent:coder" {
		t.Errorf("Source = %q", got.Source)
	}
	if got.SessionID != "traj1" {
		t.Errorf("SessionID = %q", got.SessionID)
	}
	if got.TaskID != "t1" {
		t.Errorf("TaskID = %q", got.TaskID)
	}
	if got.Data["data_artifact"] != "sha256:abc" {
		t.Errorf("Data[data_artifact] = %v", got.Data["data_artifact"])
	}
	if got.Data["trace_id"] != "trace1" {
		t.Errorf("Data[trace_id] = %v", got.Data["trace_id"])
	}
	if got.Data["job_id"] != "job1" {
		t.Errorf("Data[job_id] = %v", got.Data["job_id"])
	}
	if got.Data["key"] != "value" {
		t.Errorf("Data[key] = %v", got.Data["key"])
	}
	if got.CreatedAt != now {
		t.Errorf("CreatedAt mismatch")
	}
}

func TestConvertTrajectoryEvent_NilMeta(t *testing.T) {
	src := trajectory.Event{
		ID:           "evt2",
		TrajectoryID: "traj1",
		TS:           time.Now().UTC(),
		Kind:         trajectory.EventKindUserRequest,
	}
	got := ConvertTrajectoryEvent("ws1", src)
	if got.TaskID != "" {
		t.Errorf("TaskID should be empty for nil meta, got %q", got.TaskID)
	}
}

func TestConvertTrajectory(t *testing.T) {
	now := time.Now().UTC()
	src := trajectory.Trajectory{
		ID:            "traj1",
		WorkspaceID:   "ws1",
		RootRequestID: "req1",
		TaskIDs:       []string{"t1", "t2"},
		EpicID:        "epic1",
		AgentRole:     "coder",
		JobID:         "job1",
		TraceID:       "trace1",
		Status:        trajectory.StatusOK,
		Summary:       "Completed task",
		CreatedAt:     now,
		UpdatedAt:     now,
		SessionID:     "sess1",
	}

	got := ConvertTrajectory(src)

	if got.WorkspaceID != "ws1" {
		t.Errorf("WorkspaceID = %q", got.WorkspaceID)
	}
	if got.TaskID != "t1" {
		t.Errorf("TaskID = %q (should be first task)", got.TaskID)
	}
	if got.Objective != "Completed task" {
		t.Errorf("Objective = %q", got.Objective)
	}
	if got.Phase != "ok" {
		t.Errorf("Phase = %q", got.Phase)
	}
	// Should have 3 refs: 2 tasks + 1 epic
	if len(got.RelevantRefs) != 3 {
		t.Fatalf("RelevantRefs = %d, want 3", len(got.RelevantRefs))
	}
	if got.RelevantRefs[0].Type != contextengine.RefTypeTask || got.RelevantRefs[0].Ref != "t1" {
		t.Errorf("RelevantRefs[0] = %v", got.RelevantRefs[0])
	}
	if got.RelevantRefs[2].Type != contextengine.RefTypeTask || got.RelevantRefs[2].Ref != "epic1" {
		t.Errorf("RelevantRefs[2] = %v", got.RelevantRefs[2])
	}
}

func TestConvertTrajectory_NoEpic(t *testing.T) {
	src := trajectory.Trajectory{
		ID:          "traj2",
		WorkspaceID: "ws1",
		TaskIDs:     []string{"t1"},
		Status:      trajectory.StatusOK,
		CreatedAt:   time.Now().UTC(),
	}
	got := ConvertTrajectory(src)
	if len(got.RelevantRefs) != 1 {
		t.Errorf("RelevantRefs = %d, want 1 (no epic)", len(got.RelevantRefs))
	}
}

func TestConvertTrajectoryToContextEvent(t *testing.T) {
	now := time.Now().UTC()
	src := trajectory.Trajectory{
		ID:          "traj3",
		WorkspaceID: "ws1",
		AgentRole:   "planner",
		JobID:       "job1",
		TraceID:     "trace1",
		Status:      trajectory.StatusOK,
		TaskIDs:     []string{"t1"},
		SessionID:   "sess1",
		Outcome: &trajectory.Outcome{
			Success:        true,
			TasksCompleted: 3,
			ToolCallCount:  10,
			DurationMS:     5000,
		},
		CreatedAt: now,
	}

	got := ConvertTrajectoryToContextEvent(src)

	if got.WorkspaceID != "ws1" {
		t.Errorf("WorkspaceID = %q", got.WorkspaceID)
	}
	if got.Kind != contextengine.EventKindSessionTurnCaptured {
		t.Errorf("Kind = %q", got.Kind)
	}
	if got.Source != "trajectory:planner" {
		t.Errorf("Source = %q", got.Source)
	}
	if got.TaskID != "t1" {
		t.Errorf("TaskID = %q", got.TaskID)
	}
	if got.SessionID != "sess1" {
		t.Errorf("SessionID = %q", got.SessionID)
	}
	if got.Data["success"] != true {
		t.Errorf("Data[success] = %v", got.Data["success"])
	}
	if got.Data["tasks_completed"] != 3 {
		t.Errorf("Data[tasks_completed] = %v", got.Data["tasks_completed"])
	}
}

func TestMapTrajectoryEventKind(t *testing.T) {
	tests := []struct {
		kind trajectory.EventKind
		want contextengine.ContextEventKind
	}{
		{trajectory.EventKindToolCall, contextengine.EventKindToolEvidenceProduced},
		{trajectory.EventKindToolResult, contextengine.EventKindToolEvidenceProduced},
		{trajectory.EventKindUserRequest, contextengine.EventKindSessionTurnCaptured},
		{trajectory.EventKindAgentThought, contextengine.EventKindSessionTurnCaptured},
		{trajectory.EventKindReviewRequest, contextengine.EventKindSessionTurnCaptured},
		{trajectory.EventKindReviewResult, contextengine.EventKindCodeValidated},
		{trajectory.EventKindTaskTransition, contextengine.EventKindTaskChanged},
		{trajectory.EventKindGraphSearch, contextengine.EventKindRetrievalExecuted},
		{trajectory.EventKindSWEGrep, contextengine.EventKindRetrievalExecuted},
		{trajectory.EventKindHookCall, contextengine.EventKindToolEvidenceProduced},
		{trajectory.EventKindHookResult, contextengine.EventKindToolEvidenceProduced},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			got := mapTrajectoryEventKind(tt.kind)
			if got != tt.want {
				t.Errorf("mapTrajectoryEventKind(%q) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}
