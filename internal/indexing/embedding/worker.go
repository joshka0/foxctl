package embedding

import (
	"context"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/rs/zerolog"
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
	logger   zerolog.Logger

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}
	limiter *rateLimiter
	wg      sync.WaitGroup
}

// NewWorker creates a new background worker.
func NewWorker(cfg WorkerConfig, store *Store, provider semantic.EmbeddingProvider, logger zerolog.Logger) *Worker {
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
		logger:   logger.With().Str("component", "embedding-worker").Logger(),
		limiter:  newRateLimiter(cfg.RateLimitRPS),
	}
}

// Start begins processing jobs in the background.
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

	w.logger.Info().
		Int("workers", w.config.Workers).
		Float64("rate_limit_rps", w.config.RateLimitRPS).
		Msg("starting embedding worker")

	go w.run(ctx)
	return nil
}

// Stop gracefully shuts down the worker.
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
		w.logger.Info().Msg("embedding worker stopped")
	case <-time.After(w.config.ShutdownTimeout):
		w.logger.Warn().Msg("embedding worker shutdown timed out")
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
			w.logger.Error().Err(err).Msg("failed to claim job")
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

	if dispatched > 0 {
		w.logger.Debug().Int("dispatched", dispatched).Msg("dispatched jobs")
	}
}

func (w *Worker) worker(ctx context.Context, id int, jobCh <-chan *EmbeddingJob) {
	defer w.wg.Done()

	logger := w.logger.With().Int("worker_id", id).Logger()
	logger.Debug().Msg("worker started")

	for job := range jobCh {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		default:
		}

		w.processJob(ctx, logger, job)
	}

	logger.Debug().Msg("worker stopped")
}

func (w *Worker) processJob(ctx context.Context, logger zerolog.Logger, job *EmbeddingJob) {
	start := time.Now()

	logger = logger.With().
		Str("job_id", job.ID).
		Str("symbol_id", job.SymbolID).
		Logger()

	// Apply rate limiting
	w.limiter.wait(ctx)

	// Generate embedding
	embedding, err := w.provider.Embed(ctx, job.Content)
	if err != nil {
		logger.Error().Err(err).Msg("embedding generation failed")
		if failErr := w.store.Fail(ctx, job.ID, err.Error()); failErr != nil {
			logger.Error().Err(failErr).Msg("failed to mark job as failed")
		}
		return
	}

	// Store result
	if err := w.store.Complete(ctx, job.ID, embedding, w.provider.Model()); err != nil {
		logger.Error().Err(err).Msg("failed to store embedding")
		if failErr := w.store.Fail(ctx, job.ID, err.Error()); failErr != nil {
			logger.Error().Err(failErr).Msg("failed to mark job as failed")
		}
		return
	}

	logger.Debug().
		Dur("duration", time.Since(start)).
		Int("dimensions", len(embedding)).
		Msg("embedding generated")
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

	logger := w.logger.With().Str("job_id", job.ID).Logger()
	w.processJob(ctx, logger, job)
	return true, nil
}
