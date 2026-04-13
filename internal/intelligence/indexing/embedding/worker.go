// Package embedding manages embedding job persistence and background processing.
package embedding

import (
	"context"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/runtime/observability"
)

// WorkerConfig configures the background worker.
type WorkerConfig struct {
	// Workers is the number of concurrent workers.
	Workers int `json:"workers" yaml:"workers"`

	// BatchSize is the max items to process per batch.
	BatchSize int `json:"batch_size" yaml:"batch_size"`

	// PollInterval is how often to check for new jobs when idle.
	PollInterval time.Duration `json:"poll_interval,format:units" yaml:"poll_interval"`

	// RateLimitRPS is the max requests per second to the embedding provider.
	RateLimitRPS float64 `json:"rate_limit_rps" yaml:"rate_limit_rps"`

	// ShutdownTimeout is how long to wait for graceful shutdown.
	ShutdownTimeout time.Duration `json:"shutdown_timeout,format:units" yaml:"shutdown_timeout"`
}

// DefaultWorkerConfig returns sensible defaults.
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		Workers:         2,
		BatchSize:       10,
		PollInterval:    5 * time.Second,
		RateLimitRPS:    5.0, // 5 requests per second
		ShutdownTimeout: 30 * time.Second,
	}
}

// Worker processes embedding jobs in the background.
type Worker struct {
	config   WorkerConfig
	store    *Store
	provider semantic.EmbeddingProvider

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}
	limiter *rateLimiter
	wg      sync.WaitGroup
}

// NewWorker creates a new background worker.
func NewWorker(cfg WorkerConfig, store *Store, provider semantic.EmbeddingProvider) *Worker {
	if cfg.Workers <= 0 {
		cfg.Workers = 2
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 10
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	if cfg.RateLimitRPS <= 0 {
		cfg.RateLimitRPS = 5.0
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = 30 * time.Second
	}

	return &Worker{
		config:   cfg,
		store:    store,
		provider: provider,
		limiter:  newRateLimiter(cfg.RateLimitRPS),
	}
}

// Start begins processing jobs in the background.
//
// Index:
// - Purpose: Start the embedding worker loop
// - Flow: validate state → emit start event → spawn worker goroutine → return
// - SideEffects: starts background processing; emits worker_start event
// - FailureModes: missing store/provider, already running
// - Observability: emits embedding.worker_start
// - Related: Worker.run, Worker.Stop
// - Keywords: embedding_worker, start, rate_limit_rps, workers
func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.stopCh = make(chan struct{})
	w.doneCh = make(chan struct{})
	w.mu.Unlock()

	observability.Emit(ctx, observability.NewEvent("embedding.worker_start").
		WithComponent("embedding-worker").
		WithData("workers", w.config.Workers).
		WithData("rate_limit_rps", w.config.RateLimitRPS).
		Success(0))

	go w.run(ctx)
	return nil
}

// Stop gracefully shuts down the worker.
//
// Index:
// - Purpose: Stop the embedding worker loop
// - Flow: signal stop → wait for done or timeout → emit stop/timeout event
// - SideEffects: stops background processing; emits worker_stop/worker_timeout
// - FailureModes: shutdown timeout
// - Observability: emits embedding.worker_stop, embedding.worker_timeout
// - Related: Worker.run, Worker.Start
// - Keywords: embedding_worker, stop, shutdown_timeout
func (w *Worker) Stop() error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	close(w.stopCh)
	w.mu.Unlock()

	// Wait for shutdown with timeout
	select {
	case <-w.doneCh:
		observability.Emit(context.Background(), observability.NewEvent("embedding.worker_stop").
			WithComponent("embedding-worker").
			Success(0))
	case <-time.After(w.config.ShutdownTimeout):
		observability.Emit(context.Background(), observability.NewEvent("embedding.worker_timeout").
			WithComponent("embedding-worker").
			WithData("message", "shutdown timed out").
			Error(nil, 0))
	}

	w.mu.Lock()
	w.running = false
	w.mu.Unlock()

	return nil
}

// IsRunning returns whether the worker is currently running.
func (w *Worker) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// run processes queued embedding jobs until shutdown.
//
// Index:
// - Purpose: Poll embedding jobs and write embedding results
// - Flow: poll queue → process batch → update status → sleep/backoff
// - SideEffects: embedding provider calls; queue updates; embedding writes
// - FailureModes: provider errors, queue/store errors
// - Observability: emits embedding.job_failed, embedding.job_succeeded, embedding.job_skipped
// - Related: Store.Dequeue, Store.MarkComplete, Store.MarkFailed
// - Keywords: embedding_queue, batch, poll, job_status
func (w *Worker) run(ctx context.Context) {
	defer close(w.doneCh)

	// Create job channel for workers
	jobCh := make(chan *EmbeddingJob, w.config.Workers*2)

	// Start workers
	for i := 0; i < w.config.Workers; i++ {
		w.wg.Add(1)
		go w.worker(ctx, i, jobCh)
	}

	// Dispatcher loop
	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			close(jobCh)
			w.wg.Wait()
			return
		case <-w.stopCh:
			close(jobCh)
			w.wg.Wait()
			return
		case <-ticker.C:
			w.dispatchJobs(ctx, jobCh)
		}
	}
}

