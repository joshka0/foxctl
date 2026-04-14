package sshterm

import (
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/terminal/agentpane"
)

// SSHServerConfig holds the configuration for the SSH server.
type SSHServerConfig struct {
	// HostKey is the SSH host key. If empty, an ephemeral key is generated.
	HostKey []byte

	// DefaultTerm is the default TERM environment variable.
	// Defaults to "xterm-256color".
	DefaultTerm string

	// DefaultShell is the default shell when not using tmux.
	// Defaults to "/bin/sh".
	DefaultShell string

	// MaxSessions is the maximum number of concurrent SSH sessions per room.
	// Zero means unlimited.
	MaxSessions int

	// IdleTimeout is the timeout for idle SSH connections.
	// Zero means no timeout.
	IdleTimeout time.Duration

	// TmuxPath is the path to the tmux binary.
	// Empty means "tmux" from PATH.
	TmuxPath string
}

// DefaultSSHServerConfig returns a config with sensible defaults.
func DefaultSSHServerConfig() SSHServerConfig {
	return SSHServerConfig{
		DefaultTerm:  "xterm-256color",
		DefaultShell: "/bin/sh",
		MaxSessions:  10,
	}
}

// SessionInfo holds metadata about an active SSH session.
type SessionInfo struct {
	// ID is the unique session identifier.
	ID string

	// RoomID is the room this session is connected to.
	RoomID string

	// RemoteAddr is the remote network address.
	RemoteAddr string

	// User is the Tailscale user identity (email or name).
	User string

	// NodeName is the Tailscale node name.
	NodeName string

	// StartedAt is when the session was established.
	StartedAt time.Time

	// Terminal contains PTY info if a PTY was requested.
	Terminal *TerminalInfo
}

// TerminalInfo holds PTY-related metadata.
type TerminalInfo struct {
	// Term is the TERM environment variable value.
	Term string

	// Cols is the terminal width in columns.
	Cols uint16

	// Rows is the terminal height in rows.
	Rows uint16
}

// IdentityInfo holds Tailscale WhoIs identity information.
type IdentityInfo struct {
	// UserID is the Tailscale user ID.
	UserID string

	// UserLogin is the user's login name (typically email).
	UserLogin string

	// UserName is the user's display name.
	UserName string

	// NodeName is the Tailscale node name.
	NodeName string

	// NodeID is the Tailscale node ID.
	NodeID string
}

// String returns a human-readable representation of the identity.
func (i IdentityInfo) String() string {
	return fmt.Sprintf("%s (%s) from %s", i.UserLogin, i.UserName, i.NodeName)
}

// RoomNotFoundError is returned when a room is not registered.
type RoomNotFoundError = agentpane.RoomNotFoundError

// IdentityRejectedError is returned when a non-tailnet connection is rejected.
type IdentityRejectedError struct {
	RemoteAddr string
	Reason     string
}

func (e *IdentityRejectedError) Error() string {
	return fmt.Sprintf("connection rejected from %s: %s", e.RemoteAddr, e.Reason)
}

// SessionLimitError is returned when a room has reached its SSH session limit.
type SessionLimitError struct {
	RoomID     string
	Current    int
	MaxAllowed int
}

func (e *SessionLimitError) Error() string {
	return agentpane.FormatRoomLimitError(e.RoomID, "SSH session limit reached", e.Current, e.MaxAllowed)
}
