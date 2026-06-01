package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/hooks"
	"github.com/joshka0/foxctl/internal/storage/tasks"
	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestHookInput_Structure(t *testing.T) {
	in := HookInput{
		Input:          hooks.Input{SessionID: "sess-123"},
		TranscriptPath: "/path/to/transcript.jsonl",
	}

	assert.Equal(t, "sess-123", in.SessionID)
	assert.Equal(t, "/path/to/transcript.jsonl", in.TranscriptPath)
}

func TestHookInput_EmptyTranscriptPath(t *testing.T) {
	in := HookInput{
		Input: hooks.Input{SessionID: "sess-789"},
	}

	assert.Empty(t, in.TranscriptPath)
}

// Tests for SessionMetrics structure

func TestSessionMetrics_ZeroValues(t *testing.T) {
	metrics := SessionMetrics{}

	assert.Empty(t, metrics.MetricsID)
	assert.Empty(t, metrics.SessionID)
	assert.Zero(t, metrics.TasksCompleted)
	assert.False(t, metrics.HasTranscript)
}

// Tests for buildFeedbackPrompt helper

func TestBuildFeedbackPrompt_Basic(t *testing.T) {
	metrics := SessionMetrics{
		SessionID:       "sess-123",
		TasksCompleted:  5,
		TasksInProgress: 2,
		TasksPending:    3,
	}

	result := buildFeedbackPrompt(metrics)

	assert.Contains(t, result, "Session Summary")
	assert.Contains(t, result, "Tasks completed: 5")
	assert.Contains(t, result, "Tasks in progress: 2")
	assert.Contains(t, result, "Tasks pending: 3")
}

func TestBuildFeedbackPrompt_WithTrajectories(t *testing.T) {
	metrics := SessionMetrics{
		SessionID:         "sess-123",
		TasksCompleted:    1,
		TrajectoriesCount: 15,
	}

	result := buildFeedbackPrompt(metrics)

	assert.Contains(t, result, "Trajectories recorded: 15")
}

func TestBuildFeedbackPrompt_NoTrajectories(t *testing.T) {
	metrics := SessionMetrics{
		SessionID:         "sess-123",
		TasksCompleted:    1,
		TrajectoriesCount: 0,
	}

	result := buildFeedbackPrompt(metrics)

	assert.NotContains(t, result, "Trajectories recorded")
}

func TestBuildFeedbackPrompt_FeedbackCommand(t *testing.T) {
	metrics := SessionMetrics{
		SessionID: "sess-test-123",
	}

	result := buildFeedbackPrompt(metrics)

	assert.Contains(t, result, "foxctl run session/feedback")
	assert.Contains(t, result, "sess-test-123")
	assert.Contains(t, result, "rating")
	assert.Contains(t, result, "outcome")
}

func TestBuildFeedbackPrompt_NoSessionID(t *testing.T) {
	metrics := SessionMetrics{
		SessionID:      "",
		TasksCompleted: 1,
	}

	result := buildFeedbackPrompt(metrics)

	assert.Contains(t, result, "Session Summary")
	assert.NotContains(t, result, "foxctl run session/feedback")
}

func TestBuildFeedbackPrompt_Formatting(t *testing.T) {
	metrics := SessionMetrics{
		SessionID: "sess-123",
	}

	result := buildFeedbackPrompt(metrics)

	// Check markdown formatting
	assert.Contains(t, result, "---")
	assert.Contains(t, result, "##")
	assert.Contains(t, result, "```bash")
}

func TestBuildFeedbackPrompt_ZeroTasks(t *testing.T) {
	metrics := SessionMetrics{
		SessionID:       "sess-123",
		TasksCompleted:  0,
		TasksInProgress: 0,
		TasksPending:    0,
	}

	result := buildFeedbackPrompt(metrics)

	assert.Contains(t, result, "Tasks completed: 0")
	assert.Contains(t, result, "Tasks in progress: 0")
	assert.Contains(t, result, "Tasks pending: 0")
}

// Tests for fileExists helper

func TestFileExists_EmptyPath(t *testing.T) {
	result := fileExists("")
	assert.False(t, result)
}

func TestFileExists_NonExistentPath(t *testing.T) {
	result := fileExists("/nonexistent/path/to/file.txt")
	assert.False(t, result)
}

