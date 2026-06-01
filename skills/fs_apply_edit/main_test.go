package main

import (
	"strings"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/diffutil"
)

func TestApplyExactMatch(t *testing.T) {
	content := `func oldName() {
	return nil
}`
	edit := Edit{
		Search:    "oldName",
		Replace:   "newName",
		MatchMode: MatchExact,
	}

	result, modified, err := applyEdit(content, edit)
	if err != nil {
		t.Fatalf("applyEdit: %v", err)
	}
	if result.Replacements != 1 {
		t.Errorf("expected 1 replacement, got %d", result.Replacements)
	}
	if !strings.Contains(modified, "newName") {
		t.Error("expected modified content to contain 'newName'")
	}
	if strings.Contains(modified, "oldName") {
		t.Error("expected modified content not to contain 'oldName'")
	}
}

func TestApplyGlobalReplace(t *testing.T) {
	content := `fmt.Println("hello")
fmt.Println("world")`
	edit := Edit{
		Search:    "fmt.Println",
		Replace:   "log.Println",
		MatchMode: MatchExact,
		Global:    true,
	}

	result, modified, err := applyEdit(content, edit)
	if err != nil {
		t.Fatalf("applyEdit: %v", err)
	}
	if result.Replacements != 2 {
		t.Errorf("expected 2 replacements, got %d", result.Replacements)
	}
	if strings.Contains(modified, "fmt.Println") {
		t.Error("expected no remaining 'fmt.Println'")
	}
	if strings.Count(modified, "log.Println") != 2 {
		t.Error("expected 2 'log.Println' occurrences")
	}
}

func TestApplyFuzzyWithLineHint(t *testing.T) {
	content := `func A() {
	return nil
}

func B() {
	return nil
}

func C() {
	return nil
}`
	// Should find the one near line 6 (func B)
	edit := Edit{
		Search:    "return nil",
		Replace:   "return errors.New(\"not implemented\")",
		MatchMode: MatchFuzzy,
		LineHint:  6,
	}

	result, modified, err := applyEdit(content, edit)
	if err != nil {
		t.Fatalf("applyEdit: %v", err)
	}
	if result.Replacements != 1 {
		t.Errorf("expected 1 replacement, got %d", result.Replacements)
	}
	// Should have replaced only the one in func B (around line 6)
	if len(result.Lines) != 1 {
		t.Errorf("expected 1 line, got %d", len(result.Lines))
	}
	// Line 6 is the return in func B
	if result.Lines[0] != 6 {
		t.Errorf("expected line 6, got %d", result.Lines[0])
	}
	// Verify the modified content has the replacement
	if !strings.Contains(modified, "errors.New") {
		t.Error("expected modified content to contain replacement")
	}
}

func TestApplyRegexMatch(t *testing.T) {
	content := `var foo123 = 1
var bar456 = 2
var baz789 = 3`
	edit := Edit{
		Search:    `var \w+\d+`,
		Replace:   "const x",
		MatchMode: MatchRegex,
		Global:    true,
	}

	result, modified, err := applyEdit(content, edit)
	if err != nil {
		t.Fatalf("applyEdit: %v", err)
	}
	if result.Replacements != 3 {
		t.Errorf("expected 3 replacements, got %d", result.Replacements)
	}
	if strings.Count(modified, "const x") != 3 {
		t.Errorf("expected 3 'const x' occurrences, got content: %s", modified)
	}
}

func TestApplyNoMatch(t *testing.T) {
	content := `func hello() {}`
	edit := Edit{
		Search:    "goodbye",
		Replace:   "farewell",
		MatchMode: MatchExact,
	}

	result, modified, err := applyEdit(content, edit)
	if err != nil {
		t.Fatalf("applyEdit: %v", err)
	}
	if result.Replacements != 0 {
		t.Errorf("expected 0 replacements, got %d", result.Replacements)
	}
	if modified != content {
		t.Error("expected content unchanged")
	}
}

func TestApplyEditRejectsEmptySearch(t *testing.T) {
	content := "alpha\nbeta\n"
	for _, mode := range []MatchMode{MatchExact, MatchFuzzy, MatchRegex, ""} {
		t.Run(string(mode), func(t *testing.T) {
			result, modified, err := applyEdit(content, Edit{
				Search:    "",
				Replace:   "replacement",
				MatchMode: mode,
			})
			if err == nil {
				t.Fatal("expected empty search to be rejected")
			}
			if modified != content {
				t.Fatalf("empty search mutated content: %q", modified)
			}
			if result.Replacements != 0 || len(result.Lines) != 0 {
				t.Fatalf("empty search result recorded replacements: %+v", result)
			}
		})
	}
}

