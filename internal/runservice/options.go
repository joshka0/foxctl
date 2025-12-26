package runservice

import (
	"fmt"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/cache"
)

// DefaultTimeout is the default execution timeout if none is specified.
const DefaultTimeout = 2 * time.Minute

// RunOptions captures the configurable behavior for a run invocation.
type RunOptions struct {
	// SkillName is the name of the skill to execute.
	SkillName     string
	CLICommand    string
	CorrelationID string

	// Input is the raw input data to pass to the skill.
	Input []byte

	// Async determines if the job should run asynchronously.
	Async bool

	// Dedupe determines if deduplication should be enabled.
	Dedupe bool

	// CacheMode controls cache behavior (auto, off, only).
	CacheMode cache.Mode

	// Workspace is the workspace path for the execution.
	Workspace string

	// RememberName is the name to save the result as in memory.
	RememberName string

	// RememberType is the type of memory entry (e.g., "result", "artifact").
	RememberType string

	// RememberSummary is an optional summary for the memory entry.
	RememberSummary string

	// Timeout is the maximum duration for the execution. Zero means use DefaultTimeout.
	Timeout time.Duration

	// SessionID is the AI coding tool session ID for trajectory tracking.
	// Resolved from environment variables if not set explicitly.
	SessionID string
}

// Validate checks if the RunOptions are valid and returns an error if not.
func (o *RunOptions) Validate() error {
	if o == nil {
		return fmt.Errorf("options cannot be nil")
	}
	if o.CacheMode == "" {
		o.CacheMode = cache.ModeAuto
	}
	if o.SkillName == "" {
		return fmt.Errorf("skill name cannot be empty")
	}

	if o.Async && o.CacheMode == cache.ModeOnly {
		return fmt.Errorf("--cache=only cannot be combined with --async")
	}
	if o.Async && o.RememberName != "" {
		return fmt.Errorf("--remember cannot be used with --async")
	}

	switch o.CacheMode {
	case cache.ModeAuto, cache.ModeOff, cache.ModeOnly:
	default:
		return fmt.Errorf("invalid cache mode: %v", o.CacheMode)
	}

	return nil
}
