package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestParseInput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, in input)
	}{
		{
			name:  "minimal valid input",
			input: `{"pattern": "TODO"}`,
			check: func(t *testing.T, in input) {
				if in.Pattern != "TODO" {
					t.Errorf("Pattern = %q, want TODO", in.Pattern)
				}
				if in.MaxMatches != 10000 {
					t.Errorf("MaxMatches = %d, want 10000 (default)", in.MaxMatches)
				}
				if in.MaxBlocks != 50 {
					t.Errorf("MaxBlocks = %d, want 50 (default)", in.MaxBlocks)
				}
				if in.MaxBlockLines != 400 {
					t.Errorf("MaxBlockLines = %d, want 400 (default)", in.MaxBlockLines)
				}
			},
		},
		{
			name:  "full input",
			input: `{"pattern": "func", "path": ".", "case_insensitive": true, "glob": ["*.go"], "glob_not": ["vendor/*"], "max_matches": 100, "max_blocks": 10, "max_block_lines": 200, "hidden": true}`,
			check: func(t *testing.T, in input) {
				if in.Pattern != "func" {
					t.Errorf("Pattern = %q, want func", in.Pattern)
				}
				if in.Path != "." {
					t.Errorf("Path = %q, want .", in.Path)
				}
				if !in.CaseInsensitive {
					t.Error("CaseInsensitive = false, want true")
				}
				if len(in.Glob) != 1 || in.Glob[0] != "*.go" {
					t.Errorf("Glob = %v, want [*.go]", in.Glob)
				}
				if len(in.GlobNot) != 1 || in.GlobNot[0] != "vendor/*" {
					t.Errorf("GlobNot = %v, want [vendor/*]", in.GlobNot)
				}
				if in.MaxMatches != 100 {
					t.Errorf("MaxMatches = %d, want 100", in.MaxMatches)
				}
				if in.MaxBlocks != 10 {
					t.Errorf("MaxBlocks = %d, want 10", in.MaxBlocks)
				}
				if in.MaxBlockLines != 200 {
					t.Errorf("MaxBlockLines = %d, want 200", in.MaxBlockLines)
				}
				if !in.Hidden {
					t.Error("Hidden = false, want true")
				}
			},
		},
		{
			name:    "missing pattern",
			input:   `{"path": "."}`,
			wantErr: true,
		},
		{
			name:    "empty pattern",
			input:   `{"pattern": "  "}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   `{not json}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, err := parseInput(bytes.NewReader([]byte(tt.input)))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, in)
			}
		})
	}
}

func TestBuildRipgrepArgs(t *testing.T) {
	tests := []struct {
		name       string
		input      input
		searchPath string
		wantArgs   []string
	}{
		{
			name: "basic pattern",
			input: input{
				Pattern:    "TODO",
				MaxMatches: 100,
			},
			searchPath: "/workspace",
			wantArgs:   []string{"--json", "--no-heading", "--line-number", "--no-messages", "--max-count", "100", "--glob", "!.git", "--glob", "!node_modules", "--glob", "!vendor", "--glob", "!__pycache__", "--glob", "!.godot", "--", "TODO", "/workspace"},
		},
		{
			name: "with globs",
			input: input{
				Pattern:    "func",
				MaxMatches: 50,
				Glob:       []string{"*.go", "*.py"},
				GlobNot:    []string{"test/*"},
			},
			searchPath: ".",
			wantArgs:   []string{"--json", "--no-heading", "--line-number", "--no-messages", "--max-count", "50", "--glob", "*.go", "--glob", "*.py", "--glob", "!test/*", "--", "func", "."},
		},
		{
			name: "case insensitive and hidden",
			input: input{
				Pattern:         "error",
				MaxMatches:      100,
				CaseInsensitive: true,
				Hidden:          true,
			},
			searchPath: "/src",
			wantArgs:   []string{"--json", "--no-heading", "--line-number", "--no-messages", "--max-count", "100", "--ignore-case", "--hidden", "--glob", "!.git", "--glob", "!node_modules", "--glob", "!vendor", "--glob", "!__pycache__", "--glob", "!.godot", "--", "error", "/src"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRipgrepArgs(tt.input, tt.searchPath)
			if len(got) != len(tt.wantArgs) {
				t.Errorf("len(args) = %d, want %d\ngot:  %v\nwant: %v", len(got), len(tt.wantArgs), got, tt.wantArgs)
				return
			}
			for i, arg := range got {
				if arg != tt.wantArgs[i] {
					t.Errorf("args[%d] = %q, want %q", i, arg, tt.wantArgs[i])
				}
			}
		})
	}
}

