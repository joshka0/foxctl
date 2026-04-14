package events

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
	v2events "github.com/joshka0/foxctl/internal/v2/core/events"
)

func TestEventAppend_EnforcesMonotonicVersion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "events.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, db.Close)
	store.now = func() time.Time { return time.Date(2026, time.February, 18, 13, 0, 0, 0, time.UTC) }

	err = store.Append(ctx, v2events.Event{
		ID:            "evt-001",
		StreamID:      "run-001",
		StreamType:    v2events.StreamTypeRun,
		StreamVersion: 1,
		Sequence:      1,
		EventType:     v2events.EventRunStarted,
		OccurredAt:    store.now(),
		Payload:       v2events.MustMarshalPayload(v2events.RunStartedPayload{Mode: "autonomous"}),
	})
	if err != nil {
		t.Fatalf("append first event: %v", err)
	}

	err = store.Append(ctx, v2events.Event{
		ID:            "evt-002",
		StreamID:      "run-001",
		StreamType:    v2events.StreamTypeRun,
		StreamVersion: 3,
		Sequence:      2,
		EventType:     v2events.EventTurnRecorded,
		OccurredAt:    store.now().Add(time.Second),
		Payload:       v2events.MustMarshalPayload(v2events.TurnRecordedPayload{TurnID: "turn-001"}),
	})
	if !errors.Is(err, v2events.ErrVersionConflict) {
		t.Fatalf("append non-monotonic version error=%v, want ErrVersionConflict", err)
	}

	err = store.Append(ctx, v2events.Event{
		ID:         "evt-003",
		StreamID:   "run-001",
		StreamType: v2events.StreamTypeRun,
		EventType:  v2events.EventTurnRecorded,
		OccurredAt: store.now().Add(2 * time.Second),
		Payload:    v2events.MustMarshalPayload(v2events.TurnRecordedPayload{TurnID: "turn-001"}),
	})
	if err != nil {
		t.Fatalf("append auto-version event: %v", err)
	}

	events, err := store.ListStream(ctx, v2events.StreamFilter{
		StreamID:   "run-001",
		StreamType: v2events.StreamTypeRun,
	})
	if err != nil {
		t.Fatalf("list stream: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events len=%d want 2", len(events))
	}
	if events[0].StreamVersion != 1 || events[1].StreamVersion != 2 {
		t.Fatalf("stream versions=%d,%d want 1,2", events[0].StreamVersion, events[1].StreamVersion)
	}
}

func TestReplay_Failure_RetriedAsInternalRetryable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "events.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, db.Close)
	store.now = func() time.Time { return time.Date(2026, time.February, 18, 13, 0, 0, 0, time.UTC) }

	err = store.Append(ctx, v2events.Event{
		ID:            "evt-001",
		StreamID:      "run-001",
		StreamType:    v2events.StreamTypeRun,
		StreamVersion: 1,
		Sequence:      1,
		EventType:     v2events.EventRunStarted,
		OccurredAt:    store.now(),
		Payload:       v2events.MustMarshalPayload(v2events.RunStartedPayload{Mode: "autonomous"}),
	})
	if err != nil {
		t.Fatalf("append event: %v", err)
	}

	err = store.Replay(ctx, v2events.ReplayFilter{
		StreamID:   "run-001",
		StreamType: v2events.StreamTypeRun,
	}, func(_ context.Context, _ v2events.Event) error {
		return errors.New("boom")
	})
	var verr *v2errors.V2Error
	if !errors.As(err, &verr) {
		t.Fatalf("replay error type=%T want *V2Error (err=%v)", err, err)
	}
	if verr.Kind != v2errors.ErrInternal {
		t.Fatalf("v2error.kind=%q want %q", verr.Kind, v2errors.ErrInternal)
	}
	if !verr.Retryable {
		t.Fatal("v2error.retryable=false want true")
	}
}

func TestDeleteOrchestrationIssueHistory_RemovesMatchingWorkspaceIssues(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "events_cleanup.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, db.Close)
	now := time.Date(2026, time.April, 6, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	appendEvent := func(id, issueID, workspaceID, command string) {
		t.Helper()
		if err := store.Append(ctx, v2events.Event{
			ID:            id,
			StreamID:      "run:" + issueID,
			StreamType:    v2events.StreamTypeRun,
			StreamVersion: 1,
			Sequence:      1,
			EventType:     v2events.EventRunStarted,
			OccurredAt:    now,
			Command:       command,
			Payload: v2events.MustMarshalPayload(map[string]any{
				"issue_id":     issueID,
				"workspace_id": workspaceID,
			}),
		}); err != nil {
			t.Fatalf("append event %s: %v", id, err)
		}
	}

	appendEvent("evt-clean-1", "issue-clean-1", "ws-1", "orchestration/dispatch-issue")
	appendEvent("evt-clean-2", "issue-clean-2", "ws-1", "orchestration/card-action")
	appendEvent("evt-keep-1", "issue-keep-1", "ws-2", "orchestration/dispatch-issue")

	deleted, eventIDs, err := store.DeleteOrchestrationIssueHistory(ctx, "ws-1", []string{"issue-clean-1", "issue-clean-2"})
	if err != nil {
		t.Fatalf("DeleteOrchestrationIssueHistory() error = %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted=%d want 2", deleted)
	}
	if len(eventIDs) != 2 {
		t.Fatalf("len(eventIDs)=%d want 2", len(eventIDs))
	}

	remaining, err := store.ListStream(ctx, v2events.StreamFilter{
		StreamID:   "run:issue-keep-1",
		StreamType: v2events.StreamTypeRun,
	})
	if err != nil {
		t.Fatalf("ListStream(keep) error = %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("remaining keep events=%d want 1", len(remaining))
	}
}
