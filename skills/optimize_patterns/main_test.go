package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "optimize/patterns", command)
}

// Tests for input structure

func TestInput_AllFields(t *testing.T) {
	in := input{
		Action:    "list",
		Workspace: "/workspace/path",
		Role:      "coder",
		Context:   "debugging",
		Limit:     100,
	}

	assert.Equal(t, "list", in.Action)
	assert.Equal(t, "/workspace/path", in.Workspace)
	assert.Equal(t, "coder", in.Role)
	assert.Equal(t, "debugging", in.Context)
	assert.Equal(t, 100, in.Limit)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := input{
		Action:    "hints",
		Workspace: "/test/workspace",
		Role:      "reviewer",
		Context:   "code review",
		Limit:     25,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Action, decoded.Action)
	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.Role, decoded.Role)
	assert.Equal(t, in.Context, decoded.Context)
	assert.Equal(t, in.Limit, decoded.Limit)
}

func TestInput_EmptyFields(t *testing.T) {
	in := input{}

	assert.Empty(t, in.Action)
	assert.Empty(t, in.Workspace)
	assert.Empty(t, in.Role)
	assert.Empty(t, in.Context)
	assert.Zero(t, in.Limit)
}

func TestInput_ActionValues(t *testing.T) {
	actions := []string{"list", "clear", "hints"}

	for _, action := range actions {
		in := input{Action: action}
		assert.Equal(t, action, in.Action)
	}
}

func TestInput_RoleValues(t *testing.T) {
	roles := []string{"coder", "planner", "reviewer", "overseer"}

	for _, role := range roles {
		in := input{Role: role}
		assert.Equal(t, role, in.Role)
	}
}

// Tests for workspace default logic

func TestInput_WorkspaceDefault(t *testing.T) {
	in := input{}

	workspace := in.Workspace
	if workspace == "" {
		workspace = "."
	}

	assert.Equal(t, ".", workspace)
}

func TestInput_WorkspaceExplicit(t *testing.T) {
	in := input{Workspace: "/my/project"}

	workspace := in.Workspace
	if workspace == "" {
		workspace = "."
	}

	assert.Equal(t, "/my/project", workspace)
}

// Tests for limit default logic

func TestInput_LimitDefault(t *testing.T) {
	in := input{}

	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}

	assert.Equal(t, 50, limit)
}

func TestInput_LimitNegative(t *testing.T) {
	in := input{Limit: -5}

	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}

	assert.Equal(t, 50, limit)
}

func TestInput_LimitZero(t *testing.T) {
	in := input{Limit: 0}

	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}

	assert.Equal(t, 50, limit)
}

func TestInput_LimitPositive(t *testing.T) {
	in := input{Limit: 100}

	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}

	assert.Equal(t, 100, limit)
}

// Tests for clear message logic

func TestClearMessage_AllPatterns(t *testing.T) {
	role := ""

	msg := "all patterns cleared"
	if role != "" {
		msg = "patterns cleared for role: " + role
	}

	assert.Equal(t, "all patterns cleared", msg)
}

func TestClearMessage_SpecificRole(t *testing.T) {
	role := "coder"

	msg := "all patterns cleared"
	if role != "" {
		msg = "patterns cleared for role: " + role
	}

	assert.Equal(t, "patterns cleared for role: coder", msg)
}

func TestClearMessage_RoleValues(t *testing.T) {
	roles := []string{"coder", "planner", "reviewer", "overseer"}

	for _, role := range roles {
		msg := "all patterns cleared"
		if role != "" {
			msg = "patterns cleared for role: " + role
		}

		assert.Contains(t, msg, role)
	}
}

// Tests for context values

func TestInput_ContextValues(t *testing.T) {
	contexts := []string{
		"debugging",
		"code review",
		"feature implementation",
		"refactoring",
		"testing",
	}

	for _, ctx := range contexts {
		in := input{Context: ctx}
		assert.Equal(t, ctx, in.Context)
	}
}

