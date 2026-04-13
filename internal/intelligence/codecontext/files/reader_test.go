package files_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/policy"
	"github.com/jkatigb/agentctl/internal/intelligence/codecontext/files"
)

// createTestFile creates a temporary file with the given content.
func createTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	return path
}

// createTestDir creates a temporary directory for tests.
func createTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "codecontext-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestSafeReader_ReadBasicFile(t *testing.T) {
	dir := createTestDir(t)
	content := "line 1\nline 2\nline 3\n"
	filePath := createTestFile(t, dir, "test.go", content)

	validator, err := policy.NewPathValidator(dir, nil)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	reader := files.NewSafeReader(validator, 64*1024)
	ctx := context.Background()

	fc, err := reader.Read(ctx, filePath)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if fc.Path != filePath {
		t.Errorf("Path = %q, want %q", fc.Path, filePath)
	}
	if string(fc.Content) != content {
		t.Errorf("Content = %q, want %q", string(fc.Content), content)
	}
	if fc.Truncated {
		t.Error("Truncated = true, want false")
	}
	if fc.Language != "go" {
		t.Errorf("Language = %q, want 'go'", fc.Language)
	}
	if len(fc.Lines) != 3 {
		t.Errorf("len(Lines) = %d, want 3", len(fc.Lines))
	}
	if fc.Lines[0] != "line 1" {
		t.Errorf("Lines[0] = %q, want 'line 1'", fc.Lines[0])
	}
}

func TestSafeReader_ReadWithTruncation(t *testing.T) {
	dir := createTestDir(t)
	content := strings.Repeat("x", 1000)
	filePath := createTestFile(t, dir, "large.txt", content)

	validator, err := policy.NewPathValidator(dir, nil)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	// Set maxBytes lower than content
	reader := files.NewSafeReader(validator, 100)
	ctx := context.Background()

	fc, err := reader.Read(ctx, filePath)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if !fc.Truncated {
		t.Error("Truncated = false, want true")
	}
	if len(fc.Content) != 100 {
		t.Errorf("len(Content) = %d, want 100", len(fc.Content))
	}
}

func TestSafeReader_RejectsPathEscape(t *testing.T) {
	dir := createTestDir(t)

	// Create a file outside the workspace
	outsideDir := createTestDir(t)
	createTestFile(t, outsideDir, "secret.txt", "secret data")

	validator, err := policy.NewPathValidator(dir, nil)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	reader := files.NewSafeReader(validator, 64*1024)
	ctx := context.Background()

	// Try to escape via absolute path
	_, err = reader.Read(ctx, filepath.Join(outsideDir, "secret.txt"))
	if err == nil {
		t.Fatal("Read should have failed for path outside workspace")
	}

	readErr, ok := err.(*files.ReadError)
	if !ok {
		t.Fatalf("error should be *files.ReadError, got %T", err)
	}
	if readErr.Code != "EPOLICY" {
		t.Errorf("Code = %q, want 'EPOLICY'", readErr.Code)
	}
}

func TestSafeReader_RejectsSymlinkEscape(t *testing.T) {
	workspaceDir := createTestDir(t)
	outsideDir := createTestDir(t)

	// Create a secret file outside workspace
	secretPath := createTestFile(t, outsideDir, "secret.txt", "secret data")

	// Create a symlink inside workspace pointing outside
	symlinkPath := filepath.Join(workspaceDir, "link.txt")
	if err := os.Symlink(secretPath, symlinkPath); err != nil {
		t.Skipf("cannot create symlinks (OS restriction): %v", err)
	}

	validator, err := policy.NewPathValidator(workspaceDir, nil)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	reader := files.NewSafeReader(validator, 64*1024)
	ctx := context.Background()

	// Try to read via symlink
	_, err = reader.Read(ctx, symlinkPath)
	if err == nil {
		t.Fatal("Read should have failed for symlink pointing outside workspace")
	}

	readErr, ok := err.(*files.ReadError)
	if !ok {
		t.Fatalf("error should be *files.ReadError, got %T", err)
	}
	if readErr.Code != "EPOLICY" {
		t.Errorf("Code = %q, want 'EPOLICY'", readErr.Code)
	}
}

func TestSafeReader_RejectsDirectory(t *testing.T) {
	dir := createTestDir(t)
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	validator, err := policy.NewPathValidator(dir, nil)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	reader := files.NewSafeReader(validator, 64*1024)
	ctx := context.Background()

	_, err = reader.Read(ctx, subdir)
	if err == nil {
		t.Fatal("Read should have failed for directory")
	}

	readErr, ok := err.(*files.ReadError)
	if !ok {
		t.Fatalf("error should be *files.ReadError, got %T", err)
	}
	if readErr.Code != "EARG" {
		t.Errorf("Code = %q, want 'EARG'", readErr.Code)
	}
	if !strings.Contains(readErr.Message, "directory") {
		t.Errorf("Message = %q, should contain 'directory'", readErr.Message)
	}
}

