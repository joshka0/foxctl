// Package router fans out ATCP message.send intents over the active members
// of a room.
//
// For this slice the Router supports one delivery policy — "terminal" — and
// one content shape — plain text routed through TerminalSubmit. Other bodies
// (key, paste, write_bytes) and policies (inbox, native, overlay) are
// deliberate follow-ups; the surface is kept narrow so persistence,
// ack-required semantics, and the full plan §5a.4 autonomy loop can land
// one concern at a time.
package router

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/broker/lease"
	"github.com/joshka0/foxctl/internal/atcp/broker/room"
	"github.com/joshka0/foxctl/internal/atcp/intents"
	"github.com/oklog/ulid/v2"
)

// Dispatcher is the subset of *broker.Broker that Router needs. Declared as
// an interface so the router package has no import back into broker.
type Dispatcher interface {
	AcquireLease(req lease.AcquireRequest) (*lease.Lease, error)
	ReleaseLease(id string) error
	Submit(sessionID string, intent intents.TerminalSubmit) (int, error)
}

// MemberResolver returns the currently-active members of a room. The router
// treats it as a plug so a future SQL-backed store can replace the in-memory
// room.Manager without touching router code.
type MemberResolver interface {
	ActiveMembers(roomID string) ([]room.Member, error)
}

// Options configures a Router.
type Options struct {
	// LeaseTTL is the per-member lease window. 2s is enough for any single
	// Submit; the router always releases on the happy path, so the TTL is
	// only a safety net for caller crashes.
	LeaseTTL time.Duration
	// Clock is for tests; defaults to time.Now.
	Clock func() time.Time
}

// Router is safe for concurrent use.
type Router struct {
	d        Dispatcher
	resolver MemberResolver
	opts     Options
	mu       sync.Mutex // serialises fan-out so a burst of Send calls doesn't reorder per-member arrivals
}

