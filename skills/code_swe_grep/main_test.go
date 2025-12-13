package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/policy"
)

func TestParseInput(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantErr     bool
		errContains string
		check       func(t *testing.T, in Input)
	}{
		{
			name: "minimal valid input",
			input: `{
				"workspace_id": "ws-123",
				"question": "How does auth work?",
				"candidates": [{"path": "auth.go"}]
			}`,
			check: func(t *testing.T, in Input) {
				if in.WorkspaceID != "ws-123" {
					t.Errorf("WorkspaceID = %q, want ws-123", in.WorkspaceID)
				}
				if in.Question != "How does auth work?" {
					t.Errorf("Question = %q, want 'How does auth work?'", in.Question)
				}
				if len(in.Candidates) != 1 {
					t.Errorf("Candidates len = %d, want 1", len(in.Candidates))
				}
				if in.Candidates[0].Path != "auth.go" {
					t.Errorf("Candidates[0].Path = %q, want auth.go", in.Candidates[0].Path)
				}
			},
		},
		{
			name: "full input with limits and symbol_id",
			input: `{
				"workspace_id": "ws-456",
				"question": "Find login handlers",
				"candidates": [
					{"path": "internal/auth/login.go", "symbol_id": "internal/auth/login.go:Login", "priority": 0.95},
					{"path": "internal/auth/session.go", "priority": 0.8}
				],
				"limits": {
					"max_files": 10,
					"max_snippets": 20,
					"max_bytes_per_file": 65536
				}
			}`,
			check: func(t *testing.T, in Input) {
				if in.WorkspaceID != "ws-456" {
					t.Errorf("WorkspaceID = %q, want ws-456", in.WorkspaceID)
				}
				if len(in.Candidates) != 2 {
					t.Errorf("Candidates len = %d, want 2", len(in.Candidates))
				}
				if in.Candidates[0].SymbolID != "internal/auth/login.go:Login" {
					t.Errorf("Candidates[0].SymbolID = %q, want internal/auth/login.go:Login", in.Candidates[0].SymbolID)
				}
				if in.Candidates[0].Priority != 0.95 {
					t.Errorf("Candidates[0].Priority = %f, want 0.95", in.Candidates[0].Priority)
				}
				if in.Limits.MaxFiles != 10 {
					t.Errorf("Limits.MaxFiles = %d, want 10", in.Limits.MaxFiles)
				}
				if in.Limits.MaxSnippets != 20 {
					t.Errorf("Limits.MaxSnippets = %d, want 20", in.Limits.MaxSnippets)
				}
				if in.Limits.MaxBytesPerFile != 65536 {
					t.Errorf("Limits.MaxBytesPerFile = %d, want 65536", in.Limits.MaxBytesPerFile)
				}
			},
		},
		{
			name: "negative limits normalized to zero",
			input: `{
				"workspace_id": "ws-789",
				"question": "test",
				"candidates": [{"path": "a.go"}],
				"limits": {"max_files": -1, "max_snippets": -5}
			}`,
			check: func(t *testing.T, in Input) {
				if in.Limits.MaxFiles != 0 {
					t.Errorf("Limits.MaxFiles = %d, want 0 (normalized from -1)", in.Limits.MaxFiles)
				}
				if in.Limits.MaxSnippets != 0 {
					t.Errorf("Limits.MaxSnippets = %d, want 0 (normalized from -5)", in.Limits.MaxSnippets)
				}
			},
		},
		{
			name:        "missing workspace_id",
			input:       `{"question": "test", "candidates": [{"path": "a.go"}]}`,
			wantErr:     true,
			errContains: "workspace_id is required",
		},
		{
			name:        "missing question",
			input:       `{"workspace_id": "ws", "candidates": [{"path": "a.go"}]}`,
			wantErr:     true,
			errContains: "question is required",
		},
		{
			name:        "empty candidates array",
			input:       `{"workspace_id": "ws", "question": "test", "candidates": []}`,
			wantErr:     true,
			errContains: "no usable candidates",
		},
		{
			name:        "candidates with empty paths only",
			input:       `{"workspace_id": "ws", "question": "test", "candidates": [{"path": ""}, {"path": ""}]}`,
			wantErr:     true,
			errContains: "no usable candidates",
		},
		{
			name:        "invalid json",
			input:       `{not valid json}`,
			wantErr:     true,
			errContains: "decode input",
		},
		{
			name:        "empty input",
			input:       ``,
			wantErr:     true,
			errContains: "decode input",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, err := parseInput(bytes.NewReader([]byte(tt.input)))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
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

func TestCandidateJSON(t *testing.T) {
	// Verify Candidate struct marshals/unmarshals correctly
	input := `{
		"workspace_id": "ws",
		"question": "test",
		"candidates": [
			{"path": "foo.go", "symbol_id": "foo.go:Bar", "priority": 0.5},
			{"path": "baz.go"}
		]
	}`

	in, err := parseInput(bytes.NewReader([]byte(input)))
	if err != nil {
		t.Fatalf("parseInput failed: %v", err)
	}

	if len(in.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(in.Candidates))
	}

	c1 := in.Candidates[0]
	if c1.Path != "foo.go" {
		t.Errorf("c1.Path = %q, want foo.go", c1.Path)
	}
	if c1.SymbolID != "foo.go:Bar" {
		t.Errorf("c1.SymbolID = %q, want foo.go:Bar", c1.SymbolID)
	}
	if c1.Priority != 0.5 {
		t.Errorf("c1.Priority = %f, want 0.5", c1.Priority)
	}

	c2 := in.Candidates[1]
	if c2.Path != "baz.go" {
		t.Errorf("c2.Path = %q, want baz.go", c2.Path)
	}
	if c2.SymbolID != "" {
		t.Errorf("c2.SymbolID = %q, want empty", c2.SymbolID)
	}
	if c2.Priority != 0 {
		t.Errorf("c2.Priority = %f, want 0", c2.Priority)
	}
}

func TestInputDefaults(t *testing.T) {
	// Limits default to zero (unset) when not provided
	input := `{
		"workspace_id": "ws",
		"question": "test",
		"candidates": [{"path": "a.go"}]
	}`

	in, err := parseInput(bytes.NewReader([]byte(input)))
	if err != nil {
		t.Fatalf("parseInput failed: %v", err)
	}

	if in.Limits.MaxFiles != 0 {
		t.Errorf("Limits.MaxFiles = %d, want 0 (default unset)", in.Limits.MaxFiles)
	}
	if in.Limits.MaxSnippets != 0 {
		t.Errorf("Limits.MaxSnippets = %d, want 0 (default unset)", in.Limits.MaxSnippets)
	}
	if in.Limits.MaxBytesPerFile != 0 {
		t.Errorf("Limits.MaxBytesPerFile = %d, want 0 (default unset)", in.Limits.MaxBytesPerFile)
	}
}

func TestValidationErrorCode(t *testing.T) {
	// Verify that no-candidates error returns EARG with ValidationError
	input := `{"workspace_id": "ws", "question": "test", "candidates": []}`

	_, err := parseInput(bytes.NewReader([]byte(input)))
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}

	if ve.Code != ErrCodeArg {
		t.Errorf("ValidationError.Code = %q, want %q", ve.Code, ErrCodeArg)
	}
}

