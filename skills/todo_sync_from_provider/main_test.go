package main

import (
	"encoding/json"
	"testing"

	"github.com/joshka0/foxctl/internal/context/todosync"
	"github.com/stretchr/testify/assert"
)

// Tests for constants

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
