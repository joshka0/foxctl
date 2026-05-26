package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestInput_OutcomeValues(t *testing.T) {
	outcomes := []string{"success", "partial", "failure", "abandoned"}

	for _, outcome := range outcomes {
		in := Input{Outcome: outcome}
		assert.Equal(t, outcome, in.Outcome)
	}
}

// Tests for SessionFeedback structure

func TestRecommendation_PriorityValues(t *testing.T) {
	priorities := []string{"high", "medium", "low"}

	for _, priority := range priorities {
		rec := Recommendation{Priority: priority}
		assert.Equal(t, priority, rec.Priority)
	}
}

func TestRecommendation_CategoryValues(t *testing.T) {
	categories := []string{"workflow", "tooling", "process"}

	for _, category := range categories {
		rec := Recommendation{Category: category}
		assert.Equal(t, category, rec.Category)
	}
}

func TestTopPatterns_Empty(t *testing.T) {
	counts := make(map[string]int)
	result := topPatterns(counts, 10)

	assert.Empty(t, result)
}

func TestTopPatterns_SingleItem(t *testing.T) {
	counts := map[string]int{"pattern1": 5}
	result := topPatterns(counts, 10)

	assert.Len(t, result, 1)
	assert.Equal(t, "pattern1", result[0].Pattern)
	assert.Equal(t, 5, result[0].Count)
}

func TestTopPatterns_MultipleItems(t *testing.T) {
	counts := map[string]int{
		"pattern1": 5,
		"pattern2": 10,
		"pattern3": 3,
	}
	result := topPatterns(counts, 10)

	assert.Len(t, result, 3)
	// Should be sorted by count descending
	assert.Equal(t, "pattern2", result[0].Pattern)
	assert.Equal(t, 10, result[0].Count)
	assert.Equal(t, "pattern1", result[1].Pattern)
	assert.Equal(t, 5, result[1].Count)
	assert.Equal(t, "pattern3", result[2].Pattern)
	assert.Equal(t, 3, result[2].Count)
}

func TestTopPatterns_Limit(t *testing.T) {
	counts := map[string]int{
		"pattern1": 1,
		"pattern2": 2,
		"pattern3": 3,
		"pattern4": 4,
		"pattern5": 5,
	}
	result := topPatterns(counts, 3)

	assert.Len(t, result, 3)
	// Should be the top 3 by count
	assert.Equal(t, 5, result[0].Count)
	assert.Equal(t, 4, result[1].Count)
	assert.Equal(t, 3, result[2].Count)
}

func TestTopPatterns_LimitLargerThanData(t *testing.T) {
	counts := map[string]int{
		"pattern1": 5,
		"pattern2": 3,
	}
	result := topPatterns(counts, 10)

	assert.Len(t, result, 2)
}

// Tests for analyzeFeedback helper

func TestAnalyzeFeedback_Empty(t *testing.T) {
	feedbacks := []SessionFeedback{}
	output := analyzeFeedback(feedbacks)

	assert.Zero(t, output.FeedbackCount)
	assert.Zero(t, output.AvgRating)
	assert.NotNil(t, output.OutcomeDistribution)
	assert.Empty(t, output.OutcomeDistribution)
	assert.NotNil(t, output.Recommendations)
	assert.Empty(t, output.Recommendations)
}

func TestAnalyzeFeedback_SingleFeedback(t *testing.T) {
	feedbacks := []SessionFeedback{
		{
			Rating:          4,
			Outcome:         "success",
			WhatWorked:      []string{"Clear requirements"},
			DurationMinutes: 30,
		},
	}
	output := analyzeFeedback(feedbacks)

	assert.Equal(t, 1, output.FeedbackCount)
	assert.Equal(t, 4.0, output.AvgRating)
	assert.Equal(t, 1, output.OutcomeDistribution["success"])
	assert.Len(t, output.TopSuccesses, 1)
	assert.Equal(t, "Clear requirements", output.TopSuccesses[0].Pattern)
	assert.Equal(t, 30.0, output.AvgDurationMinutes)
}

