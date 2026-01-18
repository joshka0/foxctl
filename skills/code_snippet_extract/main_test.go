package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/codeedit"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/langutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skilltest"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/textutil"
	"github.com/jkatigb/agentctl/internal/domain/policy"
)

// parseInput is a test helper that parses JSON and validates required fields.
// Behavior:
//  1. Parse JSON - return error with "decode input" on failure (including empty input)
//  2. Validate required fields (workspace_id, question, candidates)
//  3. Normalize negative limits to 0
//
// Note: This does NOT apply defaults - those are applied in run().
func parseInput(r io.Reader) (Input, error) {
	// Read all input first to detect empty input
	data, _ := io.ReadAll(r)
	if len(bytes.TrimSpace(data)) == 0 {
		return Input{}, newSkillError(ErrCodeArg, "decode input: EOF")
	}

	in, err := skilltest.ParseInput[Input](bytes.NewReader(data))
	if err != nil {
		return in, newSkillError(ErrCodeArg, fmt.Sprintf("decode input: %v", err))
	}

	// Validate required fields
	if in.WorkspaceID == "" {
		return in, newSkillError(ErrCodeArg, "workspace_id is required")
	}
	if in.Question == "" {
		return in, newSkillError(ErrCodeArg, "question is required")
	}

	// Validate candidates have usable paths (spec §5.4)
	usable := 0
	for _, c := range in.Candidates {
		if c.Path != "" {
			usable++
		}
	}
	if usable == 0 {
		return in, newSkillError(ErrCodeNoCandidates, "no usable candidates (all paths empty)")
	}

	// Normalize negative limits to 0
	if in.Limits.MaxFiles < 0 {
		in.Limits.MaxFiles = 0
	}
	if in.Limits.MaxSnippets < 0 {
		in.Limits.MaxSnippets = 0
	}
	if in.Limits.MaxBytesPerFile < 0 {
		in.Limits.MaxBytesPerFile = 0
	}

	return in, nil
}

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

	ve, ok := err.(*skillerr.Error)
	if !ok {
		t.Fatalf("expected *skillerr.Error, got %T", err)
	}

	if ve.Code != ErrCodeNoCandidates {
		t.Errorf("skillerr.Error.Code = %q, want %q", ve.Code, ErrCodeNoCandidates)
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
			wantCode: ErrCodeCapabilityPolicy,
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
			got := textutil.CountLinesBytes(tt.content)
			if got != tt.want {
				t.Errorf("CountLinesBytes(%q) = %d, want %d", tt.content, got, tt.want)
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
		got := textutil.FindLastNewline(tt.s)
		if got != tt.want {
			t.Errorf("FindLastNewline(%q) = %d, want %d", tt.s, got, tt.want)
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
			got := textutil.SplitLinesBytes(tt.content)
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

// TestParseSymbolName tests symbol ID parsing.
func TestParseSymbolName(t *testing.T) {
	tests := []struct {
		name     string
		symbolID string
		want     string
	}{
		{"valid symbol ID", "pkg/auth/login.go:Login", "Login"},
		{"nested path", "internal/services/auth/handler.go:HandleAuth", "HandleAuth"},
		{"no colon", "invalid", ""},
		{"trailing colon", "path:", ""},
		{"multiple colons", "path/file.go:Foo:Bar", "Bar"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSymbolName(tt.symbolID)
			if got != tt.want {
				t.Errorf("parseSymbolName(%q) = %q, want %q", tt.symbolID, got, tt.want)
			}
		})
	}
}

// TestDetectLanguage tests language detection from file paths.
func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"script.py", "python"},
		{"app.ts", "typescript"},
		{"app.tsx", "typescript"},
		{"app.js", "javascript"},
		{"app.jsx", "javascript"},
		{"player.gd", "gdscript"},
		{"lib.rs", "rust"},
		{"Main.java", "java"},
		{"main.c", "c"},
		{"main.cpp", "cpp"},
		{"main.hpp", "cpp"},
		{"unknown.xyz", ""},
		{"pkg/auth/login.go", "go"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := langutil.DetectAllowed(tt.path, langutil.SnippetLanguages)
			if got != tt.want {
				t.Errorf("detectLanguage(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// TestFindSymbolDefinition tests symbol definition finding.
func TestFindSymbolDefinition(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		symbol   string
		lang     string
		wantLine int
	}{
		{
			name: "go function",
			lines: []string{
				"package main",
				"",
				"func Login(ctx context.Context) error {",
				"    return nil",
				"}",
			},
			symbol:   "Login",
			lang:     "go",
			wantLine: 2,
		},
		{
			name: "go method",
			lines: []string{
				"package main",
				"",
				"func (s *Service) HandleAuth(r *Request) error {",
				"    return nil",
				"}",
			},
			symbol:   "HandleAuth",
			lang:     "go",
			wantLine: 2,
		},
		{
			name: "python function",
			lines: []string{
				"import os",
				"",
				"def login(username, password):",
				"    return True",
			},
			symbol:   "login",
			lang:     "python",
			wantLine: 2,
		},
		{
			name: "python class",
			lines: []string{
				"class AuthService:",
				"    def __init__(self):",
				"        pass",
			},
			symbol:   "AuthService",
			lang:     "python",
			wantLine: 0,
		},
		{
			name: "not found",
			lines: []string{
				"package main",
				"func Other() {}",
			},
			symbol:   "NotFound",
			lang:     "go",
			wantLine: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findSymbolDefinition(tt.lines, tt.symbol, tt.lang)
			if got != tt.wantLine {
				t.Errorf("findSymbolDefinition() = %d, want %d", got, tt.wantLine)
			}
		})
	}
}

// TestFindBraceEnd tests brace-based block end finding.
func TestFindBraceEnd(t *testing.T) {
	tests := []struct {
		name      string
		lines     []string
		startLine int
		wantLine  int
	}{
		{
			name: "simple function",
			lines: []string{
				"func Foo() {",
				"    return 1",
				"}",
			},
			startLine: 0,
			wantLine:  2,
		},
		{
			name: "nested braces",
			lines: []string{
				"func Foo() {",
				"    if true {",
				"        x := 1",
				"    }",
				"    return 1",
				"}",
			},
			startLine: 0,
			wantLine:  5,
		},
		{
			name: "braces on same line",
			lines: []string{
				"func Foo() { return 1 }",
			},
			startLine: 0,
			wantLine:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := codeedit.FindBraceEnd(tt.lines, tt.startLine)
			if got != tt.wantLine {
				t.Errorf("findBraceEnd() = %d, want %d", got, tt.wantLine)
			}
		})
	}
}

// TestFindIndentationEnd tests indentation-based block end finding.
func TestFindIndentationEnd(t *testing.T) {
	tests := []struct {
		name      string
		lines     []string
		startLine int
		wantLine  int
	}{
		{
			name: "simple python function",
			lines: []string{
				"def foo():",
				"    x = 1",
				"    return x",
				"",
				"def bar():",
			},
			startLine: 0,
			wantLine:  2,
		},
		{
			name: "nested blocks",
			lines: []string{
				"def foo():",
				"    if True:",
				"        x = 1",
				"    return x",
				"def bar():",
			},
			startLine: 0,
			wantLine:  3,
		},
		{
			name: "with blank lines",
			lines: []string{
				"def foo():",
				"    x = 1",
				"",
				"    return x",
				"",
				"def bar():",
			},
			startLine: 0,
			wantLine:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findIndentationEnd(tt.lines, tt.startLine)
			if got != tt.wantLine {
				t.Errorf("findIndentationEnd() = %d, want %d", got, tt.wantLine)
			}
		})
	}
}

