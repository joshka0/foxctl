package claudejsonl

import (
	"encoding/json"
	"strings"
)

// Classify determines the ChunkType from a Message.
func Classify(msg *Message) ChunkType {
	switch {
	case msg.Type == "system" && msg.Subtype == "compact_boundary":
		return ChunkTypeCompactBoundary
	case msg.Type == "user" || msg.Role == "user":
		// Check if this is a tool_result response (appears as "user" message with array content)
		if len(msg.Message) > 0 {
			var nested NestedMessage
			if err := json.Unmarshal(msg.Message, &nested); err == nil && len(nested.Content) > 0 {
				// If content is an array, check if first block is tool_result
				var blocks []map[string]any
				if json.Unmarshal(nested.Content, &blocks) == nil && len(blocks) > 0 {
					if blockType, ok := blocks[0]["type"].(string); ok && blockType == "tool_result" {
						// Check for errors in tool results
						for _, block := range blocks {
							if isErr, ok := block["is_error"].(bool); ok && isErr {
								return ChunkTypeError
							}
						}
						return ChunkTypeToolOutput
					}
				}
			}
		}
		return ChunkTypeUserRequest
	case msg.Type == "assistant" || msg.Role == "assistant":
		// Check content blocks for tool_use vs text
		if len(msg.Message) > 0 {
			var nested NestedMessage
			if err := json.Unmarshal(msg.Message, &nested); err == nil && len(nested.Content) > 0 {
				var blocks []ContentBlock
				if json.Unmarshal(nested.Content, &blocks) == nil {
					hasToolUse := false
					hasText := false
					for _, block := range blocks {
						if block.Type == "tool_use" {
							hasToolUse = true
						}
						if block.Type == "text" && block.Text != "" {
							hasText = true
						}
					}
					// If only tool_use (no text), classify as tool_use
					if hasToolUse && !hasText {
						return ChunkTypeToolUse
					}
				}
			}
		}
		if msg.ToolUse != nil {
			return ChunkTypeToolUse
		}
		return ChunkTypeAssistantResponse
	case msg.Type == "tool_result" || msg.ToolResult != nil:
		if msg.ToolResult != nil && msg.ToolResult.IsError {
			return ChunkTypeError
		}
		return ChunkTypeToolOutput
	default:
		return ChunkTypeOther
	}
}

// IsCompactBoundary checks if the message is a compact boundary and returns metadata.
func IsCompactBoundary(msg *Message) (trigger string, preTokens int, ok bool) {
	if msg.Type != "system" || msg.Subtype != "compact_boundary" {
		return "", 0, false
	}
	if msg.CompactMetadata != nil {
		return msg.CompactMetadata.Trigger, msg.CompactMetadata.PreTokens, true
	}
	return "unknown", 0, true
}

// HasError checks if the message contains an error.
func HasError(msg *Message) bool {
	if msg.ToolResult != nil && msg.ToolResult.IsError {
		return true
	}
	return false
}

// ExtractErrorType extracts the error type if present.
func ExtractErrorType(msg *Message) string {
	if msg.ToolResult != nil && msg.ToolResult.IsError {
		content := msg.ToolResult.Content
		switch {
		case strings.Contains(content, "TypeError"):
			return "TypeError"
		case strings.Contains(content, "SyntaxError"):
			return "SyntaxError"
		case strings.Contains(content, "compile") || strings.Contains(content, "build"):
			return "CompileError"
		default:
			return "ToolError"
		}
	}
	return ""
}
