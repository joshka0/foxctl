package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

const (
	riverMigrationAdvisoryLockKey = int64(0x726976_6572)
	riverMigrationLockRetryDelay  = 200 * time.Millisecond
)

// RunMigrations runs River database migrations with an advisory lock guard.
// A single connection is pinned from the pool for the advisory lock's lifetime,
// since PostgreSQL advisory locks are session-scoped (tied to the connection).
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("jobs: migration pool is required")
	}

	event := observability.NewEvent("jobs.river_migrate").
		WithComponent(observability.ComponentJob)

	// Pin a single connection for advisory lock acquire + release.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		wrappedErr := fmt.Errorf("jobs: acquire migration connection: %w", err)
		observability.Emit(ctx, event.Error(wrappedErr, 0))
		return wrappedErr
	}
	defer conn.Release()

	if err := acquireMigrationLock(ctx, conn, riverMigrationAdvisoryLockKey, riverMigrationLockRetryDelay); err != nil {
		observability.Emit(ctx, event.Error(err, 0))
		return err
	}
	defer releaseMigrationLock(conn, riverMigrationAdvisoryLockKey)

	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		wrappedErr := fmt.Errorf("jobs: create river migrator: %w", err)
		observability.Emit(ctx, event.Error(wrappedErr, 0))
		return wrappedErr
	}

	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		wrappedErr := fmt.Errorf("jobs: run river migrations: %w", err)
		observability.Emit(ctx, event.Error(wrappedErr, 0))
		return wrappedErr
	}

	observability.Emit(ctx, event.Success(0))
	return nil
}

func acquireMigrationLock(ctx context.Context, conn *pgxpool.Conn, key int64, retryDelay time.Duration) error {
	for {
		var acquired bool
		err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&acquired)
		if err != nil {
			return fmt.Errorf("jobs: acquire migration advisory lock: %w", err)
		}
		if acquired {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("jobs: waiting for migration advisory lock: %w", ctx.Err())
		case <-time.After(retryDelay):
		}
	}
}

func releaseMigrationLock(conn *pgxpool.Conn, key int64) {
	_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", key)
}
