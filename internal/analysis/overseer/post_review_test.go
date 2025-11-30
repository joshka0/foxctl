package overseer

import (
	"context"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/indexing"
	"github.com/jkatigb/agentctl/internal/indexing/postreview"
	"github.com/rs/zerolog"
)

func TestPostReviewHandler_HandleReviewApproved(t *testing.T) {
	ctx := context.Background()
	store, cleanup := openTestEventStore(t)
	defer cleanup()

	var producedEvent indexing.PostReviewEvent
	handler := NewPostReviewHandler(PostReviewHandlerConfig{
		EventStore:     store,
		IndexerHandler: nil, // No indexers for this test
		Config:         indexing.DefaultPostReviewConfig(),
		Logger:         zerolog.Nop(),
	})
	handler.SetTestHook(func(e indexing.PostReviewEvent) {
		producedEvent = e
	})

	artifact := agent.ReviewArtifact{
		ID:          "review-123",
		WorkspaceID: "ws-1",
		TaskID:      "task-1",
		Kind:        "auto",
		Status:      "ok",
		CreatedAt:   time.Now().UTC(),
	}

	result, err := handler.HandleReviewApproved(ctx, artifact, nil)
	if err != nil {
		t.Fatalf("HandleReviewApproved error: %v", err)
	}

	if result.Event.ID == "" {
		t.Error("expected event ID to be set")
	}
	if result.Event.WorkspaceID != "ws-1" {
		t.Errorf("WorkspaceID = %q, want ws-1", result.Event.WorkspaceID)
	}
	if result.Event.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want task-1", result.Event.TaskID)
	}
	if result.Event.ReviewID != "review-123" {
		t.Errorf("ReviewID = %q, want review-123", result.Event.ReviewID)
	}
	if result.Event.ReviewStatus != "ok" {
		t.Errorf("ReviewStatus = %q, want ok", result.Event.ReviewStatus)
	}

	// Verify hook was called
	if producedEvent.ID != result.Event.ID {
		t.Errorf("hook event ID = %q, want %q", producedEvent.ID, result.Event.ID)
	}

	// Should be skipped since no indexer handler
	if !result.Skipped {
		t.Error("expected Skipped=true with no indexer handler")
	}
	if result.Reason != "no_indexer_handler" {
		t.Errorf("Reason = %q, want no_indexer_handler", result.Reason)
	}
}

func TestPostReviewHandler_HandleReviewApproved_Idempotent(t *testing.T) {
	ctx := context.Background()
	store, cleanup := openTestEventStore(t)
	defer cleanup()

	handler := NewPostReviewHandler(PostReviewHandlerConfig{
		EventStore:     store,
		IndexerHandler: nil,
		Config:         indexing.DefaultPostReviewConfig(),
		Logger:         zerolog.Nop(),
	})

	artifact := agent.ReviewArtifact{
		ID:          "review-456",
		WorkspaceID: "ws-2",
		TaskID:      "task-2",
		Kind:        "human",
		Status:      "ok",
	}

	first, err := handler.HandleReviewApproved(ctx, artifact, nil)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}

	second, err := handler.HandleReviewApproved(ctx, artifact, nil)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}

	if first.Event.ID != second.Event.ID {
		t.Errorf("idempotent calls should return same event ID: %q vs %q",
			first.Event.ID, second.Event.ID)
	}
}

func TestPostReviewHandler_HandleReviewApproved_RejectsNonOK(t *testing.T) {
	ctx := context.Background()
	store, cleanup := openTestEventStore(t)
	defer cleanup()

	handler := NewPostReviewHandler(PostReviewHandlerConfig{
		EventStore:     store,
		IndexerHandler: nil,
		Config:         indexing.DefaultPostReviewConfig(),
		Logger:         zerolog.Nop(),
	})

	artifact := agent.ReviewArtifact{
		ID:          "review-789",
		WorkspaceID: "ws-3",
		TaskID:      "task-3",
		Status:      "pending", // Not ok
	}

	_, err := handler.HandleReviewApproved(ctx, artifact, nil)
	if err == nil {
		t.Fatal("expected error for non-ok status")
	}
}

func TestPostReviewHandler_HandleReviewApproved_WithFiles(t *testing.T) {
	ctx := context.Background()
	store, cleanup := openTestEventStore(t)
	defer cleanup()

	handler := NewPostReviewHandler(PostReviewHandlerConfig{
		EventStore:     store,
		IndexerHandler: nil,
		Config:         indexing.DefaultPostReviewConfig(),
		Logger:         zerolog.Nop(),
	})

	artifact := agent.ReviewArtifact{
		ID:          "review-aaa",
		WorkspaceID: "ws-4",
		TaskID:      "task-4",
		Kind:        "mixed",
		Status:      "ok",
	}

	files := []indexing.FileChange{
		{Path: "foo.go", Digest: "sha256:abc", ChangeKind: indexing.ChangeKindModified},
		{Path: "bar.go", Digest: "sha256:def", ChangeKind: indexing.ChangeKindAdded},
	}

	result, err := handler.HandleReviewApproved(ctx, artifact, files)
	if err != nil {
		t.Fatalf("HandleReviewApproved error: %v", err)
	}

	if len(result.Event.Files) != 2 {
		t.Errorf("len(Files) = %d, want 2", len(result.Event.Files))
	}
	if result.Event.Files[0].Path != "foo.go" {
		t.Errorf("Files[0].Path = %q, want foo.go", result.Event.Files[0].Path)
	}
}

func TestPostReviewHandler_WithIndexerHandler(t *testing.T) {
	ctx := context.Background()
	store, cleanup := openTestEventStore(t)
	defer cleanup()

	// Create an indexer handler that's disabled
	indexerHandler := indexing.NewPostReviewHandler(
		indexing.PostReviewConfig{Enabled: false},
		zerolog.Nop(),
	)

	handler := NewPostReviewHandler(PostReviewHandlerConfig{
		EventStore:     store,
		IndexerHandler: indexerHandler,
		Config:         indexing.DefaultPostReviewConfig(),
		Logger:         zerolog.Nop(),
	})

	artifact := agent.ReviewArtifact{
		ID:          "review-bbb",
		WorkspaceID: "ws-5",
		TaskID:      "task-5",
		Status:      "ok",
	}

	result, err := handler.HandleReviewApproved(ctx, artifact, nil)
	if err != nil {
		t.Fatalf("HandleReviewApproved error: %v", err)
	}

	// Event should still be produced
	if result.Event.ID == "" {
		t.Error("expected event ID to be set")
	}

	// Indexer should report disabled
	if result.IndexerResult == nil {
		t.Fatal("expected IndexerResult to be set")
	}
	if !result.IndexerResult.Skipped {
		t.Error("expected indexer to be skipped (disabled)")
	}
	if result.IndexerResult.Reason != "disabled" {
		t.Errorf("Reason = %q, want disabled", result.IndexerResult.Reason)
	}
}

func openTestEventStore(t *testing.T) (postreview.Store, func()) {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := postreview.Open(ctx, tmp)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	return store, func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close error: %v", err)
		}
	}
}
