// Package overseer provides task scoring and post-review event coordination.
package overseer

import (
	"context"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/indexing"
	"github.com/jkatigb/agentctl/internal/indexing/postreview"
	"github.com/jkatigb/agentctl/internal/trajectorycapture"
	"github.com/rs/zerolog"
)

// PostReviewHandler coordinates post-review event production and indexer fanout.
// It is the overseer's entrypoint for handling approved reviews.
//
// See: docs/spec/post_review_harness.md §6
type PostReviewHandler struct {
	producer       *postreview.Producer
	indexerHandler *indexing.PostReviewHandler
	config         indexing.PostReviewConfig
	logger         zerolog.Logger
	storageRoot    string

	// hooks for testing
	onEventProduced func(indexing.PostReviewEvent)
}

// PostReviewHandlerConfig contains configuration for the handler.
type PostReviewHandlerConfig struct {
	// EventStore is the store for PostReviewEvents.
	EventStore postreview.Store

	TrajectoryStorageRoot string

	// IndexerHandler is the handler that fans out to indexers.
	IndexerHandler *indexing.PostReviewHandler

	// Config is the post-review configuration.
	Config indexing.PostReviewConfig

	// Logger is the logger to use.
	Logger zerolog.Logger
}

// NewPostReviewHandler creates a new handler with the given configuration.
func NewPostReviewHandler(cfg PostReviewHandlerConfig) *PostReviewHandler {
	return &PostReviewHandler{
		producer:       postreview.NewProducer(cfg.EventStore),
		indexerHandler: cfg.IndexerHandler,
		config:         cfg.Config,
		logger:         cfg.Logger.With().Str("component", "overseer.post_review").Logger(),
		storageRoot:    strings.TrimSpace(cfg.TrajectoryStorageRoot),
	}
}

// HandleReviewApproved is called when a review transitions to "ok" and the
// diff is applied. It:
//  1. Builds and persists a PostReviewEvent (idempotent).
//  2. Delegates to the indexer handler for fanout.
//
// The files parameter contains the list of changed files. If nil/empty,
// indexers will be skipped (see deferred.md D1).
//
// This method is idempotent: calling it multiple times for the same
// (workspace, task, review) tuple will not create duplicate events.
func (h *PostReviewHandler) HandleReviewApproved(
	ctx context.Context,
	artifact agent.ReviewArtifact,
	files []indexing.FileChange,
) (*PostReviewResult, error) {
	if artifact.Status != "ok" {
		return nil, fmt.Errorf("overseer: review artifact status must be 'ok', got %q", artifact.Status)
	}

	h.logger.Info().
		Str("workspace_id", artifact.WorkspaceID).
		Str("task_id", artifact.TaskID).
		Str("review_id", artifact.ID).
		Str("review_kind", artifact.Kind).
		Int("file_count", len(files)).
		Msg("handling approved review")

	// 1. Build and persist the event (idempotent)
	event, err := h.producer.Produce(ctx, artifact, files)
	if err != nil {
		h.logger.Error().Err(err).
			Str("review_id", artifact.ID).
			Msg("failed to produce post-review event")
		return nil, fmt.Errorf("overseer: produce event: %w", err)
	}

	h.logger.Debug().
		Str("event_id", event.ID).
		Msg("post-review event produced")

	if h.storageRoot != "" {
		if err := trajectorycapture.CaptureReviewOutcome(ctx, h.storageRoot, artifact, event.ID); err != nil {
			h.logger.Error().Err(err).
				Str("workspace_id", artifact.WorkspaceID).
				Str("task_id", artifact.TaskID).
				Str("review_id", artifact.ID).
				Msg("failed to capture review outcome")
		}
	}

	// Invoke test hook if set
	if h.onEventProduced != nil {
		h.onEventProduced(event)
	}

	// 2. Delegate to indexer handler for fanout
	if h.indexerHandler == nil {
		h.logger.Debug().Msg("no indexer handler configured, skipping fanout")
		return &PostReviewResult{
			Event:   event,
			Skipped: true,
			Reason:  "no_indexer_handler",
		}, nil
	}

	indexResult, err := h.indexerHandler.Handle(ctx, event)
	if err != nil {
		h.logger.Error().Err(err).
			Str("event_id", event.ID).
			Msg("indexer handler failed")
		return &PostReviewResult{
			Event: event,
			Error: err.Error(),
		}, err
	}

	result := &PostReviewResult{
		Event:         event,
		IndexerResult: indexResult,
	}
	if indexResult != nil {
		result.Skipped = indexResult.Skipped
		result.Reason = indexResult.Reason
	}
	return result, nil
}

// HandleReviewApprovedStub is a convenience method for handling approved
// reviews without a file list. This is the stub behavior until the diff
// application layer is implemented.
//
// See: docs/impl_plan/universal_swe_grep_and_agents_deferred.md D1
func (h *PostReviewHandler) HandleReviewApprovedStub(
	ctx context.Context,
	artifact agent.ReviewArtifact,
) (*PostReviewResult, error) {
	return h.HandleReviewApproved(ctx, artifact, nil)
}

// SetTestHook sets a callback invoked when an event is produced.
// For testing only.
func (h *PostReviewHandler) SetTestHook(fn func(indexing.PostReviewEvent)) {
	h.onEventProduced = fn
}

// PostReviewResult contains the outcome of handling an approved review.
type PostReviewResult struct {
	// Event is the produced PostReviewEvent.
	Event indexing.PostReviewEvent `json:"event"`

	// IndexerResult is the result from the indexer handler (if invoked).
	IndexerResult *indexing.PostReviewResult `json:"indexer_result,omitempty"`

	// Skipped is true if indexer fanout was skipped.
	Skipped bool `json:"skipped,omitempty"`

	// Reason explains why fanout was skipped (if Skipped is true).
	Reason string `json:"reason,omitempty"`

	// Error is set if the handler failed.
	Error string `json:"error,omitempty"`
}
