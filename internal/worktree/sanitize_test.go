package worktree

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeBranchName_UnsafeChars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "spaces and tilde replaced",
			input:    "feat/my cool feature~",
			expected: "feat/my-cool-feature",
		},
		{
			name:     "consecutive unsafe chars collapse",
			input:    "feat/foo~~bar",
			expected: "feat/foo-bar",
		},
		{
			name:     "multiple consecutive unsafe chars",
			input:    "fix/...bug",
			expected: "fix/bug",
		},
		{
			name:     "special shell chars",
			input:    "feat/risky;rm-rf",
			expected: "feat/risky-rm-rf",
		},
		{
			name:     "backslash replaced",
			input:    `feat\backslash`,
			expected: "feat-backslash",
		},
		{
			name:     "colon replaced",
			input:    "feat/jira:123",
			expected: "feat/jira-123",
		},
		{
			name:     "carriage return and newline",
			input:    "feat/line\r\nbreak",
			expected: "feat/line-break",
		},
		{
			name:     "tab replaced",
			input:    "feat/tab\tvalue",
			expected: "feat/tab-value",
		},
		{
			name:     "double quotes",
			input:    `feat/"quoted"`,
			expected: "feat/quoted",
		},
		{
			name:     "single quotes",
			input:    "feat/'quoted'",
			expected: "feat/quoted",
		},
		{
			name:     "pipe character",
			input:    "feat/pipe|value",
			expected: "feat/pipe-value",
		},
		{
			name:     "dollar sign",
			input:    "feat/$var",
			expected: "feat/var",
		},
		{
			name:     "ampersand",
			input:    "feat/a&b",
			expected: "feat/a-b",
		},
		{
			name:     "exclamation mark",
			input:    "feat/wow!",
			expected: "feat/wow",
		},
		{
			name:     "asterisk",
			input:    "feat/glob*",
			expected: "feat/glob",
		},
		{
			name:     "question mark",
			input:    "feat/what?",
			expected: "feat/what",
		},
		{
			name:     "angle brackets",
			input:    "feat/<redirect>",
			expected: "feat/redirect",
		},
		{
			name:     "parentheses",
			input:    "feat/(parens)",
			expected: "feat/parens",
		},
		{
			name:     "brackets",
			input:    "feat/[brackets]",
			expected: "feat/brackets",
		},
		{
			name:     "curly braces",
			input:    "feat/{braces}",
			expected: "feat/braces",
		},
		{
			name:     "leading hyphens stripped",
			input:    "-feat/x",
			expected: "feat/x",
		},
		{
			name:     "leading dot stripped",
			input:    ".feat/x",
			expected: "feat/x",
		},
		{
			name:     "trailing dot and hyphen stripped",
			input:    "feat/x.",
			expected: "feat/x",
		},
		{
			name:     "trailing slash stripped",
			input:    "feat/x/",
			expected: "feat/x",
		},
		{
			name:     "lock suffix rejected",
			input:    "feat/branch.lock",
			expected: "feat/branch",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := SanitizeBranchName(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestSanitizeBranchName_EmptyResult(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "only dots", input: "..."},
		{name: "only unsafe chars", input: "~!@#$%"},
		{name: "empty string", input: ""},
		{name: "only spaces", input: "   "},
		{name: "only hyphens", input: "---"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := SanitizeBranchName(tc.input)
			assert.Error(t, err)
			assert.Empty(t, result)
		})
	}
}

func TestSanitizeBranchName_SafePreserved(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "typical feature branch",
			input:    "feat/jira-123_fix",
			expected: "feat/jira-123_fix",
		},
		{
			name:     "simple name",
			input:    "main",
			expected: "main",
		},
		{
			name:     "alphanumeric with hyphens",
			input:    "my-feature-branch",
			expected: "my-feature-branch",
		},
		{
			name:     "alphanumeric with underscores",
			input:    "my_feature_branch",
			expected: "my_feature_branch",
		},
		{
			name:     "slash separator",
			input:    "fix/bug-123",
			expected: "fix/bug-123",
		},
		{
			name:     "numbers only",
			input:    "12345",
			expected: "12345",
		},
		{
			name:     "mixed case alphanumeric",
			input:    "feat/ABC-123",
			expected: "feat/ABC-123",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := SanitizeBranchName(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}
