// Package main implements the hooks/test_feedback skill.
// This skill surfaces failing test results to Claude after code edits.
// It is advisory only (never blocks) and provides test failure context via PostToolUse.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/hook"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/testwatch"
)

// FeedbackConfig holds configuration for the test feedback hook.
type FeedbackConfig struct {
	MaxFailures int `json:"max_failures"` // Max failures to show per watcher
}

// DefaultConfig returns default feedback configuration.
func DefaultConfig() FeedbackConfig {
	return FeedbackConfig{
		MaxFailures: 3,
	}
}

// WatcherFeedback represents test feedback for a single watcher.
type WatcherFeedback struct {
	WatcherID string              `json:"watcher_id"`
	Status    string              `json:"status"`
	Summary   string              `json:"summary"`
	Failures  []testwatch.Failure `json:"failures,omitempty"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("hooks/test_feedback", "ECONFIG", err)
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("hooks/test_feedback", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	var in hook.Input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail("hooks/test_feedback", "EARG", fmt.Errorf("decode input: %w", err))
	}

	if err := run(ctx, rc, cfg, in); err != nil {
		fail("hooks/test_feedback", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, cfg config.Config, in hook.Input) error {
	feedbackCfg := DefaultConfig()

	// Load custom config from environment if available
	if maxFail := os.Getenv("AGENTCTL_TEST_FEEDBACK_MAX_FAILURES"); maxFail != "" {
		var m int
		if _, err := fmt.Sscanf(maxFail, "%d", &m); err == nil && m > 0 {
			feedbackCfg.MaxFailures = m
		}
	}

	// Open test status store
	store, err := testwatch.Open(ctx, cfg.Storage.Root)
	if err != nil {
		// If store doesn't exist yet, emit none with no context
		output := hook.NewNone()
		output.Reason = "test watch store not initialized"
		return emitOutput(rc, output, nil)
	}
	defer func() { _ = store.Close() }()

	// Derive workspace ID from workspace root
	workspaceID := deriveWorkspaceID(in.WorkspaceRoot)

	// Get all test statuses for this workspace
	statuses, err := store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		output := hook.NewNone()
		output.Reason = fmt.Sprintf("failed to load test status: %v", err)
		return emitOutput(rc, output, nil)
	}

	if len(statuses) == 0 {
		output := hook.NewNone()
		output.Reason = "no test watchers configured for this workspace"
		return emitOutput(rc, output, nil)
	}

	// Filter to failing watchers only
	var failingWatchers []WatcherFeedback
	for _, s := range statuses {
		if s.Status == testwatch.StatusFail || s.Status == testwatch.StatusError {
			feedback := WatcherFeedback{
				WatcherID: s.WatcherID,
				Status:    string(s.Status),
				Summary:   s.Summary,
			}

			// Limit failures
			if len(s.Failures) > feedbackCfg.MaxFailures {
				feedback.Failures = s.Failures[:feedbackCfg.MaxFailures]
			} else {
				feedback.Failures = s.Failures
			}

			failingWatchers = append(failingWatchers, feedback)
		}
	}

	// If no failures, emit none
	if len(failingWatchers) == 0 {
		output := hook.NewNone()
		output.Reason = "all tests passing"
		return emitOutput(rc, output, nil)
	}

	// Build context string for Claude
	contextStr := buildContextString(failingWatchers, feedbackCfg)

	// Build output
	output := hook.NewNone() // Advisory only, never blocks
	output.Reason = fmt.Sprintf("tests failing in %d watcher(s)", len(failingWatchers))
	output.Context = contextStr

	meta := map[string]any{
		"watchers": failingWatchers,
	}

	return emitOutput(rc, output, meta)
}

func buildContextString(watchers []WatcherFeedback, cfg FeedbackConfig) string {
	var sb strings.Builder

	sb.WriteString("Tests are currently failing:\n\n")

	for _, w := range watchers {
		sb.WriteString(fmt.Sprintf("**Watcher `%s`** (%s)\n", w.WatcherID, w.Summary))

		for _, f := range w.Failures {
			if f.File != "" && f.Line > 0 {
				sb.WriteString(fmt.Sprintf("- `%s` — `%s:%d`", f.Name, f.File, f.Line))
			} else if f.File != "" {
				sb.WriteString(fmt.Sprintf("- `%s` — `%s`", f.Name, f.File))
			} else {
				sb.WriteString(fmt.Sprintf("- `%s`", f.Name))
			}

			if f.Message != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", truncate(f.Message, 100)))
			}
			sb.WriteString("\n")
		}

		// Note if there are more failures than shown
		if len(w.Failures) >= cfg.MaxFailures {
			sb.WriteString("- ... and more failures. See raw output for details.\n")
		}

		sb.WriteString("\n")
	}

	sb.WriteString("You may want to address these failures before continuing.")

	return sb.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func emitOutput(rc *runner.RunnerContext, output hook.Output, meta map[string]any) error {
	data := map[string]any{
		"hook_output": output,
	}
	if meta != nil {
		data["meta"] = meta
	}

	return rc.Emit("hooks/test_feedback", data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit hook failure")
	os.Exit(1)
}

func deriveWorkspaceID(path string) string {
	h := sha256.Sum256([]byte(path))
	return "ws-" + hex.EncodeToString(h[:8])
}
