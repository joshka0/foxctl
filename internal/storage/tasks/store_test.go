package tasks

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"
	"time"

	_ "modernc.org/sqlite"
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

func TestStore_AddRejectsInvalidStatus(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	_, err := store.Add(ctx, Task{
		WorkspaceID: "ws-1",
		Title:       "Invalid status",
		Status:      "done",
	})
	if err == nil {
		t.Fatal("expected invalid status error")
	}
	if !strings.Contains(err.Error(), "invalid task status") {
		t.Fatalf("error=%v want invalid task status", err)
	}
	tasks, err := store.ListByWorkspace(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListByWorkspace: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("invalid-status task was persisted: %+v", tasks)
	}
}

func TestStore_UpdateRejectsInvalidStatusWithoutMutatingExistingTask(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	task, err := store.Add(ctx, Task{
		WorkspaceID: "ws-1",
		Title:       "Valid task",
		Status:      StatusPending,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	task.Status = "done"
	if _, err := store.Update(ctx, task); err == nil {
		t.Fatal("expected invalid status error")
	} else if !strings.Contains(err.Error(), "invalid task status") {
		t.Fatalf("error=%v want invalid task status", err)
	}

	got, err := store.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusPending {
		t.Fatalf("status=%q want %q", got.Status, StatusPending)
	}
}

func TestStore_AddSubtaskRejectsInvalidStatusWithoutLinkingChild(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	parent, err := store.Add(ctx, Task{
		WorkspaceID: "ws-1",
		Title:       "Parent",
		Status:      StatusPending,
	})
	if err != nil {
		t.Fatalf("Add parent: %v", err)
	}

	if _, err := store.AddSubtask(ctx, parent.ID, Task{
		WorkspaceID: "ws-1",
		Title:       "Child",
		Status:      "done",
	}); err == nil {
		t.Fatal("expected invalid status error")
	} else if !strings.Contains(err.Error(), "invalid task status") {
		t.Fatalf("error=%v want invalid task status", err)
	}

	got, err := store.Get(ctx, parent.ID)
	if err != nil {
		t.Fatalf("Get parent: %v", err)
	}
	if len(got.Children) != 0 {
		t.Fatalf("parent children=%v want none after rejected child", got.Children)
	}
}

// TestValidateTaskStatusRejectsUnknownStatuses exercises the pure validator with
// broad unknown inputs. No database is opened.
func TestValidateTaskStatusRejectsUnknownStatuses(t *testing.T) {
	unknownStatusesFailClosed := func(raw string) bool {
		err := validateTaskStatus("unknown:" + raw)
		return err != nil && strings.Contains(err.Error(), "invalid task status")
	}

	if err := quick.Check(unknownStatusesFailClosed, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("generated unknown task status was accepted: %v", err)
	}
}

// TestValidateTaskReviewStatusRejectsUnknownStatuses exercises the pure validator.
func TestValidateTaskReviewStatusRejectsUnknownStatuses(t *testing.T) {
	unknownStatusesFailClosed := func(raw string) bool {
		err := validateTaskReviewStatus("unknown:" + raw)
		return err != nil && strings.Contains(err.Error(), "invalid task review status")
	}

	if err := quick.Check(unknownStatusesFailClosed, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("generated unknown review status was accepted: %v", err)
	}
}

// TestValidateEpicStatusRejectsUnknownStatuses exercises the pure validator.
func TestValidateEpicStatusRejectsUnknownStatuses(t *testing.T) {
	unknownStatusesFailClosed := func(raw string) bool {
		err := validateEpicStatus("unknown:" + raw)
		return err != nil && strings.Contains(err.Error(), "invalid epic status")
	}

	if err := quick.Check(unknownStatusesFailClosed, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("generated unknown epic status was accepted: %v", err)
	}
}

// TestStore_PersistenceRejectsInvalidDataTaskEpic proves the store-level atomic
// rejection for representative invalid values across tasks and epics, confirming
// nothing leaks to disk.
func TestStore_PersistenceRejectsInvalidDataTaskEpic(t *testing.T) {
	ctx := context.Background()

	t.Run("invalid_task_status", func(t *testing.T) {
		store := setupTestStore(t)
		_, err := store.Add(ctx, Task{WorkspaceID: "ws-1", Title: "Bad task", Status: "done"})
		if err == nil || !strings.Contains(err.Error(), "invalid task status") {
			t.Fatalf("error=%v want invalid task status", err)
		}
		tasks, listErr := store.ListByWorkspace(ctx, "ws-1")
		if listErr != nil {
			t.Fatalf("ListByWorkspace: %v", listErr)
		}
		if len(tasks) != 0 {
			t.Fatalf("invalid-status task was persisted: %+v", tasks)
		}
	})

	t.Run("invalid_task_review_status", func(t *testing.T) {
		store := setupTestStore(t)
		_, err := store.Add(ctx, Task{WorkspaceID: "ws-1", Title: "Bad review", Status: StatusReadyForReview, LastReviewStatus: "maybe"})
		if err == nil || !strings.Contains(err.Error(), "invalid task review status") {
			t.Fatalf("error=%v want invalid task review status", err)
		}
		tasks, listErr := store.ListByWorkspace(ctx, "ws-1")
		if listErr != nil {
			t.Fatalf("ListByWorkspace: %v", listErr)
		}
		if len(tasks) != 0 {
			t.Fatalf("invalid-review-status task was persisted: %+v", tasks)
		}
	})

	t.Run("invalid_epic_status", func(t *testing.T) {
		store := setupTestStore(t)
		_, err := store.AddEpic(ctx, Epic{WorkspaceID: "ws-1", Title: "Bad epic", Status: "open"})
		if err == nil || !strings.Contains(err.Error(), "invalid epic status") {
			t.Fatalf("error=%v want invalid epic status", err)
		}
		epics, listErr := store.ListEpics(ctx, "ws-1")
		if listErr != nil {
			t.Fatalf("ListEpics: %v", listErr)
		}
		if len(epics) != 0 {
			t.Fatalf("invalid-status epic was persisted: %+v", epics)
		}
	})

	t.Run("update_rejects_invalid_task_status", func(t *testing.T) {
		store := setupTestStore(t)
		task, err := store.Add(ctx, Task{WorkspaceID: "ws-1", Title: "Valid", Status: StatusPending})
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		task.Status = "archived"
		if _, err := store.Update(ctx, task); err == nil || !strings.Contains(err.Error(), "invalid task status") {
			t.Fatalf("error=%v want invalid task status", err)
		}
		got, err := store.Get(ctx, task.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Status != StatusPending {
			t.Fatalf("status=%q want %q", got.Status, StatusPending)
		}
	})
}

func TestStore_AddRejectsInvalidReviewStatus(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	_, err := store.Add(ctx, Task{
		WorkspaceID:      "ws-1",
		Title:            "Invalid review status",
		Status:           StatusReadyForReview,
		LastReviewStatus: "maybe",
		LastReviewID:     "review-1",
	})
	if err == nil {
		t.Fatal("expected invalid review status error")
	}
	if !strings.Contains(err.Error(), "invalid task review status") {
		t.Fatalf("error=%v want invalid task review status", err)
	}
	tasks, err := store.ListByWorkspace(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListByWorkspace: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("invalid-review-status task was persisted: %+v", tasks)
	}
}

func TestStore_UpdateRejectsInvalidReviewStatusWithoutMutatingExistingTask(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	task, err := store.Add(ctx, Task{
		WorkspaceID:      "ws-1",
		Title:            "Reviewed task",
		Status:           StatusReadyForReview,
		LastReviewStatus: ReviewStatusOK,
		LastReviewID:     "review-1",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	task.LastReviewStatus = "maybe"
	task.LastReviewID = "review-2"
	if _, err := store.Update(ctx, task); err == nil {
		t.Fatal("expected invalid review status error")
	} else if !strings.Contains(err.Error(), "invalid task review status") {
		t.Fatalf("error=%v want invalid task review status", err)
	}

	got, err := store.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LastReviewStatus != ReviewStatusOK {
		t.Fatalf("last_review_status=%q want %q", got.LastReviewStatus, ReviewStatusOK)
	}
	if got.LastReviewID != "review-1" {
		t.Fatalf("last_review_id=%q want review-1", got.LastReviewID)
	}
}

func TestStore_AddSubtaskRejectsInvalidReviewStatusWithoutLinkingChild(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	parent, err := store.Add(ctx, Task{
		WorkspaceID: "ws-1",
		Title:       "Parent",
		Status:      StatusPending,
	})
	if err != nil {
		t.Fatalf("Add parent: %v", err)
	}

	if _, err := store.AddSubtask(ctx, parent.ID, Task{
		WorkspaceID:      "ws-1",
		Title:            "Child",
		Status:           StatusReadyForReview,
		LastReviewStatus: "maybe",
	}); err == nil {
		t.Fatal("expected invalid review status error")
	} else if !strings.Contains(err.Error(), "invalid task review status") {
		t.Fatalf("error=%v want invalid task review status", err)
	}

	got, err := store.Get(ctx, parent.ID)
	if err != nil {
		t.Fatalf("Get parent: %v", err)
	}
	if len(got.Children) != 0 {
		t.Fatalf("parent children=%v want none after rejected child", got.Children)
	}
}

func TestStore_ReadsRejectCorruptPersistedTaskLifecycleValues(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		column     string
		value      string
		wantErr    string
		getOptions func(task Task) ListOptions
	}{
		{
			name:    "task status",
			column:  "status",
			value:   "done",
			wantErr: "invalid task status",
		},
		{
			name:    "review status",
			column:  "last_review_status",
			value:   "maybe",
			wantErr: "invalid task review status",
			getOptions: func(task Task) ListOptions {
				return ListOptions{Statuses: []string{task.Status}}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := setupTestStore(t)
			sqlStore := store.(*sqlStore)
			task, err := store.Add(ctx, Task{
				WorkspaceID:      "ws-corrupt-task",
				Title:            "Corrupt task",
				Status:           StatusReadyForReview,
				LastReviewStatus: ReviewStatusOK,
			})
			if err != nil {
				t.Fatalf("Add: %v", err)
			}
			if _, err := sqlStore.db.ExecContext(ctx, fmt.Sprintf("UPDATE tasks SET %s = $1 WHERE id = $2", tt.column), tt.value, task.ID); err != nil {
				t.Fatalf("corrupt task %s: %v", tt.column, err)
			}

			if _, err := store.Get(ctx, task.ID); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Get() error=%v want %q", err, tt.wantErr)
			}
			if _, err := store.ListByWorkspace(ctx, task.WorkspaceID); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ListByWorkspace() error=%v want %q", err, tt.wantErr)
			}
			opts := ListOptions{}
			if tt.getOptions != nil {
				opts = tt.getOptions(task)
			}
			if _, err := store.ListWithOptions(ctx, task.WorkspaceID, opts); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ListWithOptions() error=%v want %q", err, tt.wantErr)
			}
		})
	}
}

func TestStore_AddEpicRejectsInvalidStatus(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	_, err := store.AddEpic(ctx, Epic{
		WorkspaceID: "ws-1",
		Title:       "Invalid epic",
		Status:      "open",
	})
	if err == nil {
		t.Fatal("expected invalid epic status error")
	}
	if !strings.Contains(err.Error(), "invalid epic status") {
		t.Fatalf("error=%v want invalid epic status", err)
	}
	epics, err := store.ListEpics(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListEpics: %v", err)
	}
	if len(epics) != 0 {
		t.Fatalf("invalid-status epic was persisted: %+v", epics)
	}
}

func TestStore_UpdateEpicRejectsInvalidStatusWithoutMutatingExistingEpic(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	epic, err := store.AddEpic(ctx, Epic{
		WorkspaceID: "ws-1",
		Title:       "Valid epic",
		Status:      EpicStatusActive,
	})
	if err != nil {
		t.Fatalf("AddEpic: %v", err)
	}

	epic.Status = "open"
	if _, err := store.UpdateEpic(ctx, epic); err == nil {
		t.Fatal("expected invalid epic status error")
	} else if !strings.Contains(err.Error(), "invalid epic status") {
		t.Fatalf("error=%v want invalid epic status", err)
	}

	got, err := store.GetEpic(ctx, epic.ID)
	if err != nil {
		t.Fatalf("GetEpic: %v", err)
	}
	if got.Status != EpicStatusActive {
		t.Fatalf("status=%q want %q", got.Status, EpicStatusActive)
	}
}

func TestStore_ReadsRejectCorruptPersistedEpicStatus(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	sqlStore := store.(*sqlStore)

	epic, err := store.AddEpic(ctx, Epic{
		WorkspaceID: "ws-corrupt-epic",
		Title:       "Corrupt epic",
		Status:      EpicStatusActive,
	})
	if err != nil {
		t.Fatalf("AddEpic: %v", err)
	}
	if _, err := sqlStore.db.ExecContext(ctx, `UPDATE epics SET status = $1 WHERE id = $2`, "open", epic.ID); err != nil {
		t.Fatalf("corrupt epic status: %v", err)
	}

	if _, err := store.GetEpic(ctx, epic.ID); err == nil || !strings.Contains(err.Error(), "invalid epic status") {
		t.Fatalf("GetEpic() error=%v want invalid epic status", err)
	}
	if _, err := store.ListEpics(ctx, epic.WorkspaceID); err == nil || !strings.Contains(err.Error(), "invalid epic status") {
		t.Fatalf("ListEpics() error=%v want invalid epic status", err)
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

func TestStore_PersistsMilestoneLinkage(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	task, err := store.Add(ctx, Task{
		WorkspaceID: "ws-1",
		Title:       "Linked task",
		EpicID:      "epic-1",
		MilestoneID: "mile-1",
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	got, err := store.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.EpicID != "epic-1" {
		t.Fatalf("EpicID=%q want epic-1", got.EpicID)
	}
	if got.MilestoneID != "mile-1" {
		t.Fatalf("MilestoneID=%q want mile-1", got.MilestoneID)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if err := migrate(ctx, db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestMigrateReturnsIndexCreationError(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ExecContext(ctx, `CREATE TABLE idx_tasks_session (id TEXT)`); err != nil {
		t.Fatal(err)
	}

	err = migrate(ctx, db)
	if err == nil {
		t.Fatal("expected migration error")
	}
	if !strings.Contains(err.Error(), "idx_tasks_session") {
		t.Fatalf("error=%q want idx_tasks_session context", err)
	}
}

func TestStore_PersistsClaimAndBlockMetadata(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	task, err := store.Add(ctx, Task{
		WorkspaceID: "ws-1",
		Title:       "Claimed task",
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	now := time.Now().UTC()
	task.Status = StatusBlocked
	task.OwnerActorID = "agent-a"
	task.ClaimedAt = &now
	task.HeartbeatAt = &now
	task.BlockedReason = "waiting on review"
	task.BlockedAt = &now

	updated, err := store.Update(ctx, task)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.OwnerActorID != "agent-a" {
		t.Fatalf("owner=%q want agent-a", updated.OwnerActorID)
	}
	if updated.BlockedReason != "waiting on review" {
		t.Fatalf("blocked_reason=%q want waiting on review", updated.BlockedReason)
	}
	if updated.ClaimedAt == nil || updated.BlockedAt == nil || updated.HeartbeatAt == nil {
		t.Fatal("expected claim/block timestamps to be persisted")
	}
}

func TestStore_PersistsAssignmentMetadata(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	task, err := store.Add(ctx, Task{
		WorkspaceID: "ws-1",
		Title:       "Assigned task",
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	now := time.Now().UTC()
	task.AssignedActorID = "gemini-a"
	task.AssignedAt = &now

	updated, err := store.Update(ctx, task)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.AssignedActorID != "gemini-a" {
		t.Fatalf("assigned=%q want gemini-a", updated.AssignedActorID)
	}
	if updated.AssignedAt == nil {
		t.Fatal("expected assigned_at to be persisted")
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

func TestStore_SetActiveRejectsTaskFromDifferentWorkspace(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	task, err := store.Add(ctx, Task{WorkspaceID: "ws-owner", Title: "Owner task"})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if _, err := store.SetActive(ctx, "ws-other", task.ID); err == nil {
		t.Fatal("expected cross-workspace active task to be rejected")
	}

	if _, found, err := store.GetActive(ctx, "ws-other"); err != nil {
		t.Fatalf("GetActive failed: %v", err)
	} else if found {
		t.Fatal("cross-workspace active task was persisted")
	}
}

func TestStore_SetActivePropertyRejectsCrossWorkspaceTasks(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	property := func(rawOwner, rawOther string) bool {
		owner := "ws-" + safeTaskToken(rawOwner)
		other := "ws-" + safeTaskToken(rawOther)
		if owner == other {
			other += "-other"
		}

		task, err := store.Add(ctx, Task{
			WorkspaceID: owner,
			Title:       "Generated task",
		})
		if err != nil {
			t.Logf("Add: %v", err)
			return false
		}

		if _, err := store.SetActive(ctx, other, task.ID); err == nil {
			t.Logf("SetActive accepted task %q from %q as active for %q", task.ID, owner, other)
			return false
		}
		_, found, err := store.GetActive(ctx, other)
		return err == nil && !found
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("cross-workspace active task property failed: %v", err)
	}
}

func TestStore_RejectsCrossWorkspaceActiveEpicAndTaskLink(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	task, err := store.Add(ctx, Task{WorkspaceID: "ws-task", Title: "Task"})
	if err != nil {
		t.Fatalf("Add task failed: %v", err)
	}
	epic, err := store.AddEpic(ctx, Epic{WorkspaceID: "ws-epic", Title: "Epic"})
	if err != nil {
		t.Fatalf("AddEpic failed: %v", err)
	}

	if err := store.SetActiveEpic(ctx, "ws-other", "session-1", epic.ID); err == nil {
		t.Fatal("expected cross-workspace active epic to be rejected")
	}
	if _, found, err := store.GetActiveEpic(ctx, "ws-other", "session-1"); err != nil {
		t.Fatalf("GetActiveEpic failed: %v", err)
	} else if found {
		t.Fatal("cross-workspace active epic was persisted")
	}

	if err := store.LinkTaskToEpic(ctx, task.ID, epic.ID); err == nil {
		t.Fatal("expected cross-workspace task-to-epic link to be rejected")
	}
	got, err := store.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get task failed: %v", err)
	}
	if got.EpicID != "" {
		t.Fatalf("cross-workspace epic link was persisted: %q", got.EpicID)
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

func TestStore_AddSubtask_UpdatesParentChildrenAtomically(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	parent, err := store.Add(ctx, Task{
		WorkspaceID: "ws-1",
		Title:       "Parent",
	})
	if err != nil {
		t.Fatalf("Add parent failed: %v", err)
	}

	child, err := store.AddSubtask(ctx, parent.ID, Task{
		WorkspaceID: "ws-1",
		Title:       "Child",
	})
	if err != nil {
		t.Fatalf("AddSubtask failed: %v", err)
	}
	if child.ParentID != parent.ID {
		t.Fatalf("ParentID = %q, want %q", child.ParentID, parent.ID)
	}

	parent, err = store.Get(ctx, parent.ID)
	if err != nil {
		t.Fatalf("Get parent failed: %v", err)
	}
	if len(parent.Children) != 1 || parent.Children[0] != child.ID {
		t.Fatalf("parent children = %v, want [%s]", parent.Children, child.ID)
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
	if result.CompletedAt != nil {
		t.Errorf("expected CompletedAt to be cleared, got %v", result.CompletedAt)
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

func TestStore_Update_NormalizesCompletedAtFromStatus(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	task, err := store.Add(ctx, Task{
		WorkspaceID: "ws-1",
		Title:       "Normalize completion",
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	task.Status = StatusCompleted
	task.CompletedAt = nil
	task, err = store.Update(ctx, task)
	if err != nil {
		t.Fatalf("Update completed failed: %v", err)
	}
	if task.CompletedAt == nil {
		t.Fatal("expected CompletedAt to be set for completed task")
	}

	task.Status = StatusInProgress
	task, err = store.Update(ctx, task)
	if err != nil {
		t.Fatalf("Update in_progress failed: %v", err)
	}
	if task.CompletedAt != nil {
		t.Fatalf("expected CompletedAt to be cleared for non-completed task, got %v", task.CompletedAt)
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
	model := "text-embedding-qwen3-embedding-8b"

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

func TestStore_SetPageRanks(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	// Create test tasks
	task1, err := store.Add(ctx, Task{WorkspaceID: "ws-1", Title: "Task 1"})
	if err != nil {
		t.Fatalf("Add task1 failed: %v", err)
	}
	task2, err := store.Add(ctx, Task{WorkspaceID: "ws-1", Title: "Task 2"})
	if err != nil {
		t.Fatalf("Add task2 failed: %v", err)
	}
	task3, err := store.Add(ctx, Task{WorkspaceID: "ws-1", Title: "Task 3"})
	if err != nil {
		t.Fatalf("Add task3 failed: %v", err)
	}

	// Verify initial PageRank is 0
	got1, _ := store.Get(ctx, task1.ID)
	if got1.PageRank != 0 {
		t.Errorf("expected initial PageRank to be 0, got %f", got1.PageRank)
	}

	// Set PageRank values
	ranks := map[string]float64{
		task1.ID: 0.85,
		task2.ID: 0.12,
		task3.ID: 0.03,
	}
	if err := store.SetPageRanks(ctx, ranks); err != nil {
		t.Fatalf("SetPageRanks failed: %v", err)
	}

	// Verify PageRank values were persisted
	got1, _ = store.Get(ctx, task1.ID)
	if got1.PageRank != 0.85 {
		t.Errorf("task1 PageRank: expected 0.85, got %f", got1.PageRank)
	}
	got2, _ := store.Get(ctx, task2.ID)
	if got2.PageRank != 0.12 {
		t.Errorf("task2 PageRank: expected 0.12, got %f", got2.PageRank)
	}
	got3, _ := store.Get(ctx, task3.ID)
	if got3.PageRank != 0.03 {
		t.Errorf("task3 PageRank: expected 0.03, got %f", got3.PageRank)
	}

	// Verify empty map is a no-op
	if err := store.SetPageRanks(ctx, map[string]float64{}); err != nil {
		t.Errorf("SetPageRanks with empty map should not error: %v", err)
	}

	// Verify PageRanks appear in list queries
	tasks, _ := store.ListByWorkspace(ctx, "ws-1")
	var foundHighRank bool
	for _, task := range tasks {
		if task.ID == task1.ID && task.PageRank == 0.85 {
			foundHighRank = true
			break
		}
	}
	if !foundHighRank {
		t.Error("expected task1 with PageRank 0.85 in list results")
	}
}

func TestStore_SetPageRanksRejectsNonFiniteWithoutMutatingBatch(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	task1, err := store.Add(ctx, Task{WorkspaceID: "ws-1", Title: "Task 1"})
	if err != nil {
		t.Fatalf("Add task1 failed: %v", err)
	}
	task2, err := store.Add(ctx, Task{WorkspaceID: "ws-1", Title: "Task 2"})
	if err != nil {
		t.Fatalf("Add task2 failed: %v", err)
	}
	if err := store.SetPageRanks(ctx, map[string]float64{
		task1.ID: 0.25,
		task2.ID: 0.75,
	}); err != nil {
		t.Fatalf("SetPageRanks initial values failed: %v", err)
	}

	err = store.SetPageRanks(ctx, map[string]float64{
		task1.ID: 0.9,
		task2.ID: math.NaN(),
	})
	if err == nil {
		t.Fatal("expected non-finite pagerank error")
	}
	if !strings.Contains(err.Error(), "invalid pagerank") {
		t.Fatalf("error=%v want invalid pagerank", err)
	}

	got1, err := store.Get(ctx, task1.ID)
	if err != nil {
		t.Fatalf("Get task1 failed: %v", err)
	}
	got2, err := store.Get(ctx, task2.ID)
	if err != nil {
		t.Fatalf("Get task2 failed: %v", err)
	}
	if got1.PageRank != 0.25 || got2.PageRank != 0.75 {
		t.Fatalf("pageranks after rejected batch = (%f, %f), want (0.25, 0.75)", got1.PageRank, got2.PageRank)
	}
}

func TestStore_SetPageRanksRejectsGeneratedNonFiniteScores(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	task, err := store.Add(ctx, Task{WorkspaceID: "ws-1", Title: "Generated rank task"})
	if err != nil {
		t.Fatalf("Add task failed: %v", err)
	}

	property := func(raw float64) bool {
		err := store.SetPageRanks(ctx, map[string]float64{task.ID: raw})
		got, getErr := store.Get(ctx, task.ID)
		if getErr != nil {
			t.Logf("Get failed: %v", getErr)
			return false
		}
		if math.IsNaN(raw) || math.IsInf(raw, 0) {
			if err == nil {
				t.Logf("accepted non-finite pagerank %v", raw)
				return false
			}
			return !math.IsNaN(got.PageRank) && !math.IsInf(got.PageRank, 0)
		}
		return err == nil && got.PageRank == raw
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("generated pagerank property failed: %v", err)
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

func safeTaskToken(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		}
		if b.Len() >= 12 {
			break
		}
	}
	if b.Len() == 0 {
		return "empty"
	}
	return b.String()
}