func TestApplyDefaultLimits(t *testing.T) {
	tests := []struct {
		name     string
		input    Limits
		expected Limits
	}{
		{
			name:  "all zeros get defaults",
			input: Limits{},
			expected: Limits{
				MaxFiles:        DefaultMaxFiles,
				MaxSnippets:     DefaultMaxSnippets,
				MaxBytesPerFile: DefaultMaxBytesPerFile,
			},
		},
		{
			name:  "custom values preserved",
			input: Limits{MaxFiles: 5, MaxSnippets: 10, MaxBytesPerFile: 1024},
			expected: Limits{
				MaxFiles:        5,
				MaxSnippets:     10,
				MaxBytesPerFile: 1024,
			},
		},
		{
			name:  "partial custom values",
			input: Limits{MaxFiles: 5},
			expected: Limits{
				MaxFiles:        5,
				MaxSnippets:     DefaultMaxSnippets,
				MaxBytesPerFile: DefaultMaxBytesPerFile,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyDefaultLimits(tt.input)
			if got != tt.expected {
				t.Errorf("applyDefaultLimits(%+v) = %+v, want %+v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestClassifyPathError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantMsg  string
		wantCode string
	}{
		{
			name:     "path escape",
			err:      policy.ErrPathEscape,
			wantMsg:  "path escapes workspace",
			wantCode: ErrCodePolicy,
		},
		{
			name:     "symlink escape",
			err:      policy.ErrSymlinkEscape,
			wantMsg:  "symlink escapes workspace",
			wantCode: ErrCodePolicy,
		},
		{
			name:     "invalid path",
			err:      policy.ErrInvalidPath,
			wantMsg:  "invalid path",
			wantCode: ErrCodeArg,
		},
		{
			name:     "null byte",
			err:      policy.ErrNullByte,
			wantMsg:  "path contains null byte",
			wantCode: ErrCodeArg,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMsg, gotCode := classifyPathError(tt.err)
			if gotMsg != tt.wantMsg {
				t.Errorf("classifyPathError(%v) msg = %q, want %q", tt.err, gotMsg, tt.wantMsg)
			}
			if gotCode != tt.wantCode {
				t.Errorf("classifyPathError(%v) code = %q, want %q", tt.err, gotCode, tt.wantCode)
			}
		})
	}
}

func TestClassifyFileError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantMsg  string
		wantCode string
	}{
		{
			name:     "file not found",
			err:      os.ErrNotExist,
			wantMsg:  "file not found",
			wantCode: ErrCodeNotFound,
		},
		{
			name:     "permission denied",
			err:      os.ErrPermission,
			wantMsg:  "permission denied",
			wantCode: ErrCodePolicy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMsg, gotCode := classifyFileError(tt.err)
			if gotMsg != tt.wantMsg {
				t.Errorf("classifyFileError(%v) msg = %q, want %q", tt.err, gotMsg, tt.wantMsg)
			}
			if gotCode != tt.wantCode {
				t.Errorf("classifyFileError(%v) code = %q, want %q", tt.err, gotCode, tt.wantCode)
			}
		})
	}
}

