package trajectory

import (
	"context"
	"strings"
	"testing"
	"testing/quick"
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

	t.Run("trajectory status", func(t *testing.T) {
		store := openDecodeTestStore(t)
		defer store.Close()

		traj, err := store.InsertTrajectory(ctx, Trajectory{WorkspaceID: decodeTestWorkspaceID, Status: StatusOK})
		if err != nil {
			t.Fatalf("insert trajectory: %v", err)
		}
		mustExecDecodeTest(t, store, `UPDATE trajectories SET status = ? WHERE workspace_id = ? AND id = ?`, "not-a-status", decodeTestWorkspaceID, traj.ID)

		_, err = store.GetTrajectory(ctx, decodeTestWorkspaceID, traj.ID)
		requireDecodeError(t, err, "status")
	})

	t.Run("trajectory outcome", func(t *testing.T) {
		store := openDecodeTestStore(t)
		defer store.Close()

		traj, err := store.InsertTrajectory(ctx, Trajectory{WorkspaceID: decodeTestWorkspaceID, Status: StatusOK})
		if err != nil {
			t.Fatalf("insert trajectory: %v", err)
		}
		if err := store.SetOutcome(ctx, decodeTestWorkspaceID, traj.ID, Outcome{Success: true}); err != nil {
			t.Fatalf("set outcome: %v", err)
		}
		mustExecDecodeTest(t, store, `UPDATE trajectories SET outcome_json = ? WHERE workspace_id = ? AND id = ?`, `{"success":true,"human_rating":6}`, decodeTestWorkspaceID, traj.ID)

		_, err = store.GetTrajectory(ctx, decodeTestWorkspaceID, traj.ID)
		requireDecodeError(t, err, "outcome_json")
		_, err = store.ListTrajectories(ctx, ListFilter{WorkspaceID: decodeTestWorkspaceID})
		requireDecodeError(t, err, "outcome_json")
		_, err = store.ListByOutcome(ctx, OutcomeFilter{WorkspaceID: decodeTestWorkspaceID})
		requireDecodeError(t, err, "outcome_json")
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

	t.Run("user request source", func(t *testing.T) {
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
		mustExecDecodeTest(t, store, `UPDATE user_requests SET source = ? WHERE workspace_id = ? AND id = ?`, "unknown-source", decodeTestWorkspaceID, req.ID)

		_, err = store.GetUserRequest(ctx, decodeTestWorkspaceID, req.ID)
		requireDecodeError(t, err, "source")
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

	t.Run("event kind", func(t *testing.T) {
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
		mustExecDecodeTest(t, store, `UPDATE trajectory_events SET kind = ? WHERE id = ?`, "unknown-kind", event.ID)

		_, err = store.ListEvents(ctx, EventFilter{TrajectoryID: traj.ID})
		requireDecodeError(t, err, "kind")
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

func TestValidateOutcomeProperty(t *testing.T) {
	rejectsGeneratedOutOfRangeRatings := func(rating int) bool {
		if rating >= 1 && rating <= 5 {
			return true
		}
		err := validateOutcome(&Outcome{HumanRating: &rating})
		return err != nil && strings.Contains(err.Error(), "human_rating")
	}
	if err := quick.Check(rejectsGeneratedOutOfRangeRatings, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("out-of-range rating property failed: %v", err)
	}

	rejectsGeneratedNegativeDurations := func(raw uint16) bool {
		duration := -int64(raw) - 1
		err := validateOutcome(&Outcome{DurationMS: duration})
		return err != nil && strings.Contains(err.Error(), "duration_ms")
	}
	if err := quick.Check(rejectsGeneratedNegativeDurations, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("negative duration property failed: %v", err)
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
