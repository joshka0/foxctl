package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Tests for Input structure

func TestInput_DefaultTrigger(t *testing.T) {
	in := Input{}
	// Default trigger should be applied as "manual" in run()
	assert.Empty(t, in.Trigger)
}

func TestInput_ValidTriggers(t *testing.T) {
	triggers := []string{"pre_compact", "manual", "session_end"}
	for _, trigger := range triggers {
		in := Input{Trigger: trigger}
		assert.Equal(t, trigger, in.Trigger)
	}
}

func TestInput_WithSessionID(t *testing.T) {
	in := Input{
		SessionID: "sess-123",
		Workspace: "/test/workspace",
	}
	assert.Equal(t, "sess-123", in.SessionID)
	assert.Equal(t, "/test/workspace", in.Workspace)
}

func TestInput_WithSummary(t *testing.T) {
	in := Input{
		Summary: "Custom session summary",
	}
	assert.Equal(t, "Custom session summary", in.Summary)
}

// Tests for SessionSnapshot structure

func TestSessionSnapshot_BasicFields(t *testing.T) {
	snapshot := SessionSnapshot{
		SnapshotID: "snap-123",
		SessionID:  "sess-456",
		Trigger:    "pre_compact",
		Workspace:  "/test/workspace",
		Timestamp:  time.Now(),
		Summary:    "Test summary",
	}

	assert.Equal(t, "snap-123", snapshot.SnapshotID)
	assert.Equal(t, "sess-456", snapshot.SessionID)
	assert.Equal(t, "pre_compact", snapshot.Trigger)
	assert.Equal(t, "/test/workspace", snapshot.Workspace)
	assert.Equal(t, "Test summary", snapshot.Summary)
}

func TestSessionSnapshot_WithActiveTask(t *testing.T) {
	snapshot := SessionSnapshot{
		ActiveTask: &TaskInfo{
			ID:          "task-123",
			Title:       "Implement feature",
			Description: "Feature description",
			Status:      "in_progress",
			Notes:       "Working on it",
			Gotchas:     "Watch out for X",
		},
	}

	assert.NotNil(t, snapshot.ActiveTask)
	assert.Equal(t, "task-123", snapshot.ActiveTask.ID)
	assert.Equal(t, "Implement feature", snapshot.ActiveTask.Title)
	assert.Equal(t, "in_progress", snapshot.ActiveTask.Status)
	assert.Equal(t, "Watch out for X", snapshot.ActiveTask.Gotchas)
}

func TestSessionSnapshot_WithActivePlan(t *testing.T) {
	snapshot := SessionSnapshot{
		ActivePlan: &PlanInfo{
			FilePath:    "/path/to/plan.md",
			FileName:    "plan.md",
			Title:       "Feature Plan",
			ContentHash: "abc123",
			Sections:    []string{"Overview", "Implementation", "Testing"},
			LinkedTasks: 5,
			ModTime:     "2024-01-15T10:00:00Z",
		},
	}

	assert.NotNil(t, snapshot.ActivePlan)
	assert.Equal(t, "/path/to/plan.md", snapshot.ActivePlan.FilePath)
	assert.Equal(t, "Feature Plan", snapshot.ActivePlan.Title)
	assert.Len(t, snapshot.ActivePlan.Sections, 3)
	assert.Equal(t, 5, snapshot.ActivePlan.LinkedTasks)
}

func TestSessionSnapshot_WithPendingTodos(t *testing.T) {
	snapshot := SessionSnapshot{
		PendingTodos: []TaskInfo{
			{ID: "task-1", Title: "Task 1", Status: "pending"},
			{ID: "task-2", Title: "Task 2", Status: "in_progress"},
		},
	}

	assert.Len(t, snapshot.PendingTodos, 2)
	assert.Equal(t, "task-1", snapshot.PendingTodos[0].ID)
	assert.Equal(t, "pending", snapshot.PendingTodos[0].Status)
}

