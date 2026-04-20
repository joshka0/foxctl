// Package broker composes the ATCP subsystems — session manager, lease
// manager, adapter registry — into a single value that transport handlers
// (HTTP JSON, Unix socket, eventual SSE) can call.
//
// The broker does not itself listen on any transport. It is a pure in-process
// facade so handlers remain small and testable.
package broker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/adapter/generictty"
	"github.com/joshka0/foxctl/internal/atcp/adapter/profiles"
	"github.com/joshka0/foxctl/internal/atcp/broker/lease"
	"github.com/joshka0/foxctl/internal/atcp/broker/room"
	"github.com/joshka0/foxctl/internal/atcp/broker/router"
	"github.com/joshka0/foxctl/internal/atcp/broker/session"
	"github.com/joshka0/foxctl/internal/atcp/broker/storage"
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
	SetKittyKeyboardActive(bool)
	SetAllowWriteBytes(bool)
	SetDefaultSubmitKey(string)
}

// AdapterFactory returns a fresh Adapter for a newly-created session. Profile
// selection uses session.Spec.Adapter as the lookup key.
type AdapterFactory func(profile string) Adapter

// DefaultAdapterFactory returns a generic-tty adapter for any profile name. A
// nil factory on Broker.Options falls back to this.
func DefaultAdapterFactory(_ string) Adapter { return generictty.New() }

func DefaultReadinessProfile(profile string) session.ReadinessProfile {
	return profiles.DefaultReadiness(profile)
}

func mergeReadinessProfile(base, override session.ReadinessProfile) session.ReadinessProfile {
	if override.ScreenRegex != "" {
		base.ScreenRegex = override.ScreenRegex
	}
	if override.ThresholdBPS > 0 {
		base.ThresholdBPS = override.ThresholdBPS
	}
	if override.Debounce > 0 {
		base.Debounce = override.Debounce
	}
	if override.RequireNotAltScreen {
		base.RequireNotAltScreen = true
	}
	return base
}

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

	// Storage is the persistence backend. nil means storage.NewNoop(): the
	// broker runs fully in memory. When set, every room/member/message
	// mutation is write-through and the broker hydrates on construction.
	//
	// The broker does NOT own the Store's lifecycle — callers must Close
	// it themselves after Broker.Stop. That keeps the daemon (which owns
	// the DB handle) in charge of teardown ordering.
	Storage storage.Store

	// HydrateContext bounds the initial LoadRooms/LoadMembers calls that
	// happen inside New. Zero means context.Background(). Useful for
	// daemons that want a startup deadline.
	HydrateContext context.Context
}

// Broker is the single entrypoint for ATCP intents originating from transport
// handlers. It is safe for concurrent use.
type Broker struct {
	sessions   *session.Manager
	leases     *lease.Manager
	roomMgr    *room.Manager
	msgRouter  *router.Router
	store      storage.Store
	messagesMu sync.RWMutex
	messages   map[string][]storage.MessageRecord

	adaptersMu    sync.RWMutex
	adapters      map[string]Adapter
	factory       AdapterFactory
	allowUnleased bool
	detachMu      sync.Mutex
}

