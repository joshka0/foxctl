// Package kinds is the canonical registry of Foxprox v0.1 envelope kinds.
//
// Every envelope flowing through the broker carries a kind string such as
// "room.create" or "terminal.submit". This package centralizes:
//
//   - Typed constants for every spec-defined kind.
//   - Kind classification (intent vs event) so the broker can reject intents
//     on read-only ingress and events on write-only ingress.
//   - The target-scheme contract for each intent so handlers can assert that
//     envelope.Target addresses the right entity (e.g. room.join expects a
//     room: scheme, terminal.submit expects a session: scheme).
//
// Registration is the only side-effect: every kind used by the broker must be
// registered here. This gives envelope validation a single source of truth.
package kinds

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/joshka/foxprox/foxprox/addressing"
)

// Kind is the string name of an Foxprox envelope kind.
type Kind string

// Category marks whether a kind is an Intent (requests a state change) or an
// Event (observation emitted by the broker). Intents flow inbound; events flow
// outbound on the SSE stream.
type Category int

const (
	// CategoryIntent is a state-mutating request.
	CategoryIntent Category = iota + 1
	// CategoryEvent is an observation emitted by the broker.
	CategoryEvent
)

// String returns a human-readable category name.
func (c Category) String() string {
	switch c {
	case CategoryIntent:
		return "intent"
	case CategoryEvent:
		return "event"
	}
	return "unknown"
}

// Spec describes a single registered kind.
type Spec struct {
	Kind          Kind
	Category      Category
	TargetSchemes []addressing.Scheme // permissible Target schemes; empty means target is optional
	Description   string
}

// Registered kind constants. Grouped by area for readability; the list tracks
// docs/Foxprox-v0.1.md §§9-15.
const (
	// Session lifecycle.
	SessionCreate   Kind = "session.create"
	SessionDelete   Kind = "session.delete"
	SessionExited   Kind = "session.exited"
	SessionSnapshot Kind = "session.snapshot"

	// Terminal input intents (broker ingress).
	TerminalText       Kind = "terminal.text"
	TerminalKey        Kind = "terminal.key"
	TerminalSubmit     Kind = "terminal.submit"
	TerminalPaste      Kind = "terminal.paste"
	TerminalWriteBytes Kind = "terminal.write_bytes"

	// Terminal observation events (broker egress).
	TerminalOutput         Kind = "terminal.output"
	TerminalModeChanged    Kind = "terminal.mode.changed"
	TerminalScreenSnapshot Kind = "terminal.screen.snapshot"
	TerminalReady          Kind = "terminal.ready"
	TerminalActivity       Kind = "terminal.activity"

	// Leases.
	LeaseAcquire Kind = "lease.acquire"
	LeaseRelease Kind = "lease.release"
	LeaseGranted Kind = "lease.granted"
	LeaseExpired Kind = "lease.expired"

	// Message bus.
	MessageSend         Kind = "message.send"
	MessageDelivered    Kind = "message.delivered"
	MessageAcknowledged Kind = "message.acknowledged"

	// Reminders.
	ReminderSchedule Kind = "reminder.schedule"
	ReminderCancel   Kind = "reminder.cancel"
	ReminderFired    Kind = "reminder.fired"

	// Transactions.
	TransactionRun           Kind = "transaction.run"
	TransactionStepCompleted Kind = "transaction.step.completed"
	TransactionCompleted     Kind = "transaction.completed"

	// Rooms.
	RoomCreate  Kind = "room.create"
	RoomJoin    Kind = "room.join"
	RoomLeave   Kind = "room.leave"
	RoomRebind  Kind = "room.rebind"
	RoomArchive Kind = "room.archive"
	RoomDestroy Kind = "room.destroy"

	RoomMemberJoined  Kind = "room.member.joined"
	RoomMemberLeft    Kind = "room.member.left"
	RoomMemberRebound Kind = "room.member.rebound"

	// Capability negotiation + errors.
	CapabilityReport Kind = "capability.report"
	Error            Kind = "error"
)

