// Package execution provides abstractions for skill execution, decoupling
// the persistence layer from execution details.
package execution

import (
	"context"
	"io"

	"github.com/jkatigb/agentctl/internal/domain/skill"
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
func (f ExecutorFunc) Execute(ctx context.Context, opts ExecuteOptions) (*Result, error) {
	return f(ctx, opts)
}
