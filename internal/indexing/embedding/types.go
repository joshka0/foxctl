// Package embedding provides a background queue for generating symbol embeddings.
package embedding

import (
	"encoding/json"
	"time"

	"github.com/jkatigb/agentctl/internal/queue"
)

// JobState represents the state of an embedding job.
type JobState = queue.JobState

const (
	// StateQueued indicates the job is waiting to be processed.
	StateQueued = queue.StateQueued

	// StateRunning indicates the job is currently being processed.
	StateRunning = queue.StateRunning

	// StateOK indicates the job completed successfully.
	StateOK = queue.StateOK

	// StateError indicates the job failed.
	StateError = queue.StateError

	// StateRetry indicates the job will be retried.
	StateRetry = queue.StateRetry
)

// JobPriority determines processing order.
type JobPriority = queue.JobPriority

const (
	// PriorityLow for background batch processing.
	PriorityLow = queue.PriorityLow

	// PriorityNormal for standard requests.
	PriorityNormal = queue.PriorityNormal

	// PriorityHigh for user-initiated requests.
	PriorityHigh = queue.PriorityHigh
)

// EmbeddingJob represents a single embedding generation request.
type EmbeddingJob struct {
	// ID is the unique job identifier (ULID).
	ID string `json:"id"`

	// WorkspaceID identifies the workspace.
	WorkspaceID string `json:"workspace_id"`

	// SymbolID is the unique symbol identifier (file_path:symbol_name).
	SymbolID string `json:"symbol_id"`

	// FilePath is the source file path.
	FilePath string `json:"file_path"`

	// SymbolName is the symbol name.
	SymbolName string `json:"symbol_name"`

	// Content is the text to embed (symbol body or snippet).
	Content string `json:"content"`

	// ContentDigest is the SHA256 of the content for deduplication.
	ContentDigest string `json:"content_digest"`

	// State is the current job state.
	State JobState `json:"state"`

	// Priority determines processing order.
	Priority JobPriority `json:"priority"`

	// Attempts is the number of times this job has been tried.
	Attempts int `json:"attempts"`

	// MaxAttempts is the maximum retry count.
	MaxAttempts int `json:"max_attempts"`

	// Error contains the last error message if failed.
	Error string `json:"error,omitempty"`

	// CreatedAt is when the job was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the job was last updated.
	UpdatedAt time.Time `json:"updated_at"`

	// ScheduledAt is when the job should be processed (for retries).
	ScheduledAt time.Time `json:"scheduled_at,omitempty"`

	// CompletedAt is when the job finished.
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// EmbeddingResult stores the generated embedding for a symbol.
type EmbeddingResult struct {
	// SymbolID is the unique symbol identifier.
	SymbolID string `json:"symbol_id"`

	// WorkspaceID identifies the workspace.
	WorkspaceID string `json:"workspace_id"`

	// FilePath is the source file path.
	FilePath string `json:"file_path"`

	// Embedding is the vector representation.
	Embedding []float32 `json:"embedding"`

	// ContentDigest is the SHA256 of the embedded content.
	ContentDigest string `json:"content_digest"`

	// Model is the embedding model used.
	Model string `json:"model"`

	// Dimensions is the embedding vector size.
	Dimensions int `json:"dimensions"`

	// CreatedAt is when the embedding was generated.
	CreatedAt time.Time `json:"created_at"`
}

// EnqueueRequest is the input for enqueuing symbols for embedding.
type EnqueueRequest struct {
	// WorkspaceID identifies the workspace.
	WorkspaceID string `json:"workspace_id"`

	// Symbols is the list of symbols to embed.
	Symbols []SymbolInput `json:"symbols"`

	// Priority sets the job priority.
	Priority JobPriority `json:"priority,omitempty"`

	// Deduplicate skips symbols with unchanged content.
	Deduplicate bool `json:"deduplicate,omitempty"`
}

// SymbolInput describes a symbol to be embedded.
type SymbolInput struct {
	// SymbolID is the unique symbol identifier.
	SymbolID string `json:"symbol_id"`

	// FilePath is the source file path.
	FilePath string `json:"file_path"`

	// SymbolName is the symbol name.
	SymbolName string `json:"symbol_name"`

	// Content is the text to embed.
	Content string `json:"content"`
}

// EnqueueResult is the output after enqueuing symbols.
type EnqueueResult struct {
	// Queued is the number of jobs added to the queue.
	Queued int `json:"queued"`

	// Skipped is the number of symbols skipped (duplicates).
	Skipped int `json:"skipped"`

	// JobIDs are the IDs of the created jobs.
	JobIDs []string `json:"job_ids,omitempty"`
}

// QueueStats contains queue statistics.
type QueueStats struct {
	// QueuedCount is the number of jobs waiting.
	QueuedCount int `json:"queued_count"`

	// RunningCount is the number of jobs in progress.
	RunningCount int `json:"running_count"`

	// CompletedCount is the total completed jobs.
	CompletedCount int `json:"completed_count"`

	// FailedCount is the total failed jobs.
	FailedCount int `json:"failed_count"`

	// EmbeddingsCount is the total stored embeddings.
	EmbeddingsCount int `json:"embeddings_count"`

	// OldestQueuedAt is when the oldest queued job was created.
	OldestQueuedAt *time.Time `json:"oldest_queued_at,omitempty"`
}

// MarshalResult marshals an EmbeddingResult to JSON.
func MarshalResult(r EmbeddingResult) ([]byte, error) {
	return json.Marshal(r)
}

// UnmarshalResult unmarshals JSON to an EmbeddingResult.
func UnmarshalResult(data []byte) (*EmbeddingResult, error) {
	var r EmbeddingResult
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
