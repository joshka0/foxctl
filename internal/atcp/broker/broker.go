// Package broker composes the ATCP subsystems — session manager, lease
// manager, adapter registry — into a single value that transport handlers
// (HTTP JSON, Unix socket, eventual SSE) can call.
//
// The broker does not itself listen on any transport. It is a pure in-process
// facade so handlers remain small and testable.
package broker

import (
	"errors"
	"fmt"
	"sync"

	"github.com/joshka0/foxctl/internal/atcp/adapter/generictty"
	"github.com/joshka0/foxctl/internal/atcp/broker/lease"
	"github.com/joshka0/foxctl/internal/atcp/broker/session"
	"github.com/joshka0/foxctl/internal/atcp/intents"
)

// Adapter is the subset of the generic-tty adapter the broker depends on.
// Defined as an interface so alternative profiles (posix-shell, node-readline,
// claude) can slot in without subpackage churn.
type Adapter interface {
	CompileText(intents.TerminalText) ([]byte, error)
	CompileKey(intents.TerminalKey) ([]byte, error)
	CompileSubmit(intents.TerminalSubmit) ([]byte, error)
	CompilePaste(intents.TerminalPaste) ([]byte, error)
	CompileWriteBytes(intents.TerminalWriteBytes) ([]byte, error)
	SetBracketedPasteEnabled(bool)
}

// AdapterFactory returns a fresh Adapter for a newly-created session. Profile
// selection uses session.Spec.Adapter as the lookup key.
type AdapterFactory func(profile string) Adapter

// DefaultAdapterFactory returns a generic-tty adapter for any profile name. A
// nil factory on Broker.Options falls back to this.
func DefaultAdapterFactory(_ string) Adapter { return generictty.New() }

// Options configures Broker construction.
type Options struct {
	// Sessions configures the underlying session manager. Zero values use
	// session.DefaultOutputLogOptions.
	Sessions session.ManagerOptions
	// AdapterFactory picks an Adapter for each created session. Nil means
	// DefaultAdapterFactory.
	AdapterFactory AdapterFactory
	// AllowUnleasedInputForTests loosens terminal-input lease enforcement so
	// intents without a lease_id are accepted whenever no lease is currently
	// held. The broker's production invariant is that every terminal
	// mutation must acquire a lease first; this flag exists only so tests
	// can exercise paths that don't care about lease serialisation.
	//
	// Do not set this from a production code path. Keep it opt-in and
	// explicit so accidental callers are easy to spot in review.
	AllowUnleasedInputForTests bool
}

// Broker is the single entrypoint for ATCP intents originating from transport
// handlers. It is safe for concurrent use.
type Broker struct {
	sessions *session.Manager
	leases   *lease.Manager

	adaptersMu    sync.RWMutex
	adapters      map[string]Adapter
	factory       AdapterFactory
	allowUnleased bool
}

// New constructs a Broker. The caller must call Stop when shutting down.
func New(opts Options) *Broker {
	factory := opts.AdapterFactory
	if factory == nil {
		factory = DefaultAdapterFactory
	}
	return &Broker{
		sessions:      session.NewManager(opts.Sessions),
		leases:        lease.NewManager(),
		adapters:      make(map[string]Adapter),
		factory:       factory,
		allowUnleased: opts.AllowUnleasedInputForTests,
	}
}

// Sessions exposes the session manager for transports that need read-only
// lookups (e.g. a future event-stream subscriber).
func (b *Broker) Sessions() *session.Manager { return b.sessions }

// Leases exposes the lease manager for transports that serve /leases endpoints.
func (b *Broker) Leases() *lease.Manager { return b.leases }

// Stop closes every session and lease. Idempotent.
func (b *Broker) Stop() {
	b.sessions.Stop()
	b.leases.Stop()
	b.adaptersMu.Lock()
	b.adapters = map[string]Adapter{}
	b.adaptersMu.Unlock()
}

// Errors returned by broker intent handlers.
var (
	ErrSessionNotFound = errors.New("atcp broker: session not found")
	ErrLeaseRequired   = errors.New("atcp broker: terminal intents require an active lease on the session")
	ErrLeaseMismatch   = errors.New("atcp broker: supplied lease_id does not match the current holder")
	ErrIntentInvalid   = errors.New("atcp broker: intent invalid")
)

// CreateSession starts a new PTY and registers an adapter for it. Returns a
// Snapshot so callers can echo state (pid, id, status) back to the client.
func (b *Broker) CreateSession(spec session.Spec, logOpts session.OutputLogOptions) (session.Snapshot, error) {
	sess, err := b.sessions.Create(spec, logOpts)
	if err != nil {
		return session.Snapshot{}, err
	}

	adapter := b.factory(spec.Adapter)
	if adapter == nil {
		adapter = generictty.New()
	}
	b.adaptersMu.Lock()
	b.adapters[sess.ID()] = adapter
	b.adaptersMu.Unlock()

	// Mirror terminal-mode state onto the adapter so that bracketed-paste
	// wrapping follows whatever the child has enabled.
	adapter.SetBracketedPasteEnabled(sess.Tracker().Snapshot().BracketedPaste)
	modes, cancelModes := sess.Tracker().Subscribe()
	go func() {
		for c := range modes {
			adapter.SetBracketedPasteEnabled(c.Mode.BracketedPaste)
		}
	}()

	// When the session exits, drop its adapter entry and tear down the mode
	// subscription so the mirror goroutine above exits.
	go func(id string) {
		<-sess.Done()
		cancelModes()
		b.adaptersMu.Lock()
		delete(b.adapters, id)
		b.adaptersMu.Unlock()
	}(sess.ID())

	return sess.Snapshot(), nil
}

