package lease

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func validRequest() AcquireRequest {
	return AcquireRequest{
		SessionID: "sess-1",
		Scope:     ScopeTerminalInput,
		Owner:     "router",
		TTL:       500 * time.Millisecond,
	}
}

func TestAcquire_Validation(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*AcquireRequest)
		wantErr error
	}{
		{"missing session", func(r *AcquireRequest) { r.SessionID = "" }, ErrInvalidSession},
		{"missing scope", func(r *AcquireRequest) { r.Scope = "" }, ErrInvalidScope},
		{"missing owner", func(r *AcquireRequest) { r.Owner = "" }, ErrInvalidOwner},
		{"zero ttl", func(r *AcquireRequest) { r.TTL = 0 }, ErrInvalidTTL},
		{"negative ttl", func(r *AcquireRequest) { r.TTL = -1 }, ErrInvalidTTL},
	}
	m := NewManager()
	defer m.Stop()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r := validRequest()
			tc.mutate(&r)
			_, err := m.Acquire(r)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestAcquire_SucceedsOnFreeScope(t *testing.T) {
	m := NewManager()
	defer m.Stop()
	l, err := m.Acquire(validRequest())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if l.ID == "" {
		t.Error("Lease.ID empty")
	}
	if l.Reason() != ReasonActive {
		t.Errorf("Reason = %s, want active", l.Reason())
	}
	held, ok := m.Held("sess-1", ScopeTerminalInput)
	if !ok || held.ID != l.ID {
		t.Errorf("Held returned %v %v, want lease %s", held, ok, l.ID)
	}
}

func TestAcquire_RejectsWhenHeld(t *testing.T) {
	m := NewManager()
	defer m.Stop()
	_, err := m.Acquire(validRequest())
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	_, err = m.Acquire(validRequest())
	if !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("second Acquire want ErrLeaseHeld, got %v", err)
	}
}

func TestAcquire_PreemptReplacesHolder(t *testing.T) {
	m := NewManager()
	defer m.Stop()
	first, err := m.Acquire(validRequest())
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	req := validRequest()
	req.Owner = "reminder"
	req.Preempt = true
	second, err := m.Acquire(req)
	if err != nil {
		t.Fatalf("preempt: %v", err)
	}
	if second.ID == first.ID {
		t.Error("preempt should mint a new lease id")
	}

	select {
	case <-first.Expired():
	case <-time.After(time.Second):
		t.Fatal("first lease was not finalised on preempt")
	}
	if first.Reason() != ReasonPreempted {
		t.Errorf("first.Reason = %s, want preempted", first.Reason())
	}
	if first.ReleasedAt().IsZero() {
		t.Error("ReleasedAt must be set after preempt")
	}
	if _, ok := m.Get(first.ID); ok {
		t.Error("manager still tracks preempted lease")
	}
	if held, ok := m.Held("sess-1", ScopeTerminalInput); !ok || held.ID != second.ID {
		t.Errorf("Held = %v,%v, want second lease", held, ok)
	}
}

func TestRelease_ClearsLease(t *testing.T) {
	m := NewManager()
	defer m.Stop()
	l, err := m.Acquire(validRequest())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := m.Release(l.ID); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, ok := m.Held("sess-1", ScopeTerminalInput); ok {
		t.Error("scope should be free after Release")
	}
	if l.Reason() != ReasonReleased {
		t.Errorf("Reason = %s, want released", l.Reason())
	}
	select {
	case <-l.Expired():
	default:
		t.Error("Expired channel should be closed after Release")
	}
}

func TestRelease_Unknown(t *testing.T) {
	m := NewManager()
	defer m.Stop()
	if err := m.Release("nope"); !errors.Is(err, ErrUnknownLease) {
		t.Fatalf("want ErrUnknownLease, got %v", err)
	}
}

