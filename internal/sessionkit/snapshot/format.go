package snapshot

import (
	"fmt"
	"strings"
	"time"
)

// FormatForRestore formats a snapshot and related context for injection into a session.
func FormatForRestore(snap *Snapshot, windows []WindowMatch, gitChanges string) string {
	var sb strings.Builder

	sb.WriteString("<session-restore>\n")
	sb.WriteString("<!-- Auto-injected after context compaction. This is NOT part of the user's message. -->\n\n")
	sb.WriteString("## Session Continuity Context\n\n")

	// Timestamp
	if !snap.Timestamp.IsZero() {
		sb.WriteString(fmt.Sprintf("*Restored after compact (snapshot from %s ago)*\n\n", formatRelativeTime(snap.Timestamp)))
	}

	// Git changes
	if gitChanges != "" {
		sb.WriteString("### Files Modified\n")
		sb.WriteString("```\n")
		sb.WriteString(gitChanges)
		sb.WriteString("\n```\n\n")
	}

	// Related windows from past sessions
	if len(windows) > 0 {
		sb.WriteString("### Related Past Sessions\n")
		sb.WriteString("*Similar work from previous sessions:*\n\n")
		for _, w := range windows {
			summary := w.Summary
			if len(summary) > 150 {
				summary = summary[:147] + "..."
			}
			sb.WriteString(fmt.Sprintf("- **[%.0f%% match]** `session:%s` | %s\n",
				w.Similarity*100, w.SessionID[:8], summary))
		}
		sb.WriteString("\n")
	}

	// Active task
	if snap.ActiveTask != nil {
		sb.WriteString("### Active Task\n")
		sb.WriteString(fmt.Sprintf("**%s** (ID: %s)\n", snap.ActiveTask.Title, snap.ActiveTask.ID))
		if snap.ActiveTask.Description != "" {
			sb.WriteString(snap.ActiveTask.Description + "\n")
		}
		sb.WriteString("\n")
	}

	// Active plan
	if snap.ActivePlan != nil {
		sb.WriteString("### Active Plan\n")
		sb.WriteString(fmt.Sprintf("**%s** (`%s`)\n", snap.ActivePlan.Title, snap.ActivePlan.FileName))
		if len(snap.ActivePlan.Sections) > 0 {
			sb.WriteString("Sections:\n")
			for _, sec := range snap.ActivePlan.Sections {
				sb.WriteString(fmt.Sprintf("  - %s\n", sec))
			}
		}
		sb.WriteString("\n")
	}

	// Pending todos
	if len(snap.PendingTodos) > 0 {
		sb.WriteString("### Pending Work\n")
		for _, task := range snap.PendingTodos {
			status := statusIcon(task.Status)
			sb.WriteString(fmt.Sprintf("- %s %s\n", status, task.Title))
		}
		sb.WriteString("\n")
	}

	// Decisions
	if len(snap.Decisions) > 0 {
		sb.WriteString("### Key Decisions\n")
		for _, d := range snap.Decisions {
			sb.WriteString(fmt.Sprintf("- %s\n", d))
		}
		sb.WriteString("\n")
	}

	// Insights
	if len(snap.Insights) > 0 {
		sb.WriteString("### Insights\n")
		for _, i := range snap.Insights {
			sb.WriteString(fmt.Sprintf("- %s\n", i))
		}
		sb.WriteString("\n")
	}

	// Summary
	if snap.Summary != "" {
		sb.WriteString("### Summary\n")
		sb.WriteString(snap.Summary)
		sb.WriteString("\n\n")
	}

	sb.WriteString("---\n")
	sb.WriteString("*Continue where you left off.*\n\n")
	sb.WriteString("</session-restore>\n")

	return sb.String()
}

// FormatMinimal creates a minimal restore context (for quick restarts).
func FormatMinimal(snap *Snapshot) string {
	var sb strings.Builder

	sb.WriteString("<session-restore>\n")

	if snap.ActiveTask != nil {
		sb.WriteString(fmt.Sprintf("**Active:** %s\n", snap.ActiveTask.Title))
	}

	if len(snap.PendingTodos) > 0 {
		sb.WriteString(fmt.Sprintf("**Pending:** %d tasks\n", len(snap.PendingTodos)))
	}

	sb.WriteString("</session-restore>\n")

	return sb.String()
}

func statusIcon(status string) string {
	switch status {
	case "completed", "done":
		return "✅"
	case "in_progress", "active":
		return "🔄"
	case "pending":
		return "⏳"
	case "blocked":
		return "🚫"
	default:
		return "•"
	}
}

func formatRelativeTime(t time.Time) string {
	if t.IsZero() {
		return "unknown time"
	}
	dur := time.Since(t)
	switch {
	case dur < time.Minute:
		return "just now"
	case dur < time.Hour:
		mins := int(dur.Minutes())
		if mins == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", mins)
	case dur < 24*time.Hour:
		hours := int(dur.Hours())
		if hours == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", hours)
	default:
		days := int(dur.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}
