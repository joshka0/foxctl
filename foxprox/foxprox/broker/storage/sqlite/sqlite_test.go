package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka/foxprox/foxprox/broker/room"
	"github.com/joshka/foxprox/foxprox/broker/storage"
)

// openTestStore returns a Store backed by a fresh on-disk SQLite file. We
// avoid :memory: here because the shared-cache dance is more fragile than
// just using t.TempDir() with a real file.
func openTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "foxprox.db")
	s, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStore_RoomRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	r := room.Room{
		ID:          "room-1",
		Workspace:   "ws",
		Title:       "title",
		Description: "desc",
		CreatedAt:   time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC),
	}
	if err := s.SaveRoom(ctx, r); err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}

	rooms, err := s.LoadRooms(ctx)
	if err != nil {
		t.Fatalf("LoadRooms: %v", err)
	}
	if len(rooms) != 1 {
		t.Fatalf("want 1 room, got %d", len(rooms))
	}
	if got := rooms[0]; got.ID != r.ID || got.Workspace != r.Workspace ||
		!got.CreatedAt.Equal(r.CreatedAt) || !got.ArchivedAt.IsZero() {
		t.Errorf("row mismatch: %+v vs %+v", got, r)
	}

	// Upsert: archive stamp.
	r.ArchivedAt = time.Date(2026, 4, 19, 11, 0, 0, 0, time.UTC)
	if err := s.SaveRoom(ctx, r); err != nil {
		t.Fatalf("SaveRoom upsert: %v", err)
	}
	rooms, _ = s.LoadRooms(ctx)
	if got := rooms[0]; !got.ArchivedAt.Equal(r.ArchivedAt) {
		t.Errorf("ArchivedAt not updated: got %v, want %v", got.ArchivedAt, r.ArchivedAt)
	}
}

func TestStore_MemberRoundTripAndLeftAtUpdate(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	if err := s.SaveRoom(ctx, room.Room{ID: "r1", Workspace: "ws", CreatedAt: base}); err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}
	mem := room.Member{
		RoomID:    "r1",
		AgentID:   "alice",
		SessionID: "s1",
		InboxID:   "agent:alice",
		Role:      room.RoleCoder,
		CanMutate: true,
		JoinedAt:  base,
	}
	if err := s.SaveMember(ctx, mem); err != nil {
		t.Fatalf("SaveMember: %v", err)
	}

	members, err := s.LoadMembers(ctx, "r1")
	if err != nil {
		t.Fatalf("LoadMembers: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("want 1 member, got %d", len(members))
	}
	if !members[0].Active() {
		t.Error("member should be Active() after Join")
	}

	// Stamp LeftAt — writes to the SAME primary key because joined_at is
	// unchanged. Proves the upsert targets the right row.
	mem.LeftAt = base.Add(time.Minute)
	if err := s.SaveMember(ctx, mem); err != nil {
		t.Fatalf("SaveMember leave: %v", err)
	}
	members, _ = s.LoadMembers(ctx, "r1")
	if len(members) != 1 {
		t.Fatalf("leave should update, not insert: got %d rows", len(members))
	}
	if members[0].Active() {
		t.Error("member should not be Active() after LeftAt stamp")
	}
}

// TestStore_PartialUniqueIndex locks in the plan §5a.7 invariant: two
// active members cannot share a session_id across the whole table.
func TestStore_PartialUniqueIndex(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	if err := s.SaveRoom(ctx, room.Room{ID: "r1", Workspace: "ws", CreatedAt: base}); err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}
	if err := s.SaveRoom(ctx, room.Room{ID: "r2", Workspace: "ws", CreatedAt: base}); err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}

	a := room.Member{RoomID: "r1", AgentID: "alice", SessionID: "shared", JoinedAt: base}
	if err := s.SaveMember(ctx, a); err != nil {
		t.Fatalf("SaveMember a: %v", err)
	}
	b := room.Member{RoomID: "r2", AgentID: "bob", SessionID: "shared", JoinedAt: base.Add(time.Second)}
	if err := s.SaveMember(ctx, b); err == nil {
		t.Fatal("expected SaveMember to fail due to active-session unique index")
	}

	// After a leaves, the same session may be reused.
	a.LeftAt = base.Add(2 * time.Second)
	if err := s.SaveMember(ctx, a); err != nil {
		t.Fatalf("SaveMember leave: %v", err)
	}
	if err := s.SaveMember(ctx, b); err != nil {
		t.Fatalf("SaveMember after leave: %v", err)
	}
}

func TestStore_MessageRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 4, 19, 13, 0, 0, 0, time.UTC)

	if err := s.SaveRoom(ctx, room.Room{ID: "r1", Workspace: "ws", CreatedAt: base}); err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}
	rec := storage.MessageRecord{
		ID:        "m1",
		RoomID:    "r1",
		Source:    "tester",
		Text:      "hi",
		Delivery:  "terminal",
		SentAt:    base,
		Delivered: 2,
		Failed:    1,
		Members: []storage.MessageDeliveryRecord{
			{AgentID: "alice", SessionID: "s1", Delivered: true},
			{AgentID: "bob", SessionID: "s2", Delivered: true},
			{AgentID: "carol", SessionID: "s3", Delivered: false, ErrText: "acquire lease: busy"},
		},
	}
	if err := s.AppendMessage(ctx, rec); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	got, err := s.LoadMessages(ctx, "r1", 0)
	if err != nil {
		t.Fatalf("LoadMessages: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("LoadMessages len = %d, want 1", len(got))
	}
	if got[0].ID != "m1" || got[0].Source != "tester" || got[0].Text != "hi" ||
		got[0].Delivered != 2 || got[0].Failed != 1 || len(got[0].Members) != 3 {
		t.Fatalf("LoadMessages record = %+v", got[0])
	}
	if got[0].Members[2].AgentID != "carol" || got[0].Members[2].ErrText == "" {
		t.Fatalf("LoadMessages deliveries = %+v", got[0].Members)
	}
	// Duplicate ID must fail (INSERT, not upsert, for audit immutability).
	if err := s.AppendMessage(ctx, rec); err == nil {
		t.Error("expected duplicate message_id to fail")
	}
}

// TestStore_PersistsAcrossOpen proves data on disk survives Close + re-Open.
// This is the smallest possible "daemon restart" fixture.
func TestStore_PersistsAcrossOpen(t *testing.T) {
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "foxprox.db")
	ctx := context.Background()

	s1, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open 1: %v", err)
	}
	base := time.Date(2026, 4, 19, 15, 0, 0, 0, time.UTC)
	_ = s1.SaveRoom(ctx, room.Room{ID: "r1", Workspace: "ws", Title: "persisted", CreatedAt: base})
	_ = s1.SaveMember(ctx, room.Member{RoomID: "r1", AgentID: "alice", SessionID: "s1", JoinedAt: base})
	_ = s1.Close()

	s2, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open 2: %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	rooms, err := s2.LoadRooms(ctx)
	if err != nil {
		t.Fatalf("LoadRooms: %v", err)
	}
	if len(rooms) != 1 || rooms[0].Title != "persisted" {
		t.Fatalf("room did not survive reopen: %+v", rooms)
	}
	members, _ := s2.LoadMembers(ctx, "r1")
	if len(members) != 1 || members[0].AgentID != "alice" {
		t.Fatalf("member did not survive reopen: %+v", members)
	}
}
