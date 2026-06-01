package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skilltest"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for constants

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

func TestRunAddFeedbackRejectsRatingsOutsideOneToFive(t *testing.T) {
	for _, rating := range []int{-1, 0, 6, 100} {
		t.Run(fmt.Sprintf("rating_%d", rating), func(t *testing.T) {
			var buf bytes.Buffer
			rc, cleanup := skilltest.NewTestRunContext(t, &buf, nil)
			defer cleanup()

			ctx := context.Background()
			workspace := mustAbsWorkspace(t, rc)
			traj := seedFeedbackTrajectory(t, ctx, rc, workspace)

			err := run(ctx, rc, input{
				Action:       "add",
				Workspace:    workspace,
				TrajectoryID: traj.ID,
				Rating:       rating,
				Comment:      "should not be recorded",
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "rating must be between 1 and 5")

			got := getFeedbackTrajectory(t, ctx, rc, workspace, traj.ID)
			if got.Outcome != nil && got.Outcome.HumanRating != nil {
				t.Fatalf("invalid rating %d recorded outcome %+v", rating, got.Outcome)
			}
			assert.Empty(t, buf.String())
		})
	}
}

func TestRunAddFeedbackAcceptsBoundaryRatings(t *testing.T) {
	for _, rating := range []int{1, 5} {
		t.Run(fmt.Sprintf("rating_%d", rating), func(t *testing.T) {
			var buf bytes.Buffer
			rc, cleanup := skilltest.NewTestRunContext(t, &buf, nil)
			defer cleanup()

			ctx := context.Background()
			workspace := mustAbsWorkspace(t, rc)
			traj := seedFeedbackTrajectory(t, ctx, rc, workspace)

			err := run(ctx, rc, input{
				Action:       "add",
				Workspace:    workspace,
				TrajectoryID: traj.ID,
				Rating:       rating,
				Comment:      "boundary rating",
			})
			require.NoError(t, err)

			got := getFeedbackTrajectory(t, ctx, rc, workspace, traj.ID)
			if got.Outcome == nil || got.Outcome.HumanRating == nil {
				t.Fatalf("rating %d was not recorded in outcome %+v", rating, got.Outcome)
			}
			assert.Equal(t, rating, *got.Outcome.HumanRating)
			assert.Equal(t, "boundary rating", got.Outcome.Feedback)
		})
	}
}

func mustAbsWorkspace(t *testing.T, rc *skillmain.RunContext) string {
	t.Helper()
	workspace, err := filepath.Abs(rc.Workspace)
	require.NoError(t, err)
	return workspace
}

func seedFeedbackTrajectory(t *testing.T, ctx context.Context, rc *skillmain.RunContext, workspace string) trajectory.Trajectory {
	t.Helper()
	store, err := trajectory.Open(ctx, rc.Config.Storage.Root)
	require.NoError(t, err)
	defer store.Close()

	traj, err := store.InsertTrajectory(ctx, trajectory.Trajectory{
		WorkspaceID: workspace,
		Status:      trajectory.StatusOK,
	})
	require.NoError(t, err)
	return traj
}

func getFeedbackTrajectory(t *testing.T, ctx context.Context, rc *skillmain.RunContext, workspace, id string) trajectory.Trajectory {
	t.Helper()
	store, err := trajectory.Open(ctx, rc.Config.Storage.Root)
	require.NoError(t, err)
	defer store.Close()

	traj, err := store.GetTrajectory(ctx, workspace, id)
	require.NoError(t, err)
	return traj
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
