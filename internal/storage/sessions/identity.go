package sessions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ActiveSession represents the currently active session identity for a workspace.
type ActiveSession struct {
	SessionID string    `json:"session_id"`
	Workspace string    `json:"workspace"`
	AgentID   string    `json:"agent_id"`
	StartedAt time.Time `json:"started_at"`
	ParentID  string    `json:"parent_id,omitempty"`
}

// IdentityManager handles active session tracking via identity files.
// These files are stored in ~/.agentctl/sessions/active/<workspace_hash>.json
type IdentityManager struct {
	baseDir string
}

// NewIdentityManager creates a new identity manager.
func NewIdentityManager(agentctlHome string) *IdentityManager {
	return &IdentityManager{
		baseDir: filepath.Join(agentctlHome, "sessions", "active"),
	}
}

// workspaceHash creates a hash of the workspace path for the filename.
func workspaceHash(workspace string) string {
	hash := sha256.Sum256([]byte(workspace))
	return hex.EncodeToString(hash[:8]) // First 8 bytes = 16 hex chars
}

func sanitizeAgentIDForFilename(agentID string) string {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return ""
	}

	// Only allow safe filename characters.
	agentID = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		default:
			return '_'
		}
	}, agentID)

	if len(agentID) > 64 {
		agentID = agentID[:64]
	}

	return agentID
}

// identityPath returns the path to the identity file for a workspace.
func (m *IdentityManager) identityPath(workspace, agentID string) string {
	base := workspaceHash(workspace)
	agentID = sanitizeAgentIDForFilename(agentID)
	if agentID != "" {
		return filepath.Join(m.baseDir, fmt.Sprintf("%s-%s.json", base, agentID))
	}
	return filepath.Join(m.baseDir, base+".json")
}

// SetActive sets the active session for a workspace.
func (m *IdentityManager) SetActive(session ActiveSession) error {
	if err := os.MkdirAll(m.baseDir, 0o755); err != nil {
		return fmt.Errorf("create identity dir: %w", err)
	}

	session.AgentID = strings.TrimSpace(session.AgentID)
	if session.AgentID == "" {
		session.AgentID = "agentctl"
	}

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	path := m.identityPath(session.Workspace, session.AgentID)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write identity file: %w", err)
	}

	return nil
}

// GetActive returns the active session for a workspace, or nil if none.
func (m *IdentityManager) GetActive(workspace string) (*ActiveSession, error) {
	base := workspaceHash(workspace)

	candidates := make([]string, 0, 3)
	if agentID := strings.TrimSpace(os.Getenv("AGENTCTL_AGENT_ID")); agentID != "" {
		candidates = append(candidates, m.identityPath(workspace, agentID))
	}

	entries, err := os.ReadDir(m.baseDir)
	if err == nil {
		prefix := base + "-"
		newestPath := ""
		var newestMod time.Time
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			name := entry.Name()
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if newestPath == "" || info.ModTime().After(newestMod) {
				newestMod = info.ModTime()
				newestPath = filepath.Join(m.baseDir, name)
			}
		}
		if newestPath != "" {
			candidates = append(candidates, newestPath)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read identity dir: %w", err)
	}

	candidates = append(candidates, m.identityPath(workspace, ""))

	seen := make(map[string]struct{}, len(candidates))
	for _, path := range candidates {
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}

		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read identity file: %w", err)
		}

		var session ActiveSession
		if err := json.Unmarshal(data, &session); err != nil {
			return nil, fmt.Errorf("unmarshal session: %w", err)
		}
		if strings.TrimSpace(session.SessionID) == "" {
			continue
		}
		return &session, nil
	}

	return nil, nil
}

// ClearActive removes the active session for a workspace.
func (m *IdentityManager) ClearActive(workspace string) error {
	base := workspaceHash(workspace)

	legacy := m.identityPath(workspace, "")
	if legacy != "" {
		if err := os.Remove(legacy); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove identity file: %w", err)
		}
	}

	entries, err := os.ReadDir(m.baseDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read identity dir: %w", err)
	}

	prefix := base + "-"
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		path := filepath.Join(m.baseDir, entry.Name())
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove identity file: %w", err)
		}
	}

	return nil
}

// ListActive returns all active sessions across workspaces.
// Always returns a non-nil slice for stable JSON envelope output.
func (m *IdentityManager) ListActive() ([]ActiveSession, error) {
	entries, err := os.ReadDir(m.baseDir)
	if os.IsNotExist(err) {
		return []ActiveSession{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read identity dir: %w", err)
	}

	sessions := make([]ActiveSession, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(m.baseDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var session ActiveSession
		if err := json.Unmarshal(data, &session); err != nil {
			continue
		}

		sessions = append(sessions, session)
	}

	return sessions, nil
}
