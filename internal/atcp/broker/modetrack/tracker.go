// Package modetrack observes PTY output and maintains the set of DEC private
// modes the child process has toggled.
//
// The broker uses this state to answer three questions:
//
//  1. Should a terminal.paste intent be wrapped in bracketed-paste escapes?
//  2. Is the child currently on the alt-screen (e.g. running a full-screen
//     TUI) — in which case free-form text injection is likely unsafe?
//  3. What is the child's application/cursor-keys mode — used to format
//     arrow-key sequences correctly.
//
// The parser is a deliberately small, allocation-free CSI recogniser. It
// ignores everything it doesn't understand; unrecognised bytes pass through
// without side effects. The tracker is safe for concurrent readers — Snapshot
// and mode-change subscribers may run on any goroutine — but Feed must be
// called from a single writer (typically the Session's read goroutine).
package modetrack

import (
	"strings"
	"sync"
	"time"
)

// Mode captures the subset of DEC private modes the tracker understands.
type Mode struct {
	// BracketedPaste is DEC private mode 2004 — when true, the child has asked
	// the terminal to wrap pastes in ESC[200~…ESC[201~.
	BracketedPaste bool
	// AltScreen is DEC private mode 1049 (or legacy 47) — when true, the child
	// is rendering on the alternate screen buffer.
	AltScreen bool
	// ApplicationCursorKeys is DEC private mode 1 — when true, arrow keys emit
	// ESC O A/B/C/D rather than ESC [ A/B/C/D.
	ApplicationCursorKeys bool
}

// Change describes a single mode transition. Consumers subscribe via the
// tracker's Subscribe method.
type Change struct {
	Mode      Mode
	Timestamp time.Time
}

// Tracker is the stateful observer. Feed it PTY output bytes and it maintains
// a mode snapshot plus broadcasts changes to subscribers.
type Tracker struct {
	mu    sync.Mutex
	mode  Mode
	state parserState
	// buf holds parameter digits and intermediates for the in-progress CSI.
	// Bounded to 32 bytes to avoid unbounded growth on malformed input.
	buf      [32]byte
	bufLen   int
	private  bool
	lastSeen time.Time

	subsMu sync.RWMutex
	subs   map[int]chan Change
	nextID int
}

// parserState is the position in the ECMA-48 escape recogniser.
type parserState uint8

const (
	stateGround parserState = iota
	stateEscape
	stateCSI
)

// maxBuf is the largest parameter+intermediate run we will buffer before
// aborting the current CSI. Real-world sequences are always <20 bytes.
const maxBuf = 32

// New constructs a Tracker in the default (all-zero) mode.
func New() *Tracker {
	return &Tracker{subs: make(map[int]chan Change)}
}

// Snapshot returns the current mode state. Safe to call from any goroutine.
func (t *Tracker) Snapshot() Mode {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.mode
}

// LastSeen returns the wall clock of the last Feed call, or the zero time if
// Feed was never called.
func (t *Tracker) LastSeen() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastSeen
}

// Subscribe returns a channel that receives a Change on every mode transition
// plus a cancel func. The buffer is large enough that transient backpressure
// on one subscriber does not stall Feed; a full channel drops the change.
func (t *Tracker) Subscribe() (<-chan Change, func()) {
	ch := make(chan Change, 16)
	t.subsMu.Lock()
	id := t.nextID
	t.nextID++
	t.subs[id] = ch
	t.subsMu.Unlock()
	cancel := func() {
		t.subsMu.Lock()
		if c, ok := t.subs[id]; ok {
			delete(t.subs, id)
			close(c)
		}
		t.subsMu.Unlock()
	}
	return ch, cancel
}

// Feed consumes a slice of PTY output bytes and updates internal state. It
// never blocks on subscribers. Every mode transition — even multiple within
// a single Feed call — is broadcast as a separate Change so observers can
// see set/reset pairs that arrive in one write.
func (t *Tracker) Feed(b []byte) {
	if len(b) == 0 {
		return
	}
	var changes []Mode
	t.mu.Lock()
	t.lastSeen = time.Now().UTC()
	prev := t.mode
	for _, c := range b {
		t.step(c)
		if t.mode != prev {
			changes = append(changes, t.mode)
			prev = t.mode
		}
	}
	t.mu.Unlock()
	if len(changes) == 0 {
		return
	}
	now := time.Now().UTC()
	for _, m := range changes {
		t.fanout(Change{Mode: m, Timestamp: now})
	}
}

// step advances the parser by a single byte. Called with t.mu held.
func (t *Tracker) step(c byte) {
	switch t.state {
	case stateGround:
		if c == 0x1B {
			t.state = stateEscape
		}
	case stateEscape:
		switch c {
		case '[':
			t.state = stateCSI
			t.bufLen = 0
			t.private = false
		case ']':
			// OSC: skip until ST or BEL. We don't care about OSC contents.
			t.state = stateGround
		default:
			// Two-byte escape (ESC x). Not relevant to mode tracking.
			t.state = stateGround
		}
	case stateCSI:
		// CSI parameters: 0x30-0x3F (digits, ';', '?', etc.), intermediates
		// 0x20-0x2F, final byte 0x40-0x7E.
		switch {
		case c == '?' && t.bufLen == 0:
			t.private = true
		case c >= 0x30 && c <= 0x3F:
			t.bufferCSI(c)
		case c >= 0x20 && c <= 0x2F:
			t.bufferCSI(c)
		case c >= 0x40 && c <= 0x7E:
			t.finishCSI(c)
			t.state = stateGround
		default:
			// Bad byte inside a CSI — abort.
			t.state = stateGround
		}
	}
}

func (t *Tracker) bufferCSI(c byte) {
	if t.bufLen >= maxBuf {
		t.state = stateGround
		t.bufLen = 0
		return
	}
	t.buf[t.bufLen] = c
	t.bufLen++
}

// finishCSI interprets a completed CSI sequence. We only honour DEC private
// SET (`h`) and RESET (`l`) with the `?` prefix, because every mode we track
// is a DEC private mode.
func (t *Tracker) finishCSI(final byte) {
	defer func() { t.bufLen = 0 }()
	if !t.private || (final != 'h' && final != 'l') {
		return
	}
	enable := final == 'h'
	params := string(t.buf[:t.bufLen])
	for _, p := range strings.Split(params, ";") {
		switch p {
		case "1":
			t.mode.ApplicationCursorKeys = enable
		case "47", "1047", "1049":
			t.mode.AltScreen = enable
		case "2004":
			t.mode.BracketedPaste = enable
		}
	}
}

// fanout delivers a Change to every subscriber. Non-blocking — a full
// subscriber channel drops the change rather than stalling Feed.
func (t *Tracker) fanout(c Change) {
	t.subsMu.RLock()
	defer t.subsMu.RUnlock()
	for _, ch := range t.subs {
		select {
		case ch <- c:
		default:
		}
	}
}