func TestReadFileWithLimit(t *testing.T) {
	// Create temp directory with test files
	tmpDir := t.TempDir()

	// Create a small file
	smallContent := []byte("hello world")
	smallPath := filepath.Join(tmpDir, "small.txt")
	if err := os.WriteFile(smallPath, smallContent, 0o644); err != nil {
		t.Fatalf("failed to create small file: %v", err)
	}

	// Create a large file (100 bytes)
	largeContent := bytes.Repeat([]byte("x"), 100)
	largePath := filepath.Join(tmpDir, "large.txt")
	if err := os.WriteFile(largePath, largeContent, 0o644); err != nil {
		t.Fatalf("failed to create large file: %v", err)
	}

	tests := []struct {
		name          string
		path          string
		maxBytes      int
		wantLen       int
		wantTruncated bool
		wantErr       bool
	}{
		{
			name:          "small file within limit",
			path:          smallPath,
			maxBytes:      1024,
			wantLen:       len(smallContent),
			wantTruncated: false,
		},
		{
			name:          "large file truncated",
			path:          largePath,
			maxBytes:      50,
			wantLen:       50,
			wantTruncated: true,
		},
		{
			name:          "file exactly at limit",
			path:          smallPath,
			maxBytes:      len(smallContent),
			wantLen:       len(smallContent),
			wantTruncated: false,
		},
		{
			name:     "nonexistent file",
			path:     filepath.Join(tmpDir, "nonexistent.txt"),
			maxBytes: 1024,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			content, truncated, err := readFileWithLimit(ctx, tt.path, tt.maxBytes)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(content) != tt.wantLen {
				t.Errorf("content length = %d, want %d", len(content), tt.wantLen)
			}
			if truncated != tt.wantTruncated {
				t.Errorf("truncated = %v, want %v", truncated, tt.wantTruncated)
			}
		})
	}
}

func TestReadFileWithLimitCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Test with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, _, err := readFileWithLimit(ctx, testFile, 1024)
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestFileResult(t *testing.T) {
	// Verify FileResult struct works as expected
	fr := FileResult{
		Path:      "test.go",
		AbsPath:   "/workspace/test.go",
		SymbolID:  "test.go:Main",
		Priority:  0.9,
		Content:   []byte("package main"),
		Truncated: false,
		Skipped:   false,
	}

	if fr.Path != "test.go" {
		t.Errorf("Path = %q, want test.go", fr.Path)
	}
	if fr.Skipped {
		t.Error("Skipped = true, want false")
	}
	if len(fr.Content) == 0 {
		t.Error("Content should not be empty")
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		want    int
	}{
		{"empty", []byte{}, 0},
		{"single line no newline", []byte("hello"), 1},
		{"single line with newline", []byte("hello\n"), 1},
		{"two lines", []byte("line1\nline2\n"), 2},
		{"three lines no trailing newline", []byte("a\nb\nc"), 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countLines(tt.content)
			if got != tt.want {
				t.Errorf("countLines(%q) = %d, want %d", tt.content, got, tt.want)
			}
		})
	}
}

