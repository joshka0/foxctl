package dreamer

import (
	"context"
	"time"

	"github.com/joshka0/foxctl/internal/context/transcriptpipeline"
	"github.com/joshka0/foxctl/internal/storage/transcriptcache"
)

type SourceScanner struct {
	Roots []transcriptpipeline.DreamSourceRoot
}

func (s SourceScanner) Scan(ctx context.Context) ([]Source, error) {
	candidates, err := transcriptpipeline.DiscoverDreamSourceCandidatesContext(ctx, s.Roots)
	if err != nil {
		return nil, err
	}
	sources := make([]Source, 0, len(candidates))
	for _, candidate := range candidates {
		sources = append(sources, SourceFromCandidate(candidate))
	}
	return sources, nil
}

func SourceFromCandidate(candidate transcriptpipeline.DreamSourceCandidate) Source {
	return Source{
		Provider:      string(candidate.Provider),
		Path:          candidate.Path,
		SessionID:     candidate.SessionID,
		WorkspacePath: firstNonEmpty(candidate.WorkspacePath, candidate.WorkspaceHint),
		Fingerprint:   firstNonEmpty(candidate.Fingerprint, candidate.Digest),
		Size:          candidate.Size,
		ModTime:       candidate.ModTime,
		Stable:        candidate.StabilityStatus == transcriptpipeline.DreamSourceStable,
	}
}

type SourceLedger struct {
	Store             *transcriptcache.Store
	MaxAttempts       int
	FailureDelay      time.Duration
	ProcessingTimeout time.Duration
	Now               func() time.Time
}

func (l SourceLedger) UpsertDiscovered(ctx context.Context, source Source) error {
	record, err := l.Store.UpsertDiscoveredSource(ctx, transcriptcache.SourceDiscovery{
		Provider:      source.Provider,
		SourcePath:    source.Path,
		SessionID:     source.SessionID,
		WorkspaceHint: source.WorkspacePath,
		SourceSize:    source.Size,
		SourceMTime:   source.ModTime,
		Fingerprint:   source.Fingerprint,
		MaxAttempts:   l.MaxAttempts,
	})
	if err != nil {
		return err
	}
	if record.State != transcriptcache.SourceStateDiscovered {
		return nil
	}
	return l.Store.MarkSourceQueued(ctx, source.Provider, source.Path)
}

func (l SourceLedger) ListCandidates(ctx context.Context, limit int) ([]Source, error) {
	now := l.now()
	if l.ProcessingTimeout > 0 {
		_, err := l.Store.ResetStaleProcessingSources(ctx, transcriptcache.ResetStaleProcessingOptions{
			Before: now.Add(-l.ProcessingTimeout),
			Now:    now,
			Error:  "processing timed out",
		})
		if err != nil {
			return nil, err
		}
	}
	records, err := l.Store.ListSourceCandidates(ctx, transcriptcache.ListSourceCandidatesOptions{Limit: limit, Now: now})
	if err != nil {
		return nil, err
	}
	out := make([]Source, 0, len(records))
	for _, record := range records {
		out = append(out, SourceFromRecord(record))
	}
	return out, nil
}

func (l SourceLedger) MarkProcessing(ctx context.Context, source Source) error {
	return l.Store.MarkSourceProcessing(ctx, source.Provider, source.Path)
}

func (l SourceLedger) MarkProcessed(ctx context.Context, source Source, _ ProcessResult) error {
	return l.Store.MarkSourceProcessed(ctx, source.Provider, source.Path)
}

func (l SourceLedger) MarkFailed(ctx context.Context, source Source, cause error) error {
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	_, err := l.Store.MarkSourceFailed(ctx, transcriptcache.SourceFailure{
		Provider:    source.Provider,
		SourcePath:  source.Path,
		Error:       message,
		RetryAfter:  l.FailureDelay,
		MaxAttempts: l.MaxAttempts,
		Now:         l.now(),
	})
	return err
}

func SourceFromRecord(record transcriptcache.SourceRecord) Source {
	return Source{
		Provider:      record.Provider,
		Path:          record.SourcePath,
		SessionID:     record.SessionID,
		WorkspacePath: record.WorkspaceHint,
		Fingerprint:   record.Fingerprint,
		Size:          record.SourceSize,
		ModTime:       record.SourceMTime,
		Stable:        true,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (l SourceLedger) now() time.Time {
	if l.Now != nil {
		return l.Now().UTC()
	}
	return time.Now().UTC()
}

var (
	_ Scanner = SourceScanner{}
	_ Ledger  = SourceLedger{}
)
