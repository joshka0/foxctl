package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path string
		want Language
	}{
		{"main.go", LangGo},
		{"script.py", LangPython},
		{"app.js", LangJS},
		{"component.tsx", LangTS},
		{"player.gd", LangGDScript},
		{"README.md", LangGeneric},
		{"Makefile", LangGeneric},
		{"/path/to/file.go", LangGo},
		{"src/utils.py", LangPython},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := detectLanguage(tt.path)
			if got != tt.want {
				t.Errorf("detectLanguage(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestGetIndentLevel(t *testing.T) {
	tests := []struct {
		line string
		want int
	}{
		{"no indent", 0},
		{"  two spaces", 2},
		{"    four spaces", 4},
		{"\ttab", 4},
		{"\t\ttwo tabs", 8},
		{"  \tmixed", 6},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got := getIndentLevel(tt.line)
			if got != tt.want {
				t.Errorf("getIndentLevel(%q) = %d, want %d", tt.line, got, tt.want)
			}
		})
	}
}

func TestFindGoSymbols(t *testing.T) {
	code := `package main

import "fmt"

func hello() {
	fmt.Println("Hello")
}

func (s *Server) Handle() {
	// method
}

type Config struct {
	Name string
}

type Handler interface {
	Handle()
}`

	lines := strings.Split(code, "\n")
	symbols := findGoSymbols(lines)

	if len(symbols) != 4 {
		t.Fatalf("expected 4 symbols, got %d", len(symbols))
	}

	// Check hello function
	if symbols[0].Name != "hello" || symbols[0].Kind != "function" {
		t.Errorf("expected hello function, got %+v", symbols[0])
	}

	// Check Handle method
	if symbols[1].Name != "Handle" || symbols[1].Kind != "method" {
		t.Errorf("expected Handle method, got %+v", symbols[1])
	}

	// Check Config struct
	if symbols[2].Name != "Config" || symbols[2].Kind != "type" {
		t.Errorf("expected Config type, got %+v", symbols[2])
	}

	// Check Handler interface
	if symbols[3].Name != "Handler" || symbols[3].Kind != "type" {
		t.Errorf("expected Handler type, got %+v", symbols[3])
	}
}

func TestFindPythonSymbols(t *testing.T) {
	code := `import os

def hello():
    print("Hello")

async def fetch_data():
    return await get()

class Config:
    def __init__(self):
        pass`

	lines := strings.Split(code, "\n")
	symbols := findPythonSymbols(lines)

	if len(symbols) != 3 {
		t.Fatalf("expected 3 symbols, got %d: %+v", len(symbols), symbols)
	}

	if symbols[0].Name != "hello" || symbols[0].Kind != "function" {
		t.Errorf("expected hello function, got %+v", symbols[0])
	}

	if symbols[1].Name != "fetch_data" || symbols[1].Kind != "function" {
		t.Errorf("expected fetch_data function, got %+v", symbols[1])
	}

	if symbols[2].Name != "Config" || symbols[2].Kind != "class" {
		t.Errorf("expected Config class, got %+v", symbols[2])
	}
}

func TestFindJSSymbols(t *testing.T) {
	code := `import React from 'react';

function hello() {
  console.log("Hello");
}

const greet = (name) => {
  return "Hi " + name;
};

class Component {
  render() {}
}

interface Props {
  name: string;
}

type Config = {
  value: number;
};`

	lines := strings.Split(code, "\n")
	symbols := findJSSymbols(lines)

	if len(symbols) < 4 {
		t.Fatalf("expected at least 4 symbols, got %d: %+v", len(symbols), symbols)
	}

	// Check for key symbols
	names := make(map[string]string)
	for _, s := range symbols {
		names[s.Name] = s.Kind
	}

	if names["hello"] != "function" {
		t.Errorf("expected hello function, got %v", names["hello"])
	}
	if names["greet"] != "function" {
		t.Errorf("expected greet function, got %v", names["greet"])
	}
	if names["Component"] != "class" {
		t.Errorf("expected Component class, got %v", names["Component"])
	}
	if names["Props"] != "interface" {
		t.Errorf("expected Props interface, got %v", names["Props"])
	}
}

func TestFindBraceEnd(t *testing.T) {
	code := `func hello() {
	if true {
		fmt.Println("nested")
	}
	return
}`

	lines := strings.Split(code, "\n")
	end := findBraceEnd(lines, 0)

	if end != 5 {
		t.Errorf("expected end at line 5, got %d", end)
	}
}

func TestFindBraceEndWithStrings(t *testing.T) {
	code := `func hello() {
	s := "string with { brace"
	s2 := "another } brace"
	return
}`

	lines := strings.Split(code, "\n")
	end := findBraceEnd(lines, 0)

	if end != 4 {
		t.Errorf("expected end at line 4, got %d", end)
	}
}

