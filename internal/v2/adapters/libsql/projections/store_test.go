package projections

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
	v2events "github.com/joshka0/foxctl/internal/v2/core/events"
)

func TestStoreGetRunStateByRequestID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "request-id.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate projections: %v", err)
	}

	store := NewStore(db)
	store.now = func() time.Time { return time.Date(2026, time.April, 21, 8, 0, 0, 0, time.UTC) }
	if err := store.Apply(ctx, v2events.Event{
		ID:            "evt-001",
		StreamID:      "run-001",
		StreamType:    v2events.StreamTypeRun,
		StreamVersion: 1,
		EventType:     v2events.EventRunStarted,
		Command:       "spawn",
		RequestID:     "req-001",
		ActorID:       "actor:worker:1",
	}); err != nil {
		t.Fatalf("apply event: %v", err)
	}

	state, err := store.GetRunStateByRequestID(ctx, "req-001")
	if err != nil {
		t.Fatalf("get run by request_id: %v", err)
	}
	if state.RunID != "run-001" {
		t.Fatalf("run_id=%q want run-001", state.RunID)
	}
	if state.Status != string(runStatusRunning) {
		t.Fatalf("status=%q want %q", state.Status, runStatusRunning)
	}

	if _, err := store.GetRunStateByRequestID(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing request_id err=%v want ErrNotFound", err)
	}
}

func TestStoreListRunStatesFiltersAndClamp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "list-runs.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate projections: %v", err)
	}

	base := time.Date(2026, time.April, 21, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 230; i++ {
		status := "running"
		command := "spawn"
		actorID := "actor:a"
		if i%2 == 0 {
			status = "completed"
		}
		if i%3 == 0 {
			command = "ask"
		}
		if i%5 == 0 {
			actorID = "actor:b"
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO v2_run_state (
				run_id, status, last_event_id, last_stream_version, command, request_id, actor_id, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		`,
			fmt.Sprintf("run-%03d", i),
			status,
			fmt.Sprintf("evt-%03d", i),
			int64(i+1),
			command,
			fmt.Sprintf("req-%03d", i),
			actorID,
			sqlutil.FormatTimestamp(base.Add(time.Duration(i)*time.Second)),
		); err != nil {
			t.Fatalf("insert run state %d: %v", i, err)
		}
	}

	store := NewStore(db)

	defaultLimited, err := store.ListRunStates(ctx, RunStateFilter{})
	if err != nil {
		t.Fatalf("list default limit: %v", err)
	}
	if len(defaultLimited) != defaultRunStateListLimit {
		t.Fatalf("default limit count=%d want %d", len(defaultLimited), defaultRunStateListLimit)
	}

	maxLimited, err := store.ListRunStates(ctx, RunStateFilter{Limit: 999})
	if err != nil {
		t.Fatalf("list max clamp: %v", err)
	}
	if len(maxLimited) != maxRunStateListLimit {
		t.Fatalf("max clamp count=%d want %d", len(maxLimited), maxRunStateListLimit)
	}

	filtered, err := store.ListRunStates(ctx, RunStateFilter{
		Limit:   50,
		Status:  "completed",
		Command: "ask",
		ActorID: "actor:b",
	})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	for _, item := range filtered {
		if item.Status != "completed" || item.Command != "ask" || item.ActorID != "actor:b" {
			t.Fatalf("unexpected filtered item: %+v", item)
		}
	}
	if len(filtered) == 0 {
		t.Fatal("expected at least one filtered run")
	}
}

func TestServiceAdapterGetRunStateByRequestIDNotFoundMapsToEventsNotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "adapter-not-found.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate projections: %v", err)
	}

	adapter := NewServiceAdapter(NewStore(db))
	if _, err := adapter.GetRunStateByRequestID(ctx, "missing"); !errors.Is(err, v2events.ErrNotFound) {
		t.Fatalf("err=%v want events.ErrNotFound", err)
	}
}
