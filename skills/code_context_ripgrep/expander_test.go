package main

import (
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/codeblocks"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path string
		want codeblocks.Language
	}{
		// Go
		{"main.go", codeblocks.LangGo},
		{"internal/pkg/foo.go", codeblocks.LangGo},
		{"MAIN.GO", codeblocks.LangGo},

		// Python
		{"script.py", codeblocks.LangPython},
		{"app.pyw", codeblocks.LangPython},
		{"types.pyi", codeblocks.LangPython},
		{"lib/module.py", codeblocks.LangPython},

		// JavaScript
		{"app.js", codeblocks.LangJS},
		{"component.jsx", codeblocks.LangJS},
		{"module.mjs", codeblocks.LangJS},
		{"require.cjs", codeblocks.LangJS},

		// TypeScript
		{"app.ts", codeblocks.LangTS},
		{"component.tsx", codeblocks.LangTS},
		{"module.mts", codeblocks.LangTS},
		{"require.cts", codeblocks.LangTS},

		// GDScript
		{"player.gd", codeblocks.LangGDScript},
		{"scripts/enemy.gd", codeblocks.LangGDScript},

		// Elixir
		{"lib/app.ex", codeblocks.LangElixir},
		{"script.exs", codeblocks.LangElixir},

		// Ruby
		{"lib/app.rb", codeblocks.LangRuby},
		{"script.rb", codeblocks.LangRuby},

		// Lua
		{"script.lua", codeblocks.LangLua},
		{"scripts/init.lua", codeblocks.LangLua},

		// Generic fallback
		{"readme.md", codeblocks.LangGeneric},
		{"config.yaml", codeblocks.LangGeneric},
		{"data.json", codeblocks.LangGeneric},
		{"noext", codeblocks.LangGeneric},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := codeblocks.DetectLanguage(tt.path)
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
	exp := codeblocks.NewExpander(codeblocks.LangGo, 400)

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
			wantStart:  5,
		},
		{
			name:       "match in type declaration",
			matchLine:  12, // Name string
			wantSymbol: "User",
			wantKind:   "type",
			wantStart:  10,
		},
		{
			name:       "match in method",
			matchLine:  18, // return fmt.Sprintf...
			wantSymbol: "Greet",
			wantKind:   "method",
			wantStart:  16,
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

func TestExpandGoClosure(t *testing.T) {
	source := `package main

func outer() {
	dfs := func(n int) int {
		if n == 0 { return 1 }
		return dfs(n-1)
	}
	_ = dfs(2)
}
`
	lines := strings.Split(source, "\n")
	exp := codeblocks.NewExpander(codeblocks.LangGo, 400)

	matches := []rawMatch{{File: "test.go", Line: 5, Text: lines[4]}}
	blocks := exp.ExpandMatches("test.go", lines, matches)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	b := blocks[0]
	if b.SymbolName != "dfs" {
		t.Errorf("SymbolName = %q, want %q", b.SymbolName, "dfs")
	}
	if b.SymbolKind != "closure" {
		t.Errorf("SymbolKind = %q, want %q", b.SymbolKind, "closure")
	}
	if b.StartLine != 4 {
		t.Errorf("StartLine = %d, want %d", b.StartLine, 4)
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
	exp := codeblocks.NewExpander(codeblocks.LangPython, 400)

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

func TestExpandPythonDecorators(t *testing.T) {
	source := `@decorator
def decorated():
    return 1

class Service:
    @classmethod
    def build(cls):
        return cls()
`
	lines := strings.Split(source, "\n")
	exp := codeblocks.NewExpander(codeblocks.LangPython, 400)

	tests := []struct {
		name       string
		matchLine  int
		wantSymbol string
		wantKind   string
		wantStart  int
	}{
		{
			name:       "match in decorated function",
			matchLine:  3,
			wantSymbol: "decorated",
			wantKind:   "function",
			wantStart:  1,
		},
		{
			name:       "match in decorated method",
			matchLine:  8,
			wantSymbol: "build",
			wantKind:   "function",
			wantStart:  6,
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
	exp := codeblocks.NewExpander(codeblocks.LangTS, 400)

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

func TestExpandJSTSArrowExpressions(t *testing.T) {
	source := `const inline = (x) => x + 1

class Store {
    handler = (evt) => {
        return evt.type
    }
}
`
	lines := strings.Split(source, "\n")
	exp := codeblocks.NewExpander(codeblocks.LangTS, 400)

	tests := []struct {
		name       string
		matchLine  int
		wantSymbol string
		wantKind   string
		wantStart  int
		wantEnd    int
	}{
		{
			name:       "match in inline arrow",
			matchLine:  1,
			wantSymbol: "inline",
			wantKind:   "function",
			wantStart:  1,
			wantEnd:    1,
		},
		{
			name:       "match in class field arrow",
			matchLine:  5,
			wantSymbol: "handler",
			wantKind:   "method",
			wantStart:  4,
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
			if tt.wantEnd > 0 && b.EndLine != tt.wantEnd {
				t.Errorf("EndLine = %d, want %d", b.EndLine, tt.wantEnd)
			}
		})
	}
}

func TestExpandRuby(t *testing.T) {
	source := `class Store
  def add(item)
    @items << item
  end
end

module Helpers
  def self.format(x)
    x.to_s
  end
end

handler = ->(x) do
  x + 1
end
`
	lines := strings.Split(source, "\n")
	exp := codeblocks.NewExpander(codeblocks.LangRuby, 400)

	tests := []struct {
		name       string
		matchLine  int
		wantSymbol string
		wantKind   string
		wantStart  int
	}{
		{
			name:       "match in class",
			matchLine:  1,
			wantSymbol: "Store",
			wantKind:   "class",
			wantStart:  1,
		},
		{
			name:       "match in method",
			matchLine:  3,
			wantSymbol: "add",
			wantKind:   "function",
			wantStart:  2,
		},
		{
			name:       "match in module method",
			matchLine:  9,
			wantSymbol: "format",
			wantKind:   "function",
			wantStart:  8,
		},
		{
			name:       "match in closure",
			matchLine:  14,
			wantSymbol: "handler",
			wantKind:   "closure",
			wantStart:  13,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := []rawMatch{{File: "test.rb", Line: tt.matchLine, Text: lines[tt.matchLine-1]}}
			blocks := exp.ExpandMatches("test.rb", lines, matches)

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

func TestExpandLua(t *testing.T) {
	source := `local function helper(x)
  return x * 2
end

function Store:add(item)
  self.items = self.items or {}
  table.insert(self.items, item)
end

handler = function(value)
  return value + 1
end
`
	lines := strings.Split(source, "\n")
	exp := codeblocks.NewExpander(codeblocks.LangLua, 400)

	tests := []struct {
		name       string
		matchLine  int
		wantSymbol string
		wantKind   string
		wantStart  int
	}{
		{
			name:       "match in local function",
			matchLine:  2,
			wantSymbol: "helper",
			wantKind:   "function",
			wantStart:  1,
		},
		{
			name:       "match in method",
			matchLine:  6,
			wantSymbol: "Store:add",
			wantKind:   "function",
			wantStart:  5,
		},
		{
			name:       "match in closure",
			matchLine:  11,
			wantSymbol: "handler",
			wantKind:   "closure",
			wantStart:  10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := []rawMatch{{File: "test.lua", Line: tt.matchLine, Text: lines[tt.matchLine-1]}}
			blocks := exp.ExpandMatches("test.lua", lines, matches)

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
	exp := codeblocks.NewExpander(codeblocks.LangGDScript, 400)

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

func TestExpandTopLevelFallback(t *testing.T) {
	tests := []struct {
		name      string
		lang      codeblocks.Language
		file      string
		source    string
		matchLine int
		wantStart int
		wantEnd   int
	}{
		{
			name: "go top-level",
			lang: codeblocks.LangGo,
			file: "test.go",
			source: `package main

func helper() {
    println("hi")
}

var topLevel = 42
`,
			matchLine: 7,
			wantStart: 7,
			wantEnd:   7,
		},
		{
			name: "python top-level",
			lang: codeblocks.LangPython,
			file: "test.py",
			source: `def helper():
    return 1

top_level = 2
`,
			matchLine: 4,
			wantStart: 4,
			wantEnd:   4,
		},
		{
			name: "ts top-level",
			lang: codeblocks.LangTS,
			file: "test.ts",
			source: `function helper() {
  return 1;
}

const topLevel = 2;
`,
			matchLine: 5,
			wantStart: 5,
			wantEnd:   5,
		},
		{
			name: "gdscript top-level",
			lang: codeblocks.LangGDScript,
			file: "test.gd",
			source: `func helper():
    return 1

var top_level = 2
`,
			matchLine: 4,
			wantStart: 4,
			wantEnd:   4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := strings.Split(tt.source, "\n")
			exp := codeblocks.NewExpander(tt.lang, 400)

			matches := []rawMatch{{File: tt.file, Line: tt.matchLine, Text: lines[tt.matchLine-1]}}
			blocks := exp.ExpandMatches(tt.file, lines, matches)

			if len(blocks) != 1 {
				t.Fatalf("expected 1 block, got %d", len(blocks))
			}

			b := blocks[0]
			if b.SymbolName != "" {
				t.Errorf("SymbolName = %q, want empty", b.SymbolName)
			}
			if b.SymbolKind != "" {
				t.Errorf("SymbolKind = %q, want empty", b.SymbolKind)
			}
			if b.StartLine != tt.wantStart {
				t.Errorf("StartLine = %d, want %d", b.StartLine, tt.wantStart)
			}
			if b.EndLine != tt.wantEnd {
				t.Errorf("EndLine = %d, want %d", b.EndLine, tt.wantEnd)
			}
			if !strings.Contains(b.Source, lines[tt.matchLine-1]) {
				t.Errorf("Source does not contain match line")
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
	exp := codeblocks.NewExpander(codeblocks.LangGeneric, 400)

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
	exp := codeblocks.NewExpander(codeblocks.LangGo, 400)

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
	exp := codeblocks.NewExpander(codeblocks.LangGo, 400)

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

	exp := codeblocks.NewExpander(codeblocks.LangGo, 50) // Limit to 50 lines

	matches := []rawMatch{{File: "test.go", Line: 250, Text: "// line 9"}}
	blocks := exp.ExpandMatches("test.go", lines, matches)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	b := blocks[0]
	if b.StartLine > 250 || b.EndLine < 250 {
		t.Fatalf("expected block to include match line 250, got %d-%d", b.StartLine, b.EndLine)
	}
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
			got := codeblocks.IndentLevel(tt.line)
			if got != tt.want {
				t.Errorf("codeblocks.IndentLevel(%q) = %d, want %d", tt.line, got, tt.want)
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
			got := codeblocks.TrimLine(tt.line, tt.limit)
			if got != tt.want {
				t.Errorf("codeblocks.TrimLine(%q, %d) = %q, want %q", tt.line, tt.limit, got, tt.want)
			}
		})
	}
}