func TestAnalyzeFeedback_MultipleFeedbacks(t *testing.T) {
	feedbacks := []SessionFeedback{
		{
			Rating:          5,
			Outcome:         "success",
			WhatWorked:      []string{"Good docs"},
			DurationMinutes: 20,
		},
		{
			Rating:          3,
			Outcome:         "partial",
			WhatWorked:      []string{"Good docs", "Clear task"},
			DurationMinutes: 40,
		},
		{
			Rating:          2,
			Outcome:         "failure",
			WhatDidntWork:   []string{"Missing tests"},
			DurationMinutes: 60,
		},
	}
	output := analyzeFeedback(feedbacks)

	assert.Equal(t, 3, output.FeedbackCount)
	assert.InDelta(t, 3.33, output.AvgRating, 0.01) // (5+3+2)/3
	assert.Equal(t, 1, output.OutcomeDistribution["success"])
	assert.Equal(t, 1, output.OutcomeDistribution["partial"])
	assert.Equal(t, 1, output.OutcomeDistribution["failure"])
	assert.Equal(t, 40.0, output.AvgDurationMinutes) // (20+40+60)/3
}

func TestAnalyzeFeedback_NoDuration(t *testing.T) {
	feedbacks := []SessionFeedback{
		{
			Rating:  4,
			Outcome: "success",
		},
	}
	output := analyzeFeedback(feedbacks)

	assert.Zero(t, output.AvgDurationMinutes)
}

func TestAnalyzeFeedback_ToolUsageStats(t *testing.T) {
	feedbacks := []SessionFeedback{
		{
			Rating:    4,
			Outcome:   "success",
			ToolsUsed: []string{"Bash", "Read", "Edit"},
		},
		{
			Rating:    3,
			Outcome:   "partial",
			ToolsUsed: []string{"Bash", "Read"},
		},
	}
	output := analyzeFeedback(feedbacks)

	assert.Equal(t, 2, output.ToolUsageStats["Bash"])
	assert.Equal(t, 2, output.ToolUsageStats["Read"])
	assert.Equal(t, 1, output.ToolUsageStats["Edit"])
}

func TestAnalyzeFeedback_PatternCounting(t *testing.T) {
	feedbacks := []SessionFeedback{
		{
			Rating:     4,
			Outcome:    "success",
			WhatWorked: []string{"Good docs", "Clear task"},
		},
		{
			Rating:     3,
			Outcome:    "partial",
			WhatWorked: []string{"Good docs"},
		},
	}
	output := analyzeFeedback(feedbacks)

	// "Good docs" should have count 2
	found := false
	for _, p := range output.TopSuccesses {
		if p.Pattern == "Good docs" {
			assert.Equal(t, 2, p.Count)
			found = true
			break
		}
	}
	assert.True(t, found, "Should find 'Good docs' pattern")
}

// Tests for generateRecommendations helper

func TestGenerateRecommendations_EmptyFeedback(t *testing.T) {
	output := Output{
		FeedbackCount:       0,
		OutcomeDistribution: make(map[string]int),
	}

	recs := generateRecommendations(output)
	assert.Empty(t, recs)
}

func TestGenerateRecommendations_LowRating(t *testing.T) {
	output := Output{
		FeedbackCount:       5,
		AvgRating:           2.5,
		OutcomeDistribution: map[string]int{"success": 3, "failure": 2},
	}

	recs := generateRecommendations(output)

	// Should have a recommendation about low rating
	found := false
	for _, rec := range recs {
		if rec.Priority == "high" && rec.Category == "process" {
			found = true
			break
		}
	}
	assert.True(t, found, "Should recommend reviewing low ratings")
}

func TestGenerateRecommendations_HighFailureRate(t *testing.T) {
	output := Output{
		FeedbackCount:       5,
		AvgRating:           3.5,
		OutcomeDistribution: map[string]int{"success": 1, "failure": 3, "abandoned": 1},
	}

	recs := generateRecommendations(output)

	// Should have a recommendation about high failure rate
	found := false
	for _, rec := range recs {
		if rec.Description == "More sessions ending in failure than success. Investigate root causes." {
			found = true
			assert.Equal(t, "high", rec.Priority)
			break
		}
	}
	assert.True(t, found, "Should recommend investigating failures")
}

