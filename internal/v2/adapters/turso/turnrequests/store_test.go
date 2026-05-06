package turnrequests

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

func TestBeginTurnRequest_InsertCreatesRunningRow(t *testing.T) {
	t.Parallel()

	ctx, store := newTestStore(t)
	record, inserted, err := store.BeginTurnRequest(ctx, beginRecord())
	if err != nil {
		t.Fatalf("BeginTurnRequest() error = %v", err)
	}
	if !inserted {
		t.Fatalf("inserted=false want true")
	}
	if record.Status != run.TurnRequestStatusRunning {
		t.Fatalf("status=%q want running", record.Status)
	}
	if record.StartedAt.IsZero() || record.UpdatedAt.IsZero() {
		t.Fatalf("timestamps were not populated: %+v", record)
	}
	if !record.CompletedAt.IsZero() {
		t.Fatalf("completed_at=%s want zero", record.CompletedAt)
	}
}

func TestBeginTurnRequest_DuplicateReturnsExisting(t *testing.T) {
	t.Parallel()

	ctx, store := newTestStore(t)
	first, inserted, err := store.BeginTurnRequest(ctx, beginRecord())
	if err != nil {
		t.Fatalf("BeginTurnRequest() first error = %v", err)
	}
	if !inserted {
		t.Fatalf("first inserted=false want true")
	}

	second, inserted, err := store.BeginTurnRequest(ctx, beginRecord())
	if err != nil {
		t.Fatalf("BeginTurnRequest() duplicate error = %v", err)
	}
	if inserted {
		t.Fatalf("duplicate inserted=true want false")
	}
	if second.TurnID != first.TurnID || !second.StartedAt.Equal(first.StartedAt) {
		t.Fatalf("duplicate record=%+v want existing=%+v", second, first)
	}
}

func TestBeginTurnRequest_DuplicateDifferentTurnIDConflicts(t *testing.T) {
	t.Parallel()

	ctx, store := newTestStore(t)
	if _, _, err := store.BeginTurnRequest(ctx, beginRecord()); err != nil {
		t.Fatalf("BeginTurnRequest() first error = %v", err)
	}

	conflict := beginRecord()
	conflict.TurnID = "turn-002"
	_, _, err := store.BeginTurnRequest(ctx, conflict)
	if !errors.Is(err, run.ErrTurnRequestConflict) {
		t.Fatalf("BeginTurnRequest() error=%v want ErrTurnRequestConflict", err)
	}
}

func TestRecoverStaleTurnRequest_StaleRunningRowIsReclaimed(t *testing.T) {
	t.Parallel()

	ctx, store := newTestStore(t)
	old := time.Date(2026, time.May, 6, 11, 50, 0, 0, time.UTC)
	now := time.Date(2026, time.May, 6, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return old }
	first, inserted, err := store.BeginTurnRequest(ctx, beginRecord())
	if err != nil {
		t.Fatalf("BeginTurnRequest() error = %v", err)
	}
	if !inserted {
		t.Fatal("inserted=false want true")
	}

	store.now = func() time.Time { return now }
	recovered, ok, err := store.RecoverStaleTurnRequest(ctx, beginRecord(), now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("RecoverStaleTurnRequest() error = %v", err)
	}
	if !ok {
		t.Fatal("recovered=false want true")
	}
	if recovered.Status != run.TurnRequestStatusRunning {
		t.Fatalf("status=%q want running", recovered.Status)
	}
	if !recovered.StartedAt.Equal(now) || !recovered.UpdatedAt.Equal(now) {
		t.Fatalf("timestamps=%s/%s want %s", recovered.StartedAt, recovered.UpdatedAt, now)
	}
	if recovered.StartedAt.Equal(first.StartedAt) {
		t.Fatalf("started_at was not refreshed: %s", recovered.StartedAt)
	}
}

