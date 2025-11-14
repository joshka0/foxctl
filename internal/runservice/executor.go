package runservice

import (
	"context"
	"io"

	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage"
)

// AsyncRunner schedules asynchronous job execution.
type AsyncRunner func(ctx context.Context, jobID, manifestPath, artifactPath string, stderr io.Writer) error

// Executor coordinates the lifecycle of a run invocation.
type Executor struct {
	ctx    context.Context
	cfg    config.Config
	handle SkillHandle

	stdout io.Writer
	stderr io.Writer

	options RunOptions

	cacheStore storage.CacheStore
	cacheKey   string

	jobStore storage.JobStore

	asyncRunner AsyncRunner
}

// NewExecutor constructs a new Executor for the provided configuration and options.
func NewExecutor(ctx context.Context, cfg config.Config, handle SkillHandle, stdout, stderr io.Writer, opts RunOptions) *Executor {
	return &Executor{
		ctx:     ctx,
		cfg:     cfg,
		handle:  handle,
		stdout:  stdout,
		stderr:  stderr,
		options: opts,
	}
}

// SetAsyncRunner overrides the asynchronous runner implementation used by SubmitAsync.
func (e *Executor) SetAsyncRunner(r AsyncRunner) {
	e.asyncRunner = r
}

// Close releases any resources held by the executor.
func (e *Executor) Close() {
	if e.cacheStore != nil {
		errs.Ignore(e.cacheStore.Close(), "close cache store")
		e.cacheStore = nil
	}
	if e.jobStore != nil {
		errs.Ignore(e.jobStore.Close(), "close job store")
		e.jobStore = nil
	}
}
