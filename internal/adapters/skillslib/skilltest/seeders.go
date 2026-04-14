package skilltest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/joshka0/foxctl/internal/storage/tasks"
)

// TaskSeed defines initial data for seeding a task.
type TaskSeed struct {
	ID          string
	Title       string
	Description string
	Status      string
	WorkspaceID string
	SessionID   string
	EpicID      string
}

// MemorySeed defines initial data for seeding a named memory.
type MemorySeed struct {
	Name      string
	Type      string
	Workspace string
	Summary   string
	Result    []byte
	SessionID string
}

// EpicSeed defines initial data for seeding an epic.
type EpicSeed struct {
	ID          string
	Title       string
	Goal        string
	Status      string
	WorkspaceID string
	SessionID   string
}

// TestTaskStore provides a task store seeded with test data.
// The returned cleanup function should be deferred.
func TestTaskStore(t *testing.T, seeds []TaskSeed) (tasks.Store, func()) {
	t.Helper()

	ctx := context.Background()
	root := t.TempDir()

	store, err := tasks.Open(ctx, root)
	if err != nil {
		t.Fatalf("open task store: %v", err)
	}

	for _, seed := range seeds {
		task := tasks.Task{
			ID:          seed.ID,
			Title:       seed.Title,
			Description: seed.Description,
			Status:      seed.Status,
			WorkspaceID: seed.WorkspaceID,
			SessionID:   seed.SessionID,
			EpicID:      seed.EpicID,
			CreatedAt:   time.Now().UTC(),
		}
		if task.Status == "" {
			task.Status = tasks.StatusPending
		}
		if _, err := store.Add(ctx, task); err != nil {
			t.Fatalf("seed task %q: %v", seed.Title, err)
		}
	}

	return store, func() { store.Close() }
}

// TestMemoryStore provides a memory store seeded with test data.
// The returned cleanup function should be deferred.
func TestMemoryStore(t *testing.T, seeds []MemorySeed) (*memory.Store, func()) {
	t.Helper()

	ctx := context.Background()
	root := t.TempDir()
	casPath := filepath.Join(root, "cas")

	store, err := memory.Open(ctx, root, casPath)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}

	for _, seed := range seeds {
		entry := storage.NamedEntry{
			Name:      seed.Name,
			Type:      seed.Type,
			Workspace: seed.Workspace,
			Summary:   seed.Summary,
			Result:    seed.Result,
			SessionID: seed.SessionID,
		}
		if entry.Type == "" {
			entry.Type = "result"
		}
		if _, err := store.Save(ctx, entry); err != nil {
			t.Fatalf("seed memory %q: %v", seed.Name, err)
		}
	}

	return store, func() { store.Close() }
}

// TestEpicStore provides a task store with seeded epics.
// The returned cleanup function should be deferred.
func TestEpicStore(t *testing.T, epicSeeds []EpicSeed, taskSeeds []TaskSeed) (tasks.Store, func()) {
	t.Helper()

	ctx := context.Background()
	root := t.TempDir()

	store, err := tasks.Open(ctx, root)
	if err != nil {
		t.Fatalf("open task store: %v", err)
	}

	// Seed epics first
	for _, seed := range epicSeeds {
		epic := tasks.Epic{
			ID:          seed.ID,
			Title:       seed.Title,
			Goal:        seed.Goal,
			Status:      seed.Status,
			WorkspaceID: seed.WorkspaceID,
			SessionID:   seed.SessionID,
			CreatedAt:   time.Now().UTC(),
		}
		if epic.Status == "" {
			epic.Status = tasks.EpicStatusActive
		}
		if _, err := store.AddEpic(ctx, epic); err != nil {
			t.Fatalf("seed epic %q: %v", seed.Title, err)
		}
	}

	// Seed tasks (may be linked to epics)
	for _, seed := range taskSeeds {
		task := tasks.Task{
			ID:          seed.ID,
			Title:       seed.Title,
			Description: seed.Description,
			Status:      seed.Status,
			WorkspaceID: seed.WorkspaceID,
			SessionID:   seed.SessionID,
			EpicID:      seed.EpicID,
			CreatedAt:   time.Now().UTC(),
		}
		if task.Status == "" {
			task.Status = tasks.StatusPending
		}
		if _, err := store.Add(ctx, task); err != nil {
			t.Fatalf("seed task %q: %v", seed.Title, err)
		}
	}

	return store, func() { store.Close() }
}

