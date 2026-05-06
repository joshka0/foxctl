package indexing

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rs/zerolog"
)

// PostReviewHandler coordinates post-review indexers.
// It is called by the overseer/kernel when a review passes and changes are accepted.
type PostReviewHandler struct {
	config   PostReviewConfig
	indexers map[string]Indexer
	logger   zerolog.Logger

	mu sync.RWMutex
}

// NewPostReviewHandler creates a new handler with the given configuration.
func NewPostReviewHandler(cfg PostReviewConfig, logger zerolog.Logger) *PostReviewHandler {
	return &PostReviewHandler{
		config:   cfg,
		indexers: make(map[string]Indexer),
		logger:   logger.With().Str("component", "post_review_handler").Logger(),
	}
}

// RegisterIndexer adds an indexer to the handler.
// The indexer's ID must match a configured indexer ID to be activated.
func (h *PostReviewHandler) RegisterIndexer(indexer Indexer) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	id := indexer.ID()
	if _, exists := h.indexers[id]; exists {
		return fmt.Errorf("indexer %q already registered", id)
	}

	h.indexers[id] = indexer
	h.logger.Info().Str("indexer_id", id).Msg("registered indexer")
	return nil
}

// Handle processes a post-review event by invoking all enabled indexers.
// If the handler is configured for async operation, this returns immediately
// after dispatching the work; otherwise it blocks until all indexers complete.
//
// Index:
//   Purpose: Dispatch post-review events to enabled indexers
//   Keywords: post_review, indexers, fanout_mode, workspace_id, review_id
//   Related: PostReviewHandler.runIndexers, Indexer.Index
//   Flow: validate config/event → resolve enabled indexers → select mode → run indexers or spawn async
//   Resources: registered indexers, config
//   Events: post-review-handled
//   OutputFields: PostReviewResult
//
// [[protocol:post-review-handler-dispatch]]
// [[invariant:empty-files-skips-indexers]]
func (h *PostReviewHandler) Handle(ctx context.Context, event PostReviewEvent) (*PostReviewResult, error) {
	if !h.config.Enabled {
		h.logger.Debug().Msg("post-review indexing disabled, skipping")
		return &PostReviewResult{Skipped: true, Reason: "disabled"}, nil
	}

	if len(event.Files) == 0 {
		h.logger.Debug().Msg("no files in event, skipping")
		return &PostReviewResult{Skipped: true, Reason: "no_files"}, nil
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	// Find enabled indexers that match configuration
	var activeIndexers []indexerWithConfig
	for _, cfg := range h.config.Indexers {
		if !cfg.Enabled {
			continue
		}
		indexer, ok := h.indexers[cfg.ID]
		if !ok {
			h.logger.Warn().Str("indexer_id", cfg.ID).Msg("configured indexer not registered")
			continue
		}
		activeIndexers = append(activeIndexers, indexerWithConfig{indexer: indexer, config: cfg})
	}

	if len(activeIndexers) == 0 {
		h.logger.Debug().Msg("no active indexers")
		return &PostReviewResult{Skipped: true, Reason: "no_active_indexers"}, nil
	}

	mode := h.config.EffectiveMode()

	h.logger.Info().
		Str("workspace_id", event.WorkspaceID).
		Str("task_id", event.TaskID).
		Str("review_id", event.ReviewID).
		Str("mode", string(mode)).
		Int("file_count", len(event.Files)).
		Int("indexer_count", len(activeIndexers)).
		Msg("handling post-review event")

	switch mode {
	case FanoutModeInline:
		// Run synchronously in the current goroutine
		return h.runIndexers(ctx, event, activeIndexers), nil

	case FanoutModeJobs:
		// Jobs mode: should enqueue one job per indexer with concurrency cap.
		// TODO(phase2-c2): Wire to jobs system with WFQ scheduler and
		// ConcurrencyPerIndexer cap. See deferred.md D3.
		// For now, fall back to async goroutine to unblock callers.
		if h.config.Async {
			go func() {
				result := h.runIndexers(ctx, event, activeIndexers)
				if result.HasFailures() {
					h.logger.Warn().
						Str("event_id", event.ID).
						Int("failures", len(result.IndexerResults)).
						Msg("async indexers completed with failures")
				} else {
					h.logger.Info().
						Str("event_id", event.ID).
						Int("files_indexed", result.TotalFilesIndexed()).
						Msg("async indexers completed")
				}
			}()
			return &PostReviewResult{Async: true}, nil
		}
		// If Async is false but mode is jobs, run sync as fallback
		return h.runIndexers(ctx, event, activeIndexers), nil

	default:
		// Should not happen if Validate() was called, but be defensive
		h.logger.Warn().Str("mode", string(mode)).Msg("unknown mode, running inline")
		return h.runIndexers(ctx, event, activeIndexers), nil
	}
}

type indexerWithConfig struct {
	indexer Indexer
	config  IndexerConfig
}

// runIndexers executes all active indexers and collects results.
//
// Index:
//   Purpose: Execute each enabled indexer and aggregate results
//   Keywords: indexer_results, files_indexed, files_skipped, files_failed
//   Related: PostReviewHandler.filterEvent, Indexer.Index
//   Flow: filter files per indexer → run indexer → record result → continue
//   Resources: indexer-specific stores/files
//   Events: indexer-run-complete
//   OutputFields: PostReviewResult
//
// [[protocol:indexer-fanout-execution]]
// [[invariant:per-indexer-file-filtering]]
func (h *PostReviewHandler) runIndexers(ctx context.Context, event PostReviewEvent, indexers []indexerWithConfig) *PostReviewResult {
	result := &PostReviewResult{
		IndexerResults: make([]IndexerResult, 0, len(indexers)),
	}

	for _, ic := range indexers {
		// Filter files for this indexer based on its configuration
		filteredEvent := h.filterEvent(event, ic.config)
		if len(filteredEvent.Files) == 0 {
			h.logger.Debug().
				Str("indexer_id", ic.indexer.ID()).
				Msg("no files match indexer filters")
			continue
		}

		h.logger.Debug().
			Str("indexer_id", ic.indexer.ID()).
			Int("file_count", len(filteredEvent.Files)).
			Msg("running indexer")

		indexerResult, err := ic.indexer.Index(ctx, filteredEvent)
		if err != nil {
			h.logger.Error().
				Str("indexer_id", ic.indexer.ID()).
				Err(err).
				Msg("indexer failed")
			result.IndexerResults = append(result.IndexerResults, IndexerResult{
				IndexerID: ic.indexer.ID(),
				Error:     err.Error(),
			})
			continue
		}

		if indexerResult != nil {
			indexerResult.IndexerID = ic.indexer.ID()
			result.IndexerResults = append(result.IndexerResults, *indexerResult)

			h.logger.Info().
				Str("indexer_id", ic.indexer.ID()).
				Int("files_indexed", indexerResult.FilesIndexed).
				Int("files_skipped", indexerResult.FilesSkipped).
				Int("files_failed", indexerResult.FilesFailed).
				Msg("indexer completed")
		} else {
			h.logger.Info().
				Str("indexer_id", ic.indexer.ID()).
				Msg("indexer completed with nil result")
		}
	}

	return result
}

// filterEvent returns a copy of the event with files filtered by indexer configuration.
func (h *PostReviewHandler) filterEvent(event PostReviewEvent, cfg IndexerConfig) PostReviewEvent {
	filtered := PostReviewEvent{
		WorkspaceID: event.WorkspaceID,
		TaskID:      event.TaskID,
		ReviewID:    event.ReviewID,
		Reason:      event.Reason,
		Files:       make([]FileChange, 0, len(event.Files)),
	}

	for _, file := range event.Files {
		if h.shouldIncludeFile(file, cfg) {
			filtered.Files = append(filtered.Files, file)
		}
	}

	return filtered
}

// shouldIncludeFile checks if a file matches the indexer's include/exclude patterns.
func (h *PostReviewHandler) shouldIncludeFile(file FileChange, cfg IndexerConfig) bool {
	// Check max file size
	if cfg.MaxFileKB > 0 && file.SizeBytes > int64(cfg.MaxFileKB)*1024 {
		return false
	}

	// Check include globs (if any specified, file must match at least one)
	if len(cfg.IncludeGlobs) > 0 {
		matched := false
		for _, pattern := range cfg.IncludeGlobs {
			// filepath.Match error is safe to ignore for validated patterns.
			if m, _ := filepath.Match(pattern, file.Path); m { //nolint:errcheck
				matched = true
				break
			}
			// Also try matching with double-star pattern expansion
			if matchDoubleGlob(pattern, file.Path) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check exclude globs (if any match, exclude the file)
	for _, pattern := range cfg.ExcludeGlobs {
		// filepath.Match error is safe to ignore for validated patterns.
		if m, _ := filepath.Match(pattern, file.Path); m { //nolint:errcheck
			return false
		}
		if matchDoubleGlob(pattern, file.Path) {
			return false
		}
	}

	return true
}

// matchDoubleGlob provides basic support for **/* patterns.
// For production, consider using doublestar or similar library.
func matchDoubleGlob(pattern, path string) bool {
	// Simple implementation: if pattern contains **, try prefix/suffix matching
	// This is a simplified heuristic; production code should use a proper glob library
	if len(pattern) < 3 {
		return false
	}

	// Handle **/*.ext patterns (e.g., "**/*.go")
	if strings.HasPrefix(pattern, "**/") {
		suffix := pattern[3:]
		// Match any path ending with suffix
		// filepath.Match error is safe to ignore for validated patterns.
		if m, _ := filepath.Match(suffix, filepath.Base(path)); m { //nolint:errcheck
			return true
		}
	}

	// Handle dir/** patterns (e.g., "vendor/**")
	if strings.HasSuffix(pattern, "/**") {
		prefix := pattern[:len(pattern)-3]
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return false
}

// PostReviewResult contains the aggregated results from all indexers.
type PostReviewResult struct {
	// Skipped is true if indexing was skipped entirely.
	Skipped bool `json:"skipped,omitempty"`

	// Reason explains why indexing was skipped (if Skipped is true).
	Reason string `json:"reason,omitempty"`

	// Async is true if indexing was dispatched asynchronously.
	Async bool `json:"async,omitempty"`

	// IndexerResults contains results from each indexer.
	IndexerResults []IndexerResult `json:"indexer_results,omitempty"`
}

// TotalFilesIndexed returns the sum of files indexed across all indexers.
func (r *PostReviewResult) TotalFilesIndexed() int {
	total := 0
	for _, ir := range r.IndexerResults {
		total += ir.FilesIndexed
	}
	return total
}

// HasFailures returns true if any indexer reported failures.
func (r *PostReviewResult) HasFailures() bool {
	for _, ir := range r.IndexerResults {
		if ir.FilesFailed > 0 || ir.Error != "" {
			return true
		}
	}
	return false
}
