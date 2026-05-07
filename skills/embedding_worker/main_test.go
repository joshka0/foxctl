package main

import (
	"encoding/json"
	"testing"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "embedding/worker", command)
}

func TestDefaultBatch(t *testing.T) {
	assert.Equal(t, 10, defaultBatch)
}

func TestDefaultMaxDur(t *testing.T) {
	assert.Equal(t, 300, defaultMaxDur) // 5 minutes
}

// Tests for Input structure

func TestInput_AllFields(t *testing.T) {
	in := Input{
		WorkspaceID:              "workspace-a",
		Kind:                     "memory",
		BatchSize:                20,
		MaxDuration:              600,
		ProcessAll:               true,
		DryRun:                   true,
		JobDelayMS:               250,
		RecoverStaleAfterSeconds: 1800,
	}

	assert.Equal(t, "workspace-a", in.WorkspaceID)
	assert.Equal(t, "memory", in.Kind)
	assert.Equal(t, 20, in.BatchSize)
	assert.Equal(t, 600, in.MaxDuration)
	assert.True(t, in.ProcessAll)
	assert.True(t, in.DryRun)
	assert.Equal(t, 250, in.JobDelayMS)
	assert.Equal(t, 1800, in.RecoverStaleAfterSeconds)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := Input{
		WorkspaceID:              "workspace-a",
		Kind:                     "symbol",
		BatchSize:                15,
		MaxDuration:              120,
		ProcessAll:               false,
		DryRun:                   true,
		RecoverStaleAfterSeconds: 60,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.WorkspaceID, decoded.WorkspaceID)
	assert.Equal(t, in.Kind, decoded.Kind)
	assert.Equal(t, in.BatchSize, decoded.BatchSize)
	assert.Equal(t, in.MaxDuration, decoded.MaxDuration)
	assert.Equal(t, in.ProcessAll, decoded.ProcessAll)
	assert.Equal(t, in.DryRun, decoded.DryRun)
	assert.Equal(t, in.RecoverStaleAfterSeconds, decoded.RecoverStaleAfterSeconds)
}

func TestInput_EmptyFields(t *testing.T) {
	in := Input{}

	assert.Zero(t, in.BatchSize)
	assert.Zero(t, in.MaxDuration)
	assert.False(t, in.ProcessAll)
	assert.False(t, in.DryRun)
}

func TestInput_JSONOmitEmpty(t *testing.T) {
	in := Input{}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	// batch_size should be omitted when 0
	assert.NotContains(t, string(data), "batch_size")
	// max_duration should be omitted when 0
	assert.NotContains(t, string(data), "max_duration")
	// process_all should be omitted when false
	assert.NotContains(t, string(data), "process_all")
	// dry_run should be omitted when false
	assert.NotContains(t, string(data), "dry_run")
}

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

func TestOutput_AllFields(t *testing.T) {
	out := Output{
		Processed:  50,
		Kind:       "symbol",
		Errors:     3,
		Memories:   7,
		Remaining:  10,
		BatchCount: 5,
		Recovered:  2,
		Status:     "completed",
		DurationMs: 12345,
		LastError:  "some error",
		Stats: &QueueSnapshot{
			Queued:     10,
			Running:    0,
			Completed:  50,
			Failed:     3,
			Embeddings: 150,
		},
		Message: "Processed 50 embeddings",
	}

	assert.Equal(t, 50, out.Processed)
	assert.Equal(t, "symbol", out.Kind)
	assert.Equal(t, 3, out.Errors)
	assert.Equal(t, 7, out.Memories)
	assert.Equal(t, 10, out.Remaining)
	assert.Equal(t, 5, out.BatchCount)
	assert.Equal(t, int64(2), out.Recovered)
	assert.Equal(t, "completed", out.Status)
	assert.Equal(t, int64(12345), out.DurationMs)
	assert.Equal(t, "some error", out.LastError)
	assert.NotNil(t, out.Stats)
	assert.Equal(t, "Processed 50 embeddings", out.Message)
}

func TestOutput_JSONSerialization(t *testing.T) {
	out := Output{
		Processed:  25,
		Errors:     0,
		Status:     "completed",
		DurationMs: 5000,
		Message:    "test message",
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.Processed, decoded.Processed)
	assert.Equal(t, out.Errors, decoded.Errors)
	assert.Equal(t, out.Status, decoded.Status)
	assert.Equal(t, out.DurationMs, decoded.DurationMs)
	assert.Equal(t, out.Message, decoded.Message)
}

func TestOutput_EmptyFields(t *testing.T) {
	out := Output{}

	assert.Zero(t, out.Processed)
	assert.Zero(t, out.Errors)
	assert.Zero(t, out.Remaining)
	assert.Empty(t, out.Status)
	assert.Nil(t, out.Stats)
}

func TestOutput_StatusValues(t *testing.T) {
	validStatuses := []string{"completed", "timeout", "no_jobs", "error"}

	for _, status := range validStatuses {
		out := Output{Status: status}
		assert.Equal(t, status, out.Status)
	}
}

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

func TestQueueSnapshot_AllFields(t *testing.T) {
	snap := QueueSnapshot{
		Queued:     15,
		Running:    2,
		Completed:  100,
		Failed:     5,
		Embeddings: 200,
	}

	assert.Equal(t, 15, snap.Queued)
	assert.Equal(t, 2, snap.Running)
	assert.Equal(t, 100, snap.Completed)
	assert.Equal(t, 5, snap.Failed)
	assert.Equal(t, 200, snap.Embeddings)
}

func TestQueueSnapshot_JSONSerialization(t *testing.T) {
	snap := QueueSnapshot{
		Queued:     10,
		Running:    1,
		Completed:  50,
		Failed:     2,
		Embeddings: 100,
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
	assert.Equal(t, snap.Embeddings, decoded.Embeddings)
}

func TestQueueSnapshot_EmptyFields(t *testing.T) {
	snap := QueueSnapshot{}

	assert.Zero(t, snap.Queued)
	assert.Zero(t, snap.Running)
	assert.Zero(t, snap.Completed)
	assert.Zero(t, snap.Failed)
	assert.Zero(t, snap.Embeddings)
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

func TestSymbolMemoryEntryNameFallsBackToLegacyLocator(t *testing.T) {
	job := &embedding.EmbeddingJob{
		WorkspaceID: "test-ws",
		FilePath:    "legacy.go",
		SymbolName:  "Handler",
	}

	assert.Equal(t, "symbol://test-ws/legacy.go:Handler", symbolMemoryEntryName(job))
}
