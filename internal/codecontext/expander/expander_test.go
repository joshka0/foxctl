package expander_test

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/codecontext/expander"
	"github.com/jkatigb/agentctl/internal/codecontext/files"
)

// mockFileContent creates a FileContent from source lines for testing.
func mockFileContent(path, language string, lines []string) *files.FileContent {
	content := make([]byte, 0)
	offsets := make([]int, len(lines))

	for i, line := range lines {
		offsets[i] = len(content)
		content = append(content, []byte(line)...)
		content = append(content, '\n')
	}

	return &files.FileContent{
		Path:        path,
		Content:     content,
		Lines:       lines,
		LineOffsets: offsets,
		Language:    language,
		Truncated:   false,
	}
}

func TestGoExpander_FindBlock_Function(t *testing.T) {
	source := []string{
		"package main",
		"",
		"func Hello() {",
		"\tprintln(\"Hello\")",
		"\tprintln(\"World\")",
		"}",
		"",
		"func Goodbye() {",
		"\tprintln(\"Bye\")",
		"}",
	}
	content := mockFileContent("test.go", "go", source)

	exp := expander.Get("go")
	if exp == nil {
		t.Fatal("Go expander not registered")
	}

	// Find block from function definition line
	start, end, symbol, err := exp.FindBlock(content, 3)
	if err != nil {
		t.Fatalf("FindBlock failed: %v", err)
	}
	if start != 3 || end != 6 {
		t.Errorf("Expected lines 3-6, got %d-%d", start, end)
	}
	if symbol != "Hello" {
		t.Errorf("Expected symbol 'Hello', got '%s'", symbol)
	}

	// Find block from inside function
	start, end, _, err = exp.FindBlock(content, 4)
	if err != nil {
		t.Fatalf("FindBlock failed: %v", err)
	}
	if start != 3 || end != 6 {
		t.Errorf("Expected lines 3-6, got %d-%d", start, end)
	}
}

func TestGoExpander_FindBlock_Method(t *testing.T) {
	source := []string{
		"package main",
		"",
		"type Server struct{}",
		"",
		"func (s *Server) Start() {",
		"\ts.running = true",
		"}",
	}
	content := mockFileContent("test.go", "go", source)

	exp := expander.Get("go")

	start, end, symbol, err := exp.FindBlock(content, 5)
	if err != nil {
		t.Fatalf("FindBlock failed: %v", err)
	}
	if start != 5 || end != 7 {
		t.Errorf("Expected lines 5-7, got %d-%d", start, end)
	}
	if symbol != "Start" {
		t.Errorf("Expected symbol 'Start', got '%s'", symbol)
	}
}

func TestGoExpander_ExpandToSymbol(t *testing.T) {
	source := []string{
		"package main",
		"",
		"func First() {",
		"\treturn",
		"}",
		"",
		"func Second() {",
		"\treturn",
		"}",
	}
	content := mockFileContent("test.go", "go", source)

	exp := expander.Get("go")

	start, end, err := exp.ExpandToSymbol(content, "Second")
	if err != nil {
		t.Fatalf("ExpandToSymbol failed: %v", err)
	}
	if start != 7 || end != 9 {
		t.Errorf("Expected lines 7-9, got %d-%d", start, end)
	}
}

func TestPythonExpander_FindBlock(t *testing.T) {
	source := []string{
		"class MyClass:",
		"    def __init__(self):",
		"        self.value = 0",
		"",
		"    def get_value(self):",
		"        return self.value",
		"",
		"def standalone():",
		"    print('hello')",
	}
	content := mockFileContent("test.py", "python", source)

	exp := expander.Get("python")
	if exp == nil {
		t.Fatal("Python expander not registered")
	}

	// Find block from method
	start, end, symbol, err := exp.FindBlock(content, 5)
	if err != nil {
		t.Fatalf("FindBlock failed: %v", err)
	}
	if start != 5 || end != 6 {
		t.Errorf("Expected lines 5-6, got %d-%d", start, end)
	}
	if symbol != "get_value" {
		t.Errorf("Expected symbol 'get_value', got '%s'", symbol)
	}

	// Find block from standalone function
	start, end, symbol, err = exp.FindBlock(content, 8)
	if err != nil {
		t.Fatalf("FindBlock failed: %v", err)
	}
	if start != 8 || end != 9 {
		t.Errorf("Expected lines 8-9, got %d-%d", start, end)
	}
	if symbol != "standalone" {
		t.Errorf("Expected symbol 'standalone', got '%s'", symbol)
	}
}

