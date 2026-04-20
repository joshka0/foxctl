// Package lease implements per-session input leases for ATCP.
//
// A lease serializes access to a scoped capability on a session (typically
// "terminal.input") between concurrent producers — the CLI, the room router,
// reminder scheduler, transaction runner, and viewer-originated keystrokes.
// Only the current holder of the lease may mutate the scope; everyone else
// must queue, preempt, or back off.
//
// The manager keeps state in memory: leases are ephemeral by design (spec §11)
// and do not outlive a broker restart. Callers that need durable authorization
// should layer their own policy on top.
package lease

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// Scope is a well-known lease scope string. The manager treats scopes as
// opaque tags — only equality matters — but centralising canonical names
// keeps router and transaction code honest.
type Scope string

const (
	// ScopeTerminalInput serialises any producer of terminal.{text,key,submit,paste,write_bytes}.
	ScopeTerminalInput Scope = "terminal.input"
)

// AcquireRequest describes an acquire attempt.
type AcquireRequest struct {
	SessionID string
	Scope     Scope
	Owner     string
	// TTL bounds how long the lease survives without explicit release. Must be
	// positive; exceeding TTL auto-releases the lease and closes Expired().
	TTL time.Duration
	// Preempt, when true, forcibly takes the scope away from the current holder
	// if one exists. The old lease's Expired() channel is closed with
	// ReasonPreempted.
	Preempt bool
}

// ReleaseReason classifies how a lease terminated. Consumers check this via
// Lease.Reason() after Expired() fires.
type ReleaseReason int

const (
	// ReasonActive indicates the lease has not yet terminated.
	ReasonActive ReleaseReason = iota
	// ReasonReleased means the holder explicitly released the lease.
	ReasonReleased
	// ReasonExpired means TTL elapsed before release.
	ReasonExpired
	// ReasonPreempted means another acquire with Preempt=true replaced it.
	ReasonPreempted
	// ReasonManagerStopped means the manager was shut down.
	ReasonManagerStopped
)

// String returns a stable lowercase name usable in JSON/metrics.
func (r ReleaseReason) String() string {
	switch r {
	case ReasonActive:
		return "active"
	case ReasonReleased:
		return "released"
	case ReasonExpired:
		return "expired"
	case ReasonPreempted:
		return "preempted"
	case ReasonManagerStopped:
		return "manager_stopped"
	}
	return "unknown"
}

// Lease is a granted acquire. All fields are read-only after construction;
// callers must consult Expired() / Reason() rather than mutating.
type Lease struct {
	ID         string
	SessionID  string
	Scope      Scope
	Owner      string
	AcquiredAt time.Time
	TTL        time.Duration

	mu        sync.Mutex
	released  time.Time
	reason    ReleaseReason
	expiredCh chan struct{}
	timer     *time.Timer
}

// Expired returns a channel closed when the lease terminates for any reason.
func (l *Lease) Expired() <-chan struct{} { return l.expiredCh }

// Reason returns the current termination classification. While the lease is
// active it returns ReasonActive; after Expired() fires it returns the final
// cause.
func (l *Lease) Reason() ReleaseReason {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reason
}

// ReleasedAt returns the wall-clock time at which the lease terminated, or
// the zero time while active.
func (l *Lease) ReleasedAt() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.released
}

// finalize transitions the lease to a terminated state. Idempotent: the first
// caller wins and subsequent calls are no-ops.
func (l *Lease) finalize(reason ReleaseReason) bool {
	l.mu.Lock()
	if l.reason != ReasonActive {
		l.mu.Unlock()
		return false
	}
	l.reason = reason
	l.released = time.Now().UTC()
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
	ch := l.expiredCh
	l.mu.Unlock()
	close(ch)
	return true
}

// Errors returned by Manager methods.
var (
	ErrInvalidSession = errors.New("atcp lease: SessionID is required")
	ErrInvalidScope   = errors.New("atcp lease: Scope is required")
	ErrInvalidOwner   = errors.New("atcp lease: Owner is required")
	ErrInvalidTTL     = errors.New("atcp lease: TTL must be positive")
	ErrLeaseHeld      = errors.New("atcp lease: scope is already held (use Preempt to override)")
	ErrUnknownLease   = errors.New("atcp lease: unknown lease id")
	ErrManagerStopped = errors.New("atcp lease: manager is stopped")
)

