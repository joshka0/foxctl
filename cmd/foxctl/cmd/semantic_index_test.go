package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/platform/fsutil"
)

func TestSemanticIndexCommand_Init(t *testing.T) {
	cmd := newSemanticIndexCommand()
	if cmd.Use != "semantic-index" {
		t.Errorf("expected Use to be 'semantic-index', got %s", cmd.Use)
	}

	// Check subcommands
	subCmds := cmd.Commands()
	if len(subCmds) != 2 {
		t.Fatalf("expected 2 subcommands, got %d", len(subCmds))
	}

	var hasInit, hasUpdate bool
	for _, sub := range subCmds {
		switch sub.Use {
		case "init":
			hasInit = true
		case "update":
			hasUpdate = true
		}
	}

	if !hasInit {
		t.Error("expected 'init' subcommand")
	}
	if !hasUpdate {
		t.Error("expected 'update' subcommand")
	}
}

func TestSemanticIndexInit_Flags(t *testing.T) {
	cmd := newSemanticIndexInitCommand()

	// Check expected flags exist
	expectedFlags := []string{
		"workspace",
		"glob",
		"dry-run",
		"task-id",
		"chunk-bytes",
		"chunk-overlap",
		"model",
	}

	for _, name := range expectedFlags {
		flag := cmd.Flags().Lookup(name)
		if flag == nil {
			t.Errorf("expected flag --%s to exist", name)
		}
	}

	// Check defaults
	if cmd.Flags().Lookup("workspace").DefValue != "." {
		t.Errorf("expected workspace default to be '.', got %s", cmd.Flags().Lookup("workspace").DefValue)
	}
	if cmd.Flags().Lookup("glob").DefValue != "**/*.go" {
		t.Errorf("expected glob default to be '**/*.go', got %s", cmd.Flags().Lookup("glob").DefValue)
	}
}

func TestSemanticIndexUpdate_Flags(t *testing.T) {
	cmd := newSemanticIndexUpdateCommand()

	// Check expected flags exist
	expectedFlags := []string{
		"workspace",
		"files",
		"deleted",
		"dry-run",
		"task-id",
		"review-id",
		"chunk-bytes",
		"chunk-overlap",
		"model",
	}

	for _, name := range expectedFlags {
		flag := cmd.Flags().Lookup(name)
		if flag == nil {
			t.Errorf("expected flag --%s to exist", name)
		}
	}
}

func TestSemanticIndexInit_DryRun(t *testing.T) {
	// Create temp workspace with test files
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "utils.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newSemanticIndexInitCommand()
	cmd.SetContext(context.Background())

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	cmd.SetArgs([]string{
		"--workspace", tmpDir,
		"--glob", "*.go",
		"--dry-run",
	})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Parse output as envelope
	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse output as JSON: %v\nOutput: %s", err, buf.String())
	}

	if result["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", result["status"])
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}

	if data["dry_run"] != true {
		t.Errorf("expected dry_run to be true")
	}

	filesCount, ok := data["files_count"].(float64)
	if !ok || filesCount < 1 {
		t.Errorf("expected files_count to be at least 1, got %v", data["files_count"])
	}
}

func TestSemanticIndexUpdate_NoFilesError(t *testing.T) {
	cmd := newSemanticIndexUpdateCommand()
	cmd.SetContext(context.Background())

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	cmd.SetArgs([]string{
		"--workspace", ".",
	})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error when no files or deleted specified")
	}

	// Should contain EARG error
	if buf.Len() > 0 {
		var result map[string]any
		if err := json.Unmarshal(buf.Bytes(), &result); err == nil {
			if errObj, ok := result["error"].(map[string]any); ok {
				if errObj["code"] != "EARG" {
					t.Errorf("expected error code EARG, got %v", errObj["code"])
				}
			}
		}
	}
}

func TestSemanticIndexOpenAICompatDimensionsKnownLocalModel(t *testing.T) {
	got := semanticIndexOpenAICompatDimensions("text-embedding-embeddinggemma-300m-qat", 1024)
	if got != 768 {
		t.Fatalf("dimensions=%d want 768", got)
	}
}

func TestSemanticIndexOpenAICompatDimensionsCustomModelUsesConfigured(t *testing.T) {
	got := semanticIndexOpenAICompatDimensions("custom-local-embedding-model", 512)
	if got != 512 {
		t.Fatalf("dimensions=%d want configured 512", got)
	}
}

func TestFindFilesMatchingGlob(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()

	// Create test files
	subDir := filepath.Join(tmpDir, "pkg")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	testFiles := []string{
		"main.go",
		"utils.go",
		"README.md",
		"pkg/lib.go",
	}

	for _, f := range testFiles {
		path := filepath.Join(tmpDir, f)
		if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Test ** glob
	files, err := fsutil.FindFilesMatchingGlob(tmpDir, "**/*.go", nil)
	if err != nil {
		t.Fatalf("fsutil.FindFilesMatchingGlob failed: %v", err)
	}

	if len(files) < 3 {
		t.Errorf("expected at least 3 .go files, got %d: %v", len(files), files)
	}

	// Verify all files are .go
	for _, f := range files {
		if !hasGoSuffix(f) {
			t.Errorf("expected .go file, got %s", f)
		}
	}
}

func hasGoSuffix(s string) bool {
	return len(s) > 3 && s[len(s)-3:] == ".go"
}

func TestFindFilesMatchingGlob_SimplePattern(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files
	if err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "c.go"), []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := fsutil.FindFilesMatchingGlob(tmpDir, "*.txt", nil)
	if err != nil {
		t.Fatalf("fsutil.FindFilesMatchingGlob failed: %v", err)
	}

	if len(files) != 2 {
		t.Errorf("expected 2 .txt files, got %d: %v", len(files), files)
	}
}
