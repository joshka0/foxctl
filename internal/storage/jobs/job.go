package jobs

import "github.com/joshka0/foxctl/internal/storage/jobs/types"

// State re-exports the canonical job state enumeration for callers of package jobs.
type State = types.State

const (
	// StateQueued indicates a job waiting to run.
	StateQueued = types.StateQueued
	// StateRunning indicates a job currently executing.
	StateRunning = types.StateRunning
	// StateOK indicates a job that completed successfully.
	StateOK = types.StateOK
	// StateError indicates a job that failed.
	StateError = types.StateError
	// StateCanceled indicates a job canceled by the user.
	StateCanceled = types.StateCanceled
)

// Job re-exports the job metadata type for compatibility with previous API.
type Job = types.Job

var (
	// ErrNotFound surfaces when a requested job id does not exist.
	ErrNotFound = types.ErrNotFound
	// ErrInvalidState is returned when a transition is disallowed.
	ErrInvalidState = types.ErrInvalidState
)
