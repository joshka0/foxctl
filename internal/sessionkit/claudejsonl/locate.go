package claudejsonl

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// LocateSessionJSONL tries to find a Claude Code JSONL file for a session.
// It searches in the following locations (in order):
// 1. Workspace-specific: <workspace>/.claude/sessions/<session_id>.jsonl
// 2. Global with workspace hash: ~/.claude/projects/<workspace_hash>/<session_id>.jsonl
// 3. Global glob: ~/.claude/projects/*/<session_id>.jsonl
//
// Returns empty string if no file is found.
func LocateSessionJSONL(workspacePath, sessionID string) string {
	homeDir, _ := os.UserHomeDir()

	// Try various possible locations
	var patterns []string

	// Try workspace-specific path first if provided
	if workspacePath != "" {
		patterns = append(patterns,
			filepath.Join(workspacePath, ".claude", "sessions", sessionID+".jsonl"),
		)

		// Try workspace hash location
		hash := sha256.Sum256([]byte(workspacePath))
		workspaceHash := fmt.Sprintf("%x", hash)[:16]
		patterns = append(patterns,
			filepath.Join(homeDir, ".claude", "projects", workspaceHash, sessionID+".jsonl"),
		)
	}

	// Then try global Claude locations with glob
	patterns = append(patterns,
		filepath.Join(homeDir, ".claude", "projects", "*", "sessions", sessionID+".jsonl"),
		filepath.Join(homeDir, ".claude", "projects", "*", sessionID+".jsonl"),
	)

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err == nil && len(matches) > 0 {
			return matches[0]
		}
	}

	return ""
}

// LocateAllSessionJSONLs finds all JSONL files for a workspace.
// Returns a slice of (sessionID, path) pairs.
func LocateAllSessionJSONLs(workspacePath string) []struct {
	SessionID string
	Path      string
} {
	homeDir, _ := os.UserHomeDir()
	var results []struct {
		SessionID string
		Path      string
	}

	var patterns []string

	// Try workspace-specific location
	if workspacePath != "" {
		patterns = append(patterns,
			filepath.Join(workspacePath, ".claude", "sessions", "*.jsonl"),
		)

		// Try workspace hash location
		hash := sha256.Sum256([]byte(workspacePath))
		workspaceHash := fmt.Sprintf("%x", hash)[:16]
		patterns = append(patterns,
			filepath.Join(homeDir, ".claude", "projects", workspaceHash, "*.jsonl"),
		)
	}

	seen := make(map[string]bool)
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, path := range matches {
			base := filepath.Base(path)
			sessionID := base[:len(base)-6] // Remove .jsonl
			if seen[sessionID] {
				continue
			}
			seen[sessionID] = true
			results = append(results, struct {
				SessionID string
				Path      string
			}{
				SessionID: sessionID,
				Path:      path,
			})
		}
	}

	return results
}

// ClaudeProjectDir returns the Claude project directory for a workspace.
// This is ~/.claude/projects/<workspace_hash>/ where the workspace hash
// is the first 16 characters of the SHA256 hash of the workspace path.
func ClaudeProjectDir(workspacePath string) string {
	homeDir, _ := os.UserHomeDir()
	hash := sha256.Sum256([]byte(workspacePath))
	workspaceHash := fmt.Sprintf("%x", hash)[:16]
	return filepath.Join(homeDir, ".claude", "projects", workspaceHash)
}
