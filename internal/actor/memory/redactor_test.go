package memory

import (
	"strings"
	"testing"
)

func TestSecretRedactor_Redact(t *testing.T) {
	r := NewSecretRedactor()

	tests := []struct {
		name     string
		input    string
		contains string // substring that should be redacted
		wantSafe bool   // should not contain the secret after redaction
	}{
		{
			name:     "GitHub PAT classic",
			input:    "token: ghp_1234567890abcdefghijklmnopqrstuvwxyz",
			contains: "ghp_",
			wantSafe: true,
		},
		{
			name:     "GitHub fine-grained PAT",
			input:    "auth: github_pat_1234567890abcdefghij12",
			contains: "github_pat_",
			wantSafe: true,
		},
		{
			name:     "OpenAI API key",
			input:    "key = sk-abcdefghijklmnopqrstuvwxyz1234567890ABCDEFGHIJKL",
			contains: "sk-abc",
			wantSafe: true,
		},
		{
			name:     "Anthropic API key",
			input:    "ANTHROPIC_KEY=sk-ant-api03-abcdefghijklmnopqrstu",
			contains: "sk-ant-",
			wantSafe: true,
		},
		{
			name:     "Google API key",
			input:    "google: AIzaSyC1234567890abcdefghijklmnopqrstuv",
			contains: "AIza",
			wantSafe: true,
		},
		{
			name:     "AWS access key",
			input:    "aws_key: AKIAIOSFODNN7EXAMPLE",
			contains: "AKIA",
			wantSafe: true,
		},
		{
			name:     "Authorization header",
			input:    "Authorization: Bearer my-secret-token",
			contains: "Bearer",
			wantSafe: true,
		},
		{
			name:     "Generic API key",
			input:    "api_key = super-secret-key-123",
			contains: "super-secret",
			wantSafe: true,
		},
		{
			name:     "Database connection string",
			input:    "conn: postgres://user:pass@localhost:5432/db",
			contains: "postgres://",
			wantSafe: true,
		},
		{
			name:     "Slack token",
			input:    "slack: xoxb-123456789012-1234567890123-AbCdEfGhIjKlMnOpQrStUvWx",
			contains: "xoxb-",
			wantSafe: true,
		},
		{
			name:     "Stripe live key",
			input:    "stripe: sk_live_1234567890abcdefghijklmn",
			contains: "sk_live_",
			wantSafe: true,
		},
		{
			name:     "JWT token",
			input:    "jwt: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			contains: "eyJ",
			wantSafe: true,
		},
		{
			name:     "Private key header",
			input:    "-----BEGIN RSA PRIVATE KEY-----\nMIIE...",
			contains: "PRIVATE KEY",
			wantSafe: true,
		},
		{
			name:     "Safe text unchanged",
			input:    "This is just regular text without secrets",
			contains: "regular text",
			wantSafe: false, // should still contain it (not redacted)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.Redact(tt.input)

			if tt.wantSafe {
				if strings.Contains(result, tt.contains) {
					t.Errorf("Redact() result still contains %q, want it redacted", tt.contains)
				}
				if !strings.Contains(result, "[REDACTED]") {
					t.Error("Redact() result should contain [REDACTED]")
				}
			} else {
				if !strings.Contains(result, tt.contains) {
					t.Errorf("Redact() result should still contain %q (safe text)", tt.contains)
				}
			}
		})
	}
}

func TestSecretRedactor_ContainsSecrets(t *testing.T) {
	r := NewSecretRedactor()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "contains secret",
			input:    "key: ghp_1234567890abcdefghijklmnopqrstuvwxyz",
			expected: true,
		},
		{
			name:     "no secrets",
			input:    "just some regular text",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.ContainsSecrets(tt.input)
			if got != tt.expected {
				t.Errorf("ContainsSecrets() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSecretRedactor_FindSecrets(t *testing.T) {
	r := NewSecretRedactor()

	t.Run("finds multiple secrets", func(t *testing.T) {
		// Use two different secret types that definitely match patterns
		input := "key1: ghp_1234567890abcdefghijklmnopqrstuvwxyz and key2: AKIAIOSFODNN7EXAMPLE"
		matches := r.FindSecrets(input)

		if len(matches) < 2 {
			t.Errorf("FindSecrets() found %d matches, want at least 2", len(matches))
		}
	})

	t.Run("no secrets", func(t *testing.T) {
		input := "just regular text"
		matches := r.FindSecrets(input)

		if len(matches) != 0 {
			t.Errorf("FindSecrets() found %d matches, want 0", len(matches))
		}
	})
}

func TestSecretRedactor_AddPattern(t *testing.T) {
	r := NewSecretRedactor()

	// Add custom pattern for CUSTOM_TOKEN_xxxxx
	err := r.AddPattern(`CUSTOM_TOKEN_[a-z]+`)
	if err != nil {
		t.Fatalf("AddPattern() error = %v", err)
	}

	input := "secret: CUSTOM_TOKEN_abcdef"
	if !r.ContainsSecrets(input) {
		t.Error("ContainsSecrets() should detect custom pattern")
	}

	result := r.Redact(input)
	if strings.Contains(result, "CUSTOM_TOKEN") {
		t.Error("Redact() should redact custom pattern")
	}
}

func TestSecretRedactor_AddPattern_Invalid(t *testing.T) {
	r := NewSecretRedactor()

	err := r.AddPattern(`[invalid`)
	if err == nil {
		t.Error("AddPattern() should return error for invalid regex")
	}
}

func TestSecretRedactor_RedactWithPlaceholder(t *testing.T) {
	r := NewSecretRedactor()

	input := "key: ghp_1234567890abcdefghijklmnopqrstuvwxyz"
	result := r.RedactWithPlaceholder(input, "***HIDDEN***")

	if strings.Contains(result, "ghp_") {
		t.Error("RedactWithPlaceholder() should redact secret")
	}
	if !strings.Contains(result, "***HIDDEN***") {
		t.Error("RedactWithPlaceholder() should use custom placeholder")
	}
}

func TestNewSecretRedactorWithPatterns(t *testing.T) {
	patterns := []string{
		`SECRET_[0-9]+`,
		`PRIVATE_[a-z]+`,
	}

	r := NewSecretRedactorWithPatterns(patterns)

	// Should match custom patterns
	if !r.ContainsSecrets("key: SECRET_12345") {
		t.Error("Should detect SECRET pattern")
	}
	if !r.ContainsSecrets("key: PRIVATE_abc") {
		t.Error("Should detect PRIVATE pattern")
	}

	// Should not match default patterns (since we replaced them)
	if r.ContainsSecrets("ghp_1234567890abcdefghijklmnopqrstuvwxyz") {
		t.Error("Should not detect GitHub PAT with custom patterns only")
	}
}

func TestNewSecretRedactorWithPatterns_InvalidPattern(t *testing.T) {
	patterns := []string{
		`valid_pattern`,
		`[invalid`,
		`another_valid`,
	}

	r := NewSecretRedactorWithPatterns(patterns)

	// Should skip invalid pattern and continue with valid ones
	if !r.ContainsSecrets("test: valid_pattern") {
		t.Error("Should detect valid pattern")
	}
	if !r.ContainsSecrets("test: another_valid") {
		t.Error("Should detect another valid pattern")
	}
}
