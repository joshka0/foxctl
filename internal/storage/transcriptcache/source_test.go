package transcriptcache

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSourceLedgerRediscoveryIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := openSourceTestStore(t, ctx)

	discovery := testSourceDiscovery("codex", "/tmp/session-a.jsonl", "fingerprint-a")
	first, err := store.UpsertDiscoveredSource(ctx, discovery)
	if err != nil {
		t.Fatalf("UpsertDiscoveredSource() error = %v", err)
	}
	if first.State != SourceStateDiscovered {
		t.Fatalf("state=%q want %q", first.State, SourceStateDiscovered)
	}
	markSourceReadyForProcessing(t, ctx, store, discovery)
	if err := store.MarkSourceQueued(ctx, discovery.Provider, discovery.SourcePath); err == nil {
		t.Fatal("MarkSourceQueued() on processing source succeeded")
	}
	if err := store.MarkSourceProcessed(ctx, discovery.Provider, discovery.SourcePath); err != nil {
		t.Fatalf("MarkSourceProcessed() error = %v", err)
	}

	rediscovered, err := store.UpsertDiscoveredSource(ctx, discovery)
	if err != nil {
		t.Fatalf("rediscover same source error = %v", err)
	}
	if rediscovered.State != SourceStateProcessed {
		t.Fatalf("same-fingerprint rediscovery state=%q want processed", rediscovered.State)
	}

	changed := discovery
	changed.Fingerprint = "fingerprint-b"
	changed.SourceSize = 2048
	reset, err := store.UpsertDiscoveredSource(ctx, changed)
	if err != nil {
		t.Fatalf("rediscover changed source error = %v", err)
	}
	if reset.State != SourceStateDiscovered {
		t.Fatalf("changed-fingerprint state=%q want discovered", reset.State)
	}
	if reset.Attempts != 0 {
		t.Fatalf("changed-fingerprint attempts=%d want 0", reset.Attempts)
	}
	if reset.ProcessedAt != nil {
		t.Fatalf("changed-fingerprint processed_at=%v want nil", reset.ProcessedAt)
	}
}

func TestSourceLedgerStateTransitions(t *testing.T) {
	ctx := context.Background()
	store := openSourceTestStore(t, ctx)
	discovery := testSourceDiscovery("claude", "/tmp/session-b.jsonl", "fingerprint-b")
	if _, err := store.UpsertDiscoveredSource(ctx, discovery); err != nil {
		t.Fatalf("UpsertDiscoveredSource() error = %v", err)
	}

	if err := store.MarkSourceQueued(ctx, discovery.Provider, discovery.SourcePath); err != nil {
		t.Fatalf("MarkSourceQueued() error = %v", err)
	}
	assertSourceState(t, ctx, store, discovery, SourceStateQueued)

	if err := store.MarkSourceProcessing(ctx, discovery.Provider, discovery.SourcePath); err != nil {
		t.Fatalf("MarkSourceProcessing() error = %v", err)
	}
	assertSourceState(t, ctx, store, discovery, SourceStateProcessing)

	if err := store.MarkSourceProcessed(ctx, discovery.Provider, discovery.SourcePath); err != nil {
		t.Fatalf("MarkSourceProcessed() error = %v", err)
	}
	processed := assertSourceState(t, ctx, store, discovery, SourceStateProcessed)
	if processed.ProcessedAt == nil {
		t.Fatal("processed_at is nil")
	}
}

