package taskhistory

import (
	"context"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	taskstore "github.com/jkatigb/agentctl/internal/storage/tasks"
)

func TestRefreshJidoRuntimeStateAddsTaskContinuity(t *testing.T) {
	ctx := context.Background()
	workspacePath := t.TempDir()
	storageRoot := t.TempDir()
	casRoot := t.TempDir()
	wsID := workspace.CanonicalID(workspacePath)

	wsStore := contextplane.NewWorkspaceStore(workspacePath)
	if _, err := wsStore.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID: wsID,
		Objective:   "Stabilize task continuity",
		Phase:       "implement",
		UpdatedAt:   time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}

	taskDB, err := taskstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("open tasks: %v", err)
	}
	defer taskDB.Close()
	if _, err := taskDB.Add(ctx, taskstore.Task{
		ID:          "task-1",
		WorkspaceID: wsID,
		Title:       "Stabilize task continuity",
		ScopePath:   "internal/contextplane/taskhistory/taskhistory.go",
		Status:      taskstore.StatusInProgress,
	}); err != nil {
		t.Fatalf("Add task: %v", err)
	}

	state := map[string]any{
		"workspace_root": workspacePath,
		"summary":        "runtime summary",
	}
	refreshed := RefreshJidoRuntimeState(ctx, storageRoot, casRoot, state)

	if _, ok := state["task_continuity"]; ok {
		t.Fatal("expected original state map to remain unchanged")
	}
	continuity, ok := refreshed["task_continuity"].(map[string]any)
	if !ok {
		t.Fatalf("task_continuity=%T", refreshed["task_continuity"])
	}
	if got := continuity["task_id"]; got != "task-1" {
		t.Fatalf("task_id=%v want task-1", got)
	}
	if got := continuity["task_title"]; got != "Stabilize task continuity" {
		t.Fatalf("task_title=%v", got)
	}
	if got := continuity["artifact"]; got == "" {
		t.Fatalf("artifact=%v want digest", got)
	}
}

func TestRefreshJidoRuntimeStateSkipsWithoutWorkspaceRoot(t *testing.T) {
	state := map[string]any{"summary": "runtime summary"}
	refreshed := RefreshJidoRuntimeState(context.Background(), t.TempDir(), t.TempDir(), state)
	if len(refreshed) != len(state) {
		t.Fatalf("state changed unexpectedly: %v", refreshed)
	}
	if _, ok := refreshed["task_continuity"]; ok {
		t.Fatalf("unexpected task_continuity=%v", refreshed["task_continuity"])
	}
}
