package events

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/dbutil"
	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	v2events "github.com/jkatigb/agentctl/internal/v2/core/events"
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
