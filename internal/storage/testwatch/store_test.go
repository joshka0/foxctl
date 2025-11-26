package testwatch_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/testwatch"
)

func TestStore_UpsertAndGet(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := testwatch.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	now := time.Now().Truncate(time.Second)
	ts := testwatch.TestStatus{
		WorkspaceID: "ws-123",
		WatcherID:   "go",
		Status:      testwatch.StatusFail,
		Command:     "go test ./...",
		StartedAt:   &now,
		FinishedAt:  &now,
		Summary:     "1 failed, 10 passed",
		Failures: []testwatch.Failure{
			{
				Name:    "TestFoo",
				File:    "pkg/bar/foo_test.go",
				Line:    42,
				Message: "expected true, got false",
			},
		},
		RawTail: "--- FAIL: TestFoo...",
	}

	// Upsert
	if err := store.Upsert(ctx, ts); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Get
	got, found, err := store.Get(ctx, "ws-123", "go")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("expected to find status")
	}

	if got.WorkspaceID != ts.WorkspaceID {
		t.Errorf("WorkspaceID = %q, want %q", got.WorkspaceID, ts.WorkspaceID)
	}
	if got.WatcherID != ts.WatcherID {
		t.Errorf("WatcherID = %q, want %q", got.WatcherID, ts.WatcherID)
	}
	if got.Status != ts.Status {
		t.Errorf("Status = %q, want %q", got.Status, ts.Status)
	}
	if got.Command != ts.Command {
		t.Errorf("Command = %q, want %q", got.Command, ts.Command)
	}
	if got.Summary != ts.Summary {
		t.Errorf("Summary = %q, want %q", got.Summary, ts.Summary)
	}
	if len(got.Failures) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(got.Failures))
	}
	if got.Failures[0].Name != "TestFoo" {
		t.Errorf("Failure.Name = %q, want %q", got.Failures[0].Name, "TestFoo")
	}

	// Update
	ts.Status = testwatch.StatusPass
	ts.Summary = "11 passed"
	ts.Failures = nil
	if err := store.Upsert(ctx, ts); err != nil {
		t.Fatalf("Upsert (update): %v", err)
	}

	got, found, err = store.Get(ctx, "ws-123", "go")
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if !found {
		t.Fatal("expected to find status after update")
	}
	if got.Status != testwatch.StatusPass {
		t.Errorf("Status = %q, want %q", got.Status, testwatch.StatusPass)
	}
	if len(got.Failures) != 0 {
		t.Errorf("expected 0 failures, got %d", len(got.Failures))
	}
}

func TestStore_ListByWorkspace(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := testwatch.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Add multiple watchers for same workspace
	for _, id := range []string{"go", "js", "python"} {
		ts := testwatch.TestStatus{
			WorkspaceID: "ws-abc",
			WatcherID:   id,
			Status:      testwatch.StatusPass,
			Command:     id + " test",
		}
		if err := store.Upsert(ctx, ts); err != nil {
			t.Fatalf("Upsert %s: %v", id, err)
		}
	}

	// Add watcher for different workspace
	ts := testwatch.TestStatus{
		WorkspaceID: "ws-other",
		WatcherID:   "rust",
		Status:      testwatch.StatusPass,
		Command:     "cargo test",
	}
	if err := store.Upsert(ctx, ts); err != nil {
		t.Fatalf("Upsert other: %v", err)
	}

	// List by workspace
	list, err := store.ListByWorkspace(ctx, "ws-abc")
	if err != nil {
		t.Fatalf("ListByWorkspace: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 statuses, got %d", len(list))
	}

	// List by other workspace
	list, err = store.ListByWorkspace(ctx, "ws-other")
	if err != nil {
		t.Fatalf("ListByWorkspace other: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 status, got %d", len(list))
	}
}

