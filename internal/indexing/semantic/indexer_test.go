package semantic

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/indexing"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/rs/zerolog"
)

func setupTestIndexer(t *testing.T, cfg Config) (*Indexer, *memory.Store, string) {
	t.Helper()

	// Create temp directories
	tmpDir := t.TempDir()
	workspaceDir := filepath.Join(tmpDir, "workspace")
	storageDir := filepath.Join(tmpDir, "storage")
	casDir := filepath.Join(tmpDir, "cas")

	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Open memory store
	ctx := context.Background()
	store, err := memory.Open(ctx, storageDir, casDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Create indexer with no-op provider
	provider := NewNoOpProvider("test-model", 384)
	logger := zerolog.Nop()

	idx := NewIndexer(cfg, store, provider, workspaceDir, logger)

	return idx, store, workspaceDir
}

func createTestFile(t *testing.T, dir, path, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestIndexer_ID(t *testing.T) {
	idx, _, _ := setupTestIndexer(t, Config{Enabled: true})
	if idx.ID() != IndexerID {
		t.Errorf("expected ID %q, got %q", IndexerID, idx.ID())
	}
}

func TestIndexer_Index_Disabled(t *testing.T) {
	idx, _, _ := setupTestIndexer(t, Config{Enabled: false})

	event := indexing.PostReviewEvent{
		WorkspaceID: "ws-1",
		Files: []indexing.FileChange{
			{Path: "foo.go", ChangeKind: indexing.ChangeKindModified},
		},
	}

	result, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if result.FilesSkipped != 1 {
		t.Errorf("expected 1 file skipped, got %d", result.FilesSkipped)
	}
	if result.FilesIndexed != 0 {
		t.Errorf("expected 0 files indexed, got %d", result.FilesIndexed)
	}
}

func TestIndexer_Index_SingleFile(t *testing.T) {
	idx, store, workspaceDir := setupTestIndexer(t, Config{Enabled: true})

	// Create test file
	createTestFile(t, workspaceDir, "main.go", "package main\n\nfunc main() {}\n")

	event := indexing.PostReviewEvent{
		WorkspaceID: "ws-test",
		TaskID:      "task-123",
		ReviewID:    "review-456",
		Reason:      "post_review",
		Files: []indexing.FileChange{
			{Path: "main.go", ChangeKind: indexing.ChangeKindModified, Language: "go"},
		},
	}

	result, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if result.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed, got %d", result.FilesIndexed)
	}
	if result.FilesFailed != 0 {
		t.Errorf("expected 0 files failed, got %d", result.FilesFailed)
	}

	// Verify entry was saved
	ctx := context.Background()
	name := FileEmbeddingName("ws-test", "main.go")
	entry, err := store.Get(ctx, name, "ws-test")
	if err != nil {
		t.Fatalf("Get entry failed: %v", err)
	}
	if entry.Type != FileEmbeddingType {
		t.Errorf("expected type %q, got %q", FileEmbeddingType, entry.Type)
	}

	// Verify result metadata
	fileResult, err := UnmarshalFileResult(entry.Result)
	if err != nil {
		t.Fatalf("Unmarshal result failed: %v", err)
	}
	if fileResult.Path != "main.go" {
		t.Errorf("expected path 'main.go', got %q", fileResult.Path)
	}
	if fileResult.Language != "go" {
		t.Errorf("expected language 'go', got %q", fileResult.Language)
	}
	if fileResult.Source == nil {
		t.Error("expected source to be set")
	} else {
		if fileResult.Source.TaskID != "task-123" {
			t.Errorf("expected task_id 'task-123', got %q", fileResult.Source.TaskID)
		}
		if fileResult.Source.ReviewID != "review-456" {
			t.Errorf("expected review_id 'review-456', got %q", fileResult.Source.ReviewID)
		}
	}
}

func TestIndexer_Index_ChunkedFile(t *testing.T) {
	cfg := Config{
		Enabled:           true,
		ChunkBytes:        50, // Small chunks for testing
		ChunkOverlapBytes: 10,
		ProviderModel:     "test-model",
	}
	idx, store, workspaceDir := setupTestIndexer(t, cfg)

	// Create a file larger than chunk size
	content := "This is a test file with enough content to trigger chunking behavior in the semantic indexer."
	createTestFile(t, workspaceDir, "large.txt", content)

	event := indexing.PostReviewEvent{
		WorkspaceID: "ws-chunked",
		Files: []indexing.FileChange{
			{Path: "large.txt", ChangeKind: indexing.ChangeKindAdded, SizeBytes: int64(len(content))},
		},
	}

	result, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if result.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed, got %d", result.FilesIndexed)
	}

	// Verify parent entry was saved
	ctx := context.Background()
	name := FileEmbeddingName("ws-chunked", "large.txt")
	entry, err := store.Get(ctx, name, "ws-chunked")
	if err != nil {
		t.Fatalf("Get parent entry failed: %v", err)
	}

	fileResult, err := UnmarshalFileResult(entry.Result)
	if err != nil {
		t.Fatalf("Unmarshal result failed: %v", err)
	}
	if fileResult.ChunkCount == 0 {
		t.Error("expected chunk count to be set")
	}
	if fileResult.ChunkingConfigHash == "" {
		t.Error("expected chunking config hash to be set")
	}

	// Verify chunk entries were saved
	configHash := cfg.ChunkingConfigHash()
	chunkName := ChunkEmbeddingName("ws-chunked", "large.txt", "0", configHash)
	chunkEntry, err := store.Get(ctx, chunkName, "ws-chunked")
	if err != nil {
		t.Fatalf("Get chunk entry failed: %v", err)
	}
	if chunkEntry.Type != FileEmbeddingChunkType {
		t.Errorf("expected type %q, got %q", FileEmbeddingChunkType, chunkEntry.Type)
	}

	chunkResult, err := UnmarshalChunkResult(chunkEntry.Result)
	if err != nil {
		t.Fatalf("Unmarshal chunk result failed: %v", err)
	}
	if chunkResult.Chunk.Index != 0 {
		t.Errorf("expected chunk index 0, got %d", chunkResult.Chunk.Index)
	}
	if chunkResult.Chunk.Of != fileResult.ChunkCount {
		t.Errorf("expected chunk.of %d, got %d", fileResult.ChunkCount, chunkResult.Chunk.Of)
	}
}