// registry is the lookup table populated at init. It is read-only after
// init so concurrent readers do not need synchronisation.
var (
	registryMu sync.RWMutex
	registry   map[Kind]Spec
)

// ErrUnknownKind is returned by Lookup and Validate when an envelope carries a
// kind string that has not been registered.
var ErrUnknownKind = errors.New("foxprox kinds: unknown kind")

// ErrWrongTargetScheme is returned by ValidateTarget when the envelope's
// target address uses a scheme not permitted by the kind's Spec.
var ErrWrongTargetScheme = errors.New("foxprox kinds: target scheme not permitted for kind")

func init() {
	specs := []Spec{
		// Sessions.
		{SessionCreate, CategoryIntent, nil, "Create a new PTY session."},
		{SessionDelete, CategoryIntent, []addressing.Scheme{addressing.SchemeSession}, "Delete an existing session."},
		{SessionExited, CategoryEvent, []addressing.Scheme{addressing.SchemeSession}, "Session process exited."},
		{SessionSnapshot, CategoryEvent, []addressing.Scheme{addressing.SchemeSession}, "Periodic session state snapshot."},

		// Terminal input intents (target = session).
		{TerminalText, CategoryIntent, []addressing.Scheme{addressing.SchemeSession}, "Type literal text into a session."},
		{TerminalKey, CategoryIntent, []addressing.Scheme{addressing.SchemeSession}, "Send a typed key (Enter, Tab, etc.)."},
		{TerminalSubmit, CategoryIntent, []addressing.Scheme{addressing.SchemeSession}, "Type text and submit with configured key."},
		{TerminalPaste, CategoryIntent, []addressing.Scheme{addressing.SchemeSession}, "Paste text with bracketed-paste policy."},
		{TerminalWriteBytes, CategoryIntent, []addressing.Scheme{addressing.SchemeSession}, "Raw byte escape hatch (capability-gated)."},

		// Terminal observations. Room streams fan in session output and retarget
		// terminal.output to room:<id> while preserving session_id in the body.
		{TerminalOutput, CategoryEvent, []addressing.Scheme{addressing.SchemeSession, addressing.SchemeRoom}, "PTY output frame."},
		{TerminalModeChanged, CategoryEvent, []addressing.Scheme{addressing.SchemeSession}, "Terminal mode toggled (bracketed paste, alt screen, ...)."},
		{TerminalScreenSnapshot, CategoryEvent, []addressing.Scheme{addressing.SchemeSession}, "Snapshot of the current screen model."},
		{TerminalReady, CategoryEvent, []addressing.Scheme{addressing.SchemeSession}, "Terminal readiness state changed."},
		{TerminalActivity, CategoryEvent, []addressing.Scheme{addressing.SchemeSession}, "Terminal output heartbeat and delta."},

		// Leases (target = session).
		{LeaseAcquire, CategoryIntent, []addressing.Scheme{addressing.SchemeSession}, "Acquire an input lease for a session scope."},
		{LeaseRelease, CategoryIntent, []addressing.Scheme{addressing.SchemeSession}, "Release a previously acquired lease."},
		{LeaseGranted, CategoryEvent, []addressing.Scheme{addressing.SchemeSession}, "Broker granted a lease."},
		{LeaseExpired, CategoryEvent, []addressing.Scheme{addressing.SchemeSession}, "Lease exceeded TTL without release."},

		// Messaging (target = room or inbox or session).
		{MessageSend, CategoryIntent, []addressing.Scheme{addressing.SchemeRoom, addressing.SchemeInbox, addressing.SchemeSession, addressing.SchemeAgent}, "Send a message to a target."},
		{MessageDelivered, CategoryEvent, nil, "Message delivered to a recipient."},
		{MessageAcknowledged, CategoryEvent, nil, "Recipient acknowledged receipt."},

		// Reminders (target = room, inbox, session, or agent for delivery routing).
		{ReminderSchedule, CategoryIntent, []addressing.Scheme{addressing.SchemeRoom, addressing.SchemeInbox, addressing.SchemeSession, addressing.SchemeAgent}, "Schedule a reminder to fire later."},
		{ReminderCancel, CategoryIntent, []addressing.Scheme{addressing.SchemeScheduler}, "Cancel a pending reminder."},
		{ReminderFired, CategoryEvent, nil, "A reminder has fired."},

		// Transactions (target = session).
		{TransactionRun, CategoryIntent, []addressing.Scheme{addressing.SchemeSession}, "Run a scripted transaction against a session."},
		{TransactionStepCompleted, CategoryEvent, []addressing.Scheme{addressing.SchemeSession}, "Transaction step completed."},
		{TransactionCompleted, CategoryEvent, []addressing.Scheme{addressing.SchemeSession}, "Transaction completed (observed, failed, or rejected)."},

		// Rooms (target = room).
		{RoomCreate, CategoryIntent, []addressing.Scheme{addressing.SchemeRoom}, "Create a new coordination room."},
		{RoomJoin, CategoryIntent, []addressing.Scheme{addressing.SchemeRoom}, "Join a room; optionally auto-spawn a session."},
		{RoomLeave, CategoryIntent, []addressing.Scheme{addressing.SchemeRoom}, "Leave a room; session persists."},
		{RoomRebind, CategoryIntent, []addressing.Scheme{addressing.SchemeRoom}, "Swap a member's session binding."},
		{RoomArchive, CategoryIntent, []addressing.Scheme{addressing.SchemeRoom}, "Archive a room."},
		{RoomDestroy, CategoryIntent, []addressing.Scheme{addressing.SchemeRoom}, "Destroy a room and its member sessions."},

		{RoomMemberJoined, CategoryEvent, []addressing.Scheme{addressing.SchemeRoom}, "A member joined the room."},
		{RoomMemberLeft, CategoryEvent, []addressing.Scheme{addressing.SchemeRoom}, "A member left the room."},
		{RoomMemberRebound, CategoryEvent, []addressing.Scheme{addressing.SchemeRoom}, "A member's session binding changed."},

		// Capability + error.
		{CapabilityReport, CategoryEvent, nil, "Adapter capability report."},
		{Error, CategoryEvent, nil, "Generic error event."},
	}

	registry = make(map[Kind]Spec, len(specs))
	for _, sp := range specs {
		registry[sp.Kind] = sp
	}
}

