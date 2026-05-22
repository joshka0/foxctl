package testwatch

import (
	"context"
	"strings"
	"testing"
)

func TestDecodeRejectsCorruptTestStatusFields(t *testing.T) {
	ctx := context.Background()

	t.Run("failures json", func(t *testing.T) {
		store := openDecodeTestStore(t)
		defer store.Close()

		upsertDecodeStatus(t, ctx, store, "go")
		mustExecDecodeTest(t, store, `UPDATE test_status SET failures_json = ? WHERE workspace_id = ? AND watcher_id = ?`, "{", "ws-decode", "go")

		_, _, err := store.Get(ctx, "ws-decode", "go")
		requireDecodeError(t, err, "failures_json")
	})

	t.Run("started timestamp", func(t *testing.T) {
		store := openDecodeTestStore(t)
		defer store.Close()

		upsertDecodeStatus(t, ctx, store, "go")
		mustExecDecodeTest(t, store, `UPDATE test_status SET started_at = ? WHERE workspace_id = ? AND watcher_id = ?`, "not-a-time", "ws-decode", "go")

		_, _, err := store.Get(ctx, "ws-decode", "go")
		requireDecodeError(t, err, "started_at")
	})
}

func TestDecodePreservesEmptyOptionalTestStatusFields(t *testing.T) {
	ctx := context.Background()
	store := openDecodeTestStore(t)
	defer store.Close()

	upsertDecodeStatus(t, ctx, store, "go")
	mustExecDecodeTest(t, store, `UPDATE test_status SET started_at = '', finished_at = '', failures_json = '' WHERE workspace_id = ? AND watcher_id = ?`, "ws-decode", "go")

	got, found, err := store.Get(ctx, "ws-decode", "go")
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	if !found {
		t.Fatal("expected status")
	}
	if got.StartedAt != nil {
		t.Fatalf("StartedAt = %v, want nil", got.StartedAt)
	}
	if got.FinishedAt != nil {
		t.Fatalf("FinishedAt = %v, want nil", got.FinishedAt)
	}
	if len(got.Failures) != 0 {
		t.Fatalf("Failures len = %d, want 0", len(got.Failures))
	}
}

func openDecodeTestStore(t *testing.T) Store {
	t.Helper()
	store, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return store
}

func upsertDecodeStatus(t *testing.T, ctx context.Context, store Store, watcherID string) {
	t.Helper()
	if err := store.Upsert(ctx, TestStatus{
		WorkspaceID: "ws-decode",
		WatcherID:   watcherID,
		Status:      StatusPass,
		Command:     "go test ./...",
	}); err != nil {
		t.Fatalf("upsert status: %v", err)
	}
}

func mustExecDecodeTest(t *testing.T, store Store, query string, args ...any) {
	t.Helper()
	sqlStore := store.(*sqlStore)
	if _, err := sqlStore.db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec corrupt fixture: %v", err)
	}
}

func requireDecodeError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected decode error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want substring %q", err.Error(), want)
	}
}
