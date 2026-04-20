package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/client"
	"github.com/joshka0/foxctl/internal/atcp/transport/httpjson"
)

// TestDaemon_RoomsSurviveRestart is the persistence slice's headline test:
// start a daemon with DataDir, create a room via the wire, stop the
// daemon, start a second daemon on the same DataDir, and confirm the room
// is listable through the wire. This proves the end-to-end "survives
// daemon restart" promise.
func TestDaemon_RoomsSurviveRestart(t *testing.T) {
	dataDir := t.TempDir()

	// --- Run 1 ---
	d1 := New(Options{SocketPath: shortSockPath(t), DataDir: dataDir})
	if err := d1.Start(); err != nil {
		t.Fatalf("Start 1: %v", err)
	}

	c1 := client.ForSocket(d1.SocketPath())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	room, err := c1.CreateRoom(ctx, httpjson.CreateRoomRequest{
		Workspace: "ws", Title: "durable",
	})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if err := d1.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown 1: %v", err)
	}

	// --- Run 2 ---
	d2 := New(Options{SocketPath: shortSockPath(t), DataDir: dataDir})
	if err := d2.Start(); err != nil {
		t.Fatalf("Start 2: %v", err)
	}
	t.Cleanup(func() { _ = d2.Shutdown(context.Background()) })

	c2 := client.ForSocket(d2.SocketPath())
	rooms, err := c2.ListRooms(ctx)
	if err != nil {
		t.Fatalf("ListRooms: %v", err)
	}
	if len(rooms) != 1 {
		t.Fatalf("want 1 persisted room, got %d", len(rooms))
	}
	if rooms[0].ID != room.ID || rooms[0].Title != "durable" {
		t.Errorf("room mismatch after restart: %+v", rooms[0])
	}

	// Members from a pre-restart session must be marked as left.
	members, err := c2.RoomMembers(ctx, room.ID)
	if err != nil {
		t.Fatalf("RoomMembers: %v", err)
	}
	// This test didn't join anyone in run 1, so member list is empty.
	if len(members) != 0 {
		t.Errorf("expected empty members, got %d", len(members))
	}
}

// TestDaemon_DataDirCreatedIfMissing confirms the daemon materialises its
// data directory rather than erroring out when the operator points at a
// non-existent path. That's the most common fresh-install path.
func TestDaemon_DataDirCreatedIfMissing(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "sub", "atcp")
	d := New(Options{SocketPath: shortSockPath(t), DataDir: target})
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = d.Shutdown(context.Background()) })
	if !pathExists(filepath.Join(target, "atcp.db")) {
		t.Errorf("atcp.db not created under %s", target)
	}
}
