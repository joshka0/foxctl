package tools

import (
	"context"
	"testing"

	"github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTodoAdd_CreatesTask(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	cfg := Config{
		WorkspaceRoot: rootDir,
		WorkspaceID:   "ws-1",
		OpenTasksStore: func(ctx context.Context) (tasks.Store, error) {
			return tasks.Open(ctx, rootDir)
		},
	}
	r, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	args := map[string]any{
		"title":       "New Task",
		"description": "Desc",
	}
	result, err := r.todoAdd(ctx, args)
	require.NoError(t, err)
	parsed := parseResult(t, result)
	assert.True(t, parsed["success"].(bool))

	taskMap := parsed["task"].(map[string]any)
	assert.Equal(t, "New Task", taskMap["Title"])

	// Verify persistence
	store, err := tasks.Open(ctx, rootDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	all, err := store.ListByWorkspace(ctx, "ws-1")
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "New Task", all[0].Title)
}

func TestTodoComplete_UpdatesStatus(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()
	store, err := tasks.Open(ctx, rootDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	t1, err := store.Add(ctx, tasks.Task{
		WorkspaceID: "ws-1",
		Title:       "Task 1",
		Status:      tasks.StatusPending,
	})
	require.NoError(t, err)

	cfg := Config{
		WorkspaceRoot: rootDir,
		WorkspaceID:   "ws-1",
		OpenTasksStore: func(ctx context.Context) (tasks.Store, error) {
			return tasks.Open(ctx, rootDir)
		},
	}
	r, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	result, err := r.todoComplete(ctx, map[string]any{
		"id":      t1.ID,
		"summary": "Done",
	})
	require.NoError(t, err)
	parsed := parseResult(t, result)
	assert.True(t, parsed["success"].(bool))

	updated, err := store.Get(ctx, t1.ID)
	require.NoError(t, err)
	assert.Equal(t, tasks.StatusCompleted, updated.Status)
	assert.NotNil(t, updated.CompletedAt)
	assert.Contains(t, updated.Notes, "Done")
}

func TestTodoAdd_UpdatesParentChildren(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()
	store, err := tasks.Open(ctx, rootDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	parent, err := store.Add(ctx, tasks.Task{
		WorkspaceID: "ws-1",
		Title:       "Parent",
	})
	require.NoError(t, err)

	cfg := Config{
		WorkspaceRoot: rootDir,
		WorkspaceID:   "ws-1",
		OpenTasksStore: func(ctx context.Context) (tasks.Store, error) {
			return tasks.Open(ctx, rootDir)
		},
	}
	r, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	result, err := r.todoAdd(ctx, map[string]any{
		"title":     "Child",
		"parent_id": parent.ID,
	})
	require.NoError(t, err)
	parsed := parseResult(t, result)
	assert.True(t, parsed["success"].(bool))

	parent, err = store.Get(ctx, parent.ID)
	require.NoError(t, err)
	require.Len(t, parent.Children, 1)
}

func TestTodoQuery_FiltersStatus(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()
	store, err := tasks.Open(ctx, rootDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	_, err = store.Add(ctx, tasks.Task{WorkspaceID: "ws-1", Title: "T1", Status: tasks.StatusPending})
	require.NoError(t, err)
	_, err = store.Add(ctx, tasks.Task{WorkspaceID: "ws-1", Title: "T2", Status: tasks.StatusCompleted})
	require.NoError(t, err)

	cfg := Config{
		WorkspaceRoot: rootDir,
		WorkspaceID:   "ws-1",
		OpenTasksStore: func(ctx context.Context) (tasks.Store, error) {
			return tasks.Open(ctx, rootDir)
		},
	}
	r, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	// Filter pending
	result, err := r.todoQuery(ctx, map[string]any{"status": "pending"})
	require.NoError(t, err)
	parsed := parseResult(t, result)
	assert.Equal(t, 1, int(parsed["count"].(float64)))
	tasksList := parsed["tasks"].([]any)
	assert.Equal(t, "T1", tasksList[0].(map[string]any)["Title"])
}

func TestTodoSetActive_PersistsSelection(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()
	store, err := tasks.Open(ctx, rootDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	t1, err := store.Add(ctx, tasks.Task{WorkspaceID: "ws-1", Title: "T1"})
	require.NoError(t, err)

	cfg := Config{
		WorkspaceRoot: rootDir,
		WorkspaceID:   "ws-1",
		OpenTasksStore: func(ctx context.Context) (tasks.Store, error) {
			return tasks.Open(ctx, rootDir)
		},
	}
	r, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	result, err := r.todoSetActive(ctx, map[string]any{"task_id": t1.ID})
	require.NoError(t, err)
	parseResult(t, result)

	active, found, err := store.GetActive(ctx, "ws-1")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, t1.ID, active.ID)
}

func TestTodoEnsureActive_CreatesIfMissing(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	cfg := Config{
		WorkspaceRoot: rootDir,
		WorkspaceID:   "ws-1",
		OpenTasksStore: func(ctx context.Context) (tasks.Store, error) {
			return tasks.Open(ctx, rootDir)
		},
	}
	r, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	result, err := r.todoEnsureActive(ctx, map[string]any{
		"default_title": "Default Task",
	})
	require.NoError(t, err)
	parsed := parseResult(t, result)
	assert.True(t, parsed["created"].(bool))

	taskMap := parsed["active_task"].(map[string]any)
	assert.Equal(t, "Default Task", taskMap["Title"])
}

func TestTodoGraphInsights_ComputesMetrics(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()
	store, err := tasks.Open(ctx, rootDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	t1, err := store.Add(ctx, tasks.Task{WorkspaceID: "ws-1", Title: "T1"})
	require.NoError(t, err)
	t2, err := store.Add(ctx, tasks.Task{WorkspaceID: "ws-1", Title: "T2", DependsOn: []string{t1.ID}})
	require.NoError(t, err)

	cfg := Config{
		WorkspaceRoot: rootDir,
		WorkspaceID:   "ws-1",
		OpenTasksStore: func(ctx context.Context) (tasks.Store, error) {
			return tasks.Open(ctx, rootDir)
		},
	}
	r, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	result, err := r.todoGraphInsights(ctx, map[string]any{})
	require.NoError(t, err)
	parsed := parseResult(t, result)

	nodes := parsed["nodes"].([]any)
	require.Len(t, nodes, 2)

	foundT1, foundT2 := false, false
	for _, n := range nodes {
		nm := n.(map[string]any)
		if nm["task_id"] == t1.ID {
			foundT1 = true
		}
		if nm["task_id"] == t2.ID {
			foundT2 = true
		}
	}
	assert.True(t, foundT1)
	assert.True(t, foundT2)
}