func TestFindLastNewline(t *testing.T) {
	tests := []struct {
		s    string
		want int
	}{
		{"", -1},
		{"no newlines", -1},
		{"has\nnewline", 3},
		{"multiple\nnew\nlines", 12},
		{"\n", 0},
	}

	for _, tt := range tests {
		got := findLastNewline(tt.s)
		if got != tt.want {
			t.Errorf("findLastNewline(%q) = %d, want %d", tt.s, got, tt.want)
		}
	}
}

func TestExtractSnippets(t *testing.T) {
	results := []FileResult{
		{Path: "a.go", Content: []byte("package a\nfunc Login() {}\n"), Priority: 0.9},
		{Path: "b.go", Content: []byte("package b\nfunc Login() {}\n"), Priority: 0.8},
		{Path: "skipped.go", Skipped: true, Content: nil},
		{Path: "empty.go", Content: []byte{}},
	}

	// Question with "login" keyword should match both files
	snippets := extractSnippets(results, "How does login work?", 10)

	if len(snippets) != 2 {
		t.Fatalf("extractSnippets returned %d snippets, want 2", len(snippets))
	}

	if snippets[0].File != "a.go" {
		t.Errorf("snippets[0].File = %q, want a.go", snippets[0].File)
	}
	if snippets[0].Priority != 0.9 {
		t.Errorf("snippets[0].Priority = %f, want 0.9", snippets[0].Priority)
	}
	if snippets[1].File != "b.go" {
		t.Errorf("snippets[1].File = %q, want b.go", snippets[1].File)
	}
}

func TestExtractSnippetsLimit(t *testing.T) {
	results := []FileResult{
		{Path: "a.go", Content: []byte("func Login()\n")},
		{Path: "b.go", Content: []byte("func Login()\n")},
		{Path: "c.go", Content: []byte("func Login()\n")},
	}

	// Limit to 2 snippets
	snippets := extractSnippets(results, "login", 2)

	if len(snippets) != 2 {
		t.Errorf("extractSnippets with limit 2 returned %d snippets, want 2", len(snippets))
	}
}

func TestMakeInlinePreviews(t *testing.T) {
	snippets := []Snippet{
		{File: "a.go", StartLine: 1, EndLine: 5, Text: "short text", Priority: 0.9},
	}

	previews := makeInlinePreviews(snippets)

	if len(previews) != 1 {
		t.Fatalf("makeInlinePreviews returned %d previews, want 1", len(previews))
	}

	if previews[0].File != "a.go" {
		t.Errorf("previews[0].File = %q, want a.go", previews[0].File)
	}
	if previews[0].Preview != "short text" {
		t.Errorf("previews[0].Preview = %q, want 'short text'", previews[0].Preview)
	}
}

func TestMakeInlinePreviewsTruncation(t *testing.T) {
	// Create a snippet with text longer than MaxPreviewBytes
	longText := strings.Repeat("x", MaxPreviewBytes+100)
	snippets := []Snippet{
		{File: "long.go", StartLine: 1, EndLine: 1, Text: longText},
	}

	previews := makeInlinePreviews(snippets)

	if len(previews) != 1 {
		t.Fatalf("makeInlinePreviews returned %d previews, want 1", len(previews))
	}

	// Should be truncated and end with "..."
	if len(previews[0].Preview) > MaxPreviewBytes+4 { // +4 for "..."
		t.Errorf("preview length = %d, should be <= %d", len(previews[0].Preview), MaxPreviewBytes+4)
	}
	if !strings.HasSuffix(previews[0].Preview, "...") {
		t.Errorf("truncated preview should end with '...', got %q", previews[0].Preview[len(previews[0].Preview)-10:])
	}
}

func TestSnippetJSON(t *testing.T) {
	s := Snippet{
		File:      "test.go",
		SymbolID:  "test.go:Main",
		StartLine: 1,
		EndLine:   10,
		Text:      "package main",
		Priority:  0.95,
	}

	// Verify it marshals correctly
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded Snippet
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if decoded.File != s.File {
		t.Errorf("decoded.File = %q, want %q", decoded.File, s.File)
	}
	if decoded.SymbolID != s.SymbolID {
		t.Errorf("decoded.SymbolID = %q, want %q", decoded.SymbolID, s.SymbolID)
	}
}

