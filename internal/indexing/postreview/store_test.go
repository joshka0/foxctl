package postreview

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing"
)

func TestStore_Put_GeneratesID(t *testing.T) {
	ctx := context.Background()
	store, cleanup := openTestStore(t)
	defer cleanup()

	event := indexing.PostReviewEvent{
		WorkspaceID:   "ws-1",
		TaskID:        "task-1",
		ReviewID:      "review-1",
		ReviewKind:    "auto",
		DiffAppliedAt: time.Now().UTC(),
		Files: []indexing.FileChange{
			{Path: "foo.go", Digest: "sha256:abc", ChangeKind: indexing.ChangeKindModified},
		},
	}

	got, err := store.Put(ctx, event)
	if err != nil {
		t.Fatalf("Put error: %v", err)
	}
	if got.ID == "" {
		t.Error("expected ID to be generated")
	}
	if got.ReviewStatus != "ok" {
		t.Errorf("expected ReviewStatus 'ok', got %q", got.ReviewStatus)
	}
	if got.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestStore_Put_Idempotent(t *testing.T) {
	ctx := context.Background()
	store, cleanup := openTestStore(t)
	defer cleanup()

	event := indexing.PostReviewEvent{
		WorkspaceID:   "ws-1",
		TaskID:        "task-1",
		ReviewID:      "review-1",
		ReviewKind:    "auto",
		DiffAppliedAt: time.Now().UTC(),
		Files: []indexing.FileChange{
			{Path: "foo.go", Digest: "sha256:abc", ChangeKind: indexing.ChangeKindModified},
		},
	}

	first, err := store.Put(ctx, event)
	if err != nil {
		t.Fatalf("first Put error: %v", err)
	}

	// Second put with same payload should return existing event
	second, err := store.Put(ctx, event)
	if err != nil {
		t.Fatalf("second Put error: %v", err)
	}

	if first.ID != second.ID {
		t.Errorf("idempotent Put should return same ID, got %q vs %q", first.ID, second.ID)
	}
}

func TestStore_Put_RejectsDuplicateWithDifferentPayload(t *testing.T) {
	ctx := context.Background()
	store, cleanup := openTestStore(t)
	defer cleanup()

	event1 := indexing.PostReviewEvent{
		WorkspaceID:   "ws-1",
		TaskID:        "task-1",
		ReviewID:      "review-1",
		ReviewKind:    "auto",
		DiffAppliedAt: time.Now().UTC(),
		Files: []indexing.FileChange{
			{Path: "foo.go", Digest: "sha256:abc", ChangeKind: indexing.ChangeKindModified},
		},
	}

	_, err := store.Put(ctx, event1)
	if err != nil {
		t.Fatalf("first Put error: %v", err)
	}

	// Second put with different files should fail
	event2 := event1
	event2.Files = []indexing.FileChange{
		{Path: "bar.go", Digest: "sha256:def", ChangeKind: indexing.ChangeKindAdded},
	}

	_, err = store.Put(ctx, event2)
	if !errors.Is(err, ErrDuplicateEvent) {
		t.Errorf("expected ErrDuplicateEvent, got %v", err)
	}
}

func TestStore_GetByReview(t *testing.T) {
	ctx := context.Background()
	store, cleanup := openTestStore(t)
	defer cleanup()

	event := indexing.PostReviewEvent{
		WorkspaceID:   "ws-1",
		TaskID:        "task-1",
		ReviewID:      "review-1",
		ReviewKind:    "human",
		DiffAppliedAt: time.Now().UTC(),
		Source:        "review_gate_v1",
		Files: []indexing.FileChange{
			{Path: "foo.go", Digest: "sha256:abc", ChangeKind: indexing.ChangeKindModified},
		},
		Metadata: map[string]any{"branch": "main"},
	}

	stored, err := store.Put(ctx, event)
	if err != nil {
		t.Fatalf("Put error: %v", err)
	}

	got, err := store.GetByReview(ctx, "ws-1", "task-1", "review-1")
	if err != nil {
		t.Fatalf("GetByReview error: %v", err)
	}

	if got.ID != stored.ID {
		t.Errorf("ID mismatch: %q vs %q", got.ID, stored.ID)
	}
	if got.ReviewKind != "human" {
		t.Errorf("ReviewKind mismatch: %q", got.ReviewKind)
	}
	if got.Source != "review_gate_v1" {
		t.Errorf("Source mismatch: %q", got.Source)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "foo.go" {
		t.Errorf("Files mismatch: %v", got.Files)
	}
}

func TestStore_GetByReview_NotFound(t *testing.T) {
	ctx := context.Background()
	store, cleanup := openTestStore(t)
	defer cleanup()

	_, err := store.GetByReview(ctx, "ws-1", "task-1", "missing")
	if !errors.Is(err, ErrEventNotFound) {
		t.Errorf("expected ErrEventNotFound, got %v", err)
	}
}

func TestStore_List(t *testing.T) {
	ctx := context.Background()
	store, cleanup := openTestStore(t)
	defer cleanup()

	// Insert multiple events
	for i := 0; i < 3; i++ {
		event := indexing.PostReviewEvent{
			WorkspaceID:   "ws-1",
			TaskID:        "task-1",
			ReviewID:      "review-" + string(rune('a'+i)),
			ReviewKind:    "auto",
			DiffAppliedAt: time.Now().UTC(),
			Files:         []indexing.FileChange{{Path: "f.go", ChangeKind: indexing.ChangeKindModified}},
		}
		if _, err := store.Put(ctx, event); err != nil {
			t.Fatalf("Put error: %v", err)
		}
	}

	events, err := store.List(ctx, "ws-1", 10)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}
}

func openTestStore(t *testing.T) (Store, func()) {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := Open(ctx, tmp)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	return store, func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close error: %v", err)
		}
	}
}
