package main

import (
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/stretchr/testify/assert"
)

// Tests for formatMemoryContent helper

func TestFormatMemoryContent_WithSummary(t *testing.T) {
	entry := memory.NamedEntry{
		Name:      "test-memory",
		Type:      "gotcha",
		Summary:   "Watch out for this edge case",
		CreatedAt: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	result := formatMemoryContent(entry)

	assert.Contains(t, result, "[Jan 2026]")
	assert.Contains(t, result, "[gotcha]")
	assert.Contains(t, result, "Watch out for this edge case")
}

func TestFormatMemoryContent_WithoutSummary(t *testing.T) {
	entry := memory.NamedEntry{
		Name:      "important-memory",
		Type:      "decision",
		CreatedAt: time.Date(2025, 12, 1, 10, 0, 0, 0, time.UTC),
	}

	result := formatMemoryContent(entry)

	assert.Contains(t, result, "[Dec 2025]")
	assert.Contains(t, result, "[decision]")
	assert.Contains(t, result, "important-memory")
}

func TestFormatMemoryContent_WithoutType(t *testing.T) {
	entry := memory.NamedEntry{
		Name:      "untyped-memory",
		Summary:   "Some content",
		CreatedAt: time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC),
	}

	result := formatMemoryContent(entry)

	assert.Contains(t, result, "[Mar 2026]")
	assert.Contains(t, result, "[note]") // Default type
	assert.Contains(t, result, "Some content")
}

func TestFormatMemoryContent_Format(t *testing.T) {
	entry := memory.NamedEntry{
		Name:      "format-test",
		Type:      "insight",
		Summary:   "Test summary",
		CreatedAt: time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC),
	}

	result := formatMemoryContent(entry)

	// Check exact format: [Jun 2026] [insight] Test summary
	assert.Equal(t, "[Jun 2026] [insight] Test summary", result)
}

func TestFormatMemoryContent_EmptySummaryUsesName(t *testing.T) {
	entry := memory.NamedEntry{
		Name:      "my-memory-name",
		Type:      "note",
		Summary:   "",
		CreatedAt: time.Date(2026, 2, 5, 10, 0, 0, 0, time.UTC),
	}

	result := formatMemoryContent(entry)

	assert.Contains(t, result, "my-memory-name")
}

func TestFormatMemoryContent_DifferentMonths(t *testing.T) {
	tests := []struct {
		month    time.Month
		expected string
	}{
		{time.January, "Jan"},
		{time.February, "Feb"},
		{time.March, "Mar"},
		{time.April, "Apr"},
		{time.May, "May"},
		{time.June, "Jun"},
		{time.July, "Jul"},
		{time.August, "Aug"},
		{time.September, "Sep"},
		{time.October, "Oct"},
		{time.November, "Nov"},
		{time.December, "Dec"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			entry := memory.NamedEntry{
				Name:      "month-test",
				Type:      "note",
				Summary:   "Content",
				CreatedAt: time.Date(2026, tt.month, 15, 10, 0, 0, 0, time.UTC),
			}

			result := formatMemoryContent(entry)

			assert.Contains(t, result, "["+tt.expected+" 2026]")
		})
	}
}

// Tests for Input structure

func TestInput_DefaultBatchSize(t *testing.T) {
	in := Input{}
	// Default applied in run() when <= 0
	if in.BatchSize <= 0 {
		in.BatchSize = defaultBatchMax
	}
	assert.Equal(t, 10, in.BatchSize)
}

func TestInput_PreservesCustomBatchSize(t *testing.T) {
	in := Input{BatchSize: 25}
	// Only apply default if <= 0
	if in.BatchSize <= 0 {
		in.BatchSize = defaultBatchMax
	}
	assert.Equal(t, 25, in.BatchSize)
}

