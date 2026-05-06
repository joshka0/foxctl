package orchestration

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
	tursoevents "github.com/joshka0/foxctl/internal/v2/adapters/turso/events"
	coreevents "github.com/joshka0/foxctl/internal/v2/core/events"
	coreorchestration "github.com/joshka0/foxctl/internal/v2/core/orchestration"
)

func TestReplayFrom_RebuildsOrchestrationCards(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "orchestration_replay.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })

	if err := tursoevents.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate events: %v", err)
	}
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate orchestration: %v", err)
	}

	eventStore := tursoevents.NewStore(db, db.Close)
	eventStore.SetNowForTest(func() time.Time { return time.Date(2026, time.March, 5, 15, 0, 0, 0, time.UTC) })

	appendEvent := func(id, requestID, issueID, state string) {
		t.Helper()
		err := eventStore.Append(ctx, coreevents.Event{
			ID:            id,
			StreamID:      "run-" + issueID,
			StreamType:    coreevents.StreamTypeRun,
			StreamVersion: 1,
			Sequence:      1,
			EventType:     coreevents.EventRunStarted,
			OccurredAt:    time.Date(2026, time.March, 5, 15, 0, 0, 0, time.UTC),
			Command:       "orchestration/dispatch-issue",
			RequestID:     requestID,
			Payload: coreevents.MustMarshalPayload(map[string]any{
				"workspace_id": "ws-replay",
				"issue_id":     issueID,
				"state":        state,
				"eligibility":  "eligible",
			}),
		})
		if err != nil {
			t.Fatalf("append %s: %v", id, err)
		}
	}

	appendEvent("evt-001", "req-001", "issue-1", "Running")
	appendEvent("evt-002", "req-002", "issue-2", "Released")

	store := NewStore(db, StoreOptions{})
	store.SetNowForTest(func() time.Time { return time.Date(2026, time.March, 5, 15, 1, 0, 0, time.UTC) })

	if err := store.ReplayFrom(ctx, eventStore, coreevents.ReplayFilter{}); err != nil {
		t.Fatalf("replay orchestration: %v", err)
	}

	card, err := store.Card(ctx, coreorchestration.CardRequest{WorkspaceID: "ws-replay", IssueID: "issue-1"})
	if err != nil {
		t.Fatalf("card issue-1: %v", err)
	}
	if card.Card.State != coreorchestration.StateRunning {
		t.Fatalf("issue-1 state=%q want %q", card.Card.State, coreorchestration.StateRunning)
	}

	board, err := store.Board(ctx, coreorchestration.BoardRequest{WorkspaceID: "ws-replay", Limit: 10})
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	if got := board.Counts[coreorchestration.LaneRunning]; got != 1 {
		t.Fatalf("running count=%d want 1", got)
	}
}
