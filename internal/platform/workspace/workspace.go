// Package workspace provides helpers for detecting and normalizing workspaces.
package workspace

import (
	"os"
	"path/filepath"
)

// Detect returns the workspace path using the standard fallback chain:
// 1. AGENTCTL_WORKSPACE env var (highest priority - set by agentctl for sandboxes)
// 2. CLAUDE_PROJECT_DIR env var (set by Claude Code)
// 3. Walk up from start looking for .git or .agentctl markers
// 4. Return start directory if no markers found
//
// Pass empty string for start to use current working directory.
func Detect(start string) string {
	// 1. AGENTCTL_WORKSPACE has highest priority (handles sandbox scenarios)
	if ws := os.Getenv("AGENTCTL_WORKSPACE"); ws != "" {
		return ws
	}

	// 2. CLAUDE_PROJECT_DIR (set by Claude Code)
	if projDir := os.Getenv("CLAUDE_PROJECT_DIR"); projDir != "" {
		return projDir
	}

	// 3. Walk up looking for markers
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
