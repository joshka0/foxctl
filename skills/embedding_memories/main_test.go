package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skilltest"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
	"github.com/joshka0/foxctl/internal/storage/memory"
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

func TestMemoryInputsFromEntriesUsesFormattedContent(t *testing.T) {
	entries := []memory.NamedEntry{{
		Name:      "decision:test",
		Type:      "decision",
		Summary:   "Use queued embeddings",
		CreatedAt: time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC),
	}}

	inputs := memoryInputsFromEntries(entries)

	assert.Len(t, inputs, 1)
	assert.Equal(t, "decision:test", inputs[0].Name)
	assert.Equal(t, "decision", inputs[0].Type)
	assert.Equal(t, "[May 2026] [decision] Use queued embeddings", inputs[0].Content)
}

func TestMemoryInputsFromEntriesSkipsCodeOwnedMemoryTypes(t *testing.T) {
	createdAt := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	entries := []memory.NamedEntry{
		{
			Name:      "file://ws/cmd/root.go",
			Type:      semantic.FileEmbeddingType,
			Summary:   "cmd/root.go",
			CreatedAt: createdAt,
		},
		{
			Name:      "file://ws/cmd/root.go#chunk-6",
			Type:      semantic.FileEmbeddingChunkType,
			Summary:   "Chunk 6/15 of cmd/root.go",
			CreatedAt: createdAt,
		},
		{
			Name:      "symbol://ws/go:cmd::func Execute",
			Type:      symbol.SymbolType,
			Summary:   "Execute",
			CreatedAt: createdAt,
		},
		{
			Name:      "symbol-call://ws/go:cmd::Execute->Run",
			Type:      symbol.CallEdgeType,
			Summary:   "Execute calls Run",
			CreatedAt: createdAt,
		},
		{
			Name:      "file-meta://ws/cmd/root.go",
			Type:      symbol.FileMetaType,
			Summary:   "cmd/root.go metadata",
			CreatedAt: createdAt,
		},
		{
			Name:      "file://ws/cmd/root.go",
			Type:      symbol.FileSummaryType,
			Summary:   "cmd/root.go summary",
			CreatedAt: createdAt,
		},
		{
			Name:      "symbol-summary://ws/go:cmd::Execute",
			Type:      symbol.SymbolSummaryType,
			Summary:   "Execute summary",
			CreatedAt: createdAt,
		},
		{
			Name:      "decision:test",
			Type:      "decision",
			Summary:   "Use memory embeddings for human-authored memories",
			CreatedAt: createdAt,
		},
	}

	inputs := memoryInputsFromEntries(entries)

	assert.Len(t, inputs, 1)
	assert.Equal(t, "decision:test", inputs[0].Name)
	assert.Equal(t, "decision", inputs[0].Type)
	assert.Equal(t, "[May 2026] [decision] Use memory embeddings for human-authored memories", inputs[0].Content)
}

func TestMemoryInputsFromEntriesKeepsRealMemoryTypes(t *testing.T) {
	createdAt := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	entries := []memory.NamedEntry{
		{Name: "decision:test", Type: "decision", Summary: "decision", CreatedAt: createdAt},
		{Name: "gotcha:test", Type: "gotcha", Summary: "gotcha", CreatedAt: createdAt},
		{Name: "learning:test", Type: "learning", Summary: "learning", CreatedAt: createdAt},
		{Name: "note:test", Type: "note", Summary: "note", CreatedAt: createdAt},
		{Name: "fact:test", Type: "fact", Summary: "fact", CreatedAt: createdAt},
	}

	inputs := memoryInputsFromEntries(entries)

	assert.Len(t, inputs, len(entries))
	for i, input := range inputs {
		assert.Equal(t, entries[i].Name, input.Name)
		assert.Equal(t, entries[i].Type, input.Type)
	}
}

