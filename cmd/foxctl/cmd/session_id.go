package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/cmd/foxctl/cmd/sessionscmd"
	"github.com/spf13/cobra"
)

// Supported session providers
const (
	ProviderClaude   = "claude"
	ProviderCursor   = "cursor"
	ProviderOpenCode = "opencode"
	ProviderAuto     = "auto"
)

func newSessionIDCommand() *cobra.Command {
	var provider string
	var workspace string
	var agentID string

	cmd := &cobra.Command{
		Use:   "session-id",
		Short: "Detect the current session ID from a TUI provider",
		Long: `Detect the current session ID from various AI coding assistants.

Supported providers:
  claude   - Claude Code (scans ~/.claude/projects/<workspace>/ for latest session)
  cursor   - Cursor (reads CURSOR_SESSION_ID env var)
  opencode - OpenCode (reads OPENCODE_SESSION_ID env var)
  auto     - Try all providers in order (default)

Agent ID is used to distinguish multiple agents in the same workspace.
If not specified, defaults to FOXCTL_AGENT_ID env var, or the detected provider name.

Examples:
  foxctl session-id                           # Auto-detect from any provider
  foxctl session-id --provider claude         # Detect Claude Code session
  foxctl session-id --agent-id subagent:coder # For multi-agent coordination
  foxctl session-id --workspace /path         # Use specific workspace for Claude`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Resolve workspace
			ws := workspace
			if ws == "" {
				wd, err := os.Getwd()
				if err != nil {
					return writeSessionIDError(cmd, "failed to get working directory", err.Error())
				}
				ws = wd
			}

			var sessionID string
			var detectedProvider string
			var err error

			switch provider {
			case ProviderAuto:
				sessionID, detectedProvider, err = detectSessionAuto(ws)
			case ProviderClaude:
				sessionID, err = detectClaudeSession(ws)
				detectedProvider = ProviderClaude
			case ProviderCursor:
				sessionID, err = detectCursorSession()
				detectedProvider = ProviderCursor
			case ProviderOpenCode:
				sessionID, err = detectOpenCodeSession()
				detectedProvider = ProviderOpenCode
			default:
				return writeSessionIDError(cmd, "unknown provider", fmt.Sprintf("provider %q not supported", provider))
			}

			if err != nil {
				return writeSessionIDError(cmd, "session detection failed", err.Error())
			}

			if sessionID == "" {
				return writeSessionIDNotFound(cmd, provider, ws, agentID)
			}

			// Resolve agent ID: flag > env > provider
			resolvedAgentID := resolveAgentID(agentID, detectedProvider)

			return writeSessionIDSuccess(cmd, sessionID, detectedProvider, ws, resolvedAgentID)
		},
	}

	cmd.Flags().StringVarP(&provider, "provider", "p", ProviderAuto, "Provider to detect session from (claude, cursor, opencode, auto)")
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace path (default: current directory)")
	cmd.Flags().StringVarP(&agentID, "agent-id", "a", "", "Agent ID for multi-agent coordination (default: provider name)")

	return cmd
}

// resolveAgentID determines the agent ID from flag, env, or provider
func resolveAgentID(flagValue, provider string) string {
	// 1. Explicit flag
	if flagValue != "" {
		return flagValue
	}
	// 2. Environment variable
	if envID := os.Getenv("FOXCTL_AGENT_ID"); envID != "" {
		return envID
	}
	// 3. Default to provider name
	if provider != "" {
		return provider
	}
	return "foxctl"
}

