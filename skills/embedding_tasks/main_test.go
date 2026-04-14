package main

import (
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage/tasks"
	"github.com/stretchr/testify/assert"
)

// Tests for taskEmbeddingContent helper

func TestTaskEmbeddingContent_Basic(t *testing.T) {
	task := tasks.Task{
		ID:        "task-123",
		Title:     "Implement feature",
		Status:    "pending",
		CreatedAt: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	result := taskEmbeddingContent(task)

	assert.Contains(t, result, "[Jan 2026]")
	assert.Contains(t, result, "[pending]")
	assert.Contains(t, result, "Task: Implement feature")
}

func TestTaskEmbeddingContent_Completed(t *testing.T) {
	completedAt := time.Date(2026, 2, 20, 14, 0, 0, 0, time.UTC)
	task := tasks.Task{
		ID:          "task-123",
		Title:       "Fixed bug",
		Status:      "completed",
		CreatedAt:   time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		CompletedAt: &completedAt,
	}

	result := taskEmbeddingContent(task)

	// Should use CompletedAt date when available
	assert.Contains(t, result, "[Feb 2026]")
	assert.Contains(t, result, "[completed]")
}

func TestTaskEmbeddingContent_WithDescription(t *testing.T) {
	task := tasks.Task{
		ID:          "task-123",
		Title:       "Add auth",
		Description: "Implement JWT authentication",
		Status:      "in_progress",
		CreatedAt:   time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	result := taskEmbeddingContent(task)

	assert.Contains(t, result, "Description: Implement JWT authentication")
}

func TestTaskEmbeddingContent_WithDependencies(t *testing.T) {
	task := tasks.Task{
		ID:        "task-123",
		Title:     "Build feature",
		Status:    "pending",
		DependsOn: []string{"task-100", "task-101", "task-102"},
		CreatedAt: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	result := taskEmbeddingContent(task)

	assert.Contains(t, result, "Dependencies: 3 tasks")
}

func TestTaskEmbeddingContent_WithEpic(t *testing.T) {
	task := tasks.Task{
		ID:        "task-123",
		Title:     "Add button",
		Status:    "pending",
		EpicID:    "epic-auth",
		CreatedAt: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	result := taskEmbeddingContent(task)

	assert.Contains(t, result, "Epic: epic-auth")
}

func TestTaskEmbeddingContent_WithNotes(t *testing.T) {
	task := tasks.Task{
		ID:        "task-123",
		Title:     "Refactor code",
		Status:    "pending",
		Notes:     "Consider using interfaces",
		CreatedAt: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	result := taskEmbeddingContent(task)

	assert.Contains(t, result, "Notes: Consider using interfaces")
}

func TestTaskEmbeddingContent_WithGotchas(t *testing.T) {
	task := tasks.Task{
		ID:        "task-123",
		Title:     "Update API",
		Status:    "pending",
		Gotchas:   "Watch for breaking changes",
		CreatedAt: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	result := taskEmbeddingContent(task)

	assert.Contains(t, result, "Gotchas: Watch for breaking changes")
}

func TestTaskEmbeddingContent_AllFields(t *testing.T) {
	completedAt := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	task := tasks.Task{
		ID:          "task-full",
		Title:       "Full task",
		Description: "Complete implementation",
		Status:      "completed",
		DependsOn:   []string{"task-1"},
		EpicID:      "epic-1",
		Notes:       "Implementation notes",
		Gotchas:     "Edge case gotcha",
		CreatedAt:   time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		CompletedAt: &completedAt,
	}

	result := taskEmbeddingContent(task)

	assert.Contains(t, result, "[Mar 2026]")
	assert.Contains(t, result, "[completed]")
	assert.Contains(t, result, "Task: Full task")
	assert.Contains(t, result, "Description: Complete implementation")
	assert.Contains(t, result, "Dependencies: 1 tasks")
	assert.Contains(t, result, "Epic: epic-1")
	assert.Contains(t, result, "Notes: Implementation notes")
	assert.Contains(t, result, "Gotchas: Edge case gotcha")
}

func TestTaskEmbeddingContent_EmptyStatus(t *testing.T) {
	task := tasks.Task{
		ID:        "task-123",
		Title:     "New task",
		Status:    "", // Empty status
		CreatedAt: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	result := taskEmbeddingContent(task)

	// Should default to pending
	assert.Contains(t, result, "[pending]")
}

func TestTaskEmbeddingContent_EmptyTitle(t *testing.T) {
	task := tasks.Task{
		ID:          "task-123",
		Title:       "",
		Description: "Just description",
		Status:      "pending",
		CreatedAt:   time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	result := taskEmbeddingContent(task)

	// Should not contain "Task:" prefix with empty title
	assert.NotContains(t, result, "Task:")
	assert.Contains(t, result, "Description: Just description")
}

// Tests for Input structure

func TestInput_DefaultScope(t *testing.T) {
	in := Input{}
	// Scope is empty by default (requires validation in run())
	assert.Empty(t, in.Scope)
}

func TestInput_ValidScopes(t *testing.T) {
	validScopes := []string{"all", "pending", "completed", "workspace"}
	for _, scope := range validScopes {
		in := Input{Scope: scope}
		assert.Equal(t, scope, in.Scope)
	}
}

func TestInput_DefaultBatchSize(t *testing.T) {
	in := Input{}
	// Default applied in run() when <= 0
	if in.BatchSize <= 0 {
		in.BatchSize = defaultBatchMax
	}
	assert.Equal(t, 10, in.BatchSize)
}

func TestInput_AllFields(t *testing.T) {
	in := Input{
		Scope:       "workspace",
		WorkspaceID: "ws-123",
		TaskID:      "task-456",
		BatchSize:   20,
		ProcessAll:  true,
		DryRun:      true,
	}

	assert.Equal(t, "workspace", in.Scope)
	assert.Equal(t, "ws-123", in.WorkspaceID)
	assert.Equal(t, "task-456", in.TaskID)
	assert.Equal(t, 20, in.BatchSize)
	assert.True(t, in.ProcessAll)
	assert.True(t, in.DryRun)
}

// Tests for Output structure

func TestOutput_SuccessfulRun(t *testing.T) {
	output := Output{
		Scope:      "all",
		TasksFound: 15,
		Embedded:   12,
		Skipped:    2,
		Errors:     1,
		BatchCount: 2,
		DurationMs: 2500,
	}

	assert.Equal(t, "all", output.Scope)
	assert.Equal(t, 15, output.TasksFound)
	assert.Equal(t, 12, output.Embedded)
	assert.Equal(t, 2, output.Skipped)
	assert.Equal(t, 1, output.Errors)
	assert.Equal(t, 2, output.BatchCount)
}

func TestOutput_WithTaskResults(t *testing.T) {
	output := Output{
		Tasks: []TaskResult{
			{TaskID: "task-1", Title: "First", Status: "embedded", Dimensions: 1024},
			{TaskID: "task-2", Title: "Second", Status: "skipped", Message: "No content"},
		},
	}

	assert.Len(t, output.Tasks, 2)
	assert.Equal(t, "embedded", output.Tasks[0].Status)
	assert.Equal(t, "skipped", output.Tasks[1].Status)
}

func TestOutput_WithErrors(t *testing.T) {
	output := Output{
		Errors: 2,
		ErrorDetails: []string{
			"task-1: API error",
			"task-2: save failed",
		},
	}

	assert.Equal(t, 2, output.Errors)
	assert.Len(t, output.ErrorDetails, 2)
}

// Tests for TaskResult structure

func TestTaskResult_Embedded(t *testing.T) {
	result := TaskResult{
		TaskID:     "task-123",
		Title:      "Test task",
		Status:     "embedded",
		Dimensions: 768,
	}

	assert.Equal(t, "task-123", result.TaskID)
	assert.Equal(t, "embedded", result.Status)
	assert.Equal(t, 768, result.Dimensions)
}

func TestTaskResult_DryRun(t *testing.T) {
	result := TaskResult{
		TaskID:  "task-123",
		Title:   "Test task",
		Status:  "dry_run",
		Message: "Would embed 500 chars",
	}

	assert.Equal(t, "dry_run", result.Status)
	assert.Contains(t, result.Message, "Would embed")
}

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "embedding/tasks", command)
}

func TestTaskType(t *testing.T) {
	assert.Equal(t, "task_embedding", taskType)
}

func TestDefaultBatchMax(t *testing.T) {
	assert.Equal(t, 10, defaultBatchMax)
}

func TestGeminiModel(t *testing.T) {
	assert.Equal(t, "gemini-embedding-001", geminiModel)
}