// SeedTask is a convenience function to add a single task to an existing store.
func SeedTask(t *testing.T, store tasks.Store, seed TaskSeed) tasks.Task {
	t.Helper()

	ctx := context.Background()
	task := tasks.Task{
		ID:          seed.ID,
		Title:       seed.Title,
		Description: seed.Description,
		Status:      seed.Status,
		WorkspaceID: seed.WorkspaceID,
		SessionID:   seed.SessionID,
		EpicID:      seed.EpicID,
		CreatedAt:   time.Now().UTC(),
	}
	if task.Status == "" {
		task.Status = tasks.StatusPending
	}
	result, err := store.Add(ctx, task)
	if err != nil {
		t.Fatalf("seed task %q: %v", seed.Title, err)
	}
	return result
}

// SeedMemory is a convenience function to add a single memory to an existing store.
func SeedMemory(t *testing.T, store *memory.Store, seed MemorySeed) storage.NamedEntry {
	t.Helper()

	ctx := context.Background()
	entry := storage.NamedEntry{
		Name:      seed.Name,
		Type:      seed.Type,
		Workspace: seed.Workspace,
		Summary:   seed.Summary,
		Result:    seed.Result,
		SessionID: seed.SessionID,
	}
	if entry.Type == "" {
		entry.Type = "result"
	}
	result, err := store.Save(ctx, entry)
	if err != nil {
		t.Fatalf("seed memory %q: %v", seed.Name, err)
	}
	return result
}

// SeedEpic is a convenience function to add a single epic to an existing store.
func SeedEpic(t *testing.T, store tasks.Store, seed EpicSeed) tasks.Epic {
	t.Helper()

	ctx := context.Background()
	epic := tasks.Epic{
		ID:          seed.ID,
		Title:       seed.Title,
		Goal:        seed.Goal,
		Status:      seed.Status,
		WorkspaceID: seed.WorkspaceID,
		SessionID:   seed.SessionID,
		CreatedAt:   time.Now().UTC(),
	}
	if epic.Status == "" {
		epic.Status = tasks.EpicStatusActive
	}
	result, err := store.AddEpic(ctx, epic)
	if err != nil {
		t.Fatalf("seed epic %q: %v", seed.Title, err)
	}
	return result
}

// QuickTasks creates a slice of TaskSeed from simple title strings.
// All tasks use the same workspace and session, with pending status.
func QuickTasks(workspaceID, sessionID string, titles ...string) []TaskSeed {
	seeds := make([]TaskSeed, len(titles))
	for i, title := range titles {
		seeds[i] = TaskSeed{
			Title:       title,
			Status:      tasks.StatusPending,
			WorkspaceID: workspaceID,
			SessionID:   sessionID,
		}
	}
	return seeds
}

// QuickMemories creates a slice of MemorySeed from name/summary pairs.
func QuickMemories(workspace string, entries ...string) []MemorySeed {
	if len(entries)%2 != 0 {
		panic("QuickMemories requires even number of arguments (name/summary pairs)")
	}
	seeds := make([]MemorySeed, len(entries)/2)
	for i := 0; i < len(entries); i += 2 {
		seeds[i/2] = MemorySeed{
			Name:      entries[i],
			Summary:   entries[i+1],
			Type:      "result",
			Workspace: workspace,
		}
	}
	return seeds
}
