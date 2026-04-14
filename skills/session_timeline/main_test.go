package main

import (
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage/sessions"
	"github.com/stretchr/testify/assert"
)

// Tests for parseSince helper

func TestParseSince_Empty(t *testing.T) {
	result := parseSince("")
	assert.True(t, result.IsZero())
}

func TestParseSince_Whitespace(t *testing.T) {
	result := parseSince("   ")
	assert.True(t, result.IsZero())
}

func TestParseSince_Duration(t *testing.T) {
	before := time.Now()
	result := parseSince("30m")
	after := time.Now()

	// Should be roughly 30 minutes ago
	expected := before.Add(-30 * time.Minute)
	assert.WithinDuration(t, expected, result, 2*time.Second)
	assert.True(t, result.Before(after))
}

func TestParseSince_HoursDuration(t *testing.T) {
	before := time.Now()
	result := parseSince("2h")

	expected := before.Add(-2 * time.Hour)
	assert.WithinDuration(t, expected, result, 2*time.Second)
}

func TestParseSince_DaysDuration(t *testing.T) {
	before := time.Now()
	result := parseSince("7d")

	expected := before.Add(-7 * 24 * time.Hour)
	assert.WithinDuration(t, expected, result, 2*time.Second)
}

func TestParseSince_RFC3339(t *testing.T) {
	ts := "2024-01-15T10:30:00Z"
	result := parseSince(ts)

	expected, _ := time.Parse(time.RFC3339, ts)
	assert.Equal(t, expected, result)
}

func TestParseSince_DateOnly(t *testing.T) {
	result := parseSince("2024-01-15")

	expected, _ := time.Parse("2006-01-02", "2024-01-15")
	assert.Equal(t, expected, result)
}

func TestParseSince_Invalid(t *testing.T) {
	result := parseSince("not-a-time")
	assert.True(t, result.IsZero())
}

// Tests for normalizeTypes helper

func TestNormalizeTypes_Empty(t *testing.T) {
	result := normalizeTypes(nil)
	assert.Equal(t, defaultLearningTypes, result)
}

func TestNormalizeTypes_EmptySlice(t *testing.T) {
	result := normalizeTypes([]string{})
	assert.Equal(t, defaultLearningTypes, result)
}

func TestNormalizeTypes_WithTypes(t *testing.T) {
	result := normalizeTypes([]string{"decision", "gotcha"})
	assert.Equal(t, []string{"decision", "gotcha"}, result)
}

func TestNormalizeTypes_Lowercase(t *testing.T) {
	result := normalizeTypes([]string{"DECISION", "Gotcha"})
	assert.Equal(t, []string{"decision", "gotcha"}, result)
}

func TestNormalizeTypes_Deduplicated(t *testing.T) {
	result := normalizeTypes([]string{"decision", "DECISION", "decision"})
	assert.Equal(t, []string{"decision"}, result)
}

func TestNormalizeTypes_TrimsWhitespace(t *testing.T) {
	result := normalizeTypes([]string{"  decision  ", "gotcha"})
	assert.Equal(t, []string{"decision", "gotcha"}, result)
}

func TestNormalizeTypes_FiltersEmpty(t *testing.T) {
	result := normalizeTypes([]string{"", "decision", "  ", "gotcha"})
	assert.Equal(t, []string{"decision", "gotcha"}, result)
}

func TestNormalizeTypes_AllEmpty(t *testing.T) {
	result := normalizeTypes([]string{"", "  ", ""})
	assert.Equal(t, defaultLearningTypes, result)
}

// Tests for formatChunkRange helper

func TestFormatChunkRange_Same(t *testing.T) {
	result := formatChunkRange(5, 5)
	assert.Equal(t, "C5", result)
}

func TestFormatChunkRange_Different(t *testing.T) {
	result := formatChunkRange(3, 7)
	assert.Equal(t, "C3-7", result)
}

func TestFormatChunkRange_Zero(t *testing.T) {
	result := formatChunkRange(0, 0)
	assert.Equal(t, "C0", result)
}

// Tests for appendUnique helper

func TestAppendUnique_ToEmpty(t *testing.T) {
	result := appendUnique(nil, "value")
	assert.Equal(t, []string{"value"}, result)
}

