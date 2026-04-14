// Package testwatch implements the test watcher runtime.
// It monitors file changes, runs test commands with debouncing and throttling,
// and persists results to the test status store.
package testwatch

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/joshka0/foxctl/internal/storage/testwatch"
	"github.com/rs/zerolog"
)

// MaxRawTailBytes is the maximum size of raw_tail to store.
const MaxRawTailBytes = 16 * 1024

// Runner manages test watcher execution for a workspace.
type Runner struct {
	workspaceID   string
	workspaceRoot string
	config        *testwatch.Config
	store         testwatch.Store
	log           zerolog.Logger

	mu       sync.Mutex
	watchers map[string]*watcherState
}

type watcherState struct {
	cfg          testwatch.WatcherConfig
	lastRunStart time.Time
	running      bool
	pending      bool
	timer        *time.Timer
}

// NewRunner creates a new test watcher runner.
func NewRunner(workspaceID, workspaceRoot string, cfg *testwatch.Config, store testwatch.Store, log zerolog.Logger) *Runner {
	r := &Runner{
		workspaceID:   workspaceID,
		workspaceRoot: workspaceRoot,
		config:        cfg,
		store:         store,
		log:           log,
		watchers:      make(map[string]*watcherState),
	}

	for _, w := range cfg.Watchers {
		r.watchers[w.ID] = &watcherState{cfg: w}
	}

	return r
}

