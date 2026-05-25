package dreamer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/transcriptpipeline"
	"github.com/joshka0/foxctl/internal/storage/transcriptcache"
)

func TestSourceScannerAdaptsStableDiscoveryCandidates(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sessions", "2026", "05", "25", "dream-session.jsonl")
	writeAdapterTestFile(t, path, "{}\n")
	scanner := SourceScanner{Roots: []transcriptpipeline.DreamSourceRoot{{
		Provider:      transcriptpipeline.DreamSourceProviderCodex,
		RootPath:      root,
		WorkspaceHint: "/repo",
	}}}

	got, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("sources=%d want 1", len(got))
	}
	if !got[0].Stable || got[0].Provider != "codex" || got[0].Path != path || got[0].WorkspacePath != "/repo" {
		t.Fatalf("source=%+v", got[0])
	}
}

func TestSourceScannerHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scanner := SourceScanner{Roots: []transcriptpipeline.DreamSourceRoot{{
		Provider: transcriptpipeline.DreamSourceProviderCodex,
		RootPath: t.TempDir(),
	}}}

	if _, err := scanner.Scan(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Scan() error=%v want context.Canceled", err)
	}
}

func TestSourceLedgerAdaptsTranscriptCacheStore(t *testing.T) {
	ctx := context.Background()
	store, err := transcriptcache.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ledger := SourceLedger{Store: store, MaxAttempts: 2, FailureDelay: time.Hour}
	source := Source{
		Provider:      "codex",
		Path:          "/tmp/dream.jsonl",
		SessionID:     "dream",
		WorkspacePath: "/repo",
		Fingerprint:   "sha256:dream",
		Size:          10,
		ModTime:       time.Date(2026, 5, 25, 1, 0, 0, 0, time.UTC),
		Stable:        true,
	}

	if err := ledger.UpsertDiscovered(ctx, source); err != nil {
		t.Fatalf("UpsertDiscovered() error = %v", err)
	}
	candidates, err := ledger.ListCandidates(ctx, 10)
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].Path != source.Path {
		t.Fatalf("candidates=%+v", candidates)
	}
	if err := ledger.MarkProcessing(ctx, source); err != nil {
		t.Fatalf("MarkProcessing() error = %v", err)
	}
	if err := ledger.MarkFailed(ctx, source, assertErr("derive failed")); err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}
	record, err := store.GetSource(ctx, source.Provider, source.Path)
	if err != nil {
		t.Fatalf("GetSource() error = %v", err)
	}
	if record.State != transcriptcache.SourceStateFailed || record.Attempts != 1 || record.NextAttemptAt == nil {
		t.Fatalf("record=%+v", record)
	}
}

func TestSourceLedgerDoesNotRequeueProcessedSourceWithSameFingerprint(t *testing.T) {
	ctx := context.Background()
	store, err := transcriptcache.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ledger := SourceLedger{Store: store}
	source := Source{
		Provider:    "codex",
		Path:        "/tmp/dream.jsonl",
		Fingerprint: "sha256:same",
		Size:        10,
		ModTime:     time.Date(2026, 5, 25, 1, 0, 0, 0, time.UTC),
		Stable:      true,
	}

	if err := ledger.UpsertDiscovered(ctx, source); err != nil {
		t.Fatalf("UpsertDiscovered() error = %v", err)
	}
	if err := ledger.MarkProcessing(ctx, source); err != nil {
		t.Fatalf("MarkProcessing() error = %v", err)
	}
	if err := ledger.MarkProcessed(ctx, source, ProcessResult{}); err != nil {
		t.Fatalf("MarkProcessed() error = %v", err)
	}
	if err := ledger.UpsertDiscovered(ctx, source); err != nil {
		t.Fatalf("rediscover UpsertDiscovered() error = %v", err)
	}
	record, err := store.GetSource(ctx, source.Provider, source.Path)
	if err != nil {
		t.Fatalf("GetSource() error = %v", err)
	}
	if record.State != transcriptcache.SourceStateProcessed {
		t.Fatalf("state=%q want processed", record.State)
	}
	candidates, err := ledger.ListCandidates(ctx, 10)
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates=%+v want none", candidates)
	}
}

func TestSourceLedgerListsStaleProcessingSourcesForRetry(t *testing.T) {
	ctx := context.Background()
	store, err := transcriptcache.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	ledger := SourceLedger{
		Store:             store,
		MaxAttempts:       2,
		ProcessingTimeout: time.Minute,
		Now:               func() time.Time { return now },
	}
	source := Source{
		Provider:    "codex",
		Path:        "/tmp/stale-dream.jsonl",
		Fingerprint: "sha256:stale",
		Size:        10,
		ModTime:     now.Add(-time.Hour),
		Stable:      true,
	}

	if err := ledger.UpsertDiscovered(ctx, source); err != nil {
		t.Fatalf("UpsertDiscovered() error = %v", err)
	}
	if err := ledger.MarkProcessing(ctx, source); err != nil {
		t.Fatalf("MarkProcessing() error = %v", err)
	}
	now = time.Now().UTC().Add(2 * time.Minute)

	candidates, err := ledger.ListCandidates(ctx, 10)
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].Path != source.Path {
		t.Fatalf("candidates=%+v want stale source", candidates)
	}
	record, err := store.GetSource(ctx, source.Provider, source.Path)
	if err != nil {
		t.Fatalf("GetSource() error = %v", err)
	}
	if record.State != transcriptcache.SourceStateFailed || record.Attempts != 1 {
		t.Fatalf("record=%+v", record)
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

func writeAdapterTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
