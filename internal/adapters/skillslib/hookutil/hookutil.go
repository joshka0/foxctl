package hookutil

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
)

// WorkspaceID derives a stable workspace ID from a path string.
func WorkspaceID(path string) string {
	h := sha256.Sum256([]byte(path))
	return "ws-" + hex.EncodeToString(h[:8])
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
	return strings.TrimSpace(workspaceRoot)
}

// ResolveWorkspaceIDHash returns a workspace ID, hashing the workspace root as a fallback.
func ResolveWorkspaceIDHash(in hooks.Input, workspaceRoot string) string {
	if id := strings.TrimSpace(in.WorkspaceID); id != "" {
		return id
	}
	if root := strings.TrimSpace(workspaceRoot); root != "" {
		return WorkspaceID(root)
	}
	return ""
}

// ResolveActorID returns the best-effort actor ID for a hook input.
func ResolveActorID(in hooks.Input) string {
	if id := strings.TrimSpace(in.ActorID); id != "" {
		return id
	}
	if id := strings.TrimSpace(os.Getenv("AGENTCTL_AGENT_ID")); id != "" {
		return id
	}
	if id := strings.TrimSpace(os.Getenv("AGENTCTL_AGENT_NAME")); id != "" {
		return id
	}
	return fmt.Sprintf("actor:agent:%s", in.SessionID)
}