func TestRecoverStaleTurnRequest_FreshRunningRowIsNotReclaimed(t *testing.T) {
	t.Parallel()

	ctx, store := newTestStore(t)
	first, inserted, err := store.BeginTurnRequest(ctx, beginRecord())
	if err != nil {
		t.Fatalf("BeginTurnRequest() error = %v", err)
	}
	if !inserted {
		t.Fatal("inserted=false want true")
	}

	now := time.Date(2026, time.May, 6, 12, 0, 0, 0, time.UTC)
	recovered, ok, err := store.RecoverStaleTurnRequest(ctx, beginRecord(), now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("RecoverStaleTurnRequest() error = %v", err)
	}
	if ok {
		t.Fatal("recovered=true want false")
	}
	if !recovered.StartedAt.Equal(first.StartedAt) || !recovered.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("record=%+v want unchanged=%+v", recovered, first)
	}
}

func TestRecoverStaleTurnRequest_TerminalRowIsNotReclaimed(t *testing.T) {
	t.Parallel()

	ctx, store := newTestStore(t)
	old := time.Date(2026, time.May, 6, 11, 50, 0, 0, time.UTC)
	now := time.Date(2026, time.May, 6, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return old }
	if _, _, err := store.BeginTurnRequest(ctx, beginRecord()); err != nil {
		t.Fatalf("BeginTurnRequest() error = %v", err)
	}
	completed, err := store.CompleteTurnRequest(ctx, run.TurnRequestRecord{
		RunID:      "run-001",
		RequestID:  "req-001",
		TurnID:     "turn-001",
		Status:     run.TurnRequestStatusSucceeded,
		OutputJSON: []byte(`{"summary":"done"}`),
	})
	if err != nil {
		t.Fatalf("CompleteTurnRequest() error = %v", err)
	}

	store.now = func() time.Time { return now }
	recovered, ok, err := store.RecoverStaleTurnRequest(ctx, beginRecord(), now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("RecoverStaleTurnRequest() error = %v", err)
	}
	if ok {
		t.Fatal("recovered=true want false")
	}
	if recovered.Status != run.TurnRequestStatusSucceeded {
		t.Fatalf("status=%q want succeeded", recovered.Status)
	}
	if string(recovered.OutputJSON) != string(completed.OutputJSON) {
		t.Fatalf("output_json=%s want %s", recovered.OutputJSON, completed.OutputJSON)
	}
}

func TestTouchTurnRequest_RunningRowRefreshesUpdatedAt(t *testing.T) {
	t.Parallel()

	ctx, store := newTestStore(t)
	startedAt := time.Date(2026, time.May, 6, 11, 50, 0, 0, time.UTC)
	touchedAt := time.Date(2026, time.May, 6, 12, 5, 0, 0, time.UTC)
	store.now = func() time.Time { return startedAt }
	first, inserted, err := store.BeginTurnRequest(ctx, beginRecord())
	if err != nil {
		t.Fatalf("BeginTurnRequest() error = %v", err)
	}
	if !inserted {
		t.Fatal("inserted=false want true")
	}

	touched, ok, err := store.TouchTurnRequest(ctx, "run-001", "req-001", "turn-001", touchedAt)
	if err != nil {
		t.Fatalf("TouchTurnRequest() error = %v", err)
	}
	if !ok {
		t.Fatal("touched=false want true")
	}
	if !touched.StartedAt.Equal(first.StartedAt) {
		t.Fatalf("started_at=%s want unchanged %s", touched.StartedAt, first.StartedAt)
	}
	if !touched.UpdatedAt.Equal(touchedAt) {
		t.Fatalf("updated_at=%s want %s", touched.UpdatedAt, touchedAt)
	}
	if touched.Status != run.TurnRequestStatusRunning {
		t.Fatalf("status=%q want running", touched.Status)
	}
}

