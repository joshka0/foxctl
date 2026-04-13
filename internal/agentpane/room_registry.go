package agentpane

import "sync"

// TerminalRoomRegistry stores canonical runtime-terminal room configs.
type TerminalRoomRegistry struct {
	mu    sync.RWMutex
	rooms map[string]TerminalRoomConfig
}

// NewTerminalRoomRegistry creates an empty terminal room registry.
func NewTerminalRoomRegistry() *TerminalRoomRegistry {
	return &TerminalRoomRegistry{
		rooms: make(map[string]TerminalRoomConfig),
	}
}

// Register stores or updates a room config.
func (r *TerminalRoomRegistry) Register(config TerminalRoomConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rooms[config.RoomID] = config
}

// Unregister removes a room config.
func (r *TerminalRoomRegistry) Unregister(roomID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rooms, roomID)
}

// HasRoom reports whether a room is registered.
func (r *TerminalRoomRegistry) HasRoom(roomID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.rooms[roomID]
	return ok
}

// RoomIDs returns all registered room IDs.
func (r *TerminalRoomRegistry) RoomIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.rooms))
	for id := range r.rooms {
		ids = append(ids, id)
	}
	return ids
}

// RoomConfig returns the config for a room.
func (r *TerminalRoomRegistry) RoomConfig(roomID string) (TerminalRoomConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cfg, ok := r.rooms[roomID]
	return cfg, ok
}

// Reset clears the registry.
func (r *TerminalRoomRegistry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rooms = make(map[string]TerminalRoomConfig)
}