// TestCountLeadingWhitespace tests whitespace counting.
func TestCountLeadingWhitespace(t *testing.T) {
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
			got := countLeadingWhitespace(tt.line)
			if got != tt.want {
				t.Errorf("countLeadingWhitespace(%q) = %d, want %d", tt.line, got, tt.want)
			}
		})
	}
}

// TestExtractSymbolBody tests complete symbol extraction.
func TestExtractSymbolBody(t *testing.T) {
	tests := []struct {
		name    string
		fr      FileResult
		lines   []string
		want    *Snippet
		wantNil bool
	}{
		{
			name: "go function extraction",
			fr: FileResult{
				Path:     "main.go",
				SymbolID: "main.go:Login",
				Priority: 0.9,
			},
			lines: []string{
				"package main",
				"",
				"func Login(ctx context.Context) error {",
				"    return nil",
				"}",
				"",
				"func Other() {}",
			},
			want: &Snippet{
				File:      "main.go",
				SymbolID:  "main.go:Login",
				StartLine: 3, // 1-indexed
				EndLine:   5, // 1-indexed
				Priority:  0.9,
			},
		},
		{
			name: "python function extraction",
			fr: FileResult{
				Path:     "main.py",
				SymbolID: "main.py:process",
				Priority: 0.8,
			},
			lines: []string{
				"def process(data):",
				"    x = data + 1",
				"    return x",
				"",
				"def other():",
				"    pass",
			},
			want: &Snippet{
				File:      "main.py",
				SymbolID:  "main.py:process",
				StartLine: 1,
				EndLine:   3,
				Priority:  0.8,
			},
		},
		{
			name: "symbol not found",
			fr: FileResult{
				Path:     "main.go",
				SymbolID: "main.go:NotFound",
			},
			lines: []string{
				"func Other() {}",
			},
			wantNil: true,
		},
		{
			name: "empty symbol ID",
			fr: FileResult{
				Path:     "main.go",
				SymbolID: "",
			},
			lines: []string{
				"func Foo() {}",
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSymbolBody(tt.fr, tt.lines)
			if tt.wantNil {
				if got != nil {
					t.Errorf("extractSymbolBody() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("extractSymbolBody() = nil, want non-nil")
			}
			if got.File != tt.want.File {
				t.Errorf("File = %q, want %q", got.File, tt.want.File)
			}
			if got.SymbolID != tt.want.SymbolID {
				t.Errorf("SymbolID = %q, want %q", got.SymbolID, tt.want.SymbolID)
			}
			if got.StartLine != tt.want.StartLine {
				t.Errorf("StartLine = %d, want %d", got.StartLine, tt.want.StartLine)
			}
			if got.EndLine != tt.want.EndLine {
				t.Errorf("EndLine = %d, want %d", got.EndLine, tt.want.EndLine)
			}
			if got.Priority != tt.want.Priority {
				t.Errorf("Priority = %f, want %f", got.Priority, tt.want.Priority)
			}
		})
	}
}

// TestExtractSnippetsPriorityOrdering tests that snippets are sorted by priority.
func TestExtractSnippetsPriorityOrdering(t *testing.T) {
	// Create file results with different priorities
	results := []FileResult{
		{
			Path:     "low.go",
			Content:  []byte("func Low() { return }"),
			Priority: 0.3,
		},
		{
			Path:     "high.go",
			Content:  []byte("func High() { return }"),
			Priority: 0.9,
		},
		{
			Path:     "mid.go",
			Content:  []byte("func Mid() { return }"),
			Priority: 0.6,
		},
	}

	snippets := extractSnippets(results, "func", 10)

	if len(snippets) != 3 {
		t.Fatalf("expected 3 snippets, got %d", len(snippets))
	}

	// Check snippets are sorted by priority (highest first)
	if snippets[0].Priority != 0.9 {
		t.Errorf("first snippet priority = %f, want 0.9", snippets[0].Priority)
	}
	if snippets[1].Priority != 0.6 {
		t.Errorf("second snippet priority = %f, want 0.6", snippets[1].Priority)
	}
	if snippets[2].Priority != 0.3 {
		t.Errorf("third snippet priority = %f, want 0.3", snippets[2].Priority)
	}
}

// TestExtractGoSymbolBody tests AST-based Go symbol extraction.
func TestExtractGoSymbolBody(t *testing.T) {
	tests := []struct {
		name       string
		fr         FileResult
		symbolName string
		wantStart  int
		wantEnd    int
		wantNil    bool
	}{
		{
			name: "function extraction",
			fr: FileResult{
				Path:     "test.go",
				SymbolID: "test.go:Hello",
				Priority: 0.9,
				Content: []byte(`package main

func Hello() string {
	return "hello"
}

func Other() {}
`),
			},
			symbolName: "Hello",
			wantStart:  3,
			wantEnd:    5,
		},
		{
			name: "method extraction",
			fr: FileResult{
				Path:     "service.go",
				SymbolID: "service.go:Process",
				Priority: 0.8,
				Content: []byte(`package main

type Service struct{}

func (s *Service) Process(data string) error {
	if data == "" {
		return nil
	}
	return nil
}

func (s *Service) Other() {}
`),
			},
			symbolName: "Process",
			wantStart:  5,
			wantEnd:    10,
		},
		{
			name: "method matched by full name",
			fr: FileResult{
				Path:     "service.go",
				SymbolID: "service.go:Service.Process",
				Priority: 0.7,
				Content: []byte(`package main

type Service struct{}

func (s *Service) Process(data string) error {
	return nil
}
`),
			},
			symbolName: "Service.Process",
			wantStart:  5,
			wantEnd:    7,
		},
		{
			name: "struct extraction",
			fr: FileResult{
				Path:     "types.go",
				SymbolID: "types.go:Config",
				Priority: 0.6,
				Content: []byte(`package main

type Config struct {
	Name    string
	Timeout int
}
`),
			},
			symbolName: "Config",
			wantStart:  3,
			wantEnd:    6,
		},
		{
			name: "symbol not found",
			fr: FileResult{
				Path:     "test.go",
				SymbolID: "test.go:NotFound",
				Content:  []byte(`package main\n\nfunc Other() {}`),
			},
			symbolName: "NotFound",
			wantNil:    true,
		},
		{
			name: "invalid go syntax",
			fr: FileResult{
				Path:     "bad.go",
				SymbolID: "bad.go:Foo",
				Content:  []byte(`this is not valid go code {{{`),
			},
			symbolName: "Foo",
			wantNil:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := textutil.SplitLinesBytes(tt.fr.Content)
			got := extractGoSymbolBody(tt.fr, lines, tt.symbolName)

			if tt.wantNil {
				if got != nil {
					t.Errorf("extractGoSymbolBody() = %+v, want nil", got)
				}
				return
			}

			if got == nil {
				t.Fatal("extractGoSymbolBody() = nil, want non-nil")
			}
			if got.StartLine != tt.wantStart {
				t.Errorf("StartLine = %d, want %d", got.StartLine, tt.wantStart)
			}
			if got.EndLine != tt.wantEnd {
				t.Errorf("EndLine = %d, want %d", got.EndLine, tt.wantEnd)
			}
			if got.File != tt.fr.Path {
				t.Errorf("File = %q, want %q", got.File, tt.fr.Path)
			}
			if got.Priority != tt.fr.Priority {
				t.Errorf("Priority = %f, want %f", got.Priority, tt.fr.Priority)
			}
		})
	}
}

// TestExtractSymbolBodyMultiLanguage tests heuristic-based extraction for non-Go languages.
func TestExtractSymbolBodyMultiLanguage(t *testing.T) {
	tests := []struct {
		name      string
		fr        FileResult
		wantStart int
		wantEnd   int
		wantNil   bool
	}{
		{
			name: "typescript function",
			fr: FileResult{
				Path:     "utils.ts",
				SymbolID: "utils.ts:fetchData",
				Priority: 0.9,
				Content: []byte(`import axios from 'axios';

function fetchData(url: string): Promise<any> {
    return axios.get(url)
        .then(res => res.data);
}

export const other = () => {};
`),
			},
			wantStart: 3,
			wantEnd:   6, // Line 6 is the closing brace
		},
		{
			name: "typescript arrow function",
			fr: FileResult{
				Path:     "app.ts",
				SymbolID: "app.ts:handleClick",
				Priority: 0.8,
				Content: []byte(`const handleClick = (e: Event) => {
    console.log(e);
    return true;
};
`),
			},
			wantStart: 1,
			wantEnd:   4,
		},
		{
			name: "javascript class",
			fr: FileResult{
				Path:     "service.js",
				SymbolID: "service.js:UserService",
				Priority: 0.7,
				Content: []byte(`class UserService {
    constructor() {
        this.users = [];
    }

    getUser(id) {
        return this.users.find(u => u.id === id);
    }
}
`),
			},
			wantStart: 1,
			wantEnd:   9,
		},
		{
			name: "python with decorator",
			fr: FileResult{
				Path:     "views.py",
				SymbolID: "views.py:login",
				Priority: 0.6,
				Content: []byte(`from flask import request

@app.route('/login', methods=['POST'])
def login():
    data = request.json
    return {'status': 'ok'}

def logout():
    pass
`),
			},
			wantStart: 4,
			wantEnd:   6,
		},
		{
			name: "python class",
			fr: FileResult{
				Path:     "models.py",
				SymbolID: "models.py:User",
				Priority: 0.5,
				Content: []byte(`class User:
    def __init__(self, name):
        self.name = name

    def greet(self):
        return f"Hello, {self.name}"

class Other:
    pass
`),
			},
			wantStart: 1,
			wantEnd:   6,
		},
		{
			name: "rust function",
			fr: FileResult{
				Path:     "lib.rs",
				SymbolID: "lib.rs:process",
				Priority: 0.9,
				Content: []byte(`use std::io;

fn process(data: &str) -> Result<(), io::Error> {
    println!("{}", data);
    Ok(())
}

fn other() {}
`),
			},
			wantStart: 3,
			wantEnd:   6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := textutil.SplitLinesBytes(tt.fr.Content)
			got := extractSymbolBody(tt.fr, lines)

			if tt.wantNil {
				if got != nil {
					t.Errorf("extractSymbolBody() = %+v, want nil", got)
				}
				return
			}

			if got == nil {
				t.Fatal("extractSymbolBody() = nil, want non-nil")
			}
			if got.StartLine != tt.wantStart {
				t.Errorf("StartLine = %d, want %d", got.StartLine, tt.wantStart)
			}
			if got.EndLine != tt.wantEnd {
				t.Errorf("EndLine = %d, want %d", got.EndLine, tt.wantEnd)
			}
		})
	}
}
