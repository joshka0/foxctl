package semantic

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

// These tests focus on the stability and invariants of the semantic index
// naming and chunking configuration helpers.

func TestFileEmbeddingName_StableAndEscapesSpecialCharacters(t *testing.T) {
	workspace := "workspace with spaces"
	path := "dir with spaces/fi#le?.go"

	name1 := FileEmbeddingName(workspace, path)
	name2 := FileEmbeddingName(workspace, path)

	if name1 != name2 {
		t.Fatalf("FileEmbeddingName should be stable across calls, got %q and %q", name1, name2)
	}

	expectedPrefix := "file://" + url.PathEscape(workspace) + "/"
	if !strings.HasPrefix(name1, expectedPrefix) {
		t.Fatalf("expected name to start with %q, got %q", expectedPrefix, name1)
	}

	// The raw path contains '#' and '?', but these should be percent-encoded
	// in the name for the non-chunked case.
	if strings.Contains(name1, "#") {
		t.Fatalf("unexpected '#' in file embedding name %q", name1)
	}
	if strings.Contains(name1, "?") {
		t.Fatalf("unexpected '?' in file embedding name %q", name1)
	}
}

func TestChunkEmbeddingName_StableAndStructured(t *testing.T) {
	workspace := "workspace with spaces"
	path := "dir/fi#le.go"
	chunkID := "chunk id"
	cfgHash := "hash+with&chars"

	name1 := ChunkEmbeddingName(workspace, path, chunkID, cfgHash)
	name2 := ChunkEmbeddingName(workspace, path, chunkID, cfgHash)

	if name1 != name2 {
		t.Fatalf("ChunkEmbeddingName should be stable across calls, got %q and %q", name1, name2)
	}

	expectedPrefix := "file://" + url.PathEscape(workspace) + "/"
	if !strings.HasPrefix(name1, expectedPrefix) {
		t.Fatalf("expected name to start with %q, got %q", expectedPrefix, name1)
	}

	if !strings.Contains(name1, "#chunk-") {
		t.Fatalf("expected chunk name to contain '#chunk-', got %q", name1)
	}
	if !strings.Contains(name1, "?cfg=") {
		t.Fatalf("expected chunk name to contain '?cfg=', got %q", name1)
	}

	parts := strings.SplitN(name1, "#chunk-", 2)
	if len(parts) != 2 {
		t.Fatalf("expected chunk name to contain '#chunk-' once, got %q", name1)
	}
	// Path portion before '#chunk-' should not contain raw '#' or '?' from the original path.
	if strings.ContainsAny(parts[0], "#?") {
		t.Fatalf("unexpected '#' or '?' in path portion of chunk name %q", name1)
	}
}

func TestChunkingConfigHash_SensitivityToRelevantFields(t *testing.T) {
	base := Config{
		ChunkBytes:        1024,
		ChunkOverlapBytes: 128,
		ProviderModel:     "model-a",
	}
	same := Config{
		ChunkBytes:        1024,
		ChunkOverlapBytes: 128,
		ProviderModel:     "model-a",
	}

	baseHash := base.ChunkingConfigHash()
	sameHash := same.ChunkingConfigHash()

	if baseHash == "" {
		t.Fatal("expected non-empty hash for non-zero ChunkBytes")
	}
	if baseHash != sameHash {
		t.Fatalf("expected identical configs to have the same hash, got %q and %q", baseHash, sameHash)
	}

	cases := []struct {
		name string
		cfg  Config
	}{
		{
			name: "different chunk bytes",
			cfg: Config{
				ChunkBytes:        2048,
				ChunkOverlapBytes: 128,
				ProviderModel:     "model-a",
			},
		},
		{
			name: "different overlap",
			cfg: Config{
				ChunkBytes:        1024,
				ChunkOverlapBytes: 64,
				ProviderModel:     "model-a",
			},
		},
		{
			name: "different model",
			cfg: Config{
				ChunkBytes:        1024,
				ChunkOverlapBytes: 128,
				ProviderModel:     "model-b",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cfg.ChunkingConfigHash() == baseHash {
				t.Fatalf("expected %s to change hash, but got same value %q", tc.name, baseHash)
			}
		})
	}

	noChunking := Config{
		ChunkBytes:        0,
		ChunkOverlapBytes: 128,
		ProviderModel:     "model-a",
	}
	if got := noChunking.ChunkingConfigHash(); got != "" {
		t.Fatalf("expected empty hash when ChunkBytes == 0, got %q", got)
	}
}

// =============================================================================
// P3.S2: Data Model Invariants & Round-Trip Tests
// =============================================================================

