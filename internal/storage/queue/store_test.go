package queue

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
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