// New constructs a Broker. The caller must call Stop when shutting down.
//
// When opts.Storage is non-nil, New performs two startup steps before
// returning:
//
//  1. Hydrate: rooms and members are loaded from the Store. This restores
//     long-lived coordination state (room IDs, member history) across
//     process restarts.
//  2. Detach: every still-active member is stamped with LeftAt=now and
//     written back through the Store. This reflects that PTY sessions do
//     not survive a restart, so any previously-active binding is now
//     dangling. New joins post-restart start fresh.
//
// Hydrate errors are returned immediately; detach errors are logged via the
// Store's AppendMessage is NOT used here (detach is a member-level event).
// We return the joined error so callers can decide whether to continue with
// an in-memory-only mode or abort.
func New(opts Options) (*Broker, error) {
	factory := opts.AdapterFactory
	if factory == nil {
		factory = DefaultAdapterFactory
	}
	store := opts.Storage
	if store == nil {
		store = storage.NewNoop()
	}
	b := &Broker{
		sessions:      session.NewManager(opts.Sessions),
		leases:        lease.NewManager(),
		roomMgr:       room.NewManager(),
		store:         store,
		messages:      make(map[string][]storage.MessageRecord),
		adapters:      make(map[string]Adapter),
		factory:       factory,
		allowUnleased: opts.AllowUnleasedInputForTests,
	}
	// The router depends on the broker's session/lease surface, so wire it
	// here once all sub-managers exist. Broker.AcquireLease/ReleaseLease/
	// Submit satisfy router.Dispatcher; room.Manager satisfies
	// router.MemberResolver.
	b.msgRouter = router.New(b, b.roomMgr, router.Options{})

	ctx := opts.HydrateContext
	if ctx == nil {
		ctx = context.Background()
	}
	if err := b.hydrate(ctx); err != nil {
		// Hydrate failed: the broker is only half-built but the session
		// and lease managers are already running goroutines (reapers,
		// timers). Tearing them down here keeps New's "success or no
		// side effects" contract — callers that get an error back never
		// have to remember to call Stop on a broker they never received.
		b.sessions.Stop()
		b.leases.Stop()
		return nil, fmt.Errorf("atcp broker: hydrate: %w", err)
	}
	return b, nil
}

// MustNew is a convenience for callers (especially tests) that never pass a
// Store and therefore cannot experience a hydrate error. Panics on any
// error from New.
func MustNew(opts Options) *Broker {
	b, err := New(opts)
	if err != nil {
		panic(err)
	}
	return b
}

// hydrate loads persisted rooms + members and detaches stale active members.
func (b *Broker) hydrate(ctx context.Context) error {
	rooms, err := b.store.LoadRooms(ctx)
	if err != nil {
		return err
	}
	if len(rooms) == 0 {
		return nil
	}
	members := make(map[string][]room.Member, len(rooms))
	for _, r := range rooms {
		ms, err := b.store.LoadMembers(ctx, r.ID)
		if err != nil {
			return err
		}
		members[r.ID] = ms
		msgs, err := b.store.LoadMessages(ctx, r.ID, 0)
		if err != nil {
			return err
		}
		if len(msgs) > 0 {
			b.messages[r.ID] = append([]storage.MessageRecord(nil), msgs...)
		}
	}
	b.roomMgr.Hydrate(rooms, members)
	// Any member whose LeftAt is still zero pointed at a PTY that no
	// longer exists. Stamp LeftAt=now and persist so the active set starts
	// clean.
	return b.roomMgr.StampActiveMembersLeft(time.Now(), func(m room.Member) error {
		return b.store.SaveMember(ctx, m)
	})
}

// Sessions exposes the session manager for transports that need read-only
// lookups (e.g. a future event-stream subscriber).
func (b *Broker) Sessions() *session.Manager { return b.sessions }

// Leases exposes the lease manager for transports that serve /leases endpoints.
func (b *Broker) Leases() *lease.Manager { return b.leases }

// Rooms exposes the in-memory room manager. The underlying storage is
// deliberately volatile in this slice; persistence (atcp_rooms /
// atcp_room_members) lands with the daemon wiring.
func (b *Broker) Rooms() *room.Manager { return b.roomMgr }

// Router exposes the fan-out router for transports that expose
// `message.send`. Kept as a getter so tests can swap policies without
// reaching into unexported fields.
func (b *Broker) Router() *router.Router { return b.msgRouter }

// CreateRoom is a thin passthrough that writes through to storage on success.
// Persistence failures after the in-memory insert are reported but not
// rolled back — the room exists to live consumers; operators see the error
// and decide whether to retry.
//
// ctx bounds the storage write only; the in-memory mutation is synchronous
// and unaffected by cancellation so callers can't observe a room that
// exists in memory but not on disk via ctx timeout alone.
func (b *Broker) CreateRoom(ctx context.Context, req room.CreateRoomRequest) (room.Room, error) {
	r, err := b.roomMgr.CreateRoom(req)
	if err != nil {
		return room.Room{}, err
	}
	if perr := b.store.SaveRoom(ctx, r); perr != nil {
		return r, fmt.Errorf("atcp broker: persist room: %w", perr)
	}
	return r, nil
}

