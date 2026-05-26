package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestDefaultLimit(t *testing.T) {
	assert.Equal(t, 100, defaultLimit)
}

// Tests for Input structure

func TestOutput_EmptyTurns(t *testing.T) {
	out := Output{
		SessionID: "sess-123",
		Turns:     []TurnInfo{},
		Status:    "no_turns",
	}

	assert.NotNil(t, out.Turns)
	assert.Len(t, out.Turns, 0)
}

func TestOutput_NilSession(t *testing.T) {
	out := Output{
		SessionID: "sess-123",
		Session:   nil,
		Status:    "ok",
	}

	assert.Nil(t, out.Session)

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Nil(t, decoded.Session)
}

// Tests for SessionInfo structure

func TestTurnInfo_NoError(t *testing.T) {
	turn := TurnInfo{
		TurnIndex: 1,
		Role:      "assistant",
		HasError:  false,
	}

	assert.False(t, turn.HasError)
	assert.Empty(t, turn.ErrorType)
	assert.Empty(t, turn.ErrorMessage)
}

func TestTurnInfo_WithError(t *testing.T) {
	turn := TurnInfo{
		TurnIndex:    2,
		Role:         "assistant",
		HasError:     true,
		ErrorType:    "ToolError",
		ErrorMessage: "Tool failed",
	}

	assert.True(t, turn.HasError)
	assert.Equal(t, "ToolError", turn.ErrorType)
	assert.Equal(t, "Tool failed", turn.ErrorMessage)
}

// Tests for ToolCall structure

func TestToolCall_Failed(t *testing.T) {
	tc := ToolCall{
		Name:    "Bash",
		Success: false,
	}

	assert.Equal(t, "Bash", tc.Name)
	assert.False(t, tc.Success)
}

func TestLimitHandling_Zero(t *testing.T) {
	// When limit is 0 or negative, it should default to defaultLimit
	limit := 0
	if limit <= 0 {
		limit = defaultLimit
	}
	assert.Equal(t, 100, limit)
}

func TestLimitHandling_Negative(t *testing.T) {
	limit := -5
	if limit <= 0 {
		limit = defaultLimit
	}
	assert.Equal(t, 100, limit)
}

func TestLimitHandling_Positive(t *testing.T) {
	limit := 50
	if limit <= 0 {
		limit = defaultLimit
	}
	assert.Equal(t, 50, limit)
}

// Tests for status values

func TestStatusValues(t *testing.T) {
	// Status can be "ok" or "no_turns"
	statuses := []string{"ok", "no_turns"}

	for _, status := range statuses {
		out := Output{Status: status}
		assert.NotEmpty(t, out.Status)
	}
}

// Edge case tests

func TestTurnInfo_MultipleToolCalls(t *testing.T) {
	turn := TurnInfo{
		TurnIndex: 1,
		Role:      "assistant",
		ToolCalls: []ToolCall{
			{Name: "Read", Success: true},
			{Name: "Edit", Success: true},
			{Name: "Bash", Success: false},
		},
	}

	assert.Len(t, turn.ToolCalls, 3)
	assert.True(t, turn.ToolCalls[0].Success)
	assert.True(t, turn.ToolCalls[1].Success)
	assert.False(t, turn.ToolCalls[2].Success)
}

func TestOutput_LargeTurnCount(t *testing.T) {
	turns := make([]TurnInfo, 100)
	for i := range turns {
		turns[i] = TurnInfo{TurnIndex: i, Role: "assistant"}
	}

	out := Output{
		SessionID:  "sess-123",
		Turns:      turns,
		TotalTurns: 100,
	}

	assert.Len(t, out.Turns, 100)
	assert.Equal(t, 100, out.TotalTurns)
}

func TestSessionInfo_EmptySlices(t *testing.T) {
	info := SessionInfo{
		ProjectName:  "project",
		Accomplished: []string{},
		Decisions:    []string{},
		Gotchas:      []string{},
	}

	assert.NotNil(t, info.Accomplished)
	assert.NotNil(t, info.Decisions)
	assert.NotNil(t, info.Gotchas)
	assert.Len(t, info.Accomplished, 0)
}

func TestTurnInfo_EmptyToolCalls(t *testing.T) {
	turn := TurnInfo{
		TurnIndex: 1,
		Role:      "user",
		ToolCalls: []ToolCall{},
	}

	assert.NotNil(t, turn.ToolCalls)
	assert.Len(t, turn.ToolCalls, 0)
}

func TestOutput_FullJSONRoundTrip(t *testing.T) {
	out := Output{
		SessionID: "sess-full",
		Session: &SessionInfo{
			ProjectName:  "test-project",
			GitBranch:    "main",
			MessageCount: 50,
		},
		Turns: []TurnInfo{
			{
				TurnIndex:      0,
				Role:           "user",
				ContentPreview: "Hello",
				TokensUsed:     50,
			},
			{
				TurnIndex:      1,
				Role:           "assistant",
				ContentPreview: "Hi there",
				ToolCalls:      []ToolCall{{Name: "Read", Success: true}},
				TokensUsed:     200,
			},
		},
		TotalTurns: 2,
		ErrorCount: 0,
		Status:     "ok",
		Message:    "Retrieved 2 turns for session",
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.SessionID, decoded.SessionID)
	assert.Equal(t, out.TotalTurns, decoded.TotalTurns)
	assert.NotNil(t, decoded.Session)
	assert.Equal(t, "test-project", decoded.Session.ProjectName)
	assert.Len(t, decoded.Turns, 2)
}

func TestTurnInfo_RoleValues(t *testing.T) {
	roles := []string{"user", "assistant"}

	for _, role := range roles {
		turn := TurnInfo{Role: role}
		assert.Equal(t, role, turn.Role)
	}
}
