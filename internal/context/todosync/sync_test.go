package todosync

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/storage/tasks"
)

func TestSyncFromProviderUpdatesExistingTaskBackToPending(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTodoSyncTaskStore(t)
	task, err := store.Add(ctx, tasks.Task{
		WorkspaceID: "ws-1",
		Title:       "Review patch",
		Status:      StatusInProgress,
		SessionID:   "session-1",
	})
	if err != nil {
		t.Fatalf("Add task: %v", err)
	}

	result, err := NewService(store).SyncFromProvider(ctx, InboundSyncInput{
		WorkspaceID: "ws-1",
		SessionID:   "session-1",
		Todos: []ClaudeTodo{{
			Content: AppendTaskID("Review patch", task.ID),
			Status:  "pending",
		}},
	})
	if err != nil {
		t.Fatalf("SyncFromProvider: %v", err)
	}

	if result.Updated != 1 {
		t.Fatalf("Updated=%d want 1", result.Updated)
	}
	got, err := store.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get task: %v", err)
	}
	if got.Status != StatusPending {
		t.Fatalf("status=%q want %q", got.Status, StatusPending)
	}
}

func TestSyncFromProviderMarksTaskInProgressAndActive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTodoSyncTaskStore(t)
	task, err := store.Add(ctx, tasks.Task{
		WorkspaceID: "ws-1",
		Title:       "Implement parser",
		Status:      StatusPending,
		SessionID:   "session-1",
	})
	if err != nil {
		t.Fatalf("Add task: %v", err)
	}

	result, err := NewService(store).SyncFromProvider(ctx, InboundSyncInput{
		WorkspaceID: "ws-1",
		SessionID:   "session-1",
		Todos: []ClaudeTodo{{
			Content: AppendTaskID("Implement parser", task.ID),
			Status:  "in_progress",
		}},
	})
	if err != nil {
		t.Fatalf("SyncFromProvider: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("Updated=%d want 1", result.Updated)
	}

	got, err := store.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get task: %v", err)
	}
	if got.Status != StatusInProgress {
		t.Fatalf("status=%q want %q", got.Status, StatusInProgress)
	}
	active, found, err := store.GetActive(ctx, "ws-1")
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if !found || active.ID != task.ID {
		t.Fatalf("active task = (%q, %v), want %q found", active.ID, found, task.ID)
	}
}

func TestSyncFromProviderCompletesExistingTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTodoSyncTaskStore(t)
	task, err := store.Add(ctx, tasks.Task{
		WorkspaceID: "ws-1",
		Title:       "Ship parser",
		Status:      StatusInProgress,
		SessionID:   "session-1",
	})
	if err != nil {
		t.Fatalf("Add task: %v", err)
	}

	result, err := NewService(store).SyncFromProvider(ctx, InboundSyncInput{
		WorkspaceID: "ws-1",
		SessionID:   "session-1",
		Todos: []ClaudeTodo{{
			Content: AppendTaskID("Ship parser", task.ID),
			Status:  "completed",
		}},
	})
	if err != nil {
		t.Fatalf("SyncFromProvider: %v", err)
	}
	if result.Completed != 1 {
		t.Fatalf("Completed=%d want 1", result.Completed)
	}

	got, err := store.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get task: %v", err)
	}
	if got.Status != StatusCompleted {
		t.Fatalf("status=%q want %q", got.Status, StatusCompleted)
	}
	if got.CompletedAt == nil {
		t.Fatal("CompletedAt=nil want timestamp")
	}
}

func TestSyncRoundTripPreservesFoxctlOnlyStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status string
	}{
		{name: "blocked stays blocked", status: StatusBlocked},
		{name: "canceled stays canceled", status: StatusCanceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := openTodoSyncTaskStore(t)
			task, err := store.Add(ctx, tasks.Task{
				WorkspaceID: "ws-1",
				Title:       "Keep state",
				Status:      tt.status,
				SessionID:   "session-1",
			})
			if err != nil {
				t.Fatalf("Add task: %v", err)
			}

			service := NewService(store)
			outbound, err := service.SyncToProvider(ctx, OutboundSyncInput{
				WorkspaceID: "ws-1",
				SessionID:   "session-1",
			})
			if err != nil {
				t.Fatalf("SyncToProvider: %v", err)
			}
			if len(outbound.Todos) != 1 {
				t.Fatalf("projected todos=%d want 1", len(outbound.Todos))
			}

			inbound, err := service.SyncFromProvider(ctx, InboundSyncInput{
				WorkspaceID: "ws-1",
				SessionID:   "session-1",
				Todos:       outbound.Todos,
			})
			if err != nil {
				t.Fatalf("SyncFromProvider: %v", err)
			}
			if inbound.Updated != 0 || inbound.Completed != 0 || inbound.Removed != 0 {
				t.Fatalf("unexpected sync changes after round-trip: %+v", inbound.SyncResult)
			}
			got, err := store.Get(ctx, task.ID)
			if err != nil {
				t.Fatalf("Get task: %v", err)
			}
			if got.Status != tt.status {
				t.Fatalf("status=%q want %q after round-trip todo=%+v", got.Status, tt.status, outbound.Todos[0])
			}
		})
	}
}

func TestSyncFromProviderDoesNotRestoreFoxctlOnlyStatusFromUntaggedGlyph(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTodoSyncTaskStore(t)
	result, err := NewService(store).SyncFromProvider(ctx, InboundSyncInput{
		WorkspaceID: "ws-1",
		SessionID:   "session-1",
		Todos: []ClaudeTodo{{
			Content: GlyphCanceled + " Actually done",
			Status:  "completed",
		}},
	})
	if err != nil {
		t.Fatalf("SyncFromProvider: %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("Created=%d want 1", result.Created)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("Tasks=%d want 1", len(result.Tasks))
	}
	got, err := store.Get(ctx, result.Tasks[0].ID)
	if err != nil {
		t.Fatalf("Get task: %v", err)
	}
	if got.Status != StatusCompleted {
		t.Fatalf("status=%q want %q", got.Status, StatusCompleted)
	}
}

func openTodoSyncTaskStore(t *testing.T) tasks.Store {
	t.Helper()

	store, err := tasks.Open(context.Background(), filepath.Join(t.TempDir(), "storage"))
	if err != nil {
		t.Fatalf("open task store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}
