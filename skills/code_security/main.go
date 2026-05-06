// Package main implements the code/security skill.
package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/fsutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/pathutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/sliceutil"
)

const command = "code/security"

// input defines the parameters for security scanning.
type input struct {
	Path              string   `json:"path"`
	ScanMode          string   `json:"scan_mode"`
	SeverityThreshold string   `json:"severity_threshold"`
	Categories        []string `json:"categories"`
	ExcludeTests      bool     `json:"exclude_tests"`
	MaxResults        int      `json:"max_results"`
}

// vulnerability represents a security vulnerability found during scanning.
type vulnerability struct {
	ID             string `json:"id"`
	Category       string `json:"category"`
	Severity       string `json:"severity"`
	CWE            string `json:"cwe,omitempty"`
	File           string `json:"file"`
	Line           int    `json:"line"`
	Issue          string `json:"issue"`
	CodeSnippet    string `json:"code_snippet"`
	Description    string `json:"description"`
	Recommendation string `json:"recommendation"`
	Confidence     string `json:"confidence"`
}

// securityPattern defines a security vulnerability detection pattern.
type securityPattern struct {
	ID             string
	Category       string
	Severity       string
	CWE            string
	Regex          *regexp.Regexp
	Issue          string
	Description    string
	Recommendation string
	Confidence     string
}

