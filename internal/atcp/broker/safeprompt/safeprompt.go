// Package safeprompt decides whether a PTY session is at a point where it is
// safe to inject input.
//
// The rules are intentionally simple at this stage (spec §5a.8 decision #4):
//
//  1. The session must be running.
//  2. No fresh output for at least IdleWindow milliseconds (the child is
//     waiting on us).
//  3. The tail of the output log matches a regex — the "prompt" indicator.
//     Three-layer resolution is applied by callers; this package only takes
//     a single compiled regex.
//
// Consumers typically call Wait(ctx, opts) which polls these conditions and
// returns Ready / Timeout / SessionDead.
package safeprompt

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/broker/session"
)

// DefaultPromptRegex is the built-in fallback when adapters or rooms do not
// supply a more specific regex. It matches a trailing shell-style prompt.
var DefaultPromptRegex = regexp.MustCompile(`(?m)(?:\$|>|❯)[[:space:]]*$`)

// DefaultIdleWindow is the default "has stopped writing for N" threshold.
const DefaultIdleWindow = 400 * time.Millisecond

// DefaultTailBytes is how many trailing bytes of output the regex is tested
// against. Full output logs can be megabytes; we do not want to run a regex
// across all of it.
const DefaultTailBytes = 4096

// Options tunes a ReadyCheck / Wait call.
type Options struct {
	// Regex is the prompt regex. nil means DefaultPromptRegex.
	Regex *regexp.Regexp
	// IdleWindow is the minimum quiet time before the session is considered
	// ready. Zero means DefaultIdleWindow.
	IdleWindow time.Duration
	// TailBytes bounds how many trailing bytes of output the regex runs
	// against. Zero means DefaultTailBytes.
	TailBytes int
	// RequireNotAltScreen, when true, considers sessions in alt-screen mode as
	// unsafe regardless of prompt match. Default false — alt-screen TUIs like
	// droid/codex/gemini still accept typed input.
	RequireNotAltScreen bool
}

func (o Options) regex() *regexp.Regexp {
	if o.Regex != nil {
		return o.Regex
	}
	return DefaultPromptRegex
}

func (o Options) idle() time.Duration {
	if o.IdleWindow > 0 {
		return o.IdleWindow
	}
	return DefaultIdleWindow
}

func (o Options) tailBytes() int {
	if o.TailBytes > 0 {
		return o.TailBytes
	}
	return DefaultTailBytes
}

// Decision is the outcome of a single Check invocation.
type Decision int

const (
	// Unknown is the zero value; callers should not see it.
	Unknown Decision = iota
	// Ready means all gates pass: idle, prompt matches, not on alt-screen
	// (if required).
	Ready
	// Busy means the session is still actively writing or has not yet shown a
	// prompt.
	Busy
	// Blocked means the session is on alt-screen and RequireNotAltScreen is
	// set, or the session has exited.
	Blocked
)

// String returns a lowercase token suitable for logging.
func (d Decision) String() string {
	switch d {
	case Ready:
		return "ready"
	case Busy:
		return "busy"
	case Blocked:
		return "blocked"
	}
	return "unknown"
}

// ErrSessionDead is returned by Wait when the session exits before becoming
// ready.
var ErrSessionDead = errors.New("atcp safeprompt: session exited before ready")

// ErrTimeout is returned by Wait when ctx is cancelled or deadline reached
// before ready.
var ErrTimeout = errors.New("atcp safeprompt: timed out waiting for ready")

// Check evaluates the session once and returns a Decision plus a short
// diagnostic reason. It does not block.
func Check(s *session.Session, opts Options) (Decision, string) {
	if s == nil {
		return Blocked, "session is nil"
	}
	snap := s.Snapshot()
	if snap.Status != session.StatusRunning {
		return Blocked, "session is not running (status=" + snap.Status.String() + ")"
	}
	mode := s.Tracker().Snapshot()
	if opts.RequireNotAltScreen && mode.AltScreen {
		return Blocked, "session is on alt-screen"
	}

	lastSeen := s.Tracker().LastSeen()
	if lastSeen.IsZero() {
		return Busy, "no output observed yet"
	}
	if since := time.Since(lastSeen); since < opts.idle() {
		return Busy, "output too recent (" + since.String() + ")"
	}

	tail := collectTail(s, opts.tailBytes())
	if !opts.regex().Match(tail) {
		return Busy, "prompt regex did not match tail"
	}
	return Ready, "ok"
}

// Wait polls Check at pollInterval until Ready, ctx is cancelled, or the
// session exits. A pollInterval of zero defaults to IdleWindow/2.
func Wait(ctx context.Context, s *session.Session, opts Options, pollInterval time.Duration) (Decision, string, error) {
	if s == nil {
		return Blocked, "nil session", ErrSessionDead
	}
	if pollInterval <= 0 {
		pollInterval = opts.idle() / 2
		if pollInterval < 25*time.Millisecond {
			pollInterval = 25 * time.Millisecond
		}
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		d, reason := Check(s, opts)
		if d == Ready {
			return Ready, reason, nil
		}
		if d == Blocked {
			// If the session is dead, bail out immediately.
			if snap := s.Snapshot(); snap.Status != session.StatusRunning {
				return Blocked, reason, ErrSessionDead
			}
			// Otherwise Blocked is non-terminal (e.g. waiting for alt-screen
			// to exit) — keep polling.
		}
		select {
		case <-ctx.Done():
			return d, reason, ErrTimeout
		case <-s.Done():
			return Blocked, "session exited", ErrSessionDead
		case <-ticker.C:
		}
	}
}

// collectTail is a thin forwarder to OutputLog.TailBytes. Kept as a
// package-level symbol because the safeprompt tests poke at it when asserting
// that the regex sees what we think it sees.
func collectTail(s *session.Session, n int) []byte {
	return s.Log().TailBytes(n)
}
