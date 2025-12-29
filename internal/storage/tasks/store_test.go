package tasks

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_AddAndGet(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	task := Task{
		WorkspaceID: "ws-1",
		Title:       "Test Task",
		Description: "A test task",
		ScopePath:   "/path/to/file.go",
	}

	added, err := store.Add(ctx, task)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if added.ID == "" {
		t.Error("expected ID to be generated")
	}
	if added.Status != StatusPending {
		t.Errorf("expected status %q, got %q", StatusPending, added.Status)
	}
	if added.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}

	got, err := store.Get(ctx, added.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Title != task.Title {
		t.Errorf("expected title %q, got %q", task.Title, got.Title)
	}
	if got.Description != task.Description {
		t.Errorf("expected description %q, got %q", task.Description, got.Description)
	}
}

func TestStore_Update(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	task, err := store.Add(ctx, Task{
		WorkspaceID: "ws-1",
		Title:       "Original Title",
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	task.Title = "Updated Title"
	task.Description = "Now with description"
	now := time.Now().UTC()
	task.CompletedAt = &now
	task.Status = StatusCompleted

	updated, err := store.Update(ctx, task)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.Title != "Updated Title" {
		t.Errorf("expected title %q, got %q", "Updated Title", updated.Title)
	}
	if updated.Status != StatusCompleted {
		t.Errorf("expected status %q, got %q", StatusCompleted, updated.Status)
	}
	if updated.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}

func TestStore_ListByWorkspace(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	// Add tasks to different workspaces
	_, err := store.Add(ctx, Task{WorkspaceID: "ws-1", Title: "Task 1"})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	_, err = store.Add(ctx, Task{WorkspaceID: "ws-1", Title: "Task 2"})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	_, err = store.Add(ctx, Task{WorkspaceID: "ws-2", Title: "Task 3"})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	tasks, err := store.ListByWorkspace(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListByWorkspace failed: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}

	tasks, err = store.ListByWorkspace(ctx, "ws-2")
	if err != nil {
		t.Fatalf("ListByWorkspace failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasks))
	}
}

func TestStore_ActiveTask(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	// Initially no active task
	_, found, err := store.GetActive(ctx, "ws-1")
	if err != nil {
		t.Fatalf("GetActive failed: %v", err)
	}
	if found {
		t.Error("expected no active task initially")
	}

	// Add a task and set it as active
	task, err := store.Add(ctx, Task{WorkspaceID: "ws-1", Title: "Active Task"})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	active, err := store.SetActive(ctx, "ws-1", task.ID)
	if err != nil {
		t.Fatalf("SetActive failed: %v", err)
	}
	if active.ID != task.ID {
		t.Errorf("expected active task ID %q, got %q", task.ID, active.ID)
	}

	// Verify GetActive returns the task
	got, found, err := store.GetActive(ctx, "ws-1")
	if err != nil {
		t.Fatalf("GetActive failed: %v", err)
	}
	if !found {
		t.Error("expected to find active task")
	}
	if got.ID != task.ID {
		t.Errorf("expected task ID %q, got %q", task.ID, got.ID)
	}

	// Clear active task
	err = store.ClearActive(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ClearActive failed: %v", err)
	}

	_, found, err = store.GetActive(ctx, "ws-1")
	if err != nil {
		t.Fatalf("GetActive failed: %v", err)
	}
	if found {
		t.Error("expected no active task after clear")
	}
}

func TestStore_EnsureActive(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	// First call should create a new task
	task1, created, err := store.EnsureActive(ctx, "ws-1", "Default Title", "/scope/path")
	if err != nil {
		t.Fatalf("EnsureActive failed: %v", err)
	}
	if !created {
		t.Error("expected task to be created")
	}
	if task1.Title != "Default Title" {
		t.Errorf("expected title %q, got %q", "Default Title", task1.Title)
	}
	if task1.ScopePath != "/scope/path" {
		t.Errorf("expected scope_path %q, got %q", "/scope/path", task1.ScopePath)
	}

	// Second call should return existing task
	task2, created, err := store.EnsureActive(ctx, "ws-1", "Different Title", "/other/path")
	if err != nil {
		t.Fatalf("EnsureActive failed: %v", err)
	}
	if created {
		t.Error("expected task to not be created")
	}
	if task2.ID != task1.ID {
		t.Errorf("expected same task ID %q, got %q", task1.ID, task2.ID)
	}
	// Should keep original title
	if task2.Title != "Default Title" {
		t.Errorf("expected original title %q, got %q", "Default Title", task2.Title)
	}
}

func TestStore_DependsOnAndChildren(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	task := Task{
		WorkspaceID: "ws-1",
		Title:       "Task with deps",
		DependsOn:   []string{"dep-1", "dep-2"},
		Children:    []string{"child-1"},
	}

	added, err := store.Add(ctx, task)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	got, err := store.Get(ctx, added.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if len(got.DependsOn) != 2 {
		t.Errorf("expected 2 dependencies, got %d", len(got.DependsOn))
	}
	if len(got.Children) != 1 {
		t.Errorf("expected 1 child, got %d", len(got.Children))
	}
}

func TestStore_DirtyIfReviewed_PendingTask(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	// Create a pending task
	task, err := store.Add(ctx, Task{
		WorkspaceID: "ws-1",
		Title:       "Pending Task",
		Status:      StatusPending,
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// DirtyIfReviewed should not modify a pending task
	result, dirtied, err := store.DirtyIfReviewed(ctx, task.ID)
	if err != nil {
		t.Fatalf("DirtyIfReviewed failed: %v", err)
	}
	if dirtied {
		t.Error("expected dirtied=false for pending task")
	}
	if result.Status != StatusPending {
		t.Errorf("expected status %q, got %q", StatusPending, result.Status)
	}
}

func TestStore_DirtyIfReviewed_InProgressTask(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	// Create an in_progress task
	task, err := store.Add(ctx, Task{
		WorkspaceID: "ws-1",
		Title:       "In Progress Task",
		Status:      StatusInProgress,
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// DirtyIfReviewed should not modify an in_progress task
	result, dirtied, err := store.DirtyIfReviewed(ctx, task.ID)
	if err != nil {
		t.Fatalf("DirtyIfReviewed failed: %v", err)
	}
	if dirtied {
		t.Error("expected dirtied=false for in_progress task")
	}
	if result.Status != StatusInProgress {
		t.Errorf("expected status %q, got %q", StatusInProgress, result.Status)
	}
}

func TestStore_DirtyIfReviewed_ReadyForReviewTask(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	// Create a ready_for_review task with passing review
	task, err := store.Add(ctx, Task{
		WorkspaceID:      "ws-1",
		Title:            "Ready for Review Task",
		Status:           StatusReadyForReview,
		LastReviewStatus: ReviewStatusOK,
		LastReviewID:     "review-123",
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// DirtyIfReviewed should demote to in_progress and mark review as stale
	result, dirtied, err := store.DirtyIfReviewed(ctx, task.ID)
	if err != nil {
		t.Fatalf("DirtyIfReviewed failed: %v", err)
	}
	if !dirtied {
		t.Error("expected dirtied=true for ready_for_review task")
	}
	if result.Status != StatusInProgress {
		t.Errorf("expected status %q, got %q", StatusInProgress, result.Status)
	}
	if result.LastReviewStatus != ReviewStatusStale {
		t.Errorf("expected last_review_status %q, got %q", ReviewStatusStale, result.LastReviewStatus)
	}
	// Review ID should be preserved
	if result.LastReviewID != "review-123" {
		t.Errorf("expected last_review_id %q, got %q", "review-123", result.LastReviewID)
	}
}

func TestStore_DirtyIfReviewed_CompletedTask(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	// Create a completed task with passing review
	now := time.Now().UTC()
	task, err := store.Add(ctx, Task{
		WorkspaceID:      "ws-1",
		Title:            "Completed Task",
		Status:           StatusCompleted,
		CompletedAt:      &now,
		LastReviewStatus: ReviewStatusOK,
		LastReviewID:     "review-456",
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// DirtyIfReviewed should demote to in_progress and mark review as stale
	result, dirtied, err := store.DirtyIfReviewed(ctx, task.ID)
	if err != nil {
		t.Fatalf("DirtyIfReviewed failed: %v", err)
	}
	if !dirtied {
		t.Error("expected dirtied=true for completed task")
	}
	if result.Status != StatusInProgress {
		t.Errorf("expected status %q, got %q", StatusInProgress, result.Status)
	}
	if result.LastReviewStatus != ReviewStatusStale {
		t.Errorf("expected last_review_status %q, got %q", ReviewStatusStale, result.LastReviewStatus)
	}
}

func TestStore_DirtyIfReviewed_FailedReviewNotMarkedStale(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	// Create a ready_for_review task with failed review
	task, err := store.Add(ctx, Task{
		WorkspaceID:      "ws-1",
		Title:            "Failed Review Task",
		Status:           StatusReadyForReview,
		LastReviewStatus: ReviewStatusFailed,
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// DirtyIfReviewed should demote but NOT mark as stale (only ok reviews become stale)
	result, dirtied, err := store.DirtyIfReviewed(ctx, task.ID)
	if err != nil {
		t.Fatalf("DirtyIfReviewed failed: %v", err)
	}
	if !dirtied {
		t.Error("expected dirtied=true for ready_for_review task")
	}
	if result.Status != StatusInProgress {
		t.Errorf("expected status %q, got %q", StatusInProgress, result.Status)
	}
	// Failed review should remain failed, not stale
	if result.LastReviewStatus != ReviewStatusFailed {
		t.Errorf("expected last_review_status %q, got %q", ReviewStatusFailed, result.LastReviewStatus)
	}
}

func TestStore_ReviewFields(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	reviewAt := time.Now().UTC()
	task, err := store.Add(ctx, Task{
		WorkspaceID:      "ws-1",
		Title:            "Task with review",
		LastReviewStatus: ReviewStatusOK,
		LastReviewAt:     &reviewAt,
		LastReviewID:     "review-789",
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	got, err := store.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.LastReviewStatus != ReviewStatusOK {
		t.Errorf("expected last_review_status %q, got %q", ReviewStatusOK, got.LastReviewStatus)
	}
	if got.LastReviewAt == nil {
		t.Error("expected LastReviewAt to be set")
	}
	if got.LastReviewID != "review-789" {
		t.Errorf("expected last_review_id %q, got %q", "review-789", got.LastReviewID)
	}
}

func TestStore_PlanFields(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	// Create a task linked to a plan
	task, err := store.Add(ctx, Task{
		WorkspaceID: "ws-1",
		Title:       "Implement feature X",
		PlanFile:    "/home/user/.claude/plans/feature-x.md",
		PlanSection: "Phase 1 > Step 1.1",
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Verify plan fields are persisted
	got, err := store.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.PlanFile != "/home/user/.claude/plans/feature-x.md" {
		t.Errorf("expected plan_file %q, got %q", "/home/user/.claude/plans/feature-x.md", got.PlanFile)
	}
	if got.PlanSection != "Phase 1 > Step 1.1" {
		t.Errorf("expected plan_section %q, got %q", "Phase 1 > Step 1.1", got.PlanSection)
	}

	// Test update of plan fields
	got.PlanSection = "Phase 1 > Step 1.2"
	updated, err := store.Update(ctx, got)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.PlanSection != "Phase 1 > Step 1.2" {
		t.Errorf("expected plan_section %q, got %q", "Phase 1 > Step 1.2", updated.PlanSection)
	}
}

func TestStore_SetEmbedding(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	task, err := store.Add(ctx, Task{
		WorkspaceID: "ws-1",
		Title:       "Embeddable Task",
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	embedding := []byte{0x01, 0x02, 0x03}
	model := "voyage-code-3"

	if err := store.SetEmbedding(ctx, task.ID, embedding, model); err != nil {
		t.Fatalf("SetEmbedding failed: %v", err)
	}

	sqlStore, ok := store.(*sqlStore)
	if !ok {
		t.Fatalf("expected *sqlStore, got %T", store)
	}

	var storedEmbedding []byte
	var storedModel string
	row := sqlStore.db.QueryRowContext(ctx, `SELECT embedding, embedding_model FROM tasks WHERE id = ?`, task.ID)
	if err := row.Scan(&storedEmbedding, &storedModel); err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if !bytes.Equal(storedEmbedding, embedding) {
		t.Fatalf("embedding mismatch: got %v want %v", storedEmbedding, embedding)
	}
	if storedModel != model {
		t.Fatalf("model mismatch: got %q want %q", storedModel, model)
	}

	if err := store.SetEmbedding(ctx, "missing-id", embedding, model); err == nil {
		t.Fatal("expected error for missing task ID, got nil")
	}
}

func TestStore_ListByPlanFile(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	planFile := "/home/user/.claude/plans/feature-x.md"

	// Create multiple tasks linked to the same plan
	_, err := store.Add(ctx, Task{
		WorkspaceID: "ws-1",
		Title:       "Step 1",
		PlanFile:    planFile,
		PlanSection: "Phase 1 > Step 1",
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	_, err = store.Add(ctx, Task{
		WorkspaceID: "ws-1",
		Title:       "Step 2",
		PlanFile:    planFile,
		PlanSection: "Phase 1 > Step 2",
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Create a task not linked to any plan
	_, err = store.Add(ctx, Task{
		WorkspaceID: "ws-1",
		Title:       "Unlinked task",
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Create a task linked to a different plan
	_, err = store.Add(ctx, Task{
		WorkspaceID: "ws-1",
		Title:       "Other plan task",
		PlanFile:    "/home/user/.claude/plans/other.md",
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// List tasks by plan file
	tasks, err := store.ListByPlanFile(ctx, planFile)
	if err != nil {
		t.Fatalf("ListByPlanFile failed: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}

	// Verify tasks are sorted by created_at ASC
	if len(tasks) >= 2 && tasks[0].Title != "Step 1" {
		t.Errorf("expected first task to be 'Step 1', got %q", tasks[0].Title)
	}
}

func setupTestStore(t *testing.T) Store {
	t.Helper()
	dir, err := os.MkdirTemp("", "tasks-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() {
		// Test cleanup; error is not actionable.
		_ = os.RemoveAll(dir) //nolint:errcheck
	})

	store, err := Open(context.Background(), filepath.Join(dir, "storage"))
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	return store
}
