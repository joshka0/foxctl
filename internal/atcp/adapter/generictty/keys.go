package generictty

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// ErrUnknownKey is returned by CompileKey when the name does not match any
// known entry in the key table. Adapters may shadow the generic table with
// their own overrides (e.g. posix-shell disambiguates Backspace as 0x08).
var ErrUnknownKey = errors.New("atcp adapter: unknown key name")

// ErrInvalidModifier is returned when a Modifiers element is unrecognised.
var ErrInvalidModifier = errors.New("atcp adapter: invalid modifier")

// ErrModifierNotApplicable means the modifier set cannot be combined with the
// requested key (e.g. "ctrl" on a non-letter that the adapter has no mapping
// for, or unsupported "meta" on a printable).
var ErrModifierNotApplicable = errors.New("atcp adapter: modifier not applicable to this key")

// BracketedPasteStart is the xterm-documented escape sequence that tells the
// child process a paste is beginning.
var BracketedPasteStart = []byte{0x1B, '[', '2', '0', '0', '~'}

// BracketedPasteEnd terminates a bracketed paste block.
var BracketedPasteEnd = []byte{0x1B, '[', '2', '0', '1', '~'}

// keyEntry describes a single entry in the key table.
type keyEntry struct {
	bytes []byte
	// If shiftBytes != nil, applying shift swaps the emitted sequence. This is
	// rare but matches xterm for Tab (shift+Tab = ESC[Z).
	shiftBytes []byte
}

// keyTable maps canonical key names (lower-case) to their bytes. Aliases are
// resolved via keyAliases before lookup so callers may use either "Enter" or
// "RETURN".
var keyTable = map[string]keyEntry{
	"enter":     {bytes: []byte{0x0D}},
	"linefeed":  {bytes: []byte{0x0A}},
	"tab":       {bytes: []byte{0x09}, shiftBytes: []byte{0x1B, '[', 'Z'}},
	"backspace": {bytes: []byte{0x7F}},
	"space":     {bytes: []byte{0x20}},
	"escape":    {bytes: []byte{0x1B}},

	"up":    {bytes: []byte{0x1B, '[', 'A'}},
	"down":  {bytes: []byte{0x1B, '[', 'B'}},
	"right": {bytes: []byte{0x1B, '[', 'C'}},
	"left":  {bytes: []byte{0x1B, '[', 'D'}},

	"home":     {bytes: []byte{0x1B, '[', 'H'}},
	"end":      {bytes: []byte{0x1B, '[', 'F'}},
	"pageup":   {bytes: []byte{0x1B, '[', '5', '~'}},
	"pagedown": {bytes: []byte{0x1B, '[', '6', '~'}},
	"insert":   {bytes: []byte{0x1B, '[', '2', '~'}},
	"delete":   {bytes: []byte{0x1B, '[', '3', '~'}},

	"f1":  {bytes: []byte{0x1B, 'O', 'P'}},
	"f2":  {bytes: []byte{0x1B, 'O', 'Q'}},
	"f3":  {bytes: []byte{0x1B, 'O', 'R'}},
	"f4":  {bytes: []byte{0x1B, 'O', 'S'}},
	"f5":  {bytes: []byte{0x1B, '[', '1', '5', '~'}},
	"f6":  {bytes: []byte{0x1B, '[', '1', '7', '~'}},
	"f7":  {bytes: []byte{0x1B, '[', '1', '8', '~'}},
	"f8":  {bytes: []byte{0x1B, '[', '1', '9', '~'}},
	"f9":  {bytes: []byte{0x1B, '[', '2', '0', '~'}},
	"f10": {bytes: []byte{0x1B, '[', '2', '1', '~'}},
	"f11": {bytes: []byte{0x1B, '[', '2', '3', '~'}},
	"f12": {bytes: []byte{0x1B, '[', '2', '4', '~'}},
}

// keyAliases lets users spell keys the way they think of them. Keys are
// normalised to lower-case before lookup in this table.
var keyAliases = map[string]string{
	"return":      "enter",
	"cr":          "enter",
	"newline":     "linefeed",
	"lf":          "linefeed",
	"esc":         "escape",
	"del":         "delete",
	"ins":         "insert",
	"pgup":        "pageup",
	"pgdn":        "pagedown",
	"arrowup":     "up",
	"arrowdown":   "down",
	"arrowleft":   "left",
	"arrowright":  "right",
	"bs":          "backspace",
	"backtab":     "tab",
}

// Modifier is a structured form of the free-form Modifiers list. Only the
// flags set by ParseModifiers are applicable; callers must not construct
// these manually.
type Modifier uint8

const (
	// ModCtrl applies Ctrl to the key.
	ModCtrl Modifier = 1 << iota
	// ModAlt prefixes the resulting sequence with ESC.
	ModAlt
	// ModShift toggles shift-specific sequences when available.
	ModShift
	// ModMeta is accepted for spec parity but currently compiles identically to
	// ModAlt on terminals that share the same encoding.
	ModMeta
)

// ParseModifiers validates and packs a list of modifier names into a Modifier.
func ParseModifiers(mods []string) (Modifier, error) {
	var m Modifier
	for _, raw := range mods {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "ctrl", "control":
			m |= ModCtrl
		case "alt", "option":
			m |= ModAlt
		case "shift":
			m |= ModShift
		case "meta", "cmd", "command":
			m |= ModMeta
		case "":
			continue
		default:
			return 0, fmt.Errorf("%w: %q", ErrInvalidModifier, raw)
		}
	}
	return m, nil
}

// compileKey returns the byte sequence for a named key with modifiers applied.
// It is exported via the package-level CompileKey and reused internally by
// CompileSubmit when choosing the submit key.
func compileKey(name string, mods Modifier) ([]byte, error) {
	if name == "" {
		return nil, ErrUnknownKey
	}
	lower := strings.ToLower(strings.TrimSpace(name))
	if alias, ok := keyAliases[lower]; ok {
		lower = alias
	}

	// Ctrl+<letter> maps to 0x01-0x1A directly; allow Shift to be ignored.
	if mods&ModCtrl != 0 && len(lower) == 1 && unicode.IsLetter(rune(lower[0])) {
		b := byte(lower[0]-'a') + 0x01
		out := []byte{b}
		return withAlt(out, mods), nil
	}

	entry, ok := keyTable[lower]
	if !ok {
		// Single printable character fallback (e.g. "a"). Ctrl was handled above.
		if len(lower) == 1 {
			r := rune(name[0])
			if r >= 0x20 && r < 0x7F {
				return withAlt([]byte{byte(r)}, mods), nil
			}
		}
		return nil, fmt.Errorf("%w: %q", ErrUnknownKey, name)
	}

	bytes := entry.bytes
	if mods&ModShift != 0 && entry.shiftBytes != nil {
		bytes = entry.shiftBytes
	}

	// Ctrl on non-letters is only valid when the table entry explicitly
	// supports it. We have none, so reject loudly.
	if mods&ModCtrl != 0 {
		return nil, fmt.Errorf("%w: ctrl+%s", ErrModifierNotApplicable, lower)
	}

	out := make([]byte, len(bytes))
	copy(out, bytes)
	return withAlt(out, mods), nil
}

// withAlt prepends ESC when the Alt or Meta modifier is set, implementing the
// common xterm "Alt = ESC-prefix" convention.
func withAlt(b []byte, mods Modifier) []byte {
	if mods&(ModAlt|ModMeta) == 0 {
		return b
	}
	out := make([]byte, 0, len(b)+1)
	out = append(out, 0x1B)
	out = append(out, b...)
	return out
}