func TestPythonExpander_ExpandToSymbol(t *testing.T) {
	source := []string{
		"def first():",
		"    pass",
		"",
		"def second():",
		"    pass",
	}
	content := mockFileContent("test.py", "python", source)

	exp := expander.Get("python")

	start, end, err := exp.ExpandToSymbol(content, "second")
	if err != nil {
		t.Fatalf("ExpandToSymbol failed: %v", err)
	}
	if start != 4 || end != 5 {
		t.Errorf("Expected lines 4-5, got %d-%d", start, end)
	}
}

func TestJSExpander_FindBlock_Function(t *testing.T) {
	source := []string{
		"function hello() {",
		"  console.log('hello');",
		"}",
		"",
		"const arrow = () => {",
		"  return 42;",
		"};",
	}
	content := mockFileContent("test.js", "javascript", source)

	exp := expander.Get("javascript")
	if exp == nil {
		t.Fatal("JavaScript expander not registered")
	}

	// Find function block
	start, end, symbol, err := exp.FindBlock(content, 1)
	if err != nil {
		t.Fatalf("FindBlock failed: %v", err)
	}
	if start != 1 || end != 3 {
		t.Errorf("Expected lines 1-3, got %d-%d", start, end)
	}
	if symbol != "hello" {
		t.Errorf("Expected symbol 'hello', got '%s'", symbol)
	}

	// Find arrow function block
	start, end, symbol, err = exp.FindBlock(content, 5)
	if err != nil {
		t.Fatalf("FindBlock failed: %v", err)
	}
	if start != 5 || end != 7 {
		t.Errorf("Expected lines 5-7, got %d-%d", start, end)
	}
	if symbol != "arrow" {
		t.Errorf("Expected symbol 'arrow', got '%s'", symbol)
	}
}

func TestJSExpander_FindBlock_Class(t *testing.T) {
	source := []string{
		"class MyClass {",
		"  constructor() {",
		"    this.value = 0;",
		"  }",
		"",
		"  getValue() {",
		"    return this.value;",
		"  }",
		"}",
	}
	content := mockFileContent("test.js", "javascript", source)

	exp := expander.Get("javascript")

	// Find class block
	start, end, symbol, err := exp.FindBlock(content, 1)
	if err != nil {
		t.Fatalf("FindBlock failed: %v", err)
	}
	if start != 1 || end != 9 {
		t.Errorf("Expected lines 1-9, got %d-%d", start, end)
	}
	if symbol != "MyClass" {
		t.Errorf("Expected symbol 'MyClass', got '%s'", symbol)
	}
}

func TestTSExpander_Interface(t *testing.T) {
	source := []string{
		"interface User {",
		"  name: string;",
		"  age: number;",
		"}",
	}
	content := mockFileContent("test.ts", "typescript", source)

	exp := expander.Get("typescript")
	if exp == nil {
		t.Fatal("TypeScript expander not registered")
	}

	start, end, symbol, err := exp.FindBlock(content, 1)
	if err != nil {
		t.Fatalf("FindBlock failed: %v", err)
	}
	if start != 1 || end != 4 {
		t.Errorf("Expected lines 1-4, got %d-%d", start, end)
	}
	if symbol != "User" {
		t.Errorf("Expected symbol 'User', got '%s'", symbol)
	}
}

func TestGDScriptExpander_FindBlock(t *testing.T) {
	source := []string{
		"class_name Player",
		"",
		"var health = 100",
		"",
		"func take_damage(amount):",
		"    health -= amount",
		"    if health <= 0:",
		"        die()",
		"",
		"func die():",
		"    queue_free()",
	}
	content := mockFileContent("player.gd", "gdscript", source)

	exp := expander.Get("gdscript")
	if exp == nil {
		t.Fatal("GDScript expander not registered")
	}

	// Find function block
	start, end, symbol, err := exp.FindBlock(content, 5)
	if err != nil {
		t.Fatalf("FindBlock failed: %v", err)
	}
	if start != 5 || end != 8 {
		t.Errorf("Expected lines 5-8, got %d-%d", start, end)
	}
	if symbol != "take_damage" {
		t.Errorf("Expected symbol 'take_damage', got '%s'", symbol)
	}
}

