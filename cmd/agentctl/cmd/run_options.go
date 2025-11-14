package cmd

import (
	"fmt"

	"github.com/jkatigb/agentctl/internal/cache"
)

// RunOptions captures the configurable behavior for a run invocation.
type RunOptions struct {
	// SkillName is the name of the skill to execute
	SkillName string

	// Input is the raw input data to pass to the skill
	Input []byte

	// Async determines if the job should run asynchronously
	Async bool

	// Dedupe determines if deduplication should be enabled
	Dedupe bool

	// CacheMode controls cache behavior (auto, off, only)
	CacheMode cache.Mode

	// Workspace is the workspace path for the execution
	Workspace string

	// RememberName is the name to save the result as in memory
	RememberName string

	// RememberType is the type of memory entry (e.g., "result", "artifact")
	RememberType string

	// RememberSummary is an optional summary for the memory entry
	RememberSummary string
}

// Validate checks if the RunOptions are valid and returns an error if not.
func (o RunOptions) Validate() error {
	if o.SkillName == "" {
		return fmt.Errorf("skill name cannot be empty")
	}

	// Validate cache mode combinations
	if o.Async && o.CacheMode == cache.ModeOnly {
		return fmt.Errorf("--cache=only cannot be combined with --async")
	}

	// Validate remember combinations
	if o.Async && o.RememberName != "" {
		return fmt.Errorf("--remember cannot be used with --async")
	}

	// Validate cache mode
	switch o.CacheMode {
	case cache.ModeAuto, cache.ModeOff, cache.ModeOnly:
		// valid modes
	default:
		return fmt.Errorf("invalid cache mode: %v", o.CacheMode)
	}

	return nil
}
