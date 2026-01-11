// Package main implements the hooks/overseer_inbox skill.
// This skill surfaces mailbox messages sent to "overseer" (or broadcast),
// enabling human-in-the-loop communication during agent runs.
//
// Environment variables:
//   - AGENTCTL_OVERSEER_RECIPIENT: Recipient to monitor (default: "overseer", use "*" for broadcast)
//   - AGENTCTL_OVERSEER_AUTOACK: Set to "0" to disable auto-marking displayed messages as "surfaced" (default: "1")
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	wsutil "github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
)

const (
	// MaxMessagesInContext is the maximum number of messages to inject into context.
	MaxMessagesInContext = 10

	// DefaultRecipient is the default recipient to monitor.
	DefaultRecipient = "overseer"
)

func main() {
	skillmain.Main("hooks/overseer_inbox", run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in hooks.Input) error {
	paths := sessionkit.ResolvePaths(rc.Config)

	// Get workspace ID using detection chain (hook input takes priority)
	workspaceID := in.WorkspaceID
	if workspaceID == "" {
		workspaceID = in.WorkspaceRoot
	}
	if workspaceID == "" {
		workspaceID = wsutil.Detect("")
	}

	// Get recipient to monitor from env, default to "overseer"
	recipient := os.Getenv("AGENTCTL_OVERSEER_RECIPIENT")
	if recipient == "" {
		recipient = DefaultRecipient
	}

	// Check if auto-ack is enabled (default: true)
	autoAck := os.Getenv("AGENTCTL_OVERSEER_AUTOACK") != "0"

	// Open board store
	boardStore, err := blackboard.OpenBoardStore(ctx, paths.StorageRoot)
	if err != nil {
		return emitOutput(rc, hooks.Output{
			Decision: hooks.DecisionNone,
			Reason:   "overseer_inbox: could not open board store",
		})
	}
	defer boardStore.Close()

	// Query inbox for messages to the recipient.
	// The SQL query uses (recipient = ? OR recipient = '*') so broadcast messages
	// are always included regardless of the ActorID value passed.
	filter := agent.InboxFilter{
		WorkspaceID:    workspaceID,
		ActorID:        recipient, // e.g. "overseer" or "*" for broadcast
		OnlyUnread:     true,      // includes surfaced, but...
		OnlyUnsurfaced: true,      // ...hooks only want never-shown messages
		Limit:          MaxMessagesInContext * 2,
	}

	messages, err := boardStore.Inbox(ctx, filter)
	if err != nil {
		return emitOutput(rc, hooks.Output{
			Decision: hooks.DecisionNone,
			Reason:   "overseer_inbox: inbox query failed",
		})
	}

	if len(messages) == 0 {
		return emitOutput(rc, hooks.Output{
			Decision: hooks.DecisionNone,
			Reason:   "no overseer messages",
			Meta: map[string]any{
				"workspace_id": workspaceID,
				"recipient":    recipient,
			},
		})
	}

	// Build context string with messages
	contextStr := buildOverseerContext(messages, recipient)

	// Auto-mark displayed messages as "surfaced" if enabled
	if autoAck && len(messages) > 0 {
		ids := make([]string, 0, minInt(len(messages), MaxMessagesInContext))
		for i, m := range messages {
			if i >= MaxMessagesInContext {
				break
			}
			ids = append(ids, m.ID)
		}
		// Mark as surfaced using the recipient as the actor ID
		_, _ = boardStore.MarkSurfaced(ctx, workspaceID, recipient, ids) //nolint:errcheck
	}

	return emitOutput(rc, hooks.Output{
		Decision: hooks.DecisionNone, // Advisory only - never block
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

func emitOutput(rc *skillmain.RunContext, output hooks.Output) error {
	data := map[string]any{
		"hook_output": output,
	}
	return skillout.Emit(rc, "hooks/overseer_inbox", data)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
