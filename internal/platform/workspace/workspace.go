package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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

// WorkspaceInfo contains both the workspace path and repository identity.
type WorkspaceInfo struct {
	// Path is the filesystem path to the workspace root.
	Path string
	// RepoIdentity is a stable identifier for the git repository.
	// For worktrees, this returns the same identity since they share the same remote.
	// Empty if not in a git repo or no remote configured.
	RepoIdentity string
}

// DetectWithIdentity returns both the workspace path and repo identity.
// This is useful for scenarios where you need a stable identifier across worktrees.
func DetectWithIdentity(start string) WorkspaceInfo {
	path := Detect(start)
	return WorkspaceInfo{
		Path:         path,
		RepoIdentity: RepoIdentity(path),
	}
}

// RepoIdentity returns a stable identifier for the git repository at the given path.
// For worktrees, this returns the same identity since they share the same remote origin.
// Returns empty string if not in a git repo or no remote origin configured.
//
// The identity is a SHA256 hash of the normalized git remote origin URL,
// providing a consistent identifier regardless of:
// - SSH vs HTTPS URL format
// - .git suffix presence
// - Worktree vs main checkout
func RepoIdentity(workspace string) string {
	if workspace == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get git remote origin URL
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = workspace
	out, err := cmd.Output()
	if err != nil {
		return "" // Not a git repo or no remote
	}

	url := strings.TrimSpace(string(out))
	if url == "" {
		return ""
	}

	// Normalize the URL to handle SSH vs HTTPS and .git suffix variations
	normalized := normalizeGitURL(url)

	// Hash the normalized URL for a stable identifier
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:16]) // 32 hex chars
}

// normalizeGitURL converts git URLs to a canonical form for consistent hashing.
// Handles:
// - git@github.com:owner/repo.git -> github.com/owner/repo
// - https://github.com/owner/repo.git -> github.com/owner/repo
// - https://github.com/owner/repo -> github.com/owner/repo
func normalizeGitURL(url string) string {
	// Remove .git suffix
	url = strings.TrimSuffix(url, ".git")

	// Handle SSH format: git@github.com:owner/repo
	if strings.HasPrefix(url, "git@") {
		// git@github.com:owner/repo -> github.com/owner/repo
		url = strings.TrimPrefix(url, "git@")
		url = strings.Replace(url, ":", "/", 1)
		return url
	}

	// Handle HTTPS format: https://github.com/owner/repo
	if strings.HasPrefix(url, "https://") {
		return strings.TrimPrefix(url, "https://")
	}
	if strings.HasPrefix(url, "http://") {
		return strings.TrimPrefix(url, "http://")
	}

	// Return as-is for other formats
	return url
}
