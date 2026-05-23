package workspaceutil

import (
	"fmt"
	"path/filepath"
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

// ResolvePath resolves a skill workspace override against the run-context workspace.
func ResolvePath(base, override string) (string, error) {
	return ResolvePathWithFallback(base, override, "")
}

// ResolvePathWithFallback resolves a skill workspace override and uses fallback
// when both the override and run-context workspace are empty.
func ResolvePathWithFallback(base, override, fallback string) (string, error) {
	workspace := strings.TrimSpace(override)
	if workspace == "" {
		workspace = base
	}
	if workspace == "" {
		workspace = fallback
	}
	if workspace == "" {
		return "", fmt.Errorf("workspace is required")
	}
	if !filepath.IsAbs(workspace) && base != "" {
		workspace = filepath.Join(base, workspace)
	}
	return filepath.Abs(workspace)
}
