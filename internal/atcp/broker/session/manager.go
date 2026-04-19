package session

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// reapInterval is how often the reaper inspects sessions for completion. It is
// a package-level variable so tests can shorten it.
var reapInterval = 50 * time.Millisecond

func reapTick() <-chan time.Time { return time.After(reapInterval) }

// Manager owns the lifecycle of active sessions.
//
// It does not persist state: restart recovery is the responsibility of the
// broker subsystem that composes the manager with an atcp_sessions table.
// The manager only maintains the in-process registry and a reaper that prunes
// entries once their Session has signalled Done.
type Manager struct {
	opts ManagerOptions

	mu       sync.RWMutex
	sessions map[string]*Session

	reaperDone chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc
}

// ManagerOptions configures a Manager.
type ManagerOptions struct {
	// DefaultLogOptions sets the default per-session output log budget.
	// Zero values fall back to DefaultOutputLogOptions.
	DefaultLogOptions OutputLogOptions
}

// ErrSessionNotFound is returned by Get/Delete when no session matches the id.
var ErrSessionNotFound = errors.New("atcp session: not found")

// ErrManagerClosed is returned by Create after Stop.
var ErrManagerClosed = errors.New("atcp session: manager is stopped")

// NewManager constructs a Manager with the supplied options.
func NewManager(opts ManagerOptions) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		opts:       opts,
		sessions:   make(map[string]*Session),
		reaperDone: make(chan struct{}),
		ctx:        ctx,
		cancel:     cancel,
	}
	go m.reap()
	return m
}

// Create constructs a Session from spec, starts it under the manager's root
// context, and registers it. The returned session is already running.
//
// The optional logOpts overrides the Manager's default log options for this
// session only. Pass the zero value to fall back to the manager default.
func (m *Manager) Create(spec Spec, logOpts OutputLogOptions) (*Session, error) {
	m.mu.Lock()
	if m.ctx.Err() != nil {
		m.mu.Unlock()
		return nil, ErrManagerClosed
	}
	m.mu.Unlock()

	if (logOpts == OutputLogOptions{}) {
		logOpts = m.opts.DefaultLogOptions
	}
	sess, err := New(spec, logOpts)
	if err != nil {
		return nil, err
	}
	if err := sess.Start(m.ctx); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.sessions[sess.ID()] = sess
	m.mu.Unlock()

	return sess, nil
}

// Get returns the Session for id or ErrSessionNotFound.
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return s, nil
}

// List returns Snapshots for every registered session, sorted by CreatedAt.
func (m *Manager) List() []Snapshot {
	m.mu.RLock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.RUnlock()

	out := make([]Snapshot, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, s.Snapshot())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Delete closes the session for id, waits for it to exit, and removes it from
// the registry. Returns ErrSessionNotFound when unknown.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return ErrSessionNotFound
	}
	delete(m.sessions, id)
	m.mu.Unlock()

	s.Close()
	<-s.Done()
	return nil
}

// Stop closes every session and waits for them to exit. After Stop returns,
// the Manager rejects further Create calls.
func (m *Manager) Stop() {
	m.cancel()

	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = map[string]*Session{}
	m.mu.Unlock()

	for _, s := range sessions {
		s.Close()
	}
	for _, s := range sessions {
		<-s.Done()
	}
	<-m.reaperDone
}

// reap watches for sessions that have signalled Done on their own (e.g. child
// process exited naturally) and removes them from the registry.
func (m *Manager) reap() {
	defer close(m.reaperDone)
	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}

		// Snapshot current sessions and pick ones that are already done.
		m.mu.RLock()
		candidates := make([]*Session, 0, len(m.sessions))
		for _, s := range m.sessions {
			candidates = append(candidates, s)
		}
		m.mu.RUnlock()

		if len(candidates) == 0 {
			select {
			case <-m.ctx.Done():
				return
			case <-reapTick():
			}
			continue
		}

		for _, s := range candidates {
			select {
			case <-s.Done():
				m.mu.Lock()
				// Double-check under lock (Delete may have removed it).
				if existing, ok := m.sessions[s.ID()]; ok && existing == s {
					delete(m.sessions, s.ID())
				}
				m.mu.Unlock()
			default:
			}
		}

		select {
		case <-m.ctx.Done():
			return
		case <-reapTick():
		}
	}
}
