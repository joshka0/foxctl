package filesummary

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/indexing/symbol"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/retrieval"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/rs/zerolog"
)

// WorkerConfig configures the background file summary worker.
type WorkerConfig struct {
	// Workers is the number of concurrent workers.
	// NOTE: Currently only single-worker mode is implemented; this field
	// is validated but not used for actual parallelism (reserved for future use).
	Workers int `json:"workers" yaml:"workers"`

	// BatchSize is the max files to process per batch.
	BatchSize int `json:"batch_size" yaml:"batch_size"`

	// PollInterval is how often to check for new files when idle.
	PollInterval time.Duration `json:"poll_interval,format:units" yaml:"poll_interval"`

	// RateLimitRPS is the max requests per second to the LLM provider.
	RateLimitRPS float64 `json:"rate_limit_rps" yaml:"rate_limit_rps"`

	// ShutdownTimeout is how long to wait for graceful shutdown.
	ShutdownTimeout time.Duration `json:"shutdown_timeout,format:units" yaml:"shutdown_timeout"`

	// FileExtensions are the file extensions to process.
	FileExtensions []string `json:"file_extensions" yaml:"file_extensions"`

	// ExcludeDirs are directory names to skip.
	ExcludeDirs []string `json:"exclude_dirs" yaml:"exclude_dirs"`
}

// DefaultWorkerConfig returns sensible defaults.
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		Workers:         1,                // Single worker to avoid LLM rate limits
		BatchSize:       10,               // Process 10 files at a time
		PollInterval:    30 * time.Second, // Check every 30s
		RateLimitRPS:    2.0,              // 2 requests per second (conservative for LLM)
		ShutdownTimeout: 30 * time.Second,
		FileExtensions:  []string{".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".java"},
		ExcludeDirs:     []string{"node_modules", "vendor", ".git", "dist", "build", "__pycache__"},
	}
}

// Worker processes file summaries in the background.
type Worker struct {
	config    WorkerConfig
	store     storage.MemoryStore
	llm       retrieval.SummaryLLM
	embed     semantic.EmbeddingProvider
	workspace string
	logger    zerolog.Logger

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}
	limiter *rateLimiter

	// Stats
	processed int64
	errors    int64
}

// NewWorker creates a new file summary worker.
// If logger is nil, a default stderr logger is used.
func NewWorker(
	cfg WorkerConfig,
	store storage.MemoryStore,
	llm retrieval.SummaryLLM,
	embed semantic.EmbeddingProvider,
	workspace string,
	logger *zerolog.Logger,
) *Worker {
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 10
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 30 * time.Second
	}
	if cfg.RateLimitRPS <= 0 {
		cfg.RateLimitRPS = 2.0
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = 30 * time.Second
	}
	if len(cfg.FileExtensions) == 0 {
		cfg.FileExtensions = DefaultWorkerConfig().FileExtensions
	}
	if len(cfg.ExcludeDirs) == 0 {
		cfg.ExcludeDirs = DefaultWorkerConfig().ExcludeDirs
	}

	// Use default logger if not provided
	var log zerolog.Logger
	if logger != nil {
		log = logger.With().Str("component", "filesummary-worker").Logger()
	} else {
		log = zerolog.New(os.Stderr).With().Timestamp().Str("component", "filesummary-worker").Logger()
	}

	return &Worker{
		config:    cfg,
		store:     store,
		llm:       llm,
		embed:     embed,
		workspace: workspace,
		logger:    log,
		limiter:   newRateLimiter(cfg.RateLimitRPS),
	}
}

// Start begins processing files in the background.
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
		Str("workspace", w.workspace).
		Msg("starting file summary worker")

	go w.run(ctx)
	return nil
}

// Stop gracefully stops the worker.
func (w *Worker) Stop() error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	// Mark as not running and capture stopCh under lock.
	// Do NOT nil out w.stopCh - run() reads it without holding the lock.
	w.running = false
	stopCh := w.stopCh
	w.mu.Unlock()

	// Close the captured channel outside the lock to signal run() to exit.
	// Safe because run() only reads from stopCh, never closes it.
	if stopCh != nil {
		close(stopCh)
	}

	select {
	case <-w.doneCh:
		w.logger.Info().
			Int64("processed", w.processed).
			Int64("errors", w.errors).
			Msg("file summary worker stopped")
	case <-time.After(w.config.ShutdownTimeout):
		w.logger.Warn().Msg("file summary worker shutdown timed out")
	}

	return nil
}

