// Package intents defines typed request bodies for Foxprox terminal input
// intents (text, key, submit, paste, write_bytes).
//
// These types mirror docs/types.go and are the canonical Go shape that
// adapters compile into PTY byte streams. They live in their own package so
// transport decoders, router, and adapter implementations share a single
// definition without import cycles.
package intents

// TerminalText requests literal text to be typed into a session.
//
// Encoding defaults to UTF-8 when empty. Non-UTF-8 encodings are not yet
// supported by the generic-tty adapter and will be rejected at compile time.
type TerminalText struct {
	Text     string `json:"text"`
	Encoding string `json:"encoding,omitempty"`
	LeaseID  string `json:"lease_id,omitempty"`
}

// TerminalKey requests a typed key press (Enter, Tab, arrow, Ctrl+C, ...).
//
// Key is the symbolic name; Modifiers is a set of zero or more of
// {"ctrl", "alt", "shift", "meta"}. Repeat > 1 sends the compiled byte sequence
// N times.
type TerminalKey struct {
	Key       string   `json:"key"`
	Modifiers []string `json:"modifiers,omitempty"`
	Repeat    int      `json:"repeat,omitempty"`
	LeaseID   string   `json:"lease_id,omitempty"`
}

// TerminalSubmit is the canonical "type text and press a submit key" intent.
// It is the vehicle the router uses to deliver a message to an agent pane.
//
// SubmitKey defaults to "Enter" when empty. Mode is an adapter hint — typed,
// paste, paced, literal — used only by adapters that distinguish paced
// typing from instant input.
type TerminalSubmit struct {
	Text      string `json:"text"`
	SubmitKey string `json:"submit_key,omitempty"`
	Mode      string `json:"mode,omitempty"`
	LeaseID   string `json:"lease_id,omitempty"`
}

// TerminalPaste requests text to be pasted. The broker consults its mode
// tracker to decide whether to wrap in bracketed-paste escape sequences
// (ESC[200~ ... ESC[201~).
//
// Bracketed may be "auto" (wrap iff child enabled bracketed-paste), "force"
// (always wrap), or "off" (never wrap). Empty means "auto".
type TerminalPaste struct {
	Text        string `json:"text"`
	SubmitAfter bool   `json:"submit_after,omitempty"`
	Bracketed   string `json:"bracketed,omitempty"`
	LeaseID     string `json:"lease_id,omitempty"`
}

// TerminalWriteBytes is the raw escape hatch. The broker only honours it on
// sessions whose adapter has the "write_bytes" capability enabled, and it is
// lease-gated like any other input intent.
type TerminalWriteBytes struct {
	Bytes   []byte `json:"bytes"`
	LeaseID string `json:"lease_id,omitempty"`
}

// BracketedPolicy is the enum representation of TerminalPaste.Bracketed.
// Callers may convert via ParseBracketed for dispatch.
type BracketedPolicy int

// BracketedPolicy values.
const (
	BracketedAuto BracketedPolicy = iota
	BracketedForce
	BracketedOff
)

// ParseBracketed converts a JSON value into a BracketedPolicy. Unknown
// strings default to BracketedAuto.
func ParseBracketed(s string) BracketedPolicy {
	switch s {
	case "force":
		return BracketedForce
	case "off":
		return BracketedOff
	case "", "auto":
		return BracketedAuto
	}
	return BracketedAuto
}