// TestFileEmbeddingResult_RoundTrip verifies that FileEmbeddingResult can be
// marshaled and unmarshaled without data loss.
func TestFileEmbeddingResult_RoundTrip(t *testing.T) {
	original := FileEmbeddingResult{
		Path:      "src/main.go",
		Digest:    "sha256:abc123",
		Language:  "go",
		SizeBytes: 1234,
		Embedding: []float32{0.1, 0.2, 0.3},
		Source: &EmbeddingSource{
			TaskID:   "task-123",
			ReviewID: "review-456",
			Actor:    "actor:system:semantic_indexer",
			Reason:   "post_review",
		},
	}

	data, err := MarshalResult(original)
	if err != nil {
		t.Fatalf("MarshalResult failed: %v", err)
	}

	result, err := UnmarshalFileResult(data)
	if err != nil {
		t.Fatalf("UnmarshalFileResult failed: %v", err)
	}

	// Verify fields
	if result.Path != original.Path {
		t.Errorf("Path mismatch: got %q, want %q", result.Path, original.Path)
	}
	if result.Digest != original.Digest {
		t.Errorf("Digest mismatch: got %q, want %q", result.Digest, original.Digest)
	}
	if result.Language != original.Language {
		t.Errorf("Language mismatch: got %q, want %q", result.Language, original.Language)
	}
	if result.SizeBytes != original.SizeBytes {
		t.Errorf("SizeBytes mismatch: got %d, want %d", result.SizeBytes, original.SizeBytes)
	}
	if len(result.Embedding) != len(original.Embedding) {
		t.Fatalf("Embedding length mismatch: got %d, want %d", len(result.Embedding), len(original.Embedding))
	}
	for i, v := range result.Embedding {
		if v != original.Embedding[i] {
			t.Errorf("Embedding[%d] mismatch: got %f, want %f", i, v, original.Embedding[i])
		}
	}
	if result.Source == nil {
		t.Fatal("Source should not be nil")
	}
	if result.Source.TaskID != original.Source.TaskID {
		t.Errorf("Source.TaskID mismatch: got %q, want %q", result.Source.TaskID, original.Source.TaskID)
	}
	if result.Source.ReviewID != original.Source.ReviewID {
		t.Errorf("Source.ReviewID mismatch: got %q, want %q", result.Source.ReviewID, original.Source.ReviewID)
	}
	if result.Source.Actor != original.Source.Actor {
		t.Errorf("Source.Actor mismatch: got %q, want %q", result.Source.Actor, original.Source.Actor)
	}
	if result.Source.Reason != original.Source.Reason {
		t.Errorf("Source.Reason mismatch: got %q, want %q", result.Source.Reason, original.Source.Reason)
	}
}

// TestFileEmbeddingResult_ChunkedFile verifies that chunked file entries have
// ChunkCount and ChunkingConfigHash set, but no Embedding.
func TestFileEmbeddingResult_ChunkedFile(t *testing.T) {
	chunked := FileEmbeddingResult{
		Path:               "large_file.txt",
		Digest:             "sha256:def456",
		ChunkCount:         5,
		ChunkingConfigHash: "abc123",
		// No Embedding for chunked files
	}

	data, err := MarshalResult(chunked)
	if err != nil {
		t.Fatalf("MarshalResult failed: %v", err)
	}

	result, err := UnmarshalFileResult(data)
	if err != nil {
		t.Fatalf("UnmarshalFileResult failed: %v", err)
	}

	if result.ChunkCount != 5 {
		t.Errorf("ChunkCount mismatch: got %d, want 5", result.ChunkCount)
	}
	if result.ChunkingConfigHash != "abc123" {
		t.Errorf("ChunkingConfigHash mismatch: got %q, want %q", result.ChunkingConfigHash, "abc123")
	}
	if len(result.Embedding) != 0 {
		t.Errorf("Embedding should be empty for chunked files, got %d elements", len(result.Embedding))
	}
}