func TestStore_Delete(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := testwatch.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	ts := testwatch.TestStatus{
		WorkspaceID: "ws-del",
		WatcherID:   "go",
		Status:      testwatch.StatusPass,
		Command:     "go test",
	}
	if err := store.Upsert(ctx, ts); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Delete
	if err := store.Delete(ctx, "ws-del", "go"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify deleted
	_, found, err := store.Get(ctx, "ws-del", "go")
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if found {
		t.Error("expected not found after delete")
	}
}

func TestStore_DeleteByWorkspace(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := testwatch.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Add multiple watchers
	for _, id := range []string{"go", "js"} {
		ts := testwatch.TestStatus{
			WorkspaceID: "ws-bulk",
			WatcherID:   id,
			Status:      testwatch.StatusPass,
			Command:     id + " test",
		}
		if err := store.Upsert(ctx, ts); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	// Delete all
	if err := store.DeleteByWorkspace(ctx, "ws-bulk"); err != nil {
		t.Fatalf("DeleteByWorkspace: %v", err)
	}

	// Verify all deleted
	list, err := store.ListByWorkspace(ctx, "ws-bulk")
	if err != nil {
		t.Fatalf("ListByWorkspace: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 statuses, got %d", len(list))
	}
}

func TestConfig_LoadAndSave(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &testwatch.Config{
		Debounce: 3 * time.Second,
		Watchers: []testwatch.WatcherConfig{
			{
				ID:          "go",
				Command:     "go test ./...",
				Include:     []string{"**/*.go"},
				Debounce:    2 * time.Second,
				MinInterval: 30 * time.Second,
			},
			{
				ID:      "js",
				Command: "npm test",
				Include: []string{"**/*.js", "**/*.ts"},
			},
		},
	}

	// Save
	if err := testwatch.SaveConfig(tmpDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// Verify file exists
	path := testwatch.ConfigPath(tmpDir)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file not found: %v", err)
	}

	// Load
	loaded, err := testwatch.LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if loaded.Debounce != cfg.Debounce {
		t.Errorf("Debounce = %v, want %v", loaded.Debounce, cfg.Debounce)
	}
	if len(loaded.Watchers) != 2 {
		t.Fatalf("expected 2 watchers, got %d", len(loaded.Watchers))
	}

	goWatcher := loaded.GetWatcher("go")
	if goWatcher == nil {
		t.Fatal("expected to find 'go' watcher")
	}
	if goWatcher.Command != "go test ./..." {
		t.Errorf("Command = %q, want %q", goWatcher.Command, "go test ./...")
	}
	if goWatcher.Debounce != 2*time.Second {
		t.Errorf("Debounce = %v, want %v", goWatcher.Debounce, 2*time.Second)
	}
}

func TestConfig_UpsertAndRemoveWatcher(t *testing.T) {
	cfg := testwatch.NewConfig()

	// Add
	cfg.UpsertWatcher(testwatch.WatcherConfig{
		ID:      "go",
		Command: "go test",
	})
	if len(cfg.Watchers) != 1 {
		t.Fatalf("expected 1 watcher, got %d", len(cfg.Watchers))
	}

	// Update
	cfg.UpsertWatcher(testwatch.WatcherConfig{
		ID:      "go",
		Command: "go test ./...",
	})
	if len(cfg.Watchers) != 1 {
		t.Fatalf("expected 1 watcher after update, got %d", len(cfg.Watchers))
	}
	if cfg.Watchers[0].Command != "go test ./..." {
		t.Errorf("Command = %q, want %q", cfg.Watchers[0].Command, "go test ./...")
	}

	// Remove
	if !cfg.RemoveWatcher("go") {
		t.Error("expected RemoveWatcher to return true")
	}
	if len(cfg.Watchers) != 0 {
		t.Errorf("expected 0 watchers, got %d", len(cfg.Watchers))
	}

	// Remove non-existent
	if cfg.RemoveWatcher("go") {
		t.Error("expected RemoveWatcher to return false for non-existent")
	}
}

func TestConfig_EffectiveDebounce(t *testing.T) {
	cfg := &testwatch.Config{
		Debounce: 5 * time.Second,
	}

	// Watcher with own debounce
	w1 := testwatch.WatcherConfig{
		ID:       "go",
		Debounce: 3 * time.Second,
	}
	if w1.EffectiveDebounce(cfg) != 3*time.Second {
		t.Errorf("expected 3s, got %v", w1.EffectiveDebounce(cfg))
	}

	// Watcher without debounce uses config
	w2 := testwatch.WatcherConfig{ID: "js"}
	if w2.EffectiveDebounce(cfg) != 5*time.Second {
		t.Errorf("expected 5s, got %v", w2.EffectiveDebounce(cfg))
	}

	// Watcher without config uses default
	if w2.EffectiveDebounce(nil) != testwatch.DefaultDebounce {
		t.Errorf("expected default, got %v", w2.EffectiveDebounce(nil))
	}
}

func TestConfig_ConfigExists(t *testing.T) {
	tmpDir := t.TempDir()

	// No config
	if testwatch.ConfigExists(tmpDir) {
		t.Error("expected ConfigExists to return false")
	}

	// Create config
	cfg := testwatch.NewConfig()
	if err := testwatch.SaveConfig(tmpDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// Config exists
	if !testwatch.ConfigExists(tmpDir) {
		t.Error("expected ConfigExists to return true")
	}
}

func TestConfig_LoadNonExistent(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := testwatch.LoadConfig(tmpDir)
	if err == nil {
		t.Error("expected error for non-existent config")
	}
}

func TestConfig_SaveCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	workspaceRoot := filepath.Join(tmpDir, "workspace")

	cfg := testwatch.NewConfig()
	if err := testwatch.SaveConfig(workspaceRoot, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// Verify .agentctl dir was created
	agentctlDir := filepath.Join(workspaceRoot, ".agentctl")
	if _, err := os.Stat(agentctlDir); err != nil {
		t.Errorf("expected .agentctl dir to exist: %v", err)
	}
}