func TestSafeReader_RejectsNonExistent(t *testing.T) {
	dir := createTestDir(t)

	validator, err := policy.NewPathValidator(dir, nil)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	reader := files.NewSafeReader(validator, 64*1024)
	ctx := context.Background()

	_, err = reader.Read(ctx, filepath.Join(dir, "nonexistent.txt"))
	if err == nil {
		t.Fatal("Read should have failed for non-existent file")
	}

	readErr, ok := err.(*files.ReadError)
	if !ok {
		t.Fatalf("error should be *files.ReadError, got %T", err)
	}
	if readErr.Code != "ENOTFOUND" {
		t.Errorf("Code = %q, want 'ENOTFOUND'", readErr.Code)
	}
}

func TestSafeReader_RespectsContextCancellation(t *testing.T) {
	dir := createTestDir(t)
	createTestFile(t, dir, "test.txt", "content")

	validator, err := policy.NewPathValidator(dir, nil)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	reader := files.NewSafeReader(validator, 64*1024)

	// Cancel context before read
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = reader.Read(ctx, filepath.Join(dir, "test.txt"))
	if err == nil {
		t.Fatal("Read should have failed with cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestFileContent_LineAccess(t *testing.T) {
	dir := createTestDir(t)
	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	filePath := createTestFile(t, dir, "test.txt", content)

	validator, err := policy.NewPathValidator(dir, nil)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	reader := files.NewSafeReader(validator, 64*1024)
	ctx := context.Background()

	fc, err := reader.Read(ctx, filePath)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	// Test LineCount
	if fc.LineCount() != 5 {
		t.Errorf("LineCount() = %d, want 5", fc.LineCount())
	}

	// Test GetLine (1-indexed)
	tests := []struct {
		lineNum int
		want    string
	}{
		{1, "line 1"},
		{3, "line 3"},
		{5, "line 5"},
		{0, ""},  // out of range
		{6, ""},  // out of range
		{-1, ""}, // out of range
	}
	for _, tc := range tests {
		got := fc.GetLine(tc.lineNum)
		if got != tc.want {
			t.Errorf("GetLine(%d) = %q, want %q", tc.lineNum, got, tc.want)
		}
	}

	// Test GetLines (1-indexed, inclusive)
	lines := fc.GetLines(2, 4)
	if len(lines) != 3 {
		t.Errorf("GetLines(2, 4) length = %d, want 3", len(lines))
	}
	if lines[0] != "line 2" || lines[2] != "line 4" {
		t.Errorf("GetLines(2, 4) = %v, want ['line 2', 'line 3', 'line 4']", lines)
	}
}

func TestFileContent_LineOffsetsCorrect(t *testing.T) {
	dir := createTestDir(t)
	content := "abc\ndefgh\ni\n"
	filePath := createTestFile(t, dir, "test.txt", content)

	validator, err := policy.NewPathValidator(dir, nil)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	reader := files.NewSafeReader(validator, 64*1024)
	ctx := context.Background()

	fc, err := reader.Read(ctx, filePath)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	// Verify offsets
	// "abc\ndefgh\ni\n"
	// Line 1 starts at 0 ("abc")
	// Line 2 starts at 4 ("defgh")
	// Line 3 starts at 10 ("i")
	expectedOffsets := []int{0, 4, 10}
	if len(fc.LineOffsets) != len(expectedOffsets) {
		t.Fatalf("len(LineOffsets) = %d, want %d", len(fc.LineOffsets), len(expectedOffsets))
	}
	for i, want := range expectedOffsets {
		if fc.LineOffsets[i] != want {
			t.Errorf("LineOffsets[%d] = %d, want %d", i, fc.LineOffsets[i], want)
		}
	}
}

func TestSafeReader_HandlesRelativePath(t *testing.T) {
	dir := createTestDir(t)
	content := "test content"
	createTestFile(t, dir, "test.txt", content)

	validator, err := policy.NewPathValidator(dir, nil)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	reader := files.NewSafeReader(validator, 64*1024)
	ctx := context.Background()

	// Use relative path
	fc, err := reader.Read(ctx, "test.txt")
	if err != nil {
		t.Fatalf("Read with relative path failed: %v", err)
	}

	if string(fc.Content) != content {
		t.Errorf("Content = %q, want %q", string(fc.Content), content)
	}
}