var securityPatterns = []securityPattern{
	// SQL Injection
	{
		ID:             "SQL-001",
		Category:       "injection",
		Severity:       "high",
		CWE:            "CWE-89",
		Regex:          regexp.MustCompile(`(?i)(query|execute|exec)\s*\(\s*["'].*\+.*["']`),
		Issue:          "SQL injection vulnerability",
		Description:    "User input concatenated directly into SQL query",
		Recommendation: "Use parameterized queries or prepared statements",
		Confidence:     "high",
	},
	{
		ID:             "SQL-002",
		Category:       "injection",
		Severity:       "high",
		CWE:            "CWE-89",
		Regex:          regexp.MustCompile(`(?i)(Query|Exec)\s*\(\s*fmt\.(Sprintf|Printf)`),
		Issue:          "SQL injection via string formatting",
		Description:    "SQL query built with string formatting",
		Recommendation: "Use parameterized queries with $1, $2 placeholders",
		Confidence:     "high",
	},

	// Command Injection
	{
		ID:             "CMD-001",
		Category:       "injection",
		Severity:       "critical",
		CWE:            "CWE-78",
		Regex:          regexp.MustCompile(`(?i)(exec\.|system\(|shell_exec|popen|subprocess\.call).*\+`),
		Issue:          "Command injection vulnerability",
		Description:    "User input passed to system command execution",
		Recommendation: "Validate and sanitize input, use safe APIs",
		Confidence:     "high",
	},
	{
		ID:             "CMD-002",
		Category:       "injection",
		Severity:       "critical",
		CWE:            "CWE-78",
		Regex:          regexp.MustCompile(`os\.system\([^)]*\+`),
		Issue:          "OS command injection",
		Description:    "Concatenating user input into system commands",
		Recommendation: "Use subprocess with argument list, not shell=True",
		Confidence:     "high",
	},

	// Hardcoded Secrets
	{
		ID:             "SECRET-001",
		Category:       "sensitive_data",
		Severity:       "critical",
		CWE:            "CWE-798",
		Regex:          regexp.MustCompile(`(password|passwd|pwd)\s*=\s*["'][^"']{4,}["']`),
		Issue:          "Hardcoded password",
		Description:    "Password hardcoded in source code",
		Recommendation: "Load from environment variables or secure vault",
		Confidence:     "high",
	},
	{
		ID:             "SECRET-002",
		Category:       "sensitive_data",
		Severity:       "critical",
		CWE:            "CWE-798",
		Regex:          regexp.MustCompile(`(api[_-]?key|apikey|api[_-]?secret)\s*[=:]\s*["'][^"']{8,}["']`),
		Issue:          "Hardcoded API key",
		Description:    "API key hardcoded in source code",
		Recommendation: "Use environment variables or secrets management",
		Confidence:     "high",
	},
	{
		ID:             "SECRET-003",
		Category:       "sensitive_data",
		Severity:       "critical",
		CWE:            "CWE-798",
		Regex:          regexp.MustCompile(`(secret[_-]?key|private[_-]?key)\s*[=:]\s*["'][^"']{16,}["']`),
		Issue:          "Hardcoded secret key",
		Description:    "Secret key hardcoded in source code",
		Recommendation: "Use secure key management system",
		Confidence:     "high",
	},
	{
		ID:             "SECRET-004",
		Category:       "sensitive_data",
		Severity:       "critical",
		CWE:            "CWE-798",
		Regex:          regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		Issue:          "AWS access key exposed",
		Description:    "AWS access key found in code",
		Recommendation: "Remove key, rotate credentials, use IAM roles",
		Confidence:     "high",
	},
	{
		ID:             "SECRET-005",
		Category:       "sensitive_data",
		Severity:       "critical",
		CWE:            "CWE-798",
		Regex:          regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),
		Issue:          "GitHub personal access token exposed",
		Description:    "GitHub token found in code",
		Recommendation: "Remove token, revoke and regenerate",
		Confidence:     "high",
	},
	{
		ID:             "SECRET-006",
		Category:       "sensitive_data",
		Severity:       "critical",
		CWE:            "CWE-798",
		Regex:          regexp.MustCompile(`-----BEGIN\s+(RSA\s+)?PRIVATE KEY-----`),
		Issue:          "Private key embedded in code",
		Description:    "Cryptographic private key in source",
		Recommendation: "Store keys securely, never commit to repository",
		Confidence:     "high",
	},

	// Weak Cryptography
	{
		ID:             "CRYPTO-001",
		Category:       "insecure_crypto",
		Severity:       "high",
		CWE:            "CWE-327",
		Regex:          regexp.MustCompile(`(?i)(md5|sha1)\.`),
		Issue:          "Weak cryptographic algorithm",
		Description:    "MD5 or SHA1 used for security purposes",
		Recommendation: "Use SHA-256, SHA-384, or SHA-512",
		Confidence:     "medium",
	},
	{
		ID:             "CRYPTO-002",
		Category:       "insecure_crypto",
		Severity:       "high",
		CWE:            "CWE-327",
		Regex:          regexp.MustCompile(`(?i)DES|RC4|RC2`),
		Issue:          "Broken encryption algorithm",
		Description:    "DES, RC4, or RC2 encryption is broken",
		Recommendation: "Use AES-256-GCM or ChaCha20-Poly1305",
		Confidence:     "high",
	},
	{
		ID:             "CRYPTO-003",
		Category:       "insecure_crypto",
		Severity:       "medium",
		CWE:            "CWE-330",
		Regex:          regexp.MustCompile(`(?i)(math\.random|Random\(\)|rand\.Intn)`),
		Issue:          "Weak random number generator",
		Description:    "Non-cryptographic RNG used",
		Recommendation: "Use crypto/rand for security-sensitive operations",
		Confidence:     "medium",
	},

	// XSS
	{
		ID:             "XSS-001",
		Category:       "xss",
		Severity:       "high",
		CWE:            "CWE-79",
		Regex:          regexp.MustCompile(`\.innerHTML\s*=`),
		Issue:          "Potential XSS via innerHTML",
		Description:    "Setting innerHTML with user data",
		Recommendation: "Use textContent or sanitize with DOMPurify",
		Confidence:     "medium",
	},
	{
		ID:             "XSS-002",
		Category:       "xss",
		Severity:       "high",
		CWE:            "CWE-79",
		Regex:          regexp.MustCompile(`document\.write\(`),
		Issue:          "Potential XSS via document.write",
		Description:    "document.write() can enable XSS",
		Recommendation: "Use safer DOM manipulation methods",
		Confidence:     "medium",
	},
	{
		ID:             "XSS-003",
		Category:       "xss",
		Severity:       "high",
		CWE:            "CWE-79",
		Regex:          regexp.MustCompile(`dangerouslySetInnerHTML`),
		Issue:          "React dangerouslySetInnerHTML usage",
		Description:    "Dangerous HTML rendering in React",
		Recommendation: "Sanitize content or use safe rendering",
		Confidence:     "medium",
	},

	// Path Traversal
	{
		ID:             "PATH-001",
		Category:       "path_traversal",
		Severity:       "high",
		CWE:            "CWE-22",
		Regex:          regexp.MustCompile(`(open|readFile|writeFile)\([^)]*\+`),
		Issue:          "Path traversal vulnerability",
		Description:    "File path constructed from user input",
		Recommendation: "Validate and sanitize file paths, use allowlists",
		Confidence:     "medium",
	},
	{
		ID:             "PATH-002",
		Category:       "path_traversal",
		Severity:       "high",
		CWE:            "CWE-22",
		Regex:          regexp.MustCompile(`\.\./`),
		Issue:          "Directory traversal pattern",
		Description:    "Path contains directory traversal sequence",
		Recommendation: "Reject paths with ../, use path.Clean()",
		Confidence:     "low",
	},

	// Unsafe Deserialization
	{
		ID:             "DESER-001",
		Category:       "unsafe_deserialization",
		Severity:       "critical",
		CWE:            "CWE-502",
		Regex:          regexp.MustCompile(`(?i)(pickle\.loads|yaml\.load\(|unserialize)`),
		Issue:          "Unsafe deserialization",
		Description:    "Deserializing untrusted data",
		Recommendation: "Use safe_load() or validate input structure",
		Confidence:     "high",
	},

	// SSRF
	{
		ID:             "SSRF-001",
		Category:       "ssrf",
		Severity:       "high",
		CWE:            "CWE-918",
		Regex:          regexp.MustCompile(`(http\.|requests\.|fetch\().*\+`),
		Issue:          "Server-Side Request Forgery risk",
		Description:    "HTTP request with user-controlled URL",
		Recommendation: "Validate URLs against allowlist",
		Confidence:     "medium",
	},

	// Logging Sensitive Data
	{
		ID:             "LOG-001",
		Category:       "sensitive_data",
		Severity:       "medium",
		CWE:            "CWE-532",
		Regex:          regexp.MustCompile(`(?i)(log|print|console)\.(info|debug|warn|error).*password`),
		Issue:          "Logging sensitive data",
		Description:    "Logging password or sensitive information",
		Recommendation: "Never log sensitive data, redact if necessary",
		Confidence:     "medium",
	},

	// SQL Direct Query
	{
		ID:             "SQL-003",
		Category:       "injection",
		Severity:       "high",
		CWE:            "CWE-89",
		Regex:          regexp.MustCompile(`(?i)db\.(query|exec|execute)\([^)]*f["']`),
		Issue:          "SQL query with f-string",
		Description:    "SQL query using f-string formatting",
		Recommendation: "Use parameterized queries",
		Confidence:     "high",
	},

	// JavaScript eval
	{
		ID:             "EVAL-001",
		Category:       "code_injection",
		Severity:       "critical",
		CWE:            "CWE-95",
		Regex:          regexp.MustCompile(`\beval\s*\(`),
		Issue:          "Use of eval()",
		Description:    "eval() executes arbitrary code",
		Recommendation: "Never use eval(), use JSON.parse() or safe alternatives",
		Confidence:     "high",
	},
	{
		ID:             "EVAL-002",
		Category:       "code_injection",
		Severity:       "critical",
		CWE:            "CWE-95",
		Regex:          regexp.MustCompile(`Function\s*\(`),
		Issue:          "Dynamic Function constructor",
		Description:    "Function() constructor can execute arbitrary code",
		Recommendation: "Avoid dynamic code generation",
		Confidence:     "high",
	},
}

