package broker

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/broker/lease"
	"github.com/joshka0/foxctl/internal/atcp/broker/session"
	"github.com/joshka0/foxctl/internal/atcp/intents"
)

// newBrokerT builds a Broker that allows unleased terminal intents so the
// common tests can call Submit directly without orchestrating a lease. Tests
// that specifically validate lease enforcement should construct their own
// broker with AllowUnleasedInputForTests: false.
func newBrokerT(t *testing.T) *Broker {
	t.Helper()
	b := New(Options{AllowUnleasedInputForTests: true})
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
	b := New(Options{}) // no AllowUnleasedInputForTests
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
