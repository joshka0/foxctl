package router

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joshka/foxprox/foxprox/broker/lease"
	"github.com/joshka/foxprox/foxprox/broker/room"
	"github.com/joshka/foxprox/foxprox/intents"
)

// fakeDispatcher records what the router asks it to do. It can be configured
// to fail specific sessions' Submit so fan-out resilience is observable.
type fakeDispatcher struct {
	mu         sync.Mutex
	acquired   []lease.AcquireRequest
	released   []string
	submitted  []submitCall
	keyed      []keyCall
	pasted     []pasteCall
	submitErr  map[string]error // sessionID -> error
	acquireErr map[string]error // sessionID -> error
	ready      map[string]bool
	nextLease  int
}

type submitCall struct {
	sessionID string
	intent    intents.TerminalSubmit
}

type pasteCall struct {
	sessionID string
	intent    intents.TerminalPaste
}

type keyCall struct {
	sessionID string
	intent    intents.TerminalKey
}

func newFakeDispatcher() *fakeDispatcher {
	return &fakeDispatcher{submitErr: map[string]error{}, acquireErr: map[string]error{}, ready: map[string]bool{}}
}

func (f *fakeDispatcher) AcquireLease(req lease.AcquireRequest) (*lease.Lease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.acquireErr[req.SessionID]; ok {
		return nil, err
	}
	f.nextLease++
	f.acquired = append(f.acquired, req)
	return &lease.Lease{ID: fmt.Sprintf("L%d", f.nextLease), SessionID: req.SessionID, Scope: req.Scope, Owner: req.Owner}, nil
}

func (f *fakeDispatcher) ReleaseLease(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, id)
	return nil
}

func (f *fakeDispatcher) SessionReady(sessionID string) (bool, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ready, ok := f.ready[sessionID]
	if !ok {
		return true, "ready", nil
	}
	if ready {
		return true, "ready", nil
	}
	return false, "output_busy", nil
}

func (f *fakeDispatcher) SubmitKey(sessionID string, intent intents.TerminalKey) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keyed = append(f.keyed, keyCall{sessionID: sessionID, intent: intent})
	return 1, nil
}

func (f *fakeDispatcher) Submit(sessionID string, intent intents.TerminalSubmit) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.submitErr[sessionID]; ok {
		return 0, err
	}
	f.submitted = append(f.submitted, submitCall{sessionID: sessionID, intent: intent})
	return len(intent.Text), nil
}

func (f *fakeDispatcher) Paste(sessionID string, intent intents.TerminalPaste) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pasted = append(f.pasted, pasteCall{sessionID: sessionID, intent: intent})
	return len(intent.Text), nil
}

func (f *fakeDispatcher) snapshot() ([]lease.AcquireRequest, []string, []submitCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]lease.AcquireRequest(nil), f.acquired...),
		append([]string(nil), f.released...),
		append([]submitCall(nil), f.submitted...)
}

func (f *fakeDispatcher) pastedSnapshot() []pasteCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]pasteCall(nil), f.pasted...)
}

func (f *fakeDispatcher) keyedSnapshot() []keyCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]keyCall(nil), f.keyed...)
}

func roomWith(t *testing.T, members int) (*room.Manager, string, []room.Member) {
	t.Helper()
	m := room.NewManager()
	r, err := m.CreateRoom(room.CreateRoomRequest{Workspace: "ws"})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	out := make([]room.Member, 0, members)
	for i := 0; i < members; i++ {
		mem, err := m.JoinRoom(room.JoinRequest{
			RoomID:    r.ID,
			AgentID:   fmt.Sprintf("a%d", i),
			SessionID: fmt.Sprintf("s%d", i),
			CanMutate: true,
		})
		if err != nil {
			t.Fatalf("JoinRoom: %v", err)
		}
		out = append(out, mem)
	}
	return m, r.ID, out
}

