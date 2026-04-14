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

	"github.com/joshka0/foxctl/internal/platform/config"
	llmproviders "github.com/joshka0/foxctl/internal/providers/llm"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/queue"
	"github.com/joshka0/foxctl/internal/storage/sessions"
)

// Worker processes summary queue jobs in the background.
type Worker struct {
	config       WorkerConfig
	queueStore   *queue.Store
	sessionStore *sessions.Store
	providers    []llmproviders.Provider
	cfg          config.Config

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

	observability.Emit(ctx, observability.NewEvent("summary.worker_start").
		WithComponent("summary-worker").
		WithData("workers", w.config.Workers).
		WithData("rate_limit_rps", w.config.RateLimitRPS).
		Success(0))

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
		observability.Emit(context.Background(), observability.NewEvent("summary.worker_stop").
			WithComponent("summary-worker").
			Success(0))
	case <-time.After(w.config.ShutdownTimeout):
		observability.Emit(context.Background(), observability.NewEvent("summary.worker_timeout").
			WithComponent("summary-worker").
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
			observability.Emit(ctx, observability.NewEvent("summary.claim_failed").
				WithComponent("summary-worker").
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
				observability.Emit(ctx, observability.NewEvent("summary.mark_complete_failed").
					WithComponent("summary-worker").
					WithData("job_id", job.ID).
					Error(err, 0))
			}
		} else if result.Skipped {
			// Skipped jobs are still "complete" - they don't need retry
			if err := w.queueStore.Complete(ctx, job.ID); err != nil {
				observability.Emit(ctx, observability.NewEvent("summary.mark_skipped_failed").
					WithComponent("summary-worker").
					WithData("job_id", job.ID).
					Error(err, 0))
			}
		} else {
			if err := w.queueStore.Fail(ctx, job.ID, result.Error); err != nil {
				observability.Emit(ctx, observability.NewEvent("summary.mark_failed_error").
					WithComponent("summary-worker").
					WithData("job_id", job.ID).
					Error(err, 0))
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
		observability.Emit(ctx, observability.NewEvent("summary.content_preview_failed").
			WithComponent("summary-worker").
			Error(err, 0))
		contentPreview = ""
	}

	// Skip if no content preview (reduces noise per user request)
	if contentPreview == "" {
		result.Success = true
		result.Skipped = true
		result.SkipReason = "no_content_preview"
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
			observability.Emit(ctx, observability.NewEvent("summary.provider_failed").
				WithComponent("summary-worker").
				WithData("provider", provider.Name).
				Error(err, 0))
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
		req.Header.Set("HTTP-Referer", "https://github.com/joshka0/foxctl")
		req.Header.Set("X-Title", "foxctl")
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
