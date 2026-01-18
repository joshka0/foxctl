package summary

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/config"
	llmproviders "github.com/jkatigb/agentctl/internal/providers/llm"
	"github.com/jkatigb/agentctl/internal/queue"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/rs/zerolog"
)

// Worker processes summary queue jobs in the background.
type Worker struct {
	config       WorkerConfig
	queueStore   *queue.Store
	sessionStore *sessions.Store
	providers    []llmproviders.Provider
	cfg          config.Config
	logger       zerolog.Logger

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}
	limiter *rateLimiter
	wg      sync.WaitGroup
}

// NewWorker creates a new summary worker.
func NewWorker(
	cfg WorkerConfig,
	queueStore *queue.Store,
	sessionStore *sessions.Store,
	providers []llmproviders.Provider,
	appCfg config.Config,
	logger zerolog.Logger,
) *Worker {
	if cfg.Workers <= 0 {
		cfg.Workers = 2
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 5
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 10 * time.Second
	}
	if cfg.RateLimitRPS <= 0 {
		cfg.RateLimitRPS = 2.0
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = 30 * time.Second
	}

	return &Worker{
		config:       cfg,
		queueStore:   queueStore,
		sessionStore: sessionStore,
		providers:    providers,
		cfg:          appCfg,
		logger:       logger.With().Str("component", "summary-worker").Logger(),
		limiter:      newRateLimiter(cfg.RateLimitRPS),
	}
}

// Start begins the worker processing loop.
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
		Msg("starting summary worker")

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
		w.logger.Info().Msg("summary worker stopped")
	case <-time.After(w.config.ShutdownTimeout):
		w.logger.Warn().Msg("summary worker shutdown timed out")
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
	jobCh := make(chan *queue.Job, w.config.Workers*2)

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

func (w *Worker) dispatchJobs(ctx context.Context, jobCh chan<- *queue.Job) {
	dispatched := 0

	for dispatched < w.config.BatchSize {
		job, err := w.queueStore.ClaimNext(ctx, queue.ClaimOptions{})
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
		w.logger.Debug().Int("dispatched", dispatched).Msg("dispatched summary jobs")
	}
}

func (w *Worker) worker(ctx context.Context, id int, jobCh <-chan *queue.Job) {
	defer w.wg.Done()

	for job := range jobCh {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		default:
		}

		// Rate limit
		w.limiter.wait(ctx)

		// Process job
		result := w.processJob(ctx, job)

		if result.Success {
			if err := w.queueStore.Complete(ctx, job.ID); err != nil {
				w.logger.Error().Err(err).Str("job_id", job.ID).Msg("failed to mark job complete")
			}
		} else if result.Skipped {
			// Skipped jobs are still "complete" - they don't need retry
			if err := w.queueStore.Complete(ctx, job.ID); err != nil {
				w.logger.Error().Err(err).Str("job_id", job.ID).Msg("failed to mark skipped job complete")
			}
		} else {
			if err := w.queueStore.Fail(ctx, job.ID, result.Error); err != nil {
				w.logger.Error().Err(err).Str("job_id", job.ID).Msg("failed to mark job failed")
			}
		}
	}
}

