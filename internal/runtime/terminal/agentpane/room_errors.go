package agentpane

import "fmt"

// RoomNotFoundError is returned when a runtime-terminal room is not registered.
type RoomNotFoundError struct {
	RoomID string
}

func (e *RoomNotFoundError) Error() string {
	return fmt.Sprintf("room not found: %s", e.RoomID)
}

// FormatRoomLimitError formats a room-capacity error consistently across adapters.
func FormatRoomLimitError(roomID, limitKind string, current, maxAllowed int) string {
	return fmt.Sprintf("room %s: %s (%d/%d)", roomID, limitKind, current, maxAllowed)
}