func TestTouchTurnRequest_TerminalRowIsNotRewritten(t *testing.T) {
	t.Parallel()

	ctx, store := newTestStore(t)
	if _, _, err := store.BeginTurnRequest(ctx, beginRecord()); err != nil {
		t.Fatalf("BeginTurnRequest() error = %v", err)
	}
	completedAt := time.Date(2026, time.May, 6, 12, 1, 0, 0, time.UTC)
	completed, err := store.CompleteTurnRequest(ctx, run.TurnRequestRecord{
		RunID:       "run-001",
		RequestID:   "req-001",
		TurnID:      "turn-001",
		Status:      run.TurnRequestStatusSucceeded,
		OutputJSON:  []byte(`{"summary":"done"}`),
		CompletedAt: completedAt,
		UpdatedAt:   completedAt,
	})
	if err != nil {
		t.Fatalf("CompleteTurnRequest() error = %v", err)
	}

	touched, ok, err := store.TouchTurnRequest(ctx, "run-001", "req-001", "turn-001", completedAt.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("TouchTurnRequest() error = %v", err)
	}
	if ok {
		t.Fatal("touched=true want false")
	}
	if touched.Status != run.TurnRequestStatusSucceeded {
		t.Fatalf("status=%q want succeeded", touched.Status)
	}
	if !touched.UpdatedAt.Equal(completed.UpdatedAt) {
		t.Fatalf("updated_at=%s want unchanged %s", touched.UpdatedAt, completed.UpdatedAt)
	}
	if string(touched.OutputJSON) != `{"summary":"done"}` {
		t.Fatalf("output_json=%s want completed output", touched.OutputJSON)
	}
}

func TestTouchTurnRequest_DifferentTurnIDConflicts(t *testing.T) {
	t.Parallel()

	ctx, store := newTestStore(t)
	if _, _, err := store.BeginTurnRequest(ctx, beginRecord()); err != nil {
		t.Fatalf("BeginTurnRequest() error = %v", err)
	}

	_, _, err := store.TouchTurnRequest(ctx, "run-001", "req-001", "turn-002", time.Date(2026, time.May, 6, 12, 5, 0, 0, time.UTC))
	if !errors.Is(err, run.ErrTurnRequestConflict) {
		t.Fatalf("TouchTurnRequest() error=%v want ErrTurnRequestConflict", err)
	}
}

func TestRecoverStaleTurnRequest_TouchedRunningRowIsNotReclaimed(t *testing.T) {
	t.Parallel()

	ctx, store := newTestStore(t)
	old := time.Date(2026, time.May, 6, 11, 50, 0, 0, time.UTC)
	touchedAt := time.Date(2026, time.May, 6, 12, 5, 0, 0, time.UTC)
	store.now = func() time.Time { return old }
	if _, _, err := store.BeginTurnRequest(ctx, beginRecord()); err != nil {
		t.Fatalf("BeginTurnRequest() error = %v", err)
	}
	touched, ok, err := store.TouchTurnRequest(ctx, "run-001", "req-001", "turn-001", touchedAt)
	if err != nil {
		t.Fatalf("TouchTurnRequest() error = %v", err)
	}
	if !ok {
		t.Fatal("touched=false want true")
	}

	store.now = func() time.Time { return touchedAt.Add(time.Minute) }
	recovered, ok, err := store.RecoverStaleTurnRequest(ctx, beginRecord(), touchedAt.Add(-time.Minute))
	if err != nil {
		t.Fatalf("RecoverStaleTurnRequest() error = %v", err)
	}
	if ok {
		t.Fatal("recovered=true want false")
	}
	if !recovered.UpdatedAt.Equal(touched.UpdatedAt) {
		t.Fatalf("updated_at=%s want touched %s", recovered.UpdatedAt, touched.UpdatedAt)
	}
}

