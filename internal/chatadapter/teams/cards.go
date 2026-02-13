package teams

import (
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/chatadapter"
	"github.com/jkatigb/agentctl/internal/observability"
)

// cardAction defines a button for an Adaptive Card Action.Submit.
type cardAction struct {
	Title   string
	Action  string // e.g., "stop", "retry", "details"
	AgentID string
	Style   string // "default", "positive", "destructive"
}

// agentCard builds an Activity with an Adaptive Card attachment showing agent status.
func agentCard(agentID, role, status, body string, actions []cardAction) Activity {
	cardBody := []any{
		map[string]any{
			"type":   "TextBlock",
			"text":   body,
			"wrap":   true,
			"weight": "Default",
			"size":   "Default",
		},
	}

	var cardActions []any
	for _, a := range actions {
		act := map[string]any{
			"type":  "Action.Submit",
			"title": a.Title,
			"data": map[string]string{
				"action":  a.Action,
				"agentID": a.AgentID,
			},
		}
		if a.Style != "" {
			act["style"] = a.Style
		}
		cardActions = append(cardActions, act)
	}

	card := map[string]any{
		"type":    "AdaptiveCard",
		"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
		"version": "1.4",
		"body":    cardBody,
		"actions": cardActions,
	}

	return Activity{
		Type: "message",
		Attachments: []Attachment{
			{
				ContentType: "application/vnd.microsoft.card.adaptive",
				Content:     card,
			},
		},
	}
}

// actionsStopDetails returns Stop + Details buttons for a running agent.
func actionsStopDetails(agentID string) []cardAction {
	return []cardAction{
		{Title: "Stop", Action: "stop", AgentID: agentID, Style: "destructive"},
		{Title: "Details", Action: "details", AgentID: agentID},
	}
}

// actionsRetryDetails returns Retry + Details buttons for a failed agent.
func actionsRetryDetails(agentID string) []cardAction {
	return []cardAction{
		{Title: "Retry", Action: "retry", AgentID: agentID, Style: "positive"},
		{Title: "Details", Action: "details", AgentID: agentID},
	}
}

// actionsDetails returns a Details-only button for a completed/killed agent.
func actionsDetails(agentID string) []cardAction {
	return []cardAction{
		{Title: "Details", Action: "details", AgentID: agentID},
	}
}

// agentRootCardText builds the text body for an agent root card.
// Mirrors telegramAgentRootText formatting.
func agentRootCardText(event observability.ActivityEvent, status string) string {
	agentID := strings.TrimSpace(event.AgentID)
	sessionID := strings.TrimSpace(event.SessionID)

	displayID := agentID
	if displayID == "" {
		displayID = sessionID
	}
	displayShort := chatadapter.TruncateRunes(displayID, 8)

	role := chatadapter.GetDataString(event.Data, "role")
	prompt := chatadapter.GetDataString(event.Data, "prompt")
	iteration := chatadapter.GetDataString(event.Data, "iteration")
	toolName := chatadapter.GetDataString(event.Data, "tool_name")
	errMsg := strings.TrimSpace(event.ErrorMessage)
	if errMsg == "" {
		errMsg = chatadapter.GetDataString(event.Data, "error")
	}

	var b strings.Builder
	b.WriteString("**Agent ")
	b.WriteString(displayShort)
	if role != "" {
		b.WriteString(" (")
		b.WriteString(role)
		b.WriteString(")")
	}
	b.WriteString("**\n\n")
	b.WriteString("Status: **")
	b.WriteString(status)
	b.WriteString("**\n")

	if status == "running" && iteration != "" {
		b.WriteString("Iteration: ")
		b.WriteString(iteration)
		if toolName != "" {
			b.WriteString(" tool=")
			b.WriteString(toolName)
		}
		b.WriteString("\n")
	}

	if status == "error" && errMsg != "" {
		b.WriteString("Error: ")
		b.WriteString(chatadapter.TruncateRunes(errMsg, 400))
		b.WriteString("\n")
	}

	if event.Operation == "agent.spawn" && prompt != "" {
		b.WriteString("\nPrompt: ")
		b.WriteString(chatadapter.TruncateRunes(prompt, 600))
		b.WriteString("\n")
	}

	if agentID != "" {
		b.WriteString("\n`agent_id: ")
		b.WriteString(agentID)
		b.WriteString("`\n")
	}
	if sessionID != "" {
		b.WriteString("`session_id: ")
		b.WriteString(sessionID)
		b.WriteString("`\n")
	}

	return truncateForTeams(strings.TrimSpace(b.String()))
}

