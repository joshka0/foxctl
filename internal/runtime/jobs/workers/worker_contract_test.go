package workers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"
)

type mockStaleAgentRecoverer struct {
	recovered int
	err       error

	calls      int
	staleAfter time.Duration
}

func (m *mockStaleAgentRecoverer) RecoverStaleAgents(_ context.Context, staleAfter time.Duration) (int, error) {
	m.calls++
	m.staleAfter = staleAfter
	if m.err != nil {
		return 0, m.err
	}
	return m.recovered, nil
}

type mockAgentIndexCleaner struct {
	deleted int
	err     error

	calls int
}

func (m *mockAgentIndexCleaner) CleanupAgentIndexes(context.Context) (int, error) {
	m.calls++
	if m.err != nil {
		return 0, m.err
	}
	return m.deleted, nil
}

type mockTokenRefresher struct {
	err error

	calls   int
	tokenID string
}

func (m *mockTokenRefresher) RefreshToken(_ context.Context, tokenID string) error {
	m.calls++
	m.tokenID = tokenID
	return m.err
}

type mockExpiredTokenCleaner struct {
	tokensDeleted   int64
	requestsDeleted int64
	err             error

	calls int
}

func (m *mockExpiredTokenCleaner) CleanupExpiredOAuth(context.Context) (int64, int64, error) {
	m.calls++
	if m.err != nil {
		return 0, 0, m.err
	}
	return m.tokensDeleted, m.requestsDeleted, nil
}

func TestAgentHeartbeatCheckWorkerContract(t *testing.T) {
	validJob := &river.Job[AgentHeartbeatCheckArgs]{Args: AgentHeartbeatCheckArgs{}}

	t.Run("nil_worker", func(t *testing.T) {
		var worker *AgentHeartbeatCheckWorker
		err := worker.Work(context.Background(), validJob)
		if err == nil || err.Error() != "jobs: heartbeat worker is nil" {
			t.Fatalf("Work() error = %v, want nil worker error", err)
		}
	})

	t.Run("nil_job", func(t *testing.T) {
		worker := &AgentHeartbeatCheckWorker{Recoverer: &mockStaleAgentRecoverer{}}
		err := worker.Work(context.Background(), nil)
		if err == nil || err.Error() != "jobs: heartbeat job is required" {
			t.Fatalf("Work() error = %v, want nil job error", err)
		}
	})

	t.Run("nil_recoverer", func(t *testing.T) {
		worker := &AgentHeartbeatCheckWorker{}
		err := worker.Work(context.Background(), validJob)
		if err == nil || err.Error() != "jobs: stale agent recoverer is required" {
			t.Fatalf("Work() error = %v, want recoverer dependency error", err)
		}
	})

	t.Run("default_stale_after", func(t *testing.T) {
		recoverer := &mockStaleAgentRecoverer{recovered: 2}
		worker := &AgentHeartbeatCheckWorker{Recoverer: recoverer}
		if err := worker.Work(context.Background(), validJob); err != nil {
			t.Fatalf("Work() error = %v", err)
		}
		if recoverer.calls != 1 {
			t.Fatalf("RecoverStaleAgents() calls = %d, want 1", recoverer.calls)
		}
		if recoverer.staleAfter != 2*time.Minute {
			t.Fatalf("staleAfter = %v, want 2m", recoverer.staleAfter)
		}
	})

	t.Run("custom_stale_after", func(t *testing.T) {
		recoverer := &mockStaleAgentRecoverer{}
		worker := &AgentHeartbeatCheckWorker{
			Recoverer:  recoverer,
			StaleAfter: 5 * time.Minute,
		}
		if err := worker.Work(context.Background(), validJob); err != nil {
			t.Fatalf("Work() error = %v", err)
		}
		if recoverer.staleAfter != 5*time.Minute {
			t.Fatalf("staleAfter = %v, want 5m", recoverer.staleAfter)
		}
	})

	t.Run("recoverer_error", func(t *testing.T) {
		sentinel := errors.New("store unavailable")
		worker := &AgentHeartbeatCheckWorker{Recoverer: &mockStaleAgentRecoverer{err: sentinel}}
		err := worker.Work(context.Background(), validJob)
		if !errors.Is(err, sentinel) {
			t.Fatalf("Work() error = %v, want wrapped sentinel", err)
		}
		if !strings.Contains(err.Error(), "jobs: recover stale agents:") {
			t.Fatalf("Work() error = %v, want recovery context", err)
		}
	})
}

