// Package generictty is the default adapter profile for broker-owned PTYs.
//
// It compiles typed ATCP terminal intents — text, key, submit, paste,
// write_bytes — into byte sequences suitable for xterm-compatible terminals.
// Adapter-specific profiles (posix-shell, node-readline, claude) will wrap or
// override this baseline via their own packages.
package generictty

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/joshka0/foxctl/internal/atcp/intents"
)

// Errors returned by the adapter's Compile* methods.
var (
	ErrEmptyText             = errors.New("atcp adapter: text is empty")
	ErrUnsupportedEncoding   = errors.New("atcp adapter: unsupported encoding")
	ErrWriteBytesNotAllowed  = errors.New("atcp adapter: write_bytes capability is disabled")
	ErrRepeatOutOfRange      = errors.New("atcp adapter: repeat must be >= 0")
)

// Adapter is a stateful compiler. All state is local to the adapter value —
// it holds no references to sessions — so each session typically owns one
// adapter instance and feeds observed terminal mode changes back in via
// SetBracketedPasteEnabled.
type Adapter struct {
	// BracketedPasteEnabled tracks whether the child has enabled bracketed
	// paste via DECSET 2004. Updated from the ModeTracker in a later phase.
	BracketedPasteEnabled bool

	// AllowWriteBytes enables the TerminalWriteBytes escape hatch.
	// Default false — most sessions should reject raw writes because they
	// bypass lease/safe-prompt policy.
	AllowWriteBytes bool

	// DefaultSubmitKey is used when a TerminalSubmit intent leaves SubmitKey
	// blank. Defaults to "Enter" via the package-level constant.
	DefaultSubmitKey string
}

// DefaultSubmitKey is the fallback submit key name when an intent omits it.
const DefaultSubmitKey = "Enter"

// New constructs an Adapter with sensible defaults: bracketed paste disabled
// (will be toggled by the mode tracker), write_bytes denied, Enter as default
// submit key.
func New() *Adapter {
	return &Adapter{DefaultSubmitKey: DefaultSubmitKey}
}

// SetBracketedPasteEnabled updates the adapter's view of DEC private mode 2004
// on the owning session. The broker's mode tracker is expected to call this on
// every transition.
func (a *Adapter) SetBracketedPasteEnabled(enabled bool) {
	a.BracketedPasteEnabled = enabled
}

// CompileText turns a TerminalText intent into bytes.
//
// Empty text is rejected: text intents are not a valid way to poke the
// terminal with no payload (use terminal.key for that). Non-UTF-8 encodings
// are rejected in v0.1.
func (a *Adapter) CompileText(intent intents.TerminalText) ([]byte, error) {
	if intent.Text == "" {
		return nil, ErrEmptyText
	}
	if enc := strings.ToLower(strings.TrimSpace(intent.Encoding)); enc != "" && enc != "utf-8" && enc != "utf8" {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedEncoding, intent.Encoding)
	}
	if !utf8.ValidString(intent.Text) {
		return nil, fmt.Errorf("%w: text is not valid UTF-8", ErrUnsupportedEncoding)
	}
	return []byte(intent.Text), nil
}

// CompileKey turns a TerminalKey intent into bytes.
//
// Repeat == 0 is treated as 1 (spec §9.3). Repeat > 1 replicates the compiled
// sequence. Unknown key names return ErrUnknownKey; invalid modifiers return
// ErrInvalidModifier.
func (a *Adapter) CompileKey(intent intents.TerminalKey) ([]byte, error) {
	if intent.Repeat < 0 {
		return nil, ErrRepeatOutOfRange
	}
	mods, err := ParseModifiers(intent.Modifiers)
	if err != nil {
		return nil, err
	}
	seq, err := compileKey(intent.Key, mods)
	if err != nil {
		return nil, err
	}
	reps := intent.Repeat
	if reps == 0 {
		reps = 1
	}
	if reps == 1 {
		return seq, nil
	}
	out := make([]byte, 0, len(seq)*reps)
	for i := 0; i < reps; i++ {
		out = append(out, seq...)
	}
	return out, nil
}

// CompileSubmit turns a TerminalSubmit intent into bytes: the UTF-8 text
// followed by the compiled submit key sequence (default: Enter → 0x0D).
//
// Text may be empty — a "press submit on an empty prompt" intent is a valid
// way to poke an agent loop.
func (a *Adapter) CompileSubmit(intent intents.TerminalSubmit) ([]byte, error) {
	if s := strings.TrimSpace(intent.Text); s != "" {
		if !utf8.ValidString(intent.Text) {
			return nil, fmt.Errorf("%w: text is not valid UTF-8", ErrUnsupportedEncoding)
		}
	}
	submit := intent.SubmitKey
	if submit == "" {
		submit = a.defaultSubmitKey()
	}
	keyBytes, err := compileKey(submit, 0)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(intent.Text)+len(keyBytes))
	out = append(out, []byte(intent.Text)...)
	out = append(out, keyBytes...)
	return out, nil
}

// CompilePaste turns a TerminalPaste intent into bytes. Bracketed-paste
// wrapping is applied based on the intent's Bracketed field and the adapter's
// current BracketedPasteEnabled state.
func (a *Adapter) CompilePaste(intent intents.TerminalPaste) ([]byte, error) {
	if intent.Text == "" {
		return nil, ErrEmptyText
	}
	if !utf8.ValidString(intent.Text) {
		return nil, fmt.Errorf("%w: text is not valid UTF-8", ErrUnsupportedEncoding)
	}

	wrap := shouldBracket(intents.ParseBracketed(intent.Bracketed), a.BracketedPasteEnabled)
	out := make([]byte, 0, len(intent.Text)+len(BracketedPasteStart)+len(BracketedPasteEnd)+1)
	if wrap {
		out = append(out, BracketedPasteStart...)
	}
	out = append(out, []byte(intent.Text)...)
	if wrap {
		out = append(out, BracketedPasteEnd...)
	}
	if intent.SubmitAfter {
		enter, _ := compileKey(a.defaultSubmitKey(), 0)
		out = append(out, enter...)
	}
	return out, nil
}

// CompileWriteBytes is the raw escape hatch. Returns ErrWriteBytesNotAllowed
// unless the adapter has AllowWriteBytes set.
func (a *Adapter) CompileWriteBytes(intent intents.TerminalWriteBytes) ([]byte, error) {
	if !a.AllowWriteBytes {
		return nil, ErrWriteBytesNotAllowed
	}
	out := make([]byte, len(intent.Bytes))
	copy(out, intent.Bytes)
	return out, nil
}

func (a *Adapter) defaultSubmitKey() string {
	if a.DefaultSubmitKey == "" {
		return DefaultSubmitKey
	}
	return a.DefaultSubmitKey
}

func shouldBracket(policy intents.BracketedPolicy, enabled bool) bool {
	switch policy {
	case intents.BracketedForce:
		return true
	case intents.BracketedOff:
		return false
	case intents.BracketedAuto:
		return enabled
	}
	return false
}
