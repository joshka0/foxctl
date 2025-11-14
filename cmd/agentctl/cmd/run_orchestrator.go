package cmd

import (
	"context"
	"io"

	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/storage"
)

// runExecutor (RunOrchestrator) coordinates the lifecycle of a `run` command invocation.
// It manages cache, job store, and execution flow.
type runExecutor struct {
	ctx    context.Context
	cfg    config.Config
	handle SkillHandle

	stdout io.Writer
	stderr io.Writer

	options RunOptions

	cacheStore storage.CacheStore
	cacheKey   string

	jobStore storage.JobStore

	asyncRunner func(ctx context.Context, jobID, manifestPath, artifactPath string, stderr io.Writer) error
}

// newRunExecutor creates a new run executor with the given configuration.
func newRunExecutor(ctx context.Context, cfg config.Config, handle SkillHandle, stdout, stderr io.Writer, opts RunOptions) *runExecutor {
	exec := &runExecutor{
		ctx:         ctx,
		cfg:         cfg,
		handle:      handle,
		stdout:      stdout,
		stderr:      stderr,
		options:     opts,
		asyncRunner: defaultAsyncRunner,
	}
	return exec
}

// Close releases any resources held by the executor.
func (e *runExecutor) Close() {
	if e.cacheStore != nil {
		_ = e.cacheStore.Close()
		e.cacheStore = nil
	}
	if e.jobStore != nil {
		_ = e.jobStore.Close()
		e.jobStore = nil
	}
}
