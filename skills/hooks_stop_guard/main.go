// Package main implements the hooks/stop_guard skill.
// This hook blocks StopRequested until tests are green and review approval is present.
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/hookutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/jkatigb/agentctl/internal/storage/testwatch"
)

const skillName = "hooks/stop_guard"

type StopGuardConfig struct {
	RequireTests      bool   `json:"require_tests"`
	RequireReview     bool   `json:"require_review"`
	ReviewSubject     string `json:"review_subject"`
	ReviewSender      string `json:"review_sender"`
	ReviewKind        string `json:"review_kind"`
	ReviewRecipient   string `json:"review_recipient"`
	ReviewStream      string `json:"review_stream"`
	MaxFailures       int    `json:"max_failures"`
	MaxReviewMessages int    `json:"max_review_messages"`
}

func defaultStopGuardConfig() StopGuardConfig {
	return StopGuardConfig{
		RequireTests:      true,
		RequireReview:     true,
		ReviewSubject:     "review.approved",
		MaxFailures:       3,
		MaxReviewMessages: 50,
	}
}

func main() {
	skillmain.Main(skillName, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in hooks.Input) error {
	cfg := defaultStopGuardConfig()
	applyConfig(&cfg, in.HookConfig)

	if in.Event != "" && in.Event != hooks.EventStopRequested {
		return hookutil.EmitOutput(rc, skillName, hooks.NewNone(), nil)
	}

	paths := sessionkit.ResolvePaths(rc.Config)

	workspaceRoot := hookutil.ResolveWorkspaceRoot(in, "")
	workspaceID := hookutil.ResolveWorkspaceIDHash(in, workspaceRoot)

	var issues []string
	var contexts []string
	meta := map[string]any{
		"workspace_id": workspaceID,
		"actor_id":     in.ActorID,
	}

	if cfg.RequireTests {
		issue, context, testMeta := evaluateTests(ctx, paths.StorageRoot, workspaceID, cfg.MaxFailures)
		if issue != "" {
			issues = append(issues, issue)
		}
		if context != "" {
			contexts = append(contexts, context)
		}
		for k, v := range testMeta {
			meta[k] = v
		}
	}

	if cfg.RequireReview {
		issue, context, reviewMeta := evaluateReview(ctx, paths.StorageRoot, workspaceID, in.ActorID, cfg)
		if issue != "" {
			issues = append(issues, issue)
		}
		if context != "" {
			contexts = append(contexts, context)
		}
		for k, v := range reviewMeta {
			meta[k] = v
		}
	}

	if len(issues) > 0 {
		output := hooks.NewBlock(strings.Join(issues, "; "))
		output.Context = strings.Join(contexts, "\n\n")
		output.Meta = meta
		return hookutil.EmitOutput(rc, skillName, output, nil)
	}

	output := hooks.NewApprove("stop guard ok", meta)
	return hookutil.EmitOutput(rc, skillName, output, nil)
}

func evaluateTests(ctx context.Context, storageRoot, workspaceID string, maxFailures int) (string, string, map[string]any) {
	meta := map[string]any{
		"tests_required": true,
	}
	if workspaceID == "" {
		return "test status unavailable", "Workspace ID missing; cannot verify test status.", meta
	}

	store, err := testwatch.Open(ctx, storageRoot)
	if err != nil {
		return "test status unavailable", "Test watch store not initialized; configure test watchers or disable require_tests.", meta
	}
	defer store.Close()

	statuses, err := store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return "test status unavailable", fmt.Sprintf("Failed to load test status: %v", err), meta
	}
	if len(statuses) == 0 {
		return "tests not configured", "No test watchers configured for this workspace.", meta
	}

	var failing []testwatch.TestStatus
	passCount := 0
	for _, s := range statuses {
		if s.Status == testwatch.StatusPass {
			passCount++
			continue
		}
		failing = append(failing, s)
	}

	meta["tests_total"] = len(statuses)
	meta["tests_passing"] = passCount
	meta["tests_failing"] = len(failing)

	if len(failing) == 0 {
		return "", "", meta
	}

	return "tests not green", buildTestContext(failing, maxFailures), meta
}