func TestIndexer_Index_DeletedFile(t *testing.T) {
	idx, store, workspaceDir := setupTestIndexer(t, Config{Enabled: true})

	// First, create and index a file
	createTestFile(t, workspaceDir, "to_delete.go", "package main")

	ctx := context.Background()
	addEvent := indexing.PostReviewEvent{
		WorkspaceID: "ws-delete",
		Files: []indexing.FileChange{
			{Path: "to_delete.go", ChangeKind: indexing.ChangeKindAdded},
		},
	}
	_, err := idx.Index(ctx, addEvent)
	if err != nil {
		t.Fatalf("Initial index failed: %v", err)
	}

	// Verify entry exists
	name := FileEmbeddingName("ws-delete", "to_delete.go")
	_, err = store.Get(ctx, name, "ws-delete")
	if err != nil {
		t.Fatalf("Entry should exist after indexing: %v", err)
	}

	// Now delete the file
	deleteEvent := indexing.PostReviewEvent{
		WorkspaceID: "ws-delete",
		Files: []indexing.FileChange{
			{Path: "to_delete.go", ChangeKind: indexing.ChangeKindDeleted},
		},
	}

	result, err := idx.Index(ctx, deleteEvent)
	if err != nil {
		t.Fatalf("Delete index failed: %v", err)
	}
	if result.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed (deleted), got %d", result.FilesIndexed)
	}

	// Verify entry is deleted
	_, err = store.Get(ctx, name, "ws-delete")
	if err == nil {
		t.Error("expected entry to be deleted")
	}
}

func TestIndexer_Index_MultipleFiles(t *testing.T) {
	idx, _, workspaceDir := setupTestIndexer(t, Config{Enabled: true})

	// Create multiple test files
	createTestFile(t, workspaceDir, "file1.go", "package main")
	createTestFile(t, workspaceDir, "file2.go", "package lib")
	createTestFile(t, workspaceDir, "file3.go", "package util")

	event := indexing.PostReviewEvent{
		WorkspaceID: "ws-multi",
		Files: []indexing.FileChange{
			{Path: "file1.go", ChangeKind: indexing.ChangeKindModified},
			{Path: "file2.go", ChangeKind: indexing.ChangeKindAdded},
			{Path: "file3.go", ChangeKind: indexing.ChangeKindModified},
		},
	}

	result, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if result.FilesIndexed != 3 {
		t.Errorf("expected 3 files indexed, got %d", result.FilesIndexed)
	}
}

func TestIndexer_Index_FileNotFound(t *testing.T) {
	idx, _, _ := setupTestIndexer(t, Config{Enabled: true})

	event := indexing.PostReviewEvent{
		WorkspaceID: "ws-notfound",
		Files: []indexing.FileChange{
			{Path: "nonexistent.go", ChangeKind: indexing.ChangeKindModified},
		},
	}

	result, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Index should not return error: %v", err)
	}
	if result.FilesFailed != 1 {
		t.Errorf("expected 1 file failed, got %d", result.FilesFailed)
	}
	if len(result.Failures) != 1 {
		t.Errorf("expected 1 failure, got %d", len(result.Failures))
	}
}

func TestConfig_ChunkingConfigHash(t *testing.T) {
	cfg1 := Config{ChunkBytes: 1000, ChunkOverlapBytes: 100, ProviderModel: "model-a"}
	cfg2 := Config{ChunkBytes: 1000, ChunkOverlapBytes: 100, ProviderModel: "model-a"}
	cfg3 := Config{ChunkBytes: 2000, ChunkOverlapBytes: 100, ProviderModel: "model-a"}
	cfg4 := Config{ChunkBytes: 0} // No chunking

	hash1 := cfg1.ChunkingConfigHash()
	hash2 := cfg2.ChunkingConfigHash()
	hash3 := cfg3.ChunkingConfigHash()
	hash4 := cfg4.ChunkingConfigHash()

	if hash1 != hash2 {
		t.Error("identical configs should have same hash")
	}
	if hash1 == hash3 {
		t.Error("different configs should have different hash")
	}
	if hash4 != "" {
		t.Error("no chunking should have empty hash")
	}
}

