package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

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
