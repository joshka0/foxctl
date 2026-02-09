package discord

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

const (
	// Discord embed limits
	embedDescLimit = 4096
	embedTotalLimit = 6000
	embedFieldLimit = 25
	fieldValueLimit = 1024

	// Colors (decimal)
	colorSuccess = 0x2ECC71 // green
	colorError   = 0xE74C3C // red
	colorInfo    = 0x3498DB // blue
)

// FormatSkillResult converts skill JSON output to Discord embeds.
// Falls back to a generic JSON code block if no specific formatter matches.
func FormatSkillResult(skillName string, output json.RawMessage) []*discordgo.MessageEmbed {
	if len(output) == 0 {
		return []*discordgo.MessageEmbed{{
			Title:       skillName,
			Description: "Completed (no output)",
			Color:       colorSuccess,
		}}
	}

	var parsed any
	if err := json.Unmarshal(output, &parsed); err != nil {
		return genericEmbed(skillName, string(output))
	}

	m, ok := parsed.(map[string]any)
	if !ok {
		return genericEmbed(skillName, string(output))
	}

	switch {
	case strings.HasPrefix(skillName, "code/"):
		return formatSearchResult(skillName, m)
	case strings.HasPrefix(skillName, "todo/"):
		return formatTodoResult(skillName, m)
	case strings.HasPrefix(skillName, "memory/"):
		return formatMemoryResult(skillName, m)
	case strings.HasPrefix(skillName, "obs/"):
		return formatLogsResult(skillName, m)
	default:
		return genericEmbed(skillName, string(output))
	}
}

// formatSearchResult formats code/semantic_search output.
func formatSearchResult(skillName string, m map[string]any) []*discordgo.MessageEmbed {
	// Check for tree format first
	if tree, ok := m["tree"].(string); ok && tree != "" {
		return []*discordgo.MessageEmbed{{
			Title:       "Code Search Results",
			Description: truncateStr(fmt.Sprintf("```\n%s\n```", tree), embedDescLimit),
			Color:       colorInfo,
		}}
	}

	// Fallback to items/results
	items := extractList(m, "items", "results")
	if len(items) == 0 {
		return genericEmbed(skillName, "No results found.")
	}

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("Code Search (%d results)", len(items)),
		Color: colorInfo,
	}

	var desc strings.Builder
	for i, item := range items {
		if i >= 15 {
			desc.WriteString(fmt.Sprintf("\n... and %d more", len(items)-15))
			break
		}
		im, ok := item.(map[string]any)
		if !ok {
			continue
		}
		path, _ := im["path"].(string)
		score, _ := im["score"].(float64)
		if path != "" {
			desc.WriteString(fmt.Sprintf("`%s`", path))
			if score > 0 {
				desc.WriteString(fmt.Sprintf(" (%.2f)", score))
			}
			desc.WriteString("\n")
		}
	}
	embed.Description = truncateStr(desc.String(), embedDescLimit)
	return []*discordgo.MessageEmbed{embed}
}

// formatTodoResult formats todo/manage output.
func formatTodoResult(skillName string, m map[string]any) []*discordgo.MessageEmbed {
	items := extractList(m, "items", "tasks", "todos")
	if len(items) == 0 {
		// Might be a single-result action (add/complete)
		if msg, ok := m["message"].(string); ok {
			return []*discordgo.MessageEmbed{{
				Title:       "Todo",
				Description: msg,
				Color:       colorSuccess,
			}}
		}
		return genericEmbed(skillName, "No tasks.")
	}

	title := fmt.Sprintf("Tasks (%d)", len(items))
	embed := &discordgo.MessageEmbed{
		Title: title,
		Color: colorInfo,
	}

	totalChars := len(title)
	for i, item := range items {
		if i >= embedFieldLimit {
			break
		}
		im, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemTitle, _ := im["title"].(string)
		status, _ := im["status"].(string)
		id, _ := im["id"].(string)
		if itemTitle == "" {
			itemTitle = fmt.Sprintf("Task %s", id)
		}

		value := fmt.Sprintf("Status: %s", status)
		if id != "" {
			value = fmt.Sprintf("ID: `%s` | Status: %s", id, status)
		}

		name := truncateStr(itemTitle, 256)
		value = truncateStr(value, fieldValueLimit)

		fieldChars := len(name) + len(value)
		if totalChars+fieldChars > embedTotalLimit {
			break
		}
		totalChars += fieldChars

		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:  name,
			Value: value,
		})
	}
	return []*discordgo.MessageEmbed{embed}
}

