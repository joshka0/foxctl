// Package room implements ATCP's in-memory Room Manager.
//
// Rooms are the coordination scope that ties together broker-owned PTY
// sessions belonging to multiple agents. This package intentionally ships
// WITHOUT persistence — the canonical schema (atcp_rooms /
// atcp_room_members, plan §5a.7) lands in a follow-up once the wire
// protocol is proven end-to-end.
//
// Invariants (plan §5a):
//
//   - A session_id appears in at most one active member row across all rooms
//     (partial unique index).
//   - An agent_id MAY appear in multiple active rooms; callers get a warning
//     from upper layers but this package does not block it.
//   - Leave is soft: LeftAt is stamped, member rows persist. Session lifecycle
//     is "persist on leave" — the broker session stays alive.
package room

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// Role is the canonical role enum (plan §5a.7). Anything else goes to
// RoleCustom so enum changes do not require schema migrations.
type Role string

const (
	RoleCoordinator Role = "coordinator"
	RoleCoder       Role = "coder"
	RoleReviewer    Role = "reviewer"
	RoleTester      Role = "tester"
	RolePlanner     Role = "planner"
	RoleObserver    Role = "observer"
)

// IsCanonical reports whether r is one of the canonical role enum values.
func (r Role) IsCanonical() bool {
	switch r {
	case RoleCoordinator, RoleCoder, RoleReviewer, RoleTester, RolePlanner, RoleObserver:
		return true
	}
	return false
}

// Room is the durable coordination scope. Fields mirror the atcp_rooms row
// shape so adding persistence later is a straight copy.
type Room struct {
	ID          string
	Workspace   string
	Title       string
	Description string
	CreatedAt   time.Time
	ArchivedAt  time.Time // zero if not archived
}

// Member is a single agent's membership in a room, bound to exactly one
// broker-owned session. LeftAt zero means the member is still active.
type Member struct {
	RoomID     string
	AgentID    string
	SessionID  string
	InboxID    string
	Role       Role
	RoleCustom string // set iff Role.IsCanonical() is false
	CanMutate  bool   // whether the member may hold terminal.input leases
	ImportHint string
	JoinedAt   time.Time
	LeftAt     time.Time // zero if still active
}

// Active reports whether m has not left yet.
func (m Member) Active() bool { return m.LeftAt.IsZero() }

// Sentinel errors returned by Manager. Kept stable so HTTP/CLI can map them
// to canonical status codes.
var (
	ErrRoomNotFound        = errors.New("atcp room: not found")
	ErrRoomArchived        = errors.New("atcp room: archived")
	ErrMemberNotFound      = errors.New("atcp room: member not found")
	ErrSessionAlreadyBound = errors.New("atcp room: session already bound to an active member")
	ErrAgentAlreadyInRoom  = errors.New("atcp room: agent already has an active member in this room")
	ErrWorkspaceRequired   = errors.New("atcp room: workspace is required")
	ErrSessionRequired     = errors.New("atcp room: session_id is required")
	ErrAgentRequired       = errors.New("atcp room: agent_id is required")
)

// Manager holds rooms and their members in memory. It is safe for concurrent
// use; all invariants are enforced under a single mutex because room ops are
// low-volume relative to terminal IO.
type Manager struct {
	mu      sync.RWMutex
	rooms   map[string]*Room
	members map[string][]*Member // roomID -> members, active + inactive in insertion order
	// sessionToRoom is the live index that enforces "at most one active
	// member per session" without scanning every room on every join.
	sessionToRoom map[string]string
	now           func() time.Time
}

// Option tweaks Manager behavior. Tests use WithClock to freeze time; the
// default clock is time.Now.
type Option func(*Manager)

// WithClock replaces the internal clock. Intended for tests only.
func WithClock(now func() time.Time) Option {
	return func(m *Manager) { m.now = now }
}