func (w *Worker) dispatchJobs(ctx context.Context, jobCh chan<- *EmbeddingJob) {
	dispatched := 0

	for dispatched < w.config.BatchSize {
		job, err := w.store.ClaimNext(ctx)
		if err != nil {
			observability.Emit(ctx, observability.NewEvent("embedding.claim_failed").
				WithComponent("embedding-worker").
				Error(err, 0))
			return
		}
		if job == nil {
			// No more jobs available
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case jobCh <- job:
			dispatched++
		}
	}
}

func (w *Worker) worker(ctx context.Context, id int, jobCh <-chan *EmbeddingJob) {
	defer w.wg.Done()

	for job := range jobCh {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		default:
		}

		w.processJob(ctx, id, job)
	}
}

func (w *Worker) processJob(ctx context.Context, workerID int, job *EmbeddingJob) {
	start := time.Now()

	// Apply rate limiting
	w.limiter.wait(ctx)

	// Generate embedding
	embedding, err := w.provider.Embed(ctx, job.Content)
	if err != nil {
		observability.Emit(ctx, observability.NewEvent("embedding.generation_failed").
			WithComponent("embedding-worker").
			WithData("worker_id", workerID).
			WithData("job_id", job.ID).
			WithData("symbol_id", job.SymbolID).
			Error(err, time.Since(start)))
		if failErr := w.store.Fail(ctx, job.ID, err.Error()); failErr != nil {
			observability.Emit(ctx, observability.NewEvent("embedding.mark_failed_error").
				WithComponent("embedding-worker").
				WithData("job_id", job.ID).
				Error(failErr, 0))
		}
		return
	}

	// Store result
	if err := w.store.Complete(ctx, job.ID, embedding, w.provider.Model()); err != nil {
		observability.Emit(ctx, observability.NewEvent("embedding.store_failed").
			WithComponent("embedding-worker").
			WithData("worker_id", workerID).
			WithData("job_id", job.ID).
			Error(err, time.Since(start)))
		if failErr := w.store.Fail(ctx, job.ID, err.Error()); failErr != nil {
			observability.Emit(ctx, observability.NewEvent("embedding.mark_failed_error").
				WithComponent("embedding-worker").
				WithData("job_id", job.ID).
				Error(failErr, 0))
		}
		return
	}

	// Success - emit event only for significant operations (remove for high-volume)
	observability.Emit(ctx, observability.NewEvent("embedding.generated").
		WithComponent("embedding-worker").
		WithData("worker_id", workerID).
		WithData("job_id", job.ID).
		WithData("dimensions", len(embedding)).
		Success(time.Since(start)))
}

// rateLimiter implements a simple token bucket rate limiter.
type rateLimiter struct {
	mu          sync.Mutex
	rps         float64
	tokens      float64
	maxTokens   float64
	lastRefill  time.Time
	minInterval time.Duration
}

func newRateLimiter(rps float64) *rateLimiter {
	return &rateLimiter{
		rps:         rps,
		tokens:      rps, // Start with full bucket
		maxTokens:   rps * 2,
		lastRefill:  time.Now(),
		minInterval: time.Duration(float64(time.Second) / rps),
	}
}

func (rl *rateLimiter) wait(ctx context.Context) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill)
	rl.tokens += rl.rps * elapsed.Seconds()
	if rl.tokens > rl.maxTokens {
		rl.tokens = rl.maxTokens
	}
	rl.lastRefill = now

	// If we have tokens, consume one and return
	if rl.tokens >= 1.0 {
		rl.tokens--
		return
	}

	// Wait for a token
	waitTime := time.Duration(float64(time.Second) * (1.0 - rl.tokens) / rl.rps)
	rl.mu.Unlock()

	select {
	case <-ctx.Done():
	case <-time.After(waitTime):
	}

	rl.mu.Lock()
	rl.tokens = 0 // Consume the token we waited for
}

// ProcessOne processes a single job synchronously (for testing/CLI).
func (w *Worker) ProcessOne(ctx context.Context) (bool, error) {
	job, err := w.store.ClaimNext(ctx)
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, nil
	}

	w.processJob(ctx, 0, job)
	return true, nil
}
