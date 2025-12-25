package templates

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

func truncateID(id string) string {
	if len(id) > 8 {
		return id[:8] + "..."
	}
	return id
}

func formatTime(t string) string {
	parsed, err := time.Parse(time.RFC3339, t)
	if err != nil {
		return t
	}
	return parsed.Format("Jan 2 15:04")
}

func formatRelativeTime(t string) string {
	parsed, err := time.Parse(time.RFC3339, t)
	if err != nil {
		return t
	}

	now := time.Now()
	diff := now.Sub(parsed)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "yesterday"
		}
		return fmt.Sprintf("%d days ago", days)
	default:
		return parsed.Format("Jan 2, 2006")
	}
}

// formatJSONSyntaxHighlighted returns JSON with HTML spans for syntax highlighting
func formatJSONSyntaxHighlighted(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}

	s := string(b)

	// Escape HTML first
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")

	// Highlight strings (values in quotes)
	stringRegex := regexp.MustCompile(`"([^"\\]|\\.)*"`)
	s = stringRegex.ReplaceAllStringFunc(s, func(match string) string {
		// Check if it's a key (followed by :) or a value
		return `<span class="json-string">` + match + `</span>`
	})

	// Highlight numbers
	numberRegex := regexp.MustCompile(`\b(-?\d+\.?\d*)\b`)
	s = numberRegex.ReplaceAllString(s, `<span class="json-number">$1</span>`)

	// Highlight booleans and null
	s = strings.ReplaceAll(s, "true", `<span class="json-boolean">true</span>`)
	s = strings.ReplaceAll(s, "false", `<span class="json-boolean">false</span>`)
	s = strings.ReplaceAll(s, "null", `<span class="json-null">null</span>`)

	return s
}

func truncateString(s string, max int) string {
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

// isFilePath checks if a string looks like a file path
func isFilePath(s string) bool {
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "~/")
}

// extractKeyValue attempts to extract important key-value pairs from result data
type KeyValuePair struct {
	Key       string
	Value     string
	ValueType string // "text", "code", "path", "number", "boolean"
}

func extractKeyValues(data any) []KeyValuePair {
	var pairs []KeyValuePair

	switch v := data.(type) {
	case map[string]any:
		// Priority keys to show first
		priorityKeys := []string{"status", "message", "error", "result", "output", "count", "total", "success", "failed", "duration", "path", "file", "command"}

		for _, key := range priorityKeys {
			if val, ok := v[key]; ok {
				pairs = append(pairs, makeKeyValuePair(key, val))
			}
		}

		// Add other keys
		for key, val := range v {
			// Skip if already added
			found := false
			for _, p := range pairs {
				if p.Key == key {
					found = true
					break
				}
			}
			if !found {
				// Skip complex nested objects for the summary
				switch val.(type) {
				case map[string]any, []any:
					continue
				}
				pairs = append(pairs, makeKeyValuePair(key, val))
			}
		}
	}

	// Limit to 10 pairs max
	if len(pairs) > 10 {
		pairs = pairs[:10]
	}

	return pairs
}

func makeKeyValuePair(key string, val any) KeyValuePair {
	pair := KeyValuePair{Key: key}

	switch v := val.(type) {
	case string:
		pair.Value = v
		if isFilePath(v) {
			pair.ValueType = "path"
		} else if len(v) > 100 {
			pair.Value = v[:100] + "..."
			pair.ValueType = "text"
		} else {
			pair.ValueType = "text"
		}
	case float64:
		if v == float64(int(v)) {
			pair.Value = fmt.Sprintf("%d", int(v))
		} else {
			pair.Value = fmt.Sprintf("%.2f", v)
		}
		pair.ValueType = "number"
	case int, int64:
		pair.Value = fmt.Sprintf("%d", v)
		pair.ValueType = "number"
	case bool:
		pair.Value = fmt.Sprintf("%t", v)
		pair.ValueType = "boolean"
	default:
		pair.Value = fmt.Sprintf("%v", v)
		pair.ValueType = "text"
	}

	return pair
}

// hasNestedData checks if the data has nested objects/arrays
func hasNestedData(data any) bool {
	switch v := data.(type) {
	case map[string]any:
		for _, val := range v {
			switch val.(type) {
			case map[string]any, []any:
				return true
			}
		}
	case []any:
		return len(v) > 0
	}
	return false
}

// countDataItems returns the count of items if data is an array
func countDataItems(data any) int {
	if arr, ok := data.([]any); ok {
		return len(arr)
	}
	return 0
}

// formatKeyName makes a key name more human readable
func formatKeyName(key string) string {
	// Common abbreviations
	abbrevs := map[string]string{
		"id":    "ID",
		"ulid":  "ULID",
		"ksuid": "KSUID",
		"ts":    "Timestamp",
		"uri":   "URI",
		"url":   "URL",
		"ns":    "Namespace",
		"err":   "Error",
		"msg":   "Message",
	}

	// Replace underscores and hyphens with spaces
	s := strings.ReplaceAll(key, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")

	// Title case
	words := strings.Fields(s)
	for i, word := range words {
		if val, ok := abbrevs[strings.ToLower(word)]; ok {
			words[i] = val
		} else if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}

// Specialized Pattern Detection

func IsJobID(s string) bool {
	// 26 chars for ULID, 27 for KSUID
	if len(s) < 26 || len(s) > 30 {
		return false
	}
	// Check if it's alphanumeric and looks like a generic ID
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
			return false
		}
	}
	return true
}

// Specialized Pattern Detection

func IsSearchData(data any) bool {
	m, ok := data.(map[string]any)
	if !ok {
		return false
	}
	// Check for match_count and preview with file/line
	if _, hasMatches := m["match_count"]; hasMatches {
		if preview, ok := m["preview"].([]any); ok && len(preview) > 0 {
			for _, p := range preview {
				if item, ok := p.(map[string]any); ok {
					if _, hasFile := item["file"]; hasFile {
						return true
					}
				}
			}
		}
	}
	return false
}

func IsFileData(data any) bool {
	m, ok := data.(map[string]any)
	if !ok {
		return false
	}
	// Check for entry_count or preview with path/is_dir
	if preview, ok := m["preview"].([]any); ok && len(preview) > 0 {
		for _, p := range preview {
			if item, ok := p.(map[string]any); ok {
				if _, hasPath := item["path"]; hasPath {
					if _, hasIsDir := item["is_dir"]; hasIsDir {
						return true
					}
				}
			}
		}
	}
	return false
}

func IsDiffData(data any) bool {
	m, ok := data.(map[string]any)
	if !ok {
		return false
	}
	if _, ok := m["diff"]; ok {
		return true
	}
	if _, ok := m["patch"]; ok {
		return true
	}
	if v, ok := m["hunks"].([]any); ok && len(v) > 0 {
		return true
	}
	return false
}

func IsSymbolData(data any) bool {
	m, ok := data.(map[string]any)
	if !ok {
		return false
	}
	_, hasSymbolCount := m["symbol_count"]
	preview, hasPreview := m["preview"]
	if hasSymbolCount && hasPreview {
		_, ok := preview.([]any)
		return ok
	}
	return false
}

// IsTableData checks if the data represents an array of objects that could be shown in a table
func IsTableData(data any) bool {
	arr, ok := data.([]any)
	if !ok || len(arr) == 0 {
		return false
	}
	// Check if first element is a map
	_, ok = arr[0].(map[string]any)
	return ok
}

// GetKeys returns sorted keys for a map
func GetKeys(m map[string]any) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	// Sort keys if needed, but for now just return
	return keys
}