// --- PR5: Snippet Extraction Engine Tests ---

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		name     string
		question string
		want     []string
	}{
		{
			name:     "simple question",
			question: "How does login work?",
			want:     []string{"login", "work"},
		},
		{
			name:     "question with stop words",
			question: "What is the authentication handler?",
			want:     []string{"authentication", "handler"},
		},
		{
			name:     "question with short words filtered",
			question: "Is it OK to do X?",
			want:     []string{}, // all words are short or stop words
		},
		{
			name:     "question with duplicates",
			question: "login and login again",
			want:     []string{"login", "again"},
		},
		{
			name:     "question with punctuation",
			question: "Where is user.password validated?",
			want:     []string{"user", "password", "validated"},
		},
		{
			name:     "empty question",
			question: "",
			want:     []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractKeywords(tt.question)
			if len(got) != len(tt.want) {
				t.Errorf("extractKeywords(%q) = %v, want %v", tt.question, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractKeywords(%q)[%d] = %q, want %q", tt.question, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		want    []string
	}{
		{"empty", []byte{}, nil},
		{"single line no newline", []byte("hello"), []string{"hello"}},
		{"single line with newline", []byte("hello\n"), []string{"hello"}},
		{"two lines", []byte("line1\nline2\n"), []string{"line1", "line2"}},
		{"three lines no trailing newline", []byte("a\nb\nc"), []string{"a", "b", "c"}},
		{"empty lines preserved", []byte("a\n\nb\n"), []string{"a", "", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitLines(tt.content)
			if len(got) != len(tt.want) {
				t.Errorf("splitLines(%q) = %v (len %d), want %v (len %d)", tt.content, got, len(got), tt.want, len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitLines(%q)[%d] = %q, want %q", tt.content, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFindMatchingLines(t *testing.T) {
	lines := []string{
		"package main",     // 0
		"",                 // 1
		"func Login() {",   // 2
		"    // do login",  // 3
		"}",                // 4
		"",                 // 5
		"func Logout() {",  // 6
		"    // do logout", // 7
		"}",                // 8
	}

	tests := []struct {
		name     string
		keywords []string
		want     []int
	}{
		{"single keyword match", []string{"login"}, []int{2, 3}},
		{"multiple keywords", []string{"login", "logout"}, []int{2, 3, 6, 7}},
		{"no match", []string{"register"}, nil},
		{"empty keywords", []string{}, nil},
		// Note: keywords are expected to be lowercase (extractKeywords lowercases them)
		// findMatchingLines lowercases lines for comparison, so this tests that flow
		{"partial match", []string{"log"}, []int{2, 3, 6, 7}}, // "log" matches "Login" and "Logout"
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findMatchingLines(lines, tt.keywords)
			if len(got) != len(tt.want) {
				t.Errorf("findMatchingLines() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("findMatchingLines()[%d] = %d, want %d", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestGroupIntoBlocks(t *testing.T) {
	tests := []struct {
		name          string
		matchingLines []int
		totalLines    int
		want          []lineBlock
	}{
		{
			name:          "single match with context",
			matchingLines: []int{5},
			totalLines:    10,
			want:          []lineBlock{{start: 2, end: 8}}, // 5-3 to 5+3
		},
		{
			name:          "adjacent matches merged",
			matchingLines: []int{5, 6},
			totalLines:    20,
			want:          []lineBlock{{start: 2, end: 9}}, // merged
		},
		{
			name:          "separate matches",
			matchingLines: []int{2, 15},
			totalLines:    20,
			want:          []lineBlock{{start: 0, end: 5}, {start: 12, end: 18}},
		},
		{
			name:          "match at start",
			matchingLines: []int{0},
			totalLines:    10,
			want:          []lineBlock{{start: 0, end: 3}},
		},
		{
			name:          "match at end",
			matchingLines: []int{9},
			totalLines:    10,
			want:          []lineBlock{{start: 6, end: 9}},
		},
		{
			name:          "empty matches",
			matchingLines: []int{},
			totalLines:    10,
			want:          nil,
		},
		{
			name:          "capped block forces new block",
			matchingLines: generateSequence(0, 100), // 100 consecutive matches
			totalLines:    200,
			// Match 77 causes first block to exceed 80 lines, so it caps at {0, 79}
			// Match 78 starts fresh block at {75, 81}, which continues to {75, 102}
			want: []lineBlock{{start: 0, end: 79}, {start: 75, end: 102}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := groupIntoBlocks(tt.matchingLines, tt.totalLines)
			if len(got) != len(tt.want) {
				t.Errorf("groupIntoBlocks() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i].start != tt.want[i].start || got[i].end != tt.want[i].end {
					t.Errorf("groupIntoBlocks()[%d] = {%d,%d}, want {%d,%d}",
						i, got[i].start, got[i].end, tt.want[i].start, tt.want[i].end)
				}
			}
		})
	}
}

func TestJoinLines(t *testing.T) {
	lines := []string{"line0", "line1", "line2", "line3", "line4"}

	tests := []struct {
		name  string
		start int
		end   int
		want  string
	}{
		{"single line", 0, 0, "line0"},
		{"multiple lines", 1, 3, "line1\nline2\nline3"},
		{"negative start clamped", -1, 1, "line0\nline1"},
		{"end beyond length clamped", 3, 10, "line3\nline4"},
		{"start > end empty", 3, 1, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinLines(lines, tt.start, tt.end)
			if got != tt.want {
				t.Errorf("joinLines(%d, %d) = %q, want %q", tt.start, tt.end, got, tt.want)
			}
		})
	}
}

func TestExtractSnippetsForFile(t *testing.T) {
	content := []byte(`package handlers

import "net/http"

// Login handles user authentication.
func Login(w http.ResponseWriter, r *http.Request) {
    username := r.FormValue("username")
    password := r.FormValue("password")
    // validate credentials
}

// Logout terminates the session.
func Logout(w http.ResponseWriter, r *http.Request) {
    // destroy session
}
`)

	fr := FileResult{
		Path:     "handlers.go",
		Content:  content,
		Priority: 0.9,
	}

	t.Run("keyword match", func(t *testing.T) {
		keywords := []string{"login"}
		snippets := extractSnippetsForFile(fr, keywords, 10)

		if len(snippets) == 0 {
			t.Fatal("expected at least 1 snippet")
		}

		// Should contain the Login function
		if !strings.Contains(snippets[0].Text, "func Login") {
			t.Errorf("snippet should contain 'func Login', got: %s", snippets[0].Text[:min(100, len(snippets[0].Text))])
		}
	})

	t.Run("no keyword match fallback", func(t *testing.T) {
		keywords := []string{"nonexistent"}
		snippets := extractSnippetsForFile(fr, keywords, 10)

		// Should return fallback snippet from beginning
		if len(snippets) != 1 {
			t.Fatalf("expected 1 fallback snippet, got %d", len(snippets))
		}
		if snippets[0].StartLine != 1 {
			t.Errorf("fallback snippet StartLine = %d, want 1", snippets[0].StartLine)
		}
	})

	t.Run("remaining limit respected", func(t *testing.T) {
		keywords := []string{"login", "logout"}
		snippets := extractSnippetsForFile(fr, keywords, 1)

		if len(snippets) != 1 {
			t.Errorf("expected 1 snippet (limited), got %d", len(snippets))
		}
	})
}

func TestExtractSnippetsIntegration(t *testing.T) {
	// Simulate multiple files with varying relevance
	results := []FileResult{
		{
			Path: "handlers.go",
			Content: []byte(`package handlers

func Login(w http.ResponseWriter, r *http.Request) {
    // login implementation
}

func Logout(w http.ResponseWriter, r *http.Request) {
    // logout implementation
}
`),
			Priority: 0.95,
		},
		{
			Path: "config.go",
			Content: []byte(`package config

func Load() *Config {
    // load config
}
`),
			Priority: 0.5,
		},
	}

	t.Run("question matches first file", func(t *testing.T) {
		snippets := extractSnippets(results, "How does login work?", 10)

		// Should have snippet from handlers.go (login match)
		// and fallback from config.go (no login keyword)
		if len(snippets) < 1 {
			t.Fatal("expected at least 1 snippet")
		}

		// First snippet should be from handlers.go with login content
		if snippets[0].File != "handlers.go" {
			t.Errorf("first snippet should be from handlers.go, got %s", snippets[0].File)
		}
		if !strings.Contains(snippets[0].Text, "Login") {
			t.Errorf("first snippet should contain Login")
		}
	})

	t.Run("files_relevant count", func(t *testing.T) {
		// This would be tested in the run function, but we can verify
		// extractSnippets produces snippets for both files
		snippets := extractSnippets(results, "vague question", 10)

		// Both files should produce fallback snippets
		if len(snippets) != 2 {
			t.Errorf("expected 2 snippets (fallbacks for both), got %d", len(snippets))
		}
	})
}

// generateSequence returns a slice of integers from start to end-1 (exclusive).
func generateSequence(start, end int) []int {
	seq := make([]int, end-start)
	for i := range seq {
		seq[i] = start + i
	}
	return seq
}
