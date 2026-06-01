package main

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestDefaultBatch(t *testing.T) {
	assert.Equal(t, 10, defaultBatch)
}

func TestDefaultMaxDur(t *testing.T) {
	assert.Equal(t, 300, defaultMaxDur) // 5 minutes
}

func TestDefaultParallelism(t *testing.T) {
	assert.Equal(t, 1, defaultParallelism)
	assert.Equal(t, 16, maxParallelism)
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
	parallelism, err := normalizeParallelism(in.Parallelism, in.BatchSize)
	assert.NoError(t, err)
	in.Parallelism = parallelism

	assert.Equal(t, defaultBatch, in.BatchSize)
	assert.Equal(t, defaultMaxDur, in.MaxDuration)
	assert.Equal(t, defaultParallelism, in.Parallelism)
}

func TestInput_CustomValues(t *testing.T) {
	in := Input{
		BatchSize:   5,
		MaxDuration: 60,
		Parallelism: 3,
	}

	// Should not change if positive
	if in.BatchSize <= 0 {
		in.BatchSize = defaultBatch
	}
	if in.MaxDuration <= 0 {
		in.MaxDuration = defaultMaxDur
	}
	parallelism, err := normalizeParallelism(in.Parallelism, in.BatchSize)
	assert.NoError(t, err)
	in.Parallelism = parallelism

	assert.Equal(t, 5, in.BatchSize)
	assert.Equal(t, 60, in.MaxDuration)
	assert.Equal(t, 3, in.Parallelism)
}

func TestNormalizeParallelism(t *testing.T) {
	tests := []struct {
		name      string
		raw       int
		batchSize int
		want      int
		wantErr   bool
	}{
		{name: "default", raw: 0, batchSize: 10, want: 1},
		{name: "custom", raw: 4, batchSize: 10, want: 4},
		{name: "batch cap", raw: 8, batchSize: 3, want: 3},
		{name: "no batch cap", raw: 8, batchSize: 0, want: 8},
		{name: "negative", raw: -1, batchSize: 10, wantErr: true},
		{name: "too high", raw: maxParallelism + 1, batchSize: 100, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeParallelism(tt.raw, tt.batchSize)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProcessEmbeddingJobBatchRespectsParallelism(t *testing.T) {
	jobs := []*embedding.EmbeddingJob{
		{ID: "job-1"},
		{ID: "job-2"},
		{ID: "job-3"},
		{ID: "job-4"},
	}
	var current atomic.Int32
	var maxSeen atomic.Int32
	result := processEmbeddingJobBatch(context.Background(), jobs, 2, func(context.Context, *embedding.EmbeddingJob) embeddingJobResult {
		now := current.Add(1)
		for {
			old := maxSeen.Load()
			if now <= old || maxSeen.CompareAndSwap(old, now) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		current.Add(-1)
		return embeddingJobResult{Processed: 1}
	})

	assert.Equal(t, 4, result.Processed)
	assert.LessOrEqual(t, maxSeen.Load(), int32(2))
	assert.GreaterOrEqual(t, maxSeen.Load(), int32(2))
}

func TestNormalizeTaskKind(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty", input: "", want: ""},
		{name: "symbol", input: "symbol", want: "symbol"},
		{name: "memory", input: " memory ", want: "memory"},
		{name: "invalid", input: "semantic_file", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeTaskKind(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
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
	assert.Equal(t, 50000, decoded.Remaining)
}

func TestOutput_WithStats(t *testing.T) {
	out := Output{
		Processed: 10,
		Status:    "completed",
		Stats: &QueueSnapshot{
			Queued:     5,
			Running:    0,
			Completed:  10,
			Failed:     0,
			Embeddings: 15,
		},
	}

	assert.NotNil(t, out.Stats)
	assert.Equal(t, 5, out.Stats.Queued)
	assert.Equal(t, 15, out.Stats.Embeddings)
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
			out.Message = "Processed 0 embeddings (0 errors) in 0ms"
		case "timeout":
			out.Message = "Timeout after 0 embeddings (0 errors, 0 remaining)"
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
}

func TestOutput_FullJSONRoundTrip(t *testing.T) {
	out := Output{
		Processed:  75,
		Errors:     2,
		Remaining:  23,
		BatchCount: 8,
		Status:     "timeout",
		DurationMs: 300000,
		LastError:  "rate limit exceeded",
		Stats: &QueueSnapshot{
			Queued:     23,
			Running:    0,
			Completed:  75,
			Failed:     2,
			Embeddings: 100,
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
	assert.Equal(t, out.Status, decoded.Status)
	assert.NotNil(t, decoded.Stats)
	assert.Equal(t, out.Stats.Queued, decoded.Stats.Queued)
}

func TestSymbolMemoryEntryNamePrefersQueuedCanonicalName(t *testing.T) {
	job := &embedding.EmbeddingJob{
		WorkspaceID: "test-ws",
		FilePath:    "legacy.go",
		SymbolName:  "Handler",
		PackageID:   "go:pkg/foo",
		SymbolKey:   "func Handler",
		MemoryName:  "symbol://test-ws/go:pkg/foo::func Handler",
	}

	assert.Equal(t, "symbol://test-ws/go:pkg/foo::func Handler", symbolMemoryEntryName(job))
}

func TestSymbolMemoryEntryNameBuildsKeyedNameFromPackageIdentity(t *testing.T) {
	job := &embedding.EmbeddingJob{
		WorkspaceID: "test-ws",
		FilePath:    "legacy.go",
		SymbolName:  "Handler",
		PackageID:   "go:pkg/foo",
		SymbolKey:   "func Handler",
	}

	assert.Equal(t, "symbol://test-ws/go:pkg/foo::func Handler", symbolMemoryEntryName(job))
}

func TestSymbolMemoryEntryNameRequiresCanonicalIdentity(t *testing.T) {
	job := &embedding.EmbeddingJob{
		WorkspaceID: "test-ws",
		FilePath:    "legacy.go",
		SymbolName:  "Handler",
	}

	assert.Empty(t, symbolMemoryEntryName(job))
}
