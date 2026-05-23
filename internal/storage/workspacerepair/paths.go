package workspacerepair

import (
	"os"
	"path/filepath"
	"strings"

	ws "github.com/joshka0/foxctl/internal/platform/workspace"
)

// ResolvedPathWorkspace is a legacy path workspace and its stable replacement.
type ResolvedPathWorkspace struct {
	RawPath       string
	EffectivePath string
	WorkspaceID   string
}

// ResolvePathWorkspace resolves a legacy path-like workspace value to a stable workspace ID.
func ResolvePathWorkspace(raw, userHome string) (ResolvedPathWorkspace, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || ws.LooksLikeID(raw) {
		return ResolvedPathWorkspace{}, false
	}

	effective := ""
	if PathExists(raw) {
		effective = raw
	}
	repaired := RepairHomePath(raw, userHome)
	if repaired != raw && PathExists(repaired) {
		effective = repaired
	}
	if effective == "" {
		return ResolvedPathWorkspace{}, false
	}

	workspaceID := ws.ID(effective)
	if workspaceID == "" || workspaceID == raw {
		return ResolvedPathWorkspace{}, false
	}
	return ResolvedPathWorkspace{
		RawPath:       raw,
		EffectivePath: effective,
		WorkspaceID:   workspaceID,
	}, true
}

// PathExists reports whether p exists on disk.
func PathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// RepairHomePath expands legacy home selectors and macOS home paths when safe.
func RepairHomePath(raw, userHome string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if userHome == "" {
		return raw
	}

	if strings.HasPrefix(raw, "~") {
		trimmed := strings.TrimPrefix(raw, "~")
		trimmed = strings.TrimPrefix(trimmed, string(filepath.Separator))
		return filepath.Join(userHome, trimmed)
	}

	if strings.HasPrefix(raw, "/Users/") && strings.HasPrefix(userHome, "/Users/") {
		rest := strings.TrimPrefix(raw, "/Users/")
		oldUser, remainder, _ := strings.Cut(rest, "/")
		if oldUser == "" {
			return raw
		}

		homeRest := strings.TrimPrefix(userHome, "/Users/")
		newUser, _, _ := strings.Cut(homeRest, "/")
		if newUser == "" || newUser == oldUser {
			return raw
		}

		if _, err := os.Stat(filepath.Join("/Users", oldUser)); os.IsNotExist(err) {
			if remainder == "" {
				return filepath.Join("/Users", newUser)
			}
			return filepath.Join("/Users", newUser, remainder)
		}
	}

	return raw
}