func TestRouter_SendFansOutToEveryMember(t *testing.T) {
	m, roomID, members := roomWith(t, 3)
	d := newFakeDispatcher()
	r := New(d, m, Options{LeaseTTL: 500 * time.Millisecond})

	res, err := r.Send(Message{RoomID: roomID, Text: "hello"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Delivered != 3 || res.Failed != 0 {
		t.Fatalf("delivered=%d failed=%d want 3/0", res.Delivered, res.Failed)
	}

	acquired, released, submitted := d.snapshot()
	if len(acquired) != 3 || len(released) != 3 || len(submitted) != 3 {
		t.Fatalf("acquired=%d released=%d submitted=%d, want 3 each", len(acquired), len(released), len(submitted))
	}
	// Every submit MUST carry a lease_id the router acquired for it.
	for i, s := range submitted {
		if s.intent.LeaseID == "" {
			t.Errorf("submit[%d] missing lease_id", i)
		}
		for _, want := range []string{"[Foxprox receipt]", `"room_id":"` + roomID + `"`, `"reply_prefix_hint":"<AT>room:` + roomID + ` "`, "\n\nhello"} {
			if !strings.Contains(s.intent.Text, want) {
				t.Errorf("submit[%d] text missing %q: %q", i, want, s.intent.Text)
			}
		}
		if strings.Contains(s.intent.Text, "@room:") {
			t.Errorf("submit[%d] terminal receipt contains literal @room trigger: %q", i, s.intent.Text)
		}
	}
	if res.Receipt.RoomID != roomID || res.Receipt.MessageID != res.MessageID || res.Receipt.ReplyPrefix != "@room:"+roomID+" " {
		t.Fatalf("receipt = %+v, message_id=%s", res.Receipt, res.MessageID)
	}
	_ = members
}

func TestRouter_ReceiptCanBeHiddenForTerminalDelivery(t *testing.T) {
	m, roomID, _ := roomWith(t, 1)
	d := newFakeDispatcher()
	r := New(d, m, Options{LeaseTTL: 500 * time.Millisecond})

	if _, err := r.Send(Message{RoomID: roomID, Text: "hello", ReceiptVisibility: ReceiptHidden}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	_, _, submitted := d.snapshot()
	if len(submitted) != 1 {
		t.Fatalf("submitted = %d, want 1", len(submitted))
	}
	if submitted[0].intent.Text != "hello" {
		t.Fatalf("hidden receipt text = %q, want hello", submitted[0].intent.Text)
	}
}

func TestRouter_SendPropagatesSubmitKeyOverride(t *testing.T) {
	m, roomID, _ := roomWith(t, 1)
	d := newFakeDispatcher()
	r := New(d, m, Options{LeaseTTL: 500 * time.Millisecond})

	if _, err := r.Send(Message{RoomID: roomID, Text: "hello", SubmitKey: "KittyEnter"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	_, _, submitted := d.snapshot()
	if len(submitted) != 1 {
		t.Fatalf("submitted = %d, want 1", len(submitted))
	}
	if submitted[0].intent.SubmitKey != "KittyEnter" {
		t.Fatalf("SubmitKey = %q, want KittyEnter", submitted[0].intent.SubmitKey)
	}
}

func TestRouter_LargeMessageUsesPaste(t *testing.T) {
	m, roomID, _ := roomWith(t, 1)
	d := newFakeDispatcher()
	r := New(d, m, Options{LeaseTTL: 500 * time.Millisecond, LargePasteThreshold: 4})

	if _, err := r.Send(Message{RoomID: roomID, Text: "large"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	_, _, submitted := d.snapshot()
	if len(submitted) != 0 {
		t.Fatalf("submitted = %d, want 0 for large paste", len(submitted))
	}
	pasted := d.pastedSnapshot()
	if len(pasted) != 1 {
		t.Fatalf("pasted = %d, want 1", len(pasted))
	}
	if !pasted[0].intent.SubmitAfter || pasted[0].intent.Bracketed != "auto" || pasted[0].intent.LeaseID == "" {
		t.Fatalf("paste intent = %+v", pasted[0].intent)
	}
}

func TestRouter_SafePromptOnlyRejectsBusySession(t *testing.T) {
	m, roomID, _ := roomWith(t, 1)
	d := newFakeDispatcher()
	d.ready["s0"] = false
	r := New(d, m, Options{})

	res, err := r.Send(Message{RoomID: roomID, Text: "hello", TerminalPolicy: "safe-prompt-only"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Delivered != 0 || res.Failed != 1 {
		t.Fatalf("delivered=%d failed=%d, want 0/1", res.Delivered, res.Failed)
	}
	if !errors.Is(res.Members[0].Err, ErrUnsafePrompt) {
		t.Fatalf("member err = %v, want ErrUnsafePrompt", res.Members[0].Err)
	}
	_, _, submitted := d.snapshot()
	if len(submitted) != 0 {
		t.Fatalf("submitted = %+v, want none", submitted)
	}
}

func TestRouter_InterruptSendsEscapeBeforeDelivery(t *testing.T) {
	m, roomID, _ := roomWith(t, 1)
	d := newFakeDispatcher()
	r := New(d, m, Options{})

	res, err := r.Send(Message{RoomID: roomID, Text: "hello", TerminalPolicy: "interrupt"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Delivered != 1 || res.Failed != 0 {
		t.Fatalf("delivered=%d failed=%d, want 1/0", res.Delivered, res.Failed)
	}
	keys := d.keyedSnapshot()
	if len(keys) != 1 || keys[0].intent.Key != "Escape" {
		t.Fatalf("keys = %+v, want Escape", keys)
	}
	_, _, submitted := d.snapshot()
	if len(submitted) != 1 {
		t.Fatalf("submitted = %+v, want message submit", submitted)
	}
}

func TestRouter_PartialFailureDoesNotAbortFanOut(t *testing.T) {
	m, roomID, members := roomWith(t, 3)
	d := newFakeDispatcher()
	// Fail the middle member's submit.
	d.submitErr[members[1].SessionID] = errors.New("submit boom")
	r := New(d, m, Options{LeaseTTL: 500 * time.Millisecond})

	res, err := r.Send(Message{RoomID: roomID, Text: "hello"})
	if err != nil {
		t.Fatalf("Send should not return an error for partial failure, got %v", err)
	}
	if res.Delivered != 2 || res.Failed != 1 {
		t.Fatalf("delivered=%d failed=%d, want 2/1", res.Delivered, res.Failed)
	}
	// Per-member Err should be populated on the failed one.
	var sawErr bool
	for _, mr := range res.Members {
		if mr.AgentID == members[1].AgentID {
			if mr.Delivered {
				t.Errorf("member %s should not be Delivered", mr.AgentID)
			}
			if mr.Err == nil {
				t.Errorf("member %s should have Err set", mr.AgentID)
			}
			sawErr = true
		}
	}
	if !sawErr {
		t.Error("failed member not reported in Result.Members")
	}
	// Even the failing member's lease is released.
	_, released, _ := d.snapshot()
	if len(released) != 3 {
		t.Errorf("lease not released after submit failure: released=%d want 3", len(released))
	}
}

func TestRouter_LeaseAcquireFailureReported(t *testing.T) {
	m, roomID, members := roomWith(t, 2)
	d := newFakeDispatcher()
	d.acquireErr[members[0].SessionID] = errors.New("lease busy")
	r := New(d, m, Options{})

	res, err := r.Send(Message{RoomID: roomID, Text: "hi"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Delivered != 1 || res.Failed != 1 {
		t.Fatalf("delivered=%d failed=%d, want 1/1", res.Delivered, res.Failed)
	}
}

func TestRouter_SkipAgentsSuppressesSender(t *testing.T) {
	m, roomID, members := roomWith(t, 3)
	d := newFakeDispatcher()
	r := New(d, m, Options{})

	res, err := r.Send(Message{
		RoomID:     roomID,
		Text:       "self",
		Source:     members[0].AgentID,
		SkipAgents: []string{members[0].AgentID},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Delivered != 2 {
		t.Fatalf("delivered=%d, want 2 (sender suppressed)", res.Delivered)
	}
	_, _, submitted := d.snapshot()
	for _, s := range submitted {
		if s.sessionID == members[0].SessionID {
			t.Fatalf("sender's session %s should have been skipped", s.sessionID)
		}
	}
}

func TestRouter_EmptyRoomReturnsErrNoActiveMembers(t *testing.T) {
	m := room.NewManager()
	r, _ := m.CreateRoom(room.CreateRoomRequest{Workspace: "ws"})
	d := newFakeDispatcher()
	rr := New(d, m, Options{})
	_, err := rr.Send(Message{RoomID: r.ID, Text: "x"})
	if !errors.Is(err, ErrNoActiveMembers) {
		t.Fatalf("want ErrNoActiveMembers, got %v", err)
	}
}

func TestRouter_UnsupportedDeliveryRejected(t *testing.T) {
	m, roomID, _ := roomWith(t, 1)
	d := newFakeDispatcher()
	r := New(d, m, Options{})
	_, err := r.Send(Message{RoomID: roomID, Text: "x", Delivery: "inbox"})
	if !errors.Is(err, ErrUnsupportedDelivery) {
		t.Fatalf("want ErrUnsupportedDelivery, got %v", err)
	}
}

// TestRouter_NonMutableMembersSkipped locks in the P0 authority boundary:
// active members with CanMutate=false MUST NOT receive PTY writes even
// though they appear in ActiveMembers. The dispatcher should see exactly
// the mutable member's session ID and no lease for the observer.
func TestRouter_NonMutableMembersSkipped(t *testing.T) {
	m := room.NewManager()
	rm, _ := m.CreateRoom(room.CreateRoomRequest{Workspace: "ws"})
	writer, err := m.JoinRoom(room.JoinRequest{
		RoomID: rm.ID, AgentID: "writer", SessionID: "sw", CanMutate: true,
	})
	if err != nil {
		t.Fatalf("JoinRoom writer: %v", err)
	}
	observer, err := m.JoinRoom(room.JoinRequest{
		RoomID: rm.ID, AgentID: "observer", SessionID: "so", CanMutate: false,
	})
	if err != nil {
		t.Fatalf("JoinRoom observer: %v", err)
	}
	d := newFakeDispatcher()
	r := New(d, m, Options{})

	res, err := r.Send(Message{RoomID: rm.ID, Text: "hello"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Delivered != 1 || res.Failed != 0 {
		t.Fatalf("delivered=%d failed=%d, want 1/0 (observer not counted)", res.Delivered, res.Failed)
	}
	acquired, _, submitted := d.snapshot()
	if len(acquired) != 1 || acquired[0].SessionID != writer.SessionID {
		t.Errorf("leases = %+v; want single acquire on writer session %s", acquired, writer.SessionID)
	}
	for _, s := range submitted {
		if s.sessionID == observer.SessionID {
			t.Fatalf("observer session %s must NOT receive Submit", observer.SessionID)
		}
	}
	// Result.Members reflects the dispatched targets only; observers are
	// excluded from fan-out accounting so clients don't see a phantom
	// "undelivered" for something that was never a candidate.
	for _, mr := range res.Members {
		if mr.AgentID == observer.AgentID {
			t.Error("observer should not appear in Result.Members")
		}
	}
}

// TestRouter_AllObserversReturnsErrNoMutableMembers proves the distinct
// error shape — a populated but purely read-only room is a config problem
// and should surface differently from "empty room".
func TestRouter_AllObserversReturnsErrNoMutableMembers(t *testing.T) {
	m := room.NewManager()
	rm, _ := m.CreateRoom(room.CreateRoomRequest{Workspace: "ws"})
	if _, err := m.JoinRoom(room.JoinRequest{
		RoomID: rm.ID, AgentID: "o1", SessionID: "s1", CanMutate: false,
	}); err != nil {
		t.Fatalf("JoinRoom o1: %v", err)
	}
	if _, err := m.JoinRoom(room.JoinRequest{
		RoomID: rm.ID, AgentID: "o2", SessionID: "s2", CanMutate: false,
	}); err != nil {
		t.Fatalf("JoinRoom o2: %v", err)
	}
	d := newFakeDispatcher()
	r := New(d, m, Options{})
	_, err := r.Send(Message{RoomID: rm.ID, Text: "x"})
	if !errors.Is(err, ErrNoMutableMembers) {
		t.Fatalf("want ErrNoMutableMembers, got %v", err)
	}
	// Never acquired a lease for an observer.
	acquired, _, submitted := d.snapshot()
	if len(acquired) != 0 || len(submitted) != 0 {
		t.Errorf("no leases/submits should have happened: acquired=%d submitted=%d", len(acquired), len(submitted))
	}
}

// TestRouter_SkipFiltersMutableMembersYieldsActive confirms that when the
// sender skips the one mutable member, the fallback error is
// ErrNoActiveMembers (not ErrNoMutableMembers): the config is fine, the
// caller just excluded the only eligible recipient.
func TestRouter_SkipFiltersMutableMembersYieldsActive(t *testing.T) {
	m := room.NewManager()
	rm, _ := m.CreateRoom(room.CreateRoomRequest{Workspace: "ws"})
	mem, err := m.JoinRoom(room.JoinRequest{
		RoomID: rm.ID, AgentID: "writer", SessionID: "sw", CanMutate: true,
	})
	if err != nil {
		t.Fatalf("JoinRoom: %v", err)
	}
	d := newFakeDispatcher()
	r := New(d, m, Options{})
	_, err = r.Send(Message{
		RoomID:     rm.ID,
		Text:       "self",
		SkipAgents: []string{mem.AgentID},
	})
	if !errors.Is(err, ErrNoActiveMembers) {
		t.Fatalf("want ErrNoActiveMembers, got %v", err)
	}
}

func TestRouter_RequiredFields(t *testing.T) {
	m, roomID, _ := roomWith(t, 1)
	d := newFakeDispatcher()
	r := New(d, m, Options{})
	if _, err := r.Send(Message{Text: "x"}); !errors.Is(err, ErrRoomIDRequired) {
		t.Errorf("want ErrRoomIDRequired, got %v", err)
	}
	if _, err := r.Send(Message{RoomID: roomID}); !errors.Is(err, ErrEmptyMessage) {
		t.Errorf("want ErrEmptyMessage, got %v", err)
	}
}