// main is the skill entry point for code/security.
func main() {
	skillmain.Main(command, skillmain.Chain(run,
		skillmain.WithRecover[input](),
	))
}

// run orchestrates security vulnerability scanning with pattern matching and severity filtering.
//
// Index:
//   Purpose: Scan code for security vulnerabilities using regex patterns with configurable severity and category filtering
//   Flow: validate input → resolve path → scan directory/file → filter by severity → sort results → emit findings
//   SideEffects: file system traversal; pattern matching; sensitive data redaction; result persistence
//   FailureModes: invalid paths, file access errors, pattern matching failures
//   Observability: emits vulnerability counts, severity breakdown, category statistics, and risk scores
//   Related: scanDirectory, scanFile, selectPatterns, filterBySeverity, calculateStats
//   Keywords: code/security, vulnerability_scanning, security_patterns, injection_detection, secret_detection
//
// [[domain:security-vulnerability-scanning]]
// [[invariant:secret-redaction]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Apply defaults
	if in.ScanMode == "" {
		in.ScanMode = "scan"
	}
	if in.SeverityThreshold == "" {
		in.SeverityThreshold = "low"
	}
	if in.MaxResults <= 0 {
		in.MaxResults = 100
	}

	workspace, searchPath, err := skillmain.ResolvePath(rc, in.Path)
	if err != nil {
		return err
	}

	info, err := os.Stat(searchPath)
	if err != nil {
		return skillerr.WrapIO("stat path", err)
	}

	var vulns []vulnerability
	if info.IsDir() {
		vulns, err = scanDirectory(searchPath, workspace, in)
	} else {
		vulns, err = scanFile(searchPath, workspace, in)
	}
	if err != nil {
		return err
	}

	// Filter by severity threshold
	vulns = filterBySeverity(vulns, in.SeverityThreshold)

	// Sort by severity
	sort.Slice(vulns, func(i, j int) bool {
		return severityScore(vulns[i].Severity) > severityScore(vulns[j].Severity)
	})

	// Limit results
	vulns = sliceutil.Limit(vulns, in.MaxResults)

	// Calculate statistics
	stats := calculateStats(vulns)

	previewResult, err := skillout.PreviewAndPersistNDJSON(ctx, rc, vulns, rc.MaxPreview, "code_security", true)
	if err != nil {
		return err
	}

	data := map[string]any{
		"scan_mode":           in.ScanMode,
		"severity_threshold":  in.SeverityThreshold,
		"vulnerability_count": len(vulns),
		"vulnerabilities":     previewResult.Preview,
		"statistics":          stats,
	}
	skillout.AddArtifact(data, previewResult.Artifact)

	return skillout.Emit(rc, command, data)
}

