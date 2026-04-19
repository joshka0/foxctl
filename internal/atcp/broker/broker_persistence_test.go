package broker_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/broker"
	"github.com/joshka0/foxctl/internal/atcp/broker/room"
	"github.com/joshka0/foxctl/internal/atcp/broker/router"
	"github.com/joshka0/foxctl/internal/atcp/broker/session"
	"github.com/joshka0/foxctl/internal/atcp/broker/storage/sqlite"
)

// TestBroker_RoomPersistsAcrossRestart is the acceptance test for the
// persistence slice: create a room + join a member + send a message in
// broker A, Stop broker A, open broker B on the same DB, confirm the room
// is present and its now-stale member has been auto-detached. This is the
// smallest meaningful proof that plan §5a.7 persistence works end-to-end.
func TestBroker_RoomPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "atcp.db")
	ctx := context.Background()

	// --- Broker A: produce durable state. ---
	storeA, err := sqlite.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open A: %v", err)
	}
	bA, err := broker.New(broker.Options{Storage: storeA})
	if err != nil {
		t.Fatalf("New A: %v", err)
	}

	snap, err := bA.CreateSession(session.Spec{Cmd: []string{"cat"}}, session.OutputLogOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	r, err := bA.CreateRoom(room.CreateRoomRequest{Workspace: "ws", Title: "persisted"})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if _, err := bA.JoinRoom(room.JoinRequest{
		RoomID: r.ID, AgentID: "alice", SessionID: snap.ID, CanMutate: true,
	}); err != nil {
		t.Fatalf("JoinRoom: %v", err)
	}
	if _, err := bA.SendMessage(router.Message{
		RoomID: r.ID, Source: "a", Text: "PERSIST_HELLO",
	}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	// Teardown order matches the daemon's: broker first, then close the DB.
	_ = bA.DeleteSession(snap.ID)
	bA.Stop()
	if err := storeA.Close(); err != nil {
		t.Fatalf("Close A: %v", err)
	}

	// --- Broker B: reopen and inspect. ---
	storeB, err := sqlite.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open B: %v", err)
	}
	t.Cleanup(func() { _ = storeB.Close() })
	bB, err := broker.New(broker.Options{Storage: storeB})
	if err != nil {
		t.Fatalf("New B: %v", err)
	}
	t.Cleanup(func() { bB.Stop() })

	rooms := bB.Rooms().ListRooms()
	if len(rooms) != 1 || rooms[0].ID != r.ID || rooms[0].Title != "persisted" {
		t.Fatalf("room did not survive restart: %+v", rooms)
	}
	// Alice's membership row must still exist, but marked as left because
	// her session does not exist after the restart.
	members, err := bB.Rooms().Members(r.ID)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("want 1 historical member, got %d", len(members))
	}
	if members[0].AgentID != "alice" {
		t.Errorf("agent_id = %q, want alice", members[0].AgentID)
	}
	if members[0].Active() {
		t.Error("post-restart member should not be Active() (session is gone)")
	}
	if members[0].LeftAt.IsZero() {
		t.Error("post-restart member LeftAt must be stamped")
	}

	// The detach must have been persisted: close B and reopen, member is
	// still marked left (we don't re-stamp a second time).
	_ = bB.DeleteSession("never-existed") // just ensures Stop is clean below
	prevLeftAt := members[0].LeftAt
	bB.Stop()
	_ = storeB.Close()

	storeC, err := sqlite.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open C: %v", err)
	}
	t.Cleanup(func() { _ = storeC.Close() })
	bC, err := broker.New(broker.Options{Storage: storeC})
	if err != nil {
		t.Fatalf("New C: %v", err)
	}
	t.Cleanup(func() { bC.Stop() })
	membersC, _ := bC.Rooms().Members(r.ID)
	if len(membersC) != 1 {
		t.Fatalf("want 1 historical member on second restart, got %d", len(membersC))
	}
	if !membersC[0].LeftAt.Equal(prevLeftAt) {
		t.Errorf("LeftAt should be stable across restarts: got %v, want %v", membersC[0].LeftAt, prevLeftAt)
	}
}

// TestBroker_RejoinAfterRestart confirms that after a restart detaches a
// stale member, a new session from the same agent can re-join cleanly —
// the primary-key (room_id, agent_id, session_id, joined_at) discriminates
// the new row from the old one.
func TestBroker_RejoinAfterRestart(t *testing.T) {
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "atcp.db")
	ctx := context.Background()

	storeA, _ := sqlite.Open(ctx, dsn)
	bA, err := broker.New(broker.Options{Storage: storeA})
	if err != nil {
		t.Fatalf("New A: %v", err)
	}
	snap, _ := bA.CreateSession(session.Spec{Cmd: []string{"cat"}}, session.OutputLogOptions{})
	r, _ := bA.CreateRoom(room.CreateRoomRequest{Workspace: "ws"})
	_, _ = bA.JoinRoom(room.JoinRequest{RoomID: r.ID, AgentID: "alice", SessionID: snap.ID})
	_ = bA.DeleteSession(snap.ID)
	bA.Stop()
	_ = storeA.Close()

	storeB, _ := sqlite.Open(ctx, dsn)
	t.Cleanup(func() { _ = storeB.Close() })
	bB, err := broker.New(broker.Options{Storage: storeB})
	if err != nil {
		t.Fatalf("New B: %v", err)
	}
	t.Cleanup(func() { bB.Stop() })

	// A fresh session can rebind alice to the room.
	snap2, err := bB.CreateSession(session.Spec{Cmd: []string{"cat"}}, session.OutputLogOptions{})
	if err != nil {
		t.Fatalf("CreateSession B: %v", err)
	}
	t.Cleanup(func() { _ = bB.DeleteSession(snap2.ID) })
	// Sleep a beat so the new member's JoinedAt differs from the pre-restart
	// row's JoinedAt; the PK includes joined_at so same-instant joins after
	// an in-memory leave could collide in tests with a mocked clock.
	time.Sleep(2 * time.Millisecond)
	if _, err := bB.JoinRoom(room.JoinRequest{
		RoomID: r.ID, AgentID: "alice", SessionID: snap2.ID,
	}); err != nil {
		t.Fatalf("JoinRoom after restart: %v", err)
	}
}
