package companion

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

// PgTurnLock uses Postgres session-level advisory locks for cross-pod turn serialization.
type PgTurnLock struct {
	pool *pgxpool.Pool
}

var _ Locker = (*PgTurnLock)(nil)

// NewPgTurnLock creates a PgTurnLock that acquires advisory locks via the provided pool.
func NewPgTurnLock(pool *pgxpool.Pool) *PgTurnLock {
	return &PgTurnLock{pool: pool}
}

// conversationLockID generates a stable int64 lock key from a conversation ID using FNV-1a.
// Values above math.MaxInt64 wrap to negative int64; this is expected and harmless for
// Postgres advisory lock IDs which accept any int64.
func conversationLockID(conversationID string) int64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(conversationID))
	return int64(hasher.Sum64())
}

// Lock acquires a session-level advisory lock on a dedicated connection.
// The lock is held until the returned unlock function is called.
// Lock respects context cancellation while waiting to acquire the lock.
func (pl *PgTurnLock) Lock(ctx context.Context, conversationID string) (unlock func(), err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pl == nil || pl.pool == nil {
		return nil, errors.New("postgres turn lock pool is nil")
	}

	conn, err := pl.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire postgres connection: %w", err)
	}

	lockID := conversationLockID(conversationID)
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockID); err != nil {
		conn.Release()
		return nil, fmt.Errorf("acquire advisory lock: %w", err)
	}

	return advisoryUnlock(conn, lockID), nil
}

// WithTurnLock acquires the conversation lock, runs fn, and then releases the lock.
func (pl *PgTurnLock) WithTurnLock(ctx context.Context, conversationID string, fn func(ctx context.Context) error) error {
	if fn == nil {
		return errors.New("turn lock callback is nil")
	}

	unlock, err := pl.Lock(ctx, conversationID)
	if err != nil {
		return err
	}
	defer unlock()

	return fn(ctx)
}

// TryLock attempts to acquire a session-level advisory lock without blocking.
// It returns (unlock, true, nil) on success and (nil, false, nil) when the lock is held elsewhere.
func (pl *PgTurnLock) TryLock(ctx context.Context, conversationID string) (unlock func(), acquired bool, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pl == nil || pl.pool == nil {
		return nil, false, errors.New("postgres turn lock pool is nil")
	}

	conn, err := pl.pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("acquire postgres connection: %w", err)
	}

	lockID := conversationLockID(conversationID)
	var ok bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", lockID).Scan(&ok); err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("try advisory lock: %w", err)
	}
	if !ok {
		conn.Release()
		return nil, false, nil
	}

	return advisoryUnlock(conn, lockID), true, nil
}

func advisoryUnlock(conn *pgxpool.Conn, lockID int64) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			_, err := conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", lockID)
			if err != nil {
				ctx := context.Background()
				observability.Emit(ctx, observability.NewEvent("companion.advisory_unlock_failed").
					WithComponent("companion").
					WithData("lock_id", lockID).
					WithData("error", err.Error()).
					Success(0))
			}
			conn.Release()
		})
	}
}
