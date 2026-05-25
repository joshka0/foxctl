package queue

import "time"

// JobState represents the state of a queue job.
type JobState string

const (
	// StateQueued indicates the job is waiting to be processed.
	StateQueued JobState = "queued"

	// StateRunning indicates the job is currently being processed.
	StateRunning JobState = "running"

	// StateOK indicates the job completed successfully.
	StateOK JobState = "ok"

	// StateError indicates the job failed.
	StateError JobState = "error"

	// StateRetry indicates the job will be retried.
	StateRetry JobState = "retry"
)

// JobPriority determines processing order.
type JobPriority int

const (
	// PriorityUnset is the zero-value sentinel; enqueue normalizes it to PriorityNormal.
	PriorityUnset JobPriority = 0

	// PriorityLow for background batch processing.
	PriorityLow JobPriority = 10

	// PriorityNormal for standard requests.
	PriorityNormal JobPriority = 50

	// PriorityHigh for user-initiated requests.
	PriorityHigh JobPriority = 100
)

// DefaultMaxAttempts is the default retry limit.
const DefaultMaxAttempts = 3

// Job represents a single queued job.
type Job struct {
	ID          string      `json:"id"`
	GroupID     string      `json:"group_id"`
	Payload     []byte      `json:"payload"`
	DedupeKey   string      `json:"dedupe_key"`
	State       JobState    `json:"state"`
	Priority    JobPriority `json:"priority"`
	Attempts    int         `json:"attempts"`
	MaxAttempts int         `json:"max_attempts"`
	Error       string      `json:"error,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	ScheduledAt time.Time   `json:"scheduled_at,omitempty"`
	CompletedAt *time.Time  `json:"completed_at,omitempty"`
}

// EnqueueRequest defines a job enqueue request.
type EnqueueRequest struct {
	GroupID     string      `json:"group_id"`
	Payload     []byte      `json:"payload"`
	DedupeKey   string      `json:"dedupe_key"`
	Priority    JobPriority `json:"priority,omitempty"`
	MaxAttempts int         `json:"max_attempts,omitempty"`
}

// EnqueueResult summarizes enqueue activity.
type EnqueueResult struct {
	Queued  int      `json:"queued"`
	Skipped int      `json:"skipped"`
	JobIDs  []string `json:"job_ids,omitempty"`
}

// Stats summarizes queue state.
type Stats struct {
	QueuedCount    int        `json:"queued_count"`
	RunningCount   int        `json:"running_count"`
	CompletedCount int        `json:"completed_count"`
	FailedCount    int        `json:"failed_count"`
	OldestQueuedAt *time.Time `json:"oldest_queued_at,omitempty"`
}

// ClaimOptions configures job claiming behavior.
type ClaimOptions struct {
	GroupID string
	// PayloadKind restricts claiming to JSON payloads with $.kind equal to this value.
	// Empty leaves payload shape unrestricted. "symbol" also matches legacy JSON payloads
	// without a kind field because old embedding jobs decoded as symbol tasks.
	PayloadKind string
}
