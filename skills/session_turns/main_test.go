package main

import (
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "session/turns", command)
}

func TestDefaultLimit(t *testing.T) {
	assert.Equal(t, 50, defaultLimit)
}

// Tests for Input structure

func TestInput_AllFields(t *testing.T) {
	in := Input{
		Query:       "search query",
		ErrorType:   "compile_error",
		ToolPattern: "Bash",
		Role:        "assistant",
		ErrorsOnly:  true,
		Limit:       100,
	}

	assert.Equal(t, "search query", in.Query)
	assert.Equal(t, "compile_error", in.ErrorType)
	assert.Equal(t, "Bash", in.ToolPattern)
	assert.Equal(t, "assistant", in.Role)
	assert.True(t, in.ErrorsOnly)
	assert.Equal(t, 100, in.Limit)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := Input{
		Query:      "test query",
		ErrorsOnly: true,
		Limit:      25,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Query, decoded.Query)
	assert.Equal(t, in.ErrorsOnly, decoded.ErrorsOnly)
	assert.Equal(t, in.Limit, decoded.Limit)
}

func TestInput_EmptyFields(t *testing.T) {
	in := Input{}

	assert.Empty(t, in.Query)
	assert.Empty(t, in.ErrorType)
	assert.Empty(t, in.ToolPattern)
	assert.Empty(t, in.Role)
	assert.False(t, in.ErrorsOnly)
	assert.Zero(t, in.Limit)
}

func TestInput_RoleValues(t *testing.T) {
	roles := []string{"user", "assistant"}

	for _, role := range roles {
		in := Input{Role: role}
		assert.Equal(t, role, in.Role)
	}
}

// Tests for Output structure

func TestOutput_AllFields(t *testing.T) {
	output := Output{
		Query: "test query",
		Turns: []TurnResult{
			{SessionID: "sess-1", TurnIndex: 1},
		},
		TotalFound: 1,
		Status:     "ok",
		Message:    "Found 1 matching turns",
	}

	assert.Equal(t, "test query", output.Query)
	assert.Len(t, output.Turns, 1)
	assert.Equal(t, 1, output.TotalFound)
	assert.Equal(t, "ok", output.Status)
	assert.Equal(t, "Found 1 matching turns", output.Message)
}

func TestOutput_JSONSerialization(t *testing.T) {
	output := Output{
		Query:      "search",
		TotalFound: 5,
		Status:     "ok",
	}

	data, err := json.Marshal(output)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, output.Query, decoded.Query)
	assert.Equal(t, output.TotalFound, decoded.TotalFound)
	assert.Equal(t, output.Status, decoded.Status)
}

func TestOutput_StatusValues(t *testing.T) {
	statuses := []string{"ok", "no_matches"}

	for _, status := range statuses {
		output := Output{Status: status}
		assert.Equal(t, status, output.Status)
	}
}

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

func TestTurnResult_AllFields(t *testing.T) {
	result := TurnResult{
		SessionID:      "sess-123",
		ProjectName:    "my-project",
		TurnIndex:      5,
		Role:           "assistant",
		ContentPreview: "Running tests...",
		ToolCalls: []ToolCall{
			{Name: "Bash", Success: true},
		},
		FilesTouched: []string{"main.go", "test.go"},
		HasError:     true,
		ErrorType:    "test_failure",
		ErrorMessage: "Test failed: expected 1, got 2",
		Resolution:   "Fixed the assertion",
		TokensUsed:   500,
		Timestamp:    "2026-01-17T10:00:00Z",
	}

	assert.Equal(t, "sess-123", result.SessionID)
	assert.Equal(t, "my-project", result.ProjectName)
	assert.Equal(t, 5, result.TurnIndex)
	assert.Equal(t, "assistant", result.Role)
	assert.Equal(t, "Running tests...", result.ContentPreview)
	assert.Len(t, result.ToolCalls, 1)
	assert.Len(t, result.FilesTouched, 2)
	assert.True(t, result.HasError)
	assert.Equal(t, "test_failure", result.ErrorType)
	assert.Equal(t, "Test failed: expected 1, got 2", result.ErrorMessage)
	assert.Equal(t, "Fixed the assertion", result.Resolution)
	assert.Equal(t, 500, result.TokensUsed)
	assert.Equal(t, "2026-01-17T10:00:00Z", result.Timestamp)
}

func TestTurnResult_JSONSerialization(t *testing.T) {
	result := TurnResult{
		SessionID: "sess-test",
		TurnIndex: 10,
		Role:      "user",
		HasError:  false,
	}

	data, err := json.Marshal(result)
	assert.NoError(t, err)

	var decoded TurnResult
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, result.SessionID, decoded.SessionID)
	assert.Equal(t, result.TurnIndex, decoded.TurnIndex)
	assert.Equal(t, result.Role, decoded.Role)
	assert.Equal(t, result.HasError, decoded.HasError)
}

func TestTurnResult_EmptyFields(t *testing.T) {
	result := TurnResult{}

	assert.Empty(t, result.SessionID)
	assert.Empty(t, result.ProjectName)
	assert.Zero(t, result.TurnIndex)
	assert.Empty(t, result.Role)
	assert.Empty(t, result.ContentPreview)
	assert.Nil(t, result.ToolCalls)
	assert.Nil(t, result.FilesTouched)
	assert.False(t, result.HasError)
	assert.Empty(t, result.ErrorType)
	assert.Empty(t, result.ErrorMessage)
	assert.Empty(t, result.Resolution)
	assert.Zero(t, result.TokensUsed)
	assert.Empty(t, result.Timestamp)
}

// Tests for ToolCall structure

func TestToolCall_AllFields(t *testing.T) {
	tc := ToolCall{
		Name:    "Bash",
		Success: true,
	}

	assert.Equal(t, "Bash", tc.Name)
	assert.True(t, tc.Success)
}

func TestToolCall_JSONSerialization(t *testing.T) {
	tc := ToolCall{
		Name:    "Read",
		Success: false,
	}

	data, err := json.Marshal(tc)
	assert.NoError(t, err)

	var decoded ToolCall
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, tc.Name, decoded.Name)
	assert.Equal(t, tc.Success, decoded.Success)
}

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

func TestInput_JSONFieldNames(t *testing.T) {
	in := Input{
		Query:       "q",
		ErrorType:   "e",
		ToolPattern: "t",
		Role:        "r",
		ErrorsOnly:  true,
		Limit:       10,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "query")
	assert.Contains(t, jsonStr, "error_type")
	assert.Contains(t, jsonStr, "tool_pattern")
	assert.Contains(t, jsonStr, "role")
	assert.Contains(t, jsonStr, "errors_only")
	assert.Contains(t, jsonStr, "limit")
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
