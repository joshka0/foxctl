package execution

import (
	"context"
	"io"

	"github.com/joshka0/foxctl/internal/domain/skill"
)

// SkillExecutor executes a skill and returns the execution result.
// This interface decouples the jobs persistence layer from the runner
// implementation, enabling dependency injection and simplified testing.
type SkillExecutor interface {
	Execute(ctx context.Context, opts ExecuteOptions) (*Result, error)
}

// ExecuteOptions contains all parameters for skill execution.
type ExecuteOptions struct {
	// Manifest provides an already-parsed manifest. When set, ManifestPath
	// is optional and runners can skip re-reading the manifest from disk.
	Manifest skill.Manifest

	// Skill identification
	ManifestPath string
	ArtifactPath string

	// Input/Output
	Input  []byte
	Stdout io.Writer
	Stderr io.Writer

	// ExtraEnv contains additional environment variables to pass to the skill.
	// These are added to the child process environment without modifying the
	// parent process, avoiding race conditions with concurrent executions.
	// Format: []string{"KEY=value", "KEY2=value2"}
	ExtraEnv []string

	// Resource limits (future use)
	MaxMemoryBytes uint64
	MaxCPUSeconds  uint64

	// Capabilities (future use)
	AllowNetwork    bool
	AllowFilesystem bool
}

// Result contains the execution result.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Error    error
}

// ExecutorFunc is a function adapter for SkillExecutor.
// This allows functions to implement the SkillExecutor interface.
type ExecutorFunc func(ctx context.Context, opts ExecuteOptions) (*Result, error)

// Execute implements the SkillExecutor interface.
//
// Index:
// - Purpose: Invoke a functional executor with standard options
// - Flow: call wrapped function → return result
// - SideEffects: depends on wrapped executor
// - FailureModes: wrapped executor errors
// - Related: SkillExecutor
// - Keywords: skill_execute, executor_func
func (f ExecutorFunc) Execute(ctx context.Context, opts ExecuteOptions) (*Result, error) {
	return f(ctx, opts)
}
