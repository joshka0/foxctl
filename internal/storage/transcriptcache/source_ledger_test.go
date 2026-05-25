package transcriptcache

import (
	"context"
	"testing"
	"time"
)

func TestSourceLedgerUpsertDiscoveredSourceIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := openSourceLedgerTestStore(t, ctx)

	discovery := sourceDiscovery("codex", "/tmp/session.jsonl")
	first, err := store.UpsertDiscoveredSource(ctx, discovery)
	if err != nil {
		t.Fatalf("UpsertDiscoveredSource() error = %v", err)
	}
	second, err := store.UpsertDiscoveredSource(ctx, discovery)
	if err != nil {
		t.Fatalf("second UpsertDiscoveredSource() error = %v", err)
	}

	if first.Provider != second.Provider || first.SourcePath != second.SourcePath {
		t.Fatalf("rediscovered source identity changed: first=%+v second=%+v", first, second)
	}
	if second.State != SourceStateDiscovered {
		t.Fatalf("state=%q want %q", second.State, SourceStateDiscovered)
	}

	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM transcript_source_ledger`).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count=%d want 1", count)
	}
}

func TestSourceLedgerStateTransitions(t *testing.T) {
	ctx := context.Background()
	store := openSourceLedgerTestStore(t, ctx)
	discovery := sourceDiscovery("codex", "/tmp/session.jsonl")
	if _, err := store.UpsertDiscoveredSource(ctx, discovery); err != nil {
		t.Fatalf("UpsertDiscoveredSource() error = %v", err)
	}
	id := SourceIdentity{Provider: discovery.Provider, SourcePath: discovery.SourcePath}

	queued, err := store.MarkSourceQueued(ctx, id)
	if err != nil {
		t.Fatalf("MarkSourceQueued() error = %v", err)
	}
	if queued.State != SourceStateQueued || queued.QueuedAt == nil {
		t.Fatalf("queued record=%+v, want queued state and queued_at", queued)
	}

	processing, err := store.MarkSourceProcessing(ctx, id)
	if err != nil {
		t.Fatalf("MarkSourceProcessing() error = %v", err)
	}
	if processing.State != SourceStateProcessing || processing.ProcessingAt == nil {
		t.Fatalf("processing record=%+v, want processing state and processing_at", processing)
	}

	processed, err := store.MarkSourceProcessed(ctx, id)
	if err != nil {
		t.Fatalf("MarkSourceProcessed() error = %v", err)
	}
	if processed.State != SourceStateProcessed || processed.ProcessedAt == nil {
		t.Fatalf("processed record=%+v, want processed state and processed_at", processed)
	}
	if processed.LastError != "" {
		t.Fatalf("last error=%q want empty after processed", processed.LastError)
	}
}

func TestSourceLedgerFailedAttemptsAreBoundedAndExcludedFromWork(t *testing.T) {
	ctx := context.Background()
	store := openSourceLedgerTestStore(t, ctx)
	discovery := sourceDiscovery("codex", "/tmp/retry.jsonl")
	discovery.MaxAttempts = 2
	if _, err := store.UpsertDiscoveredSource(ctx, discovery); err != nil {
		t.Fatalf("UpsertDiscoveredSource() error = %v", err)
	}
	id := SourceIdentity{Provider: discovery.Provider, SourcePath: discovery.SourcePath}

	if _, err := store.MarkSourceProcessing(ctx, id); err != nil {
		t.Fatalf("MarkSourceProcessing(first) error = %v", err)
	}
	first, err := store.MarkSourceFailed(ctx, id, "first failure")
	if err != nil {
		t.Fatalf("MarkSourceFailed(first) error = %v", err)
	}
	if first.Attempts != 1 || first.MaxAttempts != 2 || first.State != SourceStateFailed {
		t.Fatalf("first failure=%+v, want attempts 1/2 failed", first)
	}
	if first.LastError != "first failure" || first.FailedAt == nil {
		t.Fatalf("first failure metadata=%+v, want error and failed_at", first)
	}

	if _, err := store.MarkSourceProcessing(ctx, id); err != nil {
		t.Fatalf("MarkSourceProcessing(second) error = %v", err)
	}
	second, err := store.MarkSourceFailed(ctx, id, "second failure")
	if err != nil {
		t.Fatalf("MarkSourceFailed(second) error = %v", err)
	}
	if second.Attempts != 2 {
		t.Fatalf("attempts second=%d want capped at 2", second.Attempts)
	}

	if _, err := store.MarkSourceProcessing(ctx, id); err == nil {
		t.Fatal("MarkSourceProcessing accepted exhausted failed source")
	}
	if _, err := store.MarkSourceQueued(ctx, id); err == nil {
		t.Fatal("MarkSourceQueued accepted exhausted failed source")
	}
	if _, err := store.MarkSourceFailed(ctx, id, "third failure"); err == nil {
		t.Fatal("MarkSourceFailed accepted non-processing exhausted source")
	}
	exhausted, err := store.GetSource(ctx, id)
	if err != nil {
		t.Fatalf("GetSource(exhausted) error = %v", err)
	}
	if exhausted.Attempts != 2 || exhausted.State != SourceStateFailed {
		t.Fatalf("exhausted source=%+v, want failed with attempts 2", exhausted)
	}

	got, err := store.ListSourcesNeedingWork(ctx, SourceWorkOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListSourcesNeedingWork() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("work candidates=%+v want none after retry bound", got)
	}
}

func TestSourceLedgerFailureRequiresProcessingState(t *testing.T) {
	ctx := context.Background()
	store := openSourceLedgerTestStore(t, ctx)
	discovery := sourceDiscovery("codex", "/tmp/failure-precondition.jsonl")
	if _, err := store.UpsertDiscoveredSource(ctx, discovery); err != nil {
		t.Fatalf("UpsertDiscoveredSource() error = %v", err)
	}
	id := SourceIdentity{Provider: discovery.Provider, SourcePath: discovery.SourcePath}

	if _, err := store.MarkSourceFailed(ctx, id, "not running"); err == nil {
		t.Fatal("MarkSourceFailed accepted discovered source")
	}
	got, err := store.GetSource(ctx, id)
	if err != nil {
		t.Fatalf("GetSource() error = %v", err)
	}
	if got.State != SourceStateDiscovered || got.Attempts != 0 || got.FailedAt != nil {
		t.Fatalf("source after rejected discovered failure=%+v", got)
	}

	if _, err := store.MarkSourceQueued(ctx, id); err != nil {
		t.Fatalf("MarkSourceQueued() error = %v", err)
	}
	if _, err := store.MarkSourceFailed(ctx, id, "not running"); err == nil {
		t.Fatal("MarkSourceFailed accepted queued source")
	}
	got, err = store.GetSource(ctx, id)
	if err != nil {
		t.Fatalf("GetSource(queued) error = %v", err)
	}
	if got.State != SourceStateQueued || got.Attempts != 0 || got.FailedAt != nil {
		t.Fatalf("source after rejected queued failure=%+v", got)
	}
}

func TestSourceLedgerProcessedSourcesAreTerminal(t *testing.T) {
	ctx := context.Background()
	store := openSourceLedgerTestStore(t, ctx)
	discovery := sourceDiscovery("codex", "/tmp/terminal.jsonl")
	if _, err := store.UpsertDiscoveredSource(ctx, discovery); err != nil {
		t.Fatalf("UpsertDiscoveredSource() error = %v", err)
	}
	id := SourceIdentity{Provider: discovery.Provider, SourcePath: discovery.SourcePath}
	if _, err := store.MarkSourceProcessing(ctx, id); err != nil {
		t.Fatalf("MarkSourceProcessing() error = %v", err)
	}
	if _, err := store.MarkSourceProcessed(ctx, id); err != nil {
		t.Fatalf("MarkSourceProcessed() error = %v", err)
	}

	reopenAttempts := []struct {
		name string
		run  func() error
	}{
		{
			name: "queue",
			run: func() error {
				_, err := store.MarkSourceQueued(ctx, id)
				return err
			},
		},
		{
			name: "processing",
			run: func() error {
				_, err := store.MarkSourceProcessing(ctx, id)
				return err
			},
		},
		{
			name: "failed",
			run: func() error {
				_, err := store.MarkSourceFailed(ctx, id, "late failure")
				return err
			},
		},
	}

	for _, attempt := range reopenAttempts {
		t.Run(attempt.name, func(t *testing.T) {
			if err := attempt.run(); err == nil {
				t.Fatalf("processed source was reopened as %s", attempt.name)
			}
			got, err := store.GetSource(ctx, id)
			if err != nil {
				t.Fatalf("GetSource() error = %v", err)
			}
			if got.State != SourceStateProcessed {
				t.Fatalf("state=%q want %q after rejected %s", got.State, SourceStateProcessed, attempt.name)
			}
			if got.Attempts != 0 {
				t.Fatalf("attempts=%d want 0 after rejected %s", got.Attempts, attempt.name)
			}
			if got.LastError != "" {
				t.Fatalf("last_error=%q want empty after rejected %s", got.LastError, attempt.name)
			}
		})
	}
}

func TestSourceLedgerRediscoveryDoesNotReopenProcessedSource(t *testing.T) {
	ctx := context.Background()
	store := openSourceLedgerTestStore(t, ctx)
	discovery := sourceDiscovery("codex", "/tmp/rediscovered-terminal.jsonl")
	if _, err := store.UpsertDiscoveredSource(ctx, discovery); err != nil {
		t.Fatalf("UpsertDiscoveredSource() error = %v", err)
	}
	id := SourceIdentity{Provider: discovery.Provider, SourcePath: discovery.SourcePath}
	if _, err := store.MarkSourceProcessing(ctx, id); err != nil {
		t.Fatalf("MarkSourceProcessing() error = %v", err)
	}
	processed, err := store.MarkSourceProcessed(ctx, id)
	if err != nil {
		t.Fatalf("MarkSourceProcessed() error = %v", err)
	}
	if processed.ProcessedAt == nil {
		t.Fatal("processed source missing processed_at")
	}

	rediscovered := discovery
	rediscovered.SessionID = "session-rediscovered"
	rediscovered.SourceDigest = DigestText("updated source contents")
	rediscovered.SizeBytes = discovery.SizeBytes + 100
	got, err := store.UpsertDiscoveredSource(ctx, rediscovered)
	if err != nil {
		t.Fatalf("rediscover processed source: %v", err)
	}
	if got.State != SourceStateProcessed {
		t.Fatalf("rediscovery reopened processed source: state=%q want %q", got.State, SourceStateProcessed)
	}
	if got.ProcessedAt == nil || !got.ProcessedAt.Equal(*processed.ProcessedAt) {
		t.Fatalf("processed_at changed after rediscovery: got=%v want=%v", got.ProcessedAt, processed.ProcessedAt)
	}
	if got.SourceDigest != rediscovered.SourceDigest || got.SessionID != rediscovered.SessionID || got.SizeBytes != rediscovered.SizeBytes {
		t.Fatalf("rediscovery did not refresh metadata: %+v", got)
	}

	work, err := store.ListSourcesNeedingWork(ctx, SourceWorkOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListSourcesNeedingWork() error = %v", err)
	}
	if len(work) != 0 {
		t.Fatalf("rediscovered processed source became work candidate: %+v", work)
	}
}

func TestSourceLedgerProcessedRequiresProcessingState(t *testing.T) {
	ctx := context.Background()
	store := openSourceLedgerTestStore(t, ctx)
	discovery := sourceDiscovery("codex", "/tmp/processed-precondition.jsonl")
	if _, err := store.UpsertDiscoveredSource(ctx, discovery); err != nil {
		t.Fatalf("UpsertDiscoveredSource() error = %v", err)
	}
	id := SourceIdentity{Provider: discovery.Provider, SourcePath: discovery.SourcePath}

	if _, err := store.MarkSourceProcessed(ctx, id); err == nil {
		t.Fatal("MarkSourceProcessed accepted discovered source")
	}
	got, err := store.GetSource(ctx, id)
	if err != nil {
		t.Fatalf("GetSource() error = %v", err)
	}
	if got.State != SourceStateDiscovered {
		t.Fatalf("state=%q want %q after rejected processed transition", got.State, SourceStateDiscovered)
	}
	if got.ProcessedAt != nil {
		t.Fatalf("processed_at=%v want nil after rejected processed transition", got.ProcessedAt)
	}

	if _, err := store.MarkSourceQueued(ctx, id); err != nil {
		t.Fatalf("MarkSourceQueued() error = %v", err)
	}
	if _, err := store.MarkSourceProcessed(ctx, id); err == nil {
		t.Fatal("MarkSourceProcessed accepted queued source")
	}
	got, err = store.GetSource(ctx, id)
	if err != nil {
		t.Fatalf("GetSource() after queued rejection error = %v", err)
	}
	if got.State != SourceStateQueued {
		t.Fatalf("state=%q want %q after rejected queued->processed transition", got.State, SourceStateQueued)
	}
	if got.ProcessedAt != nil {
		t.Fatalf("processed_at=%v want nil after rejected queued->processed transition", got.ProcessedAt)
	}
}

func TestSourceLedgerProcessingSourceCannotBeRequeued(t *testing.T) {
	ctx := context.Background()
	store := openSourceLedgerTestStore(t, ctx)
	discovery := sourceDiscovery("codex", "/tmp/processing-requeue.jsonl")
	if _, err := store.UpsertDiscoveredSource(ctx, discovery); err != nil {
		t.Fatalf("UpsertDiscoveredSource() error = %v", err)
	}
	id := SourceIdentity{Provider: discovery.Provider, SourcePath: discovery.SourcePath}
	processing, err := store.MarkSourceProcessing(ctx, id)
	if err != nil {
		t.Fatalf("MarkSourceProcessing() error = %v", err)
	}
	if processing.State != SourceStateProcessing || processing.ProcessingAt == nil {
		t.Fatalf("processing record=%+v, want processing state and processing_at", processing)
	}

	if _, err := store.MarkSourceQueued(ctx, id); err == nil {
		t.Fatal("MarkSourceQueued reopened processing source")
	}
	got, err := store.GetSource(ctx, id)
	if err != nil {
		t.Fatalf("GetSource() error = %v", err)
	}
	if got.State != SourceStateProcessing {
		t.Fatalf("state=%q want %q after rejected requeue", got.State, SourceStateProcessing)
	}
	if got.ProcessingAt == nil || !got.ProcessingAt.Equal(*processing.ProcessingAt) {
		t.Fatalf("processing_at=%v want preserved %v", got.ProcessingAt, processing.ProcessingAt)
	}
}

func TestSourceLedgerListSourcesNeedingWorkDeterministicOrdering(t *testing.T) {
	ctx := context.Background()
	store := openSourceLedgerTestStore(t, ctx)
	for _, discovery := range []SourceDiscovery{
		sourceDiscovery("codex", "/tmp/discovered-b.jsonl"),
		sourceDiscovery("codex", "/tmp/queued-b.jsonl"),
		sourceDiscovery("claude", "/tmp/queued-a.jsonl"),
		sourceDiscovery("codex", "/tmp/failed-c.jsonl"),
		sourceDiscovery("codex", "/tmp/processed-d.jsonl"),
		sourceDiscovery("codex", "/tmp/processing-e.jsonl"),
	} {
		if _, err := store.UpsertDiscoveredSource(ctx, discovery); err != nil {
			t.Fatalf("UpsertDiscoveredSource(%q) error = %v", discovery.SourcePath, err)
		}
	}

	mustMarkQueued := func(provider, path string) {
		t.Helper()
		if _, err := store.MarkSourceQueued(ctx, SourceIdentity{Provider: provider, SourcePath: path}); err != nil {
			t.Fatalf("MarkSourceQueued(%q) error = %v", path, err)
		}
	}
	mustMarkQueued("codex", "/tmp/queued-b.jsonl")
	mustMarkQueued("claude", "/tmp/queued-a.jsonl")
	if _, err := store.MarkSourceProcessing(ctx, SourceIdentity{Provider: "codex", SourcePath: "/tmp/failed-c.jsonl"}); err != nil {
		t.Fatalf("MarkSourceProcessing(failed-c) error = %v", err)
	}
	if _, err := store.MarkSourceFailed(ctx, SourceIdentity{Provider: "codex", SourcePath: "/tmp/failed-c.jsonl"}, "retryable"); err != nil {
		t.Fatalf("MarkSourceFailed() error = %v", err)
	}
	if _, err := store.MarkSourceProcessing(ctx, SourceIdentity{Provider: "codex", SourcePath: "/tmp/processed-d.jsonl"}); err != nil {
		t.Fatalf("MarkSourceProcessing(processed-d) error = %v", err)
	}
	if _, err := store.MarkSourceProcessed(ctx, SourceIdentity{Provider: "codex", SourcePath: "/tmp/processed-d.jsonl"}); err != nil {
		t.Fatalf("MarkSourceProcessed() error = %v", err)
	}
	if _, err := store.MarkSourceProcessing(ctx, SourceIdentity{Provider: "codex", SourcePath: "/tmp/processing-e.jsonl"}); err != nil {
		t.Fatalf("MarkSourceProcessing() error = %v", err)
	}

	got, err := store.ListSourcesNeedingWork(ctx, SourceWorkOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListSourcesNeedingWork() error = %v", err)
	}
	gotKeys := make([]string, 0, len(got))
	for _, record := range got {
		gotKeys = append(gotKeys, string(record.State)+" "+record.Provider+" "+record.SourcePath)
	}
	want := []string{
		"queued claude /tmp/queued-a.jsonl",
		"queued codex /tmp/queued-b.jsonl",
		"discovered codex /tmp/discovered-b.jsonl",
		"failed codex /tmp/failed-c.jsonl",
	}
	if len(gotKeys) != len(want) {
		t.Fatalf("keys=%v want %v", gotKeys, want)
	}
	for i := range want {
		if gotKeys[i] != want[i] {
			t.Fatalf("keys=%v want %v", gotKeys, want)
		}
	}
}

func openSourceLedgerTestStore(t *testing.T, ctx context.Context) *Store {
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

func sourceDiscovery(provider, path string) SourceDiscovery {
	return SourceDiscovery{
		Provider:      provider,
		SourcePath:    path,
		SessionID:     "session-" + provider,
		WorkspacePath: "/workspace/project",
		SizeBytes:     42,
		ModTime:       time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
		SourceDigest:  DigestText(provider + ":" + path),
		MaxAttempts:   DefaultSourceMaxAttempts,
	}
}
