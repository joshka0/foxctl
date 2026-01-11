package pathutil

import (
	"errors"
	"fmt"
)

// ErrOutsideWorkspace is returned when a path is outside the workspace root.
var ErrOutsideWorkspace = errors.New("path is outside workspace")

// PathError represents a path-related error with context.
type PathError struct {
	Op        string // Operation that failed (e.g., "resolve", "validate")
	Path      string // The path that caused the error
	Workspace string // The workspace root (if applicable)
	Err       error  // The underlying error
}

func (e *PathError) Error() string {
	if e.Workspace != "" {
		return fmt.Sprintf("%s %q: %v (workspace: %s)", e.Op, e.Path, e.Err, e.Workspace)
	}
	return fmt.Sprintf("%s %q: %v", e.Op, e.Path, e.Err)
}

func (e *PathError) Unwrap() error {
	return e.Err
}
