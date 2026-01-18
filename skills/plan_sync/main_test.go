package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "plan/sync", command)
}

// Tests for Input structure

func TestInput_AllFields(t *testing.T) {
	in := Input{
		Workspace:     "/workspace/path",
		WorkspaceRoot: "/root/path",
		SessionID:     "sess-123",
		PlanFile:      "plan.md",
		ImportTasks:   true,
		DryRun:        true,
		Force:         true,
		Provider:      "claude",
	}

	assert.Equal(t, "/workspace/path", in.Workspace)
	assert.Equal(t, "/root/path", in.WorkspaceRoot)
	assert.Equal(t, "sess-123", in.SessionID)
	assert.Equal(t, "plan.md", in.PlanFile)
	assert.True(t, in.ImportTasks)
	assert.True(t, in.DryRun)
	assert.True(t, in.Force)
	assert.Equal(t, "claude", in.Provider)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := Input{
		Workspace:   "/test/workspace",
		SessionID:   "sess-abc",
		PlanFile:    "test-plan.md",
		ImportTasks: true,
		Provider:    "opencode",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.SessionID, decoded.SessionID)
	assert.Equal(t, in.PlanFile, decoded.PlanFile)
	assert.Equal(t, in.ImportTasks, decoded.ImportTasks)
	assert.Equal(t, in.Provider, decoded.Provider)
}

func TestInput_EmptyFields(t *testing.T) {
	in := Input{}

	assert.Empty(t, in.Workspace)
	assert.Empty(t, in.WorkspaceRoot)
	assert.Empty(t, in.SessionID)
	assert.Empty(t, in.PlanFile)
	assert.False(t, in.ImportTasks)
	assert.False(t, in.DryRun)
	assert.False(t, in.Force)
	assert.Empty(t, in.Provider)
}

func TestInput_ProviderValues(t *testing.T) {
	providers := []string{"claude", "opencode"}

	for _, provider := range providers {
		in := Input{Provider: provider}
		assert.Equal(t, provider, in.Provider)
	}
}

// Tests for SyncResult structure

func TestSyncResult_AllFields(t *testing.T) {
	result := SyncResult{
		PlanFile:     "/path/to/plan.md",
		PlanTitle:    "Test Plan",
		ContentHash:  "abc123",
		Status:       "synced",
		TasksCreated: 5,
		TasksSkipped: 2,
		Steps:        []StepResult{{Title: "Step 1"}},
		Error:        "",
	}

	assert.Equal(t, "/path/to/plan.md", result.PlanFile)
	assert.Equal(t, "Test Plan", result.PlanTitle)
	assert.Equal(t, "abc123", result.ContentHash)
	assert.Equal(t, "synced", result.Status)
	assert.Equal(t, 5, result.TasksCreated)
	assert.Equal(t, 2, result.TasksSkipped)
	assert.Len(t, result.Steps, 1)
	assert.Empty(t, result.Error)
}

func TestSyncResult_StatusValues(t *testing.T) {
	statuses := []string{"synced", "unchanged", "created", "error"}

	for _, status := range statuses {
		result := SyncResult{Status: status}
		assert.Equal(t, status, result.Status)
	}
}

func TestSyncResult_JSONSerialization(t *testing.T) {
	result := SyncResult{
		PlanFile:     "plan.md",
		PlanTitle:    "Test Plan",
		ContentHash:  "hash123",
		Status:       "created",
		TasksCreated: 3,
	}

	data, err := json.Marshal(result)
	assert.NoError(t, err)

	var decoded SyncResult
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, result.PlanFile, decoded.PlanFile)
	assert.Equal(t, result.PlanTitle, decoded.PlanTitle)
	assert.Equal(t, result.Status, decoded.Status)
	assert.Equal(t, result.TasksCreated, decoded.TasksCreated)
}

// Tests for StepResult structure

func TestStepResult_AllFields(t *testing.T) {
	step := StepResult{
		Title:   "Implement feature",
		Section: "Phase 1 > Implementation",
		Status:  "created",
		TaskID:  "task-123",
	}

	assert.Equal(t, "Implement feature", step.Title)
	assert.Equal(t, "Phase 1 > Implementation", step.Section)
	assert.Equal(t, "created", step.Status)
	assert.Equal(t, "task-123", step.TaskID)
}

func TestStepResult_StatusValues(t *testing.T) {
	statuses := []string{"created", "exists", "skipped", "would_create", "error"}

	for _, status := range statuses {
		step := StepResult{Status: status}
		assert.Equal(t, status, step.Status)
	}
}

func TestStepResult_JSONSerialization(t *testing.T) {
	step := StepResult{
		Title:   "Test Step",
		Section: "Section 1",
		Status:  "created",
		TaskID:  "task-abc",
	}

	data, err := json.Marshal(step)
	assert.NoError(t, err)

	var decoded StepResult
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, step.Title, decoded.Title)
	assert.Equal(t, step.Section, decoded.Section)
	assert.Equal(t, step.Status, decoded.Status)
	assert.Equal(t, step.TaskID, decoded.TaskID)
}

// Tests for Output structure

