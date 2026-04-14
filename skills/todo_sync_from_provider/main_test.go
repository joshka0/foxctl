package main

import (
	"encoding/json"
	"testing"

	"github.com/joshka0/foxctl/internal/context/todosync"
	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "todo/sync_from_provider", command)
}

// Tests for input structure

func TestInput_AllFields(t *testing.T) {
	in := input{
		Provider:    "claude",
		WorkspaceID: "/home/user/project",
		SessionID:   "session-123",
		Todos: []todosync.ClaudeTodo{
			{Content: "Task 1", Status: "pending"},
		},
		DryRun: true,
	}

	assert.Equal(t, "claude", in.Provider)
	assert.Equal(t, "/home/user/project", in.WorkspaceID)
	assert.Equal(t, "session-123", in.SessionID)
	assert.Len(t, in.Todos, 1)
	assert.True(t, in.DryRun)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := input{
		Provider:    "claude",
		WorkspaceID: "/path/to/workspace",
		SessionID:   "test-session",
		DryRun:      false,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Provider, decoded.Provider)
	assert.Equal(t, in.WorkspaceID, decoded.WorkspaceID)
	assert.Equal(t, in.SessionID, decoded.SessionID)
	assert.Equal(t, in.DryRun, decoded.DryRun)
}

func TestInput_EmptyFields(t *testing.T) {
	in := input{}

	assert.Empty(t, in.Provider)
	assert.Empty(t, in.WorkspaceID)
	assert.Empty(t, in.SessionID)
	assert.Nil(t, in.Todos)
	assert.False(t, in.DryRun)
}

func TestInput_JSONFieldNames(t *testing.T) {
	in := input{
		Provider:    "claude",
		WorkspaceID: "ws",
		SessionID:   "sid",
		Todos:       []todosync.ClaudeTodo{{Content: "t"}},
		DryRun:      true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "provider")
	assert.Contains(t, jsonStr, "workspace_id")
	assert.Contains(t, jsonStr, "session_id")
	assert.Contains(t, jsonStr, "todos")
	assert.Contains(t, jsonStr, "dry_run")
}

func TestInput_ProviderValue(t *testing.T) {
	// Currently only "claude" is supported
	in := input{Provider: "claude"}
	assert.Equal(t, "claude", in.Provider)
}

func TestInput_WithTodos(t *testing.T) {
	in := input{
		Provider:    "claude",
		WorkspaceID: "/workspace",
		Todos: []todosync.ClaudeTodo{
			{Content: "Task 1", Status: "pending"},
			{Content: "Task 2", Status: "in_progress"},
			{Content: "Task 3", Status: "completed"},
		},
	}

	assert.Len(t, in.Todos, 3)
	assert.Equal(t, "pending", in.Todos[0].Status)
	assert.Equal(t, "in_progress", in.Todos[1].Status)
	assert.Equal(t, "completed", in.Todos[2].Status)
}

// Tests for output structure

func TestOutput_AllFields(t *testing.T) {
	out := output{
		Created:   5,
		Updated:   3,
		Completed: 2,
		Removed:   1,
		Mapped:    10,
		Unmapped:  2,
		DepsAdded: 4,
		Warnings:  []string{"warning1", "warning2"},
		DryRun:    true,
	}

	assert.Equal(t, 5, out.Created)
	assert.Equal(t, 3, out.Updated)
	assert.Equal(t, 2, out.Completed)
	assert.Equal(t, 1, out.Removed)
	assert.Equal(t, 10, out.Mapped)
	assert.Equal(t, 2, out.Unmapped)
	assert.Equal(t, 4, out.DepsAdded)
	assert.Len(t, out.Warnings, 2)
	assert.True(t, out.DryRun)
}

func TestOutput_JSONSerialization(t *testing.T) {
	out := output{
		Created:   3,
		Updated:   2,
		Completed: 1,
		Mapped:    6,
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.Created, decoded.Created)
	assert.Equal(t, out.Updated, decoded.Updated)
	assert.Equal(t, out.Completed, decoded.Completed)
	assert.Equal(t, out.Mapped, decoded.Mapped)
}

