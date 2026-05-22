package runservice

import (
	"context"
	"io"

	"github.com/joshka0/foxctl/internal/platform/config"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/runtime/trajectorycapture"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/jobs"
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

	jobStore skillJobStore

	trajCapture *trajectorycapture.RunCapture

	asyncRunner AsyncRunner
}

type skillJobStore interface {
	storage.JobStore
	FindOrPrepareSkillJob(ctx context.Context, name string, input []byte, dedupe bool) (jobs.Job, bool, error)
	ExecutePreparedSkill(ctx context.Context, jobID, manifestPath, artifactPath string) ([]byte, error)
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
	if e.jobStore != nil {
		errs.Ignore(e.jobStore.Close(), "close job store")
		e.jobStore = nil
	}
	if e.trajCapture != nil {
		errs.Ignore(e.trajCapture.Close(), "close trajectory capture")
		e.trajCapture = nil
	}
}
