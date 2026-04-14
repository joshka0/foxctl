package discord

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/jkatigb/agentctl/internal/interfaces/chatadapter"
	"github.com/jkatigb/agentctl/internal/runtime/observability"
)

// Embed colors for agent lifecycle states.
const (
	colorSpawn    = 0x3498DB // blue
	colorRunning  = 0xF39C12 // orange
	colorComplete = 0x2ECC71 // green
	colorKilled   = 0x95A5A6 // gray
	colorAgentErr = 0xE74C3C // red
)

// agentSpawnEmbed creates a rich embed for an agent spawn event.
func agentSpawnEmbed(event observability.ActivityEvent) *discordgo.MessageEmbed {
	role := chatadapter.GetDataString(event.Data, "role")
	prompt := chatadapter.GetDataString(event.Data, "prompt")
	prompt = chatadapter.TruncateRunesWithEllipsis(prompt, 200)

	fields := []*discordgo.MessageEmbedField{
		{Name: "Session", Value: codeBlock(chatadapter.TruncateRunes(event.SessionID, 36)), Inline: true},
	}
	if role != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Role", Value: role, Inline: true,
		})
	}
	if prompt != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Prompt", Value: prompt, Inline: false,
		})
	}

	return &discordgo.MessageEmbed{
		Title:  "Agent Spawned",
		Color:  colorSpawn,
		Fields: fields,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("agent.spawn \u2022 %s", event.Timestamp),
		},
	}
}

// agentCompleteEmbed creates a rich embed for an agent completion event.
func agentCompleteEmbed(event observability.ActivityEvent) *discordgo.MessageEmbed {
	fields := []*discordgo.MessageEmbedField{
		{Name: "Session", Value: codeBlock(chatadapter.TruncateRunes(event.SessionID, 36)), Inline: true},
	}
	if event.DurationMS > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Duration", Value: chatadapter.FormatDuration(event.DurationMS), Inline: true,
		})
	}
	iterations := chatadapter.GetDataString(event.Data, "iterations")
	if iterations != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Iterations", Value: iterations, Inline: true,
		})
	}

	return &discordgo.MessageEmbed{
		Title:  "Agent Complete",
		Color:  colorComplete,
		Fields: fields,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("agent.complete \u2022 %s", event.Timestamp),
		},
	}
}

// agentErrorEmbed creates a rich embed for an agent error event.
func agentErrorEmbed(event observability.ActivityEvent) *discordgo.MessageEmbed {
	errMsg := event.ErrorMessage
	if errMsg == "" {
		errMsg = chatadapter.GetDataString(event.Data, "error")
	}
	if errMsg == "" {
		errMsg = "Unknown error"
	}
	errMsg = chatadapter.TruncateRunesWithEllipsis(errMsg, 500)

	fields := []*discordgo.MessageEmbedField{
		{Name: "Session", Value: codeBlock(chatadapter.TruncateRunes(event.SessionID, 36)), Inline: true},
		{Name: "Error", Value: codeBlock(errMsg), Inline: false},
	}

	return &discordgo.MessageEmbed{
		Title:  "Agent Error",
		Color:  colorAgentErr,
		Fields: fields,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("agent.error \u2022 %s", event.Timestamp),
		},
	}
}

// agentKilledEmbed creates a rich embed for an agent kill event.
func agentKilledEmbed(event observability.ActivityEvent) *discordgo.MessageEmbed {
	fields := []*discordgo.MessageEmbedField{
		{Name: "Session", Value: codeBlock(chatadapter.TruncateRunes(event.SessionID, 36)), Inline: true},
	}
	if event.DurationMS > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Duration", Value: chatadapter.FormatDuration(event.DurationMS), Inline: true,
		})
	}

	return &discordgo.MessageEmbed{
		Title:  "Agent Killed",
		Color:  colorKilled,
		Fields: fields,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("agent.kill \u2022 %s", event.Timestamp),
		},
	}
}

// agentIterationEmbed creates a compact embed for an agent iteration event.
func agentIterationEmbed(event observability.ActivityEvent) *discordgo.MessageEmbed {
	iteration := chatadapter.GetDataString(event.Data, "iteration")
	toolName := chatadapter.GetDataString(event.Data, "tool_name")

	desc := fmt.Sprintf("Iteration %s", iteration)
	if toolName != "" {
		desc += fmt.Sprintf(" \u2014 tool: `%s`", toolName)
	}

	return &discordgo.MessageEmbed{
		Description: desc,
		Color:       colorRunning,
	}
}

// activityFeedLine returns a compact one-line string for the activity channel.
func activityFeedLine(event observability.ActivityEvent) string {
	op := event.Operation
	sessionShort := chatadapter.TruncateRunes(event.SessionID, 8)

	switch op {
	case "agent.spawn":
		role := chatadapter.GetDataString(event.Data, "role")
		return fmt.Sprintf("\u25b6\ufe0f **spawn** `%s` role=%s", sessionShort, role)
	case "agent.complete":
		dur := chatadapter.FormatDuration(event.DurationMS)
		return fmt.Sprintf("\u2705 **complete** `%s` %s", sessionShort, dur)
	case "agent.kill":
		return fmt.Sprintf("\u23f9\ufe0f **killed** `%s`", sessionShort)
	case "agent.iteration":
		iter := chatadapter.GetDataString(event.Data, "iteration")
		return fmt.Sprintf("\U0001f504 **iter %s** `%s`", iter, sessionShort)
	default:
		if event.Status == "error" {
			return fmt.Sprintf("\u274c **%s** `%s` %s", op, sessionShort, chatadapter.TruncateRunes(event.ErrorMessage, 80))
		}
		return fmt.Sprintf("\U0001f4cc **%s** `%s`", op, sessionShort)
	}
}

// stopButton returns a discordgo Button component for stopping an agent.
func stopButton(sessionID string) discordgo.Button {
	return discordgo.Button{
		Label:    "Stop",
		Style:    discordgo.DangerButton,
		CustomID: "stop:" + sessionID,
	}
}

// retryButton returns a discordgo Button component for retrying an agent.
func retryButton(sessionID string) discordgo.Button {
	return discordgo.Button{
		Label:    "Retry",
		Style:    discordgo.PrimaryButton,
		CustomID: "retry:" + sessionID,
	}
}

// detailsButton returns a discordgo Button component for viewing agent details.
func detailsButton(sessionID string) discordgo.Button {
	return discordgo.Button{
		Label:    "Details",
		Style:    discordgo.SecondaryButton,
		CustomID: "details:" + sessionID,
	}
}

// --- helpers ---

func codeBlock(s string) string {
	if strings.Contains(s, "`") {
		return s
	}
	return "`" + s + "`"
}