func TestTTLExpiry_FinalisesLease(t *testing.T) {
	m := NewManager()
	defer m.Stop()
	req := validRequest()
	req.TTL = 40 * time.Millisecond
	l, err := m.Acquire(req)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	select {
	case <-l.Expired():
	case <-time.After(time.Second):
		t.Fatal("lease did not expire within 1s")
	}
	if l.Reason() != ReasonExpired {
		t.Errorf("Reason = %s, want expired", l.Reason())
	}
	if _, ok := m.Held("sess-1", ScopeTerminalInput); ok {
		t.Error("scope should be free after TTL expiry")
	}
}

func TestReleaseAfterExpiry_ReturnsUnknown(t *testing.T) {
	m := NewManager()
	defer m.Stop()
	req := validRequest()
	req.TTL = 20 * time.Millisecond
	l, _ := m.Acquire(req)
	<-l.Expired()
	if err := m.Release(l.ID); !errors.Is(err, ErrUnknownLease) {
		t.Fatalf("want ErrUnknownLease, got %v", err)
	}
}

func TestAcquire_AfterReleaseFreesScope(t *testing.T) {
	m := NewManager()
	defer m.Stop()
	first, _ := m.Acquire(validRequest())
	_ = m.Release(first.ID)
	second, err := m.Acquire(validRequest())
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if second.ID == first.ID {
		t.Error("new lease should have new id")
	}
}

func TestStop_TerminatesAllLeases(t *testing.T) {
	m := NewManager()
	a, _ := m.Acquire(validRequest())
	bReq := validRequest()
	bReq.SessionID = "sess-2"
	b, _ := m.Acquire(bReq)

	m.Stop()

	for _, l := range []*Lease{a, b} {
		select {
		case <-l.Expired():
		case <-time.After(time.Second):
			t.Fatalf("lease %s not finalised by Stop", l.ID)
		}
		if l.Reason() != ReasonManagerStopped {
			t.Errorf("Reason = %s, want manager_stopped", l.Reason())
		}
	}

	if _, err := m.Acquire(validRequest()); !errors.Is(err, ErrManagerStopped) {
		t.Fatalf("Acquire after Stop want ErrManagerStopped, got %v", err)
	}
}

func TestList_ReturnsActiveLeases(t *testing.T) {
	m := NewManager()
	defer m.Stop()
	_, _ = m.Acquire(validRequest())
	r := validRequest()
	r.SessionID = "sess-2"
	_, _ = m.Acquire(r)

	list := m.List()
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}
}

func TestConcurrentAcquire_OnlyOneWins(t *testing.T) {
	m := NewManager()
	defer m.Stop()

	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	var (
		success int
		mu      sync.Mutex
	)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := m.Acquire(validRequest()); err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("want exactly 1 success, got %d", success)
	}
}

func TestReleaseReasonString(t *testing.T) {
	cases := map[ReleaseReason]string{
		ReasonActive:         "active",
		ReasonReleased:       "released",
		ReasonExpired:        "expired",
		ReasonPreempted:      "preempted",
		ReasonManagerStopped: "manager_stopped",
		ReleaseReason(999):   "unknown",
	}
	for r, want := range cases {
		if got := r.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", r, got, want)
		}
	}
}

func TestMultipleScopesIndependentOnSameSession(t *testing.T) {
	m := NewManager()
	defer m.Stop()
	a, err := m.Acquire(AcquireRequest{SessionID: "s1", Scope: "a", Owner: "o1", TTL: time.Second})
	if err != nil {
		t.Fatalf("acquire a: %v", err)
	}
	b, err := m.Acquire(AcquireRequest{SessionID: "s1", Scope: "b", Owner: "o2", TTL: time.Second})
	if err != nil {
		t.Fatalf("acquire b: %v", err)
	}
	if a.ID == b.ID {
		t.Error("different scopes should mint different leases")
	}
	if len(m.List()) != 2 {
		t.Errorf("expected 2 active leases, got %d", len(m.List()))
	}
}
