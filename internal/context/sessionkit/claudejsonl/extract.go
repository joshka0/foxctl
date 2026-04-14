package claudejsonl

import (
	"encoding/json"
	"strings"
)

const (
	// DefaultMaxPreviewLen is the default max characters for content previews.
	DefaultMaxPreviewLen = 200
)

// ExtractPreview extracts a content preview from the message.
// Follows session_summarize filtering approach:
// - User messages: only string content or text blocks (skip tool_result content)
// - Assistant messages: only text blocks (skip thinking blocks)
// - Tool results: intentionally NOT included (contains file contents, command output)
func ExtractPreview(msg *Message, maxLen int) string {
	if maxLen <= 0 {
		maxLen = DefaultMaxPreviewLen
	}

	var content string

	// Claude Code JSONL: content is inside .message.content
	if len(msg.Message) > 0 {
		var nested NestedMessage
		if err := json.Unmarshal(msg.Message, &nested); err == nil && len(nested.Content) > 0 {
			// Try as string first (user direct input)
			var textContent string
			if err := json.Unmarshal(nested.Content, &textContent); err == nil {
				content = textContent
			} else if nested.Role == "user" {
				// User message with array content - extract only text blocks
				var userBlocks []UserContentBlock
				if json.Unmarshal(nested.Content, &userBlocks) == nil {
					var textParts []string
					for _, block := range userBlocks {
						if block.Type == "text" && block.Text != "" {
							textParts = append(textParts, block.Text)
						}
						// Skip tool_result blocks - they contain file contents, command output
					}
					content = strings.Join(textParts, "\n")
				}
			} else {
				// Assistant message - extract only text blocks
				var blocks []ContentBlock
				if json.Unmarshal(nested.Content, &blocks) == nil {
					var textParts []string
					for _, block := range blocks {
						if block.Type == "text" && block.Text != "" {
							textParts = append(textParts, block.Text)
						}
						// Skip thinking blocks and tool_use (tools are tracked separately)
					}
					content = strings.Join(textParts, "\n")
				}
			}
		}
	}

	// Fallback: try top-level content field (for other message formats)
	if content == "" && len(msg.Content) > 0 {
		var text string
		if err := json.Unmarshal(msg.Content, &text); err == nil {
			content = text
		}
		// Don't try array fallback - we want to avoid tool_result content
	}

	// Note: intentionally NOT including msg.ToolResult.Content
	// Tool results contain file contents, command output - not useful for embeddings

	// Truncate
	if len(content) > maxLen {
		content = content[:maxLen] + "..."
	}

	return strings.TrimSpace(content)
}

// ExtractTools extracts tool names from the message.
func ExtractTools(msg *Message) []string {
	// Check top-level tool use
	if msg.ToolUse != nil && msg.ToolUse.Name != "" {
		return []string{msg.ToolUse.Name}
	}

	// Check nested content blocks for tool_use
	if len(msg.Message) > 0 {
		var nested NestedMessage
		if err := json.Unmarshal(msg.Message, &nested); err == nil && len(nested.Content) > 0 {
			var blocks []ContentBlock
			if err := json.Unmarshal(nested.Content, &blocks); err == nil {
				var tools []string
				for _, block := range blocks {
					if block.Type == "tool_use" && block.Name != "" {
						tools = append(tools, block.Name)
					}
				}
				if len(tools) > 0 {
					return tools
				}
			}
		}
	}

	return nil
}

// ExtractSummaryText cleans up the compact summary content.
// Removes the standard prefix and extracts the meaningful summary.
func ExtractSummaryText(content string) string {
	// The compact summary typically starts with:
	// "This session is being continued from a previous conversation that ran out of context. The conversation is summarized below:"
	prefixes := []string{
		"This session is being continued from a previous conversation that ran out of context. The conversation is summarized below:\n",
		"This session is being continued from a previous conversation that ran out of context.",
	}

	result := content
	for _, prefix := range prefixes {
		if strings.HasPrefix(result, prefix) {
			result = strings.TrimPrefix(result, prefix)
			break
		}
	}

	result = strings.TrimSpace(result)

	// Limit to reasonable size for storage (keep first 4KB for summary)
	const maxSummaryLen = 4096
	if len(result) > maxSummaryLen {
		result = result[:maxSummaryLen] + "..."
	}

	return result
}

// MaybeCompactSummary checks if the message is a compact summary and returns its content.
// Returns the cleaned summary text and true if this is a compact summary message.
func MaybeCompactSummary(msg *Message) (string, bool) {
	if !msg.IsCompactSummary {
		return "", false
	}

	// Extract content from the message
	content := ExtractPreview(msg, 0) // 0 means no truncation for summary
	if content == "" {
		return "", false
	}

	return ExtractSummaryText(content), true
}