func TestCompleteTurnRequest_SucceededStoresOutputJSON(t *testing.T) {
	t.Parallel()

	ctx, store := newTestStore(t)
	if _, _, err := store.BeginTurnRequest(ctx, beginRecord()); err != nil {
		t.Fatalf("BeginTurnRequest() error = %v", err)
	}

	completed, err := store.CompleteTurnRequest(ctx, run.TurnRequestRecord{
		RunID:      "run-001",
		RequestID:  "req-001",
		TurnID:     "turn-001",
		Status:     run.TurnRequestStatusSucceeded,
		OutputJSON: []byte(`{"summary":"done"}`),
	})
	if err != nil {
		t.Fatalf("CompleteTurnRequest() error = %v", err)
	}
	if completed.Status != run.TurnRequestStatusSucceeded {
		t.Fatalf("status=%q want succeeded", completed.Status)
	}
	if string(completed.OutputJSON) != `{"summary":"done"}` {
		t.Fatalf("output_json=%s", completed.OutputJSON)
	}
	if len(completed.ErrorJSON) != 0 {
		t.Fatalf("error_json=%s want empty", completed.ErrorJSON)
	}
}

func TestCompleteTurnRequest_FailedStoresErrorJSON(t *testing.T) {
	t.Parallel()

	ctx, store := newTestStore(t)
	if _, _, err := store.BeginTurnRequest(ctx, beginRecord()); err != nil {
		t.Fatalf("BeginTurnRequest() error = %v", err)
	}

	completed, err := store.CompleteTurnRequest(ctx, run.TurnRequestRecord{
		RunID:     "run-001",
		RequestID: "req-001",
		TurnID:    "turn-001",
		Status:    run.TurnRequestStatusFailed,
		ErrorJSON: []byte(`{"message":"failed"}`),
	})
	if err != nil {
		t.Fatalf("CompleteTurnRequest() error = %v", err)
	}
	if completed.Status != run.TurnRequestStatusFailed {
		t.Fatalf("status=%q want failed", completed.Status)
	}
	if string(completed.ErrorJSON) != `{"message":"failed"}` {
		t.Fatalf("error_json=%s", completed.ErrorJSON)
	}
	if len(completed.OutputJSON) != 0 {
		t.Fatalf("output_json=%s want empty", completed.OutputJSON)
	}
}

func TestCompleteTurnRequest_TerminalRowCannotBeOverwritten(t *testing.T) {
	t.Parallel()

	ctx, store := newTestStore(t)
	if _, _, err := store.BeginTurnRequest(ctx, beginRecord()); err != nil {
		t.Fatalf("BeginTurnRequest() error = %v", err)
	}
	first, err := store.CompleteTurnRequest(ctx, run.TurnRequestRecord{
		RunID:      "run-001",
		RequestID:  "req-001",
		TurnID:     "turn-001",
		Status:     run.TurnRequestStatusSucceeded,
		OutputJSON: []byte(`{"summary":"first"}`),
	})
	if err != nil {
		t.Fatalf("CompleteTurnRequest() first error = %v", err)
	}

	second, err := store.CompleteTurnRequest(ctx, run.TurnRequestRecord{
		RunID:     "run-001",
		RequestID: "req-001",
		TurnID:    "turn-001",
		Status:    run.TurnRequestStatusFailed,
		ErrorJSON: []byte(`{"message":"second"}`),
	})
	if err != nil {
		t.Fatalf("CompleteTurnRequest() second error = %v", err)
	}
	if second.Status != first.Status {
		t.Fatalf("status=%q want original %q", second.Status, first.Status)
	}
	if string(second.OutputJSON) != `{"summary":"first"}` {
		t.Fatalf("output_json=%s want first output", second.OutputJSON)
	}
	if len(second.ErrorJSON) != 0 {
		t.Fatalf("error_json=%s want empty", second.ErrorJSON)
	}
}

func newTestStore(t *testing.T) (context.Context, *Store) {
	t.Helper()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "turn_requests.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.May, 6, 12, 0, 0, 0, time.UTC)
	store := NewStore(db)
	store.now = func() time.Time { return now }
	return ctx, store
}

func beginRecord() run.TurnRequestRecord {
	return run.TurnRequestRecord{
		RunID:     "run-001",
		RequestID: "req-001",
		TurnID:    "turn-001",
	}
}
