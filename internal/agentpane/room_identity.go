package agentpane

import "strings"

const roomTerminalUserPrefix = "room-"

// RoomTerminalUser returns the canonical runtime-terminal SSH username for a room.
func RoomTerminalUser(roomID string) string {
	return roomTerminalUserPrefix + strings.TrimSpace(roomID)
}

// ParseRoomTerminalUser extracts the room ID from the canonical runtime-terminal SSH username.
// Valid usernames use the form "room-<id>".
func ParseRoomTerminalUser(user string) string {
	user = strings.TrimSpace(user)
	if !strings.HasPrefix(user, roomTerminalUserPrefix) || len(user) <= len(roomTerminalUserPrefix) {
		return ""
	}
	return user[len(roomTerminalUserPrefix):]
}

// DefaultRoomTmuxSession returns the compatibility tmux session name for a room.
func DefaultRoomTmuxSession(roomID string) string {
	return RoomTerminalUser(roomID)
}

// ResolveRoomTmuxSession returns an explicit tmux session override when present,
// otherwise it falls back to the canonical compatibility session for the room.
func ResolveRoomTmuxSession(roomID, override string) string {
	override = strings.TrimSpace(override)
	if override != "" {
		return override
	}
	return DefaultRoomTmuxSession(roomID)
}