func TestGenericExpander_Fallback(t *testing.T) {
	source := []string{
		"function myFunc() {",
		"  doSomething();",
		"}",
	}
	content := mockFileContent("test.xyz", "generic", source)

	exp := expander.GetOrGeneric("unknown_language")
	if exp == nil {
		t.Fatal("Generic expander not available as fallback")
	}

	start, end, symbol, err := exp.FindBlock(content, 1)
	if err != nil {
		t.Fatalf("FindBlock failed: %v", err)
	}
	if start != 1 || end != 3 {
		t.Errorf("Expected lines 1-3, got %d-%d", start, end)
	}
	if symbol != "myFunc" {
		t.Errorf("Expected symbol 'myFunc', got '%s'", symbol)
	}
}

func TestExpander_LineOutOfRange(t *testing.T) {
	source := []string{
		"func test() {}",
	}
	content := mockFileContent("test.go", "go", source)

	exp := expander.Get("go")

	_, _, _, err := exp.FindBlock(content, 0)
	if err == nil {
		t.Error("Expected error for line 0")
	}

	_, _, _, err = exp.FindBlock(content, 10)
	if err == nil {
		t.Error("Expected error for line beyond file")
	}
}

func TestExpander_SymbolNotFound(t *testing.T) {
	source := []string{
		"func existing() {}",
	}
	content := mockFileContent("test.go", "go", source)

	exp := expander.Get("go")

	_, _, err := exp.ExpandToSymbol(content, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent symbol")
	}
}

func TestRegistry_Languages(t *testing.T) {
	languages := expander.Languages()

	// Check that expected languages are registered
	expected := []string{"go", "python", "javascript", "typescript", "gdscript", "generic"}
	for _, lang := range expected {
		found := false
		for _, registered := range languages {
			if registered == lang {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected language '%s' to be registered", lang)
		}
	}
}

func TestBraceUtils_FindBraceEnd(t *testing.T) {
	lines := []string{
		"func test() {",
		"  if true {",
		"    nested()",
		"  }",
		"}",
	}

	style := expander.DefaultBraceStyle()
	end := expander.FindBraceEnd(lines, 0, style)

	if end != 4 {
		t.Errorf("Expected end at line 4, got %d", end)
	}
}

func TestBraceUtils_FindBraceEnd_WithStrings(t *testing.T) {
	lines := []string{
		"func test() {",
		`  s := "{ not a brace }"`,
		`  s2 := '{'`,
		"}",
	}

	style := expander.DefaultBraceStyle()
	end := expander.FindBraceEnd(lines, 0, style)

	if end != 3 {
		t.Errorf("Expected end at line 3, got %d", end)
	}
}

func TestBraceUtils_CountLeadingWhitespace(t *testing.T) {
	tests := []struct {
		line string
		want int
	}{
		{"no indent", 0},
		{"  two spaces", 2},
		{"    four spaces", 4},
		{"\ttab", 4},
		{"\t  tab and spaces", 6},
	}

	for _, tc := range tests {
		got := expander.CountLeadingWhitespace(tc.line)
		if got != tc.want {
			t.Errorf("CountLeadingWhitespace(%q) = %d, want %d", tc.line, got, tc.want)
		}
	}
}

func TestBraceUtils_IsBlankOrComment(t *testing.T) {
	tests := []struct {
		line    string
		comment string
		want    bool
	}{
		{"", "//", true},
		{"   ", "//", true},
		{"// comment", "//", true},
		{"   // indented comment", "//", true},
		{"code // with comment", "//", false},
		{"# python comment", "#", true},
		{"code", "//", false},
	}

	for _, tc := range tests {
		got := expander.IsBlankOrComment(tc.line, tc.comment)
		if got != tc.want {
			t.Errorf("IsBlankOrComment(%q, %q) = %v, want %v", tc.line, tc.comment, got, tc.want)
		}
	}
}

func TestBraceUtils_FindBlockByIndentation(t *testing.T) {
	lines := []string{
		"def test():",
		"    line1",
		"    line2",
		"    if True:",
		"        nested",
		"    line3",
		"",
		"def other():",
	}

	end := expander.FindBlockByIndentation(lines, 0, "#")
	if end != 5 {
		t.Errorf("Expected end at line 5, got %d", end)
	}
}
