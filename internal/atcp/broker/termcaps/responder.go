// Package termcaps implements a minimal "fake terminal" responder that
// answers capability queries a child process emits during startup.
//
// Many modern TUI agents (codex, claude-cli, gemini, etc.) probe their
// controlling terminal with escape sequences like OSC 10 ("what's the
// foreground color?") or DSR 6 ("where's the cursor?") early in
// initialisation and block until they receive a reply. A real xterm /
// iterm2 answers automatically; the broker's raw PTY master does not,
// which is why the live-smoke run found codex stuck in its init loop
// with zero input activity.
//
// This package adds a stateful byte-level parser that sits alongside
// modetrack on the PTY output stream. When it recognises a query, it
// emits a canonical response byte sequence the caller writes back to the
// PTY master — the child sees it on stdin as if from a real terminal.
//
// Scope is deliberately narrow. We answer only:
//
//   - OSC 10/11/12 color queries   → fixed black/white defaults
//   - DSR 5 device status          → "I am OK" (CSI 0 n)
//   - DSR 6 cursor position        → row 1, column 1 (CSI 1 ; 1 R)
//   - DA1 device attributes        → "VT100 with AVO" (CSI ? 1 ; 2 c)
//   - DA2 secondary DA             → xterm 420 (CSI > 41 ; 420 ; 0 c)
//   - CSI ? u (kitty kbd query)    → flags=0, legacy mode
//
// Anything else is ignored. Responses are constants — we do not maintain
// a real cursor position or color palette because the broker is headless
// and nothing downstream cares. The point is solely to unblock agents.
//
// Like modetrack.Tracker, Feed must be called from a single writer
// (typically the session's PTY read goroutine). Concurrent reads via the
// exported state surface are safe.
package termcaps

import (
	"strings"
	"sync"
)

// Responder is a stateful parser that converts recognised query bytes
// into canonical response byte sequences. The zero value is not usable;
// construct with New.
type Responder struct {
	mu    sync.Mutex
	state parserState
	// buf holds CSI parameters + intermediates OR the OSC payload being
	// assembled. Bounded to defend against a hostile child that never
	// terminates its sequence.
	buf     [64]byte
	bufLen  int
	private bool // CSI: '?' prefix seen
	gt      bool // CSI: '>' prefix seen (secondary DA)
	// stats is incremented for observability/tests — counts each query
	// type we responded to. Reads via Stats() are concurrency-safe via
	// a separate lock.
	stats Stats
}

// Stats counts recognised queries. Primarily exposed so tests + operators
// can confirm the responder is seeing traffic the way they expect.
type Stats struct {
	OSCColor        uint64 // OSC 10/11/12 responses issued
	DSRStatus       uint64 // CSI 5 n responses issued
	DSRCursor       uint64 // CSI 6 n responses issued
	DA1             uint64 // CSI c responses issued
	DA2             uint64 // CSI > c responses issued
	KittyKeyboard   uint64 // CSI ? u responses issued
	UnknownIgnored  uint64 // completed sequences we did not answer
}

// parserState is the position in the ECMA-48 escape recogniser. Mirrors
// modetrack.parserState intentionally — this package has its own copy so
// a future modetrack refactor can't silently change termcaps semantics.
type parserState uint8

const (
	stateGround parserState = iota
	stateEscape
	stateCSI
	stateOSC
	stateOSCEsc
)

// maxBuf caps the parameter / OSC-payload run we buffer before aborting
// the in-progress sequence. Real sequences are always far below this.
const maxBuf = 64

// Canonical responses. All ST-terminated responses use the two-byte
// string terminator (ESC '\\') rather than BEL, matching what xterm
// normally emits — keeps downstream parsers that prefer ST-only happy.
var (
	// OSC 10 (fg) — solid white. "rgb:ffff/ffff/ffff" is the
	// xterm-canonical 16-bit-per-channel format.
	responseOSC10 = []byte("\x1b]10;rgb:ffff/ffff/ffff\x1b\\")
	// OSC 11 (bg) — solid black.
	responseOSC11 = []byte("\x1b]11;rgb:0000/0000/0000\x1b\\")
	// OSC 12 (cursor) — solid white.
	responseOSC12 = []byte("\x1b]12;rgb:ffff/ffff/ffff\x1b\\")

	// CSI 0 n — device is OK.
	responseDSRStatus = []byte("\x1b[0n")
	// CSI 1 ; 1 R — fake cursor at top-left. Good enough for init-time
	// probes; real cursor tracking would need a full VT engine (plan
	// Phase 2).
	responseDSRCursor = []byte("\x1b[1;1R")

	// CSI ? 1 ; 2 c — "VT100 with advanced video option". Minimal
	// truthful-looking answer that satisfies clients doing DA1 feature
	// detection.
	responseDA1 = []byte("\x1b[?1;2c")
	// CSI > 41 ; 420 ; 0 c — "I am xterm version 420, no cartridge".
	// The "41" terminal ID is xterm's documented value.
	responseDA2 = []byte("\x1b[>41;420;0c")

	// CSI ? 0 u — legacy keyboard, no kitty flags active. Clients that
	// push flags with CSI > n u and then query with CSI ? u expect an
	// answer telling them what flags are currently on. We report 0 so
	// they fall back to legacy Enter / modifier encodings — which is
	// exactly what the rest of the broker (CompileKey / CompileSubmit)
	// produces today. Lying higher would break the input path.
	responseKittyKbd = []byte("\x1b[?0u")
)

// New constructs a Responder in the ground state.
func New() *Responder { return &Responder{} }

