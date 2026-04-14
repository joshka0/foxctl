// Package sshterm implements an SSH server for room-based terminal access.
//
// The SSH server authenticates connections via Tailscale WhoIs identity
// verification, routes to tmux sessions based on the SSH username
// (room-<id>@<hostname>), and provides full PTY support with signal
// propagation for interactive terminal usage.
//
// Connections from non-tailnet sources are rejected. Every connection
// triggers a WhoIs lookup and the identity is logged.
package sshterm
