package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/jkatigb/agentctl/internal/cache"
	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/jobs"
	"github.com/jkatigb/agentctl/internal/storage"
)

// runOptions captures the configurable behavior for a run invocation.
type runOptions struct {
	async           bool
	dedupe          bool
	cacheMode       cache.Mode
	workspace       string
	rememberName    string
	rememberType    string
	rememberSummary string
}

// runExecutor coordinates the lifecycle of a `run` command invocation.
type runExecutor struct {
	ctx    context.Context
	cfg    config.Config
	handle SkillHandle

	stdout io.Writer
	stderr io.Writer

	options runOptions

	cacheStore storage.CacheStore
	cacheKey   string

	jobStore storage.JobStore

	asyncRunner func(ctx context.Context, jobID, manifestPath, artifactPath string, stderr io.Writer) error
}

func newRunExecutor(ctx context.Context, cfg config.Config, handle SkillHandle, stdout, stderr io.Writer, opts runOptions) *runExecutor {
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

func defaultAsyncRunner(ctx context.Context, jobID, manifestPath, artifactPath string, stderr io.Writer) error {
	worker := exec.CommandContext(ctx, os.Args[0], "jobs", "exec-skill",
		"--job-id", jobID,
		"--manifest", manifestPath,
		"--artifact", artifactPath,
	)
	worker.Stdout = stderr
	worker.Stderr = stderr
	return worker.Start()
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

// tryServeCache attempts to respond from the cache based on the provided input.
func (e *runExecutor) tryServeCache(input []byte) (bool, error) {
	if e.options.async || e.options.cacheMode == cache.ModeOff {
		return false, nil
	}
	if e.cacheStore == nil {
		store, err := cache.Open(e.ctx, e.cfg.Paths.Cache, cache.Options{
			AutoTTL: e.cfg.Memory.AutoCacheTTL,
			CASPath: e.cfg.Paths.CAS,
		})
		if err != nil {
			return false, err
		}
		e.cacheStore = store
		key, err := cache.BuildKey(e.handle.Manifest, input, nil)
		if err != nil {
			return false, err
		}
		e.cacheKey = key
	}
	entry, ok, err := e.cacheStore.Get(e.ctx, e.cacheKey)
	if err != nil {
		return false, err
	}
	if ok {
		hit, err := cache.AnnotateHit(entry.Result, entry.CacheKey, e.options.workspace, e.handle.Manifest.Metadata.Version)
		if err != nil {
			return false, err
		}
		fmt.Fprintf(e.stderr, "cache hit %s\n", entry.CacheKey)
		if err := writeEnvelope(e.stdout, hit); err != nil {
			return true, err
		}
		return true, nil
	}
	if e.options.cacheMode == cache.ModeOnly {
		return false, fmt.Errorf("cache miss for key %s", e.cacheKey)
	}
	return false, nil
}

func (e *runExecutor) ensureJobStore() error {
	if e.jobStore != nil {
		return nil
	}
	store, err := jobs.Open(e.ctx, e.cfg.Paths.Jobs)
	if err != nil {
		return err
	}
	e.jobStore = store
	return nil
}

// prepareJob creates or finds an existing job for the given input.
func (e *runExecutor) prepareJob(input []byte) (jobs.Job, bool, error) {
	if err := e.ensureJobStore(); err != nil {
		return jobs.Job{}, false, err
	}
	job, dup, err := e.jobStore.FindOrPrepareSkillJob(e.ctx, e.handle.Manifest.Metadata.Name, input, e.options.dedupe)
	if err != nil {
		return job, dup, err
	}
	if e.options.workspace != "" {
		if err := e.jobStore.SetWorkspace(e.ctx, job.ID, e.options.workspace); err != nil {
			fmt.Fprintf(e.stderr, "warn: unable to persist workspace for job %s: %v\n", job.ID, err)
		}
	}
	return job, dup, nil
}

// handleDuplicate responds when a matching job already exists.
func (e *runExecutor) handleDuplicate(job jobs.Job) error {
	fmt.Fprintf(e.stderr, "using existing job %s (deduplicated)\n", job.ID)
	if e.options.async {
		fmt.Fprintf(e.stdout, "job %s (existing)\n", job.ID)
		return nil
	}
	if job.ResultPath != "" {
		result, err := e.jobStore.Result(e.ctx, job.ID)
		if err != nil {
			return err
		}
		if err := handleArtifacts(e.ctx, e.cfg, job.ID, result); err != nil {
			return err
		}
		annotated := annotateRunMeta(result, e.options.workspace, e.handle.Manifest.Metadata.Version)
		if err := e.persistCache(annotated); err != nil {
			fmt.Fprintf(e.stderr, "cache put failed: %v\n", err)
		}
		if err := e.remember(annotated); err != nil {
			fmt.Fprintf(e.stderr, "remember failed: %v\n", err)
		}
		return writeEnvelope(e.stdout, annotated)
	}
	waited, err := e.jobStore.WaitForCompletion(e.ctx, job.ID, 0)
	if err != nil {
		return err
	}
	job = waited
	result, err := e.jobStore.Result(e.ctx, job.ID)
	if err != nil {
		return err
	}
	if err := handleArtifacts(e.ctx, e.cfg, job.ID, result); err != nil {
		return err
	}
	annotated := annotateRunMeta(result, e.options.workspace, e.handle.Manifest.Metadata.Version)
	if err := e.persistCache(annotated); err != nil {
		fmt.Fprintf(e.stderr, "cache put failed: %v\n", err)
	}
	if err := e.remember(annotated); err != nil {
		fmt.Fprintf(e.stderr, "remember failed: %v\n", err)
	}
	return writeEnvelope(e.stdout, annotated)
}

// submitAsync schedules the job for asynchronous execution.
func (e *runExecutor) submitAsync(job jobs.Job) error {
	if err := e.asyncRunner(e.ctx, job.ID, e.handle.ManifestPath, e.handle.ArtifactPath, e.stderr); err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "job %s submitted\n", job.ID)
	return nil
}

// executeSync runs the skill synchronously and handles persistence side effects.
func (e *runExecutor) executeSync(job jobs.Job) error {
	result, runErr := e.jobStore.ExecutePreparedSkill(e.ctx, job.ID, e.handle.ManifestPath, e.handle.ArtifactPath)
	latest, getErr := e.jobStore.Get(e.ctx, job.ID)
	if getErr != nil {
		fmt.Fprintf(e.stderr, "warning: failed to fetch job %s after execution: %v\n", job.ID, getErr)
		if runErr == nil {
			return fmt.Errorf("execution completed but failed to fetch final job state for %s: %w", job.ID, getErr)
		}
	}
	if runErr != nil {
		return runErr
	}
	if err := handleArtifacts(e.ctx, e.cfg, job.ID, result); err != nil {
		return err
	}
	annotated := annotateRunMeta(result, e.options.workspace, e.handle.Manifest.Metadata.Version)
	if err := e.persistCache(annotated); err != nil {
		fmt.Fprintf(e.stderr, "cache put failed: %v\n", err)
	}
	if err := e.remember(annotated); err != nil {
		fmt.Fprintf(e.stderr, "remember failed: %v\n", err)
	}
	if err := writeEnvelope(e.stdout, annotated); err != nil {
		return err
	}
	if getErr == nil {
		fmt.Fprintf(e.stderr, "job %s state %s\n", latest.ID, latest.State)
	}
	return nil
}

func (e *runExecutor) persistCache(result []byte) error {
	if e.options.cacheMode != cache.ModeAuto || e.cacheStore == nil || e.cacheKey == "" {
		return nil
	}
	entry := cache.Entry{
		CacheKey:     e.cacheKey,
		SkillName:    e.handle.Manifest.Metadata.Name,
		SkillVersion: e.handle.Manifest.Metadata.Version,
		Workspace:    e.options.workspace,
		Result:       result,
		Digests:      cache.CollectDigests(result),
	}
	return e.cacheStore.Put(e.ctx, entry)
}

func (e *runExecutor) remember(result []byte) error {
	if strings.TrimSpace(e.options.rememberName) == "" {
		return nil
	}
	return rememberResult(e.ctx, e.cfg, e.options.rememberName, e.options.rememberType, e.options.rememberSummary, e.options.workspace, result)
}
