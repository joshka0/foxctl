package sshterm

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog"
)

// RoomManager manages SSH terminal rooms and tracks active sessions.
type RoomManager struct {
	mu       sync.RWMutex
	rooms    map[string]*sshRoom
	sessions map[string]*SSHSession // session ID -> session
	config   SSHServerConfig
	log      zerolog.Logger
}

// sshRoom tracks a room and its active SSH sessions.
type sshRoom struct {
	config   RoomConfig
	sessions map[string]*SSHSession // session ID -> session
}

// NewRoomManager creates a new room manager.
func NewRoomManager(config SSHServerConfig, log zerolog.Logger) *RoomManager {
	return &RoomManager{
		rooms:    make(map[string]*sshRoom),
		sessions: make(map[string]*SSHSession),
		config:   config,
		log:      log.With().Str("component", "sshterm-rooms").Logger(),
	}
}

// RegisterRoom registers a room for SSH terminal access.
func (m *RoomManager) RegisterRoom(roomID string, config RoomConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.rooms[roomID]; ok {
		m.log.Debug().Str("room", roomID).Msg("Room already registered, updating config")
		m.rooms[roomID].config = config
		return
	}

	m.rooms[roomID] = &sshRoom{
		config:   config,
		sessions: make(map[string]*SSHSession),
	}
	m.log.Debug().Str("room", roomID).Msg("Room registered for SSH")
}

// UnregisterRoom removes a room and disconnects all active sessions.
func (m *RoomManager) UnregisterRoom(roomID string) {
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
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.rooms[roomID]
	return ok
}

// RoomConfig returns the config for a room.
func (m *RoomManager) RoomConfig(roomID string) (RoomConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	room, ok := m.rooms[roomID]
	if !ok {
		return RoomConfig{}, false
	}
	return room.config, true
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

	maxSessions := room.config.MaxSessions
	if maxSessions <= 0 {
		maxSessions = m.config.MaxSessions
	}

	if maxSessions > 0 && len(room.sessions) >= maxSessions {
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
	prefix := "room-"
	if len(user) <= len(prefix) {
		return ""
	}
	if user[:len(prefix)] != prefix {
		return ""
	}
	return user[len(prefix):]
}

// TmuxSessionForRoom returns the tmux session name for a room.
func (m *RoomManager) TmuxSessionForRoom(_ context.Context, roomID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	room, ok := m.rooms[roomID]
	if !ok {
		return "", &RoomNotFoundError{RoomID: roomID}
	}

	sessionName := room.config.TmuxSession
	if sessionName == "" {
		sessionName = fmt.Sprintf("room-%s", roomID)
	}

	return sessionName, nil
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
	m.sessions = make(map[string]*SSHSession)
}
