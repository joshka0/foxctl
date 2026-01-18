// Package summary provides background worker for session window summarization.
package summary

import "time"

const (
	// QueueDBName is the SQLite database file for the summary queue.
	QueueDBName = "summary_queue.db"
	// QueueTable is the table name within the queue database.
	QueueTable = "summary_queue_jobs"
)

// WorkerConfig configures the background summary worker.
type WorkerConfig struct {
	// Workers is the number of concurrent workers.
	Workers int `json:"workers" yaml:"workers"`

	// BatchSize is the max jobs to dispatch per poll cycle.
	BatchSize int `json:"batch_size" yaml:"batch_size"`

	// PollInterval is how often to check for new jobs when idle.
	PollInterval time.Duration `json:"poll_interval,format:units" yaml:"poll_interval"`

	// RateLimitRPS is the max requests per second to the LLM provider.
	RateLimitRPS float64 `json:"rate_limit_rps" yaml:"rate_limit_rps"`

	// ShutdownTimeout is how long to wait for graceful shutdown.
	ShutdownTimeout time.Duration `json:"shutdown_timeout,format:units" yaml:"shutdown_timeout"`
}

// DefaultWorkerConfig returns sensible defaults for the summary worker.
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		Workers:         2,
		BatchSize:       5,
		PollInterval:    10 * time.Second,
		RateLimitRPS:    2.0, // conservative for LLM calls
		ShutdownTimeout: 30 * time.Second,
	}
}

// WindowPayload is the job payload for window summarization jobs.
type WindowPayload struct {
	SessionID   string `json:"session_id"`
	WindowIndex int    `json:"window_index"`
	Force       bool   `json:"force,omitempty"`
}

// JobResult represents the result of processing a single window job.
type JobResult struct {
	JobID       string `json:"job_id"`
	SessionID   string `json:"session_id"`
	WindowIndex int    `json:"window_index"`
	Success     bool   `json:"success"`
	Error       string `json:"error,omitempty"`
	Skipped     bool   `json:"skipped,omitempty"`
	SkipReason  string `json:"skip_reason,omitempty"`
}

// Stats tracks worker processing statistics.
type Stats struct {
	Processed int `json:"processed"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
}