// Feed consumes a slice of PTY output bytes and returns any response
// sequences the caller should write back to the PTY master. Multiple
// queries in a single Feed produce multiple response byte slices; the
// slices returned share no backing memory with the caller's input.
//
// Feed is the hot path on the read goroutine, so the common case
// (ground-state bytes with no escape codes) allocates nothing and returns
// a nil slice.
func (r *Responder) Feed(b []byte) [][]byte {
	if len(b) == 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var responses [][]byte
	for _, c := range b {
		if resp := r.step(c); resp != nil {
			responses = append(responses, resp)
		}
	}
	return responses
}

// Stats returns a snapshot of the counters. Cheap; suitable for polling
// from a health handler.
func (r *Responder) Stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats
}

// step advances the parser by a single byte and returns a non-nil
// response if this byte completed a recognised query. Called with r.mu
// held.
func (r *Responder) step(c byte) []byte {
	switch r.state {
	case stateGround:
		if c == 0x1B {
			r.state = stateEscape
		}
		return nil

	case stateEscape:
		switch c {
		case '[':
			r.state = stateCSI
			r.bufLen = 0
			r.private = false
			r.gt = false
		case ']':
			r.state = stateOSC
			r.bufLen = 0
		default:
			// Two-byte escape we don't care about. Drop to ground.
			r.state = stateGround
		}
		return nil

	case stateCSI:
		// Parameter-prefix bytes (private markers). Only valid as the
		// very first byte after CSI.
		if r.bufLen == 0 {
			switch c {
			case '?':
				r.private = true
				return nil
			case '>':
				r.gt = true
				return nil
			}
		}
		// Parameter bytes 0x30..0x3F, intermediates 0x20..0x2F.
		switch {
		case c >= 0x30 && c <= 0x3F, c >= 0x20 && c <= 0x2F:
			r.bufferCSI(c)
			return nil
		case c >= 0x40 && c <= 0x7E:
			resp := r.finishCSI(c)
			r.state = stateGround
			return resp
		default:
			// Corrupt CSI — abort back to ground.
			r.state = stateGround
			r.bufLen = 0
			return nil
		}

	case stateOSC:
		switch c {
		case 0x07: // BEL — OSC terminator
			resp := r.finishOSC()
			r.state = stateGround
			return resp
		case 0x1B:
			r.state = stateOSCEsc
			return nil
		default:
			r.bufferOSC(c)
			return nil
		}

	case stateOSCEsc:
		if c == '\\' {
			resp := r.finishOSC()
			r.state = stateGround
			return resp
		}
		// Lone ESC inside OSC — rare but harmless; resume collection.
		// Do NOT re-buffer the ESC; it's part of the terminator we
		// just abandoned.
		r.state = stateOSC
		return nil
	}
	return nil
}

func (r *Responder) bufferCSI(c byte) {
	if r.bufLen >= maxBuf {
		// Give up on this sequence; don't let a hostile child pin us.
		r.state = stateGround
		r.bufLen = 0
		return
	}
	r.buf[r.bufLen] = c
	r.bufLen++
}

func (r *Responder) bufferOSC(c byte) {
	if r.bufLen >= maxBuf {
		r.state = stateGround
		r.bufLen = 0
		return
	}
	r.buf[r.bufLen] = c
	r.bufLen++
}

// finishCSI dispatches on the CSI final byte + buffered parameters. Only
// query-flavour sequences produce responses; SGR and cursor-movement
// sequences are ignored.
func (r *Responder) finishCSI(final byte) []byte {
	defer func() {
		r.bufLen = 0
		r.private = false
		r.gt = false
	}()
	params := string(r.buf[:r.bufLen])

	switch final {
	case 'n':
		// Device status. Private/gt variants exist (CSI ? 6 n for
		// DECXCPR) but we keep it simple and answer only CSI 5 n and
		// CSI 6 n, the two a typical TUI emits.
		if r.private || r.gt {
			r.stats.UnknownIgnored++
			return nil
		}
		switch params {
		case "5":
			r.stats.DSRStatus++
			return responseDSRStatus
		case "6":
			r.stats.DSRCursor++
			return responseDSRCursor
		}
	case 'c':
		// Device attributes. CSI c == CSI 0 c == DA1. CSI > c == DA2.
		if r.gt {
			// DA2: we accept both "> c" (empty params) and "> 0 c".
			if params == "" || params == "0" {
				r.stats.DA2++
				return responseDA2
			}
		} else if !r.private {
			// DA1: CSI c / CSI 0 c. A private "? c" final would be
			// something else entirely (e.g. tertiary DA variants) —
			// ignore.
			if params == "" || params == "0" {
				r.stats.DA1++
				return responseDA1
			}
		}
	case 'u':
		// CSI ? u — kitty keyboard protocol query. CSI > n u is the
		// "push flags" form which modifies state but expects no reply.
		// We only answer the ? variant.
		if r.private && params == "" {
			r.stats.KittyKeyboard++
			return responseKittyKbd
		}
	}
	r.stats.UnknownIgnored++
	return nil
}

// finishOSC inspects the OSC payload and returns a response for the
// color queries we recognise.
func (r *Responder) finishOSC() []byte {
	defer func() { r.bufLen = 0 }()
	payload := string(r.buf[:r.bufLen])
	// OSC "N;?" — query form for Ps in {10, 11, 12}. Real xterm honours
	// many more (17 selection bg, 19 cursor fg, etc.) but those aren't
	// worth supporting until we see an agent that sends them.
	switch {
	case strings.HasPrefix(payload, "10;?"):
		r.stats.OSCColor++
		return responseOSC10
	case strings.HasPrefix(payload, "11;?"):
		r.stats.OSCColor++
		return responseOSC11
	case strings.HasPrefix(payload, "12;?"):
		r.stats.OSCColor++
		return responseOSC12
	}
	r.stats.UnknownIgnored++
	return nil
}
