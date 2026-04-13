package annotations

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jkatigb/agentctl/internal/storage/queue"
)

const annotationQueueTable = "annotation_embedding_jobs"

// AnnotationEmbeddingPayload is the job payload for annotation embedding.
type AnnotationEmbeddingPayload struct {
	SessionID     string `json:"session_id"`
	TurnIndex     int    `json:"turn_index"`
	EmbeddingText string `json:"embedding_text"`
}

// AnnotationEmbeddingJob represents a claimed queue job.
type AnnotationEmbeddingJob struct {
	ID            string
	SessionID     string
	TurnIndex     int
	EmbeddingText string
}

// Queue wraps the generic queue for annotation embedding jobs.
type Queue struct {
	q *queue.Store
}

// OpenQueue opens or creates the annotation embedding queue.
// It stores the queue DB in the given root directory as annotation_queue.db.
func OpenQueue(ctx context.Context, root string) (*Queue, error) {
	store, err := queue.OpenInRoot(ctx, root, "annotation_queue.db", queue.Options{Table: annotationQueueTable})
	if err != nil {
		return nil, fmt.Errorf("open annotation queue: %w", err)
	}
	return &Queue{q: store}, nil
}

// Enqueue adds an annotation embedding job to the queue.
func (q *Queue) Enqueue(ctx context.Context, payload AnnotationEmbeddingPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	dedupeKey := fmt.Sprintf("annotation:%s:%d", payload.SessionID, payload.TurnIndex)
	_, err = q.q.EnqueueBatch(ctx, []queue.EnqueueRequest{
		{
			GroupID:   payload.SessionID,
			Payload:   data,
			DedupeKey: dedupeKey,
			Priority:  queue.PriorityNormal,
		},
	})
	if err != nil {
		return fmt.Errorf("enqueue annotation embedding job: %w", err)
	}
	return nil
}

// ClaimNext claims the next available annotation embedding job.
func (q *Queue) ClaimNext(ctx context.Context) (*AnnotationEmbeddingJob, error) {
	job, err := q.q.ClaimNext(ctx, queue.ClaimOptions{})
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, nil
	}

	var payload AnnotationEmbeddingPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		_ = q.q.Fail(ctx, job.ID, fmt.Sprintf("unmarshal payload: %v", err))
		return nil, fmt.Errorf("unmarshal job %s: %w", job.ID, err)
	}

	return &AnnotationEmbeddingJob{
		ID:            job.ID,
		SessionID:     payload.SessionID,
		TurnIndex:     payload.TurnIndex,
		EmbeddingText: payload.EmbeddingText,
	}, nil
}

// Complete marks a job as successfully completed.
func (q *Queue) Complete(ctx context.Context, jobID string) error {
	return q.q.Complete(ctx, jobID)
}

// Fail marks a job as failed with a reason.
func (q *Queue) Fail(ctx context.Context, jobID string, reason string) error {
	return q.q.Fail(ctx, jobID, reason)
}

// Close closes the queue.
func (q *Queue) Close() error {
	return q.q.Close()
}
