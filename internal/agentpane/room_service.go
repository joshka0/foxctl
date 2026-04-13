package agentpane

import (
	"fmt"
	"strings"
)

// TerminalRoomRegistrar applies canonical room registration to a transport adapter.
type TerminalRoomRegistrar interface {
	RegisterTerminalRoom(config TerminalRoomConfig)
	UnregisterRoom(roomID string)
}

// TerminalRoomService fans runtime-terminal room registration out to one or more adapters.
type TerminalRoomService struct {
	registrars []TerminalRoomRegistrar
}

// NewTerminalRoomService creates a room service for the provided registrars.
func NewTerminalRoomService(registrars ...TerminalRoomRegistrar) *TerminalRoomService {
	filtered := make([]TerminalRoomRegistrar, 0, len(registrars))
	for _, registrar := range registrars {
		if registrar != nil {
			filtered = append(filtered, registrar)
		}
	}
	return &TerminalRoomService{registrars: filtered}
}

// Register applies a canonical room config to all registrars.
func (s *TerminalRoomService) Register(config TerminalRoomConfig) {
	for _, registrar := range s.registrars {
		registrar.RegisterTerminalRoom(config)
	}
}

// Unregister removes a room from all registrars.
func (s *TerminalRoomService) Unregister(roomID string) {
	roomID = strings.TrimSpace(roomID)
	for _, registrar := range s.registrars {
		registrar.UnregisterRoom(roomID)
	}
}

// TerminalRoomRegisterRequest is the transport-facing room registration request shape.
type TerminalRoomRegisterRequest struct {
	RoomID         string
	TmuxSession    string
	MaxConnections int
}

// NormalizeRegisterRequest validates and normalizes a room registration request.
func NormalizeRegisterRequest(req TerminalRoomRegisterRequest) (TerminalRoomConfig, error) {
	roomID := strings.TrimSpace(req.RoomID)
	if roomID == "" {
		return TerminalRoomConfig{}, fmt.Errorf("room_id is required")
	}
	tmuxSession := strings.TrimSpace(req.TmuxSession)
	if tmuxSession == "" {
		return TerminalRoomConfig{}, fmt.Errorf("tmux_session is required")
	}
	return ResolveTerminalRoomConfig(roomID, tmuxSession, req.MaxConnections), nil
}
