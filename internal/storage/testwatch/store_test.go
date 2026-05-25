package testwatch_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage/testwatch"
)

func TestStore_UpsertAndGet(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := testwatch.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

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

func TestStore_UpsertAcceptsKnownStatuses(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := testwatch.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	statuses := []testwatch.Status{
		testwatch.StatusUnknown,
		testwatch.StatusPass,
		testwatch.StatusFail,
		testwatch.StatusError,
		testwatch.StatusRunning,
	}

	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			err := store.Upsert(ctx, testwatch.TestStatus{
				WorkspaceID: "ws-statuses",
				WatcherID:   "watcher-" + string(status),
				Status:      status,
				Command:     "test command",
			})
			if err != nil {
				t.Fatalf("Upsert status %q: %v", status, err)
			}
		})
	}
}

func TestStore_UpsertRejectsInvalidStatus(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := testwatch.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	err = store.Upsert(ctx, testwatch.TestStatus{
		WorkspaceID: "ws-invalid",
		WatcherID:   "go",
		Status:      testwatch.Status("not-a-status"),
		Command:     "go test ./...",
	})
	if err == nil {
		t.Fatalf("expected invalid status to be rejected")
	}

	statuses, err := store.ListByWorkspace(ctx, "ws-invalid")
	if err != nil {
		t.Fatalf("ListByWorkspace: %v", err)
	}
	if len(statuses) != 0 {
		t.Fatalf("invalid status was persisted: %+v", statuses)
	}
}

func TestStore_UpsertRejectsInvalidStatusWithoutMutatingExisting(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := testwatch.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	if err := store.Upsert(ctx, testwatch.TestStatus{
		WorkspaceID: "ws-update-invalid",
		WatcherID:   "go",
		Status:      testwatch.StatusPass,
		Command:     "go test ./...",
		Summary:     "original summary",
	}); err != nil {
		t.Fatalf("initial Upsert: %v", err)
	}

	err = store.Upsert(ctx, testwatch.TestStatus{
		WorkspaceID: "ws-update-invalid",
		WatcherID:   "go",
		Status:      testwatch.Status("not-a-status"),
		Command:     "go test ./...",
		Summary:     "mutated summary",
	})
	if err == nil {
		t.Fatalf("expected invalid status update to be rejected")
	}

	got, found, err := store.Get(ctx, "ws-update-invalid", "go")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("expected existing status")
	}
	if got.Status != testwatch.StatusPass {
		t.Fatalf("Status = %q, want %q", got.Status, testwatch.StatusPass)
	}
	if got.Summary != "original summary" {
		t.Fatalf("Summary = %q, want original summary", got.Summary)
	}
}

func TestStore_UpsertRejectsNegativeFailureLine(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := testwatch.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	err = store.Upsert(ctx, testwatch.TestStatus{
		WorkspaceID: "ws-invalid-failure",
		WatcherID:   "go",
		Status:      testwatch.StatusFail,
		Command:     "go test ./...",
		Failures: []testwatch.Failure{
			{Name: "TestFoo", Line: -1},
		},
	})
	if err == nil {
		t.Fatalf("expected negative failure line to be rejected")
	}

	statuses, err := store.ListByWorkspace(ctx, "ws-invalid-failure")
	if err != nil {
		t.Fatalf("ListByWorkspace: %v", err)
	}
	if len(statuses) != 0 {
		t.Fatalf("invalid failure was persisted: %+v", statuses)
	}
}

func TestStore_ListByWorkspace(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := testwatch.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

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
	defer store.Close()

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
	defer store.Close()

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
		return
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

	// Verify .foxctl dir was created
	foxctlDir := filepath.Join(workspaceRoot, ".foxctl")
	if _, err := os.Stat(foxctlDir); err != nil {
		t.Errorf("expected .foxctl dir to exist: %v", err)
	}
}
