package companion

import (
	"regexp"
	"strings"
)

var (
	lowSignalAssistantReplyPattern   = regexp.MustCompile(`(?i)^(stored(?:-\d+)?|updated(?:-[a-z0-9_]+)?|remembered|ack|ok|okay|done|saved|noted)$`)
	memoryFormattingDirectivePattern = regexp.MustCompile(`(?i)\b(reply|respond|answer|return)\b.*$`)
)

func isLowSignalAssistantTurnText(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return true
	}
	if strings.Contains(text, "rlm_context_") || strings.Contains(text, "<|tool_call_end|>") {
		return true
	}
	if looksLikeContextMutationJSONText(text) {
		return true
	}
	if lowSignalAssistantReplyPattern.MatchString(text) {
		return true
	}
	return false
}

func looksLikeContextMutationJSONText(text string) bool {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	valid := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
			return false
		}
		if !strings.Contains(line, `"key"`) || !strings.Contains(line, `"value"`) {
			return false
		}
		valid++
	}
	return valid > 0
}

func sanitizeTurnContentForMemoryLayer(role string, content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(role), "user") {
		if loc := memoryFormattingDirectivePattern.FindStringIndex(content); loc != nil && loc[0] > 0 {
			content = strings.TrimSpace(content[:loc[0]])
			content = strings.TrimRight(content, " \t\n\r.,;:")
		}
	}
	return strings.TrimSpace(content)
}