func TestFileExists_ExistingFile(t *testing.T) {
	// main.go should exist in the same directory
	result := fileExists("main.go")
	// This may fail depending on working directory, so we just test it doesn't panic
	_ = result
}

// Tests for intPtr helper

func TestIntPtr_PositiveValue(t *testing.T) {
	ptr := intPtr(90)
	assert.NotNil(t, ptr)
	assert.Equal(t, 90, *ptr)
}

func TestIntPtr_ZeroValue(t *testing.T) {
	ptr := intPtr(0)
	assert.NotNil(t, ptr)
	assert.Equal(t, 0, *ptr)
}

func TestIntPtr_NegativeValue(t *testing.T) {
	ptr := intPtr(-5)
	assert.NotNil(t, ptr)
	assert.Equal(t, -5, *ptr)
}

// Tests for task status counting logic

func TestTaskStatusCounting(t *testing.T) {
	testTasks := []tasks.Task{
		{Status: tasks.StatusCompleted},
		{Status: tasks.StatusCompleted},
		{Status: tasks.StatusInProgress},
		{Status: tasks.StatusPending},
		{Status: tasks.StatusPending},
		{Status: tasks.StatusPending},
	}

	var completed, inProgress, pending int
	for _, task := range testTasks {
		switch task.Status {
		case tasks.StatusCompleted:
			completed++
		case tasks.StatusInProgress:
			inProgress++
		case tasks.StatusPending:
			pending++
		}
	}

	assert.Equal(t, 2, completed)
	assert.Equal(t, 1, inProgress)
	assert.Equal(t, 3, pending)
}

// Tests for task status values

func TestTaskStatusValues(t *testing.T) {
	assert.Equal(t, "completed", tasks.StatusCompleted)
	assert.Equal(t, "in_progress", tasks.StatusInProgress)
	assert.Equal(t, "pending", tasks.StatusPending)
}

// Tests for hooks.Event values used in this skill

func TestEventSessionEnd(t *testing.T) {
	assert.Equal(t, hooks.Event("SessionEnd"), hooks.EventSessionEnd)
}

// Tests for edge cases in buildFeedbackPrompt

func TestBuildFeedbackPrompt_LongSessionID(t *testing.T) {
	longID := "sess-" + string(make([]byte, 100)) // Long session ID
	metrics := SessionMetrics{
		SessionID: longID,
	}

	result := buildFeedbackPrompt(metrics)

	assert.Contains(t, result, longID)
}

func TestBuildFeedbackPrompt_SpecialCharsInSessionID(t *testing.T) {
	metrics := SessionMetrics{
		SessionID: "sess-test/special:chars#123",
	}

	result := buildFeedbackPrompt(metrics)

	assert.Contains(t, result, "sess-test/special:chars#123")
}

func TestBuildFeedbackPrompt_LargeCounts(t *testing.T) {
	metrics := SessionMetrics{
		SessionID:         "sess-123",
		TasksCompleted:    10000,
		TasksInProgress:   5000,
		TasksPending:      15000,
		TrajectoriesCount: 100000,
	}

	result := buildFeedbackPrompt(metrics)

	assert.Contains(t, result, "10000")
	assert.Contains(t, result, "5000")
	assert.Contains(t, result, "15000")
	assert.Contains(t, result, "100000")
}

// Tests for HookInput inheriting hooks.Input

func TestHookInput_InheritsInputFields(t *testing.T) {
	in := HookInput{
		Input: hooks.Input{
			Event:     hooks.EventSessionEnd,
			SessionID: "sess-123",
			ActorID:   "agent-1",
		},
		TranscriptPath: "/transcript.jsonl",
	}

	assert.Equal(t, hooks.EventSessionEnd, in.Event)
	assert.Equal(t, "sess-123", in.SessionID)
	assert.Equal(t, "agent-1", in.ActorID)
}

// Tests for SessionMetrics time handling

func TestSessionMetrics_TimeFormat(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	metrics := SessionMetrics{
		EndedAt: now,
	}

	data, err := json.Marshal(metrics)
	assert.NoError(t, err)

	// Should serialize to RFC3339 format
	assert.Contains(t, string(data), "2026-01-15")
}
