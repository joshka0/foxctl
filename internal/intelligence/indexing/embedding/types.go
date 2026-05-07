package embedding

import (
	"encoding/json"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedqueue"
	"github.com/joshka0/foxctl/internal/storage/queue"
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

	// Kind identifies what target the job embeds.
	Kind embedqueue.TaskKind `json:"kind"`

	// WorkspaceID identifies the workspace.
	WorkspaceID string `json:"workspace_id"`

	// SymbolID is the embedding-storage symbol identifier. New keyed symbol jobs
	// use "<package_id>::<symbol_key>"; legacy jobs may still use file/name IDs.
	SymbolID string `json:"symbol_id"`

	// FilePath is the source file path.
	FilePath string `json:"file_path"`

	// SymbolName is the symbol name.
	SymbolName string `json:"symbol_name"`

	// Language is the source language for package-scoped symbol identity.
	Language string `json:"language,omitempty"`

	// PackageID is the package identifier for package-scoped symbol identity.
	PackageID string `json:"package_id,omitempty"`

	// SymbolKey is the canonical symbol key within PackageID.
	SymbolKey string `json:"symbol_key,omitempty"`

	// MemoryName is the named memory entry name for memory embedding jobs.
	MemoryName string `json:"memory_name,omitempty"`

	// MemoryType is the named memory type for memory embedding jobs.
	MemoryType string `json:"memory_type,omitempty"`

	// Content is the text to embed (symbol body or snippet).
	Content string `json:"content"`

	// ContentDigest is the SHA256 of the content for deduplication.
	ContentDigest string `json:"content_digest"`

	// Model is the queued embedding model identity used for deduplication.
	Model string `json:"model,omitempty"`

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
	// SymbolID is the embedding-storage symbol identifier.
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

	// Model is the embedding model used for deduplication checks.
	Model string `json:"model,omitempty"`

	// Deduplicate skips symbols with unchanged content.
	Deduplicate bool `json:"deduplicate,omitempty"`
}

// MemoryEnqueueRequest is the input for enqueuing named memories for embedding.
type MemoryEnqueueRequest struct {
	// WorkspaceID identifies the workspace.
	WorkspaceID string `json:"workspace_id"`

	// Memories is the list of named memories to embed.
	Memories []MemoryInput `json:"memories"`

	// Priority sets the job priority.
	Priority JobPriority `json:"priority,omitempty"`

	// Model is the embedding model used for deduplication checks.
	Model string `json:"model,omitempty"`
}

// SymbolInput describes a symbol to be embedded.
type SymbolInput struct {
	// SymbolID is the requested symbol identifier. When PackageID and SymbolKey
	// are present, the queue stores the embedding under "<package_id>::<symbol_key>".
	SymbolID string `json:"symbol_id"`

	// FilePath is the source file path.
	FilePath string `json:"file_path"`

	// SymbolName is the symbol name.
	SymbolName string `json:"symbol_name"`

	// Language is the source language for package-scoped symbol identity.
	Language string `json:"language,omitempty"`

	// PackageID is the package identifier for package-scoped symbol identity.
	PackageID string `json:"package_id,omitempty"`

	// SymbolKey is the canonical symbol key within PackageID.
	SymbolKey string `json:"symbol_key,omitempty"`

	// MemoryName is the canonical named-memory entry name for this symbol.
	MemoryName string `json:"memory_name,omitempty"`

	// Content is the text to embed.
	Content string `json:"content"`

	// ContentDigest is the SHA256 digest of the content for deduplication.
	// When empty, the queue will compute a digest from Content.
	ContentDigest string `json:"content_digest,omitempty"`
}

// MemoryInput describes a named memory to be embedded.
type MemoryInput struct {
	// Name is the stable named-memory entry name.
	Name string `json:"name"`

	// Type is the named-memory entry type.
	Type string `json:"type,omitempty"`

	// Content is the text to embed.
	Content string `json:"content"`

	// ContentDigest is the SHA256 digest of the content for deduplication.
	// When empty, the queue will compute a digest from Content.
	ContentDigest string `json:"content_digest,omitempty"`
}

// EnqueueResult is the output after enqueuing embedding jobs.
type EnqueueResult struct {
	// Queued is the number of jobs added to the queue.
	Queued int `json:"queued"`

	// Skipped is the number of jobs skipped (duplicates or empty content).
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