func TestGenerateRecommendations_RecurringBlocker(t *testing.T) {
	output := Output{
		FeedbackCount:       5,
		AvgRating:           4.0,
		OutcomeDistribution: map[string]int{"success": 5},
		TopBlockers:         []PatternCount{{Pattern: "Slow CI", Count: 3}},
	}

	recs := generateRecommendations(output)

	found := false
	for _, rec := range recs {
		if rec.Category == "workflow" && rec.Priority == "medium" {
			found = true
			assert.Contains(t, rec.Description, "Slow CI")
			break
		}
	}
	assert.True(t, found, "Should recommend addressing recurring blocker")
}

func TestGenerateRecommendations_UserSuggestions(t *testing.T) {
	output := Output{
		FeedbackCount:       5,
		AvgRating:           4.0,
		OutcomeDistribution: map[string]int{"success": 5},
		TopSuggestions:      []PatternCount{{Pattern: "Add caching", Count: 3}},
	}

	recs := generateRecommendations(output)

	found := false
	for _, rec := range recs {
		if rec.Category == "tooling" && rec.Priority == "medium" {
			found = true
			assert.Contains(t, rec.Description, "Add caching")
			break
		}
	}
	assert.True(t, found, "Should include user suggestion as recommendation")
}

func TestGenerateRecommendations_LongDuration(t *testing.T) {
	output := Output{
		FeedbackCount:       5,
		AvgRating:           4.0,
		AvgDurationMinutes:  90.0,
		OutcomeDistribution: map[string]int{"success": 5},
	}

	recs := generateRecommendations(output)

	found := false
	for _, rec := range recs {
		if rec.Priority == "low" && rec.Category == "workflow" {
			found = true
			assert.Contains(t, rec.Description, "Sessions averaging over an hour")
			break
		}
	}
	assert.True(t, found, "Should recommend breaking down long sessions")
}

func TestGenerateRecommendations_SuccessfulPattern(t *testing.T) {
	output := Output{
		FeedbackCount:       5,
		AvgRating:           4.0,
		OutcomeDistribution: map[string]int{"success": 5},
		TopSuccesses:        []PatternCount{{Pattern: "Clear requirements", Count: 4}},
	}

	recs := generateRecommendations(output)

	found := false
	for _, rec := range recs {
		if rec.Priority == "low" && rec.Category == "process" {
			found = true
			assert.Contains(t, rec.Description, "Clear requirements")
			break
		}
	}
	assert.True(t, found, "Should recommend reinforcing successful pattern")
}

// Tests for rating range defaults

func TestInput_RatingDefaults(t *testing.T) {
	in := Input{}

	minRating := in.MinRating
	maxRating := in.MaxRating

	if minRating == 0 {
		minRating = 1
	}
	if maxRating == 0 {
		maxRating = 5
	}

	assert.Equal(t, 1, minRating)
	assert.Equal(t, 5, maxRating)
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := Input{
		Workspace: "/full/test/workspace",
		Since:     "2026-01-15T12:00:00Z",
		MinRating: 2,
		MaxRating: 4,
		Outcome:   "success",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.Since, decoded.Since)
	assert.Equal(t, in.MinRating, decoded.MinRating)
	assert.Equal(t, in.MaxRating, decoded.MaxRating)
	assert.Equal(t, in.Outcome, decoded.Outcome)
}

func TestSessionFeedback_LargeArrays(t *testing.T) {
	fb := SessionFeedback{
		FeedbackID:    "fb-large",
		WhatWorked:    make([]string, 50),
		WhatDidntWork: make([]string, 30),
		ToolsUsed:     make([]string, 20),
	}

	for i := 0; i < 50; i++ {
		fb.WhatWorked[i] = "item"
	}
	for i := 0; i < 30; i++ {
		fb.WhatDidntWork[i] = "item"
	}
	for i := 0; i < 20; i++ {
		fb.ToolsUsed[i] = "Tool"
	}

	data, err := json.Marshal(fb)
	assert.NoError(t, err)

	var decoded SessionFeedback
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Len(t, decoded.WhatWorked, 50)
	assert.Len(t, decoded.WhatDidntWork, 30)
	assert.Len(t, decoded.ToolsUsed, 20)
}
