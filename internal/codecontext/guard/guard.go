package guard

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Finding represents a detected secret or sensitive data.
type Finding struct {
	// Pattern is the name of the matching pattern.
	Pattern string `json:"pattern"`

	// Line is the 1-indexed line number where the secret was found.
	Line int `json:"line"`

	// Column is the 1-indexed column where the match starts.
	Column int `json:"column"`

	// Severity indicates how serious the finding is.
	Severity Severity `json:"severity"`

	// Masked is the matched text with the actual secret redacted.
	Masked string `json:"masked,omitempty"`
}

// Severity indicates the seriousness of a finding.
type Severity string

const (
	SeverityHigh   Severity = "high"   // Definite secret (private key, explicit API key)
	SeverityMedium Severity = "medium" // Likely secret (generic key pattern)
	SeverityLow    Severity = "low"    // Possible secret (suspicious pattern)
)

// Result contains the scan results for a file.
type Result struct {
	// Path is the file path that was scanned.
	Path string `json:"path"`

	// Findings contains any detected secrets.
	Findings []Finding `json:"findings,omitempty"`

	// Blocked indicates whether the file should be blocked from output.
	Blocked bool `json:"blocked"`

	// Reason explains why the file was blocked (if applicable).
	Reason string `json:"reason,omitempty"`
}

// Scanner detects secrets in file content.
type Scanner struct {
	patterns []pattern
	mode     Mode
}

// Mode controls how the scanner behaves when secrets are found.
type Mode string

const (
	// ModeWarn allows the file but includes warnings.
	ModeWarn Mode = "warn"

	// ModeBlock rejects files containing high-severity secrets.
	ModeBlock Mode = "block"

	// ModeStrip removes secret lines from output (not yet implemented).
	ModeStrip Mode = "strip"
)

// Opts configures the scanner.
type Opts struct {
	// Mode controls behavior when secrets are found.
	Mode Mode

	// ExcludePatterns skips patterns by name.
	ExcludePatterns []string

	// IncludeLowSeverity includes low-severity patterns in scanning.
	IncludeLowSeverity bool
}

// pattern represents a secret detection pattern.
type pattern struct {
	Name     string
	Regex    *regexp.Regexp
	Severity Severity
}

// Default patterns for secret detection.
var defaultPatterns = []pattern{
	// High severity - definite secrets
	{Name: "private_key", Regex: regexp.MustCompile(`-----BEGIN\s+(RSA|DSA|EC|OPENSSH|PGP)\s+PRIVATE KEY-----`), Severity: SeverityHigh},
	{Name: "aws_secret_key", Regex: regexp.MustCompile(`(?i)(aws_secret_access_key|aws_secret_key)\s*[=:]\s*["']?[A-Za-z0-9/+=]{40}["']?`), Severity: SeverityHigh},
	{Name: "aws_access_key", Regex: regexp.MustCompile(`AKIA[0-9A-Z]{16}`), Severity: SeverityHigh},
	{Name: "github_token", Regex: regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{36,}`), Severity: SeverityHigh},
	{Name: "github_pat", Regex: regexp.MustCompile(`github_pat_[A-Za-z0-9_]{22,}`), Severity: SeverityHigh},
	{Name: "slack_token", Regex: regexp.MustCompile(`xox[baprs]-[0-9]{10,13}-[0-9]{10,13}[a-zA-Z0-9-]*`), Severity: SeverityHigh},
	{Name: "stripe_key", Regex: regexp.MustCompile(`sk_live_[0-9a-zA-Z]{24,}`), Severity: SeverityHigh},
	{Name: "google_api_key", Regex: regexp.MustCompile(`AIza[0-9A-Za-z\-_]{35}`), Severity: SeverityHigh},
	{Name: "openai_key", Regex: regexp.MustCompile(`sk-[A-Za-z0-9]{48,}`), Severity: SeverityHigh},
	{Name: "anthropic_key", Regex: regexp.MustCompile(`sk-ant-[A-Za-z0-9\-_]{95,}`), Severity: SeverityHigh},

	// Medium severity - likely secrets
	{Name: "generic_secret", Regex: regexp.MustCompile(`(?i)(secret|password|passwd|pwd|token|api_key|apikey|auth_token|access_token|private_key)\s*[=:]\s*["'][^"'\s]{8,}["']`), Severity: SeverityMedium},
	{Name: "bearer_token", Regex: regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9\-_.]{20,}`), Severity: SeverityMedium},
	{Name: "basic_auth", Regex: regexp.MustCompile(`(?i)basic\s+[a-zA-Z0-9+/]{20,}={0,2}`), Severity: SeverityMedium},
	{Name: "jwt_token", Regex: regexp.MustCompile(`eyJ[A-Za-z0-9_-]*\.eyJ[A-Za-z0-9_-]*\.[A-Za-z0-9_-]*`), Severity: SeverityMedium},
	{Name: "connection_string", Regex: regexp.MustCompile(`(?i)(mongodb|postgres|mysql|redis|amqp|jdbc)://[^\s"']+:[^\s"'@]+@[^\s"']+`), Severity: SeverityMedium},

	// Low severity - possible secrets (disabled by default)
	{Name: "hex_32", Regex: regexp.MustCompile(`[a-fA-F0-9]{32}`), Severity: SeverityLow},
	{Name: "base64_long", Regex: regexp.MustCompile(`[A-Za-z0-9+/]{50,}={0,2}`), Severity: SeverityLow},
}

