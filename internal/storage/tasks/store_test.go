package tasks

import (
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

func setupTestStore(t *testing.T) Store {
	t.Helper()
	dir, err := os.MkdirTemp("", "tasks-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	store, err := Open(context.Background(), filepath.Join(dir, "storage"))
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	return store
}