// NewManager builds an empty Manager.
func NewManager(opts ...Option) *Manager {
	m := &Manager{
		rooms:         make(map[string]*Room),
		members:       make(map[string][]*Member),
		sessionToRoom: make(map[string]string),
		now:           time.Now,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Hydrate loads a pre-existing set of rooms + members into the manager.
// Intended for startup from a persistence layer; callers must invoke it
// before any concurrent API usage. Unlike CreateRoom/JoinRoom, this path:
//
//   - Accepts caller-supplied IDs and timestamps (no generation, no clock).
//   - Does NOT enforce the "active session unique" partial index. The
//     caller is trusted to have stamped LeftAt on any member whose session
//     no longer exists — a broker typically does this immediately after
//     Hydrate to reflect that PTYs did not survive the restart.
//   - Silently ignores members that reference unknown room IDs; that state
//     is recoverable and logging it lives above this layer.
//
// Hydrate is idempotent only in the degenerate sense: calling it twice on a
// non-empty manager will add duplicate rooms. Do not call it after API use.
func (m *Manager) Hydrate(rooms []Room, members map[string][]Member) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range rooms {
		rc := r
		m.rooms[rc.ID] = &rc
		if _, ok := m.members[rc.ID]; !ok {
			m.members[rc.ID] = nil
		}
	}
	for roomID, list := range members {
		if _, ok := m.rooms[roomID]; !ok {
			continue
		}
		for _, mem := range list {
			mc := mem
			m.members[roomID] = append(m.members[roomID], &mc)
			if mc.Active() {
				m.sessionToRoom[mc.SessionID] = roomID
			}
		}
	}
}

// StampActiveMembersLeft marks every still-active member as having left at
// the given time, writing through via onChange. Used on broker startup to
// detach members from sessions that did not survive the restart. onChange
// is invoked after each mutation so the caller can persist the new state;
// errors from onChange are collected and returned as a single joined error
// rather than aborting mid-walk.
func (m *Manager) StampActiveMembersLeft(at time.Time, onChange func(Member) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []error
	for roomID, list := range m.members {
		_ = roomID
		for _, mem := range list {
			if !mem.Active() {
				continue
			}
			mem.LeftAt = at
			delete(m.sessionToRoom, mem.SessionID)
			if onChange != nil {
				if err := onChange(*mem); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// CreateRoomRequest is the input to CreateRoom.
type CreateRoomRequest struct {
	Workspace   string
	Title       string
	Description string
}

// CreateRoom creates a fresh room and returns a copy of the stored record.
// ID is generated from a ULID so external callers need not supply one.
func (m *Manager) CreateRoom(req CreateRoomRequest) (Room, error) {
	if req.Workspace == "" {
		return Room{}, ErrWorkspaceRequired
	}
	r := &Room{
		ID:          ulid.Make().String(),
		Workspace:   req.Workspace,
		Title:       req.Title,
		Description: req.Description,
		CreatedAt:   m.now(),
	}
	m.mu.Lock()
	m.rooms[r.ID] = r
	m.members[r.ID] = nil
	m.mu.Unlock()
	return *r, nil
}

// JoinRequest binds an agent's existing broker session to a room.
//
// This slice supports only the "bind existing session" variant from plan
// §5a.3. Auto-spawn (broker-owned headless PTY creation) is composed by the
// caller: it must CreateSession first, then JoinRoom with the returned id.
type JoinRequest struct {
	RoomID     string
	AgentID    string
	SessionID  string
	InboxID    string // optional; defaults to "agent:<agent_id>" so tests don't have to thread a value
	Role       Role
	RoleCustom string
	CanMutate  bool
	ImportHint string
}

// JoinRoom inserts an active member row. It refuses:
//   - Unknown or archived rooms
//   - Re-binding a session that is already active in any room
//   - A second active row for the same (room, agent) pair
func (m *Manager) JoinRoom(req JoinRequest) (Member, error) {
	if req.SessionID == "" {
		return Member{}, ErrSessionRequired
	}
	if req.AgentID == "" {
		return Member{}, ErrAgentRequired
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.rooms[req.RoomID]
	if !ok {
		return Member{}, ErrRoomNotFound
	}
	if !r.ArchivedAt.IsZero() {
		return Member{}, fmt.Errorf("%w: %s", ErrRoomArchived, r.ID)
	}
	if other, taken := m.sessionToRoom[req.SessionID]; taken {
		return Member{}, fmt.Errorf("%w (room=%s, session=%s)", ErrSessionAlreadyBound, other, req.SessionID)
	}
	for _, existing := range m.members[req.RoomID] {
		if existing.Active() && existing.AgentID == req.AgentID {
			return Member{}, fmt.Errorf("%w (room=%s, agent=%s)", ErrAgentAlreadyInRoom, req.RoomID, req.AgentID)
		}
	}

	inbox := req.InboxID
	if inbox == "" {
		inbox = "agent:" + req.AgentID
	}
	role := req.Role
	roleCustom := req.RoleCustom
	if role != "" && !role.IsCanonical() {
		roleCustom = string(role)
		role = ""
	}
	mem := &Member{
		RoomID:     req.RoomID,
		AgentID:    req.AgentID,
		SessionID:  req.SessionID,
		InboxID:    inbox,
		Role:       role,
		RoleCustom: roleCustom,
		CanMutate:  req.CanMutate,
		ImportHint: req.ImportHint,
		JoinedAt:   m.now(),
	}
	m.members[req.RoomID] = append(m.members[req.RoomID], mem)
	m.sessionToRoom[req.SessionID] = req.RoomID
	return *mem, nil
}

// LeaveRoom soft-removes an agent's active membership. Idempotent: leaving
// an already-left agent returns ErrMemberNotFound so callers can
// distinguish "never joined" from "already gone" at the API layer.
func (m *Manager) LeaveRoom(roomID, agentID string) (Member, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rooms[roomID]; !ok {
		return Member{}, ErrRoomNotFound
	}
	for _, mem := range m.members[roomID] {
		if mem.AgentID == agentID && mem.Active() {
			mem.LeftAt = m.now()
			delete(m.sessionToRoom, mem.SessionID)
			return *mem, nil
		}
	}
	return Member{}, ErrMemberNotFound
}

// ArchiveRoom stops routing for a room but keeps its rows around. Subsequent
// JoinRoom calls fail; active members stay active until LeaveRoom.
func (m *Manager) ArchiveRoom(roomID string) (Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return Room{}, ErrRoomNotFound
	}
	if r.ArchivedAt.IsZero() {
		r.ArchivedAt = m.now()
	}
	return *r, nil
}

// GetRoom returns a copy of the room record.
func (m *Manager) GetRoom(roomID string) (Room, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return Room{}, ErrRoomNotFound
	}
	return *r, nil
}

// ListRooms returns every room, sorted by creation time ascending.
func (m *Manager) ListRooms() []Room {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Room, 0, len(m.rooms))
	for _, r := range m.rooms {
		out = append(out, *r)
	}
	// Stable order keeps CLI output deterministic.
	sortRoomsByCreated(out)
	return out
}

// Members returns a copy of every member row in a room (active and past).
// The active-only filter is a caller concern.
func (m *Manager) Members(roomID string) ([]Member, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.rooms[roomID]; !ok {
		return nil, ErrRoomNotFound
	}
	src := m.members[roomID]
	out := make([]Member, len(src))
	for i, mm := range src {
		out[i] = *mm
	}
	return out, nil
}

// ActiveMembers returns just the active rows; convenient for routers which
// only care about agents currently listening.
func (m *Manager) ActiveMembers(roomID string) ([]Member, error) {
	all, err := m.Members(roomID)
	if err != nil {
		return nil, err
	}
	active := all[:0]
	for _, mm := range all {
		if mm.Active() {
			active = append(active, mm)
		}
	}
	return active, nil
}
