package workspaceutil

import (
	"strings"

	"github.com/joshka0/foxctl/internal/context/sessionkit"
)

// Resolve returns the workspace after applying workspaceRoot overrides and defaults.
func Resolve(workspace, workspaceRoot, fallback string) string {
	ws := strings.TrimSpace(workspace)
	if ws == "" {
		root := strings.TrimSpace(workspaceRoot)
		if root != "" {
			ws = root
		}
	}
	return sessionkit.WorkspaceOrDefault(ws, fallback)
}

// ResolveID returns a workspace identifier, defaulting through sessionkit detection.
func ResolveID(workspaceID, fallback string) string {
	ws := strings.TrimSpace(workspaceID)
	return sessionkit.WorkspaceOrDefault(ws, fallback)
}
