package webterm

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/runtime/terminal/agentpane"
)

// Room represents a terminal room with one shared PTY and multiple clients.
type Room struct {
	id      string
	hub     *Hub
	mu      sync.Mutex
	clients map[*Client]struct{}
	pty     *PTYProcess
	// started is true once the PTY process has been launched for this room.
	started bool
	// startWait is non-nil while a PTY launch is in progress.
	startWait chan struct{}
	closed    bool
}

// Hub manages all web terminal rooms and their connections.
type Hub struct {
	mu       sync.RWMutex
	rooms    map[string]*Room
	registry *agentpane.TerminalRoomRegistry
	config   HubConfig
	log      zerolog.Logger
}

var (
	startTmuxAttach = StartTmuxAttach
	closePTYProcess = func(p *PTYProcess) {
		if p != nil {
			p.Close()
		}
	}
)

// NewHub creates a new web terminal hub.
func NewHub(config HubConfig, log zerolog.Logger) *Hub {
	if config.MaxConnectionsPerRoom <= 0 {
		config.MaxConnectionsPerRoom = DefaultMaxConnections
	}
	if config.PingInterval <= 0 {
		config.PingInterval = DefaultPingInterval
	}
	return &Hub{
		rooms:    make(map[string]*Room),
		registry: agentpane.NewTerminalRoomRegistry(),
		config:   config,
		log:      log.With().Str("component", "webterm").Logger(),
	}
}

// RegisterTerminalRoom registers a room from the canonical runtime-terminal config.
func (h *Hub) RegisterTerminalRoom(config agentpane.TerminalRoomConfig) {
	h.registry.Register(config)

	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.rooms[config.RoomID]; ok {
		h.log.Debug().Str("room", config.RoomID).Msg("Room config updated")
		return
	}

	h.rooms[config.RoomID] = &Room{
		id:      config.RoomID,
		hub:     h,
		clients: make(map[*Client]struct{}),
	}
	h.log.Debug().Str("room", config.RoomID).Msg("Room registered")
}

// UnregisterRoom removes a room from the hub.
// Active clients are disconnected.
func (h *Hub) UnregisterRoom(roomID string) {
	h.registry.Unregister(roomID)

	h.mu.Lock()
	room, ok := h.rooms[roomID]
	if !ok {
		h.mu.Unlock()
		return
	}
	delete(h.rooms, roomID)
	h.mu.Unlock()

	room.mu.Lock()
	room.closed = true
	clients := make([]*Client, 0, len(room.clients))
	for c := range room.clients {
		clients = append(clients, c)
	}
	room.clients = make(map[*Client]struct{})
	pty := room.pty
	room.pty = nil
	room.mu.Unlock()

	// Close all clients
	for _, c := range clients {
		c.close()
	}

	// Close PTY if running
	closePTYProcess(pty)

	h.log.Debug().Str("room", roomID).Msg("Room unregistered")
}

// HasRoom returns true if the room is registered.
func (h *Hub) HasRoom(roomID string) bool {
	return h.registry.HasRoom(roomID)
}

// RoomIDs returns all registered room IDs.
func (h *Hub) RoomIDs() []string {
	return h.registry.RoomIDs()
}

