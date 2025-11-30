package main

import (
	"strings"
	"testing"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path string
		want Language
	}{
		// Go
		{"main.go", LangGo},
		{"internal/pkg/foo.go", LangGo},
		{"MAIN.GO", LangGo},

		// Python
		{"script.py", LangPython},
		{"app.pyw", LangPython},
		{"types.pyi", LangPython},
		{"lib/module.py", LangPython},

		// JavaScript
		{"app.js", LangJS},
		{"component.jsx", LangJS},
		{"module.mjs", LangJS},
		{"require.cjs", LangJS},

		// TypeScript
		{"app.ts", LangTS},
		{"component.tsx", LangTS},
		{"module.mts", LangTS},
		{"require.cts", LangTS},

		// GDScript
		{"player.gd", LangGDScript},
		{"scripts/enemy.gd", LangGDScript},

		// Generic fallback
		{"readme.md", LangGeneric},
		{"config.yaml", LangGeneric},
		{"data.json", LangGeneric},
		{"script.rb", LangGeneric},
		{"noext", LangGeneric},
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

func TestExpandGo(t *testing.T) {
	source := `package main

import "fmt"

// Helper function
func helper(x int) int {
	return x * 2
}

// User handles user operations
type User struct {
	Name string
	Age  int
}

// Greet returns a greeting
func (u *User) Greet() string {
	return fmt.Sprintf("Hello, %s!", u.Name)
}

func main() {
	u := &User{Name: "Alice", Age: 30}
	fmt.Println(u.Greet())
	fmt.Println(helper(21))
}
`
	lines := strings.Split(source, "\n")
	exp := NewExpander(LangGo, 400)

	tests := []struct {
		name       string
		matchLine  int // 1-indexed
		wantSymbol string
		wantKind   string
		wantStart  int // 1-indexed
	}{
		{
			name:       "match in helper function",
			matchLine:  7, // return x * 2
			wantSymbol: "helper",
			wantKind:   "function",
			wantStart:  6,
		},
		{
			name:       "match in type declaration",
			matchLine:  12, // Name string
			wantSymbol: "User",
			wantKind:   "type",
			wantStart:  11,
		},
		{
			name:       "match in method",
			matchLine:  18, // return fmt.Sprintf...
			wantSymbol: "Greet",
			wantKind:   "method",
			wantStart:  17,
		},
		{
			name:       "match in main",
			matchLine:  23, // fmt.Println(u.Greet())
			wantSymbol: "main",
			wantKind:   "function",
			wantStart:  21,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := []rawMatch{{File: "test.go", Line: tt.matchLine, Text: lines[tt.matchLine-1]}}
			blocks := exp.ExpandMatches("test.go", lines, matches)

			if len(blocks) != 1 {
				t.Fatalf("expected 1 block, got %d", len(blocks))
			}

			b := blocks[0]
			if b.SymbolName != tt.wantSymbol {
				t.Errorf("SymbolName = %q, want %q", b.SymbolName, tt.wantSymbol)
			}
			if b.SymbolKind != tt.wantKind {
				t.Errorf("SymbolKind = %q, want %q", b.SymbolKind, tt.wantKind)
			}
			if b.StartLine != tt.wantStart {
				t.Errorf("StartLine = %d, want %d", b.StartLine, tt.wantStart)
			}
			if b.MatchCount != 1 {
				t.Errorf("MatchCount = %d, want 1", b.MatchCount)
			}
			if !strings.Contains(b.Source, lines[tt.matchLine-1]) {
				t.Errorf("Source does not contain match line")
			}
		})
	}
}