// formatMemoryResult formats memory/query output.
func formatMemoryResult(skillName string, m map[string]any) []*discordgo.MessageEmbed {
	items := extractList(m, "items", "results", "memories")
	if len(items) == 0 {
		return []*discordgo.MessageEmbed{{
			Title:       "Memory Search",
			Description: "No memories found.",
			Color:       colorInfo,
		}}
	}

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("Memory (%d results)", len(items)),
		Color: colorInfo,
	}

	var desc strings.Builder
	for i, item := range items {
		if i >= 10 {
			desc.WriteString(fmt.Sprintf("\n... and %d more", len(items)-10))
			break
		}
		im, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := im["name"].(string)
		summary, _ := im["summary"].(string)
		memType, _ := im["type"].(string)
		if name != "" {
			desc.WriteString(fmt.Sprintf("**%s**", name))
			if memType != "" {
				desc.WriteString(fmt.Sprintf(" [%s]", memType))
			}
			if summary != "" {
				desc.WriteString(fmt.Sprintf(": %s", summary))
			}
			desc.WriteString("\n")
		}
	}
	embed.Description = truncateStr(desc.String(), embedDescLimit)
	return []*discordgo.MessageEmbed{embed}
}

// formatLogsResult formats obs/logs output.
func formatLogsResult(skillName string, m map[string]any) []*discordgo.MessageEmbed {
	items := extractList(m, "items", "events", "logs")
	if len(items) == 0 {
		return []*discordgo.MessageEmbed{{
			Title:       "Logs",
			Description: "No log entries.",
			Color:       colorInfo,
		}}
	}

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("Logs (%d entries)", len(items)),
		Color: colorInfo,
	}

	var desc strings.Builder
	for i, item := range items {
		if i >= 20 {
			desc.WriteString(fmt.Sprintf("\n... and %d more", len(items)-20))
			break
		}
		im, ok := item.(map[string]any)
		if !ok {
			continue
		}
		event, _ := im["event"].(string)
		status, _ := im["status"].(string)
		ts, _ := im["ts"].(string)
		if event != "" {
			desc.WriteString(fmt.Sprintf("`%s` %s", event, status))
			if ts != "" {
				desc.WriteString(fmt.Sprintf(" (%s)", ts))
			}
			desc.WriteString("\n")
		}
	}
	embed.Description = truncateStr(desc.String(), embedDescLimit)
	return []*discordgo.MessageEmbed{embed}
}

// genericEmbed wraps raw output in a JSON code block.
func genericEmbed(title string, raw string) []*discordgo.MessageEmbed {
	// Try to pretty-print JSON
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
		pretty, _ := json.MarshalIndent(parsed, "", "  ")
		raw = string(pretty)
	}

	return []*discordgo.MessageEmbed{{
		Title:       title,
		Description: truncateStr(fmt.Sprintf("```json\n%s\n```", raw), embedDescLimit),
		Color:       colorInfo,
	}}
}

// extractList tries multiple keys to find a list in the output map.
func extractList(m map[string]any, keys ...string) []any {
	for _, key := range keys {
		if items, ok := m[key].([]any); ok && len(items) > 0 {
			return items
		}
	}
	return nil
}

// truncateStr truncates a string to maxLen runes, appending "..." if truncated.
func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}