// JoinRoom verifies the session exists before delegating, writes the member
// through storage on success, and maps unknown sessions to the broker-level
// sentinel.
func (b *Broker) JoinRoom(ctx context.Context, req room.JoinRequest) (room.Member, error) {
	if _, err := b.sessions.Get(req.SessionID); err != nil {
		return room.Member{}, ErrSessionNotFound
	}
	mem, err := b.roomMgr.JoinRoom(req)
	if err != nil {
		return room.Member{}, err
	}
	if perr := b.store.SaveMember(ctx, mem); perr != nil {
		return mem, fmt.Errorf("atcp broker: persist member: %w", perr)
	}
	return mem, nil
}

// LeaveRoom stamps LeftAt in memory and writes the updated row through. The
// underlying session stays alive — "persist on leave" per plan §5a.2.
func (b *Broker) LeaveRoom(ctx context.Context, roomID, agentID string) (room.Member, error) {
	mem, err := b.roomMgr.LeaveRoom(roomID, agentID)
	if err != nil {
		return room.Member{}, err
	}
	if perr := b.store.SaveMember(ctx, mem); perr != nil {
		return mem, fmt.Errorf("atcp broker: persist member leave: %w", perr)
	}
	return mem, nil
}

// SendMessage fans out through the router and persists the fan-out outcome
// for audit. Storage errors never change the delivery result — the members
// already got (or didn't get) the message; persistence is a downstream
// concern.
func (b *Broker) SendMessage(ctx context.Context, msg router.Message) (router.Result, error) {
	sentAt := time.Now()
	res, err := b.msgRouter.Send(msg)
	if err != nil {
		return res, err
	}
	// The router returns a generated message_id even on partial failure, so
	// the audit record is always well-formed. An empty MessageID would
	// mean the router rejected the input before fan-out, which also
	// returns err != nil above.
	rec := storage.NewMessageRecordFromResult(msg, res, sentAt)
	b.appendMessageRecord(rec)
	if perr := b.store.AppendMessage(ctx, rec); perr != nil {
		return res, fmt.Errorf("atcp broker: persist message: %w", perr)
	}
	return res, nil
}

// ListMessages returns the in-process message audit log for a room. When the
// broker was hydrated from storage, this includes persisted records loaded at
// startup; new sends are appended in memory regardless of storage mode.
func (b *Broker) ListMessages(roomID string, limit int) ([]storage.MessageRecord, error) {
	if _, err := b.roomMgr.GetRoom(roomID); err != nil {
		return nil, err
	}
	b.messagesMu.RLock()
	defer b.messagesMu.RUnlock()
	msgs := b.messages[roomID]
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	out := make([]storage.MessageRecord, len(msgs))
	for i, msg := range msgs {
		out[i] = cloneMessageRecord(msg)
	}
	return out, nil
}

func (b *Broker) appendMessageRecord(rec storage.MessageRecord) {
	b.messagesMu.Lock()
	defer b.messagesMu.Unlock()
	b.messages[rec.RoomID] = append(b.messages[rec.RoomID], cloneMessageRecord(rec))
}

func cloneMessageRecord(rec storage.MessageRecord) storage.MessageRecord {
	out := rec
	if rec.Members != nil {
		out.Members = append([]storage.MessageDeliveryRecord(nil), rec.Members...)
	}
	return out
}

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
	spec.Readiness = mergeReadinessProfile(DefaultReadinessProfile(spec.Adapter), spec.Readiness)
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
	// wrapping and kitty-enter submission follow whatever the child has
	// enabled.
	if spec.SubmitKey != "" {
		adapter.SetDefaultSubmitKey(spec.SubmitKey)
	}
	adapter.SetAllowWriteBytes(spec.EnableRawBytes)
	initialMode := sess.Tracker().Snapshot()
	adapter.SetBracketedPasteEnabled(initialMode.BracketedPaste)
	adapter.SetKittyKeyboardActive(initialMode.KittyKeyboard)
	modes, cancelModes := sess.Tracker().Subscribe()
	go func() {
		for c := range modes {
			adapter.SetBracketedPasteEnabled(c.Mode.BracketedPaste)
			adapter.SetKittyKeyboardActive(c.Mode.KittyKeyboard)
		}
	}()

	// When the session exits, drop its adapter entry, tear down the mode
	// subscription so the mirror goroutine above exits, AND detach any
	// active room memberships bound to this session. The third step is the
	// critical one: without it, ListMembers would still show an active
	// binding and the Router would keep trying to AcquireLease against a
	// PTY that no longer exists. Detach runs on *every* exit path —
	// natural child exit, DeleteSession, broker.Stop — because the
	// watcher keys off sess.Done().
	go func(id string) {
		<-sess.Done()
		cancelModes()
		b.adaptersMu.Lock()
		delete(b.adapters, id)
		b.adaptersMu.Unlock()
		b.detachSessionFromRooms(id)
	}(sess.ID())

	return sess.Snapshot(), nil
}