func TestExpandPython(t *testing.T) {
	source := `#!/usr/bin/env python3
"""Module docstring."""

import os

def helper(x):
    """Helper function."""
    return x * 2

class User:
    """User class."""
    
    def __init__(self, name, age):
        self.name = name
        self.age = age
    
    def greet(self):
        """Return greeting."""
        return f"Hello, {self.name}!"
    
    async def fetch_data(self):
        """Async method."""
        await some_api()
        return self.name

def main():
    u = User("Alice", 30)
    print(u.greet())
    print(helper(21))

if __name__ == "__main__":
    main()
`
	lines := strings.Split(source, "\n")
	exp := NewExpander(LangPython, 400)

	tests := []struct {
		name       string
		matchLine  int
		wantSymbol string
		wantKind   string
		wantStart  int
	}{
		{
			name:       "match in helper function",
			matchLine:  8, // return x * 2
			wantSymbol: "helper",
			wantKind:   "function",
			wantStart:  6,
		},
		{
			name:       "match in __init__ method",
			matchLine:  15, // self.age = age
			wantSymbol: "__init__",
			wantKind:   "function",
			wantStart:  13,
		},
		{
			name:       "match in greet method",
			matchLine:  20, // return f"Hello..."
			wantSymbol: "greet",
			wantKind:   "function",
			wantStart:  17, // includes docstring line
		},
		{
			name:       "match in async method",
			matchLine:  24, // await some_api()
			wantSymbol: "fetch_data",
			wantKind:   "function",
			wantStart:  21, // includes blank line before
		},
		{
			name:       "match in main",
			matchLine:  29, // print(u.greet())
			wantSymbol: "main",
			wantKind:   "function",
			wantStart:  26, // 0-indexed + 1 = line 27 is "def main():"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := []rawMatch{{File: "test.py", Line: tt.matchLine, Text: lines[tt.matchLine-1]}}
			blocks := exp.ExpandMatches("test.py", lines, matches)

			if len(blocks) != 1 {
				t.Fatalf("expected 1 block, got %d", len(blocks))
			}

			b := blocks[0]
			if b.SymbolName != tt.wantSymbol {
				t.Errorf("SymbolName = %q, want %q", b.SymbolName, tt.wantSymbol)
			}
			if b.SymbolKind != tt.wantKind {
				t.Errorf("SymbolKind = %q, want %q", b.SymbolKind, tt.wantKind)
			}
			if b.StartLine != tt.wantStart {
				t.Errorf("StartLine = %d, want %d", b.StartLine, tt.wantStart)
			}
		})
	}
}

func TestExpandJSTS(t *testing.T) {
	source := `// Module
import { something } from './other';

function helper(x) {
    return x * 2;
}

const arrowFunc = async (data) => {
    const result = await process(data);
    return result;
};

export class User {
    constructor(name, age) {
        this.name = name;
        this.age = age;
    }

    greet() {
        return ` + "`Hello, ${this.name}!`" + `;
    }

    async fetchData() {
        const data = await api.get();
        return data;
    }
}

export interface UserConfig {
    name: string;
    age: number;
}

export type UserId = string | number;

export async function main() {
    const u = new User("Alice", 30);
    console.log(u.greet());
}
`
	lines := strings.Split(source, "\n")
	exp := NewExpander(LangTS, 400)

	tests := []struct {
		name       string
		matchLine  int
		wantSymbol string
		wantKind   string
		wantStart  int
	}{
		{
			name:       "match in function",
			matchLine:  5, // return x * 2
			wantSymbol: "helper",
			wantKind:   "function",
			wantStart:  4,
		},
		{
			name:       "match in arrow function",
			matchLine:  9, // const result = await...
			wantSymbol: "arrowFunc",
			wantKind:   "function",
			wantStart:  8,
		},
		{
			name:       "match in constructor",
			matchLine:  15, // this.age = age
			wantSymbol: "constructor",
			wantKind:   "method",
			wantStart:  14, // constructor line is 14
		},
		{
			name:       "match in interface",
			matchLine:  30, // name: string
			wantSymbol: "UserConfig",
			wantKind:   "interface",
			wantStart:  29,
		},
		{
			name:       "match in type alias",
			matchLine:  34, // string | number
			wantSymbol: "UserId",
			wantKind:   "type",
			wantStart:  34,
		},
		{
			name:       "match in exported async function",
			matchLine:  38, // console.log
			wantSymbol: "main",
			wantKind:   "function",
			wantStart:  36,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := []rawMatch{{File: "test.ts", Line: tt.matchLine, Text: lines[tt.matchLine-1]}}
			blocks := exp.ExpandMatches("test.ts", lines, matches)

			if len(blocks) != 1 {
				t.Fatalf("expected 1 block, got %d", len(blocks))
			}

			b := blocks[0]
			if b.SymbolName != tt.wantSymbol {
				t.Errorf("SymbolName = %q, want %q", b.SymbolName, tt.wantSymbol)
			}
			if b.SymbolKind != tt.wantKind {
				t.Errorf("SymbolKind = %q, want %q", b.SymbolKind, tt.wantKind)
			}
			if b.StartLine != tt.wantStart {
				t.Errorf("StartLine = %d, want %d", b.StartLine, tt.wantStart)
			}
		})
	}
}