// DeleteSession tears down the named session.
func (b *Broker) DeleteSession(id string) error {
	err := b.sessions.Delete(id)
	if errors.Is(err, session.ErrSessionNotFound) {
		return ErrSessionNotFound
	}
	return err
}

// GetSession returns a Snapshot for id.
func (b *Broker) GetSession(id string) (session.Snapshot, error) {
	sess, err := b.sessions.Get(id)
	if err != nil {
		return session.Snapshot{}, ErrSessionNotFound
	}
	return sess.Snapshot(), nil
}

// ListSessions returns snapshots for every registered session.
func (b *Broker) ListSessions() []session.Snapshot {
	return b.sessions.List()
}

// Adapter returns the adapter bound to a session, or ErrSessionNotFound.
func (b *Broker) Adapter(sessionID string) (Adapter, error) {
	b.adaptersMu.RLock()
	defer b.adaptersMu.RUnlock()
	a, ok := b.adapters[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return a, nil
}

// SubmitText compiles and writes a TerminalText intent.
func (b *Broker) SubmitText(sessionID string, intent intents.TerminalText) (int, error) {
	return b.runTerminal(sessionID, intent.LeaseID, func(a Adapter) ([]byte, error) {
		return a.CompileText(intent)
	})
}

// SubmitKey compiles and writes a TerminalKey intent.
func (b *Broker) SubmitKey(sessionID string, intent intents.TerminalKey) (int, error) {
	return b.runTerminal(sessionID, intent.LeaseID, func(a Adapter) ([]byte, error) {
		return a.CompileKey(intent)
	})
}

// Submit compiles and writes a TerminalSubmit intent.
func (b *Broker) Submit(sessionID string, intent intents.TerminalSubmit) (int, error) {
	return b.runTerminal(sessionID, intent.LeaseID, func(a Adapter) ([]byte, error) {
		return a.CompileSubmit(intent)
	})
}

// Paste compiles and writes a TerminalPaste intent.
func (b *Broker) Paste(sessionID string, intent intents.TerminalPaste) (int, error) {
	return b.runTerminal(sessionID, intent.LeaseID, func(a Adapter) ([]byte, error) {
		return a.CompilePaste(intent)
	})
}

// WriteBytes forwards a raw-byte intent if the adapter permits it.
func (b *Broker) WriteBytes(sessionID string, intent intents.TerminalWriteBytes) (int, error) {
	return b.runTerminal(sessionID, intent.LeaseID, func(a Adapter) ([]byte, error) {
		return a.CompileWriteBytes(intent)
	})
}

// AcquireLease proxies to the lease manager. The broker does not auto-scope
// leases to sessions; callers must set req.SessionID themselves.
func (b *Broker) AcquireLease(req lease.AcquireRequest) (*lease.Lease, error) {
	if _, err := b.sessions.Get(req.SessionID); err != nil {
		return nil, ErrSessionNotFound
	}
	return b.leases.Acquire(req)
}

// ReleaseLease proxies to the lease manager.
func (b *Broker) ReleaseLease(id string) error {
	return b.leases.Release(id)
}

// runTerminal is the common path for every input intent. It looks up the
// session and adapter, enforces the lease requirement, compiles the intent,
// and writes the bytes.
func (b *Broker) runTerminal(sessionID, leaseID string, compile func(Adapter) ([]byte, error)) (int, error) {
	sess, err := b.sessions.Get(sessionID)
	if err != nil {
		return 0, ErrSessionNotFound
	}
	if err := b.checkLease(sessionID, leaseID); err != nil {
		return 0, err
	}
	adapter, err := b.Adapter(sessionID)
	if err != nil {
		return 0, err
	}
	data, err := compile(adapter)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrIntentInvalid, err)
	}
	return sess.Write(data)
}

// checkLease enforces that the supplied leaseID matches the current holder on
// the (sessionID, terminal.input) scope.
//
// The production invariant (plan §6) is that every terminal mutation must
// hold a lease. In tests the AllowUnleasedInputForTests broker option relaxes
// this so callers without a lease succeed when no lease is currently held;
// a lease held by someone else always rejects unleased intents.
func (b *Broker) checkLease(sessionID, leaseID string) error {
	held, ok := b.leases.Held(sessionID, lease.ScopeTerminalInput)
	if !ok {
		if leaseID == "" {
			if b.allowUnleased {
				return nil
			}
			return ErrLeaseRequired
		}
		// Client supplied a lease id but no lease is held: the lease either
		// expired or was already released. Fail fast rather than silently
		// accept.
		return ErrLeaseMismatch
	}
	if leaseID == "" {
		return ErrLeaseRequired
	}
	if held.ID != leaseID {
		return ErrLeaseMismatch
	}
	return nil
}