func TestOutput_EmptyFields(t *testing.T) {
	out := output{}

	assert.Zero(t, out.Created)
	assert.Zero(t, out.Updated)
	assert.Zero(t, out.Completed)
	assert.Zero(t, out.Removed)
	assert.Zero(t, out.Mapped)
	assert.Zero(t, out.Unmapped)
	assert.Zero(t, out.DepsAdded)
	assert.Nil(t, out.Warnings)
	assert.False(t, out.DryRun)
}

func TestOutput_JSONFieldNames(t *testing.T) {
	out := output{
		Created:   1,
		Updated:   1,
		Completed: 1,
		Removed:   1,
		Mapped:    1,
		Unmapped:  1,
		DepsAdded: 1,
		Warnings:  []string{"w"},
		DryRun:    true,
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "created")
	assert.Contains(t, jsonStr, "updated")
	assert.Contains(t, jsonStr, "completed")
	assert.Contains(t, jsonStr, "removed")
	assert.Contains(t, jsonStr, "mapped")
	assert.Contains(t, jsonStr, "unmapped")
	assert.Contains(t, jsonStr, "deps_added")
	assert.Contains(t, jsonStr, "warnings")
	assert.Contains(t, jsonStr, "dry_run")
}

func TestOutput_OmitEmptyWarnings(t *testing.T) {
	out := output{
		Created: 1,
		Mapped:  1,
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "warnings")
}

func TestOutput_OmitEmptyDryRun(t *testing.T) {
	out := output{
		Created: 1,
		DryRun:  false,
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "dry_run")
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		Provider:    "claude",
		WorkspaceID: "/full/workspace/path",
		SessionID:   "full-session-id",
		Todos: []todosync.ClaudeTodo{
			{Content: "Full task", Status: "pending"},
		},
		DryRun: true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Provider, decoded.Provider)
	assert.Equal(t, in.WorkspaceID, decoded.WorkspaceID)
	assert.Equal(t, in.SessionID, decoded.SessionID)
	assert.Len(t, decoded.Todos, 1)
	assert.Equal(t, in.DryRun, decoded.DryRun)
}

func TestOutput_FullJSONRoundTrip(t *testing.T) {
	out := output{
		Created:   10,
		Updated:   5,
		Completed: 3,
		Removed:   2,
		Mapped:    15,
		Unmapped:  5,
		DepsAdded: 8,
		Warnings:  []string{"warn1", "warn2", "warn3"},
		DryRun:    true,
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.Created, decoded.Created)
	assert.Equal(t, out.Updated, decoded.Updated)
	assert.Equal(t, out.Completed, decoded.Completed)
	assert.Equal(t, out.Removed, decoded.Removed)
	assert.Equal(t, out.Mapped, decoded.Mapped)
	assert.Equal(t, out.Unmapped, decoded.Unmapped)
	assert.Equal(t, out.DepsAdded, decoded.DepsAdded)
	assert.Equal(t, out.Warnings, decoded.Warnings)
	assert.Equal(t, out.DryRun, decoded.DryRun)
}

func TestInput_EmptyTodos(t *testing.T) {
	in := input{
		Provider:    "claude",
		WorkspaceID: "/workspace",
		Todos:       []todosync.ClaudeTodo{},
	}

	assert.Empty(t, in.Todos)
	assert.NotNil(t, in.Todos)
}

func TestInput_NoSessionID(t *testing.T) {
	// SessionID is optional - will auto-detect if empty
	in := input{
		Provider:    "claude",
		WorkspaceID: "/workspace",
	}

	assert.Empty(t, in.SessionID)
}

func TestOutput_NoChanges(t *testing.T) {
	out := output{
		Created:   0,
		Updated:   0,
		Completed: 0,
		Removed:   0,
		Mapped:    5,
		Unmapped:  0,
	}

	// All zero values for changes
	assert.Zero(t, out.Created)
	assert.Zero(t, out.Updated)
	assert.Zero(t, out.Completed)
	assert.Zero(t, out.Removed)
}

func TestOutput_WithWarnings(t *testing.T) {
	out := output{
		Created:  1,
		Warnings: []string{"Could not map task X", "Duplicate task found"},
	}

	assert.Len(t, out.Warnings, 2)
	assert.Contains(t, out.Warnings[0], "map task")
}
