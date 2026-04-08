package worktree

import (
	"context"
	"fmt"
	"strings"
)

// WorktreeError represents a structured error from a worktree operation.
// It wraps the underlying cause with an actionable message.
type WorktreeError struct {
	Op      string // Operation that failed: "create", "list", "remove", "prune", "status"
	Repo    string // Repository path
	Path    string // Worktree path (if applicable)
	Branch  string // Branch name (if applicable)
	Message string // Human-readable error description
	Cause   error  // Underlying error (e.g., exec.ExitError)
}

func (e *WorktreeError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("worktree %s: %s: %v", e.Op, e.Message, e.Cause)
	}
	return fmt.Sprintf("worktree %s: %s", e.Op, e.Message)
}

func (e *WorktreeError) Unwrap() error {
	return e.Cause
}

// newError creates a new WorktreeError.
func newError(op, repo, message string) *WorktreeError {
	return &WorktreeError{Op: op, Repo: repo, Message: message}
}

// newErrorWithPath creates a new WorktreeError with a path.
func newErrorWithPath(op, repo, path, message string) *WorktreeError {
	return &WorktreeError{Op: op, Repo: repo, Path: path, Message: message}
}

// wrapError wraps an existing error in a WorktreeError.
func wrapError(op, repo, message string, cause error) *WorktreeError {
	if cause == nil {
		return newError(op, repo, message)
	}

	// Check for context cancellation
	if cause == context.Canceled {
		return &WorktreeError{
			Op:      op,
			Repo:    repo,
			Message: "operation cancelled",
			Cause:   cause,
		}
	}

	// Structured interpretation of git error output
	msg := interpretGitError(op, cause.Error())
	if msg != "" {
		return &WorktreeError{Op: op, Repo: repo, Message: msg, Cause: cause}
	}

	return &WorktreeError{Op: op, Repo: repo, Message: message, Cause: cause}
}

// interpretGitError converts raw git stderr into a human-readable message.
// Returns empty string if no known pattern matches.
func interpretGitError(op, raw string) string {
	lower := strings.ToLower(raw)

	switch {
	case strings.Contains(lower, "not a git repository"):
		return "not a git repository"
	case strings.Contains(lower, "already exists"):
		return "branch or worktree already exists"
	case strings.Contains(lower, "dirty"):
		return "worktree has uncommitted changes; use force to remove"
	case strings.Contains(lower, "no such file or directory"):
		return "path does not exist"
	case strings.Contains(lower, "cannot resolve"):
		return "cannot resolve ref"
	case strings.Contains(lower, "invalid reference"):
		return "invalid ref"
	}

	return ""
}
