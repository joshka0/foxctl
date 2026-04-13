// Package webterm implements the WebSocket-to-PTY bridge for web terminal access.
//
// It provides:
//   - WebSocket upgrade and message framing (binary terminal I/O + JSON control)
//   - PTY lifecycle management (spawn tmux attach, resize, cleanup)
//   - Room-based session registry with concurrent client support
//   - Connection limiting per room
//   - Ping/pong keepalive for idle connections
package webterm
