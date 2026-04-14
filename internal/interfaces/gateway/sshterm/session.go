package sshterm

import (
	"sync"
	"time"
)

// TerminalState holds mutable terminal dimensions.
type TerminalState struct {
	Term string
	Cols uint16
	Rows uint16
}

// SSHSession tracks an active SSH session's state.
type SSHSession struct {
	// ID is the unique session identifier.
	ID string

	// RoomID is the room this session belongs to.
	RoomID string

	// RemoteAddr is the remote network address.
	RemoteAddr string

	// Identity holds the Tailscale WhoIs identity.
	Identity IdentityInfo

	// StartedAt is when the session was created.
	StartedAt time.Time

	// Terminal holds PTY info (may be nil if no PTY requested).
	Terminal *TerminalState

	mu     sync.Mutex
	closed bool
}

// Close marks the session as closed.
func (s *SSHSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}

// IsClosed returns whether the session has been closed.
func (s *SSHSession) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// SetTerminal updates the terminal dimensions.
func (s *SSHSession) SetTerminal(term string, cols, rows uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Terminal == nil {
		s.Terminal = &TerminalState{}
	}
	s.Terminal.Term = term
	s.Terminal.Cols = cols
	s.Terminal.Rows = rows
}
