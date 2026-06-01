package codeblocks

import (
	"strings"
	"testing"
	"testing/quick"
	"unicode/utf8"
)

func TestTrimLinePreservesValidUTF8WhenLimitSplitsRune(t *testing.T) {
	got := TrimLine("type café struct {}", len("type caf")+1)
	if got != "type caf..." {
		t.Fatalf("TrimLine split rune = %q, want type caf...", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("TrimLine returned invalid UTF-8: %q", got)
	}
}

func TestTrimLineGeneratedValidUTF8(t *testing.T) {
	prop := func(runes []rune, limit uint8) bool {
		line := string(runes)
		max := int(limit)
		got := TrimLine(line, max)
		if !utf8.ValidString(got) {
			return false
		}
		if len(line) <= max {
			return got == line
		}
		if !strings.HasSuffix(got, "...") {
			return false
		}
		prefix := strings.TrimSuffix(got, "...")
		return len(prefix) <= max && utf8.ValidString(prefix)
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatal(err)
	}
}
