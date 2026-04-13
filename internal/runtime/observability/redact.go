package observability

import (
	"regexp"
	"strings"
)

// redactPatterns are regex patterns for values that should be redacted from events.
var redactPatterns = []*regexp.Regexp{
	// Environment variable assignments: KEY=value (capture common secret patterns)
	regexp.MustCompile(`(?i)(password|passwd|secret|token|api_key|apikey|auth|credential|private_key)\s*[:=]\s*\S+`),
	// Bearer tokens in strings
	regexp.MustCompile(`(?i)bearer\s+\S+`),
	// Long base64 strings that look like keys/tokens (40+ chars)
	regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`),
}

// redactReplacement is the value used to replace sensitive content.
const redactReplacement = "[REDACTED]"

// RedactString redacts sensitive patterns from a string value.
// This is used to scrub ErrorMessage and Data values before persistence.
func RedactString(s string) string {
	for _, p := range redactPatterns {
		s = p.ReplaceAllString(s, redactReplacement)
	}
	return s
}

// RedactEvent applies redaction to sensitive fields of a WideEvent in place.
// It scrubs ErrorMessage and recursively walks Data map values containing strings.
func RedactEvent(event *WideEvent) {
	if event == nil {
		return
	}

	event.ErrorMessage = RedactString(event.ErrorMessage)

	if event.Data != nil {
		redactMap(event.Data)
	}
}

// redactMap recursively walks a map and redacts string values.
func redactMap(m map[string]any) {
	for k, v := range m {
		switch val := v.(type) {
		case string:
			if isSensitiveKey(k) {
				m[k] = redactReplacement
			} else {
				m[k] = RedactString(val)
			}
		case map[string]any:
			redactMap(val)
		}
	}
}

// isSensitiveKey returns true if the map key suggests a sensitive value.
func isSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, pattern := range []string{
		"password", "passwd", "secret", "token", "api_key", "apikey",
		"auth", "credential", "private_key", "access_key", "session_key",
	} {
		if strings.Contains(k, pattern) {
			return true
		}
	}
	return false
}
