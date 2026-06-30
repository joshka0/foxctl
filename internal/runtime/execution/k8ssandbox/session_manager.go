// Package sandbox manages per-session k8s sandbox lifecycle.
//
// Each agent session gets its own sandbox pod. The sandbox is created when
// the session starts and reused for all skill executions within that session.
// On session end, the sandbox is deleted (or hibernated if configured).
package sandbox

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/execution/k8ssandbox"
)

// SessionManager manages per-session sandbox lifecycle.
// Each agent session maps to one sandbox pod that persists across skill calls.
type SessionManager struct {
	cfg      k8ssandbox.Config
	mu       sync.Mutex
	sessions map[string]*SessionSandbox
}

// SessionSandbox wraps a k8s sandbox runner for a specific agent session.
type SessionSandbox struct {
	SessionID string
	Runner    *k8ssandbox.Runner
	CreatedAt time.Time
	LastUsed  time.Time
	apiURL    string
}

// NewSessionManager creates a session-level sandbox manager.
func NewSessionManager(cfg k8ssandbox.Config) *SessionManager {
	return &SessionManager{
		cfg:      cfg,
		sessions: make(map[string]*SessionSandbox),
	}
}

// GetOrCreate returns the sandbox for the given session, creating one if needed.
// The session ID maps to a sandbox claim name: foxctl-session-<id>.
func (m *SessionManager) GetOrCreate(ctx context.Context, sessionID string) (*SessionSandbox, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ss, ok := m.sessions[sessionID]; ok {
		ss.LastUsed = time.Now()
		return ss, nil
	}

	// Create a new runner for this session.
	// In direct mode, the APIURL is shared (pointing to the warm pool pod).
	// In gateway mode, each session would get its own pod via SandboxClaim.
	cfg := m.cfg
	runner, err := k8ssandbox.NewRunner(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("session sandbox: create runner: %w", err)
	}

	ss := &SessionSandbox{
		SessionID: sessionID,
		Runner:    runner,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
		apiURL:    cfg.APIURL,
	}
	m.sessions[sessionID] = ss
	return ss, nil
}

// Execute runs a command in the session's sandbox.
func (ss *SessionSandbox) Execute(ctx context.Context, command string, input []byte, env []string) (*k8ssandbox.RawResult, error) {
	ss.LastUsed = time.Now()
	return ss.Runner.ExecuteRaw(ctx, command, input, env)
}

// Close destroys the sandbox for this session.
func (ss *SessionSandbox) Close(ctx context.Context) error {
	return ss.Runner.Close(ctx)
}

// Release closes the sandbox for a session and removes it from the manager.
func (m *SessionManager) Release(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ss, ok := m.sessions[sessionID]
	if !ok {
		return nil
	}
	delete(m.sessions, sessionID)
	return ss.Close(ctx)
}

// ActiveSessions returns the list of active session sandbox IDs.
func (m *SessionManager) ActiveSessions() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	return ids
}

// IdleSessions returns sessions that haven't been used since the given threshold.
// These are candidates for hibernation.
func (m *SessionManager) IdleSessions(threshold time.Duration) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-threshold)
	var idle []string
	for id, ss := range m.sessions {
		if ss.LastUsed.Before(cutoff) {
			idle = append(idle, id)
		}
	}
	return idle
}
