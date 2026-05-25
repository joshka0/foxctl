package semantic

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/quick"
	"time"
)

// =============================================================================
// P3.S3: Embedding Job Contracts – Types + Unit Tests
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
			name: "blank workspace_id",
			args: JobArgs{
				WorkspaceID: " \t\n",
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
			name: "blank file path",
			args: JobArgs{
				WorkspaceID: "ws-123",
				Files: []JobFileInput{
					{Path: " \t\n"},
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

func TestJobArgsValidatePropertyRequiresNonBlankWorkspaceAndPath(t *testing.T) {
	property := func(raw string) bool {
		blank := strings.TrimSpace(raw) == ""
		workspaceArgs := JobArgs{
			WorkspaceID: raw,
			Files:       []JobFileInput{{Path: "main.go"}},
		}
		pathArgs := JobArgs{
			WorkspaceID: "ws-123",
			Files:       []JobFileInput{{Path: raw}},
		}
		workspaceErr := workspaceArgs.Validate() != nil
		pathErr := pathArgs.Validate() != nil

		return workspaceErr == blank && pathErr == blank
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatalf("non-blank job args property failed: %v", err)
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
				ChangeKind: ChangeKindModified,
			},
			{
				Path:       "README.md",
				ChangeKind: ChangeKindAdded,
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
	if result.Files[0].ChangeKind != ChangeKindModified {
		t.Errorf("Files[0].ChangeKind mismatch: got %q, want %q", result.Files[0].ChangeKind, ChangeKindModified)
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
		FilesIndexed:  10,
		ChunksIndexed: 25,
		FilesSkipped:  2,
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
	if result.ChunksIndexed != original.ChunksIndexed {
		t.Errorf("ChunksIndexed mismatch: got %d, want %d", result.ChunksIndexed, original.ChunksIndexed)
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
		ErrorCode:         ErrCodeEmbeddingProviderFailure,
		ErrorMessage:      "HTTP 503 from embedding provider",
		ProviderRequestID: "req-123",
		Timestamp:         ts,
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
	if result.ProviderRequestID != original.ProviderRequestID {
		t.Errorf("ProviderRequestID mismatch: got %q, want %q", result.ProviderRequestID, original.ProviderRequestID)
	}
	if !result.Timestamp.Equal(original.Timestamp) {
		t.Errorf("Timestamp mismatch: got %v, want %v", result.Timestamp, original.Timestamp)
	}
}

func TestJobResult_RoundTrip(t *testing.T) {
	ts := time.Date(2025, 11, 15, 12, 34, 56, 0, time.UTC)
	original := JobResult{
		Summary: JobSummary{
			FilesIndexed:  12,
			ChunksIndexed: 34,
			FilesSkipped:  1,
		},
		Failures: []JobFailure{
			{
				File: JobFailureFile{
					Path:   "failing.go",
					Digest: "sha256:xxx",
				},
				ErrorCode:    ErrCodeCASResolveError,
				ErrorMessage: "CAS read failed",
				Timestamp:    ts,
			},
		},
		CASArtifact: &CASArtifact{
			ArtifactID:   "semantic_index.update_files:01HF",
			Path:         "jobs/01HF.../semantic_index_results.ndjson",
			Digest:       "sha256:artifact123",
			EntriesCount: 46,
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var result JobResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify summary
	if result.Summary.FilesIndexed != original.Summary.FilesIndexed {
		t.Errorf("Summary.FilesIndexed mismatch: got %d, want %d", result.Summary.FilesIndexed, original.Summary.FilesIndexed)
	}

	// Verify failures
	if len(result.Failures) != 1 {
		t.Fatalf("Failures length mismatch: got %d, want 1", len(result.Failures))
	}
	if result.Failures[0].ErrorCode != ErrCodeCASResolveError {
		t.Errorf("Failures[0].ErrorCode mismatch: got %q, want %q", result.Failures[0].ErrorCode, ErrCodeCASResolveError)
	}

	// Verify CAS artifact
	if result.CASArtifact == nil {
		t.Fatal("CASArtifact should not be nil")
	}
	if result.CASArtifact.ArtifactID != original.CASArtifact.ArtifactID {
		t.Errorf("CASArtifact.ArtifactID mismatch: got %q, want %q", result.CASArtifact.ArtifactID, original.CASArtifact.ArtifactID)
	}
	if result.CASArtifact.EntriesCount != 46 {
		t.Errorf("CASArtifact.EntriesCount mismatch: got %d, want 46", result.CASArtifact.EntriesCount)
	}
}

func TestJobResult_HasFailures(t *testing.T) {
	tests := []struct {
		name     string
		result   JobResult
		expected bool
	}{
		{
			name: "no failures",
			result: JobResult{
				Summary: JobSummary{FilesIndexed: 5},
			},
			expected: false,
		},
		{
			name: "with failures",
			result: JobResult{
				Summary: JobSummary{FilesIndexed: 4},
				Failures: []JobFailure{
					{File: JobFailureFile{Path: "x.go"}, ErrorCode: "TEST"},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.HasFailures(); got != tt.expected {
				t.Errorf("HasFailures() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestJobResult_IsPartialSuccess(t *testing.T) {
	tests := []struct {
		name     string
		result   JobResult
		expected bool
	}{
		{
			name: "full success",
			result: JobResult{
				Summary: JobSummary{FilesIndexed: 5},
			},
			expected: false,
		},
		{
			name: "partial success",
			result: JobResult{
				Summary: JobSummary{FilesIndexed: 4},
				Failures: []JobFailure{
					{File: JobFailureFile{Path: "x.go"}, ErrorCode: "TEST"},
				},
			},
			expected: true,
		},
		{
			name: "complete failure",
			result: JobResult{
				Summary: JobSummary{FilesIndexed: 0},
				Failures: []JobFailure{
					{File: JobFailureFile{Path: "x.go"}, ErrorCode: "TEST"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.IsPartialSuccess(); got != tt.expected {
				t.Errorf("IsPartialSuccess() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsRecoverableError(t *testing.T) {
	tests := []struct {
		code        string
		recoverable bool
	}{
		{ErrCodeSemanticIndexNotFound, true},
		{ErrCodeChunkBoundaryMismatch, true},
		{ErrCodeVectorNotEnabled, true},
		{ErrCodeEmbeddingProviderFailure, true},
		{ErrCodeProviderConfigInvalid, false},
		{ErrCodeCASResolveError, false},
		{"UNKNOWN_ERROR", false},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			if got := IsRecoverableError(tt.code); got != tt.recoverable {
				t.Errorf("IsRecoverableError(%q) = %v, want %v", tt.code, got, tt.recoverable)
			}
		})
	}
}

func TestJobTypeConstants(t *testing.T) {
	// Verify job type constants match the spec naming
	if JobTypeInitFiles != "semantic_index.init_files" {
		t.Errorf("JobTypeInitFiles = %q, want %q", JobTypeInitFiles, "semantic_index.init_files")
	}
	if JobTypeUpdateFiles != "semantic_index.update_files" {
		t.Errorf("JobTypeUpdateFiles = %q, want %q", JobTypeUpdateFiles, "semantic_index.update_files")
	}
}

func TestIndexReason_Values(t *testing.T) {
	// Verify reason constants
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

func TestFileChangeKind_Values(t *testing.T) {
	// Verify change kind constants
	if ChangeKindAdded != "added" {
		t.Errorf("ChangeKindAdded = %q, want %q", ChangeKindAdded, "added")
	}
	if ChangeKindModified != "modified" {
		t.Errorf("ChangeKindModified = %q, want %q", ChangeKindModified, "modified")
	}
	if ChangeKindDeleted != "deleted" {
		t.Errorf("ChangeKindDeleted = %q, want %q", ChangeKindDeleted, "deleted")
	}
}

func TestJobArgs_JSONShape(t *testing.T) {
	args := JobArgs{
		WorkspaceID: "ws-test",
		Files: []JobFileInput{
			{Path: "main.go", ChangeKind: ChangeKindModified},
		},
		Reason:   ReasonPostReview,
		TaskID:   "task-123",
		ReviewID: "review-456",
	}

	data, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}

	// Verify expected keys
	expectedKeys := []string{"workspace_id", "files", "reason", "task_id", "review_id"}
	for _, key := range expectedKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected key %q in JSON", key)
		}
	}
}

func TestJobFailure_OmitEmptyProviderRequestID(t *testing.T) {
	failure := JobFailure{
		File:         JobFailureFile{Path: "test.go"},
		ErrorCode:    ErrCodeEmbeddingProviderFailure,
		ErrorMessage: "test error",
		Timestamp:    time.Now(),
		// ProviderRequestID is empty
	}

	data, err := json.Marshal(failure)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}

	if _, exists := raw["provider_request_id"]; exists {
		t.Error("provider_request_id should be omitted when empty")
	}
}
