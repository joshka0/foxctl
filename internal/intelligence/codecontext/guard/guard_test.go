package guard

import (
	"context"
	"testing"
)

func TestScanner_Scan_HighSeverity(t *testing.T) {
	tests := []struct {
		name    string
		content string
		pattern string
	}{
		{
			name:    "private key header",
			content: "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA...",
			pattern: "private_key",
		},
		{
			name:    "AWS access key",
			content: "aws_access_key_id = AKIAIOSFODNN7EXAMPLE",
			pattern: "aws_access_key",
		},
		{
			name:    "GitHub token ghp",
			content: "GITHUB_TOKEN=ghp_1234567890abcdefghijklmnopqrstuv1234",
			pattern: "github_token",
		},
		{
			name:    "OpenAI key",
			content: "OPENAI_API_KEY=sk-1234567890abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN",
			pattern: "openai_key",
		},
		{
			name:    "Anthropic key",
			content: "ANTHROPIC_API_KEY=sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz12345",
			pattern: "anthropic_key",
		},
		{
			name:    "Stripe live key",
			content: "stripe_key = sk_live_1234567890abcdefghijklmn",
			pattern: "stripe_key",
		},
	}

	scanner := NewDefault()
	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scanner.ScanString(ctx, "test.txt", tt.content)

			if !result.HasFindings() {
				t.Errorf("expected to find %s, got no findings", tt.pattern)
				return
			}

			found := false
			for _, f := range result.Findings {
				if f.Pattern == tt.pattern {
					found = true
					if f.Severity != SeverityHigh {
						t.Errorf("expected high severity for %s, got %s", tt.pattern, f.Severity)
					}
					break
				}
			}

			if !found {
				t.Errorf("expected pattern %s, got %v", tt.pattern, result.Findings)
			}
		})
	}
}

func TestScanner_Scan_MediumSeverity(t *testing.T) {
	tests := []struct {
		name    string
		content string
		pattern string
	}{
		{
			name:    "generic secret assignment",
			content: `password = "mySecretPassword123"`,
			pattern: "generic_secret",
		},
		{
			name:    "bearer token",
			content: "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			pattern: "bearer_token",
		},
		{
			name:    "JWT token",
			content: "token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
			pattern: "jwt_token",
		},
		{
			name:    "postgres connection string",
			content: "DATABASE_URL=postgres://user:password@localhost:5432/db",
			pattern: "connection_string",
		},
	}

	scanner := NewDefault()
	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scanner.ScanString(ctx, "test.txt", tt.content)

			if !result.HasFindings() {
				t.Errorf("expected to find %s, got no findings", tt.pattern)
				return
			}

			found := false
			for _, f := range result.Findings {
				if f.Pattern == tt.pattern {
					found = true
					if f.Severity != SeverityMedium {
						t.Errorf("expected medium severity for %s, got %s", tt.pattern, f.Severity)
					}
					break
				}
			}

			if !found {
				t.Errorf("expected pattern %s, got patterns: %v", tt.pattern, result.Findings)
			}
		})
	}
}

func TestScanner_Scan_NoSecrets(t *testing.T) {
	contents := []string{
		"func main() {\n\tfmt.Println(\"Hello, World!\")\n}",
		"const MAX_RETRIES = 3",
		"// This is a comment about passwords in general",
		"password := getPasswordFromEnv()",
	}

	scanner := NewDefault()
	ctx := context.Background()

	for i, content := range contents {
		result := scanner.ScanString(ctx, "test.go", content)
		if result.HasFindings() {
			t.Errorf("case %d: expected no findings, got %v", i, result.Findings)
		}
	}
}

func TestScanner_ModeBlock(t *testing.T) {
	scanner := New(Opts{Mode: ModeBlock})
	ctx := context.Background()

	// High severity should block
	result := scanner.ScanString(ctx, "secrets.txt", "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz12345")
	if !result.Blocked {
		t.Error("expected file to be blocked for high-severity secret")
	}
	if result.Reason == "" {
		t.Error("expected reason for blocking")
	}

	// Medium severity should not block
	result2 := scanner.ScanString(ctx, "config.txt", `password = "testPassword123"`)
	if result2.Blocked {
		t.Error("expected file NOT to be blocked for medium-severity secret")
	}
}

