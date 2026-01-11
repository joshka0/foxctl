// Package main implements the hooks/mail_router skill.
// This skill surfaces relevant mailbox messages into Claude's context,
// prioritizing admin and overseer messages per mailbox_blackboard.md spec.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
)

const (
	// MaxMessagesInContext is the maximum number of messages to inject into context.
	MaxMessagesInContext = 5
)

func main() {
	skillmain.Main("hooks/mail_router", run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in hooks.Input) error {
	paths := sessionkit.ResolvePaths(rc.Config)

	// Get workspace ID
	workspaceID := in.WorkspaceID
	if workspaceID == "" {
		workspaceID = in.WorkspaceRoot
	}
	if workspaceID == "" {
		// Fallback to current directory; error is not actionable.
		workspaceID, _ = os.Getwd() //nolint:errcheck
	}

	// Get actor ID from environment or derive from session
	actorID := in.ActorID
	if actorID == "" {
		actorID = os.Getenv("AGENTCTL_AGENT_ID")
	}
	if actorID == "" {
		actorID = os.Getenv("AGENTCTL_AGENT_NAME")
	}
	if actorID == "" {
		actorID = fmt.Sprintf("actor:agent:%s", in.SessionID)
	}

	// Get active task ID if available
	taskID := ""
	taskStore, err := tasks.Open(ctx, paths.StorageRoot)
	if err == nil {
		defer taskStore.Close()
		// Best-effort active task lookup; errors are ignored.
		if task, found, _ := taskStore.GetActive(ctx, workspaceID); found { //nolint:errcheck
			taskID = task.ID
		}
	}

	// Open board store
	boardStore, err := blackboard.OpenBoardStore(ctx, paths.StorageRoot)
	if err != nil {
		// If we can't open the store, just emit no-op output
		return emitOutput(rc, hooks.Output{
			Decision: hooks.DecisionNone,
			Reason:   "mail_router: could not open board store",
		})
	}
	defer boardStore.Close()

	// Query inbox for relevant messages
	filter := agent.InboxFilter{
		WorkspaceID:    workspaceID,
		ActorID:        actorID,
		TaskID:         taskID,
		OnlyUnread:     true,                     // includes surfaced, but...
		OnlyUnsurfaced: true,                     // ...hooks only want never-shown messages
		Limit:          MaxMessagesInContext * 2, // Get extra to filter
	}

	messages, err := boardStore.Inbox(ctx, filter)
	if err != nil {
		return emitOutput(rc, hooks.Output{
			Decision: hooks.DecisionNone,
			Reason:   "mail_router: inbox query failed",
		})
	}

	if len(messages) == 0 {
		return emitOutput(rc, hooks.Output{
			Decision: hooks.DecisionNone,
			Reason:   "no unread messages",
		})
	}

	// Build context string with priority messages
	contextStr := buildMailContext(messages)

	// Mark surfaced messages as surfaced (NOT read).
	// We mark all plan events (since they are always rendered), plus up to MaxMessagesInContext other messages.
	if len(messages) > 0 {
		ids := make([]string, 0, minInt(len(messages), MaxMessagesInContext))

		// Plan events: always included in context, so mark all of them as surfaced
		for _, m := range messages {
			if isPlanEvent(m.Subject) {
				ids = append(ids, m.ID)
			}
		}

		// Other messages: cap at MaxMessagesInContext
		otherCount := 0
		for _, m := range messages {
			if isPlanEvent(m.Subject) {
				continue
			}
			if otherCount >= MaxMessagesInContext {
				break
			}
			ids = append(ids, m.ID)
			otherCount++
		}

		// Best-effort; errors are not actionable.
		_, _ = boardStore.MarkSurfaced(ctx, workspaceID, actorID, ids) //nolint:errcheck
	}

	return emitOutput(rc, hooks.Output{
		Decision: hooks.DecisionNone, // Advisory only
		Reason:   fmt.Sprintf("surfaced %d messages", minInt(len(messages), MaxMessagesInContext)),
		Context:  contextStr,
		Meta: map[string]any{
			"message_count": len(messages),
			"workspace_id":  workspaceID,
			"actor_id":      actorID,
			"task_id":       taskID,
		},
	})
}

// buildMailContext creates a formatted context string from messages.
//
//nolint:revive // strings.Builder.WriteString never returns an error for in-memory writes.
func buildMailContext(messages []agent.BoardMessage) string {
	if len(messages) == 0 {
		return ""
	}

	// Separate plan events from other messages for special handling
	var planEvents []agent.BoardMessage
	var otherMessages []agent.BoardMessage
	for _, msg := range messages {
		if isPlanEvent(msg.Subject) {
			planEvents = append(planEvents, msg)
		} else {
			otherMessages = append(otherMessages, msg)
		}
	}

	var sb strings.Builder

	// Format plan events first (high visibility)
	if len(planEvents) > 0 {
		sb.WriteString("## Plan Updates from Overseer\n\n")
		for _, msg := range planEvents {
			eventType := extractPlanEventType(msg.Subject)
			taskID := extractPlanTaskID(msg.Subject)

			switch eventType {
			case "plan.created":
				sb.WriteString(fmt.Sprintf("**New Plan Created** (task: `%s`)\n", taskID))
			case "plan.updated":
				sb.WriteString(fmt.Sprintf("**Plan Updated** (task: `%s`)\n", taskID))
			case "plan.review_needed":
				sb.WriteString(fmt.Sprintf("**Plan Review Needed** (task: `%s`) [ACTION REQUIRED]\n", taskID))
			default:
				sb.WriteString(fmt.Sprintf("**Plan Event:** %s\n", msg.Subject))
			}

			if msg.Body != "" {
				sb.WriteString(fmt.Sprintf("%s\n", msg.Body))
			}
			sb.WriteString("\n")
		}
	}

	// Format other messages
	if len(otherMessages) > 0 {
		sb.WriteString("## Mailbox Messages\n\n")

		count := 0
		for _, msg := range otherMessages {
			if count >= MaxMessagesInContext {
				break
			}

			// Format sender with role indicator
			senderLabel := formatSender(msg.Sender)

			// Priority indicator
			priorityLabel := ""
			if msg.Priority <= 2 {
				priorityLabel = " [HIGH PRIORITY]"
			}

			// Ack indicator
			ackLabel := ""
			if msg.AckRequired {
				ackLabel = " [ACK REQUIRED]"
			}

			sb.WriteString(fmt.Sprintf("### %s%s%s\n", senderLabel, priorityLabel, ackLabel))
			sb.WriteString(fmt.Sprintf("**Subject:** %s\n", msg.Subject))
			if msg.Body != "" {
				sb.WriteString(fmt.Sprintf("%s\n", msg.Body))
			}
			sb.WriteString(fmt.Sprintf("_Stream: %s | ID: %s_\n\n", msg.Stream, msg.ID))

			count++
		}

		remaining := len(otherMessages) - count
		if remaining > 0 {
			sb.WriteString(fmt.Sprintf("_...and %d more unread messages_\n", remaining))
		}
	}

	return sb.String()
}

// isPlanEvent checks if a subject indicates a plan event.
func isPlanEvent(subject string) bool {
	return strings.HasPrefix(subject, "plan.")
}

// extractPlanEventType extracts the event type from a plan subject.
func extractPlanEventType(subject string) string {
	// Subject format: "plan.created:<task-id>" or "plan.updated:<task-id>"
	parts := strings.SplitN(subject, ":", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return subject
}

// extractPlanTaskID extracts the task ID from a plan subject.
func extractPlanTaskID(subject string) string {
	// Subject format: "plan.created:<task-id>"
	parts := strings.SplitN(subject, ":", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

// formatSender formats the sender with role indicators.
func formatSender(sender string) string {
	if agent.IsAdminSender(sender) {
		return fmt.Sprintf("ADMIN (%s)", sender)
	}
	if agent.IsOverseerSender(sender) {
		return fmt.Sprintf("OVERSEER (%s)", sender)
	}
	return sender
}

func emitOutput(rc *skillmain.RunContext, output hooks.Output) error {
	data := map[string]any{
		"hook_output": output,
	}
	return skillout.Emit(rc, "hooks/mail_router", data)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