// New constructs a Router. Both d and resolver are required.
func New(d Dispatcher, resolver MemberResolver, opts Options) *Router {
	if opts.LeaseTTL <= 0 {
		opts.LeaseTTL = 2 * time.Second
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	return &Router{d: d, resolver: resolver, opts: opts}
}

// DeliveryPolicy enumerates the supported delivery targets.
type DeliveryPolicy string

const (
	// DeliveryTerminal injects into each member's PTY via TerminalSubmit.
	DeliveryTerminal DeliveryPolicy = "terminal"
)

// Message is the routed payload. For this slice only text + terminal
// delivery is supported; byte-level and structured bodies come later.
type Message struct {
	// ID uniquely identifies this logical message. Callers may leave blank
	// and the router will generate one.
	ID string
	// RoomID is the destination room. Required.
	RoomID string
	// Source is a free-form identifier for who sent it (agent id, scheduler,
	// etc.). Used as the lease owner string for operator-visible traceability.
	Source string
	// Text is the content that will land on every member's terminal.
	Text string
	// Delivery selects the member-level delivery path. Must be DeliveryTerminal
	// in this slice. Empty defaults to terminal.
	Delivery DeliveryPolicy
	// SkipAgents is an optional suppression list; typically the sender
	// themselves so they don't see their own message echo back.
	SkipAgents []string
}

// MemberResult is the per-member outcome of Send.
type MemberResult struct {
	AgentID   string
	SessionID string
	// Delivered is true iff the router successfully wrote to the member's PTY.
	Delivered bool
	// Err is set when Delivered is false. The sender saw every other member
	// try regardless, because partial failures must not silently drop
	// downstream members.
	Err error
}

// Result summarises a Send call. Counts are derived to keep callers one
// step away from iterating the per-member slice.
type Result struct {
	MessageID string
	Delivered int
	Failed    int
	Members   []MemberResult
}

// Sentinel errors returned by Send.
var (
	ErrRoomIDRequired      = errors.New("atcp router: room_id is required")
	ErrEmptyMessage        = errors.New("atcp router: message text is empty")
	ErrUnsupportedDelivery = errors.New("atcp router: delivery policy not supported in this slice")
	// ErrNoActiveMembers is returned when the resolver reports zero active
	// members in the room (nobody is joined, or everyone left).
	ErrNoActiveMembers = errors.New("atcp router: no active members in room")
	// ErrNoMutableMembers is returned when active members exist but none of
	// them are flagged CanMutate=true. Observers, inbox-only, and
	// explicitly read-only joins sit in this bucket. Keeping this distinct
	// from ErrNoActiveMembers lets operators distinguish "room is empty"
	// from "room is populated but no one may be typed into".
	ErrNoMutableMembers = errors.New("atcp router: no mutable members in room")
)

// Send fans the message out to every active member of the room. The method
// does NOT abort on the first per-member error; delivery is best-effort and
// every member is attempted.
func (r *Router) Send(msg Message) (Result, error) {
	if msg.RoomID == "" {
		return Result{}, ErrRoomIDRequired
	}
	if msg.Text == "" {
		return Result{}, ErrEmptyMessage
	}
	if msg.Delivery == "" {
		msg.Delivery = DeliveryTerminal
	}
	if msg.Delivery != DeliveryTerminal {
		return Result{}, fmt.Errorf("%w: %s", ErrUnsupportedDelivery, msg.Delivery)
	}
	if msg.ID == "" {
		msg.ID = ulid.Make().String()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	members, err := r.resolver.ActiveMembers(msg.RoomID)
	if err != nil {
		return Result{}, err
	}
	if len(members) == 0 {
		return Result{MessageID: msg.ID}, ErrNoActiveMembers
	}
	// Filter out skipped agents and members explicitly flagged non-mutable.
	// The CanMutate check is the authority boundary around terminal
	// injection: observers, inbox-only bindings, and any member that joined
	// with --can-mutate=false must NOT receive PTY writes here. Leases
	// serialise producers but do not gate them on role; that check must
	// happen at the router.
	skip := make(map[string]struct{}, len(msg.SkipAgents))
	for _, a := range msg.SkipAgents {
		skip[a] = struct{}{}
	}
	// hasAnyMutable tracks mutability BEFORE the skip filter so the error
	// below can distinguish "room is populated but nobody is typeable" from
	// "room is populated but the sender excluded the only mutable targets".
	hasAnyMutable := false
	targets := members[:0]
	for _, m := range members {
		if m.CanMutate {
			hasAnyMutable = true
		}
		if _, ok := skip[m.AgentID]; ok {
			continue
		}
		if !m.CanMutate {
			continue
		}
		targets = append(targets, m)
	}
	if len(targets) == 0 {
		if !hasAnyMutable {
			// Room has members but none of them are mutable — this is a
			// role/config problem, not a skip-list problem.
			return Result{MessageID: msg.ID}, ErrNoMutableMembers
		}
		// Mutable members exist but they were all filtered out by
		// SkipAgents. From the sender's perspective that's "nobody
		// eligible to receive", which is closer to ErrNoActiveMembers
		// semantically than a role error.
		return Result{MessageID: msg.ID}, ErrNoActiveMembers
	}

	owner := msg.Source
	if owner == "" {
		owner = "router"
	}

	res := Result{MessageID: msg.ID, Members: make([]MemberResult, 0, len(targets))}
	for _, m := range targets {
		mr := r.deliverOne(m, msg, owner)
		if mr.Delivered {
			res.Delivered++
		} else {
			res.Failed++
		}
		res.Members = append(res.Members, mr)
	}
	return res, nil
}

// deliverOne is the per-member happy path:
//  1. Acquire a terminal.input lease with the router as owner.
//  2. Submit via the dispatcher.
//  3. Release the lease (always — even on submit failure).
func (r *Router) deliverOne(m room.Member, msg Message, owner string) MemberResult {
	mr := MemberResult{AgentID: m.AgentID, SessionID: m.SessionID}
	l, err := r.d.AcquireLease(lease.AcquireRequest{
		SessionID: m.SessionID,
		Scope:     lease.ScopeTerminalInput,
		Owner:     fmt.Sprintf("%s/message:%s", owner, msg.ID),
		TTL:       r.opts.LeaseTTL,
	})
	if err != nil {
		mr.Err = fmt.Errorf("acquire lease: %w", err)
		return mr
	}
	defer func() {
		// Best-effort release: if the submit succeeded the lease is already
		// "used" but releasing it promptly lets another sender proceed.
		_ = r.d.ReleaseLease(l.ID)
	}()
	if _, err := r.d.Submit(m.SessionID, intents.TerminalSubmit{
		Text:    msg.Text,
		LeaseID: l.ID,
	}); err != nil {
		mr.Err = fmt.Errorf("submit: %w", err)
		return mr
	}
	mr.Delivered = true
	return mr
}
