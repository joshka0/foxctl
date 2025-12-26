package sessions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// identityPath returns the path to the identity file for a workspace.
func (m *IdentityManager) identityPath(workspace string) string {
	return filepath.Join(m.baseDir, workspaceHash(workspace)+".json")
}

// SetActive sets the active session for a workspace.
func (m *IdentityManager) SetActive(session ActiveSession) error {
	if err := os.MkdirAll(m.baseDir, 0o755); err != nil {
		return fmt.Errorf("create identity dir: %w", err)
	}

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	path := m.identityPath(session.Workspace)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write identity file: %w", err)
	}

	return nil
}

// GetActive returns the active session for a workspace, or nil if none.
func (m *IdentityManager) GetActive(workspace string) (*ActiveSession, error) {
	path := m.identityPath(workspace)

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read identity file: %w", err)
	}

	var session ActiveSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}

	return &session, nil
}

// ClearActive removes the active session for a workspace.
func (m *IdentityManager) ClearActive(workspace string) error {
	path := m.identityPath(workspace)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove identity file: %w", err)
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
