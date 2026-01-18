package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "session/expand", command)
}

func TestDefaultLimit(t *testing.T) {
	assert.Equal(t, 100, defaultLimit)
}

// Tests for Input structure

func TestInput_AllFields(t *testing.T) {
	in := Input{
		SessionID:  "sess-123",
		ErrorsOnly: true,
		Limit:      50,
	}

	assert.Equal(t, "sess-123", in.SessionID)
	assert.True(t, in.ErrorsOnly)
	assert.Equal(t, 50, in.Limit)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := Input{
		SessionID:  "sess-456",
		ErrorsOnly: false,
		Limit:      25,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.SessionID, decoded.SessionID)
	assert.Equal(t, in.ErrorsOnly, decoded.ErrorsOnly)
	assert.Equal(t, in.Limit, decoded.Limit)
}

func TestInput_EmptyFields(t *testing.T) {
	in := Input{}

	assert.Empty(t, in.SessionID)
	assert.False(t, in.ErrorsOnly)
	assert.Zero(t, in.Limit)
}

func TestInput_JSONOmitEmpty(t *testing.T) {
	in := Input{
		SessionID: "sess-123",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	// errors_only should be omitted when false (default)
	assert.NotContains(t, string(data), "errors_only")
	// limit should be omitted when 0
	assert.NotContains(t, string(data), "limit")
}

// Tests for Output structure

func TestOutput_AllFields(t *testing.T) {
	sessionInfo := &SessionInfo{ProjectName: "test-project"}
	turns := []TurnInfo{{TurnIndex: 0, Role: "user"}}

	out := Output{
		SessionID:  "sess-123",
		Session:    sessionInfo,
		Turns:      turns,
		TotalTurns: 1,
		ErrorCount: 0,
		Status:     "ok",
		Message:    "Retrieved 1 turns for session",
	}

	assert.Equal(t, "sess-123", out.SessionID)
	assert.NotNil(t, out.Session)
	assert.Len(t, out.Turns, 1)
	assert.Equal(t, 1, out.TotalTurns)
	assert.Equal(t, 0, out.ErrorCount)
	assert.Equal(t, "ok", out.Status)
	assert.Equal(t, "Retrieved 1 turns for session", out.Message)
}

func TestOutput_JSONSerialization(t *testing.T) {
	out := Output{
		SessionID:  "sess-test",
		TotalTurns: 5,
		ErrorCount: 1,
		Status:     "ok",
		Message:    "test",
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.SessionID, decoded.SessionID)
	assert.Equal(t, out.TotalTurns, decoded.TotalTurns)
	assert.Equal(t, out.ErrorCount, decoded.ErrorCount)
	assert.Equal(t, out.Status, decoded.Status)
}

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

func TestSessionInfo_AllFields(t *testing.T) {
	info := SessionInfo{
		ProjectName:  "my-project",
		GitBranch:    "feature/test",
		Summary:      "Test session summary",
		Accomplished: []string{"task1", "task2"},
		Decisions:    []string{"decision1"},
		Gotchas:      []string{"gotcha1", "gotcha2"},
		StartedAt:    "2026-01-15T10:00:00Z",
		EndedAt:      "2026-01-15T11:00:00Z",
		MessageCount: 50,
		UserTurns:    10,
	}

	assert.Equal(t, "my-project", info.ProjectName)
	assert.Equal(t, "feature/test", info.GitBranch)
	assert.Equal(t, "Test session summary", info.Summary)
	assert.Equal(t, []string{"task1", "task2"}, info.Accomplished)
	assert.Equal(t, []string{"decision1"}, info.Decisions)
	assert.Equal(t, []string{"gotcha1", "gotcha2"}, info.Gotchas)
	assert.Equal(t, "2026-01-15T10:00:00Z", info.StartedAt)
	assert.Equal(t, "2026-01-15T11:00:00Z", info.EndedAt)
	assert.Equal(t, 50, info.MessageCount)
	assert.Equal(t, 10, info.UserTurns)
}

func TestSessionInfo_JSONSerialization(t *testing.T) {
	info := SessionInfo{
		ProjectName:  "project",
		MessageCount: 25,
		UserTurns:    5,
	}

	data, err := json.Marshal(info)
	assert.NoError(t, err)

	var decoded SessionInfo
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, info.ProjectName, decoded.ProjectName)
	assert.Equal(t, info.MessageCount, decoded.MessageCount)
	assert.Equal(t, info.UserTurns, decoded.UserTurns)
}