// Manager owns the active set of leases.
//
// Safe for concurrent use. All mutation paths take m.mu; TTL expiry fires on
// per-lease goroutines (time.AfterFunc) that re-acquire the lock when they
// run. The lock is never held across a callback into user code.
type Manager struct {
	mu      sync.Mutex
	byKey   map[key]*Lease
	byID    map[string]*Lease
	stopped bool
}

type key struct {
	sessionID string
	scope     Scope
}

// NewManager constructs an empty Manager.
func NewManager() *Manager {
	return &Manager{
		byKey: make(map[key]*Lease),
		byID:  make(map[string]*Lease),
	}
}

// Acquire grants a new lease for req.Scope on req.SessionID.
//
// If the scope is already held and Preempt is false, returns ErrLeaseHeld.
// If the scope is held and Preempt is true, the current holder is finalised
// with ReasonPreempted before the new lease is issued.
func (m *Manager) Acquire(req AcquireRequest) (*Lease, error) {
	if err := validateAcquire(req); err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return nil, ErrManagerStopped
	}
	k := key{req.SessionID, req.Scope}
	existing, held := m.byKey[k]
	if held {
		if !req.Preempt {
			m.mu.Unlock()
			return nil, fmt.Errorf("%w: sessionID=%s scope=%s owner=%s", ErrLeaseHeld, req.SessionID, req.Scope, existing.Owner)
		}
		delete(m.byKey, k)
		delete(m.byID, existing.ID)
	}

	l := &Lease{
		ID:         ulid.Make().String(),
		SessionID:  req.SessionID,
		Scope:      req.Scope,
		Owner:      req.Owner,
		AcquiredAt: time.Now().UTC(),
		TTL:        req.TTL,
		reason:     ReasonActive,
		expiredCh:  make(chan struct{}),
	}
	l.timer = time.AfterFunc(req.TTL, func() { m.expire(l) })
	m.byKey[k] = l
	m.byID[l.ID] = l
	m.mu.Unlock()

	if existing != nil {
		existing.finalize(ReasonPreempted)
	}
	return l, nil
}

// Release terminates the lease with id. Returns ErrUnknownLease when no lease
// with that id is active.
func (m *Manager) Release(id string) error {
	m.mu.Lock()
	l, ok := m.byID[id]
	if !ok {
		m.mu.Unlock()
		return ErrUnknownLease
	}
	delete(m.byID, id)
	delete(m.byKey, key{l.SessionID, l.Scope})
	m.mu.Unlock()
	l.finalize(ReasonReleased)
	return nil
}

// Held reports the current holder (if any) of scope on sessionID.
func (m *Manager) Held(sessionID string, scope Scope) (*Lease, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.byKey[key{sessionID, scope}]
	return l, ok
}

// Get looks up a lease by id.
func (m *Manager) Get(id string) (*Lease, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.byID[id]
	return l, ok
}

// List returns a snapshot slice of all currently-held leases.
func (m *Manager) List() []*Lease {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Lease, 0, len(m.byID))
	for _, l := range m.byID {
		out = append(out, l)
	}
	return out
}

// Stop terminates all leases with ReasonManagerStopped and prevents further
// acquires. Idempotent.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	leases := make([]*Lease, 0, len(m.byID))
	for _, l := range m.byID {
		leases = append(leases, l)
	}
	m.byID = map[string]*Lease{}
	m.byKey = map[key]*Lease{}
	m.mu.Unlock()
	for _, l := range leases {
		l.finalize(ReasonManagerStopped)
	}
}

// expire is invoked by the per-lease AfterFunc when TTL elapses.
func (m *Manager) expire(l *Lease) {
	m.mu.Lock()
	// If the lease was already released/preempted the id will be absent; skip.
	current, ok := m.byID[l.ID]
	if !ok || current != l {
		m.mu.Unlock()
		return
	}
	delete(m.byID, l.ID)
	delete(m.byKey, key{l.SessionID, l.Scope})
	m.mu.Unlock()
	l.finalize(ReasonExpired)
}

func validateAcquire(req AcquireRequest) error {
	if req.SessionID == "" {
		return ErrInvalidSession
	}
	if req.Scope == "" {
		return ErrInvalidScope
	}
	if req.Owner == "" {
		return ErrInvalidOwner
	}
	if req.TTL <= 0 {
		return ErrInvalidTTL
	}
	return nil
}
