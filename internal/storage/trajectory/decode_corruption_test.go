package trajectory

import (
	"context"
	"strings"
	"testing"
)

const decodeTestWorkspaceID = "0123456789abcdef0123456789abcdef"

func TestDecodeRejectsCorruptTrajectoryFields(t *testing.T) {
	ctx := context.Background()

	t.Run("trajectory json", func(t *testing.T) {
		store := openDecodeTestStore(t)
		defer store.Close()

		traj, err := store.InsertTrajectory(ctx, Trajectory{WorkspaceID: decodeTestWorkspaceID, Status: StatusOK})
		if err != nil {
			t.Fatalf("insert trajectory: %v", err)
		}
		mustExecDecodeTest(t, store, `UPDATE trajectories SET task_ids_json = ? WHERE workspace_id = ? AND id = ?`, "{", decodeTestWorkspaceID, traj.ID)

		_, err = store.GetTrajectory(ctx, decodeTestWorkspaceID, traj.ID)
		requireDecodeError(t, err, "task_ids_json")
	})

	t.Run("trajectory timestamp", func(t *testing.T) {
		store := openDecodeTestStore(t)
		defer store.Close()

		traj, err := store.InsertTrajectory(ctx, Trajectory{WorkspaceID: decodeTestWorkspaceID, Status: StatusOK})
		if err != nil {
			t.Fatalf("insert trajectory: %v", err)
		}
		mustExecDecodeTest(t, store, `UPDATE trajectories SET created_at = ? WHERE workspace_id = ? AND id = ?`, "not-a-time", decodeTestWorkspaceID, traj.ID)

		_, err = store.GetTrajectory(ctx, decodeTestWorkspaceID, traj.ID)
		requireDecodeError(t, err, "created_at")
	})

	t.Run("user request json", func(t *testing.T) {
		store := openDecodeTestStore(t)
		defer store.Close()

		req, err := store.InsertUserRequest(ctx, UserRequestCapture{
			WorkspaceID: decodeTestWorkspaceID,
			Actor:       "actor:human:test",
			Source:      SourceCLI,
			Text:        "test",
		})
		if err != nil {
			t.Fatalf("insert request: %v", err)
		}
		mustExecDecodeTest(t, store, `UPDATE user_requests SET command_context_json = ? WHERE workspace_id = ? AND id = ?`, "{", decodeTestWorkspaceID, req.ID)

		_, err = store.GetUserRequest(ctx, decodeTestWorkspaceID, req.ID)
		requireDecodeError(t, err, "command_context_json")
	})

	t.Run("event json", func(t *testing.T) {
		store := openDecodeTestStore(t)
		defer store.Close()

		traj, err := store.InsertTrajectory(ctx, Trajectory{WorkspaceID: decodeTestWorkspaceID, Status: StatusOK})
		if err != nil {
			t.Fatalf("insert trajectory: %v", err)
		}
		event, err := store.InsertEvent(ctx, Event{TrajectoryID: traj.ID, Kind: EventKindToolCall})
		if err != nil {
			t.Fatalf("insert event: %v", err)
		}
		mustExecDecodeTest(t, store, `UPDATE trajectory_events SET data_inline_json = ? WHERE id = ?`, "{", event.ID)

		_, err = store.ListEvents(ctx, EventFilter{TrajectoryID: traj.ID})
		requireDecodeError(t, err, "data_inline_json")
	})
}

func TestDecodePreservesEmptyOptionalTrajectoryFields(t *testing.T) {
	ctx := context.Background()
	store := openDecodeTestStore(t)
	defer store.Close()

	traj, err := store.InsertTrajectory(ctx, Trajectory{WorkspaceID: decodeTestWorkspaceID, Status: StatusOK})
	if err != nil {
		t.Fatalf("insert trajectory: %v", err)
	}
	mustExecDecodeTest(t, store, `UPDATE trajectories SET task_ids_json = '' WHERE workspace_id = ? AND id = ?`, decodeTestWorkspaceID, traj.ID)

	got, err := store.GetTrajectory(ctx, decodeTestWorkspaceID, traj.ID)
	if err != nil {
		t.Fatalf("get trajectory: %v", err)
	}
	if len(got.TaskIDs) != 0 {
		t.Fatalf("TaskIDs len = %d, want 0", len(got.TaskIDs))
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