// AddClient attempts to add a client to a room.
// Returns an error if the room doesn't exist or is at capacity.
func (h *Hub) AddClient(roomID string, client *Client) error {
	h.mu.RLock()
	room, ok := h.rooms[roomID]
	h.mu.RUnlock()

	if !ok {
		return &RoomNotFoundError{RoomID: roomID}
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	cfg, cfgOK := h.registry.RoomConfig(roomID)
	if !cfgOK {
		return &RoomNotFoundError{RoomID: roomID}
	}

	maxConns := agentpane.EffectiveRoomLimit(cfg.MaxConnections, h.config.MaxConnectionsPerRoom)
	if agentpane.RoomLimitReached(len(room.clients), maxConns) {
		return &ConnectionLimitError{
			RoomID:     roomID,
			Current:    len(room.clients),
			MaxAllowed: maxConns,
		}
	}

	room.clients[client] = struct{}{}
	client.room = room
	h.log.Debug().
		Str("room", roomID).
		Int("clients", len(room.clients)).
		Msg("Client added to room")

	return nil
}

// RemoveClient removes a client from its room and cleans up.
func (h *Hub) RemoveClient(client *Client) {
	room := client.room
	if room == nil {
		return
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	if _, ok := room.clients[client]; ok {
		delete(room.clients, client)
		h.log.Debug().
			Str("room", room.id).
			Int("clients", len(room.clients)).
			Msg("Client removed from room")
	}
}

// ClientCount returns the number of active clients for a room.
func (h *Hub) ClientCount(roomID string) int {
	h.mu.RLock()
	room, ok := h.rooms[roomID]
	h.mu.RUnlock()

	if !ok {
		return 0
	}

	room.mu.Lock()
	defer room.mu.Unlock()
	return len(room.clients)
}

// ResizePTY resizes the PTY for a room.
func (h *Hub) ResizePTY(roomID string, cols, rows uint16) error {
	h.mu.RLock()
	room, ok := h.rooms[roomID]
	h.mu.RUnlock()

	if !ok {
		return &RoomNotFoundError{RoomID: roomID}
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	if room.pty == nil {
		return fmt.Errorf("no PTY for room %s", roomID)
	}

	return room.pty.Resize(cols, rows)
}

// RoomPTY gets or creates the PTY for a room. Used by the WebSocket handler.
func (h *Hub) RoomPTY(ctx context.Context, roomID string, cols, rows uint16) (*PTYProcess, error) {
	h.mu.RLock()
	room, ok := h.rooms[roomID]
	h.mu.RUnlock()

	if !ok {
		return nil, &RoomNotFoundError{RoomID: roomID}
	}

	for {
		room.mu.Lock()
		if room.closed {
			room.mu.Unlock()
			return nil, &RoomNotFoundError{RoomID: roomID}
		}
		if room.pty != nil && room.pty.IsRunning() {
			pty := room.pty
			room.mu.Unlock()
			return pty, nil
		}
		if room.startWait != nil {
			wait := room.startWait
			room.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-wait:
				continue
			}
		}

		wait := make(chan struct{})
		oldPTY := room.pty
		room.pty = nil
		room.startWait = wait
		cfg, cfgOK := h.registry.RoomConfig(room.id)
		if !cfgOK {
			room.startWait = nil
			room.mu.Unlock()
			close(wait)
			closePTYProcess(oldPTY)
			return nil, &RoomNotFoundError{RoomID: roomID}
		}
		sessionName := cfg.TmuxSession
		room.mu.Unlock()

		closePTYProcess(oldPTY)
		pty, err := startTmuxAttach(ctx, TmuxOptions{
			Session:  sessionName,
			Cols:     cols,
			Rows:     rows,
			TmuxPath: h.config.TmuxPath,
			Shell:    h.config.Shell,
		})

		room.mu.Lock()
		room.startWait = nil
		roomClosed := room.closed
		if err == nil && !roomClosed {
			room.pty = pty
			room.started = true
		}
		room.mu.Unlock()
		close(wait)

		if roomClosed {
			closePTYProcess(pty)
			return nil, &RoomNotFoundError{RoomID: roomID}
		}
		if err != nil {
			return nil, fmt.Errorf("start tmux attach for room %s: %w", room.id, err)
		}

		h.log.Info().
			Str("room", room.id).
			Str("session", sessionName).
			Msg("PTY created for room")
		return pty, nil
	}
}

// Close shuts down the hub and all rooms/clients.
func (h *Hub) Close() {
	h.mu.Lock()
	rooms := h.rooms
	h.rooms = make(map[string]*Room)
	h.mu.Unlock()
	h.registry.Reset()

	for id, room := range rooms {
		room.mu.Lock()
		room.closed = true
		clients := make([]*Client, 0, len(room.clients))
		for c := range room.clients {
			clients = append(clients, c)
		}
		room.clients = make(map[*Client]struct{})
		pty := room.pty
		room.pty = nil
		room.mu.Unlock()

		for _, c := range clients {
			c.close()
		}
		closePTYProcess(pty)

		h.log.Debug().Str("room", id).Msg("Room closed")
	}
}

// RoomNotFoundError is returned when a room is not registered.
type RoomNotFoundError = agentpane.RoomNotFoundError

// ConnectionLimitError is returned when a room has reached its connection limit.
type ConnectionLimitError struct {
	RoomID     string
	Current    int
	MaxAllowed int
}

func (e *ConnectionLimitError) Error() string {
	return agentpane.FormatRoomLimitError(e.RoomID, "connection limit reached", e.Current, e.MaxAllowed)
}