func TestAgentIndexCleanupWorkerContract(t *testing.T) {
	validJob := &river.Job[AgentIndexCleanupArgs]{Args: AgentIndexCleanupArgs{}}

	t.Run("nil_worker", func(t *testing.T) {
		var worker *AgentIndexCleanupWorker
		err := worker.Work(context.Background(), validJob)
		if err == nil || err.Error() != "jobs: index cleanup worker is nil" {
			t.Fatalf("Work() error = %v, want nil worker error", err)
		}
	})

	t.Run("nil_job", func(t *testing.T) {
		worker := &AgentIndexCleanupWorker{Cleaner: &mockAgentIndexCleaner{}}
		err := worker.Work(context.Background(), nil)
		if err == nil || err.Error() != "jobs: index cleanup job is required" {
			t.Fatalf("Work() error = %v, want nil job error", err)
		}
	})

	t.Run("nil_cleaner", func(t *testing.T) {
		worker := &AgentIndexCleanupWorker{}
		err := worker.Work(context.Background(), validJob)
		if err == nil || err.Error() != "jobs: index cleaner is required" {
			t.Fatalf("Work() error = %v, want cleaner dependency error", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		cleaner := &mockAgentIndexCleaner{deleted: 3}
		worker := &AgentIndexCleanupWorker{Cleaner: cleaner}
		if err := worker.Work(context.Background(), validJob); err != nil {
			t.Fatalf("Work() error = %v", err)
		}
		if cleaner.calls != 1 {
			t.Fatalf("CleanupAgentIndexes() calls = %d, want 1", cleaner.calls)
		}
	})

	t.Run("cleaner_error", func(t *testing.T) {
		sentinel := errors.New("cleanup failed")
		worker := &AgentIndexCleanupWorker{Cleaner: &mockAgentIndexCleaner{err: sentinel}}
		err := worker.Work(context.Background(), validJob)
		if !errors.Is(err, sentinel) {
			t.Fatalf("Work() error = %v, want wrapped sentinel", err)
		}
		if !strings.Contains(err.Error(), "jobs: cleanup agent indexes:") {
			t.Fatalf("Work() error = %v, want cleanup context", err)
		}
	})
}

func TestOAuthRefreshWorkerContract(t *testing.T) {
	validJob := &river.Job[OAuthRefreshArgs]{
		Args: OAuthRefreshArgs{
			TokenID:  " token-123 ",
			TenantID: " tenant-1 ",
			Subject:  " subject-1 ",
			Provider: " provider-1 ",
		},
	}

	t.Run("nil_worker", func(t *testing.T) {
		var worker *OAuthRefreshWorker
		err := worker.Work(context.Background(), validJob)
		if err == nil || err.Error() != "jobs: oauth refresh worker is nil" {
			t.Fatalf("Work() error = %v, want nil worker error", err)
		}
	})

	t.Run("nil_job", func(t *testing.T) {
		worker := &OAuthRefreshWorker{Refresher: &mockTokenRefresher{}}
		err := worker.Work(context.Background(), nil)
		if err == nil || err.Error() != "jobs: oauth refresh job is required" {
			t.Fatalf("Work() error = %v, want nil job error", err)
		}
	})

	t.Run("nil_refresher", func(t *testing.T) {
		worker := &OAuthRefreshWorker{}
		err := worker.Work(context.Background(), validJob)
		if err == nil || err.Error() != "jobs: token refresher is required" {
			t.Fatalf("Work() error = %v, want refresher dependency error", err)
		}
	})

	t.Run("empty_token_id", func(t *testing.T) {
		refresher := &mockTokenRefresher{}
		worker := &OAuthRefreshWorker{Refresher: refresher}
		err := worker.Work(context.Background(), &river.Job[OAuthRefreshArgs]{
			Args: OAuthRefreshArgs{TokenID: "  "},
		})
		if err == nil || err.Error() != "jobs: token_id is required" {
			t.Fatalf("Work() error = %v, want token_id validation error", err)
		}
		if refresher.calls != 0 {
			t.Fatalf("RefreshToken() calls = %d, want 0", refresher.calls)
		}
	})

	t.Run("success_trims_token_id", func(t *testing.T) {
		refresher := &mockTokenRefresher{}
		worker := &OAuthRefreshWorker{Refresher: refresher}
		if err := worker.Work(context.Background(), validJob); err != nil {
			t.Fatalf("Work() error = %v", err)
		}
		if refresher.calls != 1 {
			t.Fatalf("RefreshToken() calls = %d, want 1", refresher.calls)
		}
		if refresher.tokenID != "token-123" {
			t.Fatalf("tokenID = %q, want token-123", refresher.tokenID)
		}
	})

	t.Run("refresher_error", func(t *testing.T) {
		sentinel := errors.New("refresh failed")
		worker := &OAuthRefreshWorker{Refresher: &mockTokenRefresher{err: sentinel}}
		err := worker.Work(context.Background(), validJob)
		if !errors.Is(err, sentinel) {
			t.Fatalf("Work() error = %v, want wrapped sentinel", err)
		}
		if !strings.Contains(err.Error(), "jobs: refresh oauth token:") {
			t.Fatalf("Work() error = %v, want refresh context", err)
		}
	})
}

func TestOAuthCleanupWorkerContract(t *testing.T) {
	validJob := &river.Job[OAuthCleanupArgs]{Args: OAuthCleanupArgs{}}

	t.Run("nil_worker", func(t *testing.T) {
		var worker *OAuthCleanupWorker
		err := worker.Work(context.Background(), validJob)
		if err == nil || err.Error() != "jobs: oauth cleanup worker is nil" {
			t.Fatalf("Work() error = %v, want nil worker error", err)
		}
	})

	t.Run("nil_job", func(t *testing.T) {
		worker := &OAuthCleanupWorker{Cleaner: &mockExpiredTokenCleaner{}}
		err := worker.Work(context.Background(), nil)
		if err == nil || err.Error() != "jobs: oauth cleanup job is required" {
			t.Fatalf("Work() error = %v, want nil job error", err)
		}
	})

	t.Run("nil_cleaner", func(t *testing.T) {
		worker := &OAuthCleanupWorker{}
		err := worker.Work(context.Background(), validJob)
		if err == nil || err.Error() != "jobs: expired token cleaner is required" {
			t.Fatalf("Work() error = %v, want cleaner dependency error", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		cleaner := &mockExpiredTokenCleaner{tokensDeleted: 2, requestsDeleted: 3}
		worker := &OAuthCleanupWorker{Cleaner: cleaner}
		if err := worker.Work(context.Background(), validJob); err != nil {
			t.Fatalf("Work() error = %v", err)
		}
		if cleaner.calls != 1 {
			t.Fatalf("CleanupExpiredOAuth() calls = %d, want 1", cleaner.calls)
		}
	})

	t.Run("cleaner_error", func(t *testing.T) {
		sentinel := errors.New("cleanup failed")
		worker := &OAuthCleanupWorker{Cleaner: &mockExpiredTokenCleaner{err: sentinel}}
		err := worker.Work(context.Background(), validJob)
		if !errors.Is(err, sentinel) {
			t.Fatalf("Work() error = %v, want wrapped sentinel", err)
		}
		if !strings.Contains(err.Error(), "jobs: cleanup expired oauth records:") {
			t.Fatalf("Work() error = %v, want cleanup context", err)
		}
	})
}
