package main

import (
	"bytes"
	"context"
	"encoding/json"
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
	if err := os.WriteFile(smallPath, smallContent, 0644); err != nil {
		t.Fatalf("failed to create small file: %v", err)
	}

	// Create a large file (100 bytes)
	largeContent := bytes.Repeat([]byte("x"), 100)
	largePath := filepath.Join(tmpDir, "large.txt")
	if err := os.WriteFile(largePath, largeContent, 0644); err != nil {
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
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Test with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, _, err := readFileWithLimit(ctx, testFile, 1024)
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
	if err != context.Canceled {
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
		{Path: "a.go", Content: []byte("package a\n"), Priority: 0.9},
		{Path: "b.go", Content: []byte("package b\n"), Priority: 0.8},
		{Path: "skipped.go", Skipped: true, Content: nil},
		{Path: "empty.go", Content: []byte{}},
	}

	snippets := extractSnippets(results, 10)

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
		{Path: "a.go", Content: []byte("a")},
		{Path: "b.go", Content: []byte("b")},
		{Path: "c.go", Content: []byte("c")},
	}

	snippets := extractSnippets(results, 2)

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
