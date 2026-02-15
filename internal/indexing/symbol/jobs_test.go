package symbol

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing"
)

// =============================================================================
// P4.C2: Symbol Index Job Contracts – Types + Unit Tests
// =============================================================================

func TestJobArgs_Validate(t *testing.T) {
	tests := []struct {
		name    string
		args    JobArgs
		wantErr string
	}{
		{
			name: "valid args",
			args: JobArgs{
				WorkspaceID: "ws-123",
				Files: []JobFileInput{
					{Path: "main.go"},
				},
			},
			wantErr: "",
		},
		{
			name: "missing workspace_id",
			args: JobArgs{
				WorkspaceID: "",
				Files: []JobFileInput{
					{Path: "main.go"},
				},
			},
			wantErr: "workspace_id is required",
		},
		{
			name: "empty files list",
			args: JobArgs{
				WorkspaceID: "ws-123",
				Files:       []JobFileInput{},
			},
			wantErr: "files list cannot be empty",
		},
		{
			name: "nil files list",
			args: JobArgs{
				WorkspaceID: "ws-123",
				Files:       nil,
			},
			wantErr: "files list cannot be empty",
		},
		{
			name: "empty file path",
			args: JobArgs{
				WorkspaceID: "ws-123",
				Files: []JobFileInput{
					{Path: ""},
				},
			},
			wantErr: "file path is required at index 0",
		},
		{
			name: "empty path at index 1",
			args: JobArgs{
				WorkspaceID: "ws-123",
				Files: []JobFileInput{
					{Path: "valid.go"},
					{Path: ""},
				},
			},
			wantErr: "file path is required at index 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.args.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.wantErr)
				} else if err.Error() != tt.wantErr {
					t.Errorf("Validate() error = %q, want %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestJobArgs_RoundTrip(t *testing.T) {
	original := JobArgs{
		WorkspaceID: "ws-test",
		Files: []JobFileInput{
			{
				Path:       "src/main.go",
				Digest:     "sha256:abc123",
				SizeBytes:  1234,
				Language:   "go",
				ChangeKind: indexing.ChangeKindModified,
			},
			{
				Path:       "README.md",
				ChangeKind: indexing.ChangeKindAdded,
			},
		},
		Reason:   ReasonPostReview,
		TaskID:   "task-123",
		ReviewID: "review-456",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var result JobArgs
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if result.WorkspaceID != original.WorkspaceID {
		t.Errorf("WorkspaceID mismatch: got %q, want %q", result.WorkspaceID, original.WorkspaceID)
	}
	if len(result.Files) != len(original.Files) {
		t.Fatalf("Files length mismatch: got %d, want %d", len(result.Files), len(original.Files))
	}
	if result.Files[0].Path != original.Files[0].Path {
		t.Errorf("Files[0].Path mismatch: got %q, want %q", result.Files[0].Path, original.Files[0].Path)
	}
	if result.Files[0].ChangeKind != indexing.ChangeKindModified {
		t.Errorf("Files[0].ChangeKind mismatch: got %q, want %q", result.Files[0].ChangeKind, indexing.ChangeKindModified)
	}
	if result.Reason != ReasonPostReview {
		t.Errorf("Reason mismatch: got %q, want %q", result.Reason, ReasonPostReview)
	}
	if result.TaskID != original.TaskID {
		t.Errorf("TaskID mismatch: got %q, want %q", result.TaskID, original.TaskID)
	}
	if result.ReviewID != original.ReviewID {
		t.Errorf("ReviewID mismatch: got %q, want %q", result.ReviewID, original.ReviewID)
	}
}

func TestJobSummary_RoundTrip(t *testing.T) {
	original := JobSummary{
		FilesIndexed:   10,
		SymbolsIndexed: 42,
		FilesSkipped:   2,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var result JobSummary
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if result.FilesIndexed != original.FilesIndexed {
		t.Errorf("FilesIndexed mismatch: got %d, want %d", result.FilesIndexed, original.FilesIndexed)
	}
	if result.SymbolsIndexed != original.SymbolsIndexed {
		t.Errorf("SymbolsIndexed mismatch: got %d, want %d", result.SymbolsIndexed, original.SymbolsIndexed)
	}
	if result.FilesSkipped != original.FilesSkipped {
		t.Errorf("FilesSkipped mismatch: got %d, want %d", result.FilesSkipped, original.FilesSkipped)
	}
}

func TestJobFailure_RoundTrip(t *testing.T) {
	ts := time.Date(2025, 11, 15, 12, 34, 56, 0, time.UTC)
	original := JobFailure{
		File: JobFailureFile{
			Path:   "foo/bar.go",
			Digest: "sha256:def456",
		},
		ErrorCode:    ErrCodeExtractFailed,
		ErrorMessage: "parse error: unexpected token",
		Timestamp:    ts,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var result JobFailure
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if result.File.Path != original.File.Path {
		t.Errorf("File.Path mismatch: got %q, want %q", result.File.Path, original.File.Path)
	}
	if result.File.Digest != original.File.Digest {
		t.Errorf("File.Digest mismatch: got %q, want %q", result.File.Digest, original.File.Digest)
	}
	if result.ErrorCode != original.ErrorCode {
		t.Errorf("ErrorCode mismatch: got %q, want %q", result.ErrorCode, original.ErrorCode)
	}
	if result.ErrorMessage != original.ErrorMessage {
		t.Errorf("ErrorMessage mismatch: got %q, want %q", result.ErrorMessage, original.ErrorMessage)
	}
	if !result.Timestamp.Equal(original.Timestamp) {
		t.Errorf("Timestamp mismatch: got %v, want %v", result.Timestamp, original.Timestamp)
	}
}

func TestJobResult_HasFailures(t *testing.T) {
	tests := []struct {
		name   string
		result JobResult
		want   bool
	}{
		{
			name:   "no failures",
			result: JobResult{Summary: JobSummary{FilesIndexed: 5}},
			want:   false,
		},
		{
			name: "with failures",
			result: JobResult{
				Summary: JobSummary{FilesIndexed: 3},
				Failures: []JobFailure{
					{File: JobFailureFile{Path: "fail.go"}, ErrorCode: ErrCodeExtractFailed},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.HasFailures(); got != tt.want {
				t.Errorf("HasFailures() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJobResult_IsPartialSuccess(t *testing.T) {
	tests := []struct {
		name   string
		result JobResult
		want   bool
	}{
		{
			name:   "full success",
			result: JobResult{Summary: JobSummary{FilesIndexed: 5}},
			want:   false,
		},
		{
			name: "partial success",
			result: JobResult{
				Summary: JobSummary{FilesIndexed: 3},
				Failures: []JobFailure{
					{File: JobFailureFile{Path: "fail.go"}, ErrorCode: ErrCodeExtractFailed},
				},
			},
			want: true,
		},
		{
			name: "all failed",
			result: JobResult{
				Summary: JobSummary{FilesIndexed: 0},
				Failures: []JobFailure{
					{File: JobFailureFile{Path: "fail.go"}, ErrorCode: ErrCodeExtractFailed},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.IsPartialSuccess(); got != tt.want {
				t.Errorf("IsPartialSuccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsRecoverableError(t *testing.T) {
	tests := []struct {
		code string
		want bool
	}{
		{ErrCodeSymbolIndexNotFound, true},
		{ErrCodeExtractorNotFound, true},
		{ErrCodeExtractFailed, false},
		{ErrCodeFileReadError, false},
		{ErrCodeFileTooLarge, false},
		{"UNKNOWN_ERROR", false},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			if got := IsRecoverableError(tt.code); got != tt.want {
				t.Errorf("IsRecoverableError(%q) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

func TestJobTypeConstants(t *testing.T) {
	if JobTypeInitFiles != "code_symbol_index.init_files" {
		t.Errorf("JobTypeInitFiles = %q, want %q", JobTypeInitFiles, "code_symbol_index.init_files")
	}
	if JobTypeUpdateFiles != "code_symbol_index.update_files" {
		t.Errorf("JobTypeUpdateFiles = %q, want %q", JobTypeUpdateFiles, "code_symbol_index.update_files")
	}
}

func TestIndexReason_Values(t *testing.T) {
	if ReasonInitialIndex != "initial_index" {
		t.Errorf("ReasonInitialIndex = %q, want %q", ReasonInitialIndex, "initial_index")
	}
	if ReasonPostReview != "post_review" {
		t.Errorf("ReasonPostReview = %q, want %q", ReasonPostReview, "post_review")
	}
	if ReasonManual != "manual" {
		t.Errorf("ReasonManual = %q, want %q", ReasonManual, "manual")
	}
}

// =============================================================================
// Integration tests for RunInitFilesJob and RunUpdateFilesJob
// =============================================================================

func TestIndexer_RunInitFilesJob(t *testing.T) {
	idx, _, workspaceDir, workspaceID := setupTestIndexer(t, Config{Enabled: true})

	// Create a test file
	content := `package main

func Hello() string {
	return "hello"
}
`
	createTestFile(t, workspaceDir, "hello.go", content)

	args := JobArgs{
		WorkspaceID: workspaceID,
		Files: []JobFileInput{
			{Path: "hello.go", Language: "go"},
		},
		Reason: ReasonInitialIndex,
		TaskID: "task-init",
	}

	result, err := idx.RunInitFilesJob(context.Background(), args)
	if err != nil {
		t.Fatalf("RunInitFilesJob failed: %v", err)
	}

	if result.Summary.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed, got %d", result.Summary.FilesIndexed)
	}
	if result.HasFailures() {
		t.Errorf("unexpected failures: %v", result.Failures)
	}
}

func TestIndexer_RunUpdateFilesJob(t *testing.T) {
	idx, _, workspaceDir, workspaceID := setupTestIndexer(t, Config{Enabled: true})

	// Create and index a test file
	content := `package main

func Greet() string {
	return "greet"
}
`
	createTestFile(t, workspaceDir, "greet.go", content)

	// First, init the file
	initArgs := JobArgs{
		WorkspaceID: workspaceID,
		Files: []JobFileInput{
			{Path: "greet.go", Language: "go"},
		},
		Reason: ReasonInitialIndex,
	}
	_, err := idx.RunInitFilesJob(context.Background(), initArgs)
	if err != nil {
		t.Fatalf("initial index failed: %v", err)
	}

	// Now update it
	updateContent := `package main

func Greet() string {
	return "updated greet"
}
`
	createTestFile(t, workspaceDir, "greet.go", updateContent)

	updateArgs := JobArgs{
		WorkspaceID: workspaceID,
		Files: []JobFileInput{
			{Path: "greet.go", Language: "go", ChangeKind: indexing.ChangeKindModified},
		},
		Reason:   ReasonPostReview,
		ReviewID: "review-123",
	}

	result, err := idx.RunUpdateFilesJob(context.Background(), updateArgs)
	if err != nil {
		t.Fatalf("RunUpdateFilesJob failed: %v", err)
	}

	if result.Summary.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed, got %d", result.Summary.FilesIndexed)
	}
	if result.HasFailures() {
		t.Errorf("unexpected failures: %v", result.Failures)
	}
}

func TestIndexer_RunInitFilesJob_ValidationError(t *testing.T) {
	idx, _, _, _ := setupTestIndexer(t, Config{Enabled: true})

	// Missing workspace_id
	args := JobArgs{
		WorkspaceID: "",
		Files: []JobFileInput{
			{Path: "test.go"},
		},
	}

	_, err := idx.RunInitFilesJob(context.Background(), args)
	if err == nil {
		t.Error("expected validation error for missing workspace_id")
	}
}

func TestIndexer_RunUpdateFilesJob_DeletedFile(t *testing.T) {
	idx, store, workspaceDir, workspaceID := setupTestIndexer(t, Config{Enabled: true})
	ctx := context.Background()

	// Create and index a test file
	content := `package main

func ToDelete() {}
`
	createTestFile(t, workspaceDir, "delete_me.go", content)

	initArgs := JobArgs{
		WorkspaceID: workspaceID,
		Files: []JobFileInput{
			{Path: "delete_me.go", Language: "go"},
		},
		Reason: ReasonInitialIndex,
	}
	_, err := idx.RunInitFilesJob(ctx, initArgs)
	if err != nil {
		t.Fatalf("initial index failed: %v", err)
	}

	// Verify symbol exists
	_, err = store.Get(ctx, keyEntryName(workspaceID, "delete_me.go", "ToDelete"), workspaceID)
	if err != nil {
		t.Fatalf("symbol should exist: %v", err)
	}

	// Delete the file via update job
	deleteArgs := JobArgs{
		WorkspaceID: workspaceID,
		Files: []JobFileInput{
			{Path: "delete_me.go", ChangeKind: indexing.ChangeKindDeleted},
		},
		Reason: ReasonPostReview,
	}

	result, err := idx.RunUpdateFilesJob(ctx, deleteArgs)
	if err != nil {
		t.Fatalf("RunUpdateFilesJob for delete failed: %v", err)
	}

	if result.Summary.FilesIndexed != 1 {
		t.Errorf("expected 1 file processed for deletion, got %d", result.Summary.FilesIndexed)
	}

	// Verify symbol is gone
	_, err = store.Get(ctx, keyEntryName(workspaceID, "delete_me.go", "ToDelete"), workspaceID)
	if err == nil {
		t.Error("symbol should have been deleted")
	}
}
