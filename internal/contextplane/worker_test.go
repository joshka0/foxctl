package contextplane

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/config"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	taskstore "github.com/jkatigb/agentctl/internal/storage/tasks"
)

func TestWorkerRunOnce(t *testing.T) {
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	cfg := config.Config{Storage: config.StorageSettings{Root: storageRoot}}

	tasksDB, err := taskstore.Open(context.Background(), storageRoot)
	if err != nil {
		t.Fatalf("Open tasks: %v", err)
	}
	defer tasksDB.Close()
	if _, err := tasksDB.Add(context.Background(), taskstore.Task{
		WorkspaceID: ws.ID(workspace),
		Title:       "Formalize ACA",
		Status:      taskstore.StatusPending,
		ScopePath:   "internal/contextplane",
	}); err != nil {
		t.Fatalf("Add task: %v", err)
	}

	store := NewWorkspaceStore(workspace)
	if _, err := store.AppendTension(Tension{
		Kind:        "contradiction",
		Statement:   "Runtime writes are bypassing the promotion path.",
		Impact:      "high",
		Status:      "open",
		Count:       2,
		RelatedRefs: []string{"note:write-policy"},
		CreatedAt:   time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("AppendTension: %v", err)
	}

	worker := NewWorker(WorkerConfig{
		Config:    cfg,
		Workspace: workspace,
	})
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	top, err := store.LoadTopOfMind()
	if err != nil {
		t.Fatalf("LoadTopOfMind: %v", err)
	}
	if top.Objective == "" {
		t.Fatalf("expected non-empty objective")
	}
	tasks, err := store.ListMaintenanceTasks(10)
	if err != nil {
		t.Fatalf("ListMaintenanceTasks: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatalf("expected maintenance tasks")
	}
}

func TestWorkerRunOnceWithVaultHealth(t *testing.T) {
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	cfg := config.Config{Storage: config.StorageSettings{Root: storageRoot}}
	vaultRoot := filepath.Clean(filepath.Join(repoRootForWorkerTest(t), "..", "tools", "obsidian", "testdata", "vaults", "basic"))

	worker := NewWorker(WorkerConfig{
		Config:    cfg,
		Workspace: workspace,
		VaultPath: vaultRoot,
	})
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce with vault: %v", err)
	}

	store := NewWorkspaceStore(workspace)
	tasks, err := store.ListMaintenanceTasks(50)
	if err != nil {
		t.Fatalf("ListMaintenanceTasks: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatalf("expected health-derived maintenance tasks")
	}
}

func repoRootForWorkerTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime caller unavailable")
	}
	return filepath.Dir(file)
}
