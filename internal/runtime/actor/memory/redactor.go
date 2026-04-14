package memory

import (
	"regexp"
	"sync"
)

// SecretRedactor removes sensitive information before persistence.
//
// Contract: Redact before persistence
// - Summaries, learnings, and L1/L2 artifacts are redacted
// - Raw turns may contain secrets; only archive redacted versions
type SecretRedactor struct {
	mu       sync.RWMutex
	patterns []*regexp.Regexp
}

// Default redaction patterns (Go regex).
var defaultRedactPatterns = []string{
	// Authorization headers
	`(?i)authorization:\s*\S+`,

	// Generic key-value patterns
	`(?i)(api[_-]?key|secret|token|password|credential)\s*[:=]\s*["']?[^\s"']+`,

	// GitHub tokens
	`ghp_[a-zA-Z0-9]{36}`,          // Personal access token (classic)
	`github_pat_[a-zA-Z0-9_]{22,}`, // Fine-grained PAT
	`gho_[a-zA-Z0-9]{36}`,          // OAuth token
	`ghu_[a-zA-Z0-9]{36}`,          // User-to-server token
	`ghs_[a-zA-Z0-9]{36}`,          // Server-to-server token
	`ghr_[a-zA-Z0-9]{36}`,          // Refresh token

	// OpenAI
	`sk-[a-zA-Z0-9]{48}`,          // API key
	`sk-proj-[a-zA-Z0-9\-_]{20,}`, // Project key

	// Anthropic
	`sk-ant-[a-zA-Z0-9\-_]{20,}`, // API key

	// Google
	`AIza[a-zA-Z0-9_-]{35}`, // API key

	// AWS
	`AKIA[A-Z0-9]{16}`, // Access key ID
	`(?i)aws[_-]?secret[_-]?access[_-]?key\s*[:=]\s*[^\s]+`,

	// Private keys
	`-----BEGIN (RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`,
	`-----BEGIN PGP PRIVATE KEY BLOCK-----`,

	// Slack
	`xox[baprs]-[a-zA-Z0-9-]+`,

	// Stripe
	`sk_live_[a-zA-Z0-9]{24,}`,
	`sk_test_[a-zA-Z0-9]{24,}`,
	`rk_live_[a-zA-Z0-9]{24,}`,
	`rk_test_[a-zA-Z0-9]{24,}`,

	// Twilio
	`SK[a-f0-9]{32}`,

	// SendGrid
	`SG\.[a-zA-Z0-9_-]{22,}\.[a-zA-Z0-9_-]{22,}`,

	// Database connection strings
	`(?i)(postgres|mysql|mongodb|redis)://[^\s]+`,

	// JWT tokens (basic pattern - they're base64 with dots)
	`eyJ[a-zA-Z0-9_-]*\.eyJ[a-zA-Z0-9_-]*\.[a-zA-Z0-9_-]*`,
}

// NewSecretRedactor creates a new SecretRedactor with default patterns.
func NewSecretRedactor() *SecretRedactor {
	return NewSecretRedactorWithPatterns(defaultRedactPatterns)
}

// NewSecretRedactorWithPatterns creates a SecretRedactor with custom patterns.
func NewSecretRedactorWithPatterns(patterns []string) *SecretRedactor {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			compiled = append(compiled, re)
		}
		// Skip patterns that fail to compile
	}
	return &SecretRedactor{patterns: compiled}
}

// AddPattern adds a custom redaction pattern.
func (r *SecretRedactor) AddPattern(pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.patterns = append(r.patterns, re)
	r.mu.Unlock()
	return nil
}

// Redact replaces all sensitive patterns with [REDACTED].
func (r *SecretRedactor) Redact(text string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := text
	for _, re := range r.patterns {
		result = re.ReplaceAllString(result, "[REDACTED]")
	}
	return result
}

// RedactWithPlaceholder replaces patterns with a custom placeholder.
func (r *SecretRedactor) RedactWithPlaceholder(text, placeholder string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := text
	for _, re := range r.patterns {
		result = re.ReplaceAllString(result, placeholder)
	}
	return result
}

// ContainsSecrets checks if text contains any secret patterns.
func (r *SecretRedactor) ContainsSecrets(text string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, re := range r.patterns {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// FindSecrets returns all matches of secret patterns in text.
// Useful for debugging/auditing.
func (r *SecretRedactor) FindSecrets(text string) []SecretMatch {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var matches []SecretMatch
	for i, re := range r.patterns {
		for _, loc := range re.FindAllStringIndex(text, -1) {
			matches = append(matches, SecretMatch{
				PatternIndex: i,
				Start:        loc[0],
				End:          loc[1],
				// Don't include the actual secret value
			})
		}
	}
	return matches
}

// SecretMatch represents a found secret (without the actual value).
type SecretMatch struct {
	PatternIndex int
	Start        int
	End          int
}
