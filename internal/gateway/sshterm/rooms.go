package sshterm

import (
	"context"
	"sync"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/agentpane"
	terminalruntime "github.com/jkatigb/agentctl/internal/runtime/terminal"
)

// RoomManager manages SSH terminal rooms and tracks active sessions.
type RoomManager struct {
	mu       sync.RWMutex
	rooms    map[string]*sshRoom
	registry *agentpane.TerminalRoomRegistry
	sessions map[string]*SSHSession // session ID -> session
	config   SSHServerConfig
	log      zerolog.Logger
}

// sshRoom tracks a room and its active SSH sessions.
type sshRoom struct {
	sessions map[string]*SSHSession // session ID -> session
}

// NewRoomManager creates a new room manager.
func NewRoomManager(config SSHServerConfig, log zerolog.Logger) *RoomManager {
	return &RoomManager{
		rooms:    make(map[string]*sshRoom),
		registry: agentpane.NewTerminalRoomRegistry(),
		sessions: make(map[string]*SSHSession),
		config:   config,
		log:      log.With().Str("component", "sshterm-rooms").Logger(),
	}
}

// RegisterTerminalRoom registers a room from the canonical runtime-terminal config.
func (m *RoomManager) RegisterTerminalRoom(config agentpane.TerminalRoomConfig) {
	m.registry.Register(config)

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.rooms[config.RoomID]; ok {
		m.log.Debug().Str("room", config.RoomID).Msg("Room already registered, updating config")
		return
	}

	m.rooms[config.RoomID] = &sshRoom{
		sessions: make(map[string]*SSHSession),
	}
	m.log.Debug().Str("room", config.RoomID).Msg("Room registered for SSH")
}

// UnregisterRoom removes a room and disconnects all active sessions.
func (m *RoomManager) UnregisterRoom(roomID string) {
	m.registry.Unregister(roomID)

	m.mu.Lock()
	defer m.mu.Unlock()

	room, ok := m.rooms[roomID]
	if !ok {
		return
	}

	// Close all active sessions
	for id, sess := range room.sessions {
		sess.Close()
		delete(m.sessions, id)
	}

	delete(m.rooms, roomID)
	m.log.Debug().Str("room", roomID).Msg("Room unregistered from SSH")
}

// HasRoom returns true if the room is registered.
func (m *RoomManager) HasRoom(roomID string) bool {
	return m.registry.HasRoom(roomID)
}

// TerminalRoomConfig returns the canonical runtime-terminal config for a room.
func (m *RoomManager) TerminalRoomConfig(roomID string) (agentpane.TerminalRoomConfig, bool) {
	return m.registry.RoomConfig(roomID)
}

// AddSession adds an SSH session to a room.
// Returns an error if the room doesn't exist or is at capacity.
func (m *RoomManager) AddSession(roomID string, session *SSHSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	room, ok := m.rooms[roomID]
	if !ok {
		return &RoomNotFoundError{RoomID: roomID}
	}

	cfg, cfgOK := m.registry.RoomConfig(roomID)
	if !cfgOK {
		return &RoomNotFoundError{RoomID: roomID}
	}

	maxSessions := agentpane.EffectiveRoomLimit(cfg.MaxConnections, m.config.MaxSessions)
	if agentpane.RoomLimitReached(len(room.sessions), maxSessions) {
		return &SessionLimitError{
			RoomID:     roomID,
			Current:    len(room.sessions),
			MaxAllowed: maxSessions,
		}
	}

	room.sessions[session.ID] = session
	m.sessions[session.ID] = session

	m.log.Debug().
		Str("room", roomID).
		Str("session", session.ID).
		Int("sessions", len(room.sessions)).
		Msg("SSH session added to room")

	return nil
}

// RemoveSession removes an SSH session from its room.
func (m *RoomManager) RemoveSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[sessionID]
	if !ok {
		return
	}

	if room, roomOK := m.rooms[sess.RoomID]; roomOK {
		delete(room.sessions, sessionID)
		m.log.Debug().
			Str("room", sess.RoomID).
			Str("session", sessionID).
			Int("sessions", len(room.sessions)).
			Msg("SSH session removed from room")
	}

	delete(m.sessions, sessionID)
}

// SessionCount returns the number of active SSH sessions for a room.
func (m *RoomManager) SessionCount(roomID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	room, ok := m.rooms[roomID]
	if !ok {
		return 0
	}
	return len(room.sessions)
}

// ActiveSessions returns info about all active sessions for a room.
func (m *RoomManager) ActiveSessions(roomID string) []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	room, ok := m.rooms[roomID]
	if !ok {
		return nil
	}

	result := make([]SessionInfo, 0, len(room.sessions))
	for _, sess := range room.sessions {
		info := SessionInfo{
			ID:         sess.ID,
			RoomID:     sess.RoomID,
			RemoteAddr: sess.RemoteAddr,
			User:       sess.Identity.UserLogin,
			NodeName:   sess.Identity.NodeName,
			StartedAt:  sess.StartedAt,
		}
		if sess.Terminal != nil {
			info.Terminal = &TerminalInfo{
				Term: sess.Terminal.Term,
				Cols: sess.Terminal.Cols,
				Rows: sess.Terminal.Rows,
			}
		}
		result = append(result, info)
	}
	return result
}

// ParseRoomIDFromUser parses the room ID from an SSH username.
// The expected format is "room-<id>" (e.g., "room-my-room" → "my-room").
// Returns empty string if the username doesn't match the pattern.
func ParseRoomIDFromUser(user string) string {
	return terminalruntime.ParseRoomTerminalUser(user)
}

// TmuxSessionForRoom returns the tmux session name for a room.
func (m *RoomManager) TmuxSessionForRoom(_ context.Context, roomID string) (string, error) {
	cfg, ok := m.registry.RoomConfig(roomID)
	if !ok {
		return "", &RoomNotFoundError{RoomID: roomID}
	}
	return cfg.TmuxSession, nil
}

// Close removes all rooms and sessions.
func (m *RoomManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, room := range m.rooms {
		for _, sess := range room.sessions {
			sess.Close()
		}
	}

	m.rooms = make(map[string]*sshRoom)
	m.registry.Reset()
	m.sessions = make(map[string]*SSHSession)
}
