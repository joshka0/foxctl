package overseer

import (
	"context"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/indexing"
	"github.com/jkatigb/agentctl/internal/indexing/postreview"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
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

func TestPostReviewHandler_HandleReviewApproved_CapturesTrajectoryReviewResult(t *testing.T) {
	ctx := context.Background()
	store, cleanup := openTestEventStore(t)
	defer cleanup()

	trajRoot := t.TempDir()
	trajStore, err := trajectory.Open(ctx, trajRoot)
	if err != nil {
		t.Fatalf("open trajectory store: %v", err)
	}
	t.Cleanup(func() {
		// Test cleanup; error is not actionable.
		_ = trajStore.Close() //nolint:errcheck
	})

	ur, err := trajStore.InsertUserRequest(ctx, trajectory.UserRequestCapture{ID: "req-1", WorkspaceID: "ws-1", Actor: "actor:human:test", Source: trajectory.SourceCLI, TS: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Text: "do thing"})
	if err != nil {
		t.Fatalf("insert user request: %v", err)
	}
	_, err = trajStore.InsertTrajectory(ctx, trajectory.Trajectory{ID: "traj-1", WorkspaceID: "ws-1", RootRequestID: ur.ID, TaskIDs: []string{"task-1"}, TraceID: "trace-1", Status: trajectory.StatusPartial, CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("insert trajectory: %v", err)
	}

	h := NewPostReviewHandler(PostReviewHandlerConfig{
		EventStore:            store,
		TrajectoryStorageRoot: trajRoot,
		IndexerHandler:        nil,
		Config:                indexing.DefaultPostReviewConfig(),
		Logger:                zerolog.Nop(),
	})

	review := agent.ReviewArtifact{
		ID:          "sha256:3333333333333333333333333333333333333333333333333333333333333333",
		WorkspaceID: "ws-1",
		TaskID:      "task-1",
		Kind:        "auto",
		Status:      "ok",
		Summary:     "looks good",
	}

	first, err := h.HandleReviewApproved(ctx, review, nil)
	if err != nil {
		t.Fatalf("HandleReviewApproved error: %v", err)
	}
	second, err := h.HandleReviewApproved(ctx, review, nil)
	if err != nil {
		t.Fatalf("HandleReviewApproved second error: %v", err)
	}
	if first.Event.ID != second.Event.ID {
		t.Fatalf("expected handler to be idempotent")
	}

	events, err := trajStore.ListEvents(ctx, trajectory.EventFilter{TrajectoryID: "traj-1", Kind: trajectory.EventKindReviewResult})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 review_result event got %d", len(events))
	}
	if events[0].Meta == nil {
		t.Fatalf("expected meta to be set")
	}
	if events[0].Meta.ReviewID != review.ID {
		t.Fatalf("review_id = %q, want %q", events[0].Meta.ReviewID, review.ID)
	}
	if events[0].Meta.TaskID != "task-1" {
		t.Fatalf("task_id = %q, want task-1", events[0].Meta.TaskID)
	}
	if events[0].Meta.TraceID != "trace-1" {
		t.Fatalf("trace_id = %q, want trace-1", events[0].Meta.TraceID)
	}
	if events[0].DataArtifact != review.ID {
		t.Fatalf("data_artifact = %q, want %q", events[0].DataArtifact, review.ID)
	}
	if events[0].Meta.CASDigest != review.ID {
		t.Fatalf("meta.cas_digest = %q, want %q", events[0].Meta.CASDigest, review.ID)
	}
	if s, ok := events[0].DataInline["post_review_event_id"].(string); !ok || s != first.Event.ID {
		t.Fatalf("post_review_event_id = %v, want %q", events[0].DataInline["post_review_event_id"], first.Event.ID)
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

// TestPostReviewHandler_IntegrationWithFakeIndexer is an integration-style test
// that wires a fake indexer subscriber and verifies the full flow from
// ReviewArtifact → PostReviewEvent → Indexer.Index() call.
func TestPostReviewHandler_IntegrationWithFakeIndexer(t *testing.T) {
	ctx := context.Background()
	store, cleanup := openTestEventStore(t)
	defer cleanup()

	// Create a fake indexer that records received events
	fakeIndexer := &fakeIndexer{id: "test_semantic"}

	// Create indexer handler with the fake indexer enabled
	indexerHandler := indexing.NewPostReviewHandler(
		indexing.PostReviewConfig{
			Enabled: true,
			Mode:    indexing.FanoutModeInline,
			Indexers: []indexing.IndexerConfig{
				{ID: "test_semantic", Kind: "semantic_file_index", Enabled: true},
			},
		},
		zerolog.Nop(),
	)
	if err := indexerHandler.RegisterIndexer(fakeIndexer); err != nil {
		t.Fatalf("RegisterIndexer error: %v", err)
	}

	// Create the overseer handler
	handler := NewPostReviewHandler(PostReviewHandlerConfig{
		EventStore:     store,
		IndexerHandler: indexerHandler,
		Config:         indexing.DefaultPostReviewConfig(),
		Logger:         zerolog.Nop(),
	})

	// Simulate a review artifact transitioning to ok
	artifact := agent.ReviewArtifact{
		ID:          "review-integration-1",
		WorkspaceID: "ws-integration",
		TaskID:      "task-integration",
		Kind:        "auto",
		Status:      "ok",
		CreatedAt:   time.Now().UTC(),
	}

	files := []indexing.FileChange{
		{Path: "main.go", Digest: "sha256:aaa", ChangeKind: indexing.ChangeKindModified},
		{Path: "util.go", Digest: "sha256:bbb", ChangeKind: indexing.ChangeKindAdded},
	}

	// Handle the approved review
	result, err := handler.HandleReviewApproved(ctx, artifact, files)
	if err != nil {
		t.Fatalf("HandleReviewApproved error: %v", err)
	}

	// Verify event was produced
	if result.Event.ID == "" {
		t.Error("expected event ID to be set")
	}
	if result.Event.WorkspaceID != "ws-integration" {
		t.Errorf("WorkspaceID = %q, want ws-integration", result.Event.WorkspaceID)
	}

	// Verify fake indexer was called
	if fakeIndexer.callCount != 1 {
		t.Errorf("indexer call count = %d, want 1", fakeIndexer.callCount)
	}

	// Verify the event received by the indexer has the right data
	receivedEvent := fakeIndexer.lastEvent
	if receivedEvent.WorkspaceID != "ws-integration" {
		t.Errorf("received event WorkspaceID = %q, want ws-integration", receivedEvent.WorkspaceID)
	}
	if receivedEvent.TaskID != "task-integration" {
		t.Errorf("received event TaskID = %q, want task-integration", receivedEvent.TaskID)
	}
	if receivedEvent.ReviewID != "review-integration-1" {
		t.Errorf("received event ReviewID = %q, want review-integration-1", receivedEvent.ReviewID)
	}
	if len(receivedEvent.Files) != 2 {
		t.Errorf("received event file count = %d, want 2", len(receivedEvent.Files))
	}
}

// fakeIndexer is a test double that records calls for verification.
type fakeIndexer struct {
	id        string
	callCount int
	lastEvent indexing.PostReviewEvent
}

func (f *fakeIndexer) ID() string { return f.id }

func (f *fakeIndexer) Index(_ context.Context, event indexing.PostReviewEvent) (*indexing.IndexerResult, error) {
	f.callCount++
	f.lastEvent = event
	return &indexing.IndexerResult{
		FilesIndexed: len(event.Files),
	}, nil
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
