package webterm

import (
	"time"
)

// ResizeMessage is a JSON control message sent from the browser to resize the PTY.
type ResizeMessage struct {
	Type string `json:"type"` // "resize"
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// ControlMessage is the envelope for JSON control messages over the WebSocket.
type ControlMessage struct {
	Type string `json:"type"` // "resize", etc.
}

const (
	// DefaultMaxConnections is the default maximum concurrent WebSocket connections per room.
	DefaultMaxConnections = 10

	// DefaultPingInterval is the interval for WebSocket ping/pong keepalive.
	DefaultPingInterval = 30 * time.Second

	// DefaultWriteTimeout is the timeout for writing to the WebSocket.
	DefaultWriteTimeout = 10 * time.Second

	// DefaultInitialCols is the default PTY column count.
	DefaultInitialCols = 80

	// DefaultInitialRows is the default PTY row count.
	DefaultInitialRows = 24

	// InputBufferSize is the buffer size for the client input channel.
	InputBufferSize = 256

	// OutputBufferSize is the buffer size for the client output channel.
	OutputBufferSize = 4096

	// TmuxAttachTimeout is the timeout for tmux attach to produce output.
	TmuxAttachTimeout = 10 * time.Second
)

// RoomConfig holds per-room terminal configuration.
type RoomConfig struct {
	// TmuxSession is the tmux session name for this room.
	TmuxSession string

	// MaxConnections is the max concurrent WebSocket connections for this room.
	// Zero means use the hub default.
	MaxConnections int
}

// HubConfig holds hub-level configuration.
type HubConfig struct {
	// MaxConnectionsPerRoom is the default maximum concurrent connections per room.
	// Zero means DefaultMaxConnections.
	MaxConnectionsPerRoom int

	// PingInterval is the WebSocket ping/pong interval.
	// Zero means DefaultPingInterval.
	PingInterval time.Duration

	// Shell is the command to run in the PTY when not using tmux.
	Shell string

	// TmuxPath is the path to the tmux binary.
	// Empty means use "tmux" from PATH.
	TmuxPath string
}
