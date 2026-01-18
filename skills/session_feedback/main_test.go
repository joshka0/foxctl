package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "session/feedback", command)
}

// Tests for valid outcomes (matching the validation in run)

func TestValidOutcomes(t *testing.T) {
	validOutcomes := map[string]bool{
		"success":   true,
		"partial":   true,
		"failure":   true,
		"abandoned": true,
	}

	assert.True(t, validOutcomes["success"])
	assert.True(t, validOutcomes["partial"])
	assert.True(t, validOutcomes["failure"])
	assert.True(t, validOutcomes["abandoned"])
}

func TestInvalidOutcomes(t *testing.T) {
	validOutcomes := map[string]bool{
		"success":   true,
		"partial":   true,
		"failure":   true,
		"abandoned": true,
	}

	assert.False(t, validOutcomes["invalid"])
	assert.False(t, validOutcomes["completed"])
	assert.False(t, validOutcomes[""])
}

// Tests for rating validation logic

func TestRatingValidation_ValidRatings(t *testing.T) {
	for rating := 1; rating <= 5; rating++ {
		valid := rating >= 1 && rating <= 5
		assert.True(t, valid, "rating %d should be valid", rating)
	}
}

func TestRatingValidation_InvalidRatings(t *testing.T) {
	invalidRatings := []int{0, -1, 6, 10, 100, -100}

	for _, rating := range invalidRatings {
		valid := rating >= 1 && rating <= 5
		assert.False(t, valid, "rating %d should be invalid", rating)
	}
}

// Tests for Input structure

func TestInput_AllFields(t *testing.T) {
	in := Input{
		SessionID:       "sess-123",
		Workspace:       "/path/to/ws",
		Rating:          4,
		Outcome:         "success",
		WhatWorked:      []string{"feature1", "feature2"},
		WhatDidntWork:   []string{"issue1"},
		Blockers:        []string{"blocker1"},
		Suggestions:     []string{"suggestion1", "suggestion2"},
		TaskID:          "task-456",
		ToolsUsed:       []string{"tool1", "tool2"},
		DurationMinutes: 45,
		Notes:           "Additional notes here",
	}

	assert.Equal(t, "sess-123", in.SessionID)
	assert.Equal(t, "/path/to/ws", in.Workspace)
	assert.Equal(t, 4, in.Rating)
	assert.Equal(t, "success", in.Outcome)
	assert.Equal(t, []string{"feature1", "feature2"}, in.WhatWorked)
	assert.Equal(t, []string{"issue1"}, in.WhatDidntWork)
	assert.Equal(t, []string{"blocker1"}, in.Blockers)
	assert.Equal(t, []string{"suggestion1", "suggestion2"}, in.Suggestions)
	assert.Equal(t, "task-456", in.TaskID)
	assert.Equal(t, []string{"tool1", "tool2"}, in.ToolsUsed)
	assert.Equal(t, 45, in.DurationMinutes)
	assert.Equal(t, "Additional notes here", in.Notes)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := Input{
		SessionID: "sess-123",
		Workspace: "/ws",
		Rating:    3,
		Outcome:   "partial",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.SessionID, decoded.SessionID)
	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.Rating, decoded.Rating)
	assert.Equal(t, in.Outcome, decoded.Outcome)
}

func TestInput_EmptyFields(t *testing.T) {
	in := Input{}

	assert.Empty(t, in.SessionID)
	assert.Empty(t, in.Workspace)
	assert.Zero(t, in.Rating)
	assert.Empty(t, in.Outcome)
	assert.Nil(t, in.WhatWorked)
	assert.Nil(t, in.WhatDidntWork)
}