func TestFileEmbeddingName(t *testing.T) {
	name := FileEmbeddingName("workspace-123", "src/main.go")
	expected := "file://workspace-123/src/main.go"
	if name != expected {
		t.Errorf("expected %q, got %q", expected, name)
	}
}

func TestChunkEmbeddingName(t *testing.T) {
	name := ChunkEmbeddingName("workspace-123", "src/main.go", "0", "abc123")
	expected := "file://workspace-123/src/main.go#chunk-0?cfg=abc123"
	if name != expected {
		t.Errorf("expected %q, got %q", expected, name)
	}
}

func TestSplitIntoChunks(t *testing.T) {
	cfg := Config{ChunkBytes: 10, ChunkOverlapBytes: 2}
	idx := &Indexer{config: cfg}

	content := []byte("0123456789abcdefghij") // 20 bytes
	chunks := idx.splitIntoChunks(content)

	// Expected: [0:10], [8:18], [16:20]
	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}

	// First chunk should start at 0
	if chunks[0].Start != 0 {
		t.Errorf("first chunk should start at 0, got %d", chunks[0].Start)
	}

	// Last chunk should end at content length
	lastChunk := chunks[len(chunks)-1]
	if lastChunk.End != len(content) {
		t.Errorf("last chunk should end at %d, got %d", len(content), lastChunk.End)
	}
}

func TestNoOpProvider(t *testing.T) {
	provider := NewNoOpProvider("test", 512)

	if provider.Model() != "test" {
		t.Errorf("expected model 'test', got %q", provider.Model())
	}
	if provider.Dimensions() != 512 {
		t.Errorf("expected dimensions 512, got %d", provider.Dimensions())
	}

	ctx := context.Background()
	embedding, err := provider.Embed(ctx, "test text")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(embedding) != 512 {
		t.Errorf("expected embedding length 512, got %d", len(embedding))
	}

	embeddings, err := provider.EmbedBatch(ctx, []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}
	if len(embeddings) != 3 {
		t.Errorf("expected 3 embeddings, got %d", len(embeddings))
	}
}

func TestIndexer_readFileContent_PathTraversal(t *testing.T) {
	idx, _, workspaceDir := setupTestIndexer(t, Config{Enabled: true})

	// Create a valid test file
	createTestFile(t, workspaceDir, "valid.txt", "test content")

	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{
			name:    "valid path",
			path:    "valid.txt",
			wantErr: "",
		},
		{
			name:    "path traversal with ..",
			path:    "../../../etc/passwd",
			wantErr: "path traversal not allowed",
		},
		{
			name:    "absolute path",
			path:    "/etc/passwd",
			wantErr: "absolute paths not allowed",
		},
		{
			name:    "traversal via subdirectory",
			path:    "foo/../../etc/passwd",
			wantErr: "path traversal not allowed", // After cleaning becomes ../etc/passwd
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := idx.readFileContent(tt.path)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !containsStr(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}

func TestIndexer_readFileContent_FileSizeLimit(t *testing.T) {
	idx, _, workspaceDir := setupTestIndexer(t, Config{Enabled: true})

	// Create a file larger than maxReadFileSize (10MB)
	largeContent := make([]byte, 11*1024*1024) // 11MB
	createTestFile(t, workspaceDir, "large.txt", string(largeContent))

	_, err := idx.readFileContent("large.txt")
	if err == nil {
		t.Error("expected error for large file")
	} else if !containsStr(err.Error(), "file too large") {
		t.Errorf("expected 'file too large' error, got %q", err.Error())
	}
}

func TestIndexer_EmbeddingStored(t *testing.T) {
	idx, store, workspaceDir := setupTestIndexer(t, Config{Enabled: true})

	createTestFile(t, workspaceDir, "embed.txt", "test content for embedding")

	event := indexing.PostReviewEvent{
		WorkspaceID: "ws-embed",
		Files: []indexing.FileChange{
			{Path: "embed.txt", ChangeKind: indexing.ChangeKindModified},
		},
	}

	result, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if result.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed, got %d", result.FilesIndexed)
	}

	// Verify the embedding was stored
	entryName := FileEmbeddingName("ws-embed", "embed.txt")
	entry, err := store.Get(context.Background(), entryName, "ws-embed")
	if err != nil {
		t.Fatalf("failed to get stored entry: %v", err)
	}

	var result2 FileEmbeddingResult
	if err := json.Unmarshal(entry.Result, &result2); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// NoOpProvider returns a zero vector of configured dimension (384 default)
	if len(result2.Embedding) != 384 {
		t.Errorf("expected embedding length 384 (NoOp default), got %d", len(result2.Embedding))
	}
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
