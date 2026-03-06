package projections

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/dbutil"
	libsqlevents "github.com/jkatigb/agentctl/internal/v2/adapters/libsql/events"
	"github.com/jkatigb/agentctl/internal/v2/adapters/libsql/idmap"
	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	v2events "github.com/jkatigb/agentctl/internal/v2/core/events"
)

func TestEventReplay_RebuildsProjection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "replay.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })

	if err := libsqlevents.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate events: %v", err)
	}
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate projections: %v", err)
	}

	eventStore := libsqlevents.NewStore(db, db.Close)
	eventStoreAppendNow := time.Date(2026, time.February, 18, 14, 0, 0, 0, time.UTC)
	eventStoreNow := eventStoreAppendNow
	eventStoreNowFn := func() time.Time {
		current := eventStoreNow
		eventStoreNow = eventStoreNow.Add(time.Second)
		return current
	}
	eventStore.SetNowForTest(eventStoreNowFn)

	append := func(id string, version, seq int64, eventType v2events.EventType) {
		t.Helper()
		payload := map[string]any{"agent_id": "agent-001"}
		err := eventStore.Append(ctx, v2events.Event{
			ID:            id,
			StreamID:      "run-v2-001",
			StreamType:    v2events.StreamTypeRun,
			StreamVersion: version,
			Sequence:      seq,
			EventType:     eventType,
			OccurredAt:    eventStoreNowFn(),
			Command:       "spawn",
			RequestID:     "req-001",
			ActorID:       "actor-overseer",
			Payload:       v2events.MustMarshalPayload(payload),
		})
		if err != nil {
			t.Fatalf("append %s: %v", id, err)
		}
	}

	append("evt-001", 1, 1, v2events.EventRunStarted)
	append("evt-002", 2, 2, v2events.EventTurnRecorded)
	append("evt-003", 3, 3, v2events.EventRunCompleted)

	projStore := NewStore(db)
	projNow := time.Date(2026, time.February, 18, 14, 5, 0, 0, time.UTC)
	projStore.now = func() time.Time {
		current := projNow
		projNow = projNow.Add(time.Second)
		return current
	}

	if err := projStore.ReplayFrom(ctx, eventStore, v2events.ReplayFilter{
		StreamID:   "run-v2-001",
		StreamType: v2events.StreamTypeRun,
	}); err != nil {
		t.Fatalf("replay projections: %v", err)
	}

	runState, err := projStore.GetRunState(ctx, "run-v2-001")
	if err != nil {
		t.Fatalf("get run state: %v", err)
	}
	if runState.Status != "completed" {
		t.Fatalf("run status=%q want completed", runState.Status)
	}
	if runState.LastStreamVersion != 3 {
		t.Fatalf("run last_stream_version=%d want 3", runState.LastStreamVersion)
	}
}

func TestProjectionStore_LegacyEntityLookup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "lookup.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })

	if err := libsqlevents.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate events: %v", err)
	}
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate projections: %v", err)
	}
	if err := idmap.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate idmap: %v", err)
	}

	eventStore := libsqlevents.NewStore(db, db.Close)
	eventStoreNow := time.Date(2026, time.February, 18, 14, 30, 0, 0, time.UTC)
	eventStoreNowFn := func() time.Time {
		current := eventStoreNow
		eventStoreNow = eventStoreNow.Add(time.Second)
		return current
	}
	eventStore.SetNowForTest(eventStoreNowFn)

	if err := eventStore.Append(ctx, v2events.Event{
		ID:            "evt-001",
		StreamID:      "run-v2-lookup",
		StreamType:    v2events.StreamTypeRun,
		StreamVersion: 1,
		Sequence:      1,
		EventType:     v2events.EventRunCompleted,
		OccurredAt:    eventStoreNowFn(),
		Payload:       v2events.MustMarshalPayload(map[string]any{"agent_id": "agent-lookup"}),
	}); err != nil {
		t.Fatalf("append event: %v", err)
	}

	projStore := NewStore(db)
	projStore.now = func() time.Time { return time.Date(2026, time.February, 18, 14, 31, 0, 0, time.UTC) }
	if err := projStore.ReplayFrom(ctx, eventStore, v2events.ReplayFilter{}); err != nil {
		t.Fatalf("replay projections: %v", err)
	}

	idStore := idmap.NewStore(db)
	if err := idStore.Put(ctx, "run", "legacy-run-001", "run-v2-lookup"); err != nil {
		t.Fatalf("idmap put: %v", err)
	}

	state, err := projStore.GetRunStateByRef(ctx, "legacy-run-001", idStore)
	if err != nil {
		t.Fatalf("get run state by legacy ref: %v", err)
	}
	if state.RunID != "run-v2-lookup" {
		t.Fatalf("run_id=%q want run-v2-lookup", state.RunID)
	}
}

