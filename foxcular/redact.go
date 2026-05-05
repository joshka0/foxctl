package foxcular

import (
	"regexp"
	"strings"
)

// RedactionPolicy controls how sensitive data is scrubbed from events before
// they reach any drain. Redaction always runs before drain delivery.
type RedactionPolicy struct {
	// Mask is the replacement string for redacted values.
	Mask string

	// SensitiveKeys are key names (case-insensitive substring match) whose
	// values are always masked regardless of value content.
	SensitiveKeys []string

	// CustomKeys are additional developer-configured key names to redact.
	CustomKeys []string

	// ValuePatterns are regex patterns applied to all string values
	// (including non-sensitive-key values).
	ValuePatterns []*regexp.Regexp

	// CustomValuePatterns are additional developer-configured value patterns.
	CustomValuePatterns []*regexp.Regexp

	// enabled controls whether redaction is active.
	enabled bool
}

// RedactionOption configures a RedactionPolicy.
type RedactionOption func(*RedactionPolicy)

// WithMask sets the replacement mask string.
func WithMask(mask string) RedactionOption {
	return func(p *RedactionPolicy) { p.Mask = mask }
}

// WithSensitiveKeys appends sensitive key names.
func WithSensitiveKeys(keys ...string) RedactionOption {
	return func(p *RedactionPolicy) { p.SensitiveKeys = append(p.SensitiveKeys, keys...) }
}

// WithCustomKeys appends custom redaction key names.
func WithCustomKeys(keys ...string) RedactionOption {
	return func(p *RedactionPolicy) { p.CustomKeys = append(p.CustomKeys, keys...) }
}

// WithCustomValuePatterns appends custom value redaction patterns.
func WithCustomValuePatterns(patterns ...*regexp.Regexp) RedactionOption {
	return func(p *RedactionPolicy) { p.CustomValuePatterns = append(p.CustomValuePatterns, patterns...) }
}

// NewRedactionPolicy creates a RedactionPolicy with built-in defaults.
func NewRedactionPolicy(opts ...RedactionOption) *RedactionPolicy {
	p := &RedactionPolicy{
		Mask: "[REDACTED]",
		SensitiveKeys: []string{
			"password", "passwd", "secret", "token", "api_key", "apikey",
			"authorization", "auth", "credential", "private_key", "access_key",
			"session_key", "secret_key",
		},
		enabled: true,
	}
	for _, opt := range opts {
		opt(p)
	}

	// Built-in value patterns for known sensitive formats.
	p.ValuePatterns = []*regexp.Regexp{
		// Bearer tokens
		regexp.MustCompile(`(?i)bearer\s+\S+`),
		// JWT tokens (three base64url segments separated by dots)
		regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),
		// Email addresses
		regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),
		// Credit card numbers (13-19 digits with optional spaces/dashes)
		regexp.MustCompile(`\b\d{4}[\s\-]?\d{4}[\s\-]?\d{4}[\s\-]?\d{1,4}\b`),
		// IPv4 addresses
		regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`),
		// Phone numbers (various formats)
		regexp.MustCompile(`(?i)(?:\+?\d{1,3}[\s\-]?)?\(?\d{2,4}\)?[\s\-]?\d{3,4}[\s\-]?\d{3,4}`),
		// IBAN (2 letter country code + 2 check digits + up to 30 alphanumeric)
		regexp.MustCompile(`\b[A-Z]{2}\d{2}[A-Z0-9]{4,30}\b`),
		// Long base64 strings (40+ chars, likely keys/tokens)
		regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`),
	}

	return p
}

// DisabledRedactionPolicy returns a policy that performs no redaction.
func DisabledRedactionPolicy() *RedactionPolicy {
	return &RedactionPolicy{enabled: false}
}

// Enabled reports whether redaction is active.
func (p *RedactionPolicy) Enabled() bool {
	return p != nil && p.enabled
}

// RedactEvent returns a new event with sensitive data redacted. The original
// event is not modified.
func (p *RedactionPolicy) RedactEvent(event *Event) *Event {
	if p == nil || !p.enabled || event == nil {
		return event
	}

	redacted := event.Clone()

	// Redact error message.
	redacted.ErrorMessage = p.redactString(redacted.ErrorMessage)

	// Redact message field.
	redacted.Message = p.redactString(redacted.Message)

	// Redact data map.
	if redacted.Data != nil {
		p.redactMap(redacted.Data)
	}

	return redacted
}

// redactMap recursively walks a map and redacts values.
func (p *RedactionPolicy) redactMap(m map[string]any) {
	for k, v := range m {
		switch val := v.(type) {
		case string:
			if p.isSensitiveKey(k) {
				m[k] = p.Mask
			} else {
				m[k] = p.redactString(val)
			}
		case map[string]any:
			p.redactMap(val)
		case []any:
			m[k] = p.redactSlice(val)
		default:
			// Non-string, non-container values are left as-is.
		}
	}
}

// redactSlice recursively redacts values within a slice.
func (p *RedactionPolicy) redactSlice(s []any) []any {
	result := make([]any, len(s))
	for i, v := range s {
		switch val := v.(type) {
		case string:
			result[i] = p.redactString(val)
		case map[string]any:
			cp := make(map[string]any, len(val))
			for k, sv := range val {
				cp[k] = sv
			}
			p.redactMap(cp)
			result[i] = cp
		case []any:
			result[i] = p.redactSlice(val)
		default:
			result[i] = v
		}
	}
	return result
}

// redactString applies all value patterns to a string.
func (p *RedactionPolicy) redactString(s string) string {
	if s == "" {
		return s
	}

	patterns := p.ValuePatterns
	patterns = append(patterns, p.CustomValuePatterns...)
	for _, pat := range patterns {
		s = pat.ReplaceAllString(s, p.Mask)
	}
	return s
}

// isSensitiveKey returns true if the key matches any sensitive key pattern.
func (p *RedactionPolicy) isSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	allKeys := make([]string, 0, len(p.SensitiveKeys)+len(p.CustomKeys))
	allKeys = append(allKeys, p.SensitiveKeys...)
	allKeys = append(allKeys, p.CustomKeys...)
	for _, pattern := range allKeys {
		if strings.Contains(k, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}