// detachSessionFromRooms stamps LeftAt on every active member that was
// bound to sessionID and writes those rows through storage. Errors from
// the Store are logged at a low severity through the broker's intended
// surface — for now that means they are dropped because the broker has no
// injected logger; operators will see them once observability lands. The
// room-manager mutation is authoritative regardless of persistence
// outcome, so a Store failure does NOT re-attach the member in memory.
func (b *Broker) detachSessionFromRooms(sessionID string) {
	b.detachMu.Lock()
	defer b.detachMu.Unlock()
	changed := b.roomMgr.DetachSession(sessionID, time.Now())
	if len(changed) == 0 {
		return
	}
	// Persist each detach. We intentionally use context.Background here:
	// the session-exit goroutine has no inbound request context, and
	// refusing to write-through because of a zero-value context would
	// leave the Store permanently diverged from memory. Callers that
	// need a deadline can drain via broker.Stop.
	ctx := context.Background()
	for _, mem := range changed {
		_ = b.store.SaveMember(ctx, mem)
	}
}

// DeleteSession tears down the named session. The session goroutine owns
// room detachment (see CreateSession's sess.Done watcher), so this method
// only needs to drive session teardown — detachSessionFromRooms runs
// automatically when sess.Done fires.
func (b *Broker) DeleteSession(id string) error {
	err := b.sessions.Delete(id)
	if errors.Is(err, session.ErrSessionNotFound) {
		return ErrSessionNotFound
	}
	if err != nil {
		return err
	}
	// The session-exit watcher also detaches room memberships, but it runs in
	// its own goroutine. Do it synchronously here so callers that immediately
	// close the Store after DeleteSession do not race the persistence write.
	b.detachSessionFromRooms(id)
	return nil
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

// SessionReady reports whether a session has returned to its configured
// readiness profile. It is used by terminal delivery policies before mutating
// a PTY.
func (b *Broker) SessionReady(sessionID string) (bool, string, error) {
	sess, err := b.sessions.Get(sessionID)
	if err != nil {
		return false, "session not found", ErrSessionNotFound
	}
	ready := sess.ProfileReadiness()
	switch {
	case ready.Idle:
		return true, "ready", nil
	case !ready.Idle:
		return false, "output_busy", nil
	case ready.ScreenRegex != "" && !ready.ScreenMatch:
		return false, "screen_regex_no_match", nil
	default:
		return false, "not_ready", nil
	}
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
	if intent.Text == "" {
		return b.runTerminal(sessionID, intent.LeaseID, func(a Adapter) ([]byte, error) {
			return a.CompileSubmit(intent)
		})
	}
	sess, err := b.sessions.Get(sessionID)
	if err != nil {
		return 0, ErrSessionNotFound
	}
	if err := b.checkLease(sessionID, intent.LeaseID); err != nil {
		return 0, err
	}
	adapter, err := b.Adapter(sessionID)
	if err != nil {
		return 0, err
	}
	textBytes, err := adapter.CompileText(intents.TerminalText{Text: intent.Text})
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrIntentInvalid, err)
	}
	keyBytes, err := adapter.CompileSubmit(intents.TerminalSubmit{SubmitKey: intent.SubmitKey})
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrIntentInvalid, err)
	}
	n, err := sess.Write(textBytes)
	if err != nil {
		return n, err
	}
	// Real users type text and then press Enter as separate terminal events.
	// Some raw-mode TUIs, including codex, display text but ignore an
	// immediately-concatenated kitty Enter in the same PTY write.
	time.Sleep(100 * time.Millisecond)
	m, err := sess.Write(keyBytes)
	return n + m, err
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
