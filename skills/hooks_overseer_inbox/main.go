// Package main implements the hooks/overseer_inbox skill.
// This skill surfaces mailbox messages sent to "overseer" (or broadcast),
// enabling human-in-the-loop communication during agent runs.
//
// Environment variables:
//   - AGENTCTL_OVERSEER_RECIPIENT: Recipient to monitor (default: "overseer", use "*" for broadcast)
//   - AGENTCTL_OVERSEER_AUTOACK: Set to "0" to disable auto-ack (default: "1")
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/hook"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
)

const (
	// MaxMessagesInContext is the maximum number of messages to inject into context.
	MaxMessagesInContext = 10

	// DefaultRecipient is the default recipient to monitor.
	DefaultRecipient = "overseer"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("hooks/overseer_inbox", "ERUNTIME", err)
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("hooks/overseer_inbox", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	var in hook.Input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail("hooks/overseer_inbox", "EARG", fmt.Errorf("decode input: %w", err))
	}

	if err := run(ctx, rc, cfg, in); err != nil {
		fail("hooks/overseer_inbox", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, cfg config.Config, in hook.Input) error {
	// Get workspace ID using detection chain (hook input takes priority)
	workspaceID := in.WorkspaceRoot
	if workspaceID == "" {
		workspaceID = detectWorkspace()
	}

	// Get recipient to monitor from env, default to "overseer"
	recipient := os.Getenv("AGENTCTL_OVERSEER_RECIPIENT")
	if recipient == "" {
		recipient = DefaultRecipient
	}

	// Check if auto-ack is enabled (default: true)
	autoAck := os.Getenv("AGENTCTL_OVERSEER_AUTOACK") != "0"

	// Open board store
	boardStore, err := blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
	if err != nil {
		return emitOutput(rc, hook.Output{
			Decision: hook.DecisionNone,
			Reason:   "overseer_inbox: could not open board store",
		})
	}
	defer boardStore.Close()

	// Query inbox for messages to the recipient.
	// The SQL query uses (recipient = ? OR recipient = '*') so broadcast messages
	// are always included regardless of the ActorID value passed.
	filter := agent.InboxFilter{
		WorkspaceID: workspaceID,
		ActorID:     recipient, // e.g. "overseer" or "*" for broadcast
		OnlyUnread:  true,
		Limit:       MaxMessagesInContext * 2,
	}

	messages, err := boardStore.Inbox(ctx, filter)
	if err != nil {
		return emitOutput(rc, hook.Output{
			Decision: hook.DecisionNone,
			Reason:   "overseer_inbox: inbox query failed",
		})
	}

	if len(messages) == 0 {
		return emitOutput(rc, hook.Output{
			Decision: hook.DecisionNone,
			Reason:   "no overseer messages",
			Meta: map[string]any{
				"workspace_id": workspaceID,
				"recipient":    recipient,
			},
		})
	}

	// Build context string with messages
	contextStr := buildOverseerContext(messages, recipient)

	// Auto-ack displayed messages if enabled
	if autoAck && len(messages) > 0 {
		ids := make([]string, 0, minInt(len(messages), MaxMessagesInContext))
		for i, m := range messages {
			if i >= MaxMessagesInContext {
				break
			}
			ids = append(ids, m.ID)
		}
		// Mark as read using the recipient as the actor ID
		_, _ = boardStore.MarkRead(ctx, workspaceID, recipient, ids) //nolint:errcheck
	}

	return emitOutput(rc, hook.Output{
		Decision: hook.DecisionNone, // Advisory only - never block
		Reason:   fmt.Sprintf("surfaced %d overseer messages", minInt(len(messages), MaxMessagesInContext)),
		Context:  contextStr,
		Meta: map[string]any{
			"message_count": len(messages),
			"workspace_id":  workspaceID,
			"recipient":     recipient,
			"auto_ack":      autoAck,
		},
	})
}

// buildOverseerContext creates a formatted context string from overseer messages.
//
//nolint:revive // strings.Builder.WriteString never returns an error for in-memory writes.
func buildOverseerContext(messages []agent.BoardMessage, recipient string) string {
	if len(messages) == 0 {
		return ""
	}

	var sb strings.Builder

	// Header with inbox indicator
	sb.WriteString(fmt.Sprintf("## 📬 Overseer Inbox (%d unread)\n\n", len(messages)))
	sb.WriteString(fmt.Sprintf("_Messages addressed to: %s_\n\n", recipient))

	count := 0
	for _, msg := range messages {
		if count >= MaxMessagesInContext {
			break
		}

		// Priority indicator
		priorityLabel := priorityToEmoji(msg.Priority)

		// Ack indicator
		ackLabel := ""
		if msg.AckRequired {
			ackLabel = " ⚠️ [ACTION REQUIRED]"
		}

		// Kind indicator
		kindLabel := kindToLabel(msg.Kind)

		sb.WriteString(fmt.Sprintf("### %s %s%s\n", priorityLabel, msg.Subject, ackLabel))
		sb.WriteString(fmt.Sprintf("**From:** %s | **Kind:** %s\n", msg.Sender, kindLabel))

		if msg.Body != "" {
			sb.WriteString(fmt.Sprintf("\n%s\n", msg.Body))
		}

		sb.WriteString(fmt.Sprintf("\n_ID: %s | Stream: %s_\n\n", msg.ID, msg.Stream))
		sb.WriteString("---\n\n")

		count++
	}

	remaining := len(messages) - count
	if remaining > 0 {
		sb.WriteString(fmt.Sprintf("_...and %d more unread messages_\n", remaining))
	}

	return sb.String()
}

// priorityToEmoji converts priority level to emoji indicator.
func priorityToEmoji(priority int) string {
	switch priority {
	case 1:
		return "🔴 [P1]"
	case 2:
		return "🟠 [P2]"
	case 3:
		return "🟡 [P3]"
	case 4:
		return "🟢 [P4]"
	default:
		return "⚪ [P5]"
	}
}

// kindToLabel converts message kind to human-readable label.
func kindToLabel(kind agent.BoardMessageKind) string {
	switch kind {
	case agent.BoardMessageKindInstruction:
		return "Instruction"
	case agent.BoardMessageKindInfo:
		return "Info"
	case agent.BoardMessageKindAlert:
		return "Alert"
	case agent.BoardMessageKindReviewRequest:
		return "Review Request"
	default:
		return string(kind)
	}
}

func emitOutput(rc *runner.RunnerContext, output hook.Output) error {
	data := map[string]any{
		"hook_output": output,
	}
	return rc.Emit("hooks/overseer_inbox", data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit hook failure")
	os.Exit(1)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// detectWorkspace returns the workspace root using a detection chain:
// 1. AGENTCTL_WORKSPACE - set by agentctl runner
// 2. CLAUDE_PROJECT_DIR - set by Claude Code
// 3. Git root detection from current directory
// 4. Current working directory (last resort)
func detectWorkspace() string {
	// 1. AGENTCTL_WORKSPACE (highest priority - set by agentctl)
	if ws := os.Getenv("AGENTCTL_WORKSPACE"); ws != "" {
		return ws
	}

	// 2. CLAUDE_PROJECT_DIR (set by Claude Code)
	if projDir := os.Getenv("CLAUDE_PROJECT_DIR"); projDir != "" {
		return projDir
	}

	// Get current working directory for remaining checks
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	// 3. Git root detection
	if gitRoot := findGitRoot(cwd); gitRoot != "" {
		return gitRoot
	}

	// 4. Current working directory (last resort)
	return cwd
}

// findGitRoot walks up from the given path to find the .git directory.
func findGitRoot(path string) string {
	dir := path
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
