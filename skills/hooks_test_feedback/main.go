// Package main implements the hooks/test_feedback skill.
// This skill surfaces failing test results to Claude after code edits.
// It is advisory only (never blocks) and provides test failure context via PostToolUse.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/hookutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/jkatigb/agentctl/internal/context/sessionkit"
	"github.com/jkatigb/agentctl/internal/storage/testwatch"
)

// FeedbackConfig holds configuration for the test feedback hook.
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
// WatcherFeedback represents test feedback for a single watcher.
type WatcherFeedback struct {
	WatcherID string              `json:"watcher_id"`
	Status    string              `json:"status"`
	Summary   string              `json:"summary"`
	Failures  []testwatch.Failure `json:"failures,omitempty"`
}

// main is the skill entry point for hooks/test_feedback.
func main() {
	skillmain.Main("hooks/test_feedback", run)
}

// run orchestrates test failure feedback collection and formatting for advisory context.
//
// Index:
// - Purpose: Surface failing test results to Claude after code edits with advisory context
// - Flow: load config → open store → get test statuses → filter failures → build context → emit advisory output
// - SideEffects: test status querying; context formatting; failure limiting
// - FailureModes: store access failures, missing test watchers, status loading errors
// - Observability: emits test failure counts, watcher summaries, and formatted failure context
// - Related: buildContextString
// - Keywords: hooks/test_feedback, test_failures, advisory_context, test_watching, failure_reporting
func run(ctx context.Context, rc *skillmain.RunContext, in hooks.Input) error {
	paths := sessionkit.ResolvePaths(rc.Config)
	feedbackCfg := DefaultConfig()

	// Load custom config from environment if available
	if maxFail := os.Getenv("AGENTCTL_TEST_FEEDBACK_MAX_FAILURES"); maxFail != "" {
		var m int
		if _, err := fmt.Sscanf(maxFail, "%d", &m); err == nil && m > 0 {
			feedbackCfg.MaxFailures = m
		}
	}

	// Open test status store
	store, err := testwatch.Open(ctx, paths.StorageRoot)
	if err != nil {
		// If store doesn't exist yet, emit none with no context
		output := hooks.NewNone()
		output.Reason = "test watch store not initialized"
		return emitOutput(rc, output, nil)
	}
	defer store.Close()

	workspaceRoot := hookutil.ResolveWorkspaceRoot(in, "")
	workspaceID := hookutil.ResolveWorkspaceIDHash(in, workspaceRoot)

	// Get all test statuses for this workspace
	statuses, err := store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		output := hooks.NewNone()
		output.Reason = fmt.Sprintf("failed to load test status: %v", err)
		return emitOutput(rc, output, nil)
	}

	if len(statuses) == 0 {
		output := hooks.NewNone()
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
		output := hooks.NewNone()
		output.Reason = "all tests passing"
		return emitOutput(rc, output, nil)
	}

	// Build context string for Claude
	contextStr := buildContextString(failingWatchers, feedbackCfg)

	// Build output
	output := hooks.NewNone() // Advisory only, never blocks
	output.Reason = fmt.Sprintf("tests failing in %d watcher(s)", len(failingWatchers))
	output.Context = contextStr

	meta := map[string]any{
		"watchers": failingWatchers,
	}

	return emitOutput(rc, output, meta)
}

// buildContextString formats test failure context for Claude with watcher summaries and failure details.
//
//nolint:revive // strings.Builder.WriteString never returns an error for in-memory writes.
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
				sb.WriteString(fmt.Sprintf(" (%s)", skillout.TruncateString(f.Message, 100)))
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

// emitOutput emits the hook output with optional metadata.
func emitOutput(rc *skillmain.RunContext, output hooks.Output, meta map[string]any) error {
	var extras map[string]any
	if meta != nil {
		extras = map[string]any{"meta": meta}
	}
	return hookutil.EmitOutput(rc, "hooks/test_feedback", output, extras)
}