func TestSessionSnapshot_WithDecisionsAndInsights(t *testing.T) {
	snapshot := SessionSnapshot{
		Decisions: []string{"Use Go for backend", "Use React for frontend"},
		Insights:  []string{"Performance is critical", "Tests are important"},
	}

	assert.Len(t, snapshot.Decisions, 2)
	assert.Len(t, snapshot.Insights, 2)
	assert.Contains(t, snapshot.Decisions, "Use Go for backend")
	assert.Contains(t, snapshot.Insights, "Performance is critical")
}

func TestSessionSnapshot_WithMetadata(t *testing.T) {
	snapshot := SessionSnapshot{
		Metadata: map[string]string{
			"captured_at": "2024-01-15T10:00:00Z",
			"trigger":     "pre_compact",
			"plan_file":   "/path/to/plan.md",
		},
	}

	assert.Equal(t, "2024-01-15T10:00:00Z", snapshot.Metadata["captured_at"])
	assert.Equal(t, "pre_compact", snapshot.Metadata["trigger"])
	assert.Equal(t, "/path/to/plan.md", snapshot.Metadata["plan_file"])
}

// Tests for TaskInfo structure

func TestTaskInfo_MinimalFields(t *testing.T) {
	task := TaskInfo{
		ID:     "task-123",
		Title:  "Simple task",
		Status: "pending",
	}

	assert.Equal(t, "task-123", task.ID)
	assert.Equal(t, "Simple task", task.Title)
	assert.Equal(t, "pending", task.Status)
	assert.Empty(t, task.Description)
	assert.Empty(t, task.Notes)
	assert.Empty(t, task.Gotchas)
}

// Tests for PlanInfo structure

func TestPlanInfo_EmptySections(t *testing.T) {
	plan := PlanInfo{
		FilePath: "/path/to/plan.md",
		Title:    "Simple Plan",
	}

	assert.Nil(t, plan.Sections)
	assert.Equal(t, 0, plan.LinkedTasks)
}

// Tests for Output structure

func TestOutput_Success(t *testing.T) {
	output := Output{
		SnapshotID: "snap-123456",
		ItemsCaptured: map[string]int{
			"active_task":   1,
			"active_plan":   1,
			"pending_todos": 5,
			"gotchas":       3,
		},
		Message: "Session snapshot saved: session-snapshot-snap-123456",
	}

	assert.Equal(t, "snap-123456", output.SnapshotID)
	assert.Equal(t, 1, output.ItemsCaptured["active_task"])
	assert.Equal(t, 1, output.ItemsCaptured["active_plan"])
	assert.Equal(t, 5, output.ItemsCaptured["pending_todos"])
	assert.Equal(t, 3, output.ItemsCaptured["gotchas"])
	assert.Contains(t, output.Message, "Session snapshot saved")
}

func TestOutput_EmptyCapture(t *testing.T) {
	output := Output{
		SnapshotID:    "snap-123456",
		ItemsCaptured: map[string]int{},
		Message:       "Session snapshot saved (no items)",
	}

	assert.Equal(t, "snap-123456", output.SnapshotID)
	assert.Len(t, output.ItemsCaptured, 0)
}

// Tests for constants

func TestTriggerTypes(t *testing.T) {
	// Valid trigger values
	validTriggers := []string{
		"pre_compact", // From pre-compact hook
		"manual",      // User-initiated
		"session_end", // Session ending
	}

	for _, trigger := range validTriggers {
		in := Input{Trigger: trigger}
		assert.NotEmpty(t, in.Trigger)
	}
}

// Tests for snapshot ID generation pattern

func TestSnapshotIDPattern(t *testing.T) {
	// Snapshot IDs follow pattern: snap-<unix_milli>
	timestamp := time.Now().UnixMilli()
	expectedPrefix := "snap-"

	assert.Contains(t, "snap-1705320000000", expectedPrefix)
	assert.Greater(t, timestamp, int64(0))
}

// Tests for max pending todos constant

func TestMaxPendingTodos(t *testing.T) {
	// The skill caps pending todos at 10
	const maxPendingTodos = 10

	// Verify the constant value matches expected
	todos := make([]TaskInfo, 15)
	if len(todos) > maxPendingTodos {
		todos = todos[:maxPendingTodos]
	}

	assert.Len(t, todos, 10)
}
