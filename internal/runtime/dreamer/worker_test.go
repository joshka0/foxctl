package dreamer

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunOnceSkipsUnstableSourcesAndProcessesBoundedBatch(t *testing.T) {
	ctx := context.Background()
	sources := []Source{
		{Provider: "codex", Path: "/tmp/3.jsonl", SessionID: "s3", Fingerprint: "f3", Stable: true},
		{Provider: "codex", Path: "/tmp/1.jsonl", SessionID: "s1", Fingerprint: "f1", Stable: true},
		{Provider: "codex", Path: "/tmp/2.jsonl", SessionID: "s2", Fingerprint: "f2", Stable: false},
	}
	ledger := newMemoryLedger()
	processor := &recordingProcessor{results: map[string]ProcessResult{
		"/tmp/1.jsonl": {HistoryRecords: 2, DreamNotes: 1},
		"/tmp/3.jsonl": {HistoryRecords: 1, DreamNotes: 1, Blurred: true},
	}}
	worker := mustWorker(t, Config{BatchSize: 1, Concurrency: 1}, staticScanner{sources: sources}, ledger, processor)

	report, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if report.Discovered != 3 || report.Queued != 2 || report.Skipped != 1 || report.Processed != 1 {
		t.Fatalf("report=%+v", report)
	}
	if got := processor.paths(); !slices.Equal(got, []string{"/tmp/1.jsonl"}) {
		t.Fatalf("processed paths=%v", got)
	}
	if ledger.state("/tmp/1.jsonl") != "processed" {
		t.Fatalf("source state=%q want processed", ledger.state("/tmp/1.jsonl"))
	}
}

func TestRunOnceMarksFailuresWithoutDuplicatingSuccesses(t *testing.T) {
	ctx := context.Background()
	sources := []Source{
		{Provider: "codex", Path: "/tmp/a.jsonl", SessionID: "a", Fingerprint: "fa", Stable: true},
		{Provider: "codex", Path: "/tmp/b.jsonl", SessionID: "b", Fingerprint: "fb", Stable: true},
	}
	ledger := newMemoryLedger()
	processor := &recordingProcessor{
		results: map[string]ProcessResult{"/tmp/a.jsonl": {HistoryRecords: 1}},
		errs:    map[string]error{"/tmp/b.jsonl": errors.New("derive failed")},
	}
	worker := mustWorker(t, Config{BatchSize: 10, Concurrency: 2}, staticScanner{sources: sources}, ledger, processor)

	report, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if report.Processed != 1 || report.Failed != 1 {
		t.Fatalf("report=%+v", report)
	}
	if ledger.state("/tmp/a.jsonl") != "processed" {
		t.Fatalf("a state=%q want processed", ledger.state("/tmp/a.jsonl"))
	}
	if ledger.state("/tmp/b.jsonl") != "failed" {
		t.Fatalf("b state=%q want failed", ledger.state("/tmp/b.jsonl"))
	}
	if got := processor.paths(); !slices.Equal(got, []string{"/tmp/a.jsonl", "/tmp/b.jsonl"}) {
		t.Fatalf("processed paths=%v", got)
	}

	processor.clear()
	report, err = worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("second RunOnce() error = %v", err)
	}
	if report.Processed != 0 || report.Failed != 1 {
		t.Fatalf("second report=%+v", report)
	}
	if got := processor.paths(); !slices.Equal(got, []string{"/tmp/b.jsonl"}) {
		t.Fatalf("second processed paths=%v", got)
	}
}

func TestRunOnceBoundsConcurrentProcessing(t *testing.T) {
	ctx := context.Background()
	sources := []Source{
		{Provider: "codex", Path: "/tmp/1.jsonl", Stable: true},
		{Provider: "codex", Path: "/tmp/2.jsonl", Stable: true},
		{Provider: "codex", Path: "/tmp/3.jsonl", Stable: true},
		{Provider: "codex", Path: "/tmp/4.jsonl", Stable: true},
	}
	processor := &recordingProcessor{
		results: map[string]ProcessResult{
			"/tmp/1.jsonl": {}, "/tmp/2.jsonl": {}, "/tmp/3.jsonl": {}, "/tmp/4.jsonl": {},
		},
		block: 5 * time.Millisecond,
	}
	worker := mustWorker(t, Config{BatchSize: 4, Concurrency: 2}, staticScanner{sources: sources}, newMemoryLedger(), processor)

	report, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if report.Processed != 4 {
		t.Fatalf("processed=%d want 4", report.Processed)
	}
	if processor.maxActive > 2 {
		t.Fatalf("max active processors=%d want <=2", processor.maxActive)
	}
}

func TestRunStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	scanner := &signalingScanner{done: make(chan struct{})}
	worker := mustWorker(t, Config{Interval: time.Hour}, scanner, newMemoryLedger(), &recordingProcessor{})
	errc := make(chan error, 1)

	go func() {
		errc <- worker.Run(ctx)
	}()
	<-scanner.done
	cancel()

	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error=%v want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after cancellation")
	}
}

func TestRunContinuesAfterPassError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	scanner := &recoveringScanner{
		err:    errors.New("scan failed"),
		cancel: cancel,
	}
	var observed []string
	worker := mustWorker(t, Config{
		Interval: time.Millisecond,
		OnError: func(err error) {
			observed = append(observed, err.Error())
		},
	}, scanner, newMemoryLedger(), &recordingProcessor{})

	err := worker.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error=%v want context.Canceled", err)
	}
	if scanner.calls < 2 {
		t.Fatalf("scan calls=%d want at least 2", scanner.calls)
	}
	if len(observed) != 1 || !strings.Contains(observed[0], "scan failed") {
		t.Fatalf("observed errors=%v", observed)
	}
}

func mustWorker(t *testing.T, cfg Config, scanner Scanner, ledger Ledger, processor Processor) *Worker {
	t.Helper()
	worker, err := NewWorker(cfg, scanner, ledger, processor)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	return worker
}

type staticScanner struct {
	sources []Source
}

func (s staticScanner) Scan(context.Context) ([]Source, error) {
	return append([]Source(nil), s.sources...), nil
}

type signalingScanner struct {
	once sync.Once
	done chan struct{}
}

func (s *signalingScanner) Scan(context.Context) ([]Source, error) {
	s.once.Do(func() { close(s.done) })
	return nil, nil
}

type recoveringScanner struct {
	err    error
	cancel func()
	calls  int
}

func (s *recoveringScanner) Scan(context.Context) ([]Source, error) {
	s.calls++
	if s.calls == 1 {
		return nil, s.err
	}
	if s.cancel != nil {
		s.cancel()
	}
	return nil, nil
}

type memoryLedger struct {
	mu      sync.Mutex
	sources map[string]Source
	states  map[string]string
}

func newMemoryLedger() *memoryLedger {
	return &memoryLedger{
		sources: map[string]Source{},
		states:  map[string]string{},
	}
}

func (l *memoryLedger) UpsertDiscovered(_ context.Context, source Source) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := source.Path
	l.sources[key] = source
	if l.states[key] == "" {
		l.states[key] = "discovered"
	}
	return nil
}

func (l *memoryLedger) ListCandidates(_ context.Context, limit int) ([]Source, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var keys []string
	for key, state := range l.states {
		if state == "discovered" || state == "failed" {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}
	out := make([]Source, 0, len(keys))
	for _, key := range keys {
		out = append(out, l.sources[key])
	}
	return out, nil
}

func (l *memoryLedger) MarkProcessing(_ context.Context, source Source) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.states[source.Path] = "processing"
	return nil
}

func (l *memoryLedger) MarkProcessed(_ context.Context, source Source, _ ProcessResult) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.states[source.Path] = "processed"
	return nil
}

func (l *memoryLedger) MarkFailed(_ context.Context, source Source, _ error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.states[source.Path] = "failed"
	return nil
}

func (l *memoryLedger) state(path string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.states[path]
}

type recordingProcessor struct {
	mu        sync.Mutex
	results   map[string]ProcessResult
	errs      map[string]error
	seen      []string
	active    int
	maxActive int
	block     time.Duration
}

func (p *recordingProcessor) Process(_ context.Context, source Source) (ProcessResult, error) {
	p.mu.Lock()
	p.seen = append(p.seen, source.Path)
	p.active++
	if p.active > p.maxActive {
		p.maxActive = p.active
	}
	p.mu.Unlock()

	if p.block > 0 {
		time.Sleep(p.block)
	}

	p.mu.Lock()
	p.active--
	result := p.results[source.Path]
	err := p.errs[source.Path]
	p.mu.Unlock()
	return result, err
}

func (p *recordingProcessor) paths() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := append([]string(nil), p.seen...)
	slices.Sort(out)
	return out
}

func (p *recordingProcessor) clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seen = nil
}
