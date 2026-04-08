package worktree

import (
	"fmt"
	"regexp"
	"strings"
)

// sanitizeReplacer matches any character that is not a letter, digit,
// hyphen, underscore, dot, or forward slash (the characters allowed in
// git branch names by convention).
var sanitizeReplacer = regexp.MustCompile(`[^a-zA-Z0-9._\-/]+`)

// SanitizeBranchName replaces unsafe characters with hyphens and validates
// the result. Consecutive unsafe chars collapse to a single hyphen.
//
// Returns an error if the result is empty after sanitization.
// Safe characters (letters, digits, hyphen, underscore, dot, slash) are
// preserved unchanged.
func SanitizeBranchName(name string) (string, error) {
	sanitized := sanitizeReplacer.ReplaceAllString(name, "-")

	// Collapse consecutive hyphens (from multiple unsafe chars in a row)
	// but only those that weren't part of the original input.
	// Simple approach: collapse all runs of multiple hyphens.
	sanitized = collapseHyphens(sanitized)

	// Trim leading/trailing hyphens
	sanitized = strings.Trim(sanitized, "-")

	if sanitized == "" {
		return "", fmt.Errorf("branch name is invalid after sanitization: empty result")
	}

	return sanitized, nil
}

// collapseHyphens collapses runs of 2+ consecutive hyphens into a single hyphen.
func collapseHyphens(s string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range s {
		if r == '-' {
			if prevHyphen {
				continue
			}
			prevHyphen = true
		} else {
			prevHyphen = false
		}
		b.WriteRune(r)
	}
	return b.String()
}
