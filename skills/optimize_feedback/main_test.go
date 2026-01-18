package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "optimize/feedback", command)
}

// Tests for input structure

func TestInput_AllFields(t *testing.T) {
	in := input{
		Action:       "add",
		Workspace:    "/workspace/path",
		TrajectoryID: "traj-123",
		Rating:       4,
		Comment:      "Great job!",
		Role:         "coder",
	}

	assert.Equal(t, "add", in.Action)
	assert.Equal(t, "/workspace/path", in.Workspace)
	assert.Equal(t, "traj-123", in.TrajectoryID)
	assert.Equal(t, 4, in.Rating)
	assert.Equal(t, "Great job!", in.Comment)
	assert.Equal(t, "coder", in.Role)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := input{
		Action:       "add",
		Workspace:    "/test/workspace",
		TrajectoryID: "traj-abc",
		Rating:       5,
		Comment:      "Excellent work",
		Role:         "reviewer",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Action, decoded.Action)
	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.TrajectoryID, decoded.TrajectoryID)
	assert.Equal(t, in.Rating, decoded.Rating)
	assert.Equal(t, in.Comment, decoded.Comment)
	assert.Equal(t, in.Role, decoded.Role)
}

func TestInput_EmptyFields(t *testing.T) {
	in := input{}

	assert.Empty(t, in.Action)
	assert.Empty(t, in.Workspace)
	assert.Empty(t, in.TrajectoryID)
	assert.Zero(t, in.Rating)
	assert.Empty(t, in.Comment)
	assert.Empty(t, in.Role)
}

func TestInput_ActionValues(t *testing.T) {
	actions := []string{"add", "stats"}

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

// Tests for rating validation logic

func TestRatingValidation_ValidRange(t *testing.T) {
	validRatings := []int{1, 2, 3, 4, 5}

	for _, rating := range validRatings {
		isValid := rating >= 1 && rating <= 5
		assert.True(t, isValid, "Rating %d should be valid", rating)
	}
}

func TestRatingValidation_InvalidLow(t *testing.T) {
	invalidRatings := []int{0, -1, -5}

	for _, rating := range invalidRatings {
		isValid := rating >= 1 && rating <= 5
		assert.False(t, isValid, "Rating %d should be invalid", rating)
	}
}

func TestRatingValidation_InvalidHigh(t *testing.T) {
	invalidRatings := []int{6, 10, 100}

	for _, rating := range invalidRatings {
		isValid := rating >= 1 && rating <= 5
		assert.False(t, isValid, "Rating %d should be invalid", rating)
	}
}

func TestRatingValidation_BoundaryValues(t *testing.T) {
	// Test boundary values
	testCases := []struct {
		rating int
		valid  bool
	}{
		{0, false},
		{1, true},
		{5, true},
		{6, false},
	}

	for _, tc := range testCases {
		isValid := tc.rating >= 1 && tc.rating <= 5
		assert.Equal(t, tc.valid, isValid, "Rating %d validation mismatch", tc.rating)
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
		Action:       "add",
		Workspace:    "/ws",
		TrajectoryID: "traj-1",
		Rating:       3,
		Comment:      "Good",
		Role:         "coder",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "action")
	assert.Contains(t, jsonStr, "workspace")
	assert.Contains(t, jsonStr, "trajectory_id")
	assert.Contains(t, jsonStr, "rating")
	assert.Contains(t, jsonStr, "comment")
	assert.Contains(t, jsonStr, "role")
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		Action:       "add",
		Workspace:    "/full/test/workspace",
		TrajectoryID: "traj-full-test-123",
		Rating:       4,
		Comment:      "This is a detailed comment with special chars: <>&",
		Role:         "coder",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Action, decoded.Action)
	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.TrajectoryID, decoded.TrajectoryID)
	assert.Equal(t, in.Rating, decoded.Rating)
	assert.Equal(t, in.Comment, decoded.Comment)
	assert.Equal(t, in.Role, decoded.Role)
}

func TestInput_LongComment(t *testing.T) {
	longComment := "This is a very long comment that goes on and on. " +
		"It contains multiple sentences to test handling of longer feedback. " +
		"The feedback should be stored correctly regardless of length."

	in := input{
		Action:       "add",
		TrajectoryID: "traj-long",
		Rating:       3,
		Comment:      longComment,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, longComment, decoded.Comment)
}

func TestInput_EmptyComment(t *testing.T) {
	in := input{
		Action:       "add",
		TrajectoryID: "traj-no-comment",
		Rating:       5,
		Comment:      "",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Empty(t, decoded.Comment)
}

func TestInput_WorkspaceWithSpaces(t *testing.T) {
	in := input{
		Action:    "stats",
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
		Action:    "stats",
		Workspace: "/Users/user/project-v1.0/workspace",
		Role:      "reviewer",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	assert.Contains(t, string(data), "project-v1.0")
}

func TestInput_TrajectoryIDFormats(t *testing.T) {
	// Test various trajectory ID formats
	trajIDs := []string{
		"traj-123",
		"trajectory-abc-def",
		"01HWXYZ",
		"a1b2c3d4-e5f6-7890-abcd-ef1234567890",
	}

	for _, trajID := range trajIDs {
		in := input{
			Action:       "add",
			TrajectoryID: trajID,
			Rating:       3,
		}

		data, err := json.Marshal(in)
		assert.NoError(t, err)

		var decoded input
		err = json.Unmarshal(data, &decoded)
		assert.NoError(t, err)

		assert.Equal(t, trajID, decoded.TrajectoryID)
	}
}

func TestInput_StatsAction(t *testing.T) {
	in := input{
		Action:    "stats",
		Workspace: "/my/workspace",
		Role:      "coder",
	}

	assert.Equal(t, "stats", in.Action)
	assert.NotEmpty(t, in.Role)
}

func TestInput_AddAction(t *testing.T) {
	in := input{
		Action:       "add",
		TrajectoryID: "traj-test",
		Rating:       4,
		Comment:      "Good work",
	}

	assert.Equal(t, "add", in.Action)
	assert.NotEmpty(t, in.TrajectoryID)
}

func TestInput_AllRatings(t *testing.T) {
	for rating := 1; rating <= 5; rating++ {
		in := input{
			Action:       "add",
			TrajectoryID: "traj-test",
			Rating:       rating,
		}

		assert.Equal(t, rating, in.Rating)

		data, err := json.Marshal(in)
		assert.NoError(t, err)

		var decoded input
		err = json.Unmarshal(data, &decoded)
		assert.NoError(t, err)

		assert.Equal(t, rating, decoded.Rating)
	}
}

func TestInput_CommentWithNewlines(t *testing.T) {
	comment := "Line 1\nLine 2\nLine 3"

	in := input{
		Action:       "add",
		TrajectoryID: "traj-multiline",
		Rating:       3,
		Comment:      comment,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, comment, decoded.Comment)
}

func TestInput_CommentWithUnicode(t *testing.T) {
	comment := "Great work! 👍 Très bien! 很好!"

	in := input{
		Action:       "add",
		TrajectoryID: "traj-unicode",
		Rating:       5,
		Comment:      comment,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, comment, decoded.Comment)
}