func TestInput_AllFields(t *testing.T) {
	in := Input{
		Workspace:  "/test/workspace",
		BatchSize:  20,
		ProcessAll: true,
		DryRun:     true,
	}

	assert.Equal(t, "/test/workspace", in.Workspace)
	assert.Equal(t, 20, in.BatchSize)
	assert.True(t, in.ProcessAll)
	assert.True(t, in.DryRun)
}

// Tests for Output structure

func TestOutput_SuccessfulRun(t *testing.T) {
	output := Output{
		Workspace:     "/workspace",
		MemoriesFound: 10,
		Embedded:      8,
		Skipped:       1,
		Errors:        1,
		Remaining:     0,
		BatchCount:    2,
		DurationMs:    1500,
	}

	assert.Equal(t, "/workspace", output.Workspace)
	assert.Equal(t, 10, output.MemoriesFound)
	assert.Equal(t, 8, output.Embedded)
	assert.Equal(t, 1, output.Skipped)
	assert.Equal(t, 1, output.Errors)
	assert.Equal(t, 2, output.BatchCount)
	assert.Equal(t, int64(1500), output.DurationMs)
}

func TestOutput_WithMemoryResults(t *testing.T) {
	output := Output{
		Memories: []MemoryResult{
			{Name: "mem-1", Type: "gotcha", Status: "embedded", Dimensions: 1024},
			{Name: "mem-2", Type: "decision", Status: "skipped", Message: "No content"},
		},
	}

	assert.Len(t, output.Memories, 2)
	assert.Equal(t, "embedded", output.Memories[0].Status)
	assert.Equal(t, 1024, output.Memories[0].Dimensions)
	assert.Equal(t, "skipped", output.Memories[1].Status)
}

func TestOutput_WithErrors(t *testing.T) {
	output := Output{
		Errors: 2,
		ErrorDetails: []string{
			"mem-1: API rate limit exceeded",
			"mem-2: Invalid content",
		},
	}

	assert.Equal(t, 2, output.Errors)
	assert.Len(t, output.ErrorDetails, 2)
	assert.Contains(t, output.ErrorDetails[0], "rate limit")
}

// Tests for MemoryResult structure

func TestMemoryResult_Embedded(t *testing.T) {
	result := MemoryResult{
		Name:       "test-memory",
		Type:       "gotcha",
		Status:     "embedded",
		Dimensions: 1024,
	}

	assert.Equal(t, "test-memory", result.Name)
	assert.Equal(t, "gotcha", result.Type)
	assert.Equal(t, "embedded", result.Status)
	assert.Equal(t, 1024, result.Dimensions)
	assert.Empty(t, result.Message)
}

func TestMemoryResult_Skipped(t *testing.T) {
	result := MemoryResult{
		Name:    "empty-memory",
		Type:    "note",
		Status:  "skipped",
		Message: "No content to embed",
	}

	assert.Equal(t, "skipped", result.Status)
	assert.Equal(t, "No content to embed", result.Message)
}

func TestMemoryResult_Error(t *testing.T) {
	result := MemoryResult{
		Name:    "failed-memory",
		Type:    "decision",
		Status:  "error",
		Message: "Embedding API failed",
	}

	assert.Equal(t, "error", result.Status)
	assert.Equal(t, "Embedding API failed", result.Message)
}

func TestMemoryResult_DryRun(t *testing.T) {
	result := MemoryResult{
		Name:    "dry-run-memory",
		Type:    "insight",
		Status:  "dry_run",
		Message: "Would embed",
	}

	assert.Equal(t, "dry_run", result.Status)
}

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "embedding/memories", command)
}

func TestDefaultBatchMax(t *testing.T) {
	assert.Equal(t, 10, defaultBatchMax)
}

// Tests for status values

func TestStatusValues(t *testing.T) {
	validStatuses := []string{"embedded", "skipped", "error", "dry_run"}
	for _, status := range validStatuses {
		result := MemoryResult{Status: status}
		assert.NotEmpty(t, result.Status)
	}
}