// detectSessionAuto tries all providers in priority order
func detectSessionAuto(workspace string) (sessionID, provider string, err error) {
	// 1. Check env vars first (fastest)
	if sid := os.Getenv("FOXCTL_SESSION_ID"); sid != "" {
		return sid, "foxctl", nil
	}
	if sid := os.Getenv("CLAUDE_SESSION_ID"); sid != "" {
		return sid, ProviderClaude, nil
	}
	if sid := os.Getenv("CURSOR_SESSION_ID"); sid != "" {
		return sid, ProviderCursor, nil
	}
	if sid := os.Getenv("OPENCODE_SESSION_ID"); sid != "" {
		return sid, ProviderOpenCode, nil
	}

	// 2. Try Claude file-based detection (most common case)
	sid, err := detectClaudeSession(workspace)
	if err == nil && sid != "" {
		return sid, ProviderClaude, nil
	}

	// No session found
	return "", "", nil
}

// detectClaudeSession finds the most recently modified session file for a workspace
func detectClaudeSession(workspace string) (string, error) {
	projectsDir := sessionscmd.ClaudeProjectsDir()
	if projectsDir == "" {
		return "", fmt.Errorf("could not determine Claude projects directory")
	}

	// Encode workspace path to Claude's directory format
	// /Users/alice/repos/personal/foxctl -> -Users-alice-repos-personal-foxctl
	encodedWS := encodeWorkspacePath(workspace)
	projectDir := filepath.Join(projectsDir, encodedWS)

	// Check if directory exists
	if _, err := os.Stat(projectDir); os.IsNotExist(err) {
		return "", fmt.Errorf("no Claude project found for workspace %s", workspace)
	}

	// Find most recently modified .jsonl file
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return "", fmt.Errorf("read project directory: %w", err)
	}

	var latestFile string
	var latestTime int64

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		// Extract session ID from filename
		sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")

		// Only consider UUID-format session IDs (skip agent-* files)
		if !sessionscmd.IsUUIDFormat(sessionID) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		modTime := info.ModTime().UnixNano()
		if modTime > latestTime {
			latestTime = modTime
			latestFile = sessionID
		}
	}

	return latestFile, nil
}

// detectCursorSession reads the CURSOR_SESSION_ID environment variable
func detectCursorSession() (string, error) {
	sid := os.Getenv("CURSOR_SESSION_ID")
	return sid, nil
}

// detectOpenCodeSession reads the OPENCODE_SESSION_ID environment variable
func detectOpenCodeSession() (string, error) {
	sid := os.Getenv("OPENCODE_SESSION_ID")
	return sid, nil
}

// encodeWorkspacePath converts a filesystem path to Claude's encoded directory name
// e.g., "/Users/alice/repos/personal/foxctl" -> "-Users-alice-repos-personal-foxctl"
func encodeWorkspacePath(path string) string {
	// Remove trailing slash if present
	path = strings.TrimSuffix(path, "/")
	// Replace all slashes with dashes
	return strings.ReplaceAll(path, "/", "-")
}

// writeSessionIDSuccess outputs a successful session ID detection result
func writeSessionIDSuccess(cmd *cobra.Command, sessionID, provider, workspace, agentID string) error {
	return sessionscmd.WriteOK(cmd.OutOrStdout(), "foxctl.session-id", map[string]any{
		"session_id": sessionID,
		"provider":   provider,
		"workspace":  workspace,
		"agent_id":   agentID,
	})
}

// writeSessionIDNotFound outputs a not-found result
func writeSessionIDNotFound(cmd *cobra.Command, provider, workspace, agentID string) error {
	resolvedAgentID := resolveAgentID(agentID, provider)
	return sessionscmd.WriteOK(cmd.OutOrStdout(), "foxctl.session-id", map[string]any{
		"session_id": nil,
		"provider":   provider,
		"workspace":  workspace,
		"agent_id":   resolvedAgentID,
		"message":    "no active session found",
		"hint":       "Ensure you're running from within a supported AI coding assistant",
	})
}

// writeSessionIDError outputs an error result
func writeSessionIDError(cmd *cobra.Command, message, detail string) error {
	return sessionscmd.WriteArgError(cmd.OutOrStdout(), "foxctl.session-id", message, detail)
}

func init() {
	rootCmd.AddCommand(newSessionIDCommand())
}
