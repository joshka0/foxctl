package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestInput_ProviderValues(t *testing.T) {
	providers := []string{"claude", "opencode"}

	for _, provider := range providers {
		in := Input{Provider: provider}
		assert.Equal(t, provider, in.Provider)
	}
}

// Tests for SyncResult structure

func TestSyncResult_StatusValues(t *testing.T) {
	statuses := []string{"synced", "unchanged", "created", "error"}

	for _, status := range statuses {
		result := SyncResult{Status: status}
		assert.Equal(t, status, result.Status)
	}
}

func TestStepResult_StatusValues(t *testing.T) {
	statuses := []string{"created", "exists", "skipped", "would_create", "error"}

	for _, status := range statuses {
		step := StepResult{Status: status}
		assert.Equal(t, status, step.Status)
	}
}

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
