package agent

import "time"

// ReviewArtifact represents a single execution of the review pipeline for a task.
//
// This is a logical model used by the review gate and post-review pipeline. It is
// persisted via CAS in Phase 1 and may later gain a dedicated SQLite table.
type ReviewArtifact struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	TaskID      string    `json:"task_id"`
	Kind        string    `json:"kind,omitempty"` // auto|human|mixed
	Status      string    `json:"status"`         // ok|failed|pending
	Summary     string    `json:"summary,omitempty"`
	CASDigest   string    `json:"cas_digest,omitempty"` // Optional CAS digest for large review payloads
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by,omitempty"`
}
