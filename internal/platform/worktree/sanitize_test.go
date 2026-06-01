package worktree

import (
	"strings"
	"testing"
	"testing/quick"

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
			name:     "consecutive dots inside component",
			input:    "fix/foo..bar",
			expected: "fix/foo.bar",
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
		{
			name:     "lock suffix rejected inside component",
			input:    "feat/branch.lock/next",
			expected: "feat/branch/next",
		},
		{
			name:     "repeated lock suffixes rejected inside component",
			input:    "feat/branch.lock.lock/next",
			expected: "feat/branch/next",
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
		{
			name:     "single dots inside component",
			input:    "release/1.2.3",
			expected: "release/1.2.3",
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

func TestSanitizeBranchNamePropertyLockSuffixComponentsAreRemoved(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 200}
	err := quick.Check(func(rawPrefix, rawComponent, rawSuffix string) bool {
		prefix := safeBranchComponent(rawPrefix, "feat")
		component := safeBranchComponent(rawComponent, "branch")
		suffix := safeBranchComponent(rawSuffix, "next")
		result, err := SanitizeBranchName(prefix + "/" + component + ".lock.lock/" + suffix)
		if err != nil {
			return false
		}
		return branchComponentsAreClean(result) && !strings.Contains(result, ".lock")
	}, cfg)
	if err != nil {
		t.Fatalf("SanitizeBranchName .lock component property failed: %v", err)
	}
}

func TestSanitizeBranchNamePropertySuccessfulResultsAreGitRefSafe(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 500}
	err := quick.Check(func(input string) bool {
		result, err := SanitizeBranchName(input)
		if err != nil {
			return true
		}
		return result != "" &&
			!unsafePattern.MatchString(result) &&
			!strings.Contains(result, "..") &&
			!strings.Contains(result, "//") &&
			!strings.Contains(result, "@{") &&
			!strings.HasPrefix(result, "/") &&
			!strings.HasSuffix(result, "/") &&
			!strings.HasSuffix(result, ".") &&
			!strings.HasSuffix(result, ".lock") &&
			branchComponentsAreClean(result)
	}, cfg)
	if err != nil {
		t.Fatalf("SanitizeBranchName git ref safety property failed: %v", err)
	}
}

func TestSanitizeBranchNamePropertyIdempotent(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 500}
	err := quick.Check(func(input string) bool {
		once, err := SanitizeBranchName(input)
		if err != nil {
			return true
		}
		twice, err := SanitizeBranchName(once)
		return err == nil && twice == once
	}, cfg)
	if err != nil {
		t.Fatalf("SanitizeBranchName idempotence property failed: %v", err)
	}
}

func safeBranchComponent(raw, fallback string) string {
	cleaned := unsafePattern.ReplaceAllString(raw, "-")
	cleaned = strings.Trim(cleaned, "-./ ")
	cleaned = strings.ReplaceAll(cleaned, "/", "-")
	cleaned = collapseHyphens.ReplaceAllString(cleaned, "-")
	cleaned = collapseDots.ReplaceAllString(cleaned, ".")
	if cleaned == "" || cleaned == ".lock" {
		return fallback
	}
	return cleaned
}

func branchComponentsAreClean(branch string) bool {
	for _, part := range strings.Split(branch, "/") {
		if part == "" ||
			strings.HasPrefix(part, ".") ||
			strings.HasSuffix(part, ".") ||
			strings.HasPrefix(part, "-") ||
			strings.HasSuffix(part, "-") ||
			strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}
