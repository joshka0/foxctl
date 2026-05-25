package dreamer

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultInterval    = 10 * time.Minute
	defaultBatchSize   = 20
	defaultConcurrency = 1
)

// Config controls detached transcript dreaming.
type Config struct {
	Interval    time.Duration
	BatchSize   int
	Concurrency int
	OnError     func(error)
}

// Source is a stable transcript candidate selected by the source scanner.
type Source struct {
	Provider      string    `json:"provider"`
	Path          string    `json:"path"`
	SessionID     string    `json:"session_id,omitempty"`
	WorkspacePath string    `json:"workspace_path,omitempty"`
	Fingerprint   string    `json:"fingerprint,omitempty"`
	Size          int64     `json:"size,omitempty"`
	ModTime       time.Time `json:"mtime,omitempty"`
	Stable        bool      `json:"stable"`
}

// ProcessResult describes durable work completed for one source.
type ProcessResult struct {
	HistoryRecords int  `json:"history_records"`
	DreamNotes     int  `json:"dream_notes"`
	Blurred        bool `json:"blurred"`
}

// Report is returned by one dreamer pass.
type Report struct {
	Discovered int            `json:"discovered"`
	Queued     int            `json:"queued"`
	Processed  int            `json:"processed"`
	Failed     int            `json:"failed"`
	Skipped    int            `json:"skipped"`
	Results    []SourceReport `json:"results,omitempty"`
}

type SourceReport struct {
	Source Source        `json:"source"`
	Result ProcessResult `json:"result"`
	Error  string        `json:"error,omitempty"`
}

type Scanner interface {
	Scan(ctx context.Context) ([]Source, error)
}

type Ledger interface {
	UpsertDiscovered(ctx context.Context, source Source) error
	ListCandidates(ctx context.Context, limit int) ([]Source, error)
	MarkProcessing(ctx context.Context, source Source) error
	MarkProcessed(ctx context.Context, source Source, result ProcessResult) error
	MarkFailed(ctx context.Context, source Source, cause error) error
}

type Processor interface {
	Process(ctx context.Context, source Source) (ProcessResult, error)
}

type Worker struct {
	cfg       Config
	scanner   Scanner
	ledger    Ledger
	processor Processor
}

func NewWorker(cfg Config, scanner Scanner, ledger Ledger, processor Processor) (*Worker, error) {
	cfg = normalizeConfig(cfg)
	if scanner == nil {
		return nil, fmt.Errorf("dreamer: scanner is required")
	}
	if ledger == nil {
		return nil, fmt.Errorf("dreamer: ledger is required")
	}
	if processor == nil {
		return nil, fmt.Errorf("dreamer: processor is required")
	}
	return &Worker{
		cfg:       cfg,
		scanner:   scanner,
		ledger:    ledger,
		processor: processor,
	}, nil
}

func normalizeConfig(cfg Config) Config {
	if cfg.Interval <= 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultBatchSize
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = defaultConcurrency
	}
	if cfg.Concurrency > cfg.BatchSize {
		cfg.Concurrency = cfg.BatchSize
	}
	return cfg
}

// Run executes one pass immediately and then repeats until the context is canceled.
func (w *Worker) Run(ctx context.Context) error {
	if _, err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
		w.reportError(err)
	}
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
				w.reportError(err)
			}
		}
	}
}

// RunOnce scans transcript sources, records stable candidates, and processes a bounded batch.
//
// Index:
//
//	Purpose: Detached transcript dream worker lifecycle.
//	Keywords: transcript dreams, source ledger, bounded worker, run once
//	Related: Scanner, Ledger, Processor
//
// [[domain:transcript-dream-worker]]
// [[invariant:stable-sources-before-processing]]
func (w *Worker) RunOnce(ctx context.Context) (Report, error) {
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	sources, err := w.scanner.Scan(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("dreamer: scan sources: %w", err)
	}

	report := Report{Discovered: len(sources)}
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		source = normalizeSource(source)
		if !source.Stable {
			report.Skipped++
			continue
		}
		if err := w.ledger.UpsertDiscovered(ctx, source); err != nil {
			return report, fmt.Errorf("dreamer: upsert discovered source %q: %w", source.Path, err)
		}
		report.Queued++
	}

	candidates, err := w.ledger.ListCandidates(ctx, w.cfg.BatchSize)
	if err != nil {
		return report, fmt.Errorf("dreamer: list candidates: %w", err)
	}
	if len(candidates) == 0 {
		return report, nil
	}
	results := w.processCandidates(ctx, candidates)
	for _, result := range results {
		if result.Error == "" {
			report.Processed++
		} else {
			report.Failed++
		}
	}
	report.Results = results
	return report, nil
}

func (w *Worker) processCandidates(ctx context.Context, candidates []Source) []SourceReport {
	limit := w.cfg.Concurrency
	if limit <= 0 {
		limit = defaultConcurrency
	}
	sem := make(chan struct{}, limit)
	out := make(chan SourceReport, len(candidates))
	var wg sync.WaitGroup
	for _, candidate := range candidates {
		candidate := normalizeSource(candidate)
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-ctx.Done():
				out <- SourceReport{Source: candidate, Error: ctx.Err().Error()}
				return
			case sem <- struct{}{}:
			}
			defer func() { <-sem }()
			out <- w.processOne(ctx, candidate)
		}()
	}
	wg.Wait()
	close(out)

	results := make([]SourceReport, 0, len(candidates))
	for result := range out {
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool {
		return sourceSortKey(results[i].Source) < sourceSortKey(results[j].Source)
	})
	return results
}

func (w *Worker) processOne(ctx context.Context, source Source) SourceReport {
	if err := w.ledger.MarkProcessing(ctx, source); err != nil {
		return SourceReport{Source: source, Error: fmt.Sprintf("mark processing: %v", err)}
	}
	result, err := w.processor.Process(ctx, source)
	if err != nil {
		if markErr := w.ledger.MarkFailed(ctx, source, err); markErr != nil {
			return SourceReport{Source: source, Error: fmt.Sprintf("%v; mark failed: %v", err, markErr)}
		}
		return SourceReport{Source: source, Error: err.Error()}
	}
	if err := w.ledger.MarkProcessed(ctx, source, result); err != nil {
		return SourceReport{Source: source, Result: result, Error: fmt.Sprintf("mark processed: %v", err)}
	}
	return SourceReport{Source: source, Result: result}
}

func (w *Worker) reportError(err error) {
	if err == nil || w.cfg.OnError == nil {
		return
	}
	w.cfg.OnError(err)
}

func normalizeSource(source Source) Source {
	source.Provider = strings.ToLower(strings.TrimSpace(source.Provider))
	source.Path = strings.TrimSpace(source.Path)
	source.SessionID = strings.TrimSpace(source.SessionID)
	source.WorkspacePath = strings.TrimSpace(source.WorkspacePath)
	source.Fingerprint = strings.TrimSpace(source.Fingerprint)
	return source
}

func sourceSortKey(source Source) string {
	source = normalizeSource(source)
	return strings.Join([]string{source.Provider, source.Path, source.SessionID, source.Fingerprint}, "\x00")
}
