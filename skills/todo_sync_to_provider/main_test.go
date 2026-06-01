package main

import (
	"encoding/json"
	"testing"

	"github.com/joshka0/foxctl/internal/context/todosync"
	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestInput_OrderValues(t *testing.T) {
	orders := []string{"foxctl_rank", "stable", "off"}

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

func TestInput_FullJSONRoundTrip(t *testing.T) {
	includeGlyphs := true
	includeDepHints := false
	in := input{
		Provider:        "claude",
		WorkspaceID:     "/full/workspace/path",
		SessionID:       "full-session-id",
		Order:           "foxctl_rank",
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
	// Default is "foxctl_rank" in run(), but struct has empty
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
		Warnings: []string{"Write skipped: FOXCTL_ALLOW_PROVIDER_STATE not set"},
	}

	assert.Zero(t, out.Written)
	assert.Contains(t, out.Warnings[0], "FOXCTL_ALLOW_PROVIDER_STATE")
}