func TestSourceLedgerFailedRetryBounds(t *testing.T) {
	ctx := context.Background()
	store := openSourceTestStore(t, ctx)
	discovery := testSourceDiscovery("codex", "/tmp/session-c.jsonl", "fingerprint-c")
	discovery.MaxAttempts = 2
	if _, err := store.UpsertDiscoveredSource(ctx, discovery); err != nil {
		t.Fatalf("UpsertDiscoveredSource() error = %v", err)
	}
	markSourceReadyForProcessing(t, ctx, store, discovery)
	failureNow := time.Now().UTC().Add(-2 * time.Hour)

	failed, err := store.MarkSourceFailed(ctx, SourceFailure{
		Provider:    discovery.Provider,
		SourcePath:  discovery.SourcePath,
		Error:       "temporary parse failure",
		RetryAfter:  time.Hour,
		MaxAttempts: 2,
		Now:         failureNow,
	})
	if err != nil {
		t.Fatalf("first MarkSourceFailed() error = %v", err)
	}
	if failed.Attempts != 1 {
		t.Fatalf("attempts=%d want 1", failed.Attempts)
	}
	if failed.NextAttemptAt == nil {
		t.Fatal("next_attempt_at is nil before retry exhaustion")
	}

	beforeRetry, err := store.ListSourceCandidates(ctx, ListSourceCandidatesOptions{Now: failed.NextAttemptAt.Add(-time.Second)})
	if err != nil {
		t.Fatalf("ListSourceCandidates() before retry error = %v", err)
	}
	if len(beforeRetry) != 0 {
		t.Fatalf("candidates before retry=%d want 0", len(beforeRetry))
	}

	afterRetry, err := store.ListSourceCandidates(ctx, ListSourceCandidatesOptions{Now: failed.NextAttemptAt.Add(time.Second)})
	if err != nil {
		t.Fatalf("ListSourceCandidates() after retry error = %v", err)
	}
	if len(afterRetry) != 1 {
		t.Fatalf("candidates after retry=%d want 1", len(afterRetry))
	}
	if err := store.MarkSourceProcessing(ctx, discovery.Provider, discovery.SourcePath); err != nil {
		t.Fatalf("retry MarkSourceProcessing() error = %v", err)
	}

	exhausted, err := store.MarkSourceFailed(ctx, SourceFailure{
		Provider:    discovery.Provider,
		SourcePath:  discovery.SourcePath,
		Error:       "permanent parse failure",
		RetryAfter:  time.Hour,
		MaxAttempts: 2,
		Now:         failureNow.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("second MarkSourceFailed() error = %v", err)
	}
	if exhausted.Attempts != 2 {
		t.Fatalf("attempts=%d want 2", exhausted.Attempts)
	}
	if exhausted.NextAttemptAt != nil {
		t.Fatalf("next_attempt_at=%v want nil after retry exhaustion", exhausted.NextAttemptAt)
	}

	if err := store.MarkSourceProcessing(ctx, discovery.Provider, discovery.SourcePath); err == nil {
		t.Fatal("MarkSourceProcessing() succeeded after retry exhaustion")
	}

	candidates, err := store.ListSourceCandidates(ctx, ListSourceCandidatesOptions{Now: time.Now().Add(24 * time.Hour)})
	if err != nil {
		t.Fatalf("ListSourceCandidates() exhausted error = %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates after exhaustion=%d want 0", len(candidates))
	}
}

func TestSourceLedgerResetsStaleProcessingForBoundedRetry(t *testing.T) {
	ctx := context.Background()
	store := openSourceTestStore(t, ctx)
	discovery := testSourceDiscovery("codex", "/tmp/session-stale.jsonl", "fingerprint-stale")
	discovery.MaxAttempts = 2
	if _, err := store.UpsertDiscoveredSource(ctx, discovery); err != nil {
		t.Fatalf("UpsertDiscoveredSource() error = %v", err)
	}
	markSourceReadyForProcessing(t, ctx, store, discovery)

	resetAt := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	reset, err := store.ResetStaleProcessingSources(ctx, ResetStaleProcessingOptions{
		Before: time.Now().Add(time.Second),
		Now:    resetAt,
		Error:  "worker crashed",
	})
	if err != nil {
		t.Fatalf("ResetStaleProcessingSources() error = %v", err)
	}
	if reset != 1 {
		t.Fatalf("reset=%d want 1", reset)
	}
	record := assertSourceState(t, ctx, store, discovery, SourceStateFailed)
	if record.Attempts != 1 || record.LastError != "worker crashed" {
		t.Fatalf("record=%+v", record)
	}
	if record.FailedAt == nil || !record.FailedAt.Equal(resetAt) {
		t.Fatalf("failed_at=%v want %v", record.FailedAt, resetAt)
	}
	candidates, err := store.ListSourceCandidates(ctx, ListSourceCandidatesOptions{Now: resetAt})
	if err != nil {
		t.Fatalf("ListSourceCandidates() after reset error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates=%d want 1 after stale reset", len(candidates))
	}

	if err := store.MarkSourceProcessing(ctx, discovery.Provider, discovery.SourcePath); err != nil {
		t.Fatalf("retry MarkSourceProcessing() error = %v", err)
	}
	reset, err = store.ResetStaleProcessingSources(ctx, ResetStaleProcessingOptions{
		Before: time.Now().Add(time.Second),
		Now:    resetAt.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("second ResetStaleProcessingSources() error = %v", err)
	}
	if reset != 1 {
		t.Fatalf("second reset=%d want 1", reset)
	}
	exhausted := assertSourceState(t, ctx, store, discovery, SourceStateFailed)
	if exhausted.Attempts != 2 {
		t.Fatalf("attempts=%d want 2", exhausted.Attempts)
	}
	candidates, err = store.ListSourceCandidates(ctx, ListSourceCandidatesOptions{Now: resetAt.Add(time.Hour)})
	if err != nil {
		t.Fatalf("ListSourceCandidates() after exhaustion error = %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates=%d want 0 after stale reset exhaustion", len(candidates))
	}
}

func TestSourceLedgerCandidateOrderingIsDeterministic(t *testing.T) {
	ctx := context.Background()
	store := openSourceTestStore(t, ctx)

	for _, discovery := range []SourceDiscovery{
		testSourceDiscovery("codex", "/tmp/b.jsonl", "fingerprint-b"),
		testSourceDiscovery("claude", "/tmp/a.jsonl", "fingerprint-a"),
		testSourceDiscovery("codex", "/tmp/a.jsonl", "fingerprint-c"),
	} {
		if _, err := store.UpsertDiscoveredSource(ctx, discovery); err != nil {
			t.Fatalf("UpsertDiscoveredSource(%s, %s) error = %v", discovery.Provider, discovery.SourcePath, err)
		}
		if err := store.MarkSourceQueued(ctx, discovery.Provider, discovery.SourcePath); err != nil {
			t.Fatalf("MarkSourceQueued(%s, %s) error = %v", discovery.Provider, discovery.SourcePath, err)
		}
	}

	candidates, err := store.ListSourceCandidates(ctx, ListSourceCandidatesOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListSourceCandidates() error = %v", err)
	}
	if len(candidates) != 3 {
		t.Fatalf("candidates=%d want 3", len(candidates))
	}
	got := []string{
		candidates[0].Provider + ":" + candidates[0].SourcePath,
		candidates[1].Provider + ":" + candidates[1].SourcePath,
		candidates[2].Provider + ":" + candidates[2].SourcePath,
	}
	want := []string{
		"claude:/tmp/a.jsonl",
		"codex:/tmp/a.jsonl",
		"codex:/tmp/b.jsonl",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate order=%v want %v", got, want)
		}
	}
}

func TestSourceLedgerDiscoveredSourcesAreNotCandidatesUntilQueued(t *testing.T) {
	ctx := context.Background()
	store := openSourceTestStore(t, ctx)
	discovery := testSourceDiscovery("codex", "/tmp/session-unqueued.jsonl", "fingerprint-unqueued")
	if _, err := store.UpsertDiscoveredSource(ctx, discovery); err != nil {
		t.Fatalf("UpsertDiscoveredSource() error = %v", err)
	}
	candidates, err := store.ListSourceCandidates(ctx, ListSourceCandidatesOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListSourceCandidates() error = %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates=%d want 0 before queued transition", len(candidates))
	}
}

func TestSourceLedgerRejectsInvalidTransitions(t *testing.T) {
	ctx := context.Background()
	store := openSourceTestStore(t, ctx)
	discovery := testSourceDiscovery("codex", "/tmp/session-transition.jsonl", "fingerprint-transition")
	if _, err := store.UpsertDiscoveredSource(ctx, discovery); err != nil {
		t.Fatalf("UpsertDiscoveredSource() error = %v", err)
	}
	if err := store.MarkSourceProcessed(ctx, discovery.Provider, discovery.SourcePath); err == nil {
		t.Fatal("MarkSourceProcessed() on discovered source succeeded")
	}
	if _, err := store.MarkSourceFailed(ctx, SourceFailure{Provider: discovery.Provider, SourcePath: discovery.SourcePath, Error: "not processing"}); err == nil {
		t.Fatal("MarkSourceFailed() on discovered source succeeded")
	}

	markSourceReadyForProcessing(t, ctx, store, discovery)
	if err := store.MarkSourceProcessed(ctx, discovery.Provider, discovery.SourcePath); err != nil {
		t.Fatalf("MarkSourceProcessed() error = %v", err)
	}
	if err := store.MarkSourceProcessing(ctx, discovery.Provider, discovery.SourcePath); err == nil {
		t.Fatal("MarkSourceProcessing() on processed source succeeded")
	}
}

func TestSourceLedgerProcessingClaimIsSingleWinner(t *testing.T) {
	ctx := context.Background()
	store := openSourceTestStore(t, ctx)
	discovery := testSourceDiscovery("codex", "/tmp/session-concurrent.jsonl", "fingerprint-concurrent")
	if _, err := store.UpsertDiscoveredSource(ctx, discovery); err != nil {
		t.Fatalf("UpsertDiscoveredSource() error = %v", err)
	}
	if err := store.MarkSourceQueued(ctx, discovery.Provider, discovery.SourcePath); err != nil {
		t.Fatalf("MarkSourceQueued() error = %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- store.MarkSourceProcessing(ctx, discovery.Provider, discovery.SourcePath)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("processing claim successes=%d want 1", successes)
	}
	assertSourceState(t, ctx, store, discovery, SourceStateProcessing)
}

func TestSourceLedgerRediscoveryDoesNotReplaceProcessingFingerprint(t *testing.T) {
	ctx := context.Background()
	store := openSourceTestStore(t, ctx)
	discovery := testSourceDiscovery("codex", "/tmp/session-processing.jsonl", "fingerprint-old")
	if _, err := store.UpsertDiscoveredSource(ctx, discovery); err != nil {
		t.Fatalf("UpsertDiscoveredSource() error = %v", err)
	}
	markSourceReadyForProcessing(t, ctx, store, discovery)

	changed := discovery
	changed.Fingerprint = "fingerprint-new"
	changed.SourceSize = 4096
	got, err := store.UpsertDiscoveredSource(ctx, changed)
	if err != nil {
		t.Fatalf("changed UpsertDiscoveredSource() error = %v", err)
	}
	if got.State != SourceStateProcessing {
		t.Fatalf("state=%q want processing", got.State)
	}
	if got.Fingerprint != "fingerprint-old" || got.SourceSize != discovery.SourceSize {
		t.Fatalf("processing identity changed: %+v", got)
	}
}

func openSourceTestStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()
	store, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return store
}

func markSourceReadyForProcessing(t *testing.T, ctx context.Context, store *Store, discovery SourceDiscovery) {
	t.Helper()
	if err := store.MarkSourceQueued(ctx, discovery.Provider, discovery.SourcePath); err != nil {
		t.Fatalf("MarkSourceQueued() error = %v", err)
	}
	if err := store.MarkSourceProcessing(ctx, discovery.Provider, discovery.SourcePath); err != nil {
		t.Fatalf("MarkSourceProcessing() error = %v", err)
	}
}

func testSourceDiscovery(provider, sourcePath, fingerprint string) SourceDiscovery {
	return SourceDiscovery{
		Provider:      provider,
		SourcePath:    sourcePath,
		SessionID:     "session-1",
		WorkspaceHint: "/tmp/workspace",
		SourceSize:    1024,
		SourceMTime:   time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
		Fingerprint:   fingerprint,
	}
}

func assertSourceState(t *testing.T, ctx context.Context, store *Store, discovery SourceDiscovery, want SourceState) SourceRecord {
	t.Helper()
	got, err := store.GetSource(ctx, discovery.Provider, discovery.SourcePath)
	if err != nil {
		t.Fatalf("GetSource() error = %v", err)
	}
	if got.State != want {
		t.Fatalf("state=%q want %q", got.State, want)
	}
	return got
}