// RunOnce runs all watchers once (for --once mode).
func (r *Runner) RunOnce(ctx context.Context) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(r.config.Watchers))

	for _, w := range r.config.Watchers {
		wg.Add(1)
		go func(wcfg testwatch.WatcherConfig) {
			defer wg.Done()
			if err := r.runWatcher(ctx, wcfg); err != nil {
				errCh <- fmt.Errorf("watcher %s: %w", wcfg.ID, err)
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	// Return first error if any
	for err := range errCh {
		return err
	}
	return nil
}

// OnFileChange is called when a file changes. It schedules relevant watchers.
func (r *Runner) OnFileChange(ctx context.Context, path string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	relPath, err := filepath.Rel(r.workspaceRoot, path)
	if err != nil {
		relPath = path
	}

	for id, state := range r.watchers {
		matched, err := r.matchesWatcher(relPath, state.cfg)
		if err != nil {
			r.log.Warn().Err(err).Str("path", relPath).Str("watcher", id).Msg("pattern match error")
			continue
		}
		if matched {
			r.scheduleWatcher(ctx, id, state)
		}
	}
}

func (r *Runner) matchesWatcher(relPath string, cfg testwatch.WatcherConfig) (bool, error) {
	// If no include patterns, match everything
	if len(cfg.Include) == 0 {
		return true, nil
	}

	// Check include patterns
	for _, pattern := range cfg.Include {
		matched, err := doublestar.Match(pattern, relPath)
		if err != nil {
			return false, fmt.Errorf("match pattern %q for %q: %w", pattern, relPath, err)
		}
		if matched {
			// Check exclude patterns
			for _, excl := range cfg.Exclude {
				excluded, err := doublestar.Match(excl, relPath)
				if err != nil {
					return false, fmt.Errorf("match exclude pattern %q for %q: %w", excl, relPath, err)
				}
				if excluded {
					return false, nil
				}
			}
			return true, nil
		}
	}
	return false, nil
}

func (r *Runner) scheduleWatcher(ctx context.Context, id string, state *watcherState) {
	debounce := state.cfg.EffectiveDebounce(r.config)

	// Cancel existing timer
	if state.timer != nil {
		state.timer.Stop()
	}

	// Mark as pending
	state.pending = true

	// Schedule run after debounce
	state.timer = time.AfterFunc(debounce, func() {
		r.triggerWatcher(ctx, id)
	})

	r.log.Debug().
		Str("watcher", id).
		Dur("debounce", debounce).
		Msg("scheduled watcher")
}

func (r *Runner) triggerWatcher(ctx context.Context, id string) {
	r.mu.Lock()
	state, ok := r.watchers[id]
	if !ok {
		r.mu.Unlock()
		return
	}

	// Check if already running
	if state.running {
		// Will run again after current run finishes
		state.pending = true
		r.mu.Unlock()
		return
	}

	// Check min_interval
	minInterval := state.cfg.EffectiveMinInterval()
	elapsed := time.Since(state.lastRunStart)
	if elapsed < minInterval {
		// Schedule for later
		delay := minInterval - elapsed
		state.timer = time.AfterFunc(delay, func() {
			r.triggerWatcher(ctx, id)
		})
		r.mu.Unlock()
		return
	}

	state.running = true
	state.pending = false
	state.lastRunStart = time.Now()
	cfg := state.cfg
	r.mu.Unlock()

	// Run the watcher
	if err := r.runWatcher(ctx, cfg); err != nil {
		r.log.Error().Err(err).Str("watcher", id).Msg("watcher run failed")
	}

	// Check if we need to run again
	r.mu.Lock()
	state.running = false
	if state.pending {
		// Schedule another run
		r.scheduleWatcher(ctx, id, state)
	}
	r.mu.Unlock()
}

func (r *Runner) runWatcher(ctx context.Context, cfg testwatch.WatcherConfig) error {
	r.log.Info().
		Str("watcher", cfg.ID).
		Str("command", cfg.Command).
		Msg("running test watcher")

	startedAt := time.Now()

	// Mark as running in store
	if err := r.store.Upsert(ctx, testwatch.TestStatus{
		WorkspaceID: r.workspaceID,
		WatcherID:   cfg.ID,
		Status:      testwatch.StatusRunning,
		Command:     cfg.Command,
		StartedAt:   &startedAt,
	}); err != nil {
		r.log.Warn().Err(err).Msg("failed to mark watcher as running")
	}

	// Run the command
	cmd := exec.CommandContext(ctx, "sh", "-c", cfg.Command)
	cmd.Dir = r.workspaceRoot

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	finishedAt := time.Now()

	// Combine output
	combined := stdout.String() + stderr.String()

	// Determine status
	var status testwatch.Status
	switch {
	case err == nil:
		status = testwatch.StatusPass
	case isExitError(err):
		status = testwatch.StatusFail
	default:
		status = testwatch.StatusError
	}

	// Parse output for failures and summary
	failures, summary := parseTestOutput(cfg.ID, combined, status)

	// Truncate raw_tail
	rawTail := combined
	if len(rawTail) > MaxRawTailBytes {
		rawTail = "... (truncated)\n" + rawTail[len(rawTail)-MaxRawTailBytes:]
	}

	// Store result
	ts := testwatch.TestStatus{
		WorkspaceID: r.workspaceID,
		WatcherID:   cfg.ID,
		Status:      status,
		Command:     cfg.Command,
		StartedAt:   &startedAt,
		FinishedAt:  &finishedAt,
		Summary:     summary,
		Failures:    failures,
		RawTail:     rawTail,
	}

	if err := r.store.Upsert(ctx, ts); err != nil {
		return fmt.Errorf("store result: %w", err)
	}

	r.log.Info().
		Str("watcher", cfg.ID).
		Str("status", string(status)).
		Str("summary", summary).
		Dur("duration", finishedAt.Sub(startedAt)).
		Msg("watcher completed")

	return nil
}

// parseTestOutput attempts to extract failures and a summary from test output.
// This is a best-effort parser that handles Go, pytest, and jest/vitest formats.
func parseTestOutput(watcherID, output string, status testwatch.Status) ([]testwatch.Failure, string) {
	var failures []testwatch.Failure
	var summary string

	lines := strings.Split(output, "\n")

	switch {
	case strings.Contains(watcherID, "go") || strings.Contains(output, "--- FAIL:"):
		failures, summary = parseGoTestOutput(lines)
	case strings.Contains(watcherID, "python") || strings.Contains(watcherID, "pytest") ||
		strings.Contains(output, "FAILED") && strings.Contains(output, "::"):
		failures, summary = parsePytestOutput(lines)
	case strings.Contains(watcherID, "js") || strings.Contains(watcherID, "ts") ||
		strings.Contains(output, "FAIL") && strings.Contains(output, "✕"):
		failures, summary = parseJestOutput(lines)
	default:
		// Generic fallback based on overall status
		switch status {
		case testwatch.StatusFail:
			summary = "tests failed"
		case testwatch.StatusPass:
			summary = "tests passed"
		default:
			summary = "test run completed with errors"
		}
	}

	return failures, summary
}

var (
	goFailPattern     = regexp.MustCompile(`--- FAIL: (\S+)`)
	goFileLinePattern = regexp.MustCompile(`(\S+\.go):(\d+)`)
)

func parseGoTestOutput(lines []string) ([]testwatch.Failure, string) {
	var failures []testwatch.Failure
	var summary string

	for i, line := range lines {
		if match := goFailPattern.FindStringSubmatch(line); match != nil {
			f := testwatch.Failure{Name: match[1]}

			// Look ahead for file:line and message
			for j := i + 1; j < len(lines) && j < i+10; j++ {
				nextLine := lines[j]
				if strings.HasPrefix(nextLine, "---") {
					break
				}
				if flMatch := goFileLinePattern.FindStringSubmatch(nextLine); flMatch != nil {
					f.File = flMatch[1]
					// Best-effort line number parsing; errors leave default 0.
					_, _ = fmt.Sscanf(flMatch[2], "%d", &f.Line) //nolint:errcheck
				}
				if strings.Contains(nextLine, "Error") || strings.Contains(nextLine, "expected") ||
					strings.Contains(nextLine, "got") {
					f.Message = strings.TrimSpace(nextLine)
					break
				}
			}
			failures = append(failures, f)
		}

		// Look for summary line
		if strings.HasPrefix(line, "FAIL") || strings.HasPrefix(line, "ok") ||
			strings.Contains(line, "passed") || strings.Contains(line, "failed") {
			summary = strings.TrimSpace(line)
		}
	}

	if summary == "" && len(failures) > 0 {
		summary = fmt.Sprintf("%d test(s) failed", len(failures))
	}

	return failures, summary
}

var pytestFailPattern = regexp.MustCompile(`FAILED\s+(\S+)::(\S+)`)

// pytestFileLinePattern is reserved for future use to extract line numbers from pytest output.
//
//nolint:unused
var pytestFileLinePattern = regexp.MustCompile(`(\S+\.py):(\d+)`)

func parsePytestOutput(lines []string) ([]testwatch.Failure, string) {
	var failures []testwatch.Failure
	var summary string

	for _, line := range lines {
		if match := pytestFailPattern.FindStringSubmatch(line); match != nil {
			f := testwatch.Failure{
				File: match[1],
				Name: match[2],
			}
			failures = append(failures, f)
		}

		// Look for summary line like "1 failed, 5 passed"
		if strings.Contains(line, "passed") || strings.Contains(line, "failed") ||
			strings.Contains(line, "error") {
			if strings.Contains(line, "=") {
				summary = strings.TrimSpace(line)
			}
		}
	}

	if summary == "" && len(failures) > 0 {
		summary = fmt.Sprintf("%d test(s) failed", len(failures))
	}

	return failures, summary
}

var (
	jestFailPattern     = regexp.MustCompile(`✕\s+(.+)`)
	jestFileLinePattern = regexp.MustCompile(`at\s+.+\((.+):(\d+):\d+\)`)
)

func parseJestOutput(lines []string) ([]testwatch.Failure, string) {
	var failures []testwatch.Failure
	var summary string

	for i, line := range lines {
		if match := jestFailPattern.FindStringSubmatch(line); match != nil {
			f := testwatch.Failure{Name: strings.TrimSpace(match[1])}

			// Look ahead for file:line
			for j := i + 1; j < len(lines) && j < i+5; j++ {
				if flMatch := jestFileLinePattern.FindStringSubmatch(lines[j]); flMatch != nil {
					f.File = flMatch[1]
					// Best-effort line number parsing; errors leave default 0.
					_, _ = fmt.Sscanf(flMatch[2], "%d", &f.Line) //nolint:errcheck
					break
				}
			}
			failures = append(failures, f)
		}

		// Look for summary
		if strings.Contains(line, "Tests:") && (strings.Contains(line, "passed") || strings.Contains(line, "failed")) {
			summary = strings.TrimSpace(line)
		}
	}

	if summary == "" && len(failures) > 0 {
		summary = fmt.Sprintf("%d test(s) failed", len(failures))
	}

	return failures, summary
}

// isExitError returns true if the error is an exec.ExitError with non-zero exit code.
func isExitError(err error) bool {
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode() != 0
	}
	return false
}

// Stop stops all watchers and cleans up.
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, state := range r.watchers {
		if state.timer != nil {
			state.timer.Stop()
		}
	}
}
