package textutil

import (
	"bytes"
	"strings"
)

// TailLines returns the last limit lines, or the full slice when under limit.
func TailLines(lines []string, limit int) []string {
	if limit <= 0 || len(lines) <= limit {
		return lines
	}
	return lines[len(lines)-limit:]
}

// JoinTail joins the last limit lines with sep.
func JoinTail(lines []string, limit int, sep string) string {
	return strings.Join(TailLines(lines, limit), sep)
}

// SplitLines splits text into lines, trimming a trailing newline.
func SplitLines(text string) []string {
	if text == "" {
		return nil
	}
	text = strings.TrimSuffix(text, "\n")
	return strings.Split(text, "\n")
}

// SplitLinesBytes splits content into lines, trimming a trailing newline.
func SplitLinesBytes(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	return SplitLines(string(content))
}

// CountLinesBytes counts lines in content, accounting for missing trailing newline.
func CountLinesBytes(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	count := bytes.Count(content, []byte{'\n'})
	if content[len(content)-1] != '\n' {
		count++
	}
	return count
}

// CountLinesString counts lines in text using newline separators.
// Note: empty strings return 1 to match strings.Count + 1 semantics.
func CountLinesString(text string) int {
	return strings.Count(text, "\n") + 1
}

// CountNewlines counts newline characters in text.
func CountNewlines(text string) int {
	return strings.Count(text, "\n")
}

// FindLastNewline returns the index of the last newline in text, or -1 if not found.
func FindLastNewline(text string) int {
	for i := len(text) - 1; i >= 0; i-- {
		if text[i] == '\n' {
			return i
		}
	}
	return -1
}

// TruncateWithNewlineSuffix truncates text to limit and optionally trims to a newline.
// If the last newline index exceeds minNewline, it truncates to that newline.
func TruncateWithNewlineSuffix(text string, limit int, minNewline int, suffix string) string {
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if minNewline < 0 {
		minNewline = 0
	}
	truncated := text[:limit]
	if lastNL := FindLastNewline(truncated); lastNL > minNewline {
		truncated = truncated[:lastNL+1]
	}
	return truncated + suffix
}