func TestFindIndentEnd(t *testing.T) {
	code := `def hello():
    print("line 1")
    print("line 2")
    if True:
        print("nested")
    print("back")

def goodbye():`

	lines := strings.Split(code, "\n")
	end := findIndentEnd(lines, 0, 0)

	// The function ends at "print("back")" which is line 6 (0-indexed)
	// Empty line 7 is included, then line 8 "def goodbye():" has indent 0, so we stop
	if end != 6 {
		t.Errorf("expected end at line 6, got %d", end)
	}
}

func TestApplySymbolEdit(t *testing.T) {
	code := `package main

func hello() {
	fmt.Println("Hello")
}

func goodbye() {
	fmt.Println("Goodbye")
}`

	lines := strings.Split(code, "\n")
	newCode := `func hello() {
	fmt.Println("Hello, World!")
}`

	e := edit{
		Type:    "symbol",
		Symbol:  "hello",
		NewCode: newCode,
	}

	result, found, applied, err := applySymbolEdit(lines, LangGo, e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Error("expected edit to be applied")
	}
	if len(found) != 1 || found[0] != "hello" {
		t.Errorf("expected found=['hello'], got %v", found)
	}

	resultStr := strings.Join(result, "\n")
	if !strings.Contains(resultStr, "Hello, World!") {
		t.Error("expected result to contain new code")
	}
	if !strings.Contains(resultStr, "goodbye") {
		t.Error("expected result to still contain goodbye function")
	}
}

func TestApplySymbolEditNotFound(t *testing.T) {
	code := `package main

func hello() {
	fmt.Println("Hello")
}`

	lines := strings.Split(code, "\n")
	e := edit{
		Type:    "symbol",
		Symbol:  "nonexistent",
		NewCode: "func nonexistent() {}",
	}

	_, _, _, err := applySymbolEdit(lines, LangGo, e)
	if err == nil {
		t.Error("expected error for nonexistent symbol")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestApplyLinesEdit(t *testing.T) {
	code := `line 1
line 2
line 3
line 4
line 5`

	lines := strings.Split(code, "\n")
	e := edit{
		Type:      "lines",
		StartLine: 2,
		EndLine:   4,
		NewCode:   "replaced line 2\nreplaced line 3",
	}

	result, found, applied, err := applyLinesEdit(lines, e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Error("expected edit to be applied")
	}
	if len(found) != 1 || found[0] != "lines:2-4" {
		t.Errorf("expected found=['lines:2-4'], got %v", found)
	}

	resultStr := strings.Join(result, "\n")
	if !strings.Contains(resultStr, "replaced line 2") {
		t.Error("expected result to contain replaced content")
	}
	// Original lines 2-4 should be replaced
	if strings.Contains(resultStr, "\nline 2\n") || strings.Contains(resultStr, "\nline 3\n") || strings.Contains(resultStr, "\nline 4\n") {
		t.Error("expected original lines 2-4 to be replaced")
	}
	if !strings.Contains(resultStr, "line 5") {
		t.Error("expected line 5 to remain")
	}
}

func TestApplyLinesEditInvalidRange(t *testing.T) {
	lines := []string{"line 1", "line 2"}

	tests := []struct {
		name  string
		start int
		end   int
	}{
		{"zero start", 0, 1},
		{"negative start", -1, 1},
		{"end before start", 3, 2},
		{"start beyond file", 100, 101},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := edit{
				Type:      "lines",
				StartLine: tt.start,
				EndLine:   tt.end,
				NewCode:   "new",
			}
			_, _, _, err := applyLinesEdit(lines, e)
			if err == nil {
				t.Error("expected error for invalid range")
			}
		})
	}
}

func TestApplyReplaceEdit(t *testing.T) {
	code := `func hello() {
	oldVar := 1
	oldVar = 2
	return oldVar
}`

	lines := strings.Split(code, "\n")
	e := edit{
		Type:    "replace",
		Search:  "oldVar",
		Replace: "newVar",
		All:     true,
	}

	result, _, applied, err := applyReplaceEdit(lines, LangGo, e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Error("expected edit to be applied")
	}

	resultStr := strings.Join(result, "\n")
	if strings.Contains(resultStr, "oldVar") {
		t.Error("expected all oldVar to be replaced")
	}
	if strings.Count(resultStr, "newVar") != 3 {
		t.Errorf("expected 3 occurrences of newVar, got %d", strings.Count(resultStr, "newVar"))
	}
}

func TestApplyReplaceEditFirstOnly(t *testing.T) {
	code := `oldVar := 1
oldVar = 2
oldVar = 3`

	lines := strings.Split(code, "\n")
	e := edit{
		Type:    "replace",
		Search:  "oldVar",
		Replace: "newVar",
		All:     false,
	}

	result, _, applied, err := applyReplaceEdit(lines, LangGo, e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Error("expected edit to be applied")
	}

	resultStr := strings.Join(result, "\n")
	if strings.Count(resultStr, "newVar") != 1 {
		t.Errorf("expected 1 occurrence of newVar, got %d", strings.Count(resultStr, "newVar"))
	}
	if strings.Count(resultStr, "oldVar") != 2 {
		t.Errorf("expected 2 remaining oldVar, got %d", strings.Count(resultStr, "oldVar"))
	}
}