func TestExpandGDScript(t *testing.T) {
	source := `extends CharacterBody2D
class_name Player

signal health_changed(new_health)

@export var speed: float = 200.0
@export var health: int = 100

var velocity_component: VelocityComponent

func _ready():
	velocity_component = $VelocityComponent
	health_changed.connect(_on_health_changed)

func _physics_process(delta):
	var input_dir = Input.get_vector("left", "right", "up", "down")
	velocity = input_dir * speed
	move_and_slide()

func take_damage(amount: int):
	health -= amount
	health_changed.emit(health)
	if health <= 0:
		die()

func die():
	queue_free()

class InnerHelper:
	var data: Dictionary
	
	func process_data():
		return data.keys()
`
	lines := strings.Split(source, "\n")
	exp := NewExpander(LangGDScript, 400)

	tests := []struct {
		name       string
		matchLine  int
		wantSymbol string
		wantKind   string
		wantStart  int
	}{
		{
			name:       "match in _ready",
			matchLine:  13, // health_changed.connect
			wantSymbol: "_ready",
			wantKind:   "function",
			wantStart:  11,
		},
		{
			name:       "match in _physics_process",
			matchLine:  17, // var input_dir
			wantSymbol: "_physics_process",
			wantKind:   "function",
			wantStart:  15,
		},
		{
			name:       "match in take_damage",
			matchLine:  22, // health -= amount
			wantSymbol: "take_damage",
			wantKind:   "function",
			wantStart:  20, // blank line before func
		},
		{
			name:       "match in die",
			matchLine:  28, // queue_free()
			wantSymbol: "die",
			wantKind:   "function",
			wantStart:  26, // blank line before func
		},
		{
			name:       "match in inner class",
			matchLine:  34,            // return data.keys()
			wantSymbol: "InnerHelper", // Finds enclosing class first
			wantKind:   "class",
			wantStart:  29, // class InnerHelper line
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := []rawMatch{{File: "player.gd", Line: tt.matchLine, Text: lines[tt.matchLine-1]}}
			blocks := exp.ExpandMatches("player.gd", lines, matches)

			if len(blocks) != 1 {
				t.Fatalf("expected 1 block, got %d", len(blocks))
			}

			b := blocks[0]
			if b.SymbolName != tt.wantSymbol {
				t.Errorf("SymbolName = %q, want %q", b.SymbolName, tt.wantSymbol)
			}
			if b.SymbolKind != tt.wantKind {
				t.Errorf("SymbolKind = %q, want %q", b.SymbolKind, tt.wantKind)
			}
			if b.StartLine != tt.wantStart {
				t.Errorf("StartLine = %d, want %d", b.StartLine, tt.wantStart)
			}
		})
	}
}

