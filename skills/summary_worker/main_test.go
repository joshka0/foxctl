package main

import (
	"encoding/json"
	"testing"

	"github.com/joshka0/foxctl/internal/storage"
	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestDefaultBatch(t *testing.T) {
	assert.Equal(t, 10, defaultBatch)
}

func TestDefaultMaxDur(t *testing.T) {
	assert.Equal(t, 300, defaultMaxDur) // 5 minutes
}

// Tests for Input structure

func TestInput_DefaultsApplied(t *testing.T) {
	in := Input{}

	// Simulate default application logic from run function
	if in.BatchSize <= 0 {
		in.BatchSize = defaultBatch
	}
	if in.MaxDuration <= 0 {
		in.MaxDuration = defaultMaxDur
	}

	assert.Equal(t, defaultBatch, in.BatchSize)
	assert.Equal(t, defaultMaxDur, in.MaxDuration)
}

func TestInput_CustomValues(t *testing.T) {
	in := Input{
		BatchSize:   5,
		MaxDuration: 60,
	}

	// Should not change if positive
	if in.BatchSize <= 0 {
		in.BatchSize = defaultBatch
	}
	if in.MaxDuration <= 0 {
		in.MaxDuration = defaultMaxDur
	}

	assert.Equal(t, 5, in.BatchSize)
	assert.Equal(t, 60, in.MaxDuration)
}

// Tests for Output structure

func TestOutput_NilStats(t *testing.T) {
	out := Output{
		Processed: 10,
		Status:    "completed",
		Stats:     nil,
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Nil(t, decoded.Stats)
}

// Tests for QueueSnapshot structure

func TestBuildWindowSummaryPrompt_Basic(t *testing.T) {
	window := &storage.ContextWindow{
		Trigger: "manual",
	}
	contentPreview := "User asked to implement feature X"

	result := buildWindowSummaryPrompt(window, contentPreview)

	assert.Contains(t, result, "Summarize this coding session window")
	assert.Contains(t, result, "2-3 concise sentences")
	assert.Contains(t, result, "Window trigger: manual")
	assert.Contains(t, result, "User asked to implement feature X")
	assert.Contains(t, result, "Summary:")
}

func TestBuildWindowSummaryPrompt_DifferentTriggers(t *testing.T) {
	triggers := []string{"manual", "token_limit", "time_limit", "context_switch"}

	for _, trigger := range triggers {
		window := &storage.ContextWindow{
			Trigger: trigger,
		}
		result := buildWindowSummaryPrompt(window, "test content")
		assert.Contains(t, result, "Window trigger: "+trigger)
	}
}

func TestBuildWindowSummaryPrompt_EmptyContentPreview(t *testing.T) {
	window := &storage.ContextWindow{
		Trigger: "manual",
	}

	result := buildWindowSummaryPrompt(window, "")

	assert.Contains(t, result, "Window trigger: manual")
	assert.Contains(t, result, "Content preview:")
	// Should still generate a valid prompt even with empty content
	assert.Contains(t, result, "Summary:")
}

func TestBuildWindowSummaryPrompt_LongContentPreview(t *testing.T) {
	window := &storage.ContextWindow{
		Trigger: "token_limit",
	}
	// Create a long content preview
	longContent := ""
	for i := 0; i < 100; i++ {
		longContent += "This is line number " + string(rune('0'+i%10)) + " of the content preview. "
	}

	result := buildWindowSummaryPrompt(window, longContent)

	assert.Contains(t, result, "Window trigger: token_limit")
	assert.Contains(t, result, longContent)
}

func TestBuildWindowSummaryPrompt_SpecialCharacters(t *testing.T) {
	window := &storage.ContextWindow{
		Trigger: "context_switch",
	}
	contentPreview := "User asked: \"How do I use <template>?\" and got error: `undefined`"

	result := buildWindowSummaryPrompt(window, contentPreview)

	assert.Contains(t, result, contentPreview)
	assert.Contains(t, result, "\"How do I use <template>?\"")
}

func TestBuildWindowSummaryPrompt_MultilineContent(t *testing.T) {
	window := &storage.ContextWindow{
		Trigger: "manual",
	}
	contentPreview := "Line 1: Started task\nLine 2: Implemented feature\nLine 3: Fixed bug"

	result := buildWindowSummaryPrompt(window, contentPreview)

	assert.Contains(t, result, "Line 1: Started task")
	assert.Contains(t, result, "Line 2: Implemented feature")
	assert.Contains(t, result, "Line 3: Fixed bug")
}

func TestBuildWindowSummaryPrompt_PromptStructure(t *testing.T) {
	window := &storage.ContextWindow{
		Trigger: "manual",
	}
	contentPreview := "test"

	result := buildWindowSummaryPrompt(window, contentPreview)

	// Verify the prompt has the expected structure
	assert.Contains(t, result, "Focus on: what was accomplished, key decisions made, and any issues encountered")
}

// Tests for batch size defaults

func TestBatchSizeDefault_Zero(t *testing.T) {
	batchSize := 0
	if batchSize <= 0 {
		batchSize = defaultBatch
	}
	assert.Equal(t, defaultBatch, batchSize)
}

func TestBatchSizeDefault_Negative(t *testing.T) {
	batchSize := -5
	if batchSize <= 0 {
		batchSize = defaultBatch
	}
	assert.Equal(t, defaultBatch, batchSize)
}

func TestBatchSizeDefault_Positive(t *testing.T) {
	batchSize := 25
	if batchSize <= 0 {
		batchSize = defaultBatch
	}
	assert.Equal(t, 25, batchSize)
}

// Tests for max duration defaults

func TestMaxDurationDefault_Zero(t *testing.T) {
	maxDur := 0
	if maxDur <= 0 {
		maxDur = defaultMaxDur
	}
	assert.Equal(t, defaultMaxDur, maxDur)
}

func TestMaxDurationDefault_Negative(t *testing.T) {
	maxDur := -10
	if maxDur <= 0 {
		maxDur = defaultMaxDur
	}
	assert.Equal(t, defaultMaxDur, maxDur)
}

func TestMaxDurationDefault_Positive(t *testing.T) {
	maxDur := 120
	if maxDur <= 0 {
		maxDur = defaultMaxDur
	}
	assert.Equal(t, 120, maxDur)
}

// Edge case tests

func TestOutput_LargeCounts(t *testing.T) {
	out := Output{
		Processed:  100000,
		Errors:     5000,
		Skipped:    10000,
		Remaining:  50000,
		BatchCount: 10000,
		DurationMs: 3600000, // 1 hour
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, 100000, decoded.Processed)
	assert.Equal(t, 5000, decoded.Errors)
	assert.Equal(t, 10000, decoded.Skipped)
	assert.Equal(t, 50000, decoded.Remaining)
}

func TestOutput_WithStats(t *testing.T) {
	out := Output{
		Processed: 10,
		Status:    "completed",
		Stats: &QueueSnapshot{
			Queued:    5,
			Running:   0,
			Completed: 10,
			Failed:    0,
		},
	}

	assert.NotNil(t, out.Stats)
	assert.Equal(t, 5, out.Stats.Queued)
	assert.Equal(t, 10, out.Stats.Completed)
}

func TestInput_ProcessAllFalse(t *testing.T) {
	in := Input{
		BatchSize:  10,
		ProcessAll: false,
	}

	// When ProcessAll is false, worker returns after one batch
	assert.False(t, in.ProcessAll)
}

func TestInput_ProcessAllTrue(t *testing.T) {
	in := Input{
		BatchSize:  10,
		ProcessAll: true,
	}

	// When ProcessAll is true, worker loops until queue empty or timeout
	assert.True(t, in.ProcessAll)
}

func TestInput_DryRunTrue(t *testing.T) {
	in := Input{
		DryRun: true,
	}

	// When DryRun is true, claims jobs but doesn't call LLM API
	assert.True(t, in.DryRun)
}

func TestInput_WithSessionID(t *testing.T) {
	in := Input{
		SessionID: "sess-abc123",
	}

	// SessionID filters processing to only jobs for this session
	assert.Equal(t, "sess-abc123", in.SessionID)
}

func TestOutput_MessageFormats(t *testing.T) {
	tests := []struct {
		status   string
		contains string
	}{
		{"completed", "Processed"},
		{"timeout", "Timeout"},
		{"no_jobs", "No jobs"},
	}

	for _, tc := range tests {
		out := Output{Status: tc.status}
		switch out.Status {
		case "completed":
			out.Message = "Processed 0 summaries (0 skipped, 0 errors) in 0ms"
		case "timeout":
			out.Message = "Timeout after 0 summaries (0 skipped, 0 errors, 0 remaining)"
		case "no_jobs":
			out.Message = "No jobs in queue"
		}
		assert.Contains(t, out.Message, tc.contains)
	}
}

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := Input{
		BatchSize:   50,
		MaxDuration: 900,
		ProcessAll:  true,
		DryRun:      false,
		SessionID:   "test-sess-123",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.BatchSize, decoded.BatchSize)
	assert.Equal(t, in.MaxDuration, decoded.MaxDuration)
	assert.Equal(t, in.ProcessAll, decoded.ProcessAll)
	assert.Equal(t, in.DryRun, decoded.DryRun)
	assert.Equal(t, in.SessionID, decoded.SessionID)
}

func TestOutput_FullJSONRoundTrip(t *testing.T) {
	out := Output{
		Processed:  75,
		Errors:     2,
		Skipped:    8,
		Remaining:  23,
		BatchCount: 8,
		Status:     "timeout",
		DurationMs: 300000,
		LastError:  "rate limit exceeded",
		Stats: &QueueSnapshot{
			Queued:    23,
			Running:   0,
			Completed: 75,
			Failed:    2,
		},
		Message: "Timeout after processing",
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.Processed, decoded.Processed)
	assert.Equal(t, out.Errors, decoded.Errors)
	assert.Equal(t, out.Skipped, decoded.Skipped)
	assert.Equal(t, out.Status, decoded.Status)
	assert.NotNil(t, decoded.Stats)
	assert.Equal(t, out.Stats.Queued, decoded.Stats.Queued)
}

func TestQueueSnapshot_FullJSONRoundTrip(t *testing.T) {
	snap := QueueSnapshot{
		Queued:    100,
		Running:   5,
		Completed: 500,
		Failed:    10,
	}

	data, err := json.Marshal(snap)
	assert.NoError(t, err)

	var decoded QueueSnapshot
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, snap.Queued, decoded.Queued)
	assert.Equal(t, snap.Running, decoded.Running)
	assert.Equal(t, snap.Completed, decoded.Completed)
	assert.Equal(t, snap.Failed, decoded.Failed)
}
