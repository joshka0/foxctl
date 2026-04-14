package ripgrep

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name       string
		opts       SearchOpts
		jsonOutput bool
		wantArgs   []string
		dontWant   []string
	}{
		{
			name: "basic search",
			opts: SearchOpts{
				Pattern: "test",
			},
			jsonOutput: true,
			wantArgs:   []string{"--json", "--no-heading", "--line-number", "test"},
		},
		{
			name: "case insensitive",
			opts: SearchOpts{
				Pattern:         "test",
				CaseInsensitive: true,
			},
			jsonOutput: true,
			wantArgs:   []string{"--ignore-case"},
		},
		{
			name: "fixed strings",
			opts: SearchOpts{
				Pattern:      "func()",
				FixedStrings: true,
			},
			jsonOutput: true,
			wantArgs:   []string{"--fixed-strings"},
		},
		{
			name: "word boundary",
			opts: SearchOpts{
				Pattern:      "test",
				WordBoundary: true,
			},
			jsonOutput: true,
			wantArgs:   []string{"--word-regexp"},
		},
		{
			name: "context lines",
			opts: SearchOpts{
				Pattern:      "test",
				ContextLines: 3,
			},
			jsonOutput: true,
			wantArgs:   []string{"--context", "3"},
		},
		{
			name: "max matches per file",
			opts: SearchOpts{
				Pattern:           "test",
				MaxMatchesPerFile: 5,
			},
			jsonOutput: true,
			wantArgs:   []string{"--max-count", "5"},
		},
		{
			name: "hidden files",
			opts: SearchOpts{
				Pattern: "test",
				Hidden:  true,
			},
			jsonOutput: true,
			wantArgs:   []string{"--hidden"},
		},
		{
			name: "file types",
			opts: SearchOpts{
				Pattern:   "test",
				FileTypes: []string{"go", "py"},
			},
			jsonOutput: true,
			wantArgs:   []string{"--type", "go", "--type", "py"},
		},
		{
			name: "include globs",
			opts: SearchOpts{
				Pattern:      "test",
				IncludeGlobs: []string{"*.go", "*.ts"},
			},
			jsonOutput: true,
			wantArgs:   []string{"--glob", "*.go", "--glob", "*.ts"},
		},
		{
			name: "exclude globs",
			opts: SearchOpts{
				Pattern:      "test",
				ExcludeGlobs: []string{"vendor", "*.min.js"},
			},
			jsonOutput: true,
			wantArgs:   []string{"--glob", "!vendor", "--glob", "!*.min.js"},
		},
		{
			name: "default excludes when none specified",
			opts: SearchOpts{
				Pattern: "test",
			},
			jsonOutput: true,
			wantArgs:   []string{"!.git", "!node_modules", "!vendor"},
		},
		{
			name: "no default excludes",
			opts: SearchOpts{
				Pattern:           "test",
				NoDefaultExcludes: true,
			},
			jsonOutput: true,
			dontWant:   []string{"!.git", "!node_modules"},
		},
		{
			name: "files with matches mode",
			opts: SearchOpts{
				Pattern: "test",
			},
			jsonOutput: false,
			dontWant:   []string{"--json"},
		},
		{
			name: "with path",
			opts: SearchOpts{
				Pattern: "test",
				Path:    "./src",
			},
			jsonOutput: true,
			wantArgs:   []string{"test", "./src"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildArgs(tt.opts, tt.jsonOutput)
			argsStr := strings.Join(args, " ")

			for _, want := range tt.wantArgs {
				if !strings.Contains(argsStr, want) {
					t.Errorf("buildArgs() missing %q in %v", want, args)
				}
			}

			for _, dontWant := range tt.dontWant {
				if strings.Contains(argsStr, dontWant) {
					t.Errorf("buildArgs() should not contain %q in %v", dontWant, args)
				}
			}
		})
	}
}

func TestParseJSONOutput(t *testing.T) {
	// Mock ripgrep JSON output
	output := `{"type":"begin","data":{"path":{"text":"file.go"}}}
{"type":"match","data":{"path":{"text":"file.go"},"lines":{"text":"func main() {\n"},"line_number":10,"submatches":[{"match":{"text":"main"},"start":5,"end":9}]}}
{"type":"match","data":{"path":{"text":"other.go"},"lines":{"text":"func helper() {\n"},"line_number":20,"submatches":[{"match":{"text":"helper"},"start":5,"end":11}]}}
{"type":"end","data":{"path":{"text":"file.go"},"stats":{"matches":1}}}
`

	result, err := parseJSONOutput([]byte(output), "", 100)
	if err != nil {
		t.Fatalf("parseJSONOutput() error = %v", err)
	}

	if result.MatchCount != 2 {
		t.Errorf("MatchCount = %d, want 2", result.MatchCount)
	}

	if result.FileCount != 2 {
		t.Errorf("FileCount = %d, want 2", result.FileCount)
	}

	if len(result.Matches) != 2 {
		t.Fatalf("len(Matches) = %d, want 2", len(result.Matches))
	}

	// Check first match
	m := result.Matches[0]
	if m.Path != "file.go" {
		t.Errorf("Match[0].Path = %q, want %q", m.Path, "file.go")
	}
	if m.Line != 10 {
		t.Errorf("Match[0].Line = %d, want 10", m.Line)
	}
	if m.Column != 6 { // start=5, 1-based = 6
		t.Errorf("Match[0].Column = %d, want 6", m.Column)
	}
	if !strings.Contains(m.Text, "func main()") {
		t.Errorf("Match[0].Text = %q, should contain 'func main()'", m.Text)
	}
}

