package postreview

import (
	"context"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/intelligence/indexing"
)

func TestBuildPostReviewEvent(t *testing.T) {
	artifact := agent.ReviewArtifact{
		ID:          "review-123",
		WorkspaceID: "ws-1",
		TaskID:      "task-1",
		Kind:        "auto",
		Status:      "ok",
		CreatedAt:   time.Now().UTC(),
	}

	files := []indexing.FileChange{
		{Path: "foo.go", Digest: "sha256:abc", ChangeKind: indexing.ChangeKindModified},
	}

	event := BuildPostReviewEvent(artifact, files)

	if event.ID == "" {
		t.Error("expected ID to be generated")
	}
	if event.WorkspaceID != "ws-1" {
		t.Errorf("WorkspaceID = %q, want ws-1", event.WorkspaceID)
	}
	if event.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want task-1", event.TaskID)
	}
	if event.ReviewID != "review-123" {
		t.Errorf("ReviewID = %q, want review-123", event.ReviewID)
	}
	if event.ReviewKind != "auto" {
		t.Errorf("ReviewKind = %q, want auto", event.ReviewKind)
	}
	if event.ReviewStatus != "ok" {
		t.Errorf("ReviewStatus = %q, want ok", event.ReviewStatus)
	}
	if event.Source != EventSource {
		t.Errorf("Source = %q, want %q", event.Source, EventSource)
	}
	if len(event.Files) != 1 {
		t.Errorf("len(Files) = %d, want 1", len(event.Files))
	}
	if event.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	if event.DiffAppliedAt.IsZero() {
		t.Error("DiffAppliedAt should be set")
	}
}

func TestBuildPostReviewEvent_EmptyFiles(t *testing.T) {
	artifact := agent.ReviewArtifact{
		ID:          "review-456",
		WorkspaceID: "ws-2",
		TaskID:      "task-2",
		Kind:        "human",
	}

	event := BuildPostReviewEvent(artifact, nil)

	if event.Files != nil {
		t.Errorf("Files = %v, want nil", event.Files)
	}
	// Should still have valid ID and timestamps
	if event.ID == "" {
		t.Error("expected ID to be generated")
	}
}

func TestProducer_Produce(t *testing.T) {
	ctx := context.Background()
	store, cleanup := openTestStore(t)
	defer cleanup()

	producer := NewProducer(store)

	artifact := agent.ReviewArtifact{
		ID:          "review-789",
		WorkspaceID: "ws-3",
		TaskID:      "task-3",
		Kind:        "mixed",
	}

	files := []indexing.FileChange{
		{Path: "bar.go", Digest: "sha256:def", ChangeKind: indexing.ChangeKindAdded},
	}

	event, err := producer.Produce(ctx, artifact, files)
	if err != nil {
		t.Fatalf("Produce error: %v", err)
	}

	if event.ReviewID != "review-789" {
		t.Errorf("ReviewID = %q, want review-789", event.ReviewID)
	}

	// Verify it was stored
	stored, err := store.GetByReview(ctx, "ws-3", "task-3", "review-789")
	if err != nil {
		t.Fatalf("GetByReview error: %v", err)
	}
	if stored.ID != event.ID {
		t.Errorf("stored.ID = %q, want %q", stored.ID, event.ID)
	}
}

func TestProducer_ProduceFromReview_Idempotent(t *testing.T) {
	ctx := context.Background()
	store, cleanup := openTestStore(t)
	defer cleanup()

	producer := NewProducer(store)

	artifact := agent.ReviewArtifact{
		ID:          "review-aaa",
		WorkspaceID: "ws-4",
		TaskID:      "task-4",
		Kind:        "auto",
	}

	first, err := producer.ProduceFromReview(ctx, artifact)
	if err != nil {
		t.Fatalf("first ProduceFromReview error: %v", err)
	}

	second, err := producer.ProduceFromReview(ctx, artifact)
	if err != nil {
		t.Fatalf("second ProduceFromReview error: %v", err)
	}

	if first.ID != second.ID {
		t.Errorf("idempotent calls should return same ID: %q vs %q", first.ID, second.ID)
	}
}
