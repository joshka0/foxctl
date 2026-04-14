package workers

import (
	"context"
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/riverqueue/river"
)

// Periodic worker to delete expired tokens and auth requests.

const oauthCleanupKind = "oauth.cleanup"

type OAuthCleanupArgs struct{}

func (OAuthCleanupArgs) Kind() string { return oauthCleanupKind }

// ExpiredTokenCleaner cleans up expired OAuth tokens and auth requests.
type ExpiredTokenCleaner interface {
	CleanupExpiredOAuth(ctx context.Context) (tokensDeleted int64, requestsDeleted int64, err error)
}

type OAuthCleanupWorker struct {
	river.WorkerDefaults[OAuthCleanupArgs]
	Cleaner ExpiredTokenCleaner
}

func (w *OAuthCleanupWorker) Work(ctx context.Context, job *river.Job[OAuthCleanupArgs]) error {
	if w == nil {
		return fmt.Errorf("jobs: oauth cleanup worker is nil")
	}
	if job == nil {
		return fmt.Errorf("jobs: oauth cleanup job is required")
	}
	if w.Cleaner == nil {
		return fmt.Errorf("jobs: expired token cleaner is required")
	}

	start := time.Now()
	tokensDeleted, requestsDeleted, err := w.Cleaner.CleanupExpiredOAuth(ctx)
	event := observability.NewEvent("jobs.oauth_cleanup").
		WithComponent(observability.ComponentJob).
		WithData("tokens_deleted", tokensDeleted).
		WithData("requests_deleted", requestsDeleted)
	if err != nil {
		wrappedErr := fmt.Errorf("jobs: cleanup expired oauth records: %w", err)
		observability.Emit(ctx, event.Error(wrappedErr, time.Since(start)))
		return wrappedErr
	}

	observability.Emit(ctx, event.Success(time.Since(start)))
	return nil
}

var _ river.Worker[OAuthCleanupArgs] = (*OAuthCleanupWorker)(nil)
