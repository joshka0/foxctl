package workers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/riverqueue/river"
)

const oauthRefreshKind = "oauth.refresh"

// OAuthRefreshArgs contains arguments for a token refresh job.
type OAuthRefreshArgs struct {
	TokenID  string `json:"token_id"`
	TenantID string `json:"tenant_id"`
	Subject  string `json:"subject"`
	Provider string `json:"provider"`
}

func (OAuthRefreshArgs) Kind() string { return oauthRefreshKind }

// TokenRefresher refreshes an OAuth token.
type TokenRefresher interface {
	RefreshToken(ctx context.Context, tokenID string) error
}

// OAuthRefreshWorker refreshes OAuth tokens before they expire.
type OAuthRefreshWorker struct {
	river.WorkerDefaults[OAuthRefreshArgs]
	Refresher TokenRefresher
}

func (w *OAuthRefreshWorker) Work(ctx context.Context, job *river.Job[OAuthRefreshArgs]) error {
	if w == nil {
		return fmt.Errorf("jobs: oauth refresh worker is nil")
	}
	if job == nil {
		return fmt.Errorf("jobs: oauth refresh job is required")
	}
	if w.Refresher == nil {
		return fmt.Errorf("jobs: token refresher is required")
	}

	args := job.Args
	tokenID := strings.TrimSpace(args.TokenID)
	if tokenID == "" {
		return fmt.Errorf("jobs: token_id is required")
	}

	event := observability.NewEvent("jobs.oauth_refresh").
		WithComponent(observability.ComponentJob).
		WithData("token_id", tokenID).
		WithData("tenant_id", strings.TrimSpace(args.TenantID)).
		WithData("subject", strings.TrimSpace(args.Subject)).
		WithData("provider", strings.TrimSpace(args.Provider))

	start := time.Now()
	if err := w.Refresher.RefreshToken(ctx, tokenID); err != nil {
		wrappedErr := fmt.Errorf("jobs: refresh oauth token: %w", err)
		observability.Emit(ctx, event.Error(wrappedErr, time.Since(start)))
		return wrappedErr
	}

	observability.Emit(ctx, event.Success(time.Since(start)))
	return nil
}

var _ river.Worker[OAuthRefreshArgs] = (*OAuthRefreshWorker)(nil)
