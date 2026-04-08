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
			name:     "spaces and tilde",
			input:    "feat/my cool feature~",
			expected: "feat/my-cool-feature",
		},
		{
			name:     "special characters",
			input:    "fix/bug#123!",
			expected: "fix/bug-123",
		},
		{
			name:     "consecutive unsafe chars",
			input:    "feat/hello,,,world",
			expected: "feat/hello-world",
		},
		{
			name:     "leading and trailing unsafe",
			input:    "~~~feat/test~~~",
			expected: "feat/test",
		},
		{
			name:     "mixed unsafe chars",
			input:    "release/v1.0 @prod",
			expected: "release/v1.0-prod",
		},
		{
			name:     "colon in name",
			input:    "feat:dashboard",
			expected: "feat-dashboard",
		},
		{
			name:     "parentheses",
			input:    "fix(urgent): crash",
			expected: "fix-urgent-crash",
		},
		{
			name:     "at symbol",
			input:    "user@feature",
			expected: "user-feature",
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
		{name: "dots only", input: "..."},
		{name: "all special chars", input: "~~~!!!@@@"},
		{name: "empty string", input: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := SanitizeBranchName(tc.input)
			assert.Error(t, err)
			assert.Empty(t, result)
			assert.Contains(t, err.Error(), "invalid after sanitization")
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
			name:     "safe characters unchanged",
			input:    "feat/jira-123_fix",
			expected: "feat/jira-123_fix",
		},
		{
			name:     "dots preserved",
			input:    "release/v1.2.3",
			expected: "release/v1.2.3",
		},
		{
			name:     "simple name",
			input:    "main",
			expected: "main",
		},
		{
			name:     "underscore and hyphen",
			input:    "my-feature_branch",
			expected: "my-feature_branch",
		},
		{
			name:     "just numbers",
			input:    "12345",
			expected: "12345",
		},
		{
			name:     "with dots and dashes",
			input:    "feature/v2.0-beta",
			expected: "feature/v2.0-beta",
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

func TestSanitizeBranchName_CollapsesHyphens(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "multiple spaces collapse to single hyphen",
			input:    "feat/many   spaces",
			expected: "feat/many-spaces",
		},
		{
			name:     "consecutive special chars",
			input:    "test$$$name",
			expected: "test-name",
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
