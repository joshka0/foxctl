package transcriptpipeline

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

const defaultDaemonQueueBuffer = 64

var (
	// ErrMissingDaemonQueue indicates the queue dependency is nil.
	ErrMissingDaemonQueue = errors.New("transcriptpipeline: missing daemon queue")
	// ErrMissingDaemonProcessor indicates the processor dependency is nil.
	ErrMissingDaemonProcessor = errors.New("transcriptpipeline: missing daemon processor")
)

// JobKind identifies which transcript pipeline entrypoint should execute.
type JobKind string

const (
	JobKindSingle          JobKind = "single"
	JobKindGrouped         JobKind = "grouped"
	JobKindSingleDoctrine  JobKind = "single_doctrine"
	JobKindGroupedDoctrine JobKind = "grouped_doctrine"
)

// Job is one daemon-facing transcript pipeline request.
type Job struct {
	Kind            JobKind
	Single          *SingleRunOptions
	Grouped         *GroupRunOptions
	SingleDoctrine  *SingleRunOptions
	GroupedDoctrine *GroupRunOptions
}

// Key returns the idempotency key used for enqueue dedupe.
func (j Job) Key() string {
	switch j.Kind {
	case JobKindSingle:
		if j.Single == nil {
			return ""
		}
		return strings.Join([]string{
			string(JobKindSingle),
			strings.TrimSpace(j.Single.Provider),
			strings.TrimSpace(j.Single.SourceFile),
			strings.TrimSpace(j.Single.SessionID),
			strings.TrimSpace(j.Single.Workspace),
			boolKey(j.Single.PersistMemory),
		}, "|")
	case JobKindGrouped:
		if j.Grouped == nil {
			return ""
		}
		files := make([]string, 0, len(j.Grouped.SourceFiles))
		for _, item := range j.Grouped.SourceFiles {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			files = append(files, item)
		}
		sort.Strings(files)
		return strings.Join([]string{
			string(JobKindGrouped),
			strings.Join(files, ","),
			strings.TrimSpace(j.Grouped.Workspace),
			boolKey(j.Grouped.PersistMemory),
		}, "|")
	case JobKindSingleDoctrine:
		if j.SingleDoctrine == nil {
			return ""
		}
		return strings.Join([]string{
			string(JobKindSingleDoctrine),
			strings.TrimSpace(j.SingleDoctrine.Provider),
			strings.TrimSpace(j.SingleDoctrine.SourceFile),
			strings.TrimSpace(j.SingleDoctrine.SessionID),
			strings.TrimSpace(j.SingleDoctrine.Workspace),
			boolKey(j.SingleDoctrine.PersistMemory),
			boolKey(j.SingleDoctrine.AlignDoctrine),
		}, "|")
	case JobKindGroupedDoctrine:
		if j.GroupedDoctrine == nil {
			return ""
		}
		files := make([]string, 0, len(j.GroupedDoctrine.SourceFiles))
		for _, item := range j.GroupedDoctrine.SourceFiles {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			files = append(files, item)
		}
		sort.Strings(files)
		return strings.Join([]string{
			string(JobKindGroupedDoctrine),
			strings.Join(files, ","),
			strings.TrimSpace(j.GroupedDoctrine.Workspace),
			boolKey(j.GroupedDoctrine.PersistMemory),
			boolKey(j.GroupedDoctrine.AlignDoctrine),
		}, "|")
	default:
		return ""
	}
}

// Queue is a bounded non-blocking queue with in-flight key dedupe.
type Queue struct {
	mu     sync.Mutex
	ch     chan Job
	seen   map[string]struct{}
	closed bool
}

// NewQueue creates a bounded daemon-facing queue.
func NewQueue(buffer int) *Queue {
	if buffer <= 0 {
		buffer = defaultDaemonQueueBuffer
	}
	return &Queue{
		ch:   make(chan Job, buffer),
		seen: make(map[string]struct{}),
	}
}