// TriggerFullScan initiates a full repository scan for missing summaries.
// Returns the number of files queued for processing.
func (w *Worker) TriggerFullScan(ctx context.Context) (int, error) {
	files, err := w.scanWorkspace(ctx)
	if err != nil {
		return 0, err
	}

	if len(files) == 0 {
		w.logger.Info().Msg("full scan triggered: no files to process")
		return 0, nil
	}

	w.logger.Info().
		Int("files_found", len(files)).
		Msg("full scan triggered, processing files")

	// Process the files immediately (using the same logic as processBatch)
	generator := retrieval.NewFileSummaryGenerator(
		w.store,
		w.llm,
		w.embed,
		w.workspace,
		w.logger,
	)

	var processed int
	for _, file := range files {
		select {
		case <-ctx.Done():
			return processed, ctx.Err()
		default:
		}

		// Rate limit
		w.limiter.wait()

		input, err := w.buildInput(file)
		if err != nil {
			w.logger.Debug().Err(err).Str("file", file).Msg("failed to build input")
			w.mu.Lock()
			w.errors++
			w.mu.Unlock()
			continue
		}

		_, cached, err := generator.GetOrCreateSummary(ctx, input)
		if err != nil {
			w.logger.Debug().Err(err).Str("file", file).Msg("failed to generate summary")
			w.mu.Lock()
			w.errors++
			w.mu.Unlock()
			continue
		}

		if !cached {
			w.mu.Lock()
			w.processed++
			w.mu.Unlock()
			processed++
			w.logger.Debug().Str("file", file).Msg("generated summary")
		}
	}

	return processed, nil
}

// Stats returns current worker statistics.
func (w *Worker) Stats() (processed, errors int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.processed, w.errors
}

func (w *Worker) run(ctx context.Context) {
	defer close(w.doneCh)
	defer func() {
		// Reset running flag on exit so Start() can be used again
		w.mu.Lock()
		w.running = false
		w.mu.Unlock()
	}()

	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	// Initial scan
	w.processBatch(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *Worker) processBatch(ctx context.Context) {
	start := time.Now()

	files, err := w.scanWorkspace(ctx)
	if err != nil {
		w.logger.Error().Err(err).Msg("failed to scan workspace")
		w.emitBatchEvent(ctx, 0, 0, 1, time.Since(start), err)
		return
	}

	if len(files) == 0 {
		return
	}

	// Limit to batch size
	if len(files) > w.config.BatchSize {
		files = files[:w.config.BatchSize]
	}

	w.logger.Debug().
		Int("files", len(files)).
		Msg("processing batch")

	generator := retrieval.NewFileSummaryGenerator(
		w.store,
		w.llm,
		w.embed,
		w.workspace,
		w.logger,
	)

	var batchProcessed, batchErrors int

	for _, file := range files {
		select {
		case <-ctx.Done():
			w.emitBatchEvent(ctx, batchProcessed, len(files)-batchProcessed-batchErrors, batchErrors, time.Since(start), ctx.Err())
			return
		case <-w.stopCh:
			w.emitBatchEvent(ctx, batchProcessed, len(files)-batchProcessed-batchErrors, batchErrors, time.Since(start), nil)
			return
		default:
		}

		// Rate limit
		w.limiter.wait()

		input, err := w.buildInput(file)
		if err != nil {
			w.logger.Debug().Err(err).Str("file", file).Msg("failed to build input")
			w.mu.Lock()
			w.errors++
			w.mu.Unlock()
			batchErrors++
			continue
		}

		_, cached, err := generator.GetOrCreateSummary(ctx, input)
		if err != nil {
			w.logger.Debug().Err(err).Str("file", file).Msg("failed to generate summary")
			w.mu.Lock()
			w.errors++
			w.mu.Unlock()
			batchErrors++
			continue
		}

		if !cached {
			w.mu.Lock()
			w.processed++
			w.mu.Unlock()
			batchProcessed++
			w.logger.Debug().Str("file", file).Msg("generated summary")
		}
	}

	// Emit wide event for completed batch
	w.emitBatchEvent(ctx, batchProcessed, 0, batchErrors, time.Since(start), nil)
}

// emitBatchEvent emits a wide event for batch processing.
func (w *Worker) emitBatchEvent(ctx context.Context, processed, skipped, errors int, duration time.Duration, err error) {
	// Only emit if something interesting happened
	if processed == 0 && errors == 0 {
		return
	}

	event := observability.NewEvent("filesummary.batch").
		WithComponent("worker").
		WithWorkspace(w.workspace).
		WithData("processed", processed).
		WithData("skipped", skipped).
		WithData("errors", errors).
		EnrichFromEnv().
		EnrichFromContext(ctx)

	if err != nil {
		observability.Emit(ctx, event.Error(err, duration))
	} else {
		observability.Emit(ctx, event.Success(duration))
	}
}

func (w *Worker) scanWorkspace(ctx context.Context) ([]string, error) {
	var files []string

	extSet := make(map[string]bool)
	for _, ext := range w.config.FileExtensions {
		extSet[ext] = true
	}

	excludeSet := make(map[string]bool)
	for _, dir := range w.config.ExcludeDirs {
		excludeSet[dir] = true
	}

	err := filepath.Walk(w.workspace, func(path string, info os.FileInfo, err error) error {
		// Check for context cancellation periodically
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil // Skip errors
		}

		if info.IsDir() {
			if excludeSet[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(path)
		if !extSet[ext] {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(w.workspace, path)
		if err != nil {
			return nil
		}

		// Check if summary exists using the parent context
		entryName := symbol.FileSummaryEntryName(w.workspace, relPath)
		_, err = w.store.Get(ctx, entryName, w.workspace)
		if err != nil {
			// No summary exists - add to list
			files = append(files, relPath)
		}

		return nil
	})

	return files, err
}

func (w *Worker) buildInput(relPath string) (symbol.FileSummaryInput, error) {
	fullPath := filepath.Join(w.workspace, relPath)

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return symbol.FileSummaryInput{}, err
	}

	// Extract basic info with symbols hash for cache invalidation
	input := symbol.FileSummaryInput{
		FilePath:    relPath,
		SymbolsHash: symbol.ComputeSymbolsHash(content, relPath),
	}

	// Try to extract package name for Go files
	if strings.HasSuffix(relPath, ".go") {
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "package ") {
				input.Package = strings.TrimPrefix(line, "package ")
				break
			}
		}
	}

	// Extract first comment block
	input.FirstComment = extractFirstComment(string(content))

	// Extract top symbol names (simplified)
	input.TopSymbols = extractTopSymbols(string(content), relPath)

	return input, nil
}

