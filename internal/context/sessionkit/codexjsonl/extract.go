package codexjsonl

import (
	"encoding/json"
	"strings"
)

const (
	// DefaultMaxPreviewLen is the default max characters for content previews.
	DefaultMaxPreviewLen = 200
	// DefaultTokensPerChar approximates tokens from text length (4 chars per token).
	DefaultTokensPerChar = 0.25
)

// ExtractPreview extracts a content preview from the message.
// Only includes user/assistant text blocks, skipping tool outputs.
func ExtractPreview(msg *Message, maxLen int) string {
	if maxLen <= 0 {
		maxLen = DefaultMaxPreviewLen
	}

	item, ok := parseResponseItem(msg)
	if !ok || item.Type != "message" {
		return ""
	}

	var parts []string
	for _, block := range item.Content {
		if block.Text == "" {
			continue
		}
		if item.Role == "user" && block.Type != "input_text" && block.Type != "text" {
			continue
		}
		if item.Role == "assistant" && block.Type != "output_text" && block.Type != "text" {
			continue
		}
		parts = append(parts, block.Text)
	}

	content := strings.Join(parts, "\n")
	if len(content) > maxLen {
		content = content[:maxLen] + "..."
	}
	return strings.TrimSpace(content)
}

// ExtractTools extracts tool names from tool call messages.
func ExtractTools(msg *Message) []string {
	item, ok := parseResponseItem(msg)
	if !ok {
		return nil
	}

	if item.Type == "function_call" || item.Type == "custom_tool_call" {
		if item.Name != "" {
			return []string{item.Name}
		}
	}
	return nil
}

// EstimateTokens returns a rough token estimate for the message text.
func EstimateTokens(msg *Message) int {
	if msg == nil {
		return 0
	}

	chars := 0
	if item, ok := parseResponseItem(msg); ok && item.Type == "message" {
		for _, block := range item.Content {
			if block.Text != "" {
				chars += len(block.Text)
			}
		}
	}

	if chars == 0 && msg.Type == "event_msg" && len(msg.Payload) > 0 {
		var payload struct {
			Type    string `json:"type"`
			Message string `json:"message,omitempty"`
			Text    string `json:"text,omitempty"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err == nil {
			if payload.Type == "user_message" || payload.Type == "agent_message" {
				text := payload.Message
				if text == "" {
					text = payload.Text
				}
				chars = len(text)
			}
		}
	}

	if chars == 0 {
		return 0
	}
	return int(float64(chars) * DefaultTokensPerChar)
}
