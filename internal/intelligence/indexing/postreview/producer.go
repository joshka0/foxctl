package postreview

import (
	"context"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/intelligence/indexing"
	"github.com/oklog/ulid/v2"
)

// EventSource identifies where a PostReviewEvent came from.
const EventSource = "review_gate_v1"

// Producer creates and stores PostReviewEvents when reviews pass.
// It is the bridge between the review gate (Phase 1) and the indexing
// pipeline (Phase 2+).
type Producer struct {
	store Store
}

// NewProducer creates a Producer backed by the given store.
func NewProducer(store Store) *Producer {
	return &Producer{store: store}
}

// BuildPostReviewEvent creates a PostReviewEvent from a ReviewArtifact.
// This is called when a review transitions to "ok" and the diff is applied.
//
// NOTE: The Files field is currently empty because the diff application layer
// does not exist yet. See docs/impl_plan/universal_swe_grep_and_agents_deferred.md D1.
func BuildPostReviewEvent(artifact agent.ReviewArtifact, files []indexing.FileChange) indexing.PostReviewEvent {
	now := time.Now().UTC()

	return indexing.PostReviewEvent{
		ID:            ulid.Make().String(),
		WorkspaceID:   artifact.WorkspaceID,
		TaskID:        artifact.TaskID,
		ReviewID:      artifact.ID,
		ReviewKind:    artifact.Kind,
		ReviewStatus:  "ok",
		DiffAppliedAt: now,
		Files:         files, // Populated by caller; empty until diff layer exists
		Source:        EventSource,
		Metadata:      nil, // Reserved for commit/branch info
		CreatedAt:     now,
		Sequence:      0, // Will be incremented per (workspace, task) when needed
	}
}

// Produce creates and persists a PostReviewEvent for the given artifact.
// If an event already exists for this (workspace, task, review) with the same
// payload, it returns the existing event (idempotent).
//
// The files parameter is currently expected to be nil/empty until the diff
// application layer is implemented. Indexers should handle empty file lists
// gracefully.
//
// Index:
//
//	Purpose: Build and persist post-review events for indexing
//	Keywords: post_review_event, workspace_id, review_id, files
//	Related: BuildPostReviewEvent, Store.Put
//	Flow: build event → store Put → return event
//	Resources: postreview event store
//	Events: post-review-event-produced
//	OutputFields: PostReviewEvent
//
// [[protocol:post-review-event-production]]
// [[invariant:idempotent-event-store]]
func (p *Producer) Produce(ctx context.Context, artifact agent.ReviewArtifact, files []indexing.FileChange) (indexing.PostReviewEvent, error) {
	event := BuildPostReviewEvent(artifact, files)
	return p.store.Put(ctx, event)
}

// ProduceFromReview is a convenience method that produces an event with an
// empty file list. This is the stub behavior until the diff layer exists.
//
// See: docs/impl_plan/universal_swe_grep_and_agents_deferred.md D1
func (p *Producer) ProduceFromReview(ctx context.Context, artifact agent.ReviewArtifact) (indexing.PostReviewEvent, error) {
	return p.Produce(ctx, artifact, nil)
}