// Enqueue attempts to enqueue one job without blocking.
func (q *Queue) Enqueue(job Job) bool {
	if q == nil {
		return false
	}
	key := strings.TrimSpace(job.Key())
	if key == "" {
		return false
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return false
	}
	if _, exists := q.seen[key]; exists {
		return false
	}
	select {
	case q.ch <- job:
		q.seen[key] = struct{}{}
		return true
	default:
		return false
	}
}

// Jobs returns a read-only stream of queued jobs.
func (q *Queue) Jobs() <-chan Job {
	if q == nil {
		return nil
	}
	return q.ch
}

// Release clears one in-flight dedupe key.
func (q *Queue) Release(job Job) {
	if q == nil {
		return
	}
	key := strings.TrimSpace(job.Key())
	if key == "" {
		return
	}
	q.mu.Lock()
	delete(q.seen, key)
	q.mu.Unlock()
}

// Close closes the queue exactly once.
func (q *Queue) Close() {
	if q == nil {
		return
	}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	close(q.ch)
	q.mu.Unlock()
}

// Processor runs transcript pipeline jobs.
type Processor interface {
	RunSingle(ctx context.Context, opts SingleRunOptions) (SingleRunResult, error)
	RunGrouped(ctx context.Context, opts GroupRunOptions) (GroupRunResult, error)
	RunSingleDoctrine(ctx context.Context, opts SingleRunOptions) (SingleRunResult, error)
	RunGroupedDoctrine(ctx context.Context, opts GroupRunOptions) (GroupRunResult, error)
}

// ProcessorFuncSet adapts function fields to Processor.
type ProcessorFuncSet struct {
	Single          func(ctx context.Context, opts SingleRunOptions) (SingleRunResult, error)
	Grouped         func(ctx context.Context, opts GroupRunOptions) (GroupRunResult, error)
	SingleDoctrine  func(ctx context.Context, opts SingleRunOptions) (SingleRunResult, error)
	GroupedDoctrine func(ctx context.Context, opts GroupRunOptions) (GroupRunResult, error)
}

func (p ProcessorFuncSet) RunSingle(ctx context.Context, opts SingleRunOptions) (SingleRunResult, error) {
	if p.Single == nil {
		return SingleRunResult{}, ErrMissingDaemonProcessor
	}
	return p.Single(ctx, opts)
}

func (p ProcessorFuncSet) RunGrouped(ctx context.Context, opts GroupRunOptions) (GroupRunResult, error) {
	if p.Grouped == nil {
		return GroupRunResult{}, ErrMissingDaemonProcessor
	}
	return p.Grouped(ctx, opts)
}

func (p ProcessorFuncSet) RunSingleDoctrine(ctx context.Context, opts SingleRunOptions) (SingleRunResult, error) {
	if p.SingleDoctrine == nil {
		return SingleRunResult{}, ErrMissingDaemonProcessor
	}
	return p.SingleDoctrine(ctx, opts)
}

func (p ProcessorFuncSet) RunGroupedDoctrine(ctx context.Context, opts GroupRunOptions) (GroupRunResult, error) {
	if p.GroupedDoctrine == nil {
		return GroupRunResult{}, ErrMissingDaemonProcessor
	}
	return p.GroupedDoctrine(ctx, opts)
}

// DefaultProcessor uses the package-level pipeline entrypoints.
var DefaultProcessor Processor = ProcessorFuncSet{
	Single:          RunSingle,
	Grouped:         RunGrouped,
	SingleDoctrine:  RunSingleDoctrine,
	GroupedDoctrine: RunGroupedDoctrine,
}

// JobResult is one completed daemon pipeline job.
type JobResult struct {
	Job     Job
	Single  *SingleRunResult
	Grouped *GroupRunResult
	Err     error
}

// RunnerConfig wires daemon queue processing behavior.
type RunnerConfig struct {
	Queue     *Queue
	Processor Processor
	OnResult  func(JobResult)
	OnError   func(error)
}

