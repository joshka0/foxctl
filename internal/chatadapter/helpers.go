package chatadapter

import (
	"fmt"
	"strings"
	"time"
)

// TruncateRunes returns s truncated to at most maxLen runes.
// If maxLen <= 0, it returns the empty string.
func TruncateRunes(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}

// TruncateRunesWithSuffix returns a string that is at most maxLen runes long.
//
// It truncates s as needed to reserve room for suffix, then always appends
// suffix (even when s already fits). This is useful for streaming UIs where you
// want to force a trailing marker (e.g., "...") on partial updates.
//
// Note: If suffix itself is >= maxLen, the returned string is the first maxLen
// runes of suffix.
func TruncateRunesWithSuffix(s string, suffix string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}

	sRunes := []rune(s)
	sufRunes := []rune(suffix)

	// Degenerate case: suffix is longer than the allowed message length.
	if len(sufRunes) >= maxLen {
		return string(sufRunes[:maxLen])
	}

	avail := maxLen - len(sufRunes)
	if len(sRunes) <= avail {
		return s + suffix
	}
	return string(sRunes[:avail]) + suffix
}

// TruncateRunesWithEllipsis returns s truncated to at most maxLen runes.
//
// Unlike TruncateRunesWithSuffix, it appends "..." only when truncation occurs
// (and the returned string is still <= maxLen runes).
func TruncateRunesWithEllipsis(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return TruncateRunesWithSuffix(s, "...", maxLen)
}

// IsPartial returns true if the metadata indicates a streaming partial event.
func IsPartial(meta map[string]any) bool {
	if meta == nil {
		return false
	}
	if v, ok := meta["partial"]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// GetDataString returns a best-effort string field from a loosely typed map.
func GetDataString(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	v, ok := data[key]
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val)
	case float64:
		return fmt.Sprintf("%.0f", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// FormatDuration formats a millisecond duration for compact chat displays.
func FormatDuration(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	if d < time.Second {
		return fmt.Sprintf("%dms", ms)
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%.1fm", d.Minutes())
}