func TestGroupByFile(t *testing.T) {
	matches := []rawMatch{
		{File: "a.go", Line: 1, Text: "line1"},
		{File: "b.go", Line: 2, Text: "line2"},
		{File: "a.go", Line: 3, Text: "line3"},
		{File: "c.go", Line: 4, Text: "line4"},
		{File: "a.go", Line: 5, Text: "line5"},
	}

	grouped := groupByFile(matches)

	if len(grouped) != 3 {
		t.Errorf("expected 3 files, got %d", len(grouped))
	}

	if len(grouped["a.go"]) != 3 {
		t.Errorf("a.go should have 3 matches, got %d", len(grouped["a.go"]))
	}

	if len(grouped["b.go"]) != 1 {
		t.Errorf("b.go should have 1 match, got %d", len(grouped["b.go"]))
	}

	if len(grouped["c.go"]) != 1 {
		t.Errorf("c.go should have 1 match, got %d", len(grouped["c.go"]))
	}
}

func TestPrepareBlockPreview(t *testing.T) {
	blocks := []Block{
		{File: "a.go", StartLine: 1, EndLine: 10, SymbolName: "foo", MatchCount: 2},
		{File: "b.go", StartLine: 5, EndLine: 15, SymbolName: "bar", MatchCount: 1},
		{File: "c.go", StartLine: 20, EndLine: 30, SymbolName: "baz", MatchCount: 3},
	}

	// Limit to 2
	preview := prepareBlockPreview(blocks, 2)
	if len(preview) != 2 {
		t.Errorf("expected 2 preview items, got %d", len(preview))
	}
	if preview[0].SymbolName != "foo" {
		t.Errorf("first preview should be foo, got %s", preview[0].SymbolName)
	}
	if preview[1].SymbolName != "bar" {
		t.Errorf("second preview should be bar, got %s", preview[1].SymbolName)
	}

	// No limit exceeded
	fullPreview := prepareBlockPreview(blocks, 10)
	if len(fullPreview) != 3 {
		t.Errorf("expected 3 preview items, got %d", len(fullPreview))
	}
}

func TestSummarizeTopFiles(t *testing.T) {
	counts := map[string]int{
		"a.go": 5,
		"b.go": 10,
		"c.go": 3,
		"d.go": 7,
	}

	summary := summarizeTopFiles(counts, 2)
	if len(summary) != 2 {
		t.Errorf("expected 2 items, got %d", len(summary))
	}

	// Should be sorted by count descending
	if summary[0][0] != "b.go" {
		t.Errorf("first file should be b.go (10), got %s", summary[0][0])
	}
	if summary[0][1] != 10 {
		t.Errorf("first count should be 10, got %v", summary[0][1])
	}

	if summary[1][0] != "d.go" {
		t.Errorf("second file should be d.go (7), got %s", summary[1][0])
	}
}

func TestRelativeTo(t *testing.T) {
	tests := []struct {
		base   string
		target string
		want   string
	}{
		{"/workspace", "/workspace/src/main.go", "src/main.go"},
		{"/workspace", "/other/file.go", "/other/file.go"},
		{"", "src/main.go", "src/main.go"},
		{"/workspace", "/workspace", "."},
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

func TestBlockJSON(t *testing.T) {
	block := Block{
		File:       "main.go",
		Language:   "go",
		StartLine:  10,
		EndLine:    20,
		HeaderLine: "func main() {",
		SymbolName: "main",
		SymbolKind: "function",
		Source:     "func main() {\n\tfmt.Println(\"hello\")\n}",
		MatchLines: []int{11, 12},
		MatchCount: 2,
	}

	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("failed to marshal block: %v", err)
	}

	var decoded Block
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal block: %v", err)
	}

	if decoded.File != block.File {
		t.Errorf("File = %q, want %q", decoded.File, block.File)
	}
	if decoded.SymbolName != block.SymbolName {
		t.Errorf("SymbolName = %q, want %q", decoded.SymbolName, block.SymbolName)
	}
	if decoded.MatchCount != block.MatchCount {
		t.Errorf("MatchCount = %d, want %d", decoded.MatchCount, block.MatchCount)
	}
}