// New creates a new secret scanner with the given options.
func New(opts Opts) *Scanner {
	if opts.Mode == "" {
		opts.Mode = ModeWarn
	}

	excludeSet := make(map[string]bool)
	for _, name := range opts.ExcludePatterns {
		excludeSet[name] = true
	}

	var patterns []pattern
	for _, p := range defaultPatterns {
		if excludeSet[p.Name] {
			continue
		}
		if p.Severity == SeverityLow && !opts.IncludeLowSeverity {
			continue
		}
		patterns = append(patterns, p)
	}

	return &Scanner{
		patterns: patterns,
		mode:     opts.Mode,
	}
}

// NewDefault creates a scanner with default settings (warn mode, no low severity).
func NewDefault() *Scanner {
	return New(Opts{Mode: ModeWarn})
}

// Scan checks content for secrets and returns findings.
func (s *Scanner) Scan(ctx context.Context, path string, content []byte) *Result {
	result := &Result{Path: path}

	lines := strings.Split(string(content), "\n")

	for lineNum, line := range lines {
		// Check context cancellation
		if ctx.Err() != nil {
			break
		}

		for _, p := range s.patterns {
			loc := p.Regex.FindStringIndex(line)
			if loc != nil {
				finding := Finding{
					Pattern:  p.Name,
					Line:     lineNum + 1, // 1-indexed
					Column:   loc[0] + 1,  // 1-indexed
					Severity: p.Severity,
					Masked:   maskSecret(line, loc[0], loc[1]),
				}
				result.Findings = append(result.Findings, finding)
			}
		}
	}

	// Determine if file should be blocked
	if s.mode == ModeBlock {
		for _, f := range result.Findings {
			if f.Severity == SeverityHigh {
				result.Blocked = true
				result.Reason = fmt.Sprintf("high-severity secret detected: %s at line %d", f.Pattern, f.Line)
				break
			}
		}
	}

	return result
}

// ScanString is a convenience method for scanning string content.
func (s *Scanner) ScanString(ctx context.Context, path, content string) *Result {
	return s.Scan(ctx, path, []byte(content))
}

// maskSecret replaces the middle portion of a secret with asterisks.
func maskSecret(line string, start, end int) string {
	if end-start <= 8 {
		return line[:start] + "****" + line[end:]
	}

	// Show first 4 and last 4 characters
	secret := line[start:end]
	visible := 4
	if len(secret) < 12 {
		visible = 2
	}

	masked := secret[:visible] + strings.Repeat("*", len(secret)-2*visible) + secret[len(secret)-visible:]
	return line[:start] + masked + line[end:]
}

// HasHighSeverity returns true if any finding is high severity.
func (r *Result) HasHighSeverity() bool {
	for _, f := range r.Findings {
		if f.Severity == SeverityHigh {
			return true
		}
	}
	return false
}

// HasFindings returns true if any secrets were detected.
func (r *Result) HasFindings() bool {
	return len(r.Findings) > 0
}

// HighSeverityFindings returns only high-severity findings.
func (r *Result) HighSeverityFindings() []Finding {
	var high []Finding
	for _, f := range r.Findings {
		if f.Severity == SeverityHigh {
			high = append(high, f)
		}
	}
	return high
}
