package broker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/broker/lease"
	"github.com/joshka0/foxctl/internal/atcp/broker/room"
	"github.com/joshka0/foxctl/internal/atcp/broker/router"
	"github.com/joshka0/foxctl/internal/atcp/broker/session"
	"github.com/joshka0/foxctl/internal/atcp/intents"
)

// newBrokerT builds a Broker that allows unleased terminal intents so the
// common tests can call Submit directly without orchestrating a lease. Tests
// that specifically validate lease enforcement should construct their own
// broker with AllowUnleasedInputForTests: false.
func newBrokerT(t *testing.T) *Broker {
	t.Helper()
	b := MustNew(Options{AllowUnleasedInputForTests: true})
	t.Cleanup(func() { b.Stop() })
	return b
}

func TestBroker_CreateAndDeleteSession(t *testing.T) {
	b := newBrokerT(t)
	snap, err := b.CreateSession(session.Spec{Cmd: []string{"sleep", "30"}}, session.OutputLogOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if snap.ID == "" {
		t.Error("snapshot id empty")
	}
	if _, err := b.GetSession(snap.ID); err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if err := b.DeleteSession(snap.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := b.GetSession(snap.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("want ErrSessionNotFound, got %v", err)
	}
}

func TestBroker_SubmitWritesViaPTY(t *testing.T) {
	b := newBrokerT(t)
	snap, err := b.CreateSession(session.Spec{Cmd: []string{"cat"}}, session.OutputLogOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	n, err := b.Submit(snap.ID, intents.TerminalSubmit{Text: "hello"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if n == 0 {
		t.Fatal("Submit wrote 0 bytes")
	}
	sess, _ := b.sessions.Get(snap.ID)
	assertOutputContains(t, sess, "hello", 3*time.Second)
	// Send EOF so cat exits.
	_, _ = b.WriteBytesRaw(snap.ID, []byte{0x04})
	waitDone(t, sess, 3*time.Second)
}

// WriteBytesRaw is a test helper that bypasses the adapter's AllowWriteBytes
// gate — used here only to end the cat subprocess.
func (b *Broker) WriteBytesRaw(sessionID string, raw []byte) (int, error) {
	sess, err := b.sessions.Get(sessionID)
	if err != nil {
		return 0, err
	}
	return sess.Write(raw)
}

func TestBroker_LeaseEnforcement(t *testing.T) {
	b := newBrokerT(t)
	snap, err := b.CreateSession(session.Spec{Cmd: []string{"cat"}}, session.OutputLogOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Cleanup(func() {
		if sess, err := b.sessions.Get(snap.ID); err == nil {
			sess.Close()
			<-sess.Done()
		}
	})

	// Acquire a lease.
	l, err := b.AcquireLease(lease.AcquireRequest{
		SessionID: snap.ID, Scope: lease.ScopeTerminalInput, Owner: "test", TTL: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}

	// Missing lease id now that one is held -> ErrLeaseRequired.
	if _, err := b.Submit(snap.ID, intents.TerminalSubmit{Text: "x"}); !errors.Is(err, ErrLeaseRequired) {
		t.Fatalf("submit without lease want ErrLeaseRequired, got %v", err)
	}
	// Wrong lease id -> ErrLeaseMismatch.
	if _, err := b.Submit(snap.ID, intents.TerminalSubmit{Text: "x", LeaseID: "bogus"}); !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("submit with wrong lease want ErrLeaseMismatch, got %v", err)
	}
	// Correct lease id -> success.
	if _, err := b.Submit(snap.ID, intents.TerminalSubmit{Text: "ok", LeaseID: l.ID}); err != nil {
		t.Fatalf("submit with correct lease: %v", err)
	}
	// After release, leaseless submits accepted again.
	if err := b.ReleaseLease(l.ID); err != nil {
		t.Fatalf("ReleaseLease: %v", err)
	}
	if _, err := b.Submit(snap.ID, intents.TerminalSubmit{Text: "post-release"}); err != nil {
		t.Fatalf("submit after release: %v", err)
	}
}

func TestBroker_LeaseMismatchWhenHolderExpired(t *testing.T) {
	b := newBrokerT(t)
	snap, _ := b.CreateSession(session.Spec{Cmd: []string{"cat"}}, session.OutputLogOptions{})
	t.Cleanup(func() {
		if sess, err := b.sessions.Get(snap.ID); err == nil {
			sess.Close()
			<-sess.Done()
		}
	})

	l, err := b.AcquireLease(lease.AcquireRequest{
		SessionID: snap.ID, Scope: lease.ScopeTerminalInput, Owner: "test", TTL: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}
	<-l.Expired()
	if _, err := b.Submit(snap.ID, intents.TerminalSubmit{Text: "x", LeaseID: l.ID}); !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("want ErrLeaseMismatch after expiry, got %v", err)
	}
}

func TestBroker_AcquireLeaseUnknownSession(t *testing.T) {
	b := newBrokerT(t)
	_, err := b.AcquireLease(lease.AcquireRequest{
		SessionID: "nope", Scope: lease.ScopeTerminalInput, Owner: "test", TTL: time.Second,
	})
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
}

// TestBroker_UnleasedSubmitRejectedByDefault locks in the production
// invariant that every terminal mutation must carry a lease_id once the
// flag is off.
func TestBroker_UnleasedSubmitRejectedByDefault(t *testing.T) {
	b := MustNew(Options{}) // no AllowUnleasedInputForTests
	t.Cleanup(func() { b.Stop() })
	snap, err := b.CreateSession(session.Spec{Cmd: []string{"cat"}}, session.OutputLogOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Cleanup(func() { _ = b.DeleteSession(snap.ID) })
	if _, err := b.Submit(snap.ID, intents.TerminalSubmit{Text: "x"}); !errors.Is(err, ErrLeaseRequired) {
		t.Fatalf("unleased submit should be rejected, got %v", err)
	}
	// Acquiring a lease and submitting with its id must succeed.
	l, err := b.AcquireLease(lease.AcquireRequest{
		SessionID: snap.ID, Scope: lease.ScopeTerminalInput, Owner: "test", TTL: time.Second,
	})
	if err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}
	if _, err := b.Submit(snap.ID, intents.TerminalSubmit{Text: "x", LeaseID: l.ID}); err != nil {
		t.Fatalf("leased submit: %v", err)
	}
}

func TestBroker_SubmitUnknownSession(t *testing.T) {
	b := newBrokerT(t)
	if _, err := b.Submit("nope", intents.TerminalSubmit{Text: "x"}); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
}

func TestBroker_InvalidIntentWrapsError(t *testing.T) {
	b := newBrokerT(t)
	snap, _ := b.CreateSession(session.Spec{Cmd: []string{"cat"}}, session.OutputLogOptions{})
	t.Cleanup(func() {
		_ = b.DeleteSession(snap.ID)
	})
	// Empty TerminalText intent is invalid.
	if _, err := b.SubmitText(snap.ID, intents.TerminalText{}); !errors.Is(err, ErrIntentInvalid) {
		t.Fatalf("want ErrIntentInvalid, got %v", err)
	}
}

func TestBroker_DeleteSessionRemovesAdapter(t *testing.T) {
	b := newBrokerT(t)
	snap, _ := b.CreateSession(session.Spec{Cmd: []string{"sleep", "30"}}, session.OutputLogOptions{})
	if _, err := b.Adapter(snap.ID); err != nil {
		t.Fatalf("adapter should exist: %v", err)
	}
	if err := b.DeleteSession(snap.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := b.Adapter(snap.ID); errors.Is(err, ErrSessionNotFound) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("adapter was not cleaned up after session delete")
}

// TestBroker_RoomFanOut proves the full room vertical: create two sessions,
// join them to one room, SendMessage, both PTYs receive the text. This is
// the smallest demonstration that rooms + router + leases + sessions compose
// end-to-end through the Broker facade.
func TestBroker_RoomFanOut(t *testing.T) {
	// Use the real broker (no AllowUnleasedInputForTests) so we exercise the
	// production lease path via the router.
	b := MustNew(Options{})
	t.Cleanup(func() { b.Stop() })

	snap1, err := b.CreateSession(session.Spec{Cmd: []string{"cat"}}, session.OutputLogOptions{})
	if err != nil {
		t.Fatalf("CreateSession 1: %v", err)
	}
	snap2, err := b.CreateSession(session.Spec{Cmd: []string{"cat"}}, session.OutputLogOptions{})
	if err != nil {
		t.Fatalf("CreateSession 2: %v", err)
	}
	sess1, err := b.Sessions().Get(snap1.ID)
	if err != nil {
		t.Fatalf("Get s1: %v", err)
	}
	sess2, err := b.Sessions().Get(snap2.ID)
	if err != nil {
		t.Fatalf("Get s2: %v", err)
	}
	t.Cleanup(func() {
		_ = b.DeleteSession(snap1.ID)
		_ = b.DeleteSession(snap2.ID)
		<-sess1.Done()
		<-sess2.Done()
	})

	ctx := context.Background()
	r, err := b.CreateRoom(ctx, room.CreateRoomRequest{Workspace: "ws", Title: "fanout"})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if _, err := b.JoinRoom(ctx, room.JoinRequest{
		RoomID: r.ID, AgentID: "alice", SessionID: snap1.ID, CanMutate: true,
	}); err != nil {
		t.Fatalf("JoinRoom alice: %v", err)
	}
	if _, err := b.JoinRoom(ctx, room.JoinRequest{
		RoomID: r.ID, AgentID: "bob", SessionID: snap2.ID, CanMutate: true,
	}); err != nil {
		t.Fatalf("JoinRoom bob: %v", err)
	}

	res, err := b.SendMessage(ctx, router.Message{
		RoomID: r.ID,
		Source: "test",
		Text:   "ROOM_HELLO",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if res.Delivered != 2 || res.Failed != 0 {
		t.Fatalf("delivered=%d failed=%d want 2/0", res.Delivered, res.Failed)
	}

	// `cat` echoes input back; both sessions should now show the message.
	assertOutputContains(t, sess1, "ROOM_HELLO", 2*time.Second)
	assertOutputContains(t, sess2, "ROOM_HELLO", 2*time.Second)
}

// TestBroker_DeleteSessionDetachesRoomMember locks the P1 invariant: when a
// session exits (or is deleted) any room membership still bound to it must
// be stamped LeftAt. Without this, the Router's next fan-out would try to
// AcquireLease on a dead PTY.
func TestBroker_DeleteSessionDetachesRoomMember(t *testing.T) {
	b := MustNew(Options{})
	t.Cleanup(func() { b.Stop() })

	snap, err := b.CreateSession(session.Spec{Cmd: []string{"cat"}}, session.OutputLogOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	ctx := context.Background()
	r, err := b.CreateRoom(ctx, room.CreateRoomRequest{Workspace: "ws"})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if _, err := b.JoinRoom(ctx, room.JoinRequest{
		RoomID: r.ID, AgentID: "alice", SessionID: snap.ID, CanMutate: true,
	}); err != nil {
		t.Fatalf("JoinRoom: %v", err)
	}
	// Before delete: alice is active.
	active, _ := b.Rooms().ActiveMembers(r.ID)
	if len(active) != 1 {
		t.Fatalf("pre-delete active = %d, want 1", len(active))
	}

	if err := b.DeleteSession(snap.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	// The detach runs on the session-exit goroutine, so poll briefly.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		active, _ = b.Rooms().ActiveMembers(r.ID)
		if len(active) == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(active) != 0 {
		t.Fatalf("post-delete active = %d, want 0", len(active))
	}
	members, _ := b.Rooms().Members(r.ID)
	if len(members) != 1 || members[0].Active() || members[0].LeftAt.IsZero() {
		t.Errorf("member row not correctly stamped left: %+v", members)
	}
	// Post-detach, a send to the same room surfaces ErrNoActiveMembers
	// rather than attempting to write to the dead PTY.
	_, err = b.SendMessage(ctx, router.Message{RoomID: r.ID, Source: "s", Text: "ghost"})
	if !errors.Is(err, router.ErrNoActiveMembers) {
		t.Errorf("send after detach: want ErrNoActiveMembers, got %v", err)
	}
}

// TestBroker_NaturalExitDetachesRoomMember is the second half of the P1
// invariant: a session that exits on its own (child returns) must also
// release its room binding. Uses a short-lived `true` command that exits
// immediately.
func TestBroker_NaturalExitDetachesRoomMember(t *testing.T) {
	b := MustNew(Options{})
	t.Cleanup(func() { b.Stop() })

	// Start a session that will exit on its own when we send EOF.
	snap, err := b.CreateSession(session.Spec{Cmd: []string{"cat"}}, session.OutputLogOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	ctx := context.Background()
	r, _ := b.CreateRoom(ctx, room.CreateRoomRequest{Workspace: "ws"})
	if _, err := b.JoinRoom(ctx, room.JoinRequest{
		RoomID: r.ID, AgentID: "alice", SessionID: snap.ID, CanMutate: true,
	}); err != nil {
		t.Fatalf("JoinRoom: %v", err)
	}
	// Nudge cat to exit naturally via EOF (0x04). Bypasses lease policy —
	// we're simulating a child that terminates itself, not testing input.
	if _, err := b.WriteBytesRaw(snap.ID, []byte{0x04}); err != nil {
		t.Fatalf("WriteBytesRaw EOF: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		active, _ := b.Rooms().ActiveMembers(r.ID)
		if len(active) == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("natural session exit did not detach active member within 3s")
}

// TestBroker_JoinRoomUnknownSession maps the nested room-manager error to
// the canonical broker sentinel.
func TestBroker_JoinRoomUnknownSession(t *testing.T) {
	b := MustNew(Options{})
	t.Cleanup(func() { b.Stop() })
	ctx := context.Background()
	r, err := b.CreateRoom(ctx, room.CreateRoomRequest{Workspace: "ws"})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	_, err = b.JoinRoom(ctx, room.JoinRequest{RoomID: r.ID, AgentID: "a", SessionID: "missing"})
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
}

// --- helpers ---

func assertOutputContains(t *testing.T, s *session.Session, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var buf strings.Builder
		for _, c := range s.Log().Since(0, 0) {
			buf.Write(c.Bytes)
		}
		if strings.Contains(buf.String(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("output did not contain %q within %v", want, timeout)
}

func waitDone(t *testing.T, s *session.Session, timeout time.Duration) {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(timeout):
		s.Close()
		<-s.Done()
		t.Fatalf("session did not exit within %v", timeout)
	}
}