func TestReplayFrom_DelegatesToAdditionalProjector(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "replay_delegate.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })

	if err := libsqlevents.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate events: %v", err)
	}
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate projections: %v", err)
	}

	eventStore := libsqlevents.NewStore(db, db.Close)
	eventStore.SetNowForTest(func() time.Time { return time.Date(2026, time.February, 18, 15, 0, 0, 0, time.UTC) })
	if err := eventStore.Append(ctx, v2events.Event{
		ID:            "evt-001",
		StreamID:      "run-v2-002",
		StreamType:    v2events.StreamTypeRun,
		StreamVersion: 1,
		Sequence:      1,
		EventType:     v2events.EventRunStarted,
		OccurredAt:    time.Date(2026, time.February, 18, 15, 0, 0, 0, time.UTC),
		Command:       "spawn",
		RequestID:     "req-002",
		ActorID:       "actor-overseer",
		Payload:       v2events.MustMarshalPayload(map[string]any{"agent_id": "agent-002"}),
	}); err != nil {
		t.Fatalf("append event: %v", err)
	}

	projStore := NewStore(db)
	projector := &fakeProjector{}
	if err := projStore.ReplayFrom(ctx, eventStore, v2events.ReplayFilter{}, projector); err != nil {
		t.Fatalf("replay with projector: %v", err)
	}
	if projector.calls != 1 {
		t.Fatalf("projector calls=%d want 1", projector.calls)
	}
}

func TestReplayFrom_ProjectorErrorFailsReplay(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "replay_delegate_err.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })

	if err := libsqlevents.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate events: %v", err)
	}
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate projections: %v", err)
	}

	eventStore := libsqlevents.NewStore(db, db.Close)
	eventStore.SetNowForTest(func() time.Time { return time.Date(2026, time.February, 18, 15, 10, 0, 0, time.UTC) })
	if err := eventStore.Append(ctx, v2events.Event{
		ID:            "evt-err-001",
		StreamID:      "run-v2-003",
		StreamType:    v2events.StreamTypeRun,
		StreamVersion: 1,
		Sequence:      1,
		EventType:     v2events.EventRunStarted,
		OccurredAt:    time.Date(2026, time.February, 18, 15, 10, 0, 0, time.UTC),
		Payload:       v2events.MustMarshalPayload(map[string]any{"agent_id": "agent-003"}),
	}); err != nil {
		t.Fatalf("append event: %v", err)
	}

	projStore := NewStore(db)
	wantErr := errors.New("projector boom")
	err = projStore.ReplayFrom(ctx, eventStore, v2events.ReplayFilter{}, &fakeProjector{err: wantErr})
	var verr *v2errors.V2Error
	if !errors.As(err, &verr) {
		t.Fatalf("replay error type=%T want *V2Error (err=%v)", err, err)
	}
	if !errors.Is(verr.Cause, wantErr) {
		t.Fatalf("replay cause=%v want %v", verr.Cause, wantErr)
	}
}

type fakeProjector struct {
	calls int
	err   error
}

func (f *fakeProjector) Apply(_ context.Context, _ v2events.Event) error {
	f.calls++
	return f.err
}