func TestAppendUnique_NewValue(t *testing.T) {
	result := appendUnique([]string{"a", "b"}, "c")
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestAppendUnique_DuplicateValue(t *testing.T) {
	result := appendUnique([]string{"a", "b"}, "b")
	assert.Equal(t, []string{"a", "b"}, result)
}

func TestAppendUnique_EmptyValue(t *testing.T) {
	result := appendUnique([]string{"a"}, "")
	assert.Equal(t, []string{"a"}, result)
}

func TestAppendUnique_WhitespaceValue(t *testing.T) {
	result := appendUnique([]string{"a"}, "   ")
	assert.Equal(t, []string{"a"}, result)
}

// Tests for sortedKeys helper

func TestSortedKeys_Empty(t *testing.T) {
	result := sortedKeys(map[string]struct{}{})
	assert.Nil(t, result)
}

func TestSortedKeys_Sorted(t *testing.T) {
	input := map[string]struct{}{
		"zebra": {},
		"apple": {},
		"mango": {},
	}
	result := sortedKeys(input)
	assert.Equal(t, []string{"apple", "mango", "zebra"}, result)
}

func TestSortedKeys_FiltersEmpty(t *testing.T) {
	input := map[string]struct{}{
		"apple": {},
		"":      {},
		"  ":    {},
		"mango": {},
	}
	result := sortedKeys(input)
	assert.Equal(t, []string{"apple", "mango"}, result)
}

// Tests for intFromPayload helper

func TestIntFromPayload_Int(t *testing.T) {
	result, ok := intFromPayload(42)
	assert.True(t, ok)
	assert.Equal(t, 42, result)
}

func TestIntFromPayload_Int64(t *testing.T) {
	result, ok := intFromPayload(int64(42))
	assert.True(t, ok)
	assert.Equal(t, 42, result)
}

func TestIntFromPayload_Float64(t *testing.T) {
	result, ok := intFromPayload(float64(42.0))
	assert.True(t, ok)
	assert.Equal(t, 42, result)
}

func TestIntFromPayload_String(t *testing.T) {
	_, ok := intFromPayload("42")
	assert.False(t, ok)
}

func TestIntFromPayload_Nil(t *testing.T) {
	_, ok := intFromPayload(nil)
	assert.False(t, ok)
}

// Tests for stringFromPayload helper

func TestStringFromPayload_String(t *testing.T) {
	result := stringFromPayload("hello")
	assert.Equal(t, "hello", result)
}

func TestStringFromPayload_TrimsWhitespace(t *testing.T) {
	result := stringFromPayload("  hello  ")
	assert.Equal(t, "hello", result)
}

func TestStringFromPayload_Int(t *testing.T) {
	result := stringFromPayload(42)
	assert.Equal(t, "", result)
}

func TestStringFromPayload_Nil(t *testing.T) {
	result := stringFromPayload(nil)
	assert.Equal(t, "", result)
}

// Tests for latestSummaryInWindow helper

func TestLatestSummaryInWindow_Single(t *testing.T) {
	summaries := []sessions.SessionChunkSummary{
		{ID: "sum-1", ChunkIndexMax: 5},
	}
	result := latestSummaryInWindow(summaries)
	assert.Equal(t, "sum-1", result.ID)
}

func TestLatestSummaryInWindow_Multiple(t *testing.T) {
	summaries := []sessions.SessionChunkSummary{
		{ID: "sum-1", ChunkIndexMax: 5},
		{ID: "sum-2", ChunkIndexMax: 10},
		{ID: "sum-3", ChunkIndexMax: 7},
	}
	result := latestSummaryInWindow(summaries)
	assert.Equal(t, "sum-2", result.ID)
}

// Tests for findSummaryForChunk helper

func TestFindSummaryForChunk_InRange(t *testing.T) {
	summaries := []sessions.SessionChunkSummary{
		{ID: "sum-1", ChunkIndexMin: 0, ChunkIndexMax: 5},
		{ID: "sum-2", ChunkIndexMin: 6, ChunkIndexMax: 10},
	}
	result, found := findSummaryForChunk(summaries, 3)
	assert.True(t, found)
	assert.Equal(t, "sum-1", result.ID)
}

func TestFindSummaryForChunk_OnBoundary(t *testing.T) {
	summaries := []sessions.SessionChunkSummary{
		{ID: "sum-1", ChunkIndexMin: 0, ChunkIndexMax: 5},
	}
	result, found := findSummaryForChunk(summaries, 5)
	assert.True(t, found)
	assert.Equal(t, "sum-1", result.ID)
}

func TestFindSummaryForChunk_InChunkIndices(t *testing.T) {
	summaries := []sessions.SessionChunkSummary{
		{ID: "sum-1", ChunkIndexMin: 0, ChunkIndexMax: 5},
		{ID: "sum-2", ChunkIndexMin: 10, ChunkIndexMax: 15, ChunkIndices: []int{8, 9, 10, 11}},
	}
	result, found := findSummaryForChunk(summaries, 8)
	assert.True(t, found)
	assert.Equal(t, "sum-2", result.ID)
}

func TestFindSummaryForChunk_NotFound(t *testing.T) {
	summaries := []sessions.SessionChunkSummary{
		{ID: "sum-1", ChunkIndexMin: 0, ChunkIndexMax: 5},
	}
	_, found := findSummaryForChunk(summaries, 100)
	assert.False(t, found)
}

// Tests for chunkSummaryItem helper

func TestChunkSummaryItem(t *testing.T) {
	summary := sessions.SessionChunkSummary{
		ID:            "sum-123",
		WindowIndex:   2,
		ChunkIndexMin: 10,
		ChunkIndexMax: 15,
		ChunkIndices:  []int{10, 11, 12, 13, 14, 15},
		Trigger:       "manual",
		Summary:       "Test summary",
		SummaryModel:  "claude-3",
		Tools:         []string{"Read", "Write"},
		Files:         []string{"main.go", "test.go"},
		Errors:        []string{"error1"},
	}

	result := chunkSummaryItem(summary)

	assert.Equal(t, "sum-123", result.SummaryID)
	assert.Equal(t, 2, result.WindowIndex)
	assert.Equal(t, 10, result.ChunkIndexMin)
	assert.Equal(t, 15, result.ChunkIndexMax)
	assert.Equal(t, []int{10, 11, 12, 13, 14, 15}, result.ChunkIndices)
	assert.Equal(t, "manual", result.Trigger)
	assert.Equal(t, "Test summary", result.Summary)
	assert.Equal(t, "claude-3", result.SummaryModel)
	assert.Equal(t, []string{"Read", "Write"}, result.Tools)
	assert.Equal(t, []string{"main.go", "test.go"}, result.Files)
	assert.Equal(t, []string{"error1"}, result.Errors)
}

// Tests for buildRollup helper

func TestBuildRollup_Empty(t *testing.T) {
	rollup := buildRollup(nil, nil)
	assert.NotNil(t, rollup)
	assert.Empty(t, rollup.SummaryLines)
	assert.Nil(t, rollup.Tools)
	assert.Nil(t, rollup.Files)
}

func TestBuildRollup_WithSummaries(t *testing.T) {
	summaries := []sessions.SessionChunkSummary{
		{
			WindowIndex:   0,
			ChunkIndexMin: 0,
			ChunkIndexMax: 5,
			Summary:       "First chunk",
			Tools:         []string{"Read"},
			Files:         []string{"main.go"},
		},
		{
			WindowIndex:   1,
			ChunkIndexMin: 6,
			ChunkIndexMax: 10,
			Summary:       "Second chunk",
			Tools:         []string{"Write", "Read"},
			Files:         []string{"test.go"},
			Errors:        []string{"error1"},
		},
	}

	rollup := buildRollup(summaries, nil)

	assert.Len(t, rollup.SummaryLines, 2)
	assert.Contains(t, rollup.SummaryLines[0], "W0 C0-5: First chunk")
	assert.Contains(t, rollup.SummaryLines[1], "W1 C6-10: Second chunk")
	assert.ElementsMatch(t, []string{"Read", "Write"}, rollup.Tools)
	assert.ElementsMatch(t, []string{"main.go", "test.go"}, rollup.Files)
	assert.Equal(t, []string{"error1"}, rollup.Errors)
}

func TestBuildRollup_WithLearnings(t *testing.T) {
	learnings := []LearningItem{
		{Type: "decision", Summary: "Use Go for backend"},
		{Type: "gotcha", Summary: "Don't forget error handling"},
		{Type: "preference", Summary: "Prefer early returns"},
		{Type: "anti_pattern", Summary: "Avoid global state"},
		{Type: "learning", Summary: "Tests are important"},
	}

	rollup := buildRollup(nil, learnings)

	assert.Equal(t, []string{"Use Go for backend"}, rollup.Decisions)
	assert.Equal(t, []string{"Don't forget error handling"}, rollup.Gotchas)
	assert.Equal(t, []string{"Prefer early returns"}, rollup.Preferences)
	assert.Equal(t, []string{"Avoid global state"}, rollup.AntiPatterns)
	assert.Equal(t, []string{"Tests are important"}, rollup.Learnings)
}

func TestBuildRollup_DeduplicatesLearnings(t *testing.T) {
	learnings := []LearningItem{
		{Type: "decision", Summary: "Same decision"},
		{Type: "decision", Summary: "Same decision"},
		{Type: "decision", Summary: "Different decision"},
	}

	rollup := buildRollup(nil, learnings)

	assert.Len(t, rollup.Decisions, 2)
	assert.Contains(t, rollup.Decisions, "Same decision")
	assert.Contains(t, rollup.Decisions, "Different decision")
}

// Tests for default learning types

func TestDefaultLearningTypes(t *testing.T) {
	assert.Contains(t, defaultLearningTypes, "decision")
	assert.Contains(t, defaultLearningTypes, "gotcha")
	assert.Contains(t, defaultLearningTypes, "preference")
	assert.Contains(t, defaultLearningTypes, "anti_pattern")
	assert.Contains(t, defaultLearningTypes, "learning")
}