// extractFirstComment extracts the first comment block from the file.
func extractFirstComment(content string) string {
	lines := strings.Split(content, "\n")
	var comment strings.Builder
	inBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines at start
		if trimmed == "" && comment.Len() == 0 {
			continue
		}

		// Block comment
		if strings.HasPrefix(trimmed, "/*") {
			inBlock = true
			trimmed = strings.TrimPrefix(trimmed, "/*")
		}
		if inBlock {
			if idx := strings.Index(trimmed, "*/"); idx >= 0 {
				comment.WriteString(strings.TrimSpace(trimmed[:idx]))
				break
			}
			comment.WriteString(strings.TrimSpace(trimmed))
			comment.WriteString(" ")
			continue
		}

		// Line comment
		if strings.HasPrefix(trimmed, "//") {
			comment.WriteString(strings.TrimSpace(strings.TrimPrefix(trimmed, "//")))
			comment.WriteString(" ")
			continue
		}

		// Non-comment line - stop
		if comment.Len() > 0 || !strings.HasPrefix(trimmed, "package") {
			break
		}
	}

	result := strings.TrimSpace(comment.String())
	if len(result) > 200 {
		result = result[:200]
	}
	return result
}

// extractTopSymbols extracts top-level symbol names from the file.
func extractTopSymbols(content string, path string) []string {
	var symbols []string
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Go functions/types
		if strings.HasPrefix(trimmed, "func ") {
			name := extractGoFuncName(trimmed)
			if name != "" && isExported(name) {
				symbols = append(symbols, name)
			}
		} else if strings.HasPrefix(trimmed, "type ") {
			name := extractGoTypeName(trimmed)
			if name != "" && isExported(name) {
				symbols = append(symbols, name)
			}
		}

		// TypeScript/JavaScript
		if strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx") ||
			strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".jsx") {
			if strings.HasPrefix(trimmed, "export ") {
				name := extractTSExportName(trimmed)
				if name != "" {
					symbols = append(symbols, name)
				}
			}
		}

		// Limit symbols
		if len(symbols) >= 10 {
			break
		}
	}

	return symbols
}

func extractGoFuncName(line string) string {
	// func Name( or func (r *Receiver) Name(
	line = strings.TrimPrefix(line, "func ")
	if strings.HasPrefix(line, "(") {
		// Method - skip receiver
		idx := strings.Index(line, ")")
		if idx >= 0 {
			line = strings.TrimSpace(line[idx+1:])
		}
	}
	idx := strings.Index(line, "(")
	if idx > 0 {
		return line[:idx]
	}
	return ""
}

func extractGoTypeName(line string) string {
	// type Name struct/interface
	line = strings.TrimPrefix(line, "type ")
	parts := strings.Fields(line)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func extractTSExportName(line string) string {
	line = strings.TrimPrefix(line, "export ")
	line = strings.TrimPrefix(line, "default ")

	if strings.HasPrefix(line, "function ") {
		line = strings.TrimPrefix(line, "function ")
		idx := strings.Index(line, "(")
		if idx > 0 {
			return line[:idx]
		}
	}
	if strings.HasPrefix(line, "const ") || strings.HasPrefix(line, "let ") || strings.HasPrefix(line, "var ") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			return strings.TrimSuffix(parts[1], ":")
		}
	}
	if strings.HasPrefix(line, "class ") || strings.HasPrefix(line, "interface ") || strings.HasPrefix(line, "type ") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			return strings.TrimSuffix(parts[1], "{")
		}
	}
	return ""
}

func isExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}

// rateLimiter provides simple rate limiting.
type rateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	lastCall time.Time
}

func newRateLimiter(rps float64) *rateLimiter {
	interval := time.Duration(float64(time.Second) / rps)
	return &rateLimiter{interval: interval}
}

func (r *rateLimiter) wait() {
	r.mu.Lock()
	defer r.mu.Unlock()

	elapsed := time.Since(r.lastCall)
	if elapsed < r.interval {
		time.Sleep(r.interval - elapsed)
	}
	r.lastCall = time.Now()
}
