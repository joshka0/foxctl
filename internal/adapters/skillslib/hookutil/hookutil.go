package hookutil

import (
	"fmt"
	"os"
	"strings"

	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/hooks"
)

// WorkspaceID returns the preferred workspace ID for a path.
// It uses the repo identity when available, falling back to a path-derived ID.
func WorkspaceID(path string) string {
	return workspace.ID(path)
}

// ResolveWorkspaceRoot returns the best-effort workspace root for a hook input.
func ResolveWorkspaceRoot(in hooks.Input, start string) string {
	if root := strings.TrimSpace(in.WorkspaceRoot); root != "" {
		return root
	}
	return workspace.Detect(start)
}

// ResolveWorkspaceID returns a workspace ID, falling back to the workspace root.
func ResolveWorkspaceID(in hooks.Input, workspaceRoot string) string {
	if id := strings.TrimSpace(in.WorkspaceID); id != "" {
		return id
	}
	if root := strings.TrimSpace(workspaceRoot); root != "" {
		return workspace.ID(root)
	}
	return ""
}

// ResolveWorkspaceIDHash returns a workspace ID using the workspace root as fallback.
func ResolveWorkspaceIDHash(in hooks.Input, workspaceRoot string) string {
	if id := strings.TrimSpace(in.WorkspaceID); id != "" {
		return id
	}
	if root := strings.TrimSpace(workspaceRoot); root != "" {
		return workspace.ID(root)
	}
	return ""
}

// ResolveActorID returns the best-effort actor ID for a hook input.
func ResolveActorID(in hooks.Input) string {
	if id := strings.TrimSpace(in.ActorID); id != "" {
		return id
	}
	if id := strings.TrimSpace(os.Getenv("FOXCTL_AGENT_ID")); id != "" {
		return id
	}
	if id := strings.TrimSpace(os.Getenv("FOXCTL_AGENT_NAME")); id != "" {
		return id
	}
	return fmt.Sprintf("actor:agent:%s", in.SessionID)
}