func TestApplyReplaceEditWithinSymbol(t *testing.T) {
	code := `package main

func hello() {
	x := 1
	y := 2
}

func goodbye() {
	x := 10
	y := 20
}`

	lines := strings.Split(code, "\n")
	e := edit{
		Type:         "replace",
		Search:       "x",
		Replace:      "newX",
		WithinSymbol: "hello",
		All:          true,
	}

	result, found, applied, err := applyReplaceEdit(lines, LangGo, e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Error("expected edit to be applied")
	}
	if len(found) != 1 || found[0] != "hello" {
		t.Errorf("expected found=['hello'], got %v", found)
	}

	resultStr := strings.Join(result, "\n")
	// Should replace x in hello but not in goodbye
	if !strings.Contains(resultStr, "newX := 1") {
		t.Error("expected x in hello to be replaced")
	}
	// The goodbye function should still have original x
	goodbyeIdx := strings.Index(resultStr, "goodbye")
	if goodbyeIdx == -1 {
		t.Fatal("expected goodbye function in result")
	}
	goodbyePart := resultStr[goodbyeIdx:]
	if !strings.Contains(goodbyePart, "x := 10") {
		t.Error("expected x in goodbye to remain unchanged")
	}
}

func TestApplyReplaceEditNoMatch(t *testing.T) {
	code := `func hello() {
	return 1
}`

	lines := strings.Split(code, "\n")
	e := edit{
		Type:    "replace",
		Search:  "nonexistent",
		Replace: "new",
	}

	result, _, applied, err := applyReplaceEdit(lines, LangGo, e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied {
		t.Error("expected no edit to be applied when search not found")
	}

	// Result should be unchanged
	if strings.Join(result, "\n") != strings.Join(lines, "\n") {
		t.Error("expected result to be unchanged")
	}
}

func TestGenerateUnifiedDiff(t *testing.T) {
	original := `line 1
line 2
line 3`

	modified := `line 1
modified line 2
line 3`

	diff, err := generateUnifiedDiff("test.go", original, modified, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if diff == "" {
		t.Error("expected non-empty diff")
	}
	if !strings.Contains(diff, "-line 2") {
		t.Error("expected diff to show removed line")
	}
	if !strings.Contains(diff, "+modified line 2") {
		t.Error("expected diff to show added line")
	}
	if !strings.Contains(diff, "--- a/test.go") {
		t.Error("expected diff to have from-file header")
	}
	if !strings.Contains(diff, "+++ b/test.go") {
		t.Error("expected diff to have to-file header")
	}
}

func TestGenerateUnifiedDiffNoChanges(t *testing.T) {
	content := `line 1
line 2`

	diff, err := generateUnifiedDiff("test.go", content, content, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if diff != "" {
		t.Errorf("expected empty diff for identical content, got: %s", diff)
	}
}

func TestParseInputValid(t *testing.T) {
	input := `{
		"path": "test.go",
		"edits": [
			{"type": "symbol", "symbol": "hello", "new_code": "func hello() {}"}
		],
		"dry_run": true,
		"context_lines": 5
	}`

	in, err := parseInput(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if in.Path != "test.go" {
		t.Errorf("expected path='test.go', got %s", in.Path)
	}
	if len(in.Edits) != 1 {
		t.Errorf("expected 1 edit, got %d", len(in.Edits))
	}
	if !in.DryRun {
		t.Error("expected dry_run=true")
	}
	if in.ContextLines != 5 {
		t.Errorf("expected context_lines=5, got %d", in.ContextLines)
	}
}

func TestParseInputDefaults(t *testing.T) {
	input := `{
		"path": "test.go",
		"edits": [{"type": "lines", "start_line": 1, "end_line": 1, "new_code": "x"}]
	}`

	in, err := parseInput(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if in.ContextLines != 3 {
		t.Errorf("expected default context_lines=3, got %d", in.ContextLines)
	}
}

func TestParseInputMissingPath(t *testing.T) {
	input := `{"edits": [{"type": "symbol"}]}`

	_, err := parseInput(strings.NewReader(input))
	if err == nil {
		t.Error("expected error for missing path")
	}
}

func TestParseInputNoEdits(t *testing.T) {
	input := `{"path": "test.go", "edits": []}`

	_, err := parseInput(strings.NewReader(input))
	if err == nil {
		t.Error("expected error for empty edits")
	}
}

func TestFindGDScriptSymbols(t *testing.T) {
	code := `extends Node

class_name Player

var speed = 100

func _ready():
    pass

func move(direction):
    position += direction * speed

class InnerClass:
    var value = 0`

	lines := strings.Split(code, "\n")
	symbols := findGDScriptSymbols(lines)

	if len(symbols) < 3 {
		t.Fatalf("expected at least 3 symbols, got %d: %+v", len(symbols), symbols)
	}

	names := make(map[string]string)
	for _, s := range symbols {
		names[s.Name] = s.Kind
	}

	if names["Player"] != "class" {
		t.Errorf("expected Player class, got %v", names["Player"])
	}
	if names["_ready"] != "function" {
		t.Errorf("expected _ready function, got %v", names["_ready"])
	}
	if names["move"] != "function" {
		t.Errorf("expected move function, got %v", names["move"])
	}
}

func TestRelativeTo(t *testing.T) {
	tests := []struct {
		base   string
		target string
		want   string
	}{
		{"/home/user/project", "/home/user/project/src/main.go", "src/main.go"},
		{"/home/user/project", "/other/path/file.go", "/other/path/file.go"},
		{"", "/some/path/file.go", "/some/path/file.go"},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			got := relativeTo(tt.base, tt.target)
			if got != tt.want {
				t.Errorf("relativeTo(%q, %q) = %q, want %q", tt.base, tt.target, got, tt.want)
			}
		})
	}
}

// Additional tests for coverage

func TestFindSymbolsGenericLanguage(t *testing.T) {
	// Test that LangGeneric returns nil
	lines := []string{"some content", "more content"}
	symbols := findSymbols(lines, LangGeneric)
	if symbols != nil {
		t.Errorf("expected nil for LangGeneric, got %+v", symbols)
	}
}

func TestFindSymbolsAllLanguages(t *testing.T) {
	tests := []struct {
		name     string
		lang     Language
		code     string
		wantMin  int
		wantName string
	}{
		{
			name:     "Go",
			lang:     LangGo,
			code:     "package main\nfunc hello() {\n}",
			wantMin:  1,
			wantName: "hello",
		},
		{
			name:     "Python",
			lang:     LangPython,
			code:     "def hello():\n    pass",
			wantMin:  1,
			wantName: "hello",
		},
		{
			name:     "JavaScript",
			lang:     LangJS,
			code:     "function hello() {\n}",
			wantMin:  1,
			wantName: "hello",
		},
		{
			name:     "TypeScript",
			lang:     LangTS,
			code:     "function hello() {\n}",
			wantMin:  1,
			wantName: "hello",
		},
		{
			name:     "GDScript",
			lang:     LangGDScript,
			code:     "func hello():\n    pass",
			wantMin:  1,
			wantName: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := strings.Split(tt.code, "\n")
			symbols := findSymbols(lines, tt.lang)
			if len(symbols) < tt.wantMin {
				t.Errorf("expected at least %d symbols, got %d", tt.wantMin, len(symbols))
				return
			}
			if symbols[0].Name != tt.wantName {
				t.Errorf("expected first symbol named %q, got %q", tt.wantName, symbols[0].Name)
			}
		})
	}
}

func TestApplySymbolEditSymbolNotFound(t *testing.T) {
	code := `package main

func existingFunc() {
	return
}`
	lines := strings.Split(code, "\n")
	e := edit{
		Type:    "symbol",
		Symbol:  "nonExistentFunc",
		NewCode: "func nonExistentFunc() {}",
	}

	_, _, applied, err := applySymbolEdit(lines, LangGo, e)
	// Function returns an error when symbol not found
	if err == nil {
		t.Error("expected error when symbol not found")
	}
	if applied {
		t.Error("expected not applied when symbol not found")
	}
	if !strings.Contains(err.Error(), "symbol not found") {
		t.Errorf("expected 'symbol not found' error, got: %v", err)
	}
}

func TestApplyLinesEditOutOfBounds(t *testing.T) {
	lines := []string{"line 1", "line 2", "line 3"}

	// Test start_line > total lines
	e := edit{
		Type:      "lines",
		StartLine: 10,
		EndLine:   12,
		NewCode:   "new content",
	}

	_, _, applied, err := applyLinesEdit(lines, e)
	// Function returns an error when start_line exceeds file length
	if err == nil {
		t.Error("expected error for out of bounds start_line")
	}
	if applied {
		t.Error("expected not applied for out of bounds")
	}
	if !strings.Contains(err.Error(), "exceeds file length") {
		t.Errorf("expected 'exceeds file length' error, got: %v", err)
	}
}

func TestApplyLinesEditEndLinePastEOF(t *testing.T) {
	lines := []string{"line 1", "line 2", "line 3"}

	// End line extends past EOF - should clamp to file end
	e := edit{
		Type:      "lines",
		StartLine: 2,
		EndLine:   100,
		NewCode:   "new line 2\nnew line 3",
	}

	result, _, applied, err := applyLinesEdit(lines, e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Error("expected edit to be applied")
	}

	resultStr := strings.Join(result, "\n")
	if !strings.Contains(resultStr, "line 1") {
		t.Error("expected line 1 to be preserved")
	}
	if !strings.Contains(resultStr, "new line 2") {
		t.Error("expected new line 2 to be present")
	}
}

func TestApplyReplaceEditMultipleOccurrences(t *testing.T) {
	code := `package main

func hello() {
	print("hello")
	print("hello")
}`
	lines := strings.Split(code, "\n")
	e := edit{
		Type:    "replace",
		Search:  "hello",
		Replace: "world",
	}

	result, found, applied, err := applyReplaceEdit(lines, LangGo, e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Error("expected edit to be applied")
	}

	resultStr := strings.Join(result, "\n")
	// First occurrence should be replaced
	if strings.Count(resultStr, "world") < 1 {
		t.Error("expected at least one 'world' replacement")
	}
	if len(found) == 0 {
		t.Error("expected found slice to have symbols")
	}
}

func TestApplyReplaceEditSearchNotFound(t *testing.T) {
	code := `package main

func hello() {
}`
	lines := strings.Split(code, "\n")
	e := edit{
		Type:    "replace",
		Search:  "nonexistent",
		Replace: "replacement",
	}

	result, found, applied, err := applyReplaceEdit(lines, LangGo, e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied {
		t.Error("expected not applied when search not found")
	}
	if len(found) != 0 {
		t.Errorf("expected empty found, got %v", found)
	}
	if strings.Join(result, "\n") != strings.Join(lines, "\n") {
		t.Error("result should be unchanged")
	}
}

func TestFindBraceEndNestedBraces(t *testing.T) {
	code := `func complex() {
	if true {
		for i := 0; i < 10; i++ {
			doSomething()
		}
	}
}`
	lines := strings.Split(code, "\n")
	end := findBraceEnd(lines, 0)

	// Should find the closing brace of the function
	if end != 6 {
		t.Errorf("expected end at line 6, got %d", end)
	}
}

func TestFindBraceEndUnclosed(t *testing.T) {
	code := `func unclosed() {
	doSomething()`
	lines := strings.Split(code, "\n")
	end := findBraceEnd(lines, 0)

	// When no matching brace, should return last line
	if end != 1 {
		t.Errorf("expected end at line 1 (last line), got %d", end)
	}
}

func TestFindBraceEndNoBraceOnLine(t *testing.T) {
	code := `type MyInterface interface
type Another struct
var x int`
	lines := strings.Split(code, "\n")
	end := findBraceEnd(lines, 0)

	// No braces at all, so function iterates to end and returns last line
	if end != 2 {
		t.Errorf("expected end at line 2 (last line), got %d", end)
	}
}

func TestFindIndentEndDeepNesting(t *testing.T) {
	code := `def outer():
    def inner():
        if True:
            pass
        return
    return`
	lines := strings.Split(code, "\n")
	end := findIndentEnd(lines, 0, 0)

	// Should find the end of the outer function
	if end != 5 {
		t.Errorf("expected end at line 5, got %d", end)
	}
}

func TestFindIndentEndEmptyLines(t *testing.T) {
	code := `def func():
    x = 1

    y = 2
    return`
	lines := strings.Split(code, "\n")
	end := findIndentEnd(lines, 0, 0)

	// Should skip empty lines and find the end
	if end != 4 {
		t.Errorf("expected end at line 4, got %d", end)
	}
}

func TestFindJSSymbolsArrowFunctions(t *testing.T) {
	code := `const hello = () => {
  console.log("hello");
};

const add = (a, b) => {
  return a + b;
};

class MyClass {
  constructor() {}
}`
	lines := strings.Split(code, "\n")
	symbols := findJSSymbols(lines)

	names := make(map[string]bool)
	for _, s := range symbols {
		names[s.Name] = true
	}

	if !names["hello"] {
		t.Error("expected to find 'hello' arrow function")
	}
	if !names["add"] {
		t.Error("expected to find 'add' arrow function")
	}
	if !names["MyClass"] {
		t.Error("expected to find 'MyClass'")
	}
}

func TestFindPythonSymbolsAsyncDef(t *testing.T) {
	code := `async def fetch_data():
    await do_something()
    return result

def sync_func():
    pass

class DataHandler:
    pass`
	lines := strings.Split(code, "\n")
	symbols := findPythonSymbols(lines)

	names := make(map[string]string)
	for _, s := range symbols {
		names[s.Name] = s.Kind
	}

	if names["fetch_data"] != "function" {
		t.Error("expected 'fetch_data' async function")
	}
	if names["sync_func"] != "function" {
		t.Error("expected 'sync_func' function")
	}
	if names["DataHandler"] != "class" {
		t.Error("expected 'DataHandler' class")
	}
}

func TestParseInputInvalidJSON(t *testing.T) {
	input := `{invalid json}`
	_, err := parseInput(strings.NewReader(input))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestApplySymbolEditWithGenericLanguage(t *testing.T) {
	// Generic language returns nil symbols, so symbol won't be found
	lines := []string{"some content", "more content"}
	e := edit{
		Type:    "symbol",
		Symbol:  "anything",
		NewCode: "new code",
	}

	_, _, applied, err := applySymbolEdit(lines, LangGeneric, e)
	// Function returns an error when symbol not found (generic lang has no symbols)
	if err == nil {
		t.Error("expected error for generic language (no symbols)")
	}
	if applied {
		t.Error("expected not applied for generic language")
	}
	if !strings.Contains(err.Error(), "symbol not found") {
		t.Errorf("expected 'symbol not found' error, got: %v", err)
	}
}

func TestFindGoSymbolsEmptyFile(t *testing.T) {
	lines := []string{}
	symbols := findGoSymbols(lines)
	if len(symbols) != 0 {
		t.Errorf("expected 0 symbols for empty file, got %d", len(symbols))
	}
}

func TestFindPythonSymbolsNestedFunction(t *testing.T) {
	code := `def outer():
    def inner():
        pass
    return inner`
	lines := strings.Split(code, "\n")
	symbols := findPythonSymbols(lines)

	// Should only find top-level 'outer', not nested 'inner'
	if len(symbols) != 1 {
		t.Errorf("expected 1 top-level symbol, got %d", len(symbols))
	}
	if symbols[0].Name != "outer" {
		t.Errorf("expected 'outer', got %q", symbols[0].Name)
	}
}

func TestApplyReplaceEditWithinSymbolScoped(t *testing.T) {
	code := `package main

func greet() {
	fmt.Println("message")
}

func goodbye() {
	fmt.Println("message")
}`
	lines := strings.Split(code, "\n")
	e := edit{
		Type:         "replace",
		Search:       "message",
		Replace:      "replaced",
		WithinSymbol: "greet",
	}

	result, found, applied, err := applyReplaceEdit(lines, LangGo, e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Error("expected edit to be applied")
	}

	resultStr := strings.Join(result, "\n")
	// Only the greet function should have the replacement
	if !strings.Contains(resultStr, `fmt.Println("replaced")`) {
		t.Error("expected 'replaced' in greet function")
	}
	// The goodbye function should still have "message"
	if strings.Count(resultStr, "message") != 1 {
		t.Errorf("expected 1 'message' remaining in goodbye, got %d", strings.Count(resultStr, "message"))
	}
	if len(found) == 0 || found[0] != "greet" {
		t.Errorf("expected found to contain 'greet', got %v", found)
	}
}

func TestApplyReplaceEditWithinSymbolMissing(t *testing.T) {
	code := `package main

func existingFunc() {
	fmt.Println("test")
}`
	lines := strings.Split(code, "\n")
	e := edit{
		Type:         "replace",
		Search:       "test",
		Replace:      "result",
		WithinSymbol: "nonExistentFunc",
	}

	_, _, applied, err := applyReplaceEdit(lines, LangGo, e)
	if err == nil {
		t.Error("expected error when within_symbol not found")
	}
	if applied {
		t.Error("expected not applied")
	}
	if !strings.Contains(err.Error(), "symbol not found") {
		t.Errorf("expected 'symbol not found' error, got: %v", err)
	}
}

func TestApplyReplaceEditReplaceAll(t *testing.T) {
	code := `package main

func hello() {
	print("hello")
	print("hello")
	print("hello")
}`
	lines := strings.Split(code, "\n")
	e := edit{
		Type:    "replace",
		Search:  "hello",
		Replace: "world",
		All:     true,
	}

	result, _, applied, err := applyReplaceEdit(lines, LangGo, e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Error("expected edit to be applied")
	}

	resultStr := strings.Join(result, "\n")
	// All occurrences should be replaced
	helloCount := strings.Count(resultStr, "hello")
	worldCount := strings.Count(resultStr, "world")
	if helloCount != 0 {
		t.Errorf("expected 0 'hello' remaining, got %d", helloCount)
	}
	if worldCount < 4 { // func name + 3 print calls
		t.Errorf("expected at least 4 'world', got %d", worldCount)
	}
}

func TestApplyReplaceEditFirstOccurrence(t *testing.T) {
	code := `package main

func test() {
	print("old")
	print("old")
	print("old")
}`
	lines := strings.Split(code, "\n")
	e := edit{
		Type:    "replace",
		Search:  "old",
		Replace: "new",
		All:     false, // Only first occurrence
	}

	result, _, applied, err := applyReplaceEdit(lines, LangGo, e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Error("expected edit to be applied")
	}

	resultStr := strings.Join(result, "\n")
	// Only first occurrence should be replaced
	oldCount := strings.Count(resultStr, "old")
	newCount := strings.Count(resultStr, "new")
	if oldCount != 2 {
		t.Errorf("expected 2 'old' remaining, got %d", oldCount)
	}
	if newCount != 1 {
		t.Errorf("expected 1 'new', got %d", newCount)
	}
}

func TestFindIndentEndWithComments(t *testing.T) {
	code := `def func():
    x = 1
    # This is a comment
    y = 2
    return`
	lines := strings.Split(code, "\n")
	end := findIndentEnd(lines, 0, 0)

	// Comments should not end the block
	if end != 4 {
		t.Errorf("expected end at line 4, got %d", end)
	}
}

func TestFindIndentEndWithCppComments(t *testing.T) {
	code := `func hello():
    x = 1
    // C++ style comment
    y = 2
    return`
	lines := strings.Split(code, "\n")
	end := findIndentEnd(lines, 0, 0)

	// C++ comments should not end the block
	if end != 4 {
		t.Errorf("expected end at line 4, got %d", end)
	}
}

func TestApplyLinesEditInvalidRangeReversed(t *testing.T) {
	lines := []string{"line 1", "line 2", "line 3"}

	// Test end_line < start_line
	e := edit{
		Type:      "lines",
		StartLine: 3,
		EndLine:   1,
		NewCode:   "new content",
	}

	_, _, applied, err := applyLinesEdit(lines, e)
	if err == nil {
		t.Error("expected error for invalid line range")
	}
	if applied {
		t.Error("expected not applied for invalid range")
	}
	if !strings.Contains(err.Error(), "invalid line range") {
		t.Errorf("expected 'invalid line range' error, got: %v", err)
	}
}

func TestApplyLinesEditZeroStartLine(t *testing.T) {
	lines := []string{"line 1", "line 2", "line 3"}

	// Test start_line = 0 (invalid, should be 1-indexed)
	e := edit{
		Type:      "lines",
		StartLine: 0,
		EndLine:   1,
		NewCode:   "new content",
	}

	_, _, applied, err := applyLinesEdit(lines, e)
	if err == nil {
		t.Error("expected error for start_line=0")
	}
	if applied {
		t.Error("expected not applied")
	}
}

func TestApplySymbolEditMissingNewCode(t *testing.T) {
	lines := []string{"func hello() {", "}"}
	e := edit{
		Type:    "symbol",
		Symbol:  "hello",
		NewCode: "", // Missing new_code
	}

	_, _, applied, err := applySymbolEdit(lines, LangGo, e)
	if err == nil {
		t.Error("expected error for missing new_code")
	}
	if applied {
		t.Error("expected not applied")
	}
	if !strings.Contains(err.Error(), "new_code required") {
		t.Errorf("expected 'new_code required' error, got: %v", err)
	}
}

func TestApplySymbolEditMissingSymbol(t *testing.T) {
	lines := []string{"func hello() {", "}"}
	e := edit{
		Type:    "symbol",
		Symbol:  "", // Missing symbol name
		NewCode: "func hello() {}",
	}

	_, _, applied, err := applySymbolEdit(lines, LangGo, e)
	if err == nil {
		t.Error("expected error for missing symbol")
	}
	if applied {
		t.Error("expected not applied")
	}
	if !strings.Contains(err.Error(), "symbol name required") {
		t.Errorf("expected 'symbol name required' error, got: %v", err)
	}
}

func TestApplyLinesEditMissingNewCode(t *testing.T) {
	lines := []string{"line 1", "line 2", "line 3"}
	e := edit{
		Type:      "lines",
		StartLine: 1,
		EndLine:   2,
		NewCode:   "", // Missing new_code
	}

	_, _, applied, err := applyLinesEdit(lines, e)
	if err == nil {
		t.Error("expected error for missing new_code")
	}
	if applied {
		t.Error("expected not applied")
	}
	if !strings.Contains(err.Error(), "new_code required") {
		t.Errorf("expected 'new_code required' error, got: %v", err)
	}
}

func TestApplyReplaceEditMissingSearch(t *testing.T) {
	lines := []string{"some content"}
	e := edit{
		Type:    "replace",
		Search:  "", // Missing search
		Replace: "replacement",
	}

	_, _, applied, err := applyReplaceEdit(lines, LangGo, e)
	if err == nil {
		t.Error("expected error for missing search")
	}
	if applied {
		t.Error("expected not applied")
	}
	if !strings.Contains(err.Error(), "search required") {
		t.Errorf("expected 'search required' error, got: %v", err)
	}
}

func TestFindIndentEndAtEOF(t *testing.T) {
	code := `def func():
    x = 1`
	lines := strings.Split(code, "\n")
	end := findIndentEnd(lines, 0, 0)

	// Should return last line when reaching EOF
	if end != 1 {
		t.Errorf("expected end at line 1, got %d", end)
	}
}

func TestFindIndentEndSingleLine(t *testing.T) {
	code := `def func():`
	lines := strings.Split(code, "\n")
	end := findIndentEnd(lines, 0, 0)

	// Single line, should return the start line
	if end != 0 {
		t.Errorf("expected end at line 0, got %d", end)
	}
}

// Integration tests for the run function

func newTestRunnerContext(t *testing.T, stdout *bytes.Buffer, workspace string) *runner.RunnerContext {
	t.Helper()
	t.Setenv("AGENTCTL_WORKSPACE", workspace)
	state := t.TempDir()
	cfg := config.Config{
		Home:           state,
		InlineOutputKB: 32,
		MaxCaptureKB:   10240,
		Paths: config.Paths{
			CAS:   filepath.Join(state, "cas"),
			Jobs:  filepath.Join(state, "jobs"),
			Cache: filepath.Join(state, "cache"),
		},
	}
	rc, err := runner.NewRunnerContext(cfg, stdout)
	if err != nil {
		t.Fatalf("new runner context: %v", err)
	}
	return rc
}

func TestRunSymbolEdit(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()

	// Create a test Go file
	testFile := filepath.Join(workspace, "test.go")
	content := `package main

func hello() {
	fmt.Println("Hello")
}

func goodbye() {
	fmt.Println("Goodbye")
}
`
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, workspace)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Path: testFile,
		Edits: []edit{
			{
				Type:    "symbol",
				Symbol:  "hello",
				NewCode: "func hello() {\n\tfmt.Println(\"Hi there!\")\n}",
			},
		},
		DryRun:       true,
		ContextLines: 3,
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	if env["status"] != "ok" {
		t.Fatalf("expected ok status, got %v", env["status"])
	}

	data := env["data"].(map[string]any)
	if data["dry_run"] != true {
		t.Error("expected dry_run=true")
	}
	if data["edit_count"].(float64) != 1 {
		t.Errorf("expected edit_count=1, got %v", data["edit_count"])
	}

	// Verify original file is unchanged (dry_run)
	afterContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(afterContent) != content {
		t.Error("expected file unchanged in dry_run mode")
	}
}

func TestRunLinesEdit(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()

	testFile := filepath.Join(workspace, "test.txt")
	content := "line 1\nline 2\nline 3\nline 4\n"
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, workspace)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Path: testFile,
		Edits: []edit{
			{
				Type:      "lines",
				StartLine: 2,
				EndLine:   3,
				NewCode:   "new line 2\nnew line 3",
			},
		},
		DryRun: false,
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	if env["status"] != "ok" {
		t.Fatalf("expected ok status, got %v", env["status"])
	}

	data := env["data"].(map[string]any)
	if data["edited"] != true {
		t.Error("expected edited=true")
	}

	// Verify file was modified
	afterContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(afterContent), "new line 2") {
		t.Error("expected file to contain 'new line 2'")
	}
}

func TestRunReplaceEdit(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()

	testFile := filepath.Join(workspace, "test.go")
	content := `package main

func greet() {
	fmt.Println("old value")
}
`
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, workspace)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Path: testFile,
		Edits: []edit{
			{
				Type:    "replace",
				Search:  "old value",
				Replace: "new value",
			},
		},
		DryRun: false,
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	if env["status"] != "ok" {
		t.Fatalf("expected ok status, got %v", env["status"])
	}

	// Verify file was modified
	afterContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(afterContent), "new value") {
		t.Error("expected file to contain 'new value'")
	}
	if strings.Contains(string(afterContent), "old value") {
		t.Error("expected 'old value' to be replaced")
	}
}

