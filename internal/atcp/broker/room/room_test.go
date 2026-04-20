package room

import (
	"errors"
	"testing"
	"time"
)

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestManager_CreateRoomRequiresWorkspace(t *testing.T) {
	m := NewManager()
	if _, err := m.CreateRoom(CreateRoomRequest{}); !errors.Is(err, ErrWorkspaceRequired) {
		t.Fatalf("want ErrWorkspaceRequired, got %v", err)
	}
}

func TestManager_CreateAndGetRoom(t *testing.T) {
	ts := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	m := NewManager(WithClock(fixedClock(ts)))
	r, err := m.CreateRoom(CreateRoomRequest{Workspace: "ws", Title: "planning"})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if r.ID == "" {
		t.Error("room id empty")
	}
	if !r.CreatedAt.Equal(ts) {
		t.Errorf("CreatedAt = %v, want %v", r.CreatedAt, ts)
	}
	got, err := m.GetRoom(r.ID)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if got.ID != r.ID {
		t.Errorf("GetRoom id mismatch: %s != %s", got.ID, r.ID)
	}
}

func TestManager_JoinRequiresSessionAndAgent(t *testing.T) {
	m := NewManager()
	r, _ := m.CreateRoom(CreateRoomRequest{Workspace: "ws"})
	if _, err := m.JoinRoom(JoinRequest{RoomID: r.ID, AgentID: "a"}); !errors.Is(err, ErrSessionRequired) {
		t.Errorf("want ErrSessionRequired, got %v", err)
	}
	if _, err := m.JoinRoom(JoinRequest{RoomID: r.ID, SessionID: "s"}); !errors.Is(err, ErrAgentRequired) {
		t.Errorf("want ErrAgentRequired, got %v", err)
	}
}

func TestManager_JoinRejectsUnknownRoom(t *testing.T) {
	m := NewManager()
	_, err := m.JoinRoom(JoinRequest{RoomID: "nope", AgentID: "a", SessionID: "s"})
	if !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("want ErrRoomNotFound, got %v", err)
	}
}

func TestManager_JoinRejectsArchivedRoom(t *testing.T) {
	m := NewManager()
	r, _ := m.CreateRoom(CreateRoomRequest{Workspace: "ws"})
	if _, err := m.ArchiveRoom(r.ID); err != nil {
		t.Fatalf("ArchiveRoom: %v", err)
	}
	_, err := m.JoinRoom(JoinRequest{RoomID: r.ID, AgentID: "a", SessionID: "s"})
	if !errors.Is(err, ErrRoomArchived) {
		t.Fatalf("want ErrRoomArchived, got %v", err)
	}
}

// TestManager_SessionUniqueAcrossRooms locks the partial unique index
// invariant: a session can be bound to at most one active member.
func TestManager_SessionUniqueAcrossRooms(t *testing.T) {
	m := NewManager()
	r1, _ := m.CreateRoom(CreateRoomRequest{Workspace: "ws"})
	r2, _ := m.CreateRoom(CreateRoomRequest{Workspace: "ws"})
	if _, err := m.JoinRoom(JoinRequest{RoomID: r1.ID, AgentID: "a", SessionID: "shared"}); err != nil {
		t.Fatalf("first join: %v", err)
	}
	if _, err := m.JoinRoom(JoinRequest{RoomID: r2.ID, AgentID: "a2", SessionID: "shared"}); !errors.Is(err, ErrSessionAlreadyBound) {
		t.Fatalf("want ErrSessionAlreadyBound, got %v", err)
	}
	// After leave, rebinding the session elsewhere is allowed.
	if _, err := m.LeaveRoom(r1.ID, "a"); err != nil {
		t.Fatalf("LeaveRoom: %v", err)
	}
	if _, err := m.JoinRoom(JoinRequest{RoomID: r2.ID, AgentID: "a2", SessionID: "shared"}); err != nil {
		t.Fatalf("rebind after leave: %v", err)
	}
}

func TestManager_AgentCannotDoubleJoinSameRoom(t *testing.T) {
	m := NewManager()
	r, _ := m.CreateRoom(CreateRoomRequest{Workspace: "ws"})
	if _, err := m.JoinRoom(JoinRequest{RoomID: r.ID, AgentID: "a", SessionID: "s1"}); err != nil {
		t.Fatalf("first join: %v", err)
	}
	if _, err := m.JoinRoom(JoinRequest{RoomID: r.ID, AgentID: "a", SessionID: "s2"}); !errors.Is(err, ErrAgentAlreadyInRoom) {
		t.Fatalf("want ErrAgentAlreadyInRoom, got %v", err)
	}
}

func TestManager_AgentMayJoinMultipleRooms(t *testing.T) {
	m := NewManager()
	r1, _ := m.CreateRoom(CreateRoomRequest{Workspace: "ws"})
	r2, _ := m.CreateRoom(CreateRoomRequest{Workspace: "ws"})
	if _, err := m.JoinRoom(JoinRequest{RoomID: r1.ID, AgentID: "a", SessionID: "s1"}); err != nil {
		t.Fatalf("join r1: %v", err)
	}
	if _, err := m.JoinRoom(JoinRequest{RoomID: r2.ID, AgentID: "a", SessionID: "s2"}); err != nil {
		t.Fatalf("join r2 (multi-room) should be allowed: %v", err)
	}
}

func TestManager_LeaveIsSoft(t *testing.T) {
	m := NewManager()
	r, _ := m.CreateRoom(CreateRoomRequest{Workspace: "ws"})
	if _, err := m.JoinRoom(JoinRequest{RoomID: r.ID, AgentID: "a", SessionID: "s1"}); err != nil {
		t.Fatalf("join: %v", err)
	}
	if _, err := m.LeaveRoom(r.ID, "a"); err != nil {
		t.Fatalf("LeaveRoom: %v", err)
	}
	// A second leave returns ErrMemberNotFound.
	if _, err := m.LeaveRoom(r.ID, "a"); !errors.Is(err, ErrMemberNotFound) {
		t.Fatalf("second leave want ErrMemberNotFound, got %v", err)
	}
	// Member row is retained with LeftAt stamped.
	members, _ := m.Members(r.ID)
	if len(members) != 1 {
		t.Fatalf("expected 1 historical member row, got %d", len(members))
	}
	if members[0].Active() {
		t.Error("member should not be Active() after leave")
	}
	active, _ := m.ActiveMembers(r.ID)
	if len(active) != 0 {
		t.Errorf("ActiveMembers should be empty, got %d", len(active))
	}
}

// TestManager_NonCanonicalRoleMovesToCustom enforces the plan §5a.7 rule that
// unknown role strings must not be stored in Role.
func TestManager_NonCanonicalRoleMovesToCustom(t *testing.T) {
	m := NewManager()
	r, _ := m.CreateRoom(CreateRoomRequest{Workspace: "ws"})
	mem, err := m.JoinRoom(JoinRequest{RoomID: r.ID, AgentID: "a", SessionID: "s", Role: "oracle"})
	if err != nil {
		t.Fatalf("JoinRoom: %v", err)
	}
	if mem.Role != "" {
		t.Errorf("Role should be cleared for non-canonical values, got %q", mem.Role)
	}
	if mem.RoleCustom != "oracle" {
		t.Errorf("RoleCustom = %q, want oracle", mem.RoleCustom)
	}
}
