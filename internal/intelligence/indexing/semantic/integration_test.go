package semantic_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/intelligence/indexing"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/rs/zerolog"
)

// TestSemanticIndexerWithPostReviewHandler tests the full integration of
// the semantic indexer with the post-review pipeline.
func TestSemanticIndexerWithPostReviewHandler(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	workspaceDir := filepath.Join(tmpDir, "workspace")
	storageDir := filepath.Join(tmpDir, "storage")
	casDir := filepath.Join(tmpDir, "cas")

	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	store, err := memory.Open(ctx, storageDir, casDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	logger := zerolog.Nop()
	workspaceID := workspace.ID(workspaceDir)

	// Create post-review handler with semantic indexer enabled
	handlerCfg := indexing.PostReviewConfig{
		Enabled: true,
		Async:   false, // Synchronous for testing
		Indexers: []indexing.IndexerConfig{
			{
				ID:           semantic.IndexerID,
				Kind:         "semantic_file_index",
				Enabled:      true,
				IncludeGlobs: []string{"*.go", "*.md"},
				MaxFileKB:    256,
			},
		},
	}

	handler := indexing.NewPostReviewHandler(handlerCfg, logger)

	// Register semantic indexer
	indexerCfg := handlerCfg.Indexers[0]
	if err := semantic.RegisterWithHandler(handler, store, workspaceDir, indexerCfg, logger); err != nil {
		t.Fatalf("RegisterWithHandler failed: %v", err)
	}

	// Create test files
	createFile(t, workspaceDir, "main.go", "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n")
	createFile(t, workspaceDir, "README.md", "# Test Project\n\nThis is a test.\n")
	createFile(t, workspaceDir, "data.json", `{"key": "value"}`) // Should be filtered out

	// Simulate post-review event
	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		TaskID:      "task-integration",
		ReviewID:    "review-integration",
		Reason:      "post_review",
		Files: []indexing.FileChange{
			{Path: "main.go", ChangeKind: indexing.ChangeKindModified, Language: "go"},
			{Path: "README.md", ChangeKind: indexing.ChangeKindAdded, Language: "markdown"},
			{Path: "data.json", ChangeKind: indexing.ChangeKindAdded, Language: "json"}, // Filtered
		},
	}

	result, err := handler.Handle(ctx, event)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	// Verify results
	if result.Skipped {
		t.Error("expected handler not to skip")
	}
	if len(result.IndexerResults) != 1 {
		t.Fatalf("expected 1 indexer result, got %d", len(result.IndexerResults))
	}

	idxResult := result.IndexerResults[0]
	if idxResult.IndexerID != semantic.IndexerID {
		t.Errorf("expected indexer ID %q, got %q", semantic.IndexerID, idxResult.IndexerID)
	}

	// data.json should be filtered by IncludeGlobs, so only 2 files indexed
	if idxResult.FilesIndexed != 2 {
		t.Errorf("expected 2 files indexed (go + md), got %d", idxResult.FilesIndexed)
	}

	// Verify entries were saved
	goName := semantic.FileEmbeddingName(workspaceID, "main.go")
	goEntry, err := store.Get(ctx, goName, workspaceID)
	if err != nil {
		t.Fatalf("Get main.go entry failed: %v", err)
	}
	if goEntry.Type != semantic.FileEmbeddingType {
		t.Errorf("expected type %q, got %q", semantic.FileEmbeddingType, goEntry.Type)
	}

	mdName := semantic.FileEmbeddingName(workspaceID, "README.md")
	mdEntry, err := store.Get(ctx, mdName, workspaceID)
	if err != nil {
		t.Fatalf("Get README.md entry failed: %v", err)
	}
	if mdEntry.Type != semantic.FileEmbeddingType {
		t.Errorf("expected type %q, got %q", semantic.FileEmbeddingType, mdEntry.Type)
	}

	// Verify data.json was NOT indexed (filtered by globs)
	jsonName := semantic.FileEmbeddingName("integration-test-ws", "data.json")
	_, err = store.Get(ctx, jsonName, "integration-test-ws")
	if err == nil {
		t.Error("data.json should not have been indexed (filtered by globs)")
	}
}

func createFile(t *testing.T, dir, path, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
