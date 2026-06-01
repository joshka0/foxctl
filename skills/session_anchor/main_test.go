package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestAnchorName(t *testing.T) {
	assert.Equal(t, "session-anchor", anchorName)
}

func TestAnchorType(t *testing.T) {
	assert.Equal(t, "session_anchor", anchorType)
}

func TestAllowedOps(t *testing.T) {
	expected := []string{
		"get",
		"set",
		"append_learnings",
		"bump_compaction",
		"set_question",
		"clear_question",
		"clear",
	}
	assert.Equal(t, expected, allowedOps)
}

func TestAllowedOps_ContainsGet(t *testing.T) {
	assert.Contains(t, allowedOps, "get")
}

func TestAllowedOps_ContainsSet(t *testing.T) {
	assert.Contains(t, allowedOps, "set")
}

func TestAllowedOps_ContainsAppendLearnings(t *testing.T) {
	assert.Contains(t, allowedOps, "append_learnings")
}

// Tests for Input structure

func TestOutput_Found(t *testing.T) {
	anchor := &Anchor{AnchorID: "a-1"}
	out := Output{
		Found:   true,
		Anchor:  anchor,
		Message: "anchor loaded",
	}

	assert.True(t, out.Found)
	assert.NotNil(t, out.Anchor)
	assert.Equal(t, "anchor loaded", out.Message)
}

func TestOutput_NotFound(t *testing.T) {
	out := Output{
		Found:   false,
		Anchor:  nil,
		Message: "no anchor set",
	}

	assert.False(t, out.Found)
	assert.Nil(t, out.Anchor)
	assert.Equal(t, "no anchor set", out.Message)
}

func TestMessageForGet_Found(t *testing.T) {
	result := messageForGet(true)
	assert.Equal(t, "anchor loaded", result)
}

func TestMessageForGet_NotFound(t *testing.T) {
	result := messageForGet(false)
	assert.Equal(t, "no anchor set", result)
}

// Tests for truncateOneLine helper

func TestTruncateOneLine_ShortString(t *testing.T) {
	result := truncateOneLine("hello", 10)
	assert.Equal(t, "hello", result)
}

func TestTruncateOneLine_ExactLength(t *testing.T) {
	result := truncateOneLine("hello", 5)
	assert.Equal(t, "hello", result)
}

func TestTruncateOneLine_Truncated(t *testing.T) {
	result := truncateOneLine("hello world", 5)
	assert.Equal(t, "hello", result)
}

func TestTruncateOneLine_ZeroMax(t *testing.T) {
	result := truncateOneLine("hello", 0)
	assert.Equal(t, "", result)
}

func TestTruncateOneLine_NegativeMax(t *testing.T) {
	result := truncateOneLine("hello", -1)
	assert.Equal(t, "", result)
}

func TestTruncateOneLine_EmptyString(t *testing.T) {
	result := truncateOneLine("", 10)
	assert.Equal(t, "", result)
}

func TestTruncateOneLine_NewlineReplacement(t *testing.T) {
	result := truncateOneLine("hello\nworld", 20)
	assert.Equal(t, "hello world", result)
}

func TestTruncateOneLine_MultipleNewlines(t *testing.T) {
	result := truncateOneLine("line1\nline2\nline3", 20)
	assert.Equal(t, "line1 line2 line3", result)
}

func TestTruncateOneLine_WhitespaceTrimmed(t *testing.T) {
	result := truncateOneLine("  hello  ", 10)
	assert.Equal(t, "hello", result)
}

func TestTruncateOneLine_MultipleSpacesNormalized(t *testing.T) {
	result := truncateOneLine("hello    world", 20)
	assert.Equal(t, "hello world", result)
}

func TestTruncateOneLine_MixedWhitespace(t *testing.T) {
	result := truncateOneLine("hello\n\n  world  \n  test", 30)
	assert.Equal(t, "hello world test", result)
}

func TestTruncateOneLine_TruncateAfterNormalization(t *testing.T) {
	result := truncateOneLine("hello\nworld", 8)
	assert.Equal(t, "hello wo", result)
}

// Tests for capRecentLearnings helper

func TestCapRecentLearnings_UnderLimit(t *testing.T) {
	learnings := []Learning{
		{Summary: "l1"},
		{Summary: "l2"},
	}
	result := capRecentLearnings(learnings, 5)
	assert.Len(t, result, 2)
}

func TestCapRecentLearnings_ExactlyAtLimit(t *testing.T) {
	learnings := []Learning{
		{Summary: "l1"},
		{Summary: "l2"},
		{Summary: "l3"},
	}
	result := capRecentLearnings(learnings, 3)
	assert.Len(t, result, 3)
}

func TestCapRecentLearnings_OverLimit(t *testing.T) {
	learnings := []Learning{
		{Summary: "l1"},
		{Summary: "l2"},
		{Summary: "l3"},
		{Summary: "l4"},
		{Summary: "l5"},
	}
	result := capRecentLearnings(learnings, 3)
	assert.Len(t, result, 3)
	// Should keep the most recent (last 3)
	assert.Equal(t, "l3", result[0].Summary)
	assert.Equal(t, "l4", result[1].Summary)
	assert.Equal(t, "l5", result[2].Summary)
}

func TestCapRecentLearnings_ZeroMax(t *testing.T) {
	learnings := []Learning{{Summary: "l1"}}
	result := capRecentLearnings(learnings, 0)
	assert.Len(t, result, 0)
}

