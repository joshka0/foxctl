// Package main implements the hooks/overseer_inbox skill.
// This skill surfaces mailbox messages sent to "overseer" (or broadcast),
// enabling human-in-the-loop communication during agent runs.
//
// Environment variables:
//   - FOXCTL_OVERSEER_RECIPIENT: Recipient to monitor (default: "overseer", use "*" for broadcast)
//   - FOXCTL_OVERSEER_AUTOACK: Set to "0" to disable auto-marking displayed messages as "surfaced" (default: "1")
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/hookutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/mathutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/context/sessionkit"
	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/runtime/hooks"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
)

const (
	// MaxMessagesInContext is the maximum number of messages to inject into context.
	MaxMessagesInContext = 10

	// DefaultRecipient is the default recipient to monitor.
	DefaultRecipient = "overseer"
)

// main is the skill entry point for hooks/overseer_inbox.
func main() {
	skillmain.Main("hooks/overseer_inbox", run)
}

// run orchestrates overseer inbox monitoring with recipient filtering and auto-acknowledgment.
//
// Index:
//
//	Purpose: Surface mailbox messages sent to overseer (or broadcast) for human-in-the-loop communication
//	Keywords: hooks/overseer_inbox, overseer_communication, message_monitoring, auto_acknowledgment, human_in_the_loop
//	Related: buildOverseerContext, priorityToEmoji, kindToLabel
//	Flow: resolve workspace → get recipient config → open board store → query inbox → build context → auto-mark surfaced → emit results
//	Resources: blackboard store (SQLite)
//	Events: overseer-messages-surfaced
//	OutputFields: message_count, workspace_id, recipient, context
//
// [[domain:mailbox-routing]]
// [[protocol:overseer-communication]]
func run(ctx context.Context, rc *skillmain.RunContext, in hooks.Input) error {
	paths := sessionkit.ResolvePaths(rc.Config)

	workspaceRoot := hookutil.ResolveWorkspaceRoot(in, "")
	workspaceID := hookutil.ResolveWorkspaceID(in, workspaceRoot)

	// Get recipient to monitor from env, default to "overseer"
	recipient := os.Getenv("FOXCTL_OVERSEER_RECIPIENT")
	if recipient == "" {
		recipient = DefaultRecipient
	}

	// Check if auto-ack is enabled (default: true)
	autoAck := os.Getenv("FOXCTL_OVERSEER_AUTOACK") != "0"

	// Open board store
	boardStore, err := blackboard.OpenBoardStore(ctx, paths.StorageRoot)
	if err != nil {
		return hookutil.EmitOutput(rc, "hooks/overseer_inbox", hooks.Output{
			Decision: hooks.DecisionNone,
			Reason:   "overseer_inbox: could not open board store",
		}, nil)
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
		return hookutil.EmitOutput(rc, "hooks/overseer_inbox", hooks.Output{
			Decision: hooks.DecisionNone,
			Reason:   "overseer_inbox: inbox query failed",
		}, nil)
	}

	if len(messages) == 0 {
		return hookutil.EmitOutput(rc, "hooks/overseer_inbox", hooks.Output{
			Decision: hooks.DecisionNone,
			Reason:   "no overseer messages",
			Meta: map[string]any{
				"workspace_id": workspaceID,
				"recipient":    recipient,
			},
		}, nil)
	}

	// Build context string with messages
	contextStr := buildOverseerContext(messages, recipient)

	// Auto-mark displayed messages as "surfaced" if enabled
	if autoAck && len(messages) > 0 {
		ids := make([]string, 0, mathutil.MinInt(len(messages), MaxMessagesInContext))
		for i, m := range messages {
			if i >= MaxMessagesInContext {
				break
			}
			ids = append(ids, m.ID)
		}
		// Mark as surfaced using the recipient as the actor ID
		_, _ = boardStore.MarkSurfaced(ctx, workspaceID, recipient, ids) //nolint:errcheck
	}

	return hookutil.EmitOutput(rc, "hooks/overseer_inbox", hooks.Output{
		Decision: hooks.DecisionNone, // Advisory only - never block
		Reason:   fmt.Sprintf("surfaced %d overseer messages", mathutil.MinInt(len(messages), MaxMessagesInContext)),
		Context:  contextStr,
		Meta: map[string]any{
			"message_count": len(messages),
			"workspace_id":  workspaceID,
			"recipient":     recipient,
			"auto_ack":      autoAck,
		},
	}, nil)
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