// Lookup returns the Spec for a kind, or ErrUnknownKind when not registered.
func Lookup(k Kind) (Spec, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	sp, ok := registry[k]
	if !ok {
		return Spec{}, fmt.Errorf("%w: %q", ErrUnknownKind, k)
	}
	return sp, nil
}

// IsRegistered reports whether k has a Spec.
func IsRegistered(k Kind) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	_, ok := registry[k]
	return ok
}

// Validate reports whether the kind is known to the registry.
func Validate(k Kind) error {
	if _, err := Lookup(k); err != nil {
		return err
	}
	return nil
}

// ValidateTarget checks that target (which may be empty) satisfies the kind's
// target-scheme contract. A nil TargetSchemes list means the target is optional
// and any scheme (or empty) is accepted.
func ValidateTarget(k Kind, target string) error {
	sp, err := Lookup(k)
	if err != nil {
		return err
	}
	if len(sp.TargetSchemes) == 0 {
		return nil
	}
	if target == "" {
		return fmt.Errorf("%w: %s requires target", ErrWrongTargetScheme, k)
	}
	addr, err := addressing.Parse(target)
	if err != nil {
		return err
	}
	for _, want := range sp.TargetSchemes {
		if addr.Scheme == want {
			return nil
		}
	}
	return fmt.Errorf("%w: kind %s got %s, want one of %v", ErrWrongTargetScheme, k, addr.Scheme, sp.TargetSchemes)
}

// All returns every registered Spec sorted by kind string. Useful for CLI
// listings, documentation generation, and tests.
func All() []Spec {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Spec, 0, len(registry))
	for _, sp := range registry {
		out = append(out, sp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}