// TestChunkEmbeddingResult_RoundTrip verifies that ChunkEmbeddingResult can be
// marshaled and unmarshaled without data loss.
func TestChunkEmbeddingResult_RoundTrip(t *testing.T) {
	original := ChunkEmbeddingResult{
		Path:      "large_file.txt",
		Digest:    "sha256:def456",
		Language:  "text",
		Embedding: []float32{0.4, 0.5, 0.6},
		Chunk: ChunkInfo{
			ID:    "0",
			Index: 0,
			Of:    3,
			Span: &ChunkSpan{
				Unit:  "byte",
				Start: 0,
				End:   1024,
			},
		},
		Source: &EmbeddingSource{
			TaskID:   "task-789",
			ReviewID: "review-abc",
			Actor:    "actor:system:semantic_indexer",
			Reason:   "initial_index",
		},
	}

	data, err := MarshalResult(original)
	if err != nil {
		t.Fatalf("MarshalResult failed: %v", err)
	}

	result, err := UnmarshalChunkResult(data)
	if err != nil {
		t.Fatalf("UnmarshalChunkResult failed: %v", err)
	}

	// Verify core fields
	if result.Path != original.Path {
		t.Errorf("Path mismatch: got %q, want %q", result.Path, original.Path)
	}
	if result.Digest != original.Digest {
		t.Errorf("Digest mismatch: got %q, want %q", result.Digest, original.Digest)
	}

	// Verify chunk info
	if result.Chunk.ID != original.Chunk.ID {
		t.Errorf("Chunk.ID mismatch: got %q, want %q", result.Chunk.ID, original.Chunk.ID)
	}
	if result.Chunk.Index != original.Chunk.Index {
		t.Errorf("Chunk.Index mismatch: got %d, want %d", result.Chunk.Index, original.Chunk.Index)
	}
	if result.Chunk.Of != original.Chunk.Of {
		t.Errorf("Chunk.Of mismatch: got %d, want %d", result.Chunk.Of, original.Chunk.Of)
	}

	// Verify span
	if result.Chunk.Span == nil {
		t.Fatal("Chunk.Span should not be nil")
	}
	if result.Chunk.Span.Unit != "byte" {
		t.Errorf("Chunk.Span.Unit mismatch: got %q, want %q", result.Chunk.Span.Unit, "byte")
	}
	if result.Chunk.Span.Start != 0 {
		t.Errorf("Chunk.Span.Start mismatch: got %d, want 0", result.Chunk.Span.Start)
	}
	if result.Chunk.Span.End != 1024 {
		t.Errorf("Chunk.Span.End mismatch: got %d, want 1024", result.Chunk.Span.End)
	}
}

// TestChunkEmbeddingResult_SpanUnitInvariants verifies that Span.Unit is always
// set to a valid value when Span is present.
func TestChunkEmbeddingResult_SpanUnitInvariants(t *testing.T) {
	cases := []struct {
		name     string
		span     *ChunkSpan
		wantUnit string
	}{
		{
			name:     "byte unit",
			span:     &ChunkSpan{Unit: "byte", Start: 0, End: 100},
			wantUnit: "byte",
		},
		{
			name:     "line unit",
			span:     &ChunkSpan{Unit: "line", Start: 1, End: 50},
			wantUnit: "line",
		},
		{
			name:     "nil span",
			span:     nil,
			wantUnit: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chunk := ChunkEmbeddingResult{
				Path: "test.txt",
				Chunk: ChunkInfo{
					ID:    "0",
					Index: 0,
					Of:    1,
					Span:  tc.span,
				},
			}

			data, err := MarshalResult(chunk)
			if err != nil {
				t.Fatalf("MarshalResult failed: %v", err)
			}

			result, err := UnmarshalChunkResult(data)
			if err != nil {
				t.Fatalf("UnmarshalChunkResult failed: %v", err)
			}

			if tc.span == nil {
				if result.Chunk.Span != nil {
					t.Error("expected nil Span")
				}
			} else {
				if result.Chunk.Span == nil {
					t.Fatal("expected non-nil Span")
				}
				if result.Chunk.Span.Unit != tc.wantUnit {
					t.Errorf("Span.Unit mismatch: got %q, want %q", result.Chunk.Span.Unit, tc.wantUnit)
				}
			}
		})
	}
}

// TestEmbeddingSource_OmitEmptyFields verifies that empty source fields are
// omitted from JSON output.
func TestEmbeddingSource_OmitEmptyFields(t *testing.T) {
	result := FileEmbeddingResult{
		Path: "test.go",
		Source: &EmbeddingSource{
			TaskID: "task-only",
			// ReviewID, Actor, Reason are empty
		},
	}

	data, err := MarshalResult(result)
	if err != nil {
		t.Fatalf("MarshalResult failed: %v", err)
	}

	// Verify JSON doesn't contain empty fields
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	source, ok := raw["source"].(map[string]any)
	if !ok {
		t.Fatal("expected source field in JSON")
	}

	if _, exists := source["review_id"]; exists {
		t.Error("review_id should be omitted when empty")
	}
	if _, exists := source["actor"]; exists {
		t.Error("actor should be omitted when empty")
	}
	if _, exists := source["reason"]; exists {
		t.Error("reason should be omitted when empty")
	}

	// task_id should be present
	if _, exists := source["task_id"]; !exists {
		t.Error("task_id should be present")
	}
}
