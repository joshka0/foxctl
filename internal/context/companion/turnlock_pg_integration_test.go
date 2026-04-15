//go:build integration

package companion

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPgTurnLock_LockUnlockCycle(t *testing.T) {
	dsn := os.Getenv("FOXCTL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FOXCTL_TEST_POSTGRES_DSN to run integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create pgxpool: %v", err)
	}
	defer pool.Close()

	locker := NewPgTurnLock(pool)
	conversationID := "integration-lock-cycle"

	unlock1, err := locker.Lock(ctx, conversationID)
	if err != nil {
		t.Fatalf("lock 1: %v", err)
	}

	if unlock2, acquired, err := locker.TryLock(ctx, conversationID); err != nil {
		t.Fatalf("try lock while held: %v", err)
	} else if acquired {
		if unlock2 != nil {
			unlock2()
		}
		t.Fatal("expected try-lock miss while first lock is held")
	}

	unlock1()

	unlock3, acquired, err := locker.TryLock(ctx, conversationID)
	if err != nil {
		t.Fatalf("try lock after unlock: %v", err)
	}
	if !acquired {
		t.Fatal("expected try-lock success after unlock")
	}
	unlock3()
}

func TestPgTurnLock_Lock_RespectsContextCancellation(t *testing.T) {
	dsn := os.Getenv("FOXCTL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FOXCTL_TEST_POSTGRES_DSN to run integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create pgxpool: %v", err)
	}
	defer pool.Close()

	locker := NewPgTurnLock(pool)
	conversationID := "integration-lock-cancel"

	unlock, err := locker.Lock(ctx, conversationID)
	if err != nil {
		t.Fatalf("initial lock: %v", err)
	}
	defer unlock()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer waitCancel()

	if _, err := locker.Lock(waitCtx, conversationID); err == nil {
		t.Fatal("expected cancellation error while waiting for lock")
	} else if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation error, got: %v", err)
	}
}
