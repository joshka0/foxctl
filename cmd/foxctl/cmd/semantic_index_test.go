package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/platform/fsutil"
)

func TestSemanticIndexCommand_Init(t *testing.T) {
	cmd := newSemanticIndexCommand()
	if cmd.Use != "semantic-index" {
		t.Errorf("expected Use to be 'semantic-index', got %s", cmd.Use)
	}

	// Check subcommands
	subCmds := cmd.Commands()
	if len(subCmds) != 4 {
		t.Fatalf("expected 4 subcommands, got %d", len(subCmds))
	}

	var hasInit, hasUpdate, hasStats, hasDrain bool
	for _, sub := range subCmds {
		switch sub.Use {
		case "init":
			hasInit = true
		case "update":
			hasUpdate = true
		case "stats":
			hasStats = true
		case "drain":
			hasDrain = true
		}
	}

	if !hasInit {
		t.Error("expected 'init' subcommand")
	}
	if !hasUpdate {
		t.Error("expected 'update' subcommand")
	}
	if !hasStats {
		t.Error("expected 'stats' subcommand")
	}
	if !hasDrain {
		t.Error("expected 'drain' subcommand")
	}
}

func TestWriteSemanticErrorSerializesEnvelope(t *testing.T) {
	cmd := newSemanticIndexCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := writeSemanticError(cmd, "EARG", "bad input")
	if err == nil {
		t.Fatal("expected returned command error")
	}

	var env map[string]any
	if decodeErr := json.Unmarshal(buf.Bytes(), &env); decodeErr != nil {
		t.Fatalf("decode envelope: %v\nbody=%s", decodeErr, buf.String())
	}
	if env["status"] != "error" || env["command"] != "semantic_index" {
		t.Fatalf("unexpected envelope: %v", env)
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
		"chunk-delay",
		"model",
		"provider",
		"batch-size",
		"batch-delay",
		"max-file-bytes",
		"enqueue",
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
		"chunk-delay",
		"model",
		"provider",
		"batch-size",
		"batch-delay",
		"max-file-bytes",
		"enqueue",
	}

	for _, name := range expectedFlags {
		flag := cmd.Flags().Lookup(name)
		if flag == nil {
			t.Errorf("expected flag --%s to exist", name)
		}
	}
}

func TestSemanticIndexStats_Flags(t *testing.T) {
	cmd := newSemanticIndexStatsCommand()
	if cmd.Flags().Lookup("workspace") == nil {
		t.Fatal("expected flag --workspace to exist")
	}
}

func TestSemanticIndexDrain_Flags(t *testing.T) {
	cmd := newSemanticIndexDrainCommand()

	expectedFlags := []string{
		"workspace",
		"model",
		"provider",
		"batch-size",
		"max-duration",
		"process-all",
		"job-delay",
		"recover-stale-after",
		"chunk-bytes",
		"chunk-overlap",
		"chunk-delay",
	}

	for _, name := range expectedFlags {
		if cmd.Flags().Lookup(name) == nil {
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

func TestSemanticIndexBatchRanges(t *testing.T) {
	got := semanticIndexBatchRanges(5, 2)
	want := [][2]int{{0, 2}, {2, 4}, {4, 5}}
	if len(got) != len(want) {
		t.Fatalf("ranges=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("range[%d]=%v want %v", i, got[i], want[i])
		}
	}
}

func TestValidateSemanticIndexOptions(t *testing.T) {
	tests := []struct {
		name         string
		chunkBytes   int
		chunkOverlap int
		chunkDelay   time.Duration
		batchOpts    semanticIndexBatchOptions
		wantErr      string
	}{
		{
			name:      "valid defaults",
			batchOpts: semanticIndexBatchOptions{},
		},
		{
			name:       "negative chunk bytes",
			chunkBytes: -1,
			wantErr:    "chunk-bytes must be >= 0",
		},
		{
			name:         "negative chunk overlap",
			chunkOverlap: -1,
			wantErr:      "chunk-overlap must be >= 0",
		},
		{
			name:         "overlap requires chunking",
			chunkOverlap: 1,
			wantErr:      "chunk-overlap requires chunk-bytes > 0",
		},
		{
			name:         "overlap must be smaller than chunk",
			chunkBytes:   1024,
			chunkOverlap: 1024,
			wantErr:      "chunk-overlap must be less than chunk-bytes",
		},
		{
			name:       "negative chunk delay",
			chunkDelay: -time.Millisecond,
			wantErr:    "chunk-delay must be >= 0",
		},
		{
			name:      "negative batch size",
			batchOpts: semanticIndexBatchOptions{BatchSize: -1},
			wantErr:   "batch-size must be >= 0",
		},
		{
			name:      "negative batch delay",
			batchOpts: semanticIndexBatchOptions{BatchDelay: -time.Millisecond},
			wantErr:   "batch-delay must be >= 0",
		},
		{
			name:      "negative max file bytes",
			batchOpts: semanticIndexBatchOptions{MaxFileBytes: -1},
			wantErr:   "max-file-bytes must be >= 0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSemanticIndexOptions(tc.chunkBytes, tc.chunkOverlap, tc.chunkDelay, tc.batchOpts)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %q", tc.wantErr)
			}
			if err.Error() != tc.wantErr {
				t.Fatalf("error=%q want %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestFilterSemanticIndexFilesBySizeSkipsLargeFiles(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "small.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "large.go"), []byte("package main\nvar X = `0123456789`\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files := []semantic.JobFileInput{
		{Path: "small.go", ChangeKind: semantic.ChangeKindModified},
		{Path: "large.go", ChangeKind: semantic.ChangeKindModified},
		{Path: "deleted.go", ChangeKind: semantic.ChangeKindDeleted},
	}
	filtered, skipped, err := filterSemanticIndexFilesBySize(tmpDir, files, 16, nil)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 {
		t.Fatalf("skipped=%d want 1", skipped)
	}
	if len(filtered) != 2 {
		t.Fatalf("filtered=%v want small + deleted", filtered)
	}
	if filtered[0].Path != "small.go" || filtered[0].SizeBytes == 0 {
		t.Fatalf("small file not retained with size: %+v", filtered[0])
	}
	if filtered[1].Path != "deleted.go" {
		t.Fatalf("deleted file should bypass size filter: %+v", filtered[1])
	}
}

func TestSemanticIndexOpenAICompatDimensionsKnownLocalModel(t *testing.T) {
	got := semanticIndexOpenAICompatDimensions("text-embedding-embeddinggemma-300m-qat", 1024)
	if got != 768 {
		t.Fatalf("dimensions=%d want 768", got)
	}
}

func TestSemanticIndexOpenAICompatDimensionsKnownQwenLocalModel(t *testing.T) {
	got := semanticIndexOpenAICompatDimensions("text-embedding-qwen3-embedding-8b", 1024)
	if got != 4096 {
		t.Fatalf("dimensions=%d want 4096", got)
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
