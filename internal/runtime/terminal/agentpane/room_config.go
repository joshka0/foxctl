package agentpane

import (
	"strings"

	terminalruntime "github.com/joshka0/foxctl/internal/runtime/terminal"
)

// TerminalRoomConfig is the canonical runtime-terminal room registration shape.
type TerminalRoomConfig struct {
	RoomID         string
	TmuxSession    string
	MaxConnections int
}

// ResolveTerminalRoomConfig normalizes a room registration through the
// runtime-terminal contract.
func ResolveTerminalRoomConfig(roomID, tmuxSession string, maxConnections int) TerminalRoomConfig {
	roomID = strings.TrimSpace(roomID)
	return TerminalRoomConfig{
		RoomID:         roomID,
		TmuxSession:    terminalruntime.ResolveRoomTmuxSession(roomID, tmuxSession),
		MaxConnections: maxConnections,
	}
}