// Runner consumes queued transcript pipeline jobs and processes them serially.
type Runner struct {
	queue     *Queue
	processor Processor
	onResult  func(JobResult)
	onError   func(error)
	active    atomic.Int64
}

// NewRunner creates a daemon-facing transcript pipeline runner.
func NewRunner(cfg RunnerConfig) *Runner {
	processor := cfg.Processor
	if processor == nil {
		processor = DefaultProcessor
	}
	onError := cfg.OnError
	if onError == nil {
		onError = func(error) {}
	}
	onResult := cfg.OnResult
	if onResult == nil {
		onResult = func(JobResult) {}
	}
	return &Runner{
		queue:     cfg.Queue,
		processor: processor,
		onResult:  onResult,
		onError:   onError,
	}
}

// Run blocks until context cancellation or queue close.
func (r *Runner) Run(ctx context.Context) error {
	if r == nil || r.queue == nil {
		return ErrMissingDaemonQueue
	}
	if r.processor == nil {
		return ErrMissingDaemonProcessor
	}
	jobs := r.queue.Jobs()
	for {
		select {
		case <-ctx.Done():
			return nil
		case job, ok := <-jobs:
			if !ok {
				return nil
			}
			r.process(ctx, job)
		}
	}
}

func (r *Runner) process(ctx context.Context, job Job) {
	if r.queue != nil {
		defer r.queue.Release(job)
	}
	r.active.Add(1)
	defer r.active.Add(-1)

	result := JobResult{Job: job}
	switch job.Kind {
	case JobKindSingle:
		if job.Single == nil {
			result.Err = ErrMissingDaemonProcessor
			break
		}
		out, err := r.processor.RunSingle(ctx, *job.Single)
		if err != nil {
			result.Err = err
		} else {
			result.Single = &out
		}
	case JobKindGrouped:
		if job.Grouped == nil {
			result.Err = ErrMissingDaemonProcessor
			break
		}
		out, err := r.processor.RunGrouped(ctx, *job.Grouped)
		if err != nil {
			result.Err = err
		} else {
			result.Grouped = &out
		}
	case JobKindSingleDoctrine:
		if job.SingleDoctrine == nil {
			result.Err = ErrMissingDaemonProcessor
			break
		}
		out, err := r.processor.RunSingleDoctrine(ctx, *job.SingleDoctrine)
		if err != nil {
			result.Err = err
		} else {
			result.Single = &out
		}
	case JobKindGroupedDoctrine:
		if job.GroupedDoctrine == nil {
			result.Err = ErrMissingDaemonProcessor
			break
		}
		out, err := r.processor.RunGroupedDoctrine(ctx, *job.GroupedDoctrine)
		if err != nil {
			result.Err = err
		} else {
			result.Grouped = &out
		}
	default:
		result.Err = ErrMissingDaemonProcessor
	}
	if result.Err != nil {
		r.onError(result.Err)
	}
	r.onResult(result)
}

// ActiveJobs returns the number of currently running jobs.
func (r *Runner) ActiveJobs() int64 {
	if r == nil {
		return 0
	}
	return r.active.Load()
}

func boolKey(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// EnqueueSingleDoctrine enqueues one doctrine-only transcript job.
func EnqueueSingleDoctrine(q *Queue, opts SingleRunOptions) bool {
	if q == nil {
		return false
	}
	return q.Enqueue(Job{
		Kind:           JobKindSingleDoctrine,
		SingleDoctrine: &opts,
	})
}

// EnqueueGroupedDoctrine enqueues one grouped doctrine-only transcript job.
func EnqueueGroupedDoctrine(q *Queue, opts GroupRunOptions) bool {
	if q == nil {
		return false
	}
	return q.Enqueue(Job{
		Kind:            JobKindGroupedDoctrine,
		GroupedDoctrine: &opts,
	})
}