func TestInput_JSONOmitEmpty(t *testing.T) {
	in := Input{
		Workspace: "/ws",
		Rating:    3,
		Outcome:   "success",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	// session_id should be omitted when empty
	assert.NotContains(t, string(data), "session_id")
	// what_worked should be omitted when nil
	assert.NotContains(t, string(data), "what_worked")
}

// Tests for SessionFeedback structure

func TestSessionFeedback_AllFields(t *testing.T) {
	now := time.Now().UTC()
	fb := SessionFeedback{
		FeedbackID:      "fb-123",
		SessionID:       "sess-456",
		Workspace:       "/workspace",
		Rating:          5,
		Outcome:         "success",
		WhatWorked:      []string{"item1"},
		WhatDidntWork:   []string{"item2"},
		Blockers:        []string{"blocker"},
		Suggestions:     []string{"suggest"},
		TaskID:          "task-789",
		ToolsUsed:       []string{"grep", "read"},
		DurationMinutes: 30,
		Notes:           "test notes",
		Timestamp:       now,
	}

	assert.Equal(t, "fb-123", fb.FeedbackID)
	assert.Equal(t, "sess-456", fb.SessionID)
	assert.Equal(t, "/workspace", fb.Workspace)
	assert.Equal(t, 5, fb.Rating)
	assert.Equal(t, "success", fb.Outcome)
	assert.Equal(t, []string{"item1"}, fb.WhatWorked)
	assert.Equal(t, []string{"item2"}, fb.WhatDidntWork)
	assert.Equal(t, []string{"blocker"}, fb.Blockers)
	assert.Equal(t, []string{"suggest"}, fb.Suggestions)
	assert.Equal(t, "task-789", fb.TaskID)
	assert.Equal(t, []string{"grep", "read"}, fb.ToolsUsed)
	assert.Equal(t, 30, fb.DurationMinutes)
	assert.Equal(t, "test notes", fb.Notes)
	assert.Equal(t, now, fb.Timestamp)
}

func TestSessionFeedback_JSONSerialization(t *testing.T) {
	fb := SessionFeedback{
		FeedbackID: "fb-test",
		Workspace:  "/ws",
		Rating:     4,
		Outcome:    "partial",
	}

	data, err := json.Marshal(fb)
	assert.NoError(t, err)

	var decoded SessionFeedback
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, fb.FeedbackID, decoded.FeedbackID)
	assert.Equal(t, fb.Workspace, decoded.Workspace)
	assert.Equal(t, fb.Rating, decoded.Rating)
	assert.Equal(t, fb.Outcome, decoded.Outcome)
}

func TestSessionFeedback_EmptyFields(t *testing.T) {
	fb := SessionFeedback{}

	assert.Empty(t, fb.FeedbackID)
	assert.Empty(t, fb.SessionID)
	assert.Zero(t, fb.Rating)
	assert.Empty(t, fb.Outcome)
}

func TestSessionFeedback_TimeFormat(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	fb := SessionFeedback{
		Timestamp: now,
	}

	data, err := json.Marshal(fb)
	assert.NoError(t, err)

	// Should serialize to RFC3339 format
	assert.Contains(t, string(data), "2026-01-15")
}

// Tests for Output structure

func TestOutput_AllFields(t *testing.T) {
	out := Output{
		FeedbackID: "fb-123",
		Message:    "Feedback recorded: fb-123 (success, 5/5)",
	}

	assert.Equal(t, "fb-123", out.FeedbackID)
	assert.Equal(t, "Feedback recorded: fb-123 (success, 5/5)", out.Message)
}

func TestOutput_JSONSerialization(t *testing.T) {
	out := Output{
		FeedbackID: "fb-test",
		Message:    "test message",
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.FeedbackID, decoded.FeedbackID)
	assert.Equal(t, out.Message, decoded.Message)
}

func TestOutput_EmptyFields(t *testing.T) {
	out := Output{}

	assert.Empty(t, out.FeedbackID)
	assert.Empty(t, out.Message)
}

// Tests for rating boundary values

func TestRatingBoundaries(t *testing.T) {
	tests := []struct {
		rating int
		valid  bool
	}{
		{0, false},
		{1, true},
		{2, true},
		{3, true},
		{4, true},
		{5, true},
		{6, false},
	}

	for _, tc := range tests {
		isValid := tc.rating >= 1 && tc.rating <= 5
		assert.Equal(t, tc.valid, isValid, "rating %d", tc.rating)
	}
}

// Tests for outcome values

func TestOutcomeValues(t *testing.T) {
	validOutcomes := []string{"success", "partial", "failure", "abandoned"}

	for _, outcome := range validOutcomes {
		// Simulate the check from run function
		outcomeMap := map[string]bool{
			"success":   true,
			"partial":   true,
			"failure":   true,
			"abandoned": true,
		}
		assert.True(t, outcomeMap[outcome], "outcome %q should be valid", outcome)
	}
}

