package queue

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"
	"time"

	"github.com/joshka0/foxctl/internal/storage/sqlutil"
)

func TestRequeueStaleRunningMovesOldRunningJobToRetry(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "queue.db"), Options{Table: "test_queue"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	result, err := store.Enqueue(ctx, EnqueueRequest{
		GroupID:   "workspace",
		Payload:   []byte(`{"path":"main.go"}`),
		DedupeKey: "dedupe",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Queued != 1 || len(result.JobIDs) != 1 {
		t.Fatalf("enqueue result=%+v", result)
	}

	claimed, err := store.ClaimNext(ctx, ClaimOptions{GroupID: "workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil {
		t.Fatal("expected claimed job")
	}

	affected, err := store.RequeueStaleRunning(ctx, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if affected != 0 {
		t.Fatalf("affected=%d want 0 for fresh running job", affected)
	}

	staleUpdatedAt := sqlutil.FormatTimestamp(time.Now().UTC().Add(-2 * time.Hour))
	if _, err := store.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET updated_at = ? WHERE id = ?
	`, store.table), staleUpdatedAt, claimed.ID); err != nil {
		t.Fatal(err)
	}

	affected, err = store.RequeueStaleRunning(ctx, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if affected != 1 {
		t.Fatalf("affected=%d want 1", affected)
	}

	stats, err := store.Stats(ctx, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	if stats.QueuedCount != 1 || stats.RunningCount != 0 {
		t.Fatalf("stats=%+v want queued=1 running=0", stats)
	}

	reclaimed, err := store.ClaimNext(ctx, ClaimOptions{GroupID: "workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed == nil || reclaimed.ID != claimed.ID {
		t.Fatalf("reclaimed=%+v want id=%s", reclaimed, claimed.ID)
	}
	if reclaimed.Attempts != 2 {
		t.Fatalf("attempts=%d want 2", reclaimed.Attempts)
	}
}

func TestFailRetriesUntilMaxAttemptsThenTerminalError(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "queue.db"), Options{Table: "test_queue"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	result, err := store.Enqueue(ctx, EnqueueRequest{
		GroupID:     "workspace",
		Payload:     []byte(`{"kind":"summary","id":"a"}`),
		DedupeKey:   "dedupe",
		MaxAttempts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Queued != 1 || len(result.JobIDs) != 1 {
		t.Fatalf("enqueue result=%+v", result)
	}

	claimed, err := store.ClaimNext(ctx, ClaimOptions{GroupID: "workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil {
		t.Fatal("expected claimed job")
	}
	if claimed.Attempts != 1 {
		t.Fatalf("first claim attempts=%d want 1", claimed.Attempts)
	}

	if err := store.Fail(ctx, claimed.ID, "first failure"); err != nil {
		t.Fatal(err)
	}
	retryJob, err := store.GetJob(ctx, claimed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retryJob.State != StateRetry || retryJob.Error != "first failure" || retryJob.ScheduledAt.IsZero() || retryJob.CompletedAt != nil {
		t.Fatalf("retry job=%+v, want retry state with schedule, error, and no completion", retryJob)
	}

	blocked, err := store.ClaimNext(ctx, ClaimOptions{GroupID: "workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if blocked != nil {
		t.Fatalf("scheduled retry was claimed too early: %+v", blocked)
	}

	pastScheduledAt := sqlutil.FormatTimestamp(time.Now().UTC().Add(-time.Minute))
	if _, err := store.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET scheduled_at = ? WHERE id = ?
	`, store.table), pastScheduledAt, claimed.ID); err != nil {
		t.Fatal(err)
	}

	reclaimed, err := store.ClaimNext(ctx, ClaimOptions{GroupID: "workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed == nil || reclaimed.ID != claimed.ID {
		t.Fatalf("reclaimed=%+v want id=%s", reclaimed, claimed.ID)
	}
	if reclaimed.Attempts != 2 {
		t.Fatalf("second claim attempts=%d want 2", reclaimed.Attempts)
	}

	if err := store.Fail(ctx, reclaimed.ID, "terminal failure"); err != nil {
		t.Fatal(err)
	}
	failedJob, err := store.GetJob(ctx, claimed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failedJob.State != StateError || failedJob.Error != "terminal failure" || failedJob.CompletedAt == nil {
		t.Fatalf("failed job=%+v, want terminal error with completion time", failedJob)
	}

	terminalClaim, err := store.ClaimNext(ctx, ClaimOptions{GroupID: "workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if terminalClaim != nil {
		t.Fatalf("terminal error job should not be claimable: %+v", terminalClaim)
	}

	stats, err := store.Stats(ctx, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	if stats.FailedCount != 1 || stats.QueuedCount != 0 || stats.RunningCount != 0 {
		t.Fatalf("stats=%+v want failed=1 queued=0 running=0", stats)
	}
}

func TestCompleteRequiresRunningJob(t *testing.T) {
	ctx := context.Background()
	store := openTestQueueStore(t, ctx)

	result, err := store.Enqueue(ctx, EnqueueRequest{
		GroupID:   "workspace",
		Payload:   []byte(`{"kind":"summary","id":"queued"}`),
		DedupeKey: "queued",
	})
	if err != nil {
		t.Fatal(err)
	}
	jobID := result.JobIDs[0]

	if err := store.Complete(ctx, jobID); err == nil {
		t.Fatal("expected completing a queued job to be rejected")
	}
	queued, err := store.GetJob(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if queued.State != StateQueued || queued.CompletedAt != nil {
		t.Fatalf("queued job changed after rejected complete: %+v", queued)
	}

	claimed, err := store.ClaimNext(ctx, ClaimOptions{GroupID: "workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != jobID {
		t.Fatalf("claimed=%+v want queued job %s", claimed, jobID)
	}
	if err := store.Complete(ctx, jobID); err != nil {
		t.Fatalf("complete running job: %v", err)
	}
	completed, err := store.GetJob(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != StateOK || completed.CompletedAt == nil {
		t.Fatalf("completed job=%+v want terminal ok with completion time", completed)
	}
}

func TestFailRequiresRunningJob(t *testing.T) {
	ctx := context.Background()
	store := openTestQueueStore(t, ctx)

	result, err := store.Enqueue(ctx, EnqueueRequest{
		GroupID:   "workspace",
		Payload:   []byte(`{"kind":"summary","id":"queued"}`),
		DedupeKey: "queued",
	})
	if err != nil {
		t.Fatal(err)
	}
	jobID := result.JobIDs[0]

	if err := store.Fail(ctx, jobID, "should not fail before claim"); err == nil {
		t.Fatal("expected failing a queued job to be rejected")
	}
	got, err := store.GetJob(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateQueued || got.Error != "" || !got.ScheduledAt.IsZero() {
		t.Fatalf("queued job changed after rejected fail: %+v", got)
	}
}

func TestTerminalQueueJobsRejectFurtherTransitions(t *testing.T) {
	ctx := context.Background()
	store := openTestQueueStore(t, ctx)

	okResult, err := store.Enqueue(ctx, EnqueueRequest{
		GroupID:   "workspace",
		Payload:   []byte(`{"kind":"summary","id":"ok"}`),
		DedupeKey: "ok",
	})
	if err != nil {
		t.Fatal(err)
	}
	okJob, err := store.ClaimNext(ctx, ClaimOptions{GroupID: "workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if okJob == nil || okJob.ID != okResult.JobIDs[0] {
		t.Fatalf("claimed=%+v want ok job", okJob)
	}
	if err := store.Complete(ctx, okJob.ID); err != nil {
		t.Fatalf("complete ok job: %v", err)
	}
	completed, err := store.GetJob(ctx, okJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	completedAt := completed.CompletedAt
	if completedAt == nil {
		t.Fatal("completed job missing completed_at")
	}

	if err := store.Fail(ctx, okJob.ID, "late failure"); err == nil {
		t.Fatal("expected failing a completed job to be rejected")
	}
	if err := store.Complete(ctx, okJob.ID); err == nil {
		t.Fatal("expected completing an already completed job to be rejected")
	}
	afterOK, err := store.GetJob(ctx, okJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterOK.State != StateOK || afterOK.Error != "" || afterOK.CompletedAt == nil || !afterOK.CompletedAt.Equal(*completedAt) {
		t.Fatalf("terminal ok job changed after rejected transitions: %+v", afterOK)
	}

	errResult, err := store.Enqueue(ctx, EnqueueRequest{
		GroupID:     "workspace",
		Payload:     []byte(`{"kind":"summary","id":"error"}`),
		DedupeKey:   "error",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	errJob, err := store.ClaimNext(ctx, ClaimOptions{GroupID: "workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if errJob == nil || errJob.ID != errResult.JobIDs[0] {
		t.Fatalf("claimed=%+v want error job", errJob)
	}
	if err := store.Fail(ctx, errJob.ID, "terminal failure"); err != nil {
		t.Fatalf("terminal fail: %v", err)
	}
	if err := store.Complete(ctx, errJob.ID); err == nil {
		t.Fatal("expected completing a terminal error job to be rejected")
	}
	afterError, err := store.GetJob(ctx, errJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterError.State != StateError || afterError.Error != "terminal failure" || afterError.CompletedAt == nil {
		t.Fatalf("terminal error job changed after rejected complete: %+v", afterError)
	}
}

func TestTerminalQueueJobPropertyRejectsReopen(t *testing.T) {
	ctx := context.Background()

	prop := func(completeFirst, retryWithFail bool) bool {
		store := openTestQueueStore(t, ctx)
		maxAttempts := 1
		if retryWithFail {
			maxAttempts = 2
		}
		result, err := store.Enqueue(ctx, EnqueueRequest{
			GroupID:     "workspace",
			Payload:     []byte(`{"kind":"summary","id":"terminal"}`),
			DedupeKey:   "terminal",
			MaxAttempts: maxAttempts,
		})
		if err != nil {
			t.Logf("enqueue: %v", err)
			return false
		}
		claimed, err := store.ClaimNext(ctx, ClaimOptions{GroupID: "workspace"})
		if err != nil || claimed == nil || claimed.ID != result.JobIDs[0] {
			t.Logf("claim: job=%+v err=%v", claimed, err)
			return false
		}

		wantState := StateError
		wantError := "terminal failure"
		if completeFirst {
			wantState = StateOK
			wantError = ""
			if err := store.Complete(ctx, claimed.ID); err != nil {
				t.Logf("complete terminal: %v", err)
				return false
			}
		} else {
			if retryWithFail {
				if err := store.Fail(ctx, claimed.ID, "retry failure"); err != nil {
					t.Logf("retry fail: %v", err)
					return false
				}
				if _, err := store.db.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET scheduled_at = ? WHERE id = ?`, store.table), sqlutil.FormatTimestamp(time.Now().UTC().Add(-time.Minute)), claimed.ID); err != nil {
					t.Logf("force retry due: %v", err)
					return false
				}
				claimed, err = store.ClaimNext(ctx, ClaimOptions{GroupID: "workspace"})
				if err != nil || claimed == nil {
					t.Logf("claim retry: job=%+v err=%v", claimed, err)
					return false
				}
			}
			if err := store.Fail(ctx, claimed.ID, wantError); err != nil {
				t.Logf("terminal fail: %v", err)
				return false
			}
		}

		if err := store.Complete(ctx, claimed.ID); err == nil {
			t.Logf("complete reopened terminal job")
			return false
		}
		if err := store.Fail(ctx, claimed.ID, "late failure"); err == nil {
			t.Logf("fail reopened terminal job")
			return false
		}
		got, err := store.GetJob(ctx, claimed.ID)
		return err == nil && got.State == wantState && got.Error == wantError && got.CompletedAt != nil
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 50}); err != nil {
		t.Fatalf("terminal queue job property failed: %v", err)
	}
}

func TestEnqueueBatchInvalidRequestRollsBackEarlierInsert(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "queue.db"), Options{Table: "test_queue"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	_, err = store.EnqueueBatch(ctx, []EnqueueRequest{
		{
			GroupID:   "workspace",
			Payload:   []byte(`{"kind":"summary","id":"a"}`),
			DedupeKey: "valid",
		},
		{
			GroupID: "workspace",
			Payload: []byte(`{"kind":"summary","id":"b"}`),
		},
	})
	if err == nil {
		t.Fatal("expected batch validation error")
	}

	stats, err := store.Stats(ctx, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	if stats.QueuedCount != 0 || stats.RunningCount != 0 || stats.CompletedCount != 0 || stats.FailedCount != 0 {
		t.Fatalf("stats=%+v want empty queue after failed batch", stats)
	}

	claimed, err := store.ClaimNext(ctx, ClaimOptions{GroupID: "workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if claimed != nil {
		t.Fatalf("failed batch inserted claimable job: %+v", claimed)
	}
}

func TestGetJobRejectsCorruptPersistedTimestamps(t *testing.T) {
	ctx := context.Background()

	for _, column := range []string{"created_at", "updated_at", "scheduled_at", "completed_at"} {
		t.Run(column, func(t *testing.T) {
			store := openTestQueueStore(t, ctx)
			result, err := store.Enqueue(ctx, EnqueueRequest{
				GroupID:   "workspace",
				Payload:   []byte(`{"kind":"summary","id":"corrupt-timestamp"}`),
				DedupeKey: "corrupt-" + column,
			})
			if err != nil {
				t.Fatal(err)
			}

			if _, err := store.db.ExecContext(ctx, fmt.Sprintf(`
				UPDATE %s SET %s = ? WHERE id = ?
			`, store.table, column), "not-a-timestamp", result.JobIDs[0]); err != nil {
				t.Fatal(err)
			}

			_, err = store.GetJob(ctx, result.JobIDs[0])
			if err == nil {
				t.Fatalf("GetJob accepted corrupt %s", column)
			}
			if !strings.Contains(err.Error(), column) {
				t.Fatalf("GetJob error=%v, want it to name corrupt column %s", err, column)
			}
		})
	}
}

func TestQueueStatsRejectsCorruptQueuedTimestamp(t *testing.T) {
	ctx := context.Background()
	store := openTestQueueStore(t, ctx)

	result, err := store.Enqueue(ctx, EnqueueRequest{
		GroupID:   "workspace",
		Payload:   []byte(`{"kind":"summary","id":"corrupt-oldest"}`),
		DedupeKey: "corrupt-oldest",
	})
	if err != nil {
		t.Fatal(err)
	}
	jobID := result.JobIDs[0]

	if _, err := store.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET created_at = ? WHERE id = ?
	`, store.table), "not-a-timestamp", jobID); err != nil {
		t.Fatalf("corrupt created_at: %v", err)
	}

	if _, err := store.Stats(ctx, "workspace"); err == nil {
		t.Fatal("Stats accepted corrupt queued created_at")
	} else if !strings.Contains(err.Error(), "created_at") {
		t.Fatalf("Stats error=%v, want it to name created_at", err)
	}

	if _, err := store.StatsForKind(ctx, "workspace", "summary"); err == nil {
		t.Fatal("StatsForKind accepted corrupt queued created_at")
	} else if !strings.Contains(err.Error(), "created_at") {
		t.Fatalf("StatsForKind error=%v, want it to name created_at", err)
	}
}

func TestQueueReadsRejectCorruptPersistedState(t *testing.T) {
	ctx := context.Background()
	store := openTestQueueStore(t, ctx)

	result, err := store.Enqueue(ctx, EnqueueRequest{
		GroupID:   "workspace",
		Payload:   []byte(`{"kind":"summary","id":"corrupt-state"}`),
		DedupeKey: "corrupt-state",
	})
	if err != nil {
		t.Fatal(err)
	}
	jobID := result.JobIDs[0]

	if _, err := store.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET state = ? WHERE id = ?
	`, store.table), "paused", jobID); err != nil {
		t.Fatal(err)
	}

	if _, err := store.GetJob(ctx, jobID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("GetJob() error=%v, want ErrInvalidState", err)
	} else if !strings.Contains(err.Error(), "paused") {
		t.Fatalf("GetJob() error=%v, want corrupt state value", err)
	}

	if _, err := store.Stats(ctx, "workspace"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("Stats() error=%v, want ErrInvalidState", err)
	}
	if _, err := store.StatsForKind(ctx, "workspace", "summary"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("StatsForKind() error=%v, want ErrInvalidState", err)
	}
}

func TestEnqueueDefaultPriorityIsNormalAndExplicitLowIsPreserved(t *testing.T) {
	ctx := context.Background()
	store := openTestQueueStore(t, ctx)

	result, err := store.EnqueueBatch(ctx, []EnqueueRequest{
		{
			GroupID:   "workspace",
			Payload:   []byte(`{"kind":"summary","id":"default"}`),
			DedupeKey: "default",
		},
		{
			GroupID:   "workspace",
			Payload:   []byte(`{"kind":"summary","id":"low"}`),
			DedupeKey: "low",
			Priority:  PriorityLow,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Queued != 2 {
		t.Fatalf("enqueue result=%+v, want two queued jobs", result)
	}

	defaultJob, err := store.GetJob(ctx, result.JobIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if defaultJob.Priority != PriorityNormal {
		t.Fatalf("default priority=%d want normal=%d", defaultJob.Priority, PriorityNormal)
	}
	lowJob, err := store.GetJob(ctx, result.JobIDs[1])
	if err != nil {
		t.Fatal(err)
	}
	if lowJob.Priority != PriorityLow {
		t.Fatalf("explicit low priority=%d want low=%d", lowJob.Priority, PriorityLow)
	}
}

func TestClaimNextOrdersByPriorityThenCreatedAt(t *testing.T) {
	ctx := context.Background()
	store := openTestQueueStore(t, ctx)
	now := time.Now().UTC()

	lowResult, err := store.Enqueue(ctx, EnqueueRequest{
		GroupID:   "workspace",
		Payload:   []byte(`{"kind":"summary","id":"low"}`),
		DedupeKey: "low",
		Priority:  PriorityLow,
	})
	if err != nil {
		t.Fatal(err)
	}
	highResult, err := store.Enqueue(ctx, EnqueueRequest{
		GroupID:   "workspace",
		Payload:   []byte(`{"kind":"summary","id":"high"}`),
		DedupeKey: "high",
		Priority:  PriorityHigh,
	})
	if err != nil {
		t.Fatal(err)
	}
	forceQueueJobCreatedAt(t, ctx, store, lowResult.JobIDs[0], now.Add(-time.Hour))
	forceQueueJobCreatedAt(t, ctx, store, highResult.JobIDs[0], now)

	claimed, err := store.ClaimNext(ctx, ClaimOptions{GroupID: "workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.DedupeKey != "high" {
		t.Fatalf("first claimed=%+v want newer high-priority job before older low-priority job", claimed)
	}

	firstResult, err := store.Enqueue(ctx, EnqueueRequest{
		GroupID:   "fifo",
		Payload:   []byte(`{"kind":"summary","id":"first"}`),
		DedupeKey: "first",
		Priority:  PriorityNormal,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondResult, err := store.Enqueue(ctx, EnqueueRequest{
		GroupID:   "fifo",
		Payload:   []byte(`{"kind":"summary","id":"second"}`),
		DedupeKey: "second",
		Priority:  PriorityNormal,
	})
	if err != nil {
		t.Fatal(err)
	}
	forceQueueJobCreatedAt(t, ctx, store, firstResult.JobIDs[0], now.Add(-2*time.Hour))
	forceQueueJobCreatedAt(t, ctx, store, secondResult.JobIDs[0], now.Add(-time.Hour))

	claimed, err = store.ClaimNext(ctx, ClaimOptions{GroupID: "fifo"})
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.DedupeKey != "first" {
		t.Fatalf("same-priority claimed=%+v want oldest queued job first", claimed)
	}
}

func TestClaimNextPropertyClaimsHighestPriorityFirst(t *testing.T) {
	ctx := context.Background()

	property := func(rawLow, rawDelta uint8) bool {
		store, err := Open(ctx, filepath.Join(t.TempDir(), "queue.db"), Options{Table: "test_queue"})
		if err != nil {
			t.Logf("open queue: %v", err)
			return false
		}
		defer func() { _ = store.Close() }()

		lowPriority := PriorityLow + JobPriority(rawLow%30)
		highPriority := lowPriority + JobPriority(rawDelta%50) + 1
		lowResult, err := store.Enqueue(ctx, EnqueueRequest{
			GroupID:   "workspace",
			Payload:   []byte(`{"kind":"summary","id":"low"}`),
			DedupeKey: "low",
			Priority:  lowPriority,
		})
		if err != nil {
			t.Logf("enqueue low priority: %v", err)
			return false
		}
		highResult, err := store.Enqueue(ctx, EnqueueRequest{
			GroupID:   "workspace",
			Payload:   []byte(`{"kind":"summary","id":"high"}`),
			DedupeKey: "high",
			Priority:  highPriority,
		})
		if err != nil {
			t.Logf("enqueue high priority: %v", err)
			return false
		}
		forceQueueJobCreatedAt(t, ctx, store, lowResult.JobIDs[0], time.Now().UTC().Add(-time.Hour))
		forceQueueJobCreatedAt(t, ctx, store, highResult.JobIDs[0], time.Now().UTC())

		claimed, err := store.ClaimNext(ctx, ClaimOptions{GroupID: "workspace"})
		if err != nil {
			t.Logf("claim: %v", err)
			return false
		}
		if claimed == nil || claimed.DedupeKey != "high" || claimed.Priority != highPriority {
			t.Logf("claimed=%+v lowPriority=%d highPriority=%d", claimed, lowPriority, highPriority)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 75}); err != nil {
		t.Fatalf("priority claim property failed: %v", err)
	}
}

func openTestQueueStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "queue.db"), Options{Table: "test_queue"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close queue store: %v", err)
		}
	})
	return store
}

func forceQueueJobCreatedAt(t *testing.T, ctx context.Context, store *Store, jobID string, createdAt time.Time) {
	t.Helper()
	if _, err := store.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET created_at = ? WHERE id = ?
	`, store.table), sqlutil.FormatTimestamp(createdAt), jobID); err != nil {
		t.Fatalf("force created_at for %s: %v", jobID, err)
	}
}

func seedCompletedQueueJobs(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()

	for _, req := range []EnqueueRequest{
		{GroupID: "workspace", Payload: []byte(`{"kind":"memory","name":"m"}`), DedupeKey: "memory"},
		{GroupID: "workspace", Payload: []byte(`{"kind":"symbol","name":"s"}`), DedupeKey: "symbol"},
		{GroupID: "other", Payload: []byte(`{"kind":"memory","name":"m2"}`), DedupeKey: "other-memory"},
	} {
		if _, err := store.Enqueue(ctx, req); err != nil {
			t.Fatalf("enqueue %s: %v", req.DedupeKey, err)
		}
	}

	for i := 0; i < 3; i++ {
		claimed, err := store.ClaimNext(ctx, ClaimOptions{})
		if err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
		if claimed == nil {
			t.Fatalf("claim %d returned nil", i)
		}
		if err := store.Complete(ctx, claimed.ID); err != nil {
			t.Fatalf("complete %s: %v", claimed.ID, err)
		}
	}
}

func TestClaimNextPayloadKindOnlyClaimsMatchingJSONKind(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "queue.db"), Options{Table: "test_queue"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for _, req := range []EnqueueRequest{
		{GroupID: "workspace", Payload: []byte(`{"kind":"memory","name":"m"}`), DedupeKey: "memory"},
		{GroupID: "workspace", Payload: []byte(`{"name":"legacy-symbol"}`), DedupeKey: "legacy-symbol"},
		{GroupID: "workspace", Payload: []byte(`{"kind":"symbol","name":"s"}`), DedupeKey: "symbol"},
	} {
		if _, err := store.Enqueue(ctx, req); err != nil {
			t.Fatalf("enqueue %s: %v", req.DedupeKey, err)
		}
	}

	claimed, err := store.ClaimNext(ctx, ClaimOptions{GroupID: "workspace", PayloadKind: "symbol"})
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.DedupeKey != "legacy-symbol" {
		t.Fatalf("claimed=%+v want legacy symbol", claimed)
	}

	stats, err := store.Stats(ctx, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	if stats.QueuedCount != 2 || stats.RunningCount != 1 {
		t.Fatalf("stats=%+v want queued=2 running=1", stats)
	}

	symbolStats, err := store.StatsForKind(ctx, "workspace", "symbol")
	if err != nil {
		t.Fatal(err)
	}
	if symbolStats.QueuedCount != 1 || symbolStats.RunningCount != 1 {
		t.Fatalf("symbolStats=%+v want queued=1 running=1", symbolStats)
	}

	memoryStats, err := store.StatsForKind(ctx, "workspace", "memory")
	if err != nil {
		t.Fatal(err)
	}
	if memoryStats.QueuedCount != 1 || memoryStats.RunningCount != 0 {
		t.Fatalf("memoryStats=%+v want queued=1 running=0", memoryStats)
	}
}

func TestRequeueStaleRunningForGroupKindOnlyRecoversMatchingJSONKind(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "queue.db"), Options{Table: "test_queue"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for _, req := range []EnqueueRequest{
		{GroupID: "workspace", Payload: []byte(`{"kind":"memory","name":"m"}`), DedupeKey: "memory"},
		{GroupID: "workspace", Payload: []byte(`{"kind":"symbol","name":"s"}`), DedupeKey: "symbol"},
	} {
		if _, err := store.Enqueue(ctx, req); err != nil {
			t.Fatalf("enqueue %s: %v", req.DedupeKey, err)
		}
	}
	for i := 0; i < 2; i++ {
		claimed, err := store.ClaimNext(ctx, ClaimOptions{GroupID: "workspace"})
		if err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
		if claimed == nil {
			t.Fatalf("claim %d returned nil", i)
		}
	}

	staleUpdatedAt := sqlutil.FormatTimestamp(time.Now().UTC().Add(-2 * time.Hour))
	if _, err := store.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET updated_at = ? WHERE state = 'running'
	`, store.table), staleUpdatedAt); err != nil {
		t.Fatal(err)
	}

	affected, err := store.RequeueStaleRunningForGroupKind(ctx, time.Hour, "workspace", "symbol")
	if err != nil {
		t.Fatal(err)
	}
	if affected != 1 {
		t.Fatalf("affected=%d want 1", affected)
	}

	symbolStats, err := store.StatsForKind(ctx, "workspace", "symbol")
	if err != nil {
		t.Fatal(err)
	}
	if symbolStats.QueuedCount != 1 || symbolStats.RunningCount != 0 {
		t.Fatalf("symbolStats=%+v want queued=1 running=0", symbolStats)
	}

	memoryStats, err := store.StatsForKind(ctx, "workspace", "memory")
	if err != nil {
		t.Fatal(err)
	}
	if memoryStats.QueuedCount != 0 || memoryStats.RunningCount != 1 {
		t.Fatalf("memoryStats=%+v want queued=0 running=1", memoryStats)
	}
}

func TestCleanupForGroupKindOnlyDeletesMatchingCompletedJobs(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "queue.db"), Options{Table: "test_queue"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for _, req := range []EnqueueRequest{
		{GroupID: "workspace", Payload: []byte(`{"kind":"memory","name":"m"}`), DedupeKey: "memory"},
		{GroupID: "workspace", Payload: []byte(`{"kind":"symbol","name":"s"}`), DedupeKey: "symbol"},
		{GroupID: "other", Payload: []byte(`{"kind":"memory","name":"m2"}`), DedupeKey: "other-memory"},
	} {
		if _, err := store.Enqueue(ctx, req); err != nil {
			t.Fatalf("enqueue %s: %v", req.DedupeKey, err)
		}
	}
	for i := 0; i < 3; i++ {
		claimed, err := store.ClaimNext(ctx, ClaimOptions{})
		if err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
		if claimed == nil {
			t.Fatalf("claim %d returned nil", i)
		}
		if err := store.Complete(ctx, claimed.ID); err != nil {
			t.Fatalf("complete %s: %v", claimed.ID, err)
		}
	}

	deleted, err := store.CleanupForGroupKind(ctx, 0, "workspace", "memory")
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted=%d want 1", deleted)
	}

	workspaceStats, err := store.Stats(ctx, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	if workspaceStats.CompletedCount != 1 {
		t.Fatalf("workspace stats=%+v want one remaining completed job", workspaceStats)
	}
	otherStats, err := store.Stats(ctx, "other")
	if err != nil {
		t.Fatal(err)
	}
	if otherStats.CompletedCount != 1 {
		t.Fatalf("other stats=%+v want untouched completed job", otherStats)
	}
}

func TestCleanupNegativeDurationDoesNotDeleteCompletedJobs(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		cleanup func(*Store) (int64, error)
	}{
		{
			name:    "all",
			cleanup: func(store *Store) (int64, error) { return store.Cleanup(ctx, -time.Hour) },
		},
		{
			name:    "group",
			cleanup: func(store *Store) (int64, error) { return store.CleanupForGroup(ctx, -time.Hour, "workspace") },
		},
		{
			name:    "kind",
			cleanup: func(store *Store) (int64, error) { return store.CleanupForKind(ctx, -time.Hour, "memory") },
		},
		{
			name: "group kind",
			cleanup: func(store *Store) (int64, error) {
				return store.CleanupForGroupKind(ctx, -time.Hour, "workspace", "memory")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openTestQueueStore(t, ctx)
			seedCompletedQueueJobs(t, ctx, store)

			deleted, err := tt.cleanup(store)
			if err != nil {
				t.Fatalf("cleanup: %v", err)
			}
			if deleted != 0 {
				t.Fatalf("deleted=%d want 0 for negative cleanup duration", deleted)
			}

			stats, err := store.Stats(ctx, "")
			if err != nil {
				t.Fatalf("stats: %v", err)
			}
			if stats.CompletedCount != 3 || stats.QueuedCount != 0 || stats.RunningCount != 0 || stats.FailedCount != 0 {
				t.Fatalf("stats after negative cleanup=%+v want all completed jobs preserved", stats)
			}
		})
	}
}

func TestPurgeDeletesOnlyMatchingGroupKind(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "queue.db"), Options{Table: "test_queue"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for _, req := range []EnqueueRequest{
		{GroupID: "workspace", Payload: []byte(`{"kind":"memory","name":"m"}`), DedupeKey: "memory"},
		{GroupID: "workspace", Payload: []byte(`{"kind":"symbol","name":"s"}`), DedupeKey: "symbol"},
		{GroupID: "other", Payload: []byte(`{"kind":"memory","name":"m2"}`), DedupeKey: "other-memory"},
	} {
		if _, err := store.Enqueue(ctx, req); err != nil {
			t.Fatalf("enqueue %s: %v", req.DedupeKey, err)
		}
	}

	deleted, err := store.Purge(ctx, "workspace", "memory")
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted=%d want 1", deleted)
	}

	workspaceStats, err := store.Stats(ctx, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	if workspaceStats.QueuedCount != 1 {
		t.Fatalf("workspace stats=%+v want one queued symbol job", workspaceStats)
	}
	otherStats, err := store.Stats(ctx, "other")
	if err != nil {
		t.Fatal(err)
	}
	if otherStats.QueuedCount != 1 {
		t.Fatalf("other stats=%+v want untouched memory job", otherStats)
	}
}