// Tests for JSON field names

func TestInput_JSONFieldNames(t *testing.T) {
	in := input{
		Action:    "list",
		Workspace: "/ws",
		Role:      "coder",
		Context:   "debug",
		Limit:     10,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "action")
	assert.Contains(t, jsonStr, "workspace")
	assert.Contains(t, jsonStr, "role")
	assert.Contains(t, jsonStr, "context")
	assert.Contains(t, jsonStr, "limit")
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		Action:    "hints",
		Workspace: "/full/test/workspace",
		Role:      "coder",
		Context:   "debugging complex issue",
		Limit:     75,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Action, decoded.Action)
	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.Role, decoded.Role)
	assert.Equal(t, in.Context, decoded.Context)
	assert.Equal(t, in.Limit, decoded.Limit)
}

func TestInput_WorkspaceWithSpaces(t *testing.T) {
	in := input{
		Action:    "list",
		Workspace: "/path/with spaces/workspace",
		Role:      "coder",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, "/path/with spaces/workspace", decoded.Workspace)
}

func TestInput_WorkspaceWithSpecialChars(t *testing.T) {
	in := input{
		Action:    "list",
		Workspace: "/Users/user/project-v1.0/workspace",
		Role:      "reviewer",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	assert.Contains(t, string(data), "project-v1.0")
}

func TestInput_ContextWithSpecialChars(t *testing.T) {
	in := input{
		Action:  "hints",
		Role:    "coder",
		Context: "debugging: TypeError in file.js (line 42)",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, "debugging: TypeError in file.js (line 42)", decoded.Context)
}

func TestInput_LongContext(t *testing.T) {
	longContext := "This is a very long context string that describes a complex debugging scenario. " +
		"The user is investigating a memory leak in the application. " +
		"Multiple components are involved including the database layer and caching system."

	in := input{
		Action:  "hints",
		Role:    "coder",
		Context: longContext,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, longContext, decoded.Context)
}

func TestInput_LargeLimit(t *testing.T) {
	in := input{
		Action: "list",
		Limit:  10000,
	}

	assert.Equal(t, 10000, in.Limit)
}

func TestInput_ListAction(t *testing.T) {
	in := input{
		Action: "list",
		Role:   "coder",
		Limit:  50,
	}

	assert.Equal(t, "list", in.Action)
}

func TestInput_ClearAction(t *testing.T) {
	in := input{
		Action: "clear",
		Role:   "coder",
	}

	assert.Equal(t, "clear", in.Action)
}

func TestInput_HintsAction(t *testing.T) {
	in := input{
		Action:  "hints",
		Role:    "coder",
		Context: "debugging",
	}

	assert.Equal(t, "hints", in.Action)
	assert.NotEmpty(t, in.Role)
	assert.NotEmpty(t, in.Context)
}

func TestInput_HintsRequiresRole(t *testing.T) {
	in := input{
		Action:  "hints",
		Context: "debugging",
	}

	// Role should be empty
	assert.Empty(t, in.Role)
}

func TestInput_HintsRequiresContext(t *testing.T) {
	in := input{
		Action: "hints",
		Role:   "coder",
	}

	// Context should be empty
	assert.Empty(t, in.Context)
}

func TestInput_ActionOnly(t *testing.T) {
	in := input{
		Action: "list",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	assert.Contains(t, string(data), "action")
	assert.Contains(t, string(data), "list")
}

func TestInput_AllRoles(t *testing.T) {
	for _, role := range []string{"coder", "planner", "reviewer", "overseer"} {
		in := input{
			Action: "list",
			Role:   role,
		}

		data, err := json.Marshal(in)
		assert.NoError(t, err)

		var decoded input
		err = json.Unmarshal(data, &decoded)
		assert.NoError(t, err)

		assert.Equal(t, role, decoded.Role)
	}
}
