package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "optimize/weights", command)
}

// Tests for input structure

func TestInput_AllFields(t *testing.T) {
	in := input{
		Action:    "show",
		Workspace: "/workspace/path",
	}

	assert.Equal(t, "show", in.Action)
	assert.Equal(t, "/workspace/path", in.Workspace)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := input{
		Action:    "learn",
		Workspace: "/test/workspace",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Action, decoded.Action)
	assert.Equal(t, in.Workspace, decoded.Workspace)
}

func TestInput_EmptyFields(t *testing.T) {
	in := input{}

	assert.Empty(t, in.Action)
	assert.Empty(t, in.Workspace)
}

func TestInput_ActionValues(t *testing.T) {
	actions := []string{"show", "learn"}

	for _, action := range actions {
		in := input{Action: action}
		assert.Equal(t, action, in.Action)
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

// Tests for JSON field names

func TestInput_JSONFieldNames(t *testing.T) {
	in := input{
		Action:    "show",
		Workspace: "/ws",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "action")
	assert.Contains(t, jsonStr, "workspace")
}

// Tests for show action

func TestInput_ShowAction(t *testing.T) {
	in := input{
		Action:    "show",
		Workspace: "/workspace",
	}

	assert.Equal(t, "show", in.Action)
}

// Tests for learn action

func TestInput_LearnAction(t *testing.T) {
	in := input{
		Action:    "learn",
		Workspace: "/workspace",
	}

	assert.Equal(t, "learn", in.Action)
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		Action:    "learn",
		Workspace: "/full/test/workspace",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Action, decoded.Action)
	assert.Equal(t, in.Workspace, decoded.Workspace)
}

func TestInput_WorkspaceWithSpaces(t *testing.T) {
	in := input{
		Action:    "show",
		Workspace: "/path/with spaces/workspace",
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
		Action:    "show",
		Workspace: "/Users/user/project-v1.0/workspace",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	assert.Contains(t, string(data), "project-v1.0")
}

func TestInput_ActionOnly(t *testing.T) {
	in := input{
		Action: "show",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	assert.Contains(t, string(data), "action")
	assert.Contains(t, string(data), "show")
}
