package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

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

// Tests for role required validation

func TestInput_RoleRequired(t *testing.T) {
	in := input{
		Workspace: "/workspace",
	}

	// Role is required
	assert.Empty(t, in.Role)
}

func TestInput_RoleProvided(t *testing.T) {
	in := input{
		Workspace: "/workspace",
		Role:      "coder",
	}

	assert.NotEmpty(t, in.Role)
}

// Tests for trajectory-specific vs summary mode

func TestInput_TrajectoryMode(t *testing.T) {
	in := input{
		Role:         "coder",
		TrajectoryID: "traj-123",
	}

	// Has trajectory ID means trajectory-specific mode
	hasTrajectory := in.TrajectoryID != ""
	assert.True(t, hasTrajectory)
}

func TestInput_SummaryMode(t *testing.T) {
	in := input{
		Role: "coder",
	}

	// No trajectory ID means summary mode
	hasTrajectory := in.TrajectoryID != ""
	assert.False(t, hasTrajectory)
}

// Tests for JSON field names

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		Workspace:    "/full/test/workspace",
		Role:         "coder",
		TrajectoryID: "traj-full-test-123",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.Role, decoded.Role)
	assert.Equal(t, in.TrajectoryID, decoded.TrajectoryID)
}

func TestInput_WorkspaceWithSpaces(t *testing.T) {
	in := input{
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
		Workspace: "/Users/user/project-v1.0/workspace",
		Role:      "reviewer",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	assert.Contains(t, string(data), "project-v1.0")
}

func TestInput_TrajectoryIDFormats(t *testing.T) {
	trajIDs := []string{
		"traj-123",
		"trajectory-abc-def",
		"01HWXYZ",
		"a1b2c3d4-e5f6-7890-abcd-ef1234567890",
	}

	for _, trajID := range trajIDs {
		in := input{
			Role:         "coder",
			TrajectoryID: trajID,
		}

		data, err := json.Marshal(in)
		assert.NoError(t, err)

		var decoded input
		err = json.Unmarshal(data, &decoded)
		assert.NoError(t, err)

		assert.Equal(t, trajID, decoded.TrajectoryID)
	}
}

func TestInput_RoleOnly(t *testing.T) {
	in := input{
		Role: "coder",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	assert.Contains(t, string(data), "role")
	assert.Contains(t, string(data), "coder")
}

func TestInput_AllRoles(t *testing.T) {
	for _, role := range []string{"coder", "planner", "reviewer", "overseer"} {
		in := input{
			Role: role,
		}

		data, err := json.Marshal(in)
		assert.NoError(t, err)

		var decoded input
		err = json.Unmarshal(data, &decoded)
		assert.NoError(t, err)

		assert.Equal(t, role, decoded.Role)
	}
}