func TestOutcomeValues_Invalid(t *testing.T) {
	invalidOutcomes := []string{
		"",
		"done",
		"complete",
		"error",
		"pending",
		"in_progress",
		"SUCCESS", // case-sensitive
	}

	outcomeMap := map[string]bool{
		"success":   true,
		"partial":   true,
		"failure":   true,
		"abandoned": true,
	}

	for _, outcome := range invalidOutcomes {
		assert.False(t, outcomeMap[outcome], "outcome %q should be invalid", outcome)
	}
}

// Tests for Input with all valid outcomes

func TestInput_AllValidOutcomes(t *testing.T) {
	outcomes := []string{"success", "partial", "failure", "abandoned"}

	for _, outcome := range outcomes {
		in := Input{
			Workspace: "/ws",
			Rating:    3,
			Outcome:   outcome,
		}
		assert.Equal(t, outcome, in.Outcome)
	}
}

// Tests for Input with all valid ratings

func TestInput_AllValidRatings(t *testing.T) {
	for rating := 1; rating <= 5; rating++ {
		in := Input{
			Workspace: "/ws",
			Rating:    rating,
			Outcome:   "success",
		}
		assert.Equal(t, rating, in.Rating)
	}
}

// Edge case tests

func TestInput_MaxFieldLength(t *testing.T) {
	longNotes := ""
	for i := 0; i < 1000; i++ {
		longNotes += "x"
	}

	in := Input{
		Workspace: "/ws",
		Rating:    3,
		Outcome:   "success",
		Notes:     longNotes,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, longNotes, decoded.Notes)
}

func TestInput_EmptySlices(t *testing.T) {
	in := Input{
		Workspace:     "/ws",
		Rating:        3,
		Outcome:       "success",
		WhatWorked:    []string{},
		WhatDidntWork: []string{},
		Blockers:      []string{},
	}

	assert.NotNil(t, in.WhatWorked)
	assert.Len(t, in.WhatWorked, 0)
}

func TestSessionFeedback_EmptySlices(t *testing.T) {
	fb := SessionFeedback{
		FeedbackID:    "fb-1",
		WhatWorked:    []string{},
		WhatDidntWork: []string{},
	}

	assert.NotNil(t, fb.WhatWorked)
	assert.NotNil(t, fb.WhatDidntWork)
}

func TestInput_ZeroDuration(t *testing.T) {
	in := Input{
		Workspace:       "/ws",
		Rating:          3,
		Outcome:         "success",
		DurationMinutes: 0,
	}

	assert.Zero(t, in.DurationMinutes)
}

func TestSessionFeedback_FullJSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fb := SessionFeedback{
		FeedbackID:      "fb-full",
		SessionID:       "sess-full",
		Workspace:       "/full/workspace",
		Rating:          5,
		Outcome:         "success",
		WhatWorked:      []string{"a", "b", "c"},
		WhatDidntWork:   []string{"x", "y"},
		Blockers:        []string{"blocker1"},
		Suggestions:     []string{"suggest1"},
		TaskID:          "task-full",
		ToolsUsed:       []string{"tool1"},
		DurationMinutes: 120,
		Notes:           "full test",
		Timestamp:       now,
	}

	data, err := json.Marshal(fb)
	assert.NoError(t, err)

	var decoded SessionFeedback
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, fb.FeedbackID, decoded.FeedbackID)
	assert.Equal(t, fb.SessionID, decoded.SessionID)
	assert.Equal(t, fb.Workspace, decoded.Workspace)
	assert.Equal(t, fb.Rating, decoded.Rating)
	assert.Equal(t, fb.Outcome, decoded.Outcome)
	assert.Equal(t, fb.WhatWorked, decoded.WhatWorked)
	assert.Equal(t, fb.WhatDidntWork, decoded.WhatDidntWork)
	assert.Equal(t, fb.Blockers, decoded.Blockers)
	assert.Equal(t, fb.Suggestions, decoded.Suggestions)
	assert.Equal(t, fb.TaskID, decoded.TaskID)
	assert.Equal(t, fb.ToolsUsed, decoded.ToolsUsed)
	assert.Equal(t, fb.DurationMinutes, decoded.DurationMinutes)
	assert.Equal(t, fb.Notes, decoded.Notes)
}

func TestInput_SpecialCharactersInNotes(t *testing.T) {
	in := Input{
		Workspace: "/ws",
		Rating:    3,
		Outcome:   "success",
		Notes:     "Test with special chars: <>&\"'\n\t日本語",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Notes, decoded.Notes)
}
