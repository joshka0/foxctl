package skillmain

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
)

type pathOptions struct {
	message string
	hint    string
}

// PathOption customizes path validation errors.
type PathOption func(*pathOptions)

// WithPathMessage sets a custom error message prefix for path validation failures.
func WithPathMessage(message string) PathOption {
	return func(opts *pathOptions) {
		opts.message = strings.TrimSpace(message)
	}
}

// WithPathHint overrides the default path validation hint.
func WithPathHint(hint string) PathOption {
	return func(opts *pathOptions) {
		opts.hint = strings.TrimSpace(hint)
	}
}

// ValidatePath validates a candidate path against the run context policy.
func ValidatePath(rc *RunContext, candidate string, options ...PathOption) (string, error) {
	if rc == nil || rc.PathValidator == nil {
		return "", skillerr.Arg("path validator not configured")
	}

	opts := &pathOptions{
		hint: "Provide paths within the workspace or an allowed root.",
	}
	for _, opt := range options {
		opt(opts)
	}

	resolved, err := rc.PathValidator.ValidatePath(candidate)
	if err != nil {
		message := opts.message
		if message == "" {
			message = "path validation failed"
		}
		return "", skillerr.Arg(
			fmt.Sprintf("%s for %q: %v", message, candidate, err),
			skillerr.WithHint(opts.hint),
			skillerr.WithCause(err),
		)
	}

	return resolved, nil
}

// ResolvePaths validates a single path, multiple paths, and/or glob patterns.
// Glob matches are validated individually and invalid matches are skipped.
func ResolvePaths(rc *RunContext, singlePath string, multiplePaths []string) ([]string, error) {
	if rc == nil || rc.PathValidator == nil {
		return nil, skillerr.Arg("path validator not configured")
	}

	workspace := rc.PathValidator.Workspace()

	var patterns []string
	if singlePath != "" {
		patterns = append(patterns, singlePath)
	}
	patterns = append(patterns, multiplePaths...)

	var resolved []string
	for _, pattern := range patterns {
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(workspace, pattern)
		}

		if strings.ContainsAny(pattern, "*?[") {
			matches, err := filepath.Glob(pattern)
			if err != nil {
				return nil, skillerr.WrapValidation(fmt.Sprintf("invalid glob pattern %q", pattern), err)
			}
			for _, match := range matches {
				absPath, err := ValidatePath(rc, match)
				if err == nil {
					resolved = append(resolved, absPath)
				}
			}
			continue
		}

		absPath, err := ValidatePath(rc, pattern)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, absPath)
	}

	return resolved, nil
}