func TestApplyEditPropertyInvalidEmptySearchNeverMutatesContent(t *testing.T) {
	modes := []MatchMode{MatchExact, MatchFuzzy, MatchRegex, ""}

	property := func(content string, modeSeed uint8) bool {
		mode := modes[int(modeSeed)%len(modes)]
		result, modified, err := applyEdit(content, Edit{
			Search:    "",
			Replace:   "replacement",
			MatchMode: mode,
		})
		if err == nil {
			t.Logf("empty search accepted for mode %q", mode)
			return false
		}
		if modified != content {
			t.Logf("empty search mutated content for mode %q: got %q want %q", mode, modified, content)
			return false
		}
		if result.Replacements != 0 || len(result.Lines) != 0 {
			t.Logf("empty search recorded replacement metadata for mode %q: %+v", mode, result)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func FuzzApplyEditMaintainsResultInvariants(f *testing.F) {
	seeds := []struct {
		content  string
		search   string
		replace  string
		modeSeed uint8
		lineHint int
		global   bool
	}{
		{content: "alpha\nbeta\n", search: "alpha", replace: "ALPHA", modeSeed: 0},
		{content: "func A() {\n\treturn nil\n}\n", search: "return nil", replace: "return nil, err", modeSeed: 1, lineHint: 2},
		{content: "func A() {\n\treturn    nil\n}\n", search: "return nil", replace: "return nil, err", modeSeed: 1, lineHint: 2},
		{content: "var foo123 = 1\nvar bar456 = 2\n", search: `var \w+\d+`, replace: "const x", modeSeed: 2, global: true},
		{content: "unchanged", search: "", replace: "x", modeSeed: 3},
		{content: "not regex", search: "[", replace: "x", modeSeed: 2},
	}
	for _, seed := range seeds {
		f.Add(seed.content, seed.search, seed.replace, seed.modeSeed, seed.lineHint, seed.global)
	}

	f.Fuzz(func(t *testing.T, content, search, replace string, modeSeed uint8, lineHint int, global bool) {
		if len(content) > 4096 || len(search) > 256 || len(replace) > 256 {
			t.Skip()
		}
		modes := []MatchMode{MatchExact, MatchFuzzy, MatchRegex, MatchMode("unknown")}
		edit := Edit{
			Search:    search,
			Replace:   replace,
			MatchMode: modes[int(modeSeed)%len(modes)],
			LineHint:  boundedLineHint(lineHint),
			Global:    global,
		}

		result, modified, err := applyEdit(content, edit)
		if err != nil {
			if modified != content {
				t.Fatalf("errored edit mutated content: err=%v content=%q modified=%q", err, content, modified)
			}
			if result.Replacements != 0 || len(result.Lines) != 0 {
				t.Fatalf("errored edit recorded replacement metadata: err=%v result=%+v", err, result)
			}
			return
		}

		if result.Replacements < 0 {
			t.Fatalf("negative replacement count: %+v", result)
		}
		if result.Replacements == 0 {
			if modified != content {
				t.Fatalf("zero-replacement edit mutated content: content=%q modified=%q result=%+v", content, modified, result)
			}
			if len(result.Lines) != 0 {
				t.Fatalf("zero-replacement edit recorded lines: %+v", result)
			}
			return
		}
		if len(result.Lines) == 0 {
			t.Fatalf("replacement missing line metadata: %+v", result)
		}
		lineCount := strings.Count(content, "\n") + 1
		for _, line := range result.Lines {
			if line < 1 || line > lineCount {
				t.Fatalf("line %d outside content line range 1..%d for result %+v", line, lineCount, result)
			}
		}
	})
}

func boundedLineHint(lineHint int) int {
	if lineHint < 0 {
		return 0
	}
	if lineHint > 10_000 {
		return 10_000
	}
	return lineHint
}

func TestGenerateUnifiedDiff(t *testing.T) {
	original := `line1
line2
line3`
	modified := `line1
line2-changed
line3`

	diff, err := diffutil.UnifiedDiff("test.txt", original, modified, 0)
	if err != nil {
		t.Fatalf("UnifiedDiff: %v", err)
	}
	if !strings.Contains(diff, "--- a/test.txt") {
		t.Error("diff should contain original file header")
	}
	if !strings.Contains(diff, "+++ b/test.txt") {
		t.Error("diff should contain modified file header")
	}
	if !strings.Contains(diff, "-line2") {
		t.Error("diff should show removed line")
	}
	if !strings.Contains(diff, "+line2-changed") {
		t.Error("diff should show added line")
	}
}

func TestNormalizeWhitespace(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  hello   world  ", "hello world"},
		{"foo\t\tbar", "foo bar"},
		{"single", "single"},
		{"  ", ""},
	}

	for _, tt := range tests {
		result := normalizeWhitespace(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeWhitespace(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