func TestCapRecentLearnings_NegativeMax(t *testing.T) {
	learnings := []Learning{{Summary: "l1"}}
	result := capRecentLearnings(learnings, -1)
	assert.Len(t, result, 0)
}

func TestCapRecentLearnings_EmptyInput(t *testing.T) {
	learnings := []Learning{}
	result := capRecentLearnings(learnings, 5)
	assert.Len(t, result, 0)
}

func TestCapRecentLearnings_NilInput(t *testing.T) {
	result := capRecentLearnings(nil, 5)
	assert.Len(t, result, 0)
}

func TestCapRecentLearnings_SingleElement(t *testing.T) {
	learnings := []Learning{{Summary: "single"}}
	result := capRecentLearnings(learnings, 1)
	assert.Len(t, result, 1)
	assert.Equal(t, "single", result[0].Summary)
}

func TestCapRecentLearnings_PreservesOrder(t *testing.T) {
	learnings := []Learning{
		{Summary: "oldest"},
		{Summary: "middle"},
		{Summary: "newest"},
	}
	result := capRecentLearnings(learnings, 2)
	assert.Len(t, result, 2)
	assert.Equal(t, "middle", result[0].Summary)
	assert.Equal(t, "newest", result[1].Summary)
}

// Tests for normalizeLearnings helper

func TestNormalizeLearnings_NilInput(t *testing.T) {
	result := normalizeLearnings(nil)
	assert.NotNil(t, result)
	assert.Len(t, result, 0)
}

func TestNormalizeLearnings_EmptyInput(t *testing.T) {
	result := normalizeLearnings([]Learning{})
	assert.NotNil(t, result)
	assert.Len(t, result, 0)
}

func TestNormalizeLearnings_PreservesData(t *testing.T) {
	learnings := []Learning{
		{
			Summary:   "test",
			Decisions: []string{"d1"},
			Gotchas:   []string{"g1"},
			Progress:  []string{"p1"},
		},
	}
	result := normalizeLearnings(learnings)
	assert.Len(t, result, 1)
	assert.Equal(t, "test", result[0].Summary)
	assert.NotNil(t, result[0].Decisions)
	assert.NotNil(t, result[0].Gotchas)
	assert.NotNil(t, result[0].Progress)
}

func TestNormalizeLearnings_NilSlicesBecomEmpty(t *testing.T) {
	learnings := []Learning{
		{
			Summary:   "test",
			Decisions: nil,
			Gotchas:   nil,
			Progress:  nil,
		},
	}
	result := normalizeLearnings(learnings)
	assert.Len(t, result, 1)
	// After normalization, nil slices should be empty slices
	assert.NotNil(t, result[0].Decisions)
	assert.NotNil(t, result[0].Gotchas)
	assert.NotNil(t, result[0].Progress)
}

func TestNormalizeLearnings_MultipleEntries(t *testing.T) {
	learnings := []Learning{
		{Summary: "first"},
		{Summary: "second"},
		{Summary: "third"},
	}
	result := normalizeLearnings(learnings)
	assert.Len(t, result, 3)
	assert.Equal(t, "first", result[0].Summary)
	assert.Equal(t, "second", result[1].Summary)
	assert.Equal(t, "third", result[2].Summary)
}

func TestNormalizeLearnings_PreservesTimestamp(t *testing.T) {
	now := time.Now().UTC()
	learnings := []Learning{
		{At: now, Summary: "test"},
	}
	result := normalizeLearnings(learnings)
	assert.Equal(t, now, result[0].At)
}

// Edge case tests

func TestTruncateOneLine_OnlyWhitespace(t *testing.T) {
	result := truncateOneLine("   \n\n   ", 10)
	assert.Equal(t, "", result)
}

func TestTruncateOneLine_TabsAndNewlines(t *testing.T) {
	result := truncateOneLine("hello\tworld\ngoodbye", 20)
	assert.Equal(t, "hello world goodbye", result)
}

func TestAnchor_EmptyRequirements(t *testing.T) {
	anchor := Anchor{
		Requirements: []string{},
	}
	assert.NotNil(t, anchor.Requirements)
	assert.Len(t, anchor.Requirements, 0)
}

func TestLearning_EmptySlices(t *testing.T) {
	learning := Learning{
		Decisions: []string{},
		Gotchas:   []string{},
		Progress:  []string{},
	}
	assert.NotNil(t, learning.Decisions)
	assert.NotNil(t, learning.Gotchas)
	assert.NotNil(t, learning.Progress)
}

func TestOutput_NilAnchor(t *testing.T) {
	out := Output{
		Found:   false,
		Anchor:  nil,
		Message: "no anchor",
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Nil(t, decoded.Anchor)
}

func TestCapRecentLearnings_LargeList(t *testing.T) {
	learnings := make([]Learning, 100)
	for i := range learnings {
		learnings[i] = Learning{Summary: string(rune('0' + (i % 10)))}
	}
	result := capRecentLearnings(learnings, 10)
	assert.Len(t, result, 10)
	// Should have last 10 items
	assert.Equal(t, learnings[90].Summary, result[0].Summary)
}

func TestTruncateOneLine_UnicodeCharacters(t *testing.T) {
	result := truncateOneLine("hello 世界", 10)
	// Should truncate by bytes, not runes
	assert.True(t, len(result) <= 10)
}

func TestAnchor_JSONTimeFormat(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	anchor := Anchor{
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := json.Marshal(anchor)
	assert.NoError(t, err)

	// Should serialize to RFC3339 format
	assert.Contains(t, string(data), "2026-01-15")
}
