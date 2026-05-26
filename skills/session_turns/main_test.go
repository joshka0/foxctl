package main

import (
	"encoding/json"
	"testing"

	"github.com/joshka0/foxctl/internal/storage/sessions"
	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestDefaultLimit(t *testing.T) {
	assert.Equal(t, 50, defaultLimit)
}

// Tests for Input structure

func TestInput_RoleValues(t *testing.T) {
	roles := []string{"user", "assistant"}

	for _, role := range roles {
		in := Input{Role: role}
		assert.Equal(t, role, in.Role)
	}
}

// Tests for Output structure

func TestOutput_EmptyTurns(t *testing.T) {
	output := Output{
		Turns:   []TurnResult{},
		Status:  "no_matches",
		Message: "No turns matched the specified filters",
	}

	assert.Empty(t, output.Turns)
	assert.Equal(t, "no_matches", output.Status)
}

// Tests for TurnResult structure

func TestToolCall_ToolNames(t *testing.T) {
	tools := []string{"Bash", "Read", "Write", "Edit", "Grep", "Glob"}

	for _, tool := range tools {
		tc := ToolCall{Name: tool}
		assert.Equal(t, tool, tc.Name)
	}
}

// Tests for matchesToolPattern helper

func TestMatchesToolPattern_Empty(t *testing.T) {
	result := matchesToolPattern(nil, "Bash")
	assert.False(t, result)
}

func TestMatchesToolPattern_NoMatch(t *testing.T) {
	toolCalls := []sessions.ToolCall{
		{Name: "Read"},
		{Name: "Write"},
	}
	result := matchesToolPattern(toolCalls, "Bash")
	assert.False(t, result)
}

func TestMatchesToolPattern_ExactMatch(t *testing.T) {
	toolCalls := []sessions.ToolCall{
		{Name: "Bash"},
	}
	result := matchesToolPattern(toolCalls, "Bash")
	assert.True(t, result)
}

func TestMatchesToolPattern_PartialMatch(t *testing.T) {
	toolCalls := []sessions.ToolCall{
		{Name: "BashCommand"},
	}
	result := matchesToolPattern(toolCalls, "Bash")
	assert.True(t, result)
}

func TestMatchesToolPattern_CaseInsensitive(t *testing.T) {
	toolCalls := []sessions.ToolCall{
		{Name: "BASH"},
	}
	result := matchesToolPattern(toolCalls, "bash")
	assert.True(t, result)
}

func TestMatchesToolPattern_MultipleTools(t *testing.T) {
	toolCalls := []sessions.ToolCall{
		{Name: "Read"},
		{Name: "Bash"},
		{Name: "Write"},
	}
	result := matchesToolPattern(toolCalls, "bash")
	assert.True(t, result)
}

func TestMatchesToolPattern_EmptyPattern(t *testing.T) {
	toolCalls := []sessions.ToolCall{
		{Name: "Bash"},
	}
	result := matchesToolPattern(toolCalls, "")
	// Empty pattern matches any substring
	assert.True(t, result)
}

// Tests for limit default logic

func TestInput_LimitDefault(t *testing.T) {
	in := Input{}

	limit := in.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	assert.Equal(t, 50, limit)
}

func TestInput_LimitPositive(t *testing.T) {
	in := Input{Limit: 100}

	limit := in.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	assert.Equal(t, 100, limit)
}

func TestInput_LimitNegative(t *testing.T) {
	in := Input{Limit: -5}

	limit := in.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	assert.Equal(t, 50, limit)
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := Input{
		Query:       "full test query",
		ErrorType:   "runtime_error",
		ToolPattern: "Edit",
		Role:        "assistant",
		ErrorsOnly:  true,
		Limit:       200,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Query, decoded.Query)
	assert.Equal(t, in.ErrorType, decoded.ErrorType)
	assert.Equal(t, in.ToolPattern, decoded.ToolPattern)
	assert.Equal(t, in.Role, decoded.Role)
	assert.Equal(t, in.ErrorsOnly, decoded.ErrorsOnly)
	assert.Equal(t, in.Limit, decoded.Limit)
}

func TestTurnResult_WithToolCalls(t *testing.T) {
	result := TurnResult{
		SessionID: "sess-multi",
		TurnIndex: 3,
		ToolCalls: []ToolCall{
			{Name: "Bash", Success: true},
			{Name: "Read", Success: true},
			{Name: "Edit", Success: false},
		},
	}

	assert.Len(t, result.ToolCalls, 3)
	assert.Equal(t, "Bash", result.ToolCalls[0].Name)
	assert.True(t, result.ToolCalls[0].Success)
	assert.Equal(t, "Edit", result.ToolCalls[2].Name)
	assert.False(t, result.ToolCalls[2].Success)
}

func TestOutput_MultipleTurns(t *testing.T) {
	output := Output{
		Query: "test",
		Turns: []TurnResult{
			{SessionID: "sess-1", TurnIndex: 1, Role: "user"},
			{SessionID: "sess-1", TurnIndex: 2, Role: "assistant"},
			{SessionID: "sess-2", TurnIndex: 1, Role: "user"},
		},
		TotalFound: 3,
		Status:     "ok",
	}

	assert.Len(t, output.Turns, 3)
	assert.Equal(t, 3, output.TotalFound)
}

func TestTurnResult_ErrorFields(t *testing.T) {
	result := TurnResult{
		SessionID:    "sess-err",
		TurnIndex:    1,
		HasError:     true,
		ErrorType:    "syntax_error",
		ErrorMessage: "unexpected token",
		Resolution:   "added missing semicolon",
	}

	assert.True(t, result.HasError)
	assert.Equal(t, "syntax_error", result.ErrorType)
	assert.Equal(t, "unexpected token", result.ErrorMessage)
	assert.Equal(t, "added missing semicolon", result.Resolution)
}
