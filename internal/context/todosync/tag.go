// Package todosync provides bidirectional synchronization between
// Claude Code's native todo system and agentctl's task management.
package todosync

import (
	"regexp"
	"strings"
)

// taskIDTagRe matches the stable task ID tag format: 〔T:<id>〕
var taskIDTagRe = regexp.MustCompile(`〔T:([A-Za-z0-9]+)〕`)

// ParseTaskID extracts the task ID from content containing a tag.
// Returns empty string if no tag is found.
func ParseTaskID(content string) string {
	matches := taskIDTagRe.FindStringSubmatch(content)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// StripTaskID removes the task ID tag from content.
func StripTaskID(content string) string {
	return strings.TrimSpace(taskIDTagRe.ReplaceAllString(content, ""))
}

// AppendTaskID adds a task ID tag to content.
// If content already has a tag, it is replaced.
func AppendTaskID(content, taskID string) string {
	stripped := StripTaskID(content)
	return stripped + " 〔T:" + taskID + "〕"
}

// HasTaskID reports whether content contains a task ID tag.
func HasTaskID(content string) bool {
	return taskIDTagRe.MatchString(content)
}