func TestOutput_AllFields(t *testing.T) {
	output := Output{
		PlansProcessed: 5,
		PlansChanged:   3,
		TasksCreated:   10,
		DryRun:         false,
		Provider:       "claude",
		Results:        []SyncResult{{Status: "synced"}},
		Message:        "Synced successfully",
	}

	assert.Equal(t, 5, output.PlansProcessed)
	assert.Equal(t, 3, output.PlansChanged)
	assert.Equal(t, 10, output.TasksCreated)
	assert.False(t, output.DryRun)
	assert.Equal(t, "claude", output.Provider)
	assert.Len(t, output.Results, 1)
	assert.Equal(t, "Synced successfully", output.Message)
}

func TestOutput_JSONSerialization(t *testing.T) {
	output := Output{
		PlansProcessed: 2,
		PlansChanged:   1,
		TasksCreated:   5,
		DryRun:         true,
		Provider:       "opencode",
		Message:        "Dry run complete",
	}

	data, err := json.Marshal(output)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, output.PlansProcessed, decoded.PlansProcessed)
	assert.Equal(t, output.PlansChanged, decoded.PlansChanged)
	assert.Equal(t, output.TasksCreated, decoded.TasksCreated)
	assert.Equal(t, output.DryRun, decoded.DryRun)
	assert.Equal(t, output.Provider, decoded.Provider)
}

// Tests for PlanSyncState structure

func TestPlanSyncState_AllFields(t *testing.T) {
	now := time.Now()
	state := PlanSyncState{
		PlanFile:    "/path/plan.md",
		ContentHash: "hash123",
		SyncedAt:    now,
		TasksLinked: []string{"task-1", "task-2"},
	}

	assert.Equal(t, "/path/plan.md", state.PlanFile)
	assert.Equal(t, "hash123", state.ContentHash)
	assert.Equal(t, now, state.SyncedAt)
	assert.Len(t, state.TasksLinked, 2)
}

func TestPlanSyncState_JSONSerialization(t *testing.T) {
	state := PlanSyncState{
		PlanFile:    "plan.md",
		ContentHash: "abc123",
		SyncedAt:    time.Now().Truncate(time.Second),
		TasksLinked: []string{"task-1"},
	}

	data, err := json.Marshal(state)
	assert.NoError(t, err)

	var decoded PlanSyncState
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, state.PlanFile, decoded.PlanFile)
	assert.Equal(t, state.ContentHash, decoded.ContentHash)
	assert.Equal(t, state.TasksLinked, decoded.TasksLinked)
}

// Tests for sanitizeFileName helper

func TestSanitizeFileName_Basic(t *testing.T) {
	result := sanitizeFileName("Test Plan.md")
	assert.Equal(t, "test-plan", result)
}

func TestSanitizeFileName_WithSpaces(t *testing.T) {
	result := sanitizeFileName("My Test Plan.md")
	assert.Equal(t, "my-test-plan", result)
}

func TestSanitizeFileName_AlreadyLowercase(t *testing.T) {
	result := sanitizeFileName("already-lowercase.md")
	assert.Equal(t, "already-lowercase", result)
}

func TestSanitizeFileName_NoExtension(t *testing.T) {
	result := sanitizeFileName("NoExtension")
	assert.Equal(t, "noextension", result)
}

func TestSanitizeFileName_MultipleExtensions(t *testing.T) {
	result := sanitizeFileName("file.test.md")
	assert.Equal(t, "file.test", result)
}

func TestSanitizeFileName_JSONExtension(t *testing.T) {
	result := sanitizeFileName("todos.json")
	assert.Equal(t, "todos", result)
}

func TestSanitizeFileName_Empty(t *testing.T) {
	result := sanitizeFileName("")
	assert.Equal(t, "", result)
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := Input{
		Workspace:     "/full/test/workspace",
		WorkspaceRoot: "/root",
		SessionID:     "sess-full",
		PlanFile:      "full-plan.md",
		ImportTasks:   true,
		DryRun:        true,
		Force:         true,
		Provider:      "claude",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.WorkspaceRoot, decoded.WorkspaceRoot)
	assert.Equal(t, in.SessionID, decoded.SessionID)
	assert.Equal(t, in.PlanFile, decoded.PlanFile)
	assert.Equal(t, in.ImportTasks, decoded.ImportTasks)
	assert.Equal(t, in.DryRun, decoded.DryRun)
	assert.Equal(t, in.Force, decoded.Force)
	assert.Equal(t, in.Provider, decoded.Provider)
}

func TestOutput_EmptyResults(t *testing.T) {
	output := Output{
		PlansProcessed: 0,
		PlansChanged:   0,
		TasksCreated:   0,
		Results:        []SyncResult{},
	}

	assert.Empty(t, output.Results)
	assert.Zero(t, output.PlansProcessed)
}

func TestSyncResult_WithSteps(t *testing.T) {
	result := SyncResult{
		PlanFile: "plan.md",
		Status:   "synced",
		Steps: []StepResult{
			{Title: "Step 1", Status: "created", TaskID: "task-1"},
			{Title: "Step 2", Status: "exists", TaskID: "task-2"},
			{Title: "Step 3", Status: "skipped"},
		},
		TasksCreated: 1,
		TasksSkipped: 2,
	}

	assert.Len(t, result.Steps, 3)
	assert.Equal(t, 1, result.TasksCreated)
	assert.Equal(t, 2, result.TasksSkipped)
}
