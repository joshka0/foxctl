package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joshka/foxprox/foxprox/broker/session"
	"github.com/joshka/foxprox/foxprox/client"
	"github.com/joshka/foxprox/foxprox/transport/httpjson"
)

// shortSockPath returns a unique socket path under /tmp (not t.TempDir())
// because Darwin caps sun_path at 104 bytes and TempDir paths can exceed
// that.
var sockSeq atomic.Uint64

func shortSockPath(t *testing.T) string {
	t.Helper()
	n := sockSeq.Add(1)
	p := filepath.Join("/tmp", fmt.Sprintf("atcpd-test-%d-%d.sock", time.Now().UnixNano(), n))
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

// TestDaemon_RoomsOverWire is the daemon slice acceptance test: spin up the
// daemon on a Unix socket, drive it with an HTTP client as if we were a
// separate `foxctl` process, create two PTYs, join them to one room, fan
// out a message, and assert both PTY logs saw it.
func TestDaemon_RoomsOverWire(t *testing.T) {
	d := New(Options{SocketPath: shortSockPath(t)})
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = d.Shutdown(context.Background()) })

	c := client.ForSocket(d.SocketPath())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Health(ctx); err != nil {
		t.Fatalf("Health: %v", err)
	}

	s1, err := c.CreateSession(ctx, httpjson.CreateSessionRequest{Cmd: []string{"cat"}})
	if err != nil {
		t.Fatalf("CreateSession 1: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteSession(context.Background(), s1.ID) })
	s2, err := c.CreateSession(ctx, httpjson.CreateSessionRequest{Cmd: []string{"cat"}})
	if err != nil {
		t.Fatalf("CreateSession 2: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteSession(context.Background(), s2.ID) })

	room, err := c.CreateRoom(ctx, httpjson.CreateRoomRequest{Workspace: "ws", Title: "wire"})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if _, err := c.JoinRoom(ctx, room.ID, httpjson.JoinRoomRequest{
		AgentID: "alice", SessionID: s1.ID, CanMutate: true,
	}); err != nil {
		t.Fatalf("JoinRoom alice: %v", err)
	}
	if _, err := c.JoinRoom(ctx, room.ID, httpjson.JoinRoomRequest{
		AgentID: "bob", SessionID: s2.ID, CanMutate: true,
	}); err != nil {
		t.Fatalf("JoinRoom bob: %v", err)
	}

	sent, err := c.SendMessage(ctx, httpjson.SendMessageRequest{
		RoomID: room.ID, Source: "wire-test", Text: "WIRE_OK",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if sent.Delivered != 2 || sent.Failed != 0 {
		t.Fatalf("delivered=%d failed=%d want 2/0", sent.Delivered, sent.Failed)
	}

	// The HTTP surface for replaying session output is SSE; for this test we
	// peek at the in-process broker log directly. A separate-process client
	// would use GET /v1/events. Both prove the same thing.
	b := d.Broker()
	sess1, err := b.Sessions().Get(s1.ID)
	if err != nil {
		t.Fatalf("Get s1: %v", err)
	}
	sess2, err := b.Sessions().Get(s2.ID)
	if err != nil {
		t.Fatalf("Get s2: %v", err)
	}
	assertLogContains(t, sess1, "WIRE_OK", 2*time.Second)
	assertLogContains(t, sess2, "WIRE_OK", 2*time.Second)

	if _, err := c.LeaveRoom(ctx, room.ID, httpjson.LeaveRoomRequest{AgentID: "alice"}); err != nil {
		t.Fatalf("LeaveRoom alice: %v", err)
	}
	members, err := c.RoomMembers(ctx, room.ID)
	if err != nil {
		t.Fatalf("RoomMembers: %v", err)
	}
	var aliceActive, bobActive int
	for _, m := range members {
		if m.LeftAt != nil && !m.LeftAt.IsZero() {
			continue
		}
		switch m.AgentID {
		case "alice":
			aliceActive++
		case "bob":
			bobActive++
		}
	}
	if aliceActive != 0 {
		t.Errorf("alice should have left, got %d active rows", aliceActive)
	}
	if bobActive != 1 {
		t.Errorf("bob should still be active, got %d", bobActive)
	}
}

// TestDaemon_ShutdownUnlinksSocket confirms a graceful shutdown removes the
// socket file so the next startup doesn't trip over a stale entry.
func TestDaemon_ShutdownUnlinksSocket(t *testing.T) {
	path := shortSockPath(t)
	d := New(Options{SocketPath: path})
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := d.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if pathExists(path) {
		t.Errorf("socket file still present after Shutdown: %s", path)
	}
}

// TestDaemon_DoubleStartIsRejected prevents accidental re-binding of the
// same daemon value.
func TestDaemon_DoubleStartIsRejected(t *testing.T) {
	d := New(Options{SocketPath: shortSockPath(t)})
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = d.Shutdown(context.Background()) })
	if err := d.Start(); err == nil {
		t.Error("second Start should have returned an error")
	}
}

func assertLogContains(t *testing.T, sess *session.Session, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var b strings.Builder
		for _, c := range sess.Log().Since(0, 0) {
			b.Write(c.Bytes)
		}
		if strings.Contains(b.String(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("text %q never appeared in session output", want)
}
