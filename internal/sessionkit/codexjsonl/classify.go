package codexjsonl

import (
	"encoding/json"
	"strings"
)

// Classify determines the ChunkType from a Message.
func Classify(msg *Message) ChunkType {
	if _, _, ok := IsCompactBoundary(msg); ok {
		return ChunkTypeCompactBoundary
	}

	item, ok := parseResponseItem(msg)
	if !ok {
		return ChunkTypeOther
	}

	switch item.Type {
	case "message":
		if item.Role == "user" {
			return ChunkTypeUserRequest
		}
		if item.Role == "assistant" {
			return ChunkTypeAssistantResponse
		}
	case "function_call", "custom_tool_call":
		return ChunkTypeToolUse
	case "function_call_output", "custom_tool_call_output":
		if isToolErrorStatus(item.Status) {
			return ChunkTypeError
		}
		return ChunkTypeToolOutput
	}

	return ChunkTypeOther
}

func parseResponseItem(msg *Message) (ResponseItem, bool) {
	if msg == nil || msg.Type != "response_item" || len(msg.Payload) == 0 {
		return ResponseItem{}, false
	}
	var item ResponseItem
	if err := json.Unmarshal(msg.Payload, &item); err != nil {
		return ResponseItem{}, false
	}
	return item, true
}

// IsCompactBoundary reports Codex compaction boundaries when present.
func IsCompactBoundary(msg *Message) (string, int, bool) {
	if msg == nil {
		return "", 0, false
	}
	if msg.Type == "compacted" {
		return "compacted", 0, true
	}
	if msg.Type != "event_msg" || len(msg.Payload) == 0 {
		return "", 0, false
	}
	var payload struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return "", 0, false
	}
	if payload.Type == "context_compacted" {
		return "context_compacted", 0, true
	}
	return "", 0, false
}

// HasError checks if the message contains an error.
func HasError(msg *Message) bool {
	item, ok := parseResponseItem(msg)
	if !ok {
		return false
	}
	if item.Type == "function_call_output" || item.Type == "custom_tool_call_output" {
		return isToolErrorStatus(item.Status)
	}
	return false
}

// ExtractErrorType extracts the error type if present.
func ExtractErrorType(msg *Message) string {
	item, ok := parseResponseItem(msg)
	if !ok {
		return ""
	}
	if item.Type == "function_call_output" || item.Type == "custom_tool_call_output" {
		if isToolErrorStatus(item.Status) {
			return "ToolError"
		}
	}
	return ""
}

func isToolErrorStatus(status string) bool {
	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		return false
	}
	return status != "ok" && status != "success"
}
