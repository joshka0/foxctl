package main

import (
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/diffutil"
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