func TestScanner_ModeWarn(t *testing.T) {
	scanner := New(Opts{Mode: ModeWarn})
	ctx := context.Background()

	// Even high severity should not block in warn mode
	result := scanner.ScanString(ctx, "secrets.txt", "AKIAIOSFODNN7EXAMPLE")
	if result.Blocked {
		t.Error("expected file NOT to be blocked in warn mode")
	}
	if !result.HasHighSeverity() {
		t.Error("expected high-severity finding")
	}
}

func TestScanner_ExcludePatterns(t *testing.T) {
	scanner := New(Opts{
		Mode:            ModeWarn,
		ExcludePatterns: []string{"aws_access_key"},
	})
	ctx := context.Background()

	result := scanner.ScanString(ctx, "test.txt", "AKIAIOSFODNN7EXAMPLE")
	for _, f := range result.Findings {
		if f.Pattern == "aws_access_key" {
			t.Error("expected aws_access_key pattern to be excluded")
		}
	}
}

func TestScanner_IncludeLowSeverity(t *testing.T) {
	// Without low severity
	scanner1 := New(Opts{Mode: ModeWarn, IncludeLowSeverity: false})
	ctx := context.Background()

	hexContent := "hash = abcdef1234567890abcdef1234567890"
	result1 := scanner1.ScanString(ctx, "test.txt", hexContent)

	hasHex32 := false
	for _, f := range result1.Findings {
		if f.Pattern == "hex_32" {
			hasHex32 = true
		}
	}
	if hasHex32 {
		t.Error("expected hex_32 pattern to be excluded by default")
	}

	// With low severity
	scanner2 := New(Opts{Mode: ModeWarn, IncludeLowSeverity: true})
	result2 := scanner2.ScanString(ctx, "test.txt", hexContent)

	hasHex32 = false
	for _, f := range result2.Findings {
		if f.Pattern == "hex_32" {
			hasHex32 = true
		}
	}
	if !hasHex32 {
		t.Error("expected hex_32 pattern to be included with IncludeLowSeverity")
	}
}

func TestMaskSecret(t *testing.T) {
	tests := []struct {
		line     string
		start    int
		end      int
		expected string
	}{
		{
			line:     "key=abc123def456",
			start:    4,
			end:      16,
			expected: "key=abc1****f456",
		},
		{
			line:     "short=ab",
			start:    6,
			end:      8,
			expected: "short=****",
		},
	}

	for _, tt := range tests {
		got := maskSecret(tt.line, tt.start, tt.end)
		if got != tt.expected {
			t.Errorf("maskSecret(%q, %d, %d) = %q, want %q", tt.line, tt.start, tt.end, got, tt.expected)
		}
	}
}

func TestResult_Methods(t *testing.T) {
	result := &Result{
		Path: "test.txt",
		Findings: []Finding{
			{Pattern: "high1", Severity: SeverityHigh},
			{Pattern: "medium1", Severity: SeverityMedium},
			{Pattern: "high2", Severity: SeverityHigh},
		},
	}

	if !result.HasFindings() {
		t.Error("expected HasFindings() to return true")
	}

	if !result.HasHighSeverity() {
		t.Error("expected HasHighSeverity() to return true")
	}

	high := result.HighSeverityFindings()
	if len(high) != 2 {
		t.Errorf("expected 2 high-severity findings, got %d", len(high))
	}

	// Test empty result
	empty := &Result{Path: "empty.txt"}
	if empty.HasFindings() {
		t.Error("expected HasFindings() to return false for empty result")
	}
	if empty.HasHighSeverity() {
		t.Error("expected HasHighSeverity() to return false for empty result")
	}
}
