// Package jobs manages durable job metadata and persistence.
package jobs

import (
	"errors"
	"time"
)

// State represents the lifecycle of a job.
type State string

const (
	// StateQueued represents a job waiting to be executed.
	StateQueued State = "queued"
	// StateRunning represents a job currently executing.
	StateRunning State = "running"
	// StateOK indicates the job finished successfully.
	StateOK State = "ok"
	// StateError indicates the job failed with an error.
	StateError State = "error"
	// StateCanceled indicates the job was canceled by the user.
	StateCanceled State = "canceled"
)

// Job captures the persisted metadata for a job.
type Job struct {
	ID         string    `json:"id"`
	Command    string    `json:"command"`
	ArgsJSON   string    `json:"args_json"`
	ArgsHash   string    `json:"args_hash"`
	State      State     `json:"state"`
	ResultPath string    `json:"result_path,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

var (
	// ErrNotFound indicates the requested job id does not exist.
	ErrNotFound = errors.New("jobs: not found")
	// ErrInvalidState is returned when a transition is not allowed.
	ErrInvalidState = errors.New("jobs: invalid state transition")
)
