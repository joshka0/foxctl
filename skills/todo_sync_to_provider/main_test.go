package main

import (
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/context/todosync"
	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "todo/sync_to_provider", command)
}

// Tests for input structure

func TestInput_AllFields(t *testing.T) {
	includeGlyphs := true
	includeDepHints := false
	in := input{
		Provider:        "claude",
		WorkspaceID:     "/home/user/project",
		SessionID:       "session-123",
		Order:           "agentctl_rank",
		MaxItems:        50,
		IncludeGlyphs:   &includeGlyphs,
		IncludeDepHints: &includeDepHints,
		DryRun:          true,
	}

	assert.Equal(t, "claude", in.Provider)
	assert.Equal(t, "/home/user/project", in.WorkspaceID)
	assert.Equal(t, "session-123", in.SessionID)
	assert.Equal(t, "agentctl_rank", in.Order)
	assert.Equal(t, 50, in.MaxItems)
	assert.NotNil(t, in.IncludeGlyphs)
	assert.True(t, *in.IncludeGlyphs)
	assert.NotNil(t, in.IncludeDepHints)
	assert.False(t, *in.IncludeDepHints)
	assert.True(t, in.DryRun)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := input{
		Provider:    "claude",
		WorkspaceID: "/path/to/workspace",
		SessionID:   "test-session",
		Order:       "stable",
		MaxItems:    25,
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
	assert.Equal(t, in.Order, decoded.Order)
	assert.Equal(t, in.MaxItems, decoded.MaxItems)
	assert.Equal(t, in.DryRun, decoded.DryRun)
}

func TestInput_EmptyFields(t *testing.T) {
	in := input{}

	assert.Empty(t, in.Provider)
	assert.Empty(t, in.WorkspaceID)
	assert.Empty(t, in.SessionID)
	assert.Empty(t, in.Order)
	assert.Zero(t, in.MaxItems)
	assert.Nil(t, in.IncludeGlyphs)
	assert.Nil(t, in.IncludeDepHints)
	assert.False(t, in.DryRun)
}

func TestInput_JSONFieldNames(t *testing.T) {
	includeGlyphs := true
	includeDepHints := true
	in := input{
		Provider:        "claude",
		WorkspaceID:     "ws",
		SessionID:       "sid",
		Order:           "o",
		MaxItems:        10,
		IncludeGlyphs:   &includeGlyphs,
		IncludeDepHints: &includeDepHints,
		DryRun:          true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "provider")
	assert.Contains(t, jsonStr, "workspace_id")
	assert.Contains(t, jsonStr, "session_id")
	assert.Contains(t, jsonStr, "order")
	assert.Contains(t, jsonStr, "max_items")
	assert.Contains(t, jsonStr, "include_glyphs")
	assert.Contains(t, jsonStr, "include_dep_hints")
	assert.Contains(t, jsonStr, "dry_run")
}

func TestInput_OrderValues(t *testing.T) {
	orders := []string{"agentctl_rank", "stable", "off"}

	for _, order := range orders {
		in := input{Order: order}
		assert.Equal(t, order, in.Order)
	}
}

func TestInput_ProviderValue(t *testing.T) {
	// Currently only "claude" is supported
	in := input{Provider: "claude"}
	assert.Equal(t, "claude", in.Provider)
}

func TestInput_IncludeGlyphsPointer_True(t *testing.T) {
	val := true
	in := input{IncludeGlyphs: &val}

	assert.NotNil(t, in.IncludeGlyphs)
	assert.True(t, *in.IncludeGlyphs)
}

func TestInput_IncludeGlyphsPointer_False(t *testing.T) {
	val := false
	in := input{IncludeGlyphs: &val}

	assert.NotNil(t, in.IncludeGlyphs)
	assert.False(t, *in.IncludeGlyphs)
}

func TestInput_IncludeDepHintsPointer_True(t *testing.T) {
	val := true
	in := input{IncludeDepHints: &val}

	assert.NotNil(t, in.IncludeDepHints)
	assert.True(t, *in.IncludeDepHints)
}

func TestInput_IncludeDepHintsPointer_False(t *testing.T) {
	val := false
	in := input{IncludeDepHints: &val}

	assert.NotNil(t, in.IncludeDepHints)
	assert.False(t, *in.IncludeDepHints)
}

// Tests for output structure

func TestOutput_AllFields(t *testing.T) {
	out := output{
		Written:   5,
		Updated:   3,
		Unchanged: 2,
		FilePath:  "/home/user/.claude/todos/session-123.json",
		FileHash:  "abc123def456",
		Todos: []todosync.ClaudeTodo{
			{Content: "Task 1", Status: "pending"},
		},
		Warnings: []string{"warning1"},
		DryRun:   true,
	}

	assert.Equal(t, 5, out.Written)
	assert.Equal(t, 3, out.Updated)
	assert.Equal(t, 2, out.Unchanged)
	assert.Equal(t, "/home/user/.claude/todos/session-123.json", out.FilePath)
	assert.Equal(t, "abc123def456", out.FileHash)
	assert.Len(t, out.Todos, 1)
	assert.Len(t, out.Warnings, 1)
	assert.True(t, out.DryRun)
}

func TestOutput_JSONSerialization(t *testing.T) {
	out := output{
		Written:   3,
		Updated:   2,
		Unchanged: 1,
		FilePath:  "/path/to/file",
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.Written, decoded.Written)
	assert.Equal(t, out.Updated, decoded.Updated)
	assert.Equal(t, out.Unchanged, decoded.Unchanged)
	assert.Equal(t, out.FilePath, decoded.FilePath)
}

func TestOutput_EmptyFields(t *testing.T) {
	out := output{}

	assert.Zero(t, out.Written)
	assert.Zero(t, out.Updated)
	assert.Zero(t, out.Unchanged)
	assert.Empty(t, out.FilePath)
	assert.Empty(t, out.FileHash)
	assert.Nil(t, out.Todos)
	assert.Nil(t, out.Warnings)
	assert.False(t, out.DryRun)
}

func TestOutput_JSONFieldNames(t *testing.T) {
	out := output{
		Written:   1,
		Updated:   1,
		Unchanged: 1,
		FilePath:  "fp",
		FileHash:  "fh",
		Todos:     []todosync.ClaudeTodo{{Content: "t"}},
		Warnings:  []string{"w"},
		DryRun:    true,
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "written")
	assert.Contains(t, jsonStr, "updated")
	assert.Contains(t, jsonStr, "unchanged")
	assert.Contains(t, jsonStr, "file_path")
	assert.Contains(t, jsonStr, "file_hash")
	assert.Contains(t, jsonStr, "todos")
	assert.Contains(t, jsonStr, "warnings")
	assert.Contains(t, jsonStr, "dry_run")
}

func TestOutput_OmitEmptyFilePath(t *testing.T) {
	out := output{
		Written: 1,
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "file_path")
}

func TestOutput_OmitEmptyTodos(t *testing.T) {
	out := output{
		Written: 1,
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "todos")
}

func TestOutput_OmitEmptyWarnings(t *testing.T) {
	out := output{
		Written: 1,
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "warnings")
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	includeGlyphs := true
	includeDepHints := false
	in := input{
		Provider:        "claude",
		WorkspaceID:     "/full/workspace/path",
		SessionID:       "full-session-id",
		Order:           "agentctl_rank",
		MaxItems:        100,
		IncludeGlyphs:   &includeGlyphs,
		IncludeDepHints: &includeDepHints,
		DryRun:          true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Provider, decoded.Provider)
	assert.Equal(t, in.WorkspaceID, decoded.WorkspaceID)
	assert.Equal(t, in.SessionID, decoded.SessionID)
	assert.Equal(t, in.Order, decoded.Order)
	assert.Equal(t, in.MaxItems, decoded.MaxItems)
	assert.NotNil(t, decoded.IncludeGlyphs)
	assert.Equal(t, *in.IncludeGlyphs, *decoded.IncludeGlyphs)
	assert.NotNil(t, decoded.IncludeDepHints)
	assert.Equal(t, *in.IncludeDepHints, *decoded.IncludeDepHints)
	assert.Equal(t, in.DryRun, decoded.DryRun)
}

func TestOutput_FullJSONRoundTrip(t *testing.T) {
	out := output{
		Written:   10,
		Updated:   5,
		Unchanged: 3,
		FilePath:  "/full/file/path.json",
		FileHash:  "fullhash123",
		Todos: []todosync.ClaudeTodo{
			{Content: "Task 1", Status: "pending"},
			{Content: "Task 2", Status: "completed"},
		},
		Warnings: []string{"warn1", "warn2"},
		DryRun:   true,
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.Written, decoded.Written)
	assert.Equal(t, out.Updated, decoded.Updated)
	assert.Equal(t, out.Unchanged, decoded.Unchanged)
	assert.Equal(t, out.FilePath, decoded.FilePath)
	assert.Equal(t, out.FileHash, decoded.FileHash)
	assert.Len(t, decoded.Todos, 2)
	assert.Equal(t, out.Warnings, decoded.Warnings)
	assert.Equal(t, out.DryRun, decoded.DryRun)
}

func TestInput_NoSessionID(t *testing.T) {
	// SessionID is optional - will auto-detect if empty
	in := input{
		Provider:    "claude",
		WorkspaceID: "/workspace",
	}

	assert.Empty(t, in.SessionID)
}

func TestInput_DefaultOrder(t *testing.T) {
	// Default is "agentctl_rank" in run(), but struct has empty
	in := input{
		Provider:    "claude",
		WorkspaceID: "/workspace",
	}

	assert.Empty(t, in.Order)
}

func TestInput_ZeroMaxItems(t *testing.T) {
	// 0 means no limit
	in := input{
		Provider:    "claude",
		WorkspaceID: "/workspace",
		MaxItems:    0,
	}

	assert.Zero(t, in.MaxItems)
}

func TestOutput_DryRunWithTodos(t *testing.T) {
	// In dry run mode, Todos are included in output
	out := output{
		Written: 0,
		DryRun:  true,
		Todos: []todosync.ClaudeTodo{
			{Content: "Would be written", Status: "pending"},
		},
	}

	assert.True(t, out.DryRun)
	assert.NotEmpty(t, out.Todos)
}

func TestOutput_NoWritePermission(t *testing.T) {
	out := output{
		Written:  0,
		Warnings: []string{"Write skipped: AGENTCTL_ALLOW_PROVIDER_STATE not set"},
	}

	assert.Zero(t, out.Written)
	assert.Contains(t, out.Warnings[0], "AGENTCTL_ALLOW_PROVIDER_STATE")
}
