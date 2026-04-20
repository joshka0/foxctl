package generictty

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/atcp/intents"
)

func TestCompileText_UTF8(t *testing.T) {
	a := New()
	got, err := a.CompileText(intents.TerminalText{Text: "hello"})
	if err != nil {
		t.Fatalf("CompileText: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

func TestCompileText_EmptyRejected(t *testing.T) {
	a := New()
	if _, err := a.CompileText(intents.TerminalText{}); !errors.Is(err, ErrEmptyText) {
		t.Fatalf("want ErrEmptyText, got %v", err)
	}
}

func TestCompileText_UnsupportedEncoding(t *testing.T) {
	a := New()
	_, err := a.CompileText(intents.TerminalText{Text: "x", Encoding: "latin-1"})
	if !errors.Is(err, ErrUnsupportedEncoding) {
		t.Fatalf("want ErrUnsupportedEncoding, got %v", err)
	}
}

func TestCompileText_InvalidUTF8(t *testing.T) {
	a := New()
	bad := string([]byte{0xff, 0xfe}) // invalid UTF-8
	_, err := a.CompileText(intents.TerminalText{Text: bad})
	if !errors.Is(err, ErrUnsupportedEncoding) {
		t.Fatalf("want ErrUnsupportedEncoding, got %v", err)
	}
}

func TestKeyTable_CoreKeys(t *testing.T) {
	a := New()
	cases := []struct {
		key  string
		want []byte
	}{
		{"Enter", []byte{0x0D}},
		{"LineFeed", []byte{0x0A}},
		{"Tab", []byte{0x09}},
		{"Backspace", []byte{0x7F}},
		{"Escape", []byte{0x1B}},
		{"Space", []byte{0x20}},
		{"Up", []byte{0x1B, '[', 'A'}},
		{"Down", []byte{0x1B, '[', 'B'}},
		{"Right", []byte{0x1B, '[', 'C'}},
		{"Left", []byte{0x1B, '[', 'D'}},
		{"F1", []byte{0x1B, 'O', 'P'}},
		{"F5", []byte{0x1B, '[', '1', '5', '~'}},
		{"Home", []byte{0x1B, '[', 'H'}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.key, func(t *testing.T) {
			got, err := a.CompileKey(intents.TerminalKey{Key: tc.key})
			if err != nil {
				t.Fatalf("CompileKey(%s): %v", tc.key, err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("key %s = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}

func TestKeyTable_EnterLineFeedDistinct(t *testing.T) {
	// spec §9.3: Enter is CR, LineFeed is LF. They must NOT be interchangeable.
	a := New()
	enter, err := a.CompileKey(intents.TerminalKey{Key: "Enter"})
	if err != nil {
		t.Fatal(err)
	}
	lf, err := a.CompileKey(intents.TerminalKey{Key: "LineFeed"})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(enter, lf) {
		t.Fatalf("Enter and LineFeed must differ (%v vs %v)", enter, lf)
	}
}

func TestKeyAliases(t *testing.T) {
	a := New()
	want, _ := a.CompileKey(intents.TerminalKey{Key: "Enter"})
	got, err := a.CompileKey(intents.TerminalKey{Key: "return"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("Return alias = %v, want %v", got, want)
	}
}

func TestCompileKey_Repeat(t *testing.T) {
	a := New()
	got, err := a.CompileKey(intents.TerminalKey{Key: "Backspace", Repeat: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{0x7F, 0x7F, 0x7F}) {
		t.Errorf("Repeat=3 Backspace = %v", got)
	}
}

func TestCompileKey_RepeatZeroIsOnce(t *testing.T) {
	a := New()
	got, err := a.CompileKey(intents.TerminalKey{Key: "Enter", Repeat: 0})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{0x0D}) {
		t.Errorf("Repeat=0 should emit once, got %v", got)
	}
}

func TestCompileKey_RepeatNegative(t *testing.T) {
	a := New()
	if _, err := a.CompileKey(intents.TerminalKey{Key: "Enter", Repeat: -1}); !errors.Is(err, ErrRepeatOutOfRange) {
		t.Fatalf("want ErrRepeatOutOfRange, got %v", err)
	}
}

func TestCompileKey_Unknown(t *testing.T) {
	a := New()
	if _, err := a.CompileKey(intents.TerminalKey{Key: "Telepathy"}); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("want ErrUnknownKey, got %v", err)
	}
}

func TestCompileKey_CtrlLetter(t *testing.T) {
	a := New()
	// Ctrl+C = 0x03
	got, err := a.CompileKey(intents.TerminalKey{Key: "c", Modifiers: []string{"ctrl"}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{0x03}) {
		t.Errorf("Ctrl+C = %v, want 0x03", got)
	}
	// Ctrl+A = 0x01
	got, _ = a.CompileKey(intents.TerminalKey{Key: "a", Modifiers: []string{"ctrl"}})
	if !bytes.Equal(got, []byte{0x01}) {
		t.Errorf("Ctrl+A = %v, want 0x01", got)
	}
}

func TestCompileKey_AltPrefix(t *testing.T) {
	a := New()
	got, err := a.CompileKey(intents.TerminalKey{Key: "Enter", Modifiers: []string{"alt"}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{0x1B, 0x0D}) {
		t.Errorf("Alt+Enter = %v, want ESC CR", got)
	}
}

func TestCompileKey_ShiftTab(t *testing.T) {
	a := New()
	got, err := a.CompileKey(intents.TerminalKey{Key: "Tab", Modifiers: []string{"shift"}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{0x1B, '[', 'Z'}) {
		t.Errorf("Shift+Tab = %v, want ESC [ Z", got)
	}
}

func TestCompileKey_CtrlOnNonLetterRejected(t *testing.T) {
	a := New()
	_, err := a.CompileKey(intents.TerminalKey{Key: "Enter", Modifiers: []string{"ctrl"}})
	if !errors.Is(err, ErrModifierNotApplicable) {
		t.Fatalf("want ErrModifierNotApplicable, got %v", err)
	}
}

func TestCompileKey_InvalidModifier(t *testing.T) {
	a := New()
	_, err := a.CompileKey(intents.TerminalKey{Key: "Enter", Modifiers: []string{"superhyper"}})
	if !errors.Is(err, ErrInvalidModifier) {
		t.Fatalf("want ErrInvalidModifier, got %v", err)
	}
}

func TestCompileKey_PrintableFallback(t *testing.T) {
	a := New()
	got, err := a.CompileKey(intents.TerminalKey{Key: "q"})
	if err != nil {
		t.Fatalf("CompileKey(q): %v", err)
	}
	if !bytes.Equal(got, []byte{'q'}) {
		t.Errorf("printable q = %v", got)
	}
}

func TestCompileSubmit_DefaultEnterLineFeed(t *testing.T) {
	a := New()
	got, err := a.CompileSubmit(intents.TerminalSubmit{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{'h', 'e', 'l', 'l', 'o', 0x0D, 0x0A}) {
		t.Errorf("submit = %v", got)
	}
}

func TestCompileSubmit_CustomSubmitKey(t *testing.T) {
	a := New()
	got, err := a.CompileSubmit(intents.TerminalSubmit{Text: "x", SubmitKey: "LineFeed"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{'x', 0x0A}) {
		t.Errorf("submit with LineFeed = %v", got)
	}
}

func TestCompileSubmit_DefaultSubmitKeyOverride(t *testing.T) {
	a := New()
	a.SetDefaultSubmitKey("LineFeed")
	got, err := a.CompileSubmit(intents.TerminalSubmit{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{'x', 0x0A}) {
		t.Errorf("submit with default LineFeed = %v", got)
	}
}

func TestCompileSubmit_KittyKeyboardUsesKittyEnter(t *testing.T) {
	a := New()
	a.SetKittyKeyboardActive(true)
	got, err := a.CompileSubmit(intents.TerminalSubmit{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{'x', 0x1B, '[', '1', '3', 'u'}) {
		t.Errorf("kitty submit = %v", got)
	}
}

func TestCompileSubmit_ExplicitKeyOverridesKittyKeyboard(t *testing.T) {
	a := New()
	a.SetKittyKeyboardActive(true)
	got, err := a.CompileSubmit(intents.TerminalSubmit{Text: "x", SubmitKey: "LineFeed"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{'x', 0x0A}) {
		t.Errorf("explicit submit key = %v", got)
	}
}

func TestCompileSubmit_EmptyTextOK(t *testing.T) {
	a := New()
	got, err := a.CompileSubmit(intents.TerminalSubmit{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{0x0D, 0x0A}) {
		t.Errorf("empty submit = %v, want CRLF only", got)
	}
}

func TestCompilePaste_AutoOffWhenDisabled(t *testing.T) {
	a := New()
	a.SetBracketedPasteEnabled(false)
	got, err := a.CompilePaste(intents.TerminalPaste{Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hi" {
		t.Errorf("auto+disabled should not wrap, got %q", got)
	}
}

func TestCompilePaste_AutoOnWhenEnabled(t *testing.T) {
	a := New()
	a.SetBracketedPasteEnabled(true)
	got, err := a.CompilePaste(intents.TerminalPaste{Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{}
	want = append(want, BracketedPasteStart...)
	want = append(want, 'h', 'i')
	want = append(want, BracketedPasteEnd...)
	if !bytes.Equal(got, want) {
		t.Errorf("auto+enabled = %v, want wrapped", got)
	}
}

func TestCompilePaste_ForceAlwaysWraps(t *testing.T) {
	a := New() // bracketed paste disabled
	got, err := a.CompilePaste(intents.TerminalPaste{Text: "hi", Bracketed: "force"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), string(BracketedPasteStart)) {
		t.Errorf("force policy should wrap even when disabled")
	}
}

func TestCompilePaste_OffNeverWraps(t *testing.T) {
	a := New()
	a.SetBracketedPasteEnabled(true)
	got, err := a.CompilePaste(intents.TerminalPaste{Text: "hi", Bracketed: "off"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), string(BracketedPasteStart)) {
		t.Errorf("off policy must not wrap")
	}
}

func TestCompilePaste_SubmitAfterAppendsEnter(t *testing.T) {
	a := New()
	got, err := a.CompilePaste(intents.TerminalPaste{Text: "hi", SubmitAfter: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(got, []byte{0x0D, 0x0A}) {
		t.Errorf("submit_after should end with CRLF, got %v", got)
	}
}

func TestCompilePaste_EmptyRejected(t *testing.T) {
	a := New()
	if _, err := a.CompilePaste(intents.TerminalPaste{}); !errors.Is(err, ErrEmptyText) {
		t.Fatalf("want ErrEmptyText, got %v", err)
	}
}

func TestCompileWriteBytes_DisabledByDefault(t *testing.T) {
	a := New()
	_, err := a.CompileWriteBytes(intents.TerminalWriteBytes{Bytes: []byte{0x01}})
	if !errors.Is(err, ErrWriteBytesNotAllowed) {
		t.Fatalf("want ErrWriteBytesNotAllowed, got %v", err)
	}
}

func TestCompileWriteBytes_EnabledPassesThrough(t *testing.T) {
	a := New()
	a.SetAllowWriteBytes(true)
	got, err := a.CompileWriteBytes(intents.TerminalWriteBytes{Bytes: []byte{0x01, 0x02, 0x03}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{0x01, 0x02, 0x03}) {
		t.Errorf("write_bytes passthrough = %v", got)
	}
}

func TestCompileWriteBytes_DoesNotRetainInput(t *testing.T) {
	a := New()
	a.SetAllowWriteBytes(true)
	src := []byte{0x01, 0x02}
	got, _ := a.CompileWriteBytes(intents.TerminalWriteBytes{Bytes: src})
	src[0] = 0xFF
	if got[0] != 0x01 {
		t.Errorf("adapter must copy input bytes; got mutated %v", got)
	}
}

func TestParseModifiers_Synonyms(t *testing.T) {
	cases := [][]string{
		{"control"},
		{"CTRL"},
		{"option"},
		{"ALT"},
		{"CMD"},
		{"command"},
		{"meta"},
		{"Shift"},
	}
	for _, mods := range cases {
		if _, err := ParseModifiers(mods); err != nil {
			t.Errorf("ParseModifiers(%v) unexpectedly failed: %v", mods, err)
		}
	}
}

func TestParseBracketed(t *testing.T) {
	cases := map[string]intents.BracketedPolicy{
		"":      intents.BracketedAuto,
		"auto":  intents.BracketedAuto,
		"force": intents.BracketedForce,
		"off":   intents.BracketedOff,
		"weird": intents.BracketedAuto,
	}
	for in, want := range cases {
		if got := intents.ParseBracketed(in); got != want {
			t.Errorf("ParseBracketed(%q) = %v, want %v", in, got, want)
		}
	}
}
