// Package addressing parses and constructs ATCP target URIs of the form
// <scheme>:<id>. See docs/atcp/ATCP-v0.1.md §8.
//
// Known schemes: room, session, agent, inbox, scheduler. Unknown schemes are
// rejected so the broker can refuse state-mutating intents with an invalid
// addressing envelope field before they reach any subsystem.
package addressing

import (
	"errors"
	"fmt"
	"strings"
)

// Scheme identifies the kind of entity an address points at.
type Scheme string

// Known ATCP address schemes.
const (
	SchemeRoom      Scheme = "room"
	SchemeSession   Scheme = "session"
	SchemeAgent     Scheme = "agent"
	SchemeInbox     Scheme = "inbox"
	SchemeScheduler Scheme = "scheduler"
)

// Address is a parsed ATCP target URI.
type Address struct {
	Scheme Scheme
	ID     string
}

// Errors surfaced by the parser.
var (
	ErrEmptyAddress   = errors.New("atcp address: empty input")
	ErrMissingScheme  = errors.New("atcp address: missing scheme delimiter ':'")
	ErrEmptyScheme    = errors.New("atcp address: scheme is empty")
	ErrEmptyID        = errors.New("atcp address: id is empty")
	ErrUnknownScheme  = errors.New("atcp address: unknown scheme")
	ErrWrongScheme    = errors.New("atcp address: scheme does not match expected")
	ErrInvalidIDChars = errors.New("atcp address: id contains forbidden characters")
)

// Parse parses a target URI of the form "<scheme>:<id>" into an Address.
// Scheme lookups are case-sensitive to keep routing deterministic.
func Parse(raw string) (Address, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Address{}, ErrEmptyAddress
	}
	idx := strings.IndexByte(s, ':')
	if idx < 0 {
		return Address{}, ErrMissingScheme
	}
	scheme := s[:idx]
	id := s[idx+1:]
	if scheme == "" {
		return Address{}, ErrEmptyScheme
	}
	if id == "" {
		return Address{}, ErrEmptyID
	}
	if !validID(id) {
		return Address{}, fmt.Errorf("%w: %q", ErrInvalidIDChars, id)
	}
	parsed := Scheme(scheme)
	if !IsKnown(parsed) {
		return Address{}, fmt.Errorf("%w: %q", ErrUnknownScheme, scheme)
	}
	return Address{Scheme: parsed, ID: id}, nil
}

// MustParse is the test-friendly Parse variant that panics on failure.
func MustParse(raw string) Address {
	a, err := Parse(raw)
	if err != nil {
		panic(err)
	}
	return a
}

// ParseExpect parses a target URI and requires a specific scheme. This is the
// workhorse used by intent handlers to assert that target addresses resolve to
// the right entity kind (e.g. room.join requires a room target).
func ParseExpect(raw string, want Scheme) (Address, error) {
	a, err := Parse(raw)
	if err != nil {
		return Address{}, err
	}
	if a.Scheme != want {
		return Address{}, fmt.Errorf("%w: got %s, want %s", ErrWrongScheme, a.Scheme, want)
	}
	return a, nil
}

// String renders the address back into its canonical "<scheme>:<id>" form.
func (a Address) String() string {
	if a.Scheme == "" || a.ID == "" {
		return ""
	}
	return string(a.Scheme) + ":" + a.ID
}

// IsKnown reports whether s is one of the registered ATCP schemes.
func IsKnown(s Scheme) bool {
	switch s {
	case SchemeRoom, SchemeSession, SchemeAgent, SchemeInbox, SchemeScheduler:
		return true
	}
	return false
}

// KnownSchemes returns the registered scheme list in a deterministic order,
// useful for tests and CLI help output.
func KnownSchemes() []Scheme {
	return []Scheme{SchemeRoom, SchemeSession, SchemeAgent, SchemeInbox, SchemeScheduler}
}

// Convenience constructors so callers do not hand-concatenate strings.

// Room returns a room address for the given id.
func Room(id string) Address { return Address{Scheme: SchemeRoom, ID: id} }

// Session returns a session address for the given id.
func Session(id string) Address { return Address{Scheme: SchemeSession, ID: id} }

// Agent returns an agent address for the given id.
func Agent(id string) Address { return Address{Scheme: SchemeAgent, ID: id} }

// Inbox returns an inbox address for the given id.
func Inbox(id string) Address { return Address{Scheme: SchemeInbox, ID: id} }

// Scheduler returns a scheduler address for the given id.
func Scheduler(id string) Address { return Address{Scheme: SchemeScheduler, ID: id} }

// validID rejects ids that contain whitespace or a literal colon. The permissive
// character set keeps migration easy (ULIDs, workspace-scoped ids, etc.) while
// still ruling out obvious mistakes like embedded newlines or a missing scheme.
func validID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r == ':' {
			return false
		}
		switch r {
		case ' ', '\t', '\n', '\r':
			return false
		}
	}
	return true
}