func TestEmbeddingMemoriesDryRunSkipsCodeOwnedMemoryTypes(t *testing.T) {
	ctx := context.Background()
	var stdout bytes.Buffer
	rc, cleanup := skilltest.NewTestRunContext(t, &stdout, nil)
	defer cleanup()
	memStore, err := memory.OpenWithConfig(ctx, rc.Config)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer memStore.Close()
	seedMemoryResult := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-07T00:00:00Z"},"error":{}}`)
	for _, entry := range []struct {
		name string
		typ  string
	}{
		{"file://ws/root.go", semantic.FileEmbeddingType},
		{"symbol://ws/root.go:Run", symbol.SymbolType},
		{"decision:test", "decision"},
		{"gotcha:test", "gotcha"},
	} {
		if _, err := memStore.SaveFromResult(ctx, entry.name, entry.typ, rc.Workspace, entry.name, seedMemoryResult); err != nil {
			t.Fatalf("save memory %s: %v", entry.name, err)
		}
	}

	if err := run(ctx, rc, Input{Workspace: rc.Workspace, DryRun: true}); err != nil {
		t.Fatalf("run dry-run: %v", err)
	}

	_, output, err := skilltest.DecodeEnvelopeData[Output](stdout.Bytes())
	if err != nil {
		t.Fatalf("decode output: %v; raw=%s", err, stdout.String())
	}
	assert.Equal(t, 4, output.MemoriesFound)
	assert.Equal(t, 2, output.Skipped)
	statusByName := map[string]string{}
	for _, result := range output.Memories {
		statusByName[result.Name] = result.Status
	}
	assert.Equal(t, "skipped", statusByName["file://ws/root.go"])
	assert.Equal(t, "skipped", statusByName["symbol://ws/root.go:Run"])
	assert.Equal(t, "dry_run", statusByName["decision:test"])
	assert.Equal(t, "dry_run", statusByName["gotcha:test"])
}

func TestEmbeddingMemoriesEnqueueSkipsCodeOwnedMemoryTypes(t *testing.T) {
	ctx := context.Background()
	var stdout bytes.Buffer
	rc, cleanup := skilltest.NewTestRunContext(t, &stdout, nil)
	defer cleanup()
	memStore, err := memory.OpenWithConfig(ctx, rc.Config)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer memStore.Close()
	seedMemoryResult := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-07T00:00:00Z"},"error":{}}`)
	for _, entry := range []struct {
		name string
		typ  string
	}{
		{"chunk://ws/root.go#1", semantic.FileEmbeddingChunkType},
		{"learning:test", "learning"},
		{"note:test", "note"},
		{"fact:test", "fact"},
	} {
		if _, err := memStore.SaveFromResult(ctx, entry.name, entry.typ, rc.Workspace, entry.name, seedMemoryResult); err != nil {
			t.Fatalf("save memory %s: %v", entry.name, err)
		}
	}

	if err := run(ctx, rc, Input{Workspace: rc.Workspace, Enqueue: true, ProcessAll: true, BatchSize: 2}); err != nil {
		t.Fatalf("run enqueue: %v", err)
	}

	_, output, err := skilltest.DecodeEnvelopeData[Output](stdout.Bytes())
	if err != nil {
		t.Fatalf("decode output: %v; raw=%s", err, stdout.String())
	}
	assert.Equal(t, 3, output.Queued)
	assert.Equal(t, 1, output.Skipped)

	queueStore, err := embedding.OpenStore(ctx, rc.Config.Paths.Cache)
	if err != nil {
		t.Fatalf("open queue store: %v", err)
	}
	defer queueStore.Close()
	var queuedNames []string
	for {
		job, err := queueStore.ClaimNext(ctx)
		if err != nil {
			t.Fatalf("claim job: %v", err)
		}
		if job == nil {
			break
		}
		queuedNames = append(queuedNames, job.MemoryName)
	}
	assert.ElementsMatch(t, []string{"learning:test", "note:test", "fact:test"}, queuedNames)
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

func TestOutput_SuccessfulRun(t *testing.T) {
	output := Output{
		Workspace:     "/workspace",
		MemoriesFound: 10,
		Embedded:      8,
		Queued:        7,
		Skipped:       1,
		Errors:        1,
		Remaining:     0,
		BatchCount:    2,
		DurationMs:    1500,
	}

	assert.Equal(t, "/workspace", output.Workspace)
	assert.Equal(t, 10, output.MemoriesFound)
	assert.Equal(t, 8, output.Embedded)
	assert.Equal(t, 7, output.Queued)
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
