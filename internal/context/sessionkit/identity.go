package sessionkit

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshka0/foxctl/internal/platform/workspace"
)

// IdentityFile represents the session identity JSON structure.
type IdentityFile struct {
	SessionID string `json:"session_id"`
	AgentID   string `json:"agent_id,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

// ResolveSessionID returns the session ID from environment variables or identity file.
// Priority: explicit > FOXCTL_SESSION_ID > CLAUDE_SESSION_ID > OPENCODE_SESSION_ID >
// CURSOR_SESSION_ID > identity file > TERM_SESSION_ID.
// Returns empty string if none found.
func ResolveSessionID(ws, explicit string) string {
	// Explicit value takes priority
	if explicit != "" {
		return explicit
	}

	// Environment variables in priority order
	envVars := []string{
		"FOXCTL_SESSION_ID",
		"CLAUDE_SESSION_ID",
		"OPENCODE_SESSION_ID",
		"CURSOR_SESSION_ID",
	}
	for _, env := range envVars {
		if sid := os.Getenv(env); sid != "" {
			return sid
		}
	}

	// Try identity file
	if sid := ResolveSessionIDFromIdentityFile(ws); sid != "" {
		return sid
	}

	// TERM_SESSION_ID is last resort (generic terminal session, not foxctl)
	return os.Getenv("TERM_SESSION_ID")
}

// ResolveSessionIDFromIdentityFile reads the session ID from the identity file.
// Identity files are stored at ~/.foxctl/sessions/active/<workspace_hash>-<agent_id>.json
func ResolveSessionIDFromIdentityFile(ws string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	// Resolve workspace using platform detection (handles sandbox scenarios)
	// Uses FOXCTL_WORKSPACE -> CLAUDE_PROJECT_DIR -> git root -> cwd
	if ws == "" {
		ws = workspace.Detect("")
	}
	if ws == "" {
		return ""
	}

	// Compute workspace hash (matches session-identity.sh: shasum -a 256 | cut -c1-16)
	hash := sha256.Sum256([]byte(ws))
	workspaceHash := fmt.Sprintf("%x", hash)[:16]

	// Try agent-specific identity files, then base
	identityDir := filepath.Join(homeDir, ".foxctl", "sessions", "active")
	candidates := []string{
		filepath.Join(identityDir, workspaceHash+"-claude.json"),
		filepath.Join(identityDir, workspaceHash+"-foxctl.json"),
		filepath.Join(identityDir, workspaceHash+".json"),
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var identity IdentityFile
		if err := json.Unmarshal(data, &identity); err != nil {
			continue
		}
		if identity.SessionID != "" {
			return identity.SessionID
		}
	}

	return ""
}

// WorkspaceOrDefault returns the provided workspace or falls back to platform detection.
// Uses FOXCTL_WORKSPACE -> CLAUDE_PROJECT_DIR -> git root -> cwd -> defaultWorkspace
func WorkspaceOrDefault(ws, defaultWorkspace string) string {
	if ws != "" {
		return ws
	}
	// Use platform detection which handles sandbox scenarios properly
	if detected := workspace.Detect(""); detected != "" {
		return detected
	}
	return defaultWorkspace
}

// ComputeWorkspaceHash computes the 16-char workspace hash.
// Used for identity file paths and Claude project directories.
func ComputeWorkspaceHash(workspace string) string {
	hash := sha256.Sum256([]byte(workspace))
	return fmt.Sprintf("%x", hash)[:16]
}
