package worktree

import (
	"fmt"
	"regexp"
	"strings"
)

// unsafePattern matches characters that are not allowed in git branch names.
// Safe characters: alphanumeric, hyphen, underscore, dot, forward slash.
var unsafePattern = regexp.MustCompile(`[^a-zA-Z0-9._/\-]`)

// collapseHyphens matches consecutive hyphens.
var collapseHyphens = regexp.MustCompile(`-{2,}`)

// collapseSlashes matches consecutive forward slashes.
var collapseSlashes = regexp.MustCompile(`/{2,}`)

// collapseDots matches consecutive dots, which are not valid in git ref names.
var collapseDots = regexp.MustCompile(`\.{2,}`)

// SanitizeBranchName cleans a proposed branch name by replacing unsafe characters
// with hyphens and collapsing consecutive unsafe characters. Returns an error if
// the result would be empty or invalid.
//
// This is a pure function — no IO, no side effects.
func SanitizeBranchName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("branch name is empty")
	}

	// Replace unsafe characters with hyphens
	result := unsafePattern.ReplaceAllString(name, "-")

	// Collapse consecutive hyphens into a single hyphen
	result = collapseHyphens.ReplaceAllString(result, "-")

	// Collapse consecutive slashes
	result = collapseSlashes.ReplaceAllString(result, "/")

	// Collapse consecutive dots so the result cannot contain git's parent-ref marker.
	result = collapseDots.ReplaceAllString(result, ".")

	// Strip leading/trailing hyphens, dots, and slashes from the whole name
	result = strings.Trim(result, "-./ ")

	// Handle component-level cleanup: split by "/", clean each part, rejoin
	parts := strings.Split(result, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = cleanBranchComponent(part)
		if part == "" {
			continue
		}
		cleaned = append(cleaned, part)
	}
	result = strings.Join(cleaned, "/")

	if result == "" {
		return "", fmt.Errorf("branch name is invalid after sanitization")
	}

	// Reject names ending with .lock
	if strings.HasSuffix(result, ".lock") {
		result = strings.TrimSuffix(result, ".lock")
		result = strings.TrimRight(result, "-. ")
	}

	// Reject names with @{ (reflog notation)
	if strings.Contains(result, "@{") {
		result = strings.ReplaceAll(result, "@{", "-")
		result = strings.ReplaceAll(result, "}", "")
	}

	if result == "" {
		return "", fmt.Errorf("branch name is invalid after sanitization")
	}

	return result, nil
}

func cleanBranchComponent(part string) string {
	// Strip leading/trailing dots and hyphens from each component.
	part = strings.Trim(part, "-. ")
	for strings.HasSuffix(part, ".lock") {
		part = strings.TrimSuffix(part, ".lock")
		part = strings.TrimRight(part, "-. ")
	}
	return part
}