func TestParseJSONOutput_Truncation(t *testing.T) {
	// Generate output with many matches
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		sb.WriteString(`{"type":"match","data":{"path":{"text":"file.go"},"lines":{"text":"line\n"},"line_number":` + string(rune('0'+i%10)) + `,"submatches":[]}}`)
		sb.WriteString("\n")
	}

	result, err := parseJSONOutput([]byte(sb.String()), "", 5)
	if err != nil {
		t.Fatalf("parseJSONOutput() error = %v", err)
	}

	if result.MatchCount != 5 {
		t.Errorf("MatchCount = %d, want 5", result.MatchCount)
	}

	if !result.Truncated {
		t.Error("Truncated should be true")
	}
}

func TestParseFilePaths(t *testing.T) {
	output := `file1.go
file2.go
src/file3.go
`

	paths := parseFilePaths([]byte(output), "")
	if len(paths) != 3 {
		t.Fatalf("len(paths) = %d, want 3", len(paths))
	}

	expected := []string{"file1.go", "file2.go", "src/file3.go"}
	for i, want := range expected {
		if paths[i] != want {
			t.Errorf("paths[%d] = %q, want %q", i, paths[i], want)
		}
	}
}

func TestRelativeTo(t *testing.T) {
	tests := []struct {
		workspace string
		path      string
		want      string
	}{
		{"", "file.go", "file.go"},
		{"/home/user/project", "/home/user/project/file.go", "file.go"},
		{"/home/user/project", "/home/user/project/src/main.go", "src/main.go"},
		{"/home/user/project", "/other/path/file.go", "../../../other/path/file.go"},
	}

	for _, tt := range tests {
		got := relativeTo(tt.workspace, tt.path)
		// Normalize for comparison (handle different OS path separators)
		got = filepath.ToSlash(got)
		if got != tt.want && !strings.HasSuffix(got, tt.want) {
			t.Errorf("relativeTo(%q, %q) = %q, want %q", tt.workspace, tt.path, got, tt.want)
		}
	}
}

func TestAvailable(t *testing.T) {
	// This test depends on the system having ripgrep installed
	// If not installed, test should still pass (just noting availability)
	available := Available()
	t.Logf("ripgrep available: %v", available)
}

func TestSearchJSON_Integration(t *testing.T) {
	if !Available() {
		t.Skip("ripgrep not installed")
	}

	// Create a temporary directory with test files
	tmpDir := t.TempDir()

	// Create test file
	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

func main() {
	fmt.Println("hello world")
}

func helper() {
	// helper function
}
`
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := context.Background()

	// Test SearchJSON
	result, err := SearchJSON(ctx, SearchOpts{
		Pattern:           "func",
		Path:              tmpDir,
		NoDefaultExcludes: true,
	})
	if err != nil {
		t.Fatalf("SearchJSON: %v", err)
	}

	if result.MatchCount < 2 {
		t.Errorf("MatchCount = %d, want >= 2", result.MatchCount)
	}

	if result.FileCount != 1 {
		t.Errorf("FileCount = %d, want 1", result.FileCount)
	}
}

func TestFilesWithMatches_Integration(t *testing.T) {
	if !Available() {
		t.Skip("ripgrep not installed")
	}

	// Create a temporary directory with test files
	tmpDir := t.TempDir()

	// Create test files
	file1 := filepath.Join(tmpDir, "file1.go")
	file2 := filepath.Join(tmpDir, "file2.go")
	file3 := filepath.Join(tmpDir, "file3.txt")

	_ = os.WriteFile(file1, []byte("func main() {}"), 0o644)
	_ = os.WriteFile(file2, []byte("func helper() {}"), 0o644)
	_ = os.WriteFile(file3, []byte("no match here"), 0o644)

	ctx := context.Background()

	paths, err := FilesWithMatches(ctx, SearchOpts{
		Pattern:           "func",
		Path:              tmpDir,
		NoDefaultExcludes: true,
	})
	if err != nil {
		t.Fatalf("FilesWithMatches: %v", err)
	}

	if len(paths) != 2 {
		t.Errorf("len(paths) = %d, want 2", len(paths))
	}
}

func TestSearchJSON_NoMatches(t *testing.T) {
	if !Available() {
		t.Skip("ripgrep not installed")
	}

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	_ = os.WriteFile(testFile, []byte("nothing here"), 0o644)

	ctx := context.Background()

	result, err := SearchJSON(ctx, SearchOpts{
		Pattern:           "xyznotfound123",
		Path:              tmpDir,
		NoDefaultExcludes: true,
	})
	if err != nil {
		t.Fatalf("SearchJSON: %v", err)
	}

	if result.MatchCount != 0 {
		t.Errorf("MatchCount = %d, want 0", result.MatchCount)
	}
}

func TestSearchJSON_EmptyPattern(t *testing.T) {
	ctx := context.Background()

	_, err := SearchJSON(ctx, SearchOpts{
		Pattern: "",
	})
	if err == nil {
		t.Error("SearchJSON with empty pattern should return error")
	}
}
