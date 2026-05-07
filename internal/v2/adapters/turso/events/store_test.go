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

func TestAppendIfAbsent_ReplayReturnsStoredEventWithStaleCursor(t *testing.T) {
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
	now := time.Date(2026, time.February, 18, 13, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	event := v2events.Event{
		ID:            "evt-idempotent-001",
		StreamID:      "run-idempotent",
		StreamType:    v2events.StreamTypeRun,
		StreamVersion: 1,
		Sequence:      1,
		EventType:     v2events.EventRunStarted,
		OccurredAt:    now,
		CorrelationID: "corr-001",
		CausationID:   "cause-001",
		ActorID:       "actor-001",
		RequestID:     "req-001",
		Command:       "runtime/start",
		Payload:       v2events.MustMarshalPayload(v2events.RunStartedPayload{Mode: "autonomous"}),
	}
	result, err := store.AppendIfAbsent(ctx, event)
	if err != nil {
		t.Fatalf("AppendIfAbsent(first) error = %v", err)
	}
	if !result.Appended {
		t.Fatal("AppendIfAbsent(first).Appended=false want true")
	}

	replay := event
	replay.StreamVersion = 0
	replay.Sequence = 0
	replay.OccurredAt = now.Add(5 * time.Minute)
	result, err = store.AppendIfAbsent(ctx, replay)
	if err != nil {
		t.Fatalf("AppendIfAbsent(replay) error = %v", err)
	}
	if result.Appended {
		t.Fatal("AppendIfAbsent(replay).Appended=true want false")
	}
	if result.Event.StreamVersion != 1 || result.Event.Sequence != 1 {
		t.Fatalf("stored cursor=%d/%d want 1/1", result.Event.StreamVersion, result.Event.Sequence)
	}
	if !result.Event.OccurredAt.Equal(now) {
		t.Fatalf("stored occurred_at=%s want %s", result.Event.OccurredAt, now)
	}

	events, err := store.ListStream(ctx, v2events.StreamFilter{
		StreamID:   "run-idempotent",
		StreamType: v2events.StreamTypeRun,
	})
	if err != nil {
		t.Fatalf("ListStream() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len=%d want 1", len(events))
	}
}

func TestAppendIfAbsent_ConflictsOnSameIDDifferentMaterialFields(t *testing.T) {
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
	now := time.Date(2026, time.February, 18, 13, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	event := v2events.Event{
		ID:            "evt-idempotent-conflict",
		StreamID:      "run-idempotent-conflict",
		StreamType:    v2events.StreamTypeRun,
		StreamVersion: 1,
		Sequence:      1,
		EventType:     v2events.EventRunStarted,
		OccurredAt:    now,
		Payload:       v2events.MustMarshalPayload(v2events.RunStartedPayload{Mode: "autonomous"}),
	}
	if _, err := store.AppendIfAbsent(ctx, event); err != nil {
		t.Fatalf("AppendIfAbsent(first) error = %v", err)
	}

	changedPayload := event
	changedPayload.StreamVersion = 0
	changedPayload.Sequence = 0
	changedPayload.Payload = v2events.MustMarshalPayload(v2events.RunStartedPayload{Mode: "reactive"})
	if _, err := store.AppendIfAbsent(ctx, changedPayload); !errors.Is(err, v2events.ErrIdempotencyConflict) {
		t.Fatalf("AppendIfAbsent(changed payload) error=%v want ErrIdempotencyConflict", err)
	}

	changedType := event
	changedType.StreamVersion = 0
	changedType.Sequence = 0
	changedType.EventType = v2events.EventTurnRecorded
	if _, err := store.AppendIfAbsent(ctx, changedType); !errors.Is(err, v2events.ErrIdempotencyConflict) {
		t.Fatalf("AppendIfAbsent(changed type) error=%v want ErrIdempotencyConflict", err)
	}
}

func TestAppendIfAbsent_NewIDWithStaleCursorReturnsVersionConflict(t *testing.T) {
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
	now := time.Date(2026, time.February, 18, 13, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	if _, err := store.AppendIfAbsent(ctx, v2events.Event{
		ID:            "evt-stale-cursor-001",
		StreamID:      "run-stale-cursor",
		StreamType:    v2events.StreamTypeRun,
		StreamVersion: 1,
		Sequence:      1,
		EventType:     v2events.EventRunStarted,
		OccurredAt:    now,
		Payload:       v2events.MustMarshalPayload(v2events.RunStartedPayload{Mode: "autonomous"}),
	}); err != nil {
		t.Fatalf("AppendIfAbsent(first) error = %v", err)
	}

	_, err = store.AppendIfAbsent(ctx, v2events.Event{
		ID:            "evt-stale-cursor-002",
		StreamID:      "run-stale-cursor",
		StreamType:    v2events.StreamTypeRun,
		StreamVersion: 1,
		Sequence:      1,
		EventType:     v2events.EventTurnRecorded,
		OccurredAt:    now.Add(time.Second),
		Payload:       v2events.MustMarshalPayload(v2events.TurnRecordedPayload{TurnID: "turn-001"}),
	})
	if !errors.Is(err, v2events.ErrVersionConflict) {
		t.Fatalf("AppendIfAbsent(stale new id) error=%v want ErrVersionConflict", err)
	}
}

func TestReadStreamCursor_EmptyStreamReturnsZeroCursor(t *testing.T) {
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
	cursor, err := store.ReadStreamCursor(ctx, v2events.StreamCursorRequest{
		StreamID:   "run-empty",
		StreamType: v2events.StreamTypeRun,
	})
	if err != nil {
		t.Fatalf("ReadStreamCursor() error = %v", err)
	}
	if cursor.StreamVersion != 0 || cursor.Sequence != 0 {
		t.Fatalf("cursor=%+v want zero cursor", cursor)
	}
}

func TestReadStreamCursor_PopulatedStreamReturnsMaxCursor(t *testing.T) {
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
	now := time.Date(2026, time.February, 18, 13, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	for i, event := range []v2events.Event{
		{
			ID:         "evt-cursor-001",
			StreamID:   "run-cursor",
			StreamType: v2events.StreamTypeRun,
			EventType:  v2events.EventRunStarted,
			OccurredAt: now,
			Payload:    v2events.MustMarshalPayload(v2events.RunStartedPayload{Mode: "autonomous"}),
		},
		{
			ID:         "evt-cursor-002",
			StreamID:   "run-cursor",
			StreamType: v2events.StreamTypeRun,
			EventType:  v2events.EventTurnRecorded,
			OccurredAt: now.Add(time.Second),
			Payload:    v2events.MustMarshalPayload(v2events.TurnRecordedPayload{TurnID: "turn-001"}),
		},
		{
			ID:         "evt-cursor-other",
			StreamID:   "run-other",
			StreamType: v2events.StreamTypeRun,
			EventType:  v2events.EventRunStarted,
			OccurredAt: now.Add(2 * time.Second),
			Payload:    v2events.MustMarshalPayload(v2events.RunStartedPayload{Mode: "reactive"}),
		},
	} {
		if err := store.Append(ctx, event); err != nil {
			t.Fatalf("append event %d: %v", i, err)
		}
	}

	cursor, err := store.ReadStreamCursor(ctx, v2events.StreamCursorRequest{
		StreamID:   "run-cursor",
		StreamType: v2events.StreamTypeRun,
	})
	if err != nil {
		t.Fatalf("ReadStreamCursor() error = %v", err)
	}
	if cursor.StreamVersion != 2 || cursor.Sequence != 2 {
		t.Fatalf("cursor=%+v want stream_version=2 sequence=2", cursor)
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