func TestExpandGeneric(t *testing.T) {
	source := `First block line 1
First block line 2
First block line 3

Second block line 1
Second block line 2

Third block line 1
`
	lines := strings.Split(source, "\n")
	exp := NewExpander(LangGeneric, 400)

	tests := []struct {
		name      string
		matchLine int
		wantStart int
		wantEnd   int
	}{
		{
			name:      "match in first block",
			matchLine: 2,
			wantStart: 1,
			wantEnd:   3,
		},
		{
			name:      "match in second block",
			matchLine: 5,
			wantStart: 5,
			wantEnd:   6,
		},
		{
			name:      "match in third block",
			matchLine: 8,
			wantStart: 8,
			wantEnd:   8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := []rawMatch{{File: "test.txt", Line: tt.matchLine, Text: lines[tt.matchLine-1]}}
			blocks := exp.ExpandMatches("test.txt", lines, matches)

			if len(blocks) != 1 {
				t.Fatalf("expected 1 block, got %d", len(blocks))
			}

			b := blocks[0]
			if b.StartLine != tt.wantStart {
				t.Errorf("StartLine = %d, want %d", b.StartLine, tt.wantStart)
			}
			if b.EndLine != tt.wantEnd {
				t.Errorf("EndLine = %d, want %d", b.EndLine, tt.wantEnd)
			}
		})
	}
}

func TestExpandDeduplication(t *testing.T) {
	source := `package main

func example() {
	// First match
	doSomething()
	// Second match
	doSomethingElse()
	// Third match
	return
}
`
	lines := strings.Split(source, "\n")
	exp := NewExpander(LangGo, 400)

	// Multiple matches in same function should produce one block
	matches := []rawMatch{
		{File: "test.go", Line: 4, Text: "// First match"},
		{File: "test.go", Line: 6, Text: "// Second match"},
		{File: "test.go", Line: 8, Text: "// Third match"},
	}

	blocks := exp.ExpandMatches("test.go", lines, matches)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 deduplicated block, got %d", len(blocks))
	}

	b := blocks[0]
	if b.MatchCount != 3 {
		t.Errorf("MatchCount = %d, want 3", b.MatchCount)
	}
	if len(b.MatchLines) != 3 {
		t.Errorf("len(MatchLines) = %d, want 3", len(b.MatchLines))
	}
}

func TestExpandMultipleFunctions(t *testing.T) {
	source := `package main

func first() {
	// match in first
}

func second() {
	// match in second
}

func third() {
	// match in third
}
`
	lines := strings.Split(source, "\n")
	exp := NewExpander(LangGo, 400)

	matches := []rawMatch{
		{File: "test.go", Line: 4, Text: "// match in first"},
		{File: "test.go", Line: 8, Text: "// match in second"},
		{File: "test.go", Line: 12, Text: "// match in third"},
	}

	blocks := exp.ExpandMatches("test.go", lines, matches)

	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}

	expectedSymbols := []string{"first", "second", "third"}
	for i, b := range blocks {
		if b.SymbolName != expectedSymbols[i] {
			t.Errorf("block[%d].SymbolName = %q, want %q", i, b.SymbolName, expectedSymbols[i])
		}
	}
}

func TestExpandMaxBlockLines(t *testing.T) {
	// Create a very long function
	var lines []string
	lines = append(lines, "func veryLong() {")
	for i := 0; i < 500; i++ {
		lines = append(lines, "    // line "+string(rune('0'+i%10)))
	}
	lines = append(lines, "}")

	exp := NewExpander(LangGo, 50) // Limit to 50 lines

	matches := []rawMatch{{File: "test.go", Line: 250, Text: "// line 9"}}
	blocks := exp.ExpandMatches("test.go", lines, matches)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	b := blocks[0]
	blockLines := b.EndLine - b.StartLine + 1
	if blockLines > 51 { // Allow some margin for clamping
		t.Errorf("block has %d lines, expected <= 51", blockLines)
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
		{"  \t  mixed", 8}, // 2 spaces + 1 tab (4) + 2 spaces = 8
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

func TestTrimLine(t *testing.T) {
	tests := []struct {
		line  string
		limit int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is longer than ten", 10, "this is lo..."},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got := trimLine(tt.line, tt.limit)
			if got != tt.want {
				t.Errorf("trimLine(%q, %d) = %q, want %q", tt.line, tt.limit, got, tt.want)
			}
		})
	}
}