// sessionSummaryCard builds an Adaptive Card with FactSet for a session summary.
func sessionSummaryCard(agentID string, sess sessionSummary) Activity {
	agentShort := chatadapter.TruncateRunes(agentID, 8)
	sessShort := chatadapter.TruncateRunes(sess.ID, 8)

	var title strings.Builder
	title.WriteString("Result")
	if agentShort != "" {
		title.WriteString(" agent ")
		title.WriteString(agentShort)
	}
	if sessShort != "" {
		title.WriteString(" session ")
		title.WriteString(sessShort)
	}
	if strings.TrimSpace(sess.Status) != "" {
		title.WriteString(" (")
		title.WriteString(strings.TrimSpace(sess.Status))
		title.WriteString(")")
	}

	cardBody := []any{
		map[string]any{
			"type":   "TextBlock",
			"text":   title.String(),
			"weight": "Bolder",
			"size":   "Medium",
			"wrap":   true,
		},
	}

	if strings.TrimSpace(sess.Summary) != "" {
		cardBody = append(cardBody, map[string]any{
			"type": "TextBlock",
			"text": strings.TrimSpace(sess.Summary),
			"wrap": true,
		})
	}

	addList := func(heading string, items []string, max int) {
		if len(items) == 0 {
			return
		}
		cardBody = append(cardBody, map[string]any{
			"type":   "TextBlock",
			"text":   "**" + heading + "**",
			"wrap":   true,
			"weight": "Bolder",
		})
		n := len(items)
		if max > 0 && n > max {
			n = max
		}
		var text strings.Builder
		for i := 0; i < n; i++ {
			item := strings.TrimSpace(items[i])
			if item == "" {
				continue
			}
			text.WriteString("- ")
			text.WriteString(item)
			text.WriteString("\n")
		}
		if max > 0 && len(items) > max {
			text.WriteString(fmt.Sprintf("... (%d more)\n", len(items)-max))
		}
		cardBody = append(cardBody, map[string]any{
			"type": "TextBlock",
			"text": text.String(),
			"wrap": true,
		})
	}

	addList("Accomplished", sess.Accomplished, 12)
	addList("Decisions", sess.Decisions, 8)
	addList("Gotchas", sess.Gotchas, 8)

	// IDs as a FactSet for easy reference.
	var facts []map[string]string
	if strings.TrimSpace(agentID) != "" {
		facts = append(facts, map[string]string{"title": "agent_id", "value": agentID})
	}
	if strings.TrimSpace(sess.ID) != "" {
		facts = append(facts, map[string]string{"title": "session_id", "value": sess.ID})
	}
	if len(facts) > 0 {
		cardBody = append(cardBody, map[string]any{
			"type":  "FactSet",
			"facts": facts,
		})
	}

	card := map[string]any{
		"type":    "AdaptiveCard",
		"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
		"version": "1.4",
		"body":    cardBody,
	}

	return Activity{
		Type: "message",
		Attachments: []Attachment{
			{
				ContentType: "application/vnd.microsoft.card.adaptive",
				Content:     card,
			},
		},
	}
}

// activityFeedLine formats a compact one-line status for an activity feed conversation.
func activityFeedLine(event observability.ActivityEvent) string {
	idShort := chatadapter.TruncateRunes(agentKey(event), 8)
	switch event.Operation {
	case "agent.spawn":
		role := chatadapter.GetDataString(event.Data, "role")
		if role != "" {
			return fmt.Sprintf("spawn %s role=%s", idShort, role)
		}
		return fmt.Sprintf("spawn %s", idShort)
	case "agent.complete":
		return fmt.Sprintf("complete %s %s", idShort, chatadapter.FormatDuration(event.DurationMS))
	case "agent.kill":
		return fmt.Sprintf("killed %s", idShort)
	default:
		if event.Status == "error" {
			errMsg := strings.TrimSpace(event.ErrorMessage)
			if errMsg == "" {
				errMsg = chatadapter.GetDataString(event.Data, "error")
			}
			errMsg = chatadapter.TruncateRunes(errMsg, 120)
			if errMsg != "" {
				return fmt.Sprintf("error %s %s", idShort, errMsg)
			}
			return fmt.Sprintf("error %s", idShort)
		}
		return ""
	}
}