func buildTestContext(statuses []testwatch.TestStatus, maxFailures int) string {
	var sb strings.Builder
	sb.WriteString("Tests are not green:\n\n")

	for _, s := range statuses {
		sb.WriteString(fmt.Sprintf("**Watcher `%s`** (%s)\n", s.WatcherID, s.Summary))
		failures := s.Failures
		if maxFailures > 0 && len(failures) > maxFailures {
			failures = failures[:maxFailures]
		}
		for _, f := range failures {
			if f.File != "" && f.Line > 0 {
				sb.WriteString(fmt.Sprintf("- `%s` — `%s:%d`", f.Name, f.File, f.Line))
			} else if f.File != "" {
				sb.WriteString(fmt.Sprintf("- `%s` — `%s`", f.Name, f.File))
			} else {
				sb.WriteString(fmt.Sprintf("- `%s`", f.Name))
			}
			if f.Message != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", skillout.TruncateString(f.Message, 120)))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Resolve failures or update the test watchers before stopping.")
	return sb.String()
}

func evaluateReview(
	ctx context.Context,
	storageRoot string,
	workspaceID string,
	actorID string,
	cfg StopGuardConfig,
) (string, string, map[string]any) {
	meta := map[string]any{
		"review_required": true,
		"review_subject":  cfg.ReviewSubject,
	}
	if workspaceID == "" {
		return "review approval missing", "Workspace ID missing; cannot verify review approval.", meta
	}

	recipient := cfg.ReviewRecipient
	if recipient == "" {
		recipient = actorID
	}
	if recipient == "" {
		recipient = agent.BroadcastRecipient
	}
	meta["review_recipient"] = recipient

	boardStore, err := blackboard.OpenBoardStore(ctx, storageRoot)
	if err != nil {
		return "review approval missing", "Board store not initialized; cannot verify review approval.", meta
	}
	defer boardStore.Close()

	filter := agent.InboxFilter{
		WorkspaceID: workspaceID,
		ActorID:     recipient,
		Limit:       cfg.MaxReviewMessages,
		Stream:      cfg.ReviewStream,
	}

	messages, err := boardStore.Inbox(ctx, filter)
	if err != nil {
		return "review approval missing", fmt.Sprintf("Failed to query review inbox: %v", err), meta
	}
	meta["review_messages_checked"] = len(messages)

	if hasReviewApproval(messages, cfg) {
		return "", "", meta
	}

	context := fmt.Sprintf("Awaiting review approval. Ask a reviewer to send a mailbox message with subject %q.", cfg.ReviewSubject)
	return "review approval missing", context, meta
}

func hasReviewApproval(messages []agent.BoardMessage, cfg StopGuardConfig) bool {
	for _, msg := range messages {
		if cfg.ReviewKind != "" && string(msg.Kind) != cfg.ReviewKind {
			continue
		}
		if cfg.ReviewSender != "" && msg.Sender != cfg.ReviewSender {
			continue
		}
		if !matchesSubject(msg.Subject, cfg.ReviewSubject) {
			continue
		}
		return true
	}
	return false
}

func matchesSubject(subject, expected string) bool {
	if expected == "" {
		return true
	}
	subject = strings.ToLower(strings.TrimSpace(subject))
	expected = strings.ToLower(strings.TrimSpace(expected))
	if subject == expected {
		return true
	}
	return strings.HasPrefix(subject, expected+":")
}

func applyConfig(cfg *StopGuardConfig, raw map[string]any) {
	if raw == nil {
		return
	}
	if v, ok := raw["require_tests"]; ok {
		if b, ok := asBool(v); ok {
			cfg.RequireTests = b
		}
	}
	if v, ok := raw["require_review"]; ok {
		if b, ok := asBool(v); ok {
			cfg.RequireReview = b
		}
	}
	if v, ok := raw["review_subject"].(string); ok && v != "" {
		cfg.ReviewSubject = v
	}
	if v, ok := raw["review_sender"].(string); ok && v != "" {
		cfg.ReviewSender = v
	}
	if v, ok := raw["review_kind"].(string); ok && v != "" {
		cfg.ReviewKind = v
	}
	if v, ok := raw["review_recipient"].(string); ok && v != "" {
		cfg.ReviewRecipient = v
	}
	if v, ok := raw["review_stream"].(string); ok && v != "" {
		cfg.ReviewStream = v
	}
	if v, ok := raw["max_failures"]; ok {
		if n, ok := asInt(v); ok && n >= 0 {
			cfg.MaxFailures = n
		}
	}
	if v, ok := raw["max_review_messages"]; ok {
		if n, ok := asInt(v); ok && n > 0 {
			cfg.MaxReviewMessages = n
		}
	}
}

func asBool(v any) (bool, bool) {
	switch val := v.(type) {
	case bool:
		return val, true
	case string:
		switch strings.ToLower(strings.TrimSpace(val)) {
		case "true", "1", "yes", "y":
			return true, true
		case "false", "0", "no", "n":
			return false, true
		}
	}
	return false, false
}

func asInt(v any) (int, bool) {
	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	case string:
		var out int
		if _, err := fmt.Sscanf(val, "%d", &out); err == nil {
			return out, true
		}
	}
	return 0, false
}
