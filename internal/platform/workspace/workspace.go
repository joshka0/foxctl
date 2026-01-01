// Package workspace provides helpers for detecting and normalizing workspaces.
package workspace

import (
	"os"
	"path/filepath"
)

// Detect walks up from start (or current working directory if empty) looking
// for directories that contain `.agentctl` or `.git`. It returns the first
// match, or the starting directory if none are found.
func Detect(start string) string {
	dir := start
	if dir == "" {
		if cwd, err := os.Getwd(); err == nil {
			dir = cwd
		}
	}
	if dir == "" {
		return ""
	}
	dir = filepath.Clean(dir)
	candidate := dir
	for {
		if hasMarker(candidate, ".agentctl") || hasMarker(candidate, ".git") {
			return candidate
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			break
		}
		candidate = parent
	}
	return dir
}

// Normalize cleans the workspace path for display/persistence.
func Normalize(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func hasMarker(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		return false
	}
	// .agentctl must be a directory, but .git can be either a directory
	// (normal repo) or a file (git worktree pointing to main repo)
	if name == ".git" {
		return true // exists as file or directory
	}
	return info.IsDir()
}
