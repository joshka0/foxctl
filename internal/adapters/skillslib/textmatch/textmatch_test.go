package textmatch

import (
	"strings"
	"testing"
	"testing/quick"
	"unicode/utf8"
)

func TestTrimLinePreservesValidUTF8WhenLimitSplitsRune(t *testing.T) {
	got := TrimLine("ab🙂cd", 4)
	if got != "ab..." {
		t.Fatalf("TrimLine split rune = %q, want ab...", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("TrimLine returned invalid UTF-8: %q", got)
	}
}

func TestTrimLineHandlesNonPositiveLimit(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		limit int
		want  string
	}{
		{name: "empty", line: "", limit: 0, want: ""},
		{name: "zero", line: "abc", limit: 0, want: "..."},
		{name: "negative", line: "abc", limit: -1, want: "..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TrimLine(tt.line, tt.limit); got != tt.want {
				t.Fatalf("TrimLine(%q, %d) = %q, want %q", tt.line, tt.limit, got, tt.want)
			}
		})
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

func FuzzCompileRegexDoesNotPanic(f *testing.F) {
	seeds := []struct {
		pattern         string
		caseInsensitive bool
		wordBoundary    bool
		multiline       bool
	}{
		{pattern: "needle"},
		{pattern: "[", caseInsensitive: true},
		{pattern: `\w+`, wordBoundary: true},
		{pattern: "a.*b", multiline: true},
		{pattern: "(?i)already", caseInsensitive: true, multiline: true},
	}
	for _, seed := range seeds {
		f.Add(seed.pattern, seed.caseInsensitive, seed.wordBoundary, seed.multiline)
	}

	f.Fuzz(func(t *testing.T, pattern string, caseInsensitive, wordBoundary, multiline bool) {
		if len(pattern) > 512 {
			t.Skip()
		}
		_, _ = CompileRegex(pattern, RegexOptions{
			CaseInsensitive: caseInsensitive,
			WordBoundary:    wordBoundary,
			Multiline:       multiline,
		})
	})
}