func (w *Worker) processJob(ctx context.Context, job *queue.Job) JobResult {
	result := JobResult{
		JobID: job.ID,
	}

	// Parse payload
	var payload WindowPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		result.Error = fmt.Sprintf("unmarshal payload: %v", err)
		return result
	}
	result.SessionID = payload.SessionID
	result.WindowIndex = payload.WindowIndex

	// Get the window
	window, err := w.sessionStore.GetContextWindow(ctx, payload.SessionID, payload.WindowIndex)
	if err != nil {
		result.Error = fmt.Sprintf("get window: %v", err)
		return result
	}

	// Skip if already has summary (unless force)
	if window.Summary != "" && !payload.Force {
		result.Success = true
		result.Skipped = true
		result.SkipReason = "already_summarized"
		return result
	}

	// Get content preview from chunks for this window
	contentPreview, err := w.getWindowContentPreview(ctx, payload.SessionID, window.ChunkStart, window.ChunkEnd)
	if err != nil {
		w.logger.Warn().Err(err).Msg("failed to get content preview, using empty")
		contentPreview = ""
	}

	// Skip if no content preview (reduces noise per user request)
	if contentPreview == "" {
		result.Success = true
		result.Skipped = true
		result.SkipReason = "no_content_preview"
		w.logger.Debug().
			Str("session_id", payload.SessionID).
			Int("window_index", payload.WindowIndex).
			Msg("skipping window with no content preview")
		return result
	}

	// Summarize the window
	summary, err := w.summarizeWindow(ctx, &window, contentPreview)
	if err != nil {
		result.Error = fmt.Sprintf("summarize: %v", err)
		return result
	}

	// Save the summary using window ID
	if err := w.sessionStore.UpdateContextWindowSummary(ctx, window.ID, summary); err != nil {
		result.Error = fmt.Sprintf("save summary: %v", err)
		return result
	}

	result.Success = true
	w.logger.Debug().
		Str("session_id", payload.SessionID).
		Int("window_index", payload.WindowIndex).
		Int("summary_len", len(summary)).
		Msg("summarized window")

	return result
}

func (w *Worker) getWindowContentPreview(ctx context.Context, sessionID string, chunkStart, chunkEnd int) (string, error) {
	// Get chunks for this window's range
	chunks, err := w.sessionStore.GetChunks(ctx, sessionID, 0) // Get all chunks
	if err != nil {
		return "", err
	}

	var previews []string
	for _, chunk := range chunks {
		if chunk.ChunkIndex >= chunkStart && chunk.ChunkIndex <= chunkEnd {
			if chunk.ContentPreview != "" {
				previews = append(previews, chunk.ContentPreview)
			}
		}
	}

	// Join with newlines, limit total length
	combined := strings.Join(previews, "\n")
	const maxLen = 2000
	if len(combined) > maxLen {
		combined = combined[:maxLen] + "..."
	}

	return combined, nil
}

func (w *Worker) summarizeWindow(ctx context.Context, window *storage.ContextWindow, contentPreview string) (string, error) {
	if len(w.providers) == 0 {
		return "", fmt.Errorf("no LLM providers configured")
	}

	// Build prompt from window content
	prompt := buildWindowSummaryPrompt(window, contentPreview)

	// Try providers in order
	var lastErr error
	for _, provider := range w.providers {
		summary, err := callLLM(ctx, provider, prompt)
		if err != nil {
			lastErr = err
			w.logger.Warn().
				Err(err).
				Str("provider", provider.Name).
				Msg("provider failed, trying next")
			continue
		}
		return summary, nil
	}

	return "", fmt.Errorf("all providers failed: %w", lastErr)
}

func buildWindowSummaryPrompt(window *storage.ContextWindow, contentPreview string) string {
	return fmt.Sprintf(`Summarize this coding session window in 2-3 concise sentences.
Focus on: what was accomplished, key decisions made, and any issues encountered.

Window trigger: %s
Content preview:
%s

Summary:`, window.Trigger, contentPreview)
}

func callLLM(ctx context.Context, provider llmproviders.Provider, prompt string) (string, error) {
	if provider.IsCLI {
		return "", fmt.Errorf("CLI providers not supported in worker")
	}

	maxTokens := provider.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 500
	}

	reqBody := map[string]any{
		"model":      provider.Model,
		"max_tokens": maxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)

	if strings.HasPrefix(provider.Name, "openrouter:") {
		req.Header.Set("HTTP-Referer", "https://github.com/jkatigb/agentctl")
		req.Header.Set("X-Title", "agentctl")
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

// --- Rate Limiter ---

type rateLimiter struct {
	mu       sync.Mutex
	lastCall time.Time
	interval time.Duration
}

func newRateLimiter(rps float64) *rateLimiter {
	if rps <= 0 {
		rps = 1.0
	}
	return &rateLimiter{
		interval: time.Duration(float64(time.Second) / rps),
	}
}

func (r *rateLimiter) wait(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(r.lastCall)

	if elapsed < r.interval {
		sleepTime := r.interval - elapsed
		select {
		case <-ctx.Done():
			return
		case <-time.After(sleepTime):
		}
	}

	r.lastCall = time.Now()
}