// scanDirectory walks a directory tree and scans all eligible files for security vulnerabilities.
func scanDirectory(dir, workspace string, in input) ([]vulnerability, error) {
	var vulns []vulnerability

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if fsutil.ShouldSkipHiddenOrCommon(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		if in.ExcludeTests && fsutil.IsTestFile(d.Name()) {
			return nil
		}

		fileVulns, err := scanFile(path, workspace, in)
		if err != nil {
			return nil
		}

		vulns = append(vulns, fileVulns...)
		return nil
	})

	return vulns, err
}

// scanFile scans a single file for security vulnerabilities using pattern matching.
func scanFile(path, workspace string, in input) ([]vulnerability, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Skip binary files
	if fsutil.IsBinaryContent(content) {
		return nil, nil
	}

	relPath := pathutil.RelTo(workspace, path)
	lines := strings.Split(string(content), "\n")

	var vulns []vulnerability
	patterns := selectPatterns(in)

	for i, line := range lines {
		for _, pattern := range patterns {
			if pattern.Regex.MatchString(line) {
				snippet := strings.TrimSpace(line)
				if pattern.Category == "sensitive_data" {
					// Redact sensitive data
					if len(snippet) > 20 {
						snippet = snippet[:10] + "...[REDACTED]..." + snippet[len(snippet)-10:]
					} else {
						snippet = "[REDACTED]"
					}
				} else if len(snippet) > 100 {
					snippet = snippet[:100] + "..."
				}

				vuln := vulnerability{
					ID:             pattern.ID,
					Category:       pattern.Category,
					Severity:       pattern.Severity,
					CWE:            pattern.CWE,
					File:           relPath,
					Line:           i + 1,
					Issue:          pattern.Issue,
					CodeSnippet:    snippet,
					Description:    pattern.Description,
					Recommendation: pattern.Recommendation,
					Confidence:     pattern.Confidence,
				}

				vulns = append(vulns, vuln)
			}
		}
	}

	return vulns, nil
}

// selectPatterns filters security patterns based on scan mode and category selection.
func selectPatterns(in input) []securityPattern {
	if in.ScanMode == "scan" && len(in.Categories) == 0 {
		return securityPatterns
	}

	// Filter by scan mode
	var patterns []securityPattern
	for _, p := range securityPatterns {
		include := false

		switch in.ScanMode {
		case "secrets":
			include = p.Category == "sensitive_data"
		case "injection":
			include = p.Category == "injection" || p.Category == "code_injection"
		case "crypto":
			include = p.Category == "insecure_crypto"
		default: // "scan"
			include = true
		}

		// Filter by categories if specified
		if len(in.Categories) > 0 {
			include = false
			for _, cat := range in.Categories {
				if p.Category == cat {
					include = true
					break
				}
			}
		}

		if include {
			patterns = append(patterns, p)
		}
	}

	return patterns
}

// filterBySeverity removes vulnerabilities below the specified severity threshold.
func filterBySeverity(vulns []vulnerability, threshold string) []vulnerability {
	minScore := severityScore(threshold)
	filtered := make([]vulnerability, 0)

	for _, v := range vulns {
		if severityScore(v.Severity) >= minScore {
			filtered = append(filtered, v)
		}
	}

	return filtered
}

// severityScore converts severity string to numeric score for comparison.
func severityScore(severity string) int {
	scores := map[string]int{
		"critical": 4,
		"high":     3,
		"medium":   2,
		"low":      1,
	}
	return scores[severity]
}

// calculateStats computes vulnerability statistics including severity/category breakdowns and risk score.
func calculateStats(vulns []vulnerability) map[string]any {
	stats := map[string]any{
		"total_issues": len(vulns),
		"by_severity":  make(map[string]int),
		"by_category":  make(map[string]int),
	}

	sevCounts := make(map[string]int)
	catCounts := make(map[string]int)

	for _, v := range vulns {
		sevCounts[v.Severity]++
		catCounts[v.Category]++
	}

	stats["by_severity"] = sevCounts
	stats["by_category"] = catCounts

	// Calculate risk score (0-100)
	riskScore := float64(sevCounts["critical"])*10 + float64(sevCounts["high"])*5 +
		float64(sevCounts["medium"])*2 + float64(sevCounts["low"])*0.5
	if riskScore > 100 {
		riskScore = 100
	}
	stats["risk_score"] = riskScore

	return stats
}