func TestSessionInfo_EmptyFields(t *testing.T) {
	info := SessionInfo{}

	assert.Empty(t, info.ProjectName)
	assert.Empty(t, info.GitBranch)
	assert.Nil(t, info.Accomplished)
	assert.Zero(t, info.MessageCount)
}

func TestSessionInfo_JSONOmitEmpty(t *testing.T) {
	info := SessionInfo{
		ProjectName:  "project",
		MessageCount: 10,
	}

	data, err := json.Marshal(info)
	assert.NoError(t, err)

	// git_branch should be omitted when empty
	assert.NotContains(t, string(data), "git_branch")
	// summary should be omitted when empty
	assert.NotContains(t, string(data), "summary")
}

// Tests for TurnInfo structure

func TestTurnInfo_AllFields(t *testing.T) {
	turn := TurnInfo{
		TurnIndex:      5,
		Role:           "assistant",
		ContentPreview: "This is a preview...",
		ToolCalls:      []ToolCall{{Name: "Bash", Success: true}},
		FilesTouched:   []string{"file1.go", "file2.go"},
		HasError:       true,
		ErrorType:      "RuntimeError",
		ErrorMessage:   "Something went wrong",
		Resolution:     "Fixed by retrying",
		TokensUsed:     1500,
		Timestamp:      "2026-01-15T10:30:00Z",
	}

	assert.Equal(t, 5, turn.TurnIndex)
	assert.Equal(t, "assistant", turn.Role)
	assert.Equal(t, "This is a preview...", turn.ContentPreview)
	assert.Len(t, turn.ToolCalls, 1)
	assert.Equal(t, []string{"file1.go", "file2.go"}, turn.FilesTouched)
	assert.True(t, turn.HasError)
	assert.Equal(t, "RuntimeError", turn.ErrorType)
	assert.Equal(t, "Something went wrong", turn.ErrorMessage)
	assert.Equal(t, "Fixed by retrying", turn.Resolution)
	assert.Equal(t, 1500, turn.TokensUsed)
	assert.Equal(t, "2026-01-15T10:30:00Z", turn.Timestamp)
}

func TestTurnInfo_JSONSerialization(t *testing.T) {
	turn := TurnInfo{
		TurnIndex:      1,
		Role:           "user",
		ContentPreview: "Hello",
		TokensUsed:     100,
	}

	data, err := json.Marshal(turn)
	assert.NoError(t, err)

	var decoded TurnInfo
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, turn.TurnIndex, decoded.TurnIndex)
	assert.Equal(t, turn.Role, decoded.Role)
	assert.Equal(t, turn.ContentPreview, decoded.ContentPreview)
	assert.Equal(t, turn.TokensUsed, decoded.TokensUsed)
}

func TestTurnInfo_EmptyFields(t *testing.T) {
	turn := TurnInfo{}

	assert.Zero(t, turn.TurnIndex)
	assert.Empty(t, turn.Role)
	assert.Empty(t, turn.ContentPreview)
	assert.Nil(t, turn.ToolCalls)
	assert.False(t, turn.HasError)
}

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

func TestToolCall_AllFields(t *testing.T) {
	tc := ToolCall{
		Name:    "Read",
		Success: true,
	}

	assert.Equal(t, "Read", tc.Name)
	assert.True(t, tc.Success)
}

func TestToolCall_Failed(t *testing.T) {
	tc := ToolCall{
		Name:    "Bash",
		Success: false,
	}

	assert.Equal(t, "Bash", tc.Name)
	assert.False(t, tc.Success)
}

func TestToolCall_JSONSerialization(t *testing.T) {
	tc := ToolCall{
		Name:    "Edit",
		Success: true,
	}

	data, err := json.Marshal(tc)
	assert.NoError(t, err)

	var decoded ToolCall
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, tc.Name, decoded.Name)
	assert.Equal(t, tc.Success, decoded.Success)
}

func TestToolCall_EmptyFields(t *testing.T) {
	tc := ToolCall{}

	assert.Empty(t, tc.Name)
	assert.False(t, tc.Success)
}

// Tests for limit handling logic

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