func TestRunUnknownEditType(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()

	testFile := filepath.Join(workspace, "test.txt")
	content := "test content"
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, workspace)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Path: testFile,
		Edits: []edit{
			{
				Type:    "unknown_type",
				NewCode: "something",
			},
		},
	}

	err := run(ctx, rc, in)
	if err == nil {
		t.Error("expected error for unknown edit type")
	}
	if !strings.Contains(err.Error(), "unknown edit type") {
		t.Errorf("expected 'unknown edit type' error, got: %v", err)
	}
}

func TestRunNoChanges(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()

	testFile := filepath.Join(workspace, "test.go")
	content := `package main

func hello() {
	fmt.Println("Hello")
}
`
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, workspace)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Path: testFile,
		Edits: []edit{
			{
				Type:    "replace",
				Search:  "nonexistent",
				Replace: "replacement",
			},
		},
		DryRun: true,
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	if env["status"] != "ok" {
		t.Fatalf("expected ok status, got %v", env["status"])
	}

	data := env["data"].(map[string]any)
	if data["edit_count"].(float64) != 0 {
		t.Errorf("expected edit_count=0, got %v", data["edit_count"])
	}
	if data["message"] != "no changes made" {
		t.Errorf("expected 'no changes made' message, got %v", data["message"])
	}
}

func TestRunFileNotFound(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, workspace)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Path: filepath.Join(workspace, "nonexistent.go"),
		Edits: []edit{
			{
				Type:    "symbol",
				Symbol:  "test",
				NewCode: "func test() {}",
			},
		},
	}

	err := run(ctx, rc, in)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "read file") {
		t.Errorf("expected 'read file' error, got: %v", err)
	}
}
