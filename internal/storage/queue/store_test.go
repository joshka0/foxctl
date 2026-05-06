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
